package internal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	bankv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/bank/v1"
	commonv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/common/v1"
	consentv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/consent/v1"
	ledgerv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/ledger/v1"
	riskv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/risk/v1"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/domainerr"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/idempotency"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	platformOwnerRef     = "platform"
	idempotencyWait      = 5 * time.Second
	minPlatformFeePence  = int64(1)
	platformFeeRateBPS   = int64(100) // 1% = 100 basis points
)

// paymentRepo is the persistence surface used by the payment saga orchestrator.
// *Repo implements it; tests supply an in-memory fake.
type paymentRepo interface {
	CreatePayment(ctx context.Context, p *Payment) error
	GetByID(ctx context.Context, id uuid.UUID) (*Payment, error)
	GetByIdempotencyKey(ctx context.Context, key string) (*Payment, error)
	UpdateAfterConsent(ctx context.Context, p *Payment, reservationID uuid.UUID, consumerID string) error
	UpdateAfterRisk(ctx context.Context, p *Payment, score int32, decision string) error
	UpdateAfterBank(ctx context.Context, p *Payment, bankRef string) error
	SettleWithOutbox(ctx context.Context, p *Payment) error
	MarkFailed(ctx context.Context, p *Payment, reason string) error
	MarkManualReview(ctx context.Context, p *Payment, reason string) error
	ResetForRetry(ctx context.Context, p *Payment) error
}

type Orchestrator struct {
	repo    paymentRepo
	idem    *idempotency.Store
	consent consentv1.ConsentServiceClient
	risk    riskv1.RiskServiceClient
	bank    bankv1.BankAdapterClient
	ledger  ledgerv1.LedgerServiceClient
}

func NewOrchestrator(
	repo paymentRepo,
	idem *idempotency.Store,
	consent consentv1.ConsentServiceClient,
	risk riskv1.RiskServiceClient,
	bank bankv1.BankAdapterClient,
	ledger ledgerv1.LedgerServiceClient,
) *Orchestrator {
	return &Orchestrator{
		repo:    repo,
		idem:    idem,
		consent: consent,
		risk:    risk,
		bank:    bank,
		ledger:  ledger,
	}
}

type InitiateInput struct {
	IdempotencyKey string
	MerchantID     string
	ConsentID      string
	AmountPence    int64
	Currency       string
	Description    string
	// SkipIdempotency is set for RetryPayment — reuses existing payment row.
	SkipIdempotency bool
	// Existing is set when retrying a FAILED payment.
	Existing *Payment
}

func (o *Orchestrator) Initiate(ctx context.Context, in InitiateInput) (*Payment, error) {
	if in.SkipIdempotency {
		if in.Existing == nil {
			return nil, domainerr.New(domainerr.CodeInternal, "retry requires existing payment")
		}
		if err := o.repo.ResetForRetry(ctx, in.Existing); err != nil {
			return nil, err
		}
		if err := o.runSaga(ctx, in.Existing); err != nil {
			// Terminal state already persisted; return payment + domain error when useful.
			p, gerr := o.repo.GetByID(ctx, in.Existing.ID)
			if gerr == nil {
				return p, err
			}
			return in.Existing, err
		}
		return o.repo.GetByID(ctx, in.Existing.ID)
	}

	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return nil, domainerr.New(domainerr.CodeValidation, "idempotency_key is required")
	}
	if err := validateInitiate(in); err != nil {
		return nil, err
	}

	ok, completed, err := o.idem.Begin(ctx, in.IdempotencyKey)
	if err != nil {
		return nil, domainerr.Wrap(domainerr.CodeInternal, "idempotency begin", err)
	}
	if !ok {
		if completed != "" {
			id, perr := uuid.Parse(completed)
			if perr != nil {
				// value might be bare id
				return o.repo.GetByIdempotencyKey(ctx, in.IdempotencyKey)
			}
			return o.repo.GetByID(ctx, id)
		}
		// Concurrent processing — wait for completion.
		val, done, werr := o.idem.WaitForCompletion(ctx, in.IdempotencyKey, idempotencyWait)
		if werr != nil {
			return nil, domainerr.Wrap(domainerr.CodeInternal, "idempotency wait", werr)
		}
		if done && val != "" {
			if id, perr := uuid.Parse(val); perr == nil {
				return o.repo.GetByID(ctx, id)
			}
		}
		// Fallback to DB lookup by key.
		if p, gerr := o.repo.GetByIdempotencyKey(ctx, in.IdempotencyKey); gerr == nil {
			return p, nil
		}
		return nil, domainerr.New(domainerr.CodeConflict, "payment still processing")
	}

	merchantID, err := uuid.Parse(in.MerchantID)
	if err != nil {
		_ = o.idem.Release(ctx, in.IdempotencyKey)
		return nil, domainerr.New(domainerr.CodeValidation, "invalid merchant_id")
	}
	consentID, err := uuid.Parse(in.ConsentID)
	if err != nil {
		_ = o.idem.Release(ctx, in.IdempotencyKey)
		return nil, domainerr.New(domainerr.CodeValidation, "invalid consent_id")
	}

	p := &Payment{
		ID:             uuid.New(),
		IdempotencyKey: in.IdempotencyKey,
		MerchantID:     merchantID,
		ConsentID:      consentID,
		AmountPence:    in.AmountPence,
		Currency:       strings.ToUpper(strings.TrimSpace(in.Currency)),
		Status:         StatusInitiated,
		Description:    in.Description,
	}
	if err := o.repo.CreatePayment(ctx, p); err != nil {
		_ = o.idem.Release(ctx, in.IdempotencyKey)
		// Race: another writer inserted same key.
		if domainerr.Is(err, domainerr.CodeDuplicateIdempotency) {
			if existing, gerr := o.repo.GetByIdempotencyKey(ctx, in.IdempotencyKey); gerr == nil {
				_ = o.idem.Complete(ctx, in.IdempotencyKey, existing.ID.String())
				return existing, nil
			}
		}
		return nil, err
	}

	sagaErr := o.runSaga(ctx, p)

	// Always complete idempotency with payment id once a row exists (success or terminal fail).
	if cerr := o.idem.Complete(ctx, in.IdempotencyKey, p.ID.String()); cerr != nil {
		slog.ErrorContext(ctx, "idempotency complete failed", "payment_id", p.ID, "err", cerr)
	}

	fresh, gerr := o.repo.GetByID(ctx, p.ID)
	if gerr != nil {
		if sagaErr != nil {
			return p, sagaErr
		}
		return p, gerr
	}
	return fresh, sagaErr
}

func validateInitiate(in InitiateInput) error {
	if strings.TrimSpace(in.MerchantID) == "" {
		return domainerr.New(domainerr.CodeValidation, "merchant_id is required")
	}
	if strings.TrimSpace(in.ConsentID) == "" {
		return domainerr.New(domainerr.CodeValidation, "consent_id is required")
	}
	if in.AmountPence <= 0 {
		return domainerr.New(domainerr.CodeValidation, "amount must be positive")
	}
	cur := strings.ToUpper(strings.TrimSpace(in.Currency))
	if len(cur) != 3 {
		return domainerr.New(domainerr.CodeValidation, "currency must be 3-letter ISO code")
	}
	return nil
}

func (o *Orchestrator) runSaga(ctx context.Context, p *Payment) error {
	start := time.Now()
	log := slog.With("payment_id", p.ID.String(), "merchant_id", p.MerchantID.String())

	// Step 1: Consent reserve
	stepStart := time.Now()
	reserve, err := o.consent.ValidateAndReserve(ctx, &consentv1.ReserveRequest{
		ConsentId: p.ConsentID.String(),
		PaymentId: p.ID.String(),
		Amount:    &commonv1.Money{AmountPence: p.AmountPence, Currency: p.Currency},
	})
	log.InfoContext(ctx, "saga step", "step", "consent.ValidateAndReserve", "duration", time.Since(stepStart).String(), "err", err)
	if err != nil {
		reason := "CONSENT_REJECTED"
		if mapped := mapDownstreamErr(err); mapped != nil {
			if de, ok := asDomain(mapped); ok {
				reason = string(de.Code)
				_ = o.repo.MarkFailed(ctx, p, reason+": "+de.Message)
				return mapped
			}
		}
		_ = o.repo.MarkFailed(ctx, p, reason+": "+err.Error())
		return mapDownstreamErr(err)
	}
	resID, err := uuid.Parse(reserve.GetReservationId())
	if err != nil {
		_ = o.repo.MarkFailed(ctx, p, "invalid reservation_id from consent")
		return domainerr.Wrap(domainerr.CodeInternal, "parse reservation_id", err)
	}
	if err := o.repo.UpdateAfterConsent(ctx, p, resID, reserve.GetConsumerId()); err != nil {
		p.ReservationID = &resID
		if cerr := o.compensate(ctx, p, "persist consent failed", true, false); cerr != nil {
			_ = o.repo.MarkManualReview(ctx, p, "persist consent failed; compensate failed: "+cerr.Error())
			return err
		}
		_ = o.repo.MarkFailed(ctx, p, "persist consent failed")
		return err
	}
	bankConsentRef := reserve.GetBankConsentRef()

	// Step 2: Risk score
	stepStart = time.Now()
	scoreResp, err := o.risk.Score(ctx, &riskv1.ScoreRequest{
		MerchantId: p.MerchantID.String(),
		ConsumerId: p.ConsumerID,
		ConsentId:  p.ConsentID.String(),
		PaymentId:  p.ID.String(),
		Amount:     &commonv1.Money{AmountPence: p.AmountPence, Currency: p.Currency},
	})
	log.InfoContext(ctx, "saga step", "step", "risk.Score", "duration", time.Since(stepStart).String(), "err", err)
	if err != nil {
		return o.failAndCompensate(ctx, p, "RISK_ERROR: "+err.Error(), true, false, mapDownstreamErr(err))
	}
	decision := scoreResp.GetDecision()
	decisionName := decision.String()
	if decision == riskv1.RiskDecision_DECLINE {
		p.RiskScore = new(scoreResp.GetScore())
		p.RiskDecision = decisionName
		return o.failAndCompensate(ctx, p, "RISK_DECLINED: "+scoreResp.GetReason(), true, false,
			domainerr.New(domainerr.CodeRiskDeclined, scoreResp.GetReason()))
	}
	// ALLOW and REVIEW both continue; REVIEW still RISK_PASSED with decision flag
	if err := o.repo.UpdateAfterRisk(ctx, p, scoreResp.GetScore(), decisionName); err != nil {
		return o.failAndCompensate(ctx, p, "persist risk failed", true, false, err)
	}

	// Step 3: Bank initiate
	stepStart = time.Now()
	bankResp, err := o.bank.InitiatePayment(ctx, &bankv1.InitiateRequest{
		PaymentId:      p.ID.String(),
		BankConsentRef: bankConsentRef,
		ConsumerId:     p.ConsumerID,
		Amount:         &commonv1.Money{AmountPence: p.AmountPence, Currency: p.Currency},
		Description:    p.Description,
	})
	log.InfoContext(ctx, "saga step", "step", "bank.InitiatePayment", "duration", time.Since(stepStart).String(), "err", err)
	if err != nil {
		return o.failAndCompensate(ctx, p, "BANK_ERROR: "+err.Error(), true, false, mapDownstreamErr(err))
	}
	if bankResp.GetStatus() == bankv1.BankPaymentStatus_REJECTED {
		reason := bankResp.GetFailureReason()
		if reason == "" {
			reason = "bank rejected payment"
		}
		return o.failAndCompensate(ctx, p, "BANK_REJECTED: "+reason, true, false,
			domainerr.New(domainerr.CodeBankRejected, reason))
	}
	if err := o.repo.UpdateAfterBank(ctx, p, bankResp.GetBankPaymentRef()); err != nil {
		// Bank already moved money / authorised — compensate bank + consent
		return o.failAndCompensate(ctx, p, "persist bank failed", true, true, err)
	}

	// Step 4: Ledger double-entry
	fee := platformFee(p.AmountPence)
	merchantAmt := p.AmountPence - fee
	stepStart = time.Now()
	_, err = o.ledger.PostDoubleEntry(ctx, &ledgerv1.PostEntryRequest{
		PaymentId:   p.ID.String(),
		Description: fmt.Sprintf("VRP settlement %s", p.ID.String()),
		Lines: []*ledgerv1.JournalLine{
			{
				AccountType: ledgerv1.AccountType_CONSUMER_ESCROW,
				OwnerRef:    p.ConsumerID,
				Direction:   ledgerv1.Direction_DR,
				Amount:      &commonv1.Money{AmountPence: p.AmountPence, Currency: p.Currency},
			},
			{
				AccountType: ledgerv1.AccountType_MERCHANT_ESCROW,
				OwnerRef:    p.MerchantID.String(),
				Direction:   ledgerv1.Direction_CR,
				Amount:      &commonv1.Money{AmountPence: merchantAmt, Currency: p.Currency},
			},
			{
				AccountType: ledgerv1.AccountType_PLATFORM_FEE,
				OwnerRef:    platformOwnerRef,
				Direction:   ledgerv1.Direction_CR,
				Amount:      &commonv1.Money{AmountPence: fee, Currency: p.Currency},
			},
		},
	})
	log.InfoContext(ctx, "saga step", "step", "ledger.PostDoubleEntry", "duration", time.Since(stepStart).String(), "err", err)
	if err != nil {
		return o.failAndCompensate(ctx, p, "LEDGER_ERROR: "+err.Error(), true, true, mapDownstreamErr(err))
	}

	// Step 5: Settle + outbox (same DB tx)
	stepStart = time.Now()
	if err := o.repo.SettleWithOutbox(ctx, p); err != nil {
		// Funds moved + ledger posted — reverse bank, reverse ledger, release consent
		if cerr := o.compensateFull(ctx, p, "settle failed"); cerr != nil {
			_ = o.repo.MarkManualReview(ctx, p, "settle failed; compensate failed: "+cerr.Error())
			return domainerr.Wrap(domainerr.CodeInternal, "settle and compensate failed", err)
		}
		_ = o.repo.MarkFailed(ctx, p, "SETTLE_FAILED: "+err.Error())
		return err
	}
	log.InfoContext(ctx, "saga step", "step", "settle+outbox", "duration", time.Since(stepStart).String())

	// Step 6: Confirm reservation
	stepStart = time.Now()
	if p.ReservationID != nil {
		_, err = o.consent.ConfirmReservation(ctx, &consentv1.ConfirmRequest{
			ReservationId: p.ReservationID.String(),
			PaymentId:     p.ID.String(),
		})
		log.InfoContext(ctx, "saga step", "step", "consent.ConfirmReservation", "duration", time.Since(stepStart).String(), "err", err)
		if err != nil {
			// Payment already SETTLED; log loudly but do not fail the client path.
			slog.ErrorContext(ctx, "confirm reservation failed after settle", "payment_id", p.ID, "err", err)
		}
	}

	log.InfoContext(ctx, "saga completed", "status", p.Status, "duration", time.Since(start).String())
	return nil
}

func (o *Orchestrator) failAndCompensate(ctx context.Context, p *Payment, reason string, releaseConsent, reverseBank bool, retErr error) error {
	if err := o.compensate(ctx, p, reason, releaseConsent, reverseBank); err != nil {
		_ = o.repo.MarkManualReview(ctx, p, reason+"; compensate failed: "+err.Error())
		if retErr != nil {
			return retErr
		}
		return domainerr.New(domainerr.CodeInternal, "compensation failed")
	}
	_ = o.repo.MarkFailed(ctx, p, reason)
	if retErr != nil {
		return retErr
	}
	return domainerr.New(domainerr.CodeInternal, reason)
}

func (o *Orchestrator) compensate(ctx context.Context, p *Payment, reason string, releaseConsent, reverseBank bool) error {
	var errs []string
	if reverseBank && p.BankPaymentRef != "" {
		_, err := o.bank.ReversePayment(ctx, &bankv1.ReverseRequest{
			BankPaymentRef: p.BankPaymentRef,
			Reason:         reason,
		})
		if err != nil {
			slog.ErrorContext(ctx, "compensate bank reverse failed", "payment_id", p.ID, "err", err)
			errs = append(errs, "bank: "+err.Error())
		}
	}
	if releaseConsent && p.ReservationID != nil {
		_, err := o.consent.ReleaseReservation(ctx, &consentv1.ReleaseRequest{
			ReservationId: p.ReservationID.String(),
			Reason:        reason,
		})
		if err != nil {
			slog.ErrorContext(ctx, "compensate consent release failed", "payment_id", p.ID, "err", err)
			errs = append(errs, "consent: "+err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (o *Orchestrator) compensateFull(ctx context.Context, p *Payment, reason string) error {
	var errs []string
	_, err := o.ledger.ReverseEntry(ctx, &ledgerv1.ReverseRequest{
		PaymentId: p.ID.String(),
		Reason:    reason,
	})
	if err != nil {
		slog.ErrorContext(ctx, "compensate ledger reverse failed", "payment_id", p.ID, "err", err)
		errs = append(errs, "ledger: "+err.Error())
	}
	if err := o.compensate(ctx, p, reason, true, true); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func platformFee(amountPence int64) int64 {
	fee := amountPence * platformFeeRateBPS / 10000
	if fee < minPlatformFeePence {
		fee = minPlatformFeePence
	}
	if fee > amountPence {
		fee = amountPence
	}
	return fee
}

func asDomain(err error) (*domainerr.Error, bool) {
	var de *domainerr.Error
	if errors.As(err, &de) {
		return de, true
	}
	return nil, false
}

func mapDownstreamErr(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := asDomain(err); ok {
		return err
	}
	st, ok := status.FromError(err)
	if !ok {
		return domainerr.Wrap(domainerr.CodeInternal, "downstream error", err)
	}
	msg := st.Message()
	code := domainerr.CodeInternal
	// Messages often look like "CONSENT_LIMIT_EXCEEDED: ..."
	upper := strings.ToUpper(msg)
	switch {
	case strings.Contains(upper, string(domainerr.CodeConsentLimitExceeded)):
		code = domainerr.CodeConsentLimitExceeded
	case strings.Contains(upper, string(domainerr.CodeConsentExpired)):
		code = domainerr.CodeConsentExpired
	case strings.Contains(upper, string(domainerr.CodeConsentRevoked)):
		code = domainerr.CodeConsentRevoked
	case strings.Contains(upper, string(domainerr.CodeConsentInactive)):
		code = domainerr.CodeConsentInactive
	case strings.Contains(upper, string(domainerr.CodeRiskDeclined)):
		code = domainerr.CodeRiskDeclined
	case strings.Contains(upper, string(domainerr.CodeBankRejected)):
		code = domainerr.CodeBankRejected
	case strings.Contains(upper, string(domainerr.CodeBankUnavailable)):
		code = domainerr.CodeBankUnavailable
	case strings.Contains(upper, string(domainerr.CodeMerchantSuspended)):
		code = domainerr.CodeMerchantSuspended
	case st.Code() == codes.NotFound:
		code = domainerr.CodeNotFound
	case st.Code() == codes.InvalidArgument:
		code = domainerr.CodeValidation
	case st.Code() == codes.Unavailable:
		code = domainerr.CodeBankUnavailable
	case st.Code() == codes.FailedPrecondition:
		code = domainerr.CodeConflict
	}
	return domainerr.Wrap(code, msg, err)
}
