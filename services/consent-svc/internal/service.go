package internal

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/domainerr"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/redis/go-redis/v9"
)

const cacheTTL = 5 * time.Minute

type Service struct {
	repo *Repo
	rdb  *redis.Client
}

func NewService(repo *Repo, rdb *redis.Client) *Service {
	return &Service{repo: repo, rdb: rdb}
}

type CreateInput struct {
	MerchantID      string
	ConsumerID      string
	BankConsentRef  string
	MaxAmountPence  int64
	MaxMonthlyPence int64
	Currency        string
	ValidUntil      time.Time
}

type ReserveInput struct {
	ConsentID   string
	PaymentID   string
	AmountPence int64
	Currency    string
}

type ReserveResult struct {
	ReservationID     string
	RemainingMonthly  int64
	Currency          string
	ConsumerID        string
	BankConsentRef    string
}

type UsageResult struct {
	UsedPence      int64
	RemainingPence int64
	Currency       string
	TxCount        int32
}

func (s *Service) CreateConsent(ctx context.Context, in CreateInput) (*ConsentRow, error) {
	if in.MerchantID == "" {
		return nil, domainerr.New(domainerr.CodeValidation, "merchant_id is required")
	}
	if in.ConsumerID == "" {
		return nil, domainerr.New(domainerr.CodeValidation, "consumer_id is required")
	}
	if in.BankConsentRef == "" {
		return nil, domainerr.New(domainerr.CodeValidation, "bank_consent_ref is required")
	}
	if in.MaxAmountPence <= 0 {
		return nil, domainerr.New(domainerr.CodeValidation, "max_per_transaction must be positive")
	}
	if in.MaxMonthlyPence <= 0 {
		return nil, domainerr.New(domainerr.CodeValidation, "max_per_month must be positive")
	}
	if in.MaxAmountPence > in.MaxMonthlyPence {
		return nil, domainerr.New(domainerr.CodeValidation, "max_per_transaction cannot exceed max_per_month")
	}
	if in.ValidUntil.IsZero() || !in.ValidUntil.After(time.Now()) {
		return nil, domainerr.New(domainerr.CodeValidation, "valid_until must be in the future")
	}
	cur := in.Currency
	if cur == "" {
		cur = defaultCurrency
	}

	// Demo: bank auth already completed — activate immediately.
	row := &ConsentRow{
		MerchantID:      in.MerchantID,
		ConsumerID:      in.ConsumerID,
		BankConsentRef:  in.BankConsentRef,
		Status:          StatusActive,
		MaxAmountPence:  in.MaxAmountPence,
		MaxMonthlyPence: in.MaxMonthlyPence,
		Currency:        cur,
		ValidUntil:      in.ValidUntil,
	}
	if err := s.repo.InsertConsent(ctx, row); err != nil {
		if isUniqueViolation(err) {
			return nil, domainerr.Wrap(domainerr.CodeAlreadyExists, "bank_consent_ref already registered", err)
		}
		return nil, domainerr.Wrap(domainerr.CodeInternal, "insert consent", err)
	}
	s.cacheSet(ctx, row)
	return row, nil
}

func (s *Service) GetConsent(ctx context.Context, consentID, merchantID string) (*ConsentRow, error) {
	if consentID == "" {
		return nil, domainerr.New(domainerr.CodeValidation, "consent_id is required")
	}
	if c := s.cacheGet(ctx, consentID); c != nil {
		if merchantID != "" && c.MerchantID != merchantID {
			return nil, domainerr.New(domainerr.CodeNotFound, "consent not found")
		}
		return c, nil
	}

	row, err := s.repo.GetConsent(ctx, consentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainerr.New(domainerr.CodeNotFound, "consent not found")
	}
	if err != nil {
		return nil, domainerr.Wrap(domainerr.CodeInternal, "get consent", err)
	}
	if merchantID != "" && row.MerchantID != merchantID {
		return nil, domainerr.New(domainerr.CodeNotFound, "consent not found")
	}
	s.cacheSet(ctx, row)
	return row, nil
}

func (s *Service) RevokeConsent(ctx context.Context, consentID, merchantID, _ string) (*ConsentRow, error) {
	if consentID == "" {
		return nil, domainerr.New(domainerr.CodeValidation, "consent_id is required")
	}

	// Load first for precise error codes.
	existing, err := s.repo.GetConsent(ctx, consentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainerr.New(domainerr.CodeNotFound, "consent not found")
	}
	if err != nil {
		return nil, domainerr.Wrap(domainerr.CodeInternal, "get consent", err)
	}
	if merchantID != "" && existing.MerchantID != merchantID {
		return nil, domainerr.New(domainerr.CodeNotFound, "consent not found")
	}
	if existing.Status == StatusRevoked {
		s.cacheDel(ctx, consentID)
		return existing, nil
	}
	if existing.Status != StatusActive {
		return nil, domainerr.New(domainerr.CodeConsentInactive, "consent is not active")
	}

	row, err := s.repo.RevokeConsent(ctx, consentID, merchantID)
	if errors.Is(err, pgx.ErrNoRows) {
		// Race: status changed between get and update.
		return nil, domainerr.New(domainerr.CodeConflict, "consent could not be revoked")
	}
	if err != nil {
		return nil, domainerr.Wrap(domainerr.CodeInternal, "revoke consent", err)
	}
	s.cacheDel(ctx, consentID)
	return row, nil
}

func (s *Service) ListConsents(ctx context.Context, consumerID, merchantID, status string, limit, offset int) ([]ConsentRow, int, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}
	rows, total, err := s.repo.ListConsents(ctx, consumerID, merchantID, status, limit, offset)
	if err != nil {
		return nil, 0, domainerr.Wrap(domainerr.CodeInternal, "list consents", err)
	}
	return rows, total, nil
}

func (s *Service) ValidateAndReserve(ctx context.Context, in ReserveInput) (*ReserveResult, error) {
	if in.ConsentID == "" {
		return nil, domainerr.New(domainerr.CodeValidation, "consent_id is required")
	}
	if in.PaymentID == "" {
		return nil, domainerr.New(domainerr.CodeValidation, "payment_id is required")
	}
	if in.AmountPence <= 0 {
		return nil, domainerr.New(domainerr.CodeValidation, "amount must be positive")
	}

	var result *ReserveResult
	err := pgx.BeginFunc(ctx, s.repo.Pool(), func(tx pgx.Tx) error {
		// Idempotent: payment_id already reserved.
		existing, err := s.repo.GetReservationByPaymentID(ctx, tx, in.PaymentID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return domainerr.Wrap(domainerr.CodeInternal, "lookup reservation", err)
		}
		if existing != nil {
			c, err := s.repo.LockConsent(ctx, tx, existing.ConsentID)
			if err != nil {
				return domainerr.Wrap(domainerr.CodeInternal, "lock consent", err)
			}
			used, held, err := s.repo.RollingUsage(ctx, tx, c.ID)
			if err != nil {
				return domainerr.Wrap(domainerr.CodeInternal, "rolling usage", err)
			}
			// Existing held amount is already in `held`; remaining after this reservation.
			remaining := c.MaxMonthlyPence - used - held
			if remaining < 0 {
				remaining = 0
			}
			result = &ReserveResult{
				ReservationID:    existing.ID,
				RemainingMonthly: remaining,
				Currency:         c.Currency,
				ConsumerID:       c.ConsumerID,
				BankConsentRef:   c.BankConsentRef,
			}
			return nil
		}

		c, err := s.repo.LockConsent(ctx, tx, in.ConsentID)
		if errors.Is(err, pgx.ErrNoRows) {
			return domainerr.New(domainerr.CodeNotFound, "consent not found")
		}
		if err != nil {
			return domainerr.Wrap(domainerr.CodeInternal, "lock consent", err)
		}

		switch {
		case c.Status == StatusRevoked:
			return domainerr.New(domainerr.CodeConsentRevoked, "consent revoked")
		case c.Status == StatusExpired || !c.ValidUntil.After(time.Now()):
			return domainerr.New(domainerr.CodeConsentExpired, "consent expired")
		case c.Status != StatusActive:
			return domainerr.New(domainerr.CodeConsentInactive, "consent is not active")
		}

		if in.Currency != "" && c.Currency != "" && in.Currency != c.Currency {
			return domainerr.New(domainerr.CodeValidation, "currency mismatch")
		}
		if in.AmountPence > c.MaxAmountPence {
			return domainerr.New(domainerr.CodeConsentLimitExceeded, "amount exceeds per-transaction limit")
		}

		used, held, err := s.repo.RollingUsage(ctx, tx, c.ID)
		if err != nil {
			return domainerr.Wrap(domainerr.CodeInternal, "rolling usage", err)
		}
		if used+held+in.AmountPence > c.MaxMonthlyPence {
			return domainerr.New(domainerr.CodeConsentLimitExceeded, "amount exceeds monthly limit")
		}

		res, err := s.repo.InsertReservation(ctx, tx, c.ID, in.PaymentID, in.AmountPence)
		if err != nil {
			if isUniqueViolation(err) {
				// Concurrent insert for same payment_id — re-read.
				existing, rerr := s.repo.GetReservationByPaymentID(ctx, tx, in.PaymentID)
				if rerr != nil {
					return domainerr.Wrap(domainerr.CodeInternal, "re-read reservation", rerr)
				}
				remaining := c.MaxMonthlyPence - used - held
				if remaining < 0 {
					remaining = 0
				}
				result = &ReserveResult{
					ReservationID:    existing.ID,
					RemainingMonthly: remaining,
					Currency:         c.Currency,
					ConsumerID:       c.ConsumerID,
					BankConsentRef:   c.BankConsentRef,
				}
				return nil
			}
			return domainerr.Wrap(domainerr.CodeInternal, "insert reservation", err)
		}

		remaining := c.MaxMonthlyPence - used - held - in.AmountPence
		if remaining < 0 {
			remaining = 0
		}
		result = &ReserveResult{
			ReservationID:    res.ID,
			RemainingMonthly: remaining,
			Currency:         c.Currency,
			ConsumerID:       c.ConsumerID,
			BankConsentRef:   c.BankConsentRef,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) ConfirmReservation(ctx context.Context, reservationID, paymentID string) error {
	if reservationID == "" {
		return domainerr.New(domainerr.CodeValidation, "reservation_id is required")
	}

	return pgx.BeginFunc(ctx, s.repo.Pool(), func(tx pgx.Tx) error {
		res, err := s.repo.GetReservation(ctx, tx, reservationID)
		if errors.Is(err, pgx.ErrNoRows) {
			return domainerr.New(domainerr.CodeNotFound, "reservation not found")
		}
		if err != nil {
			return domainerr.Wrap(domainerr.CodeInternal, "get reservation", err)
		}
		if paymentID != "" && res.PaymentID != paymentID {
			return domainerr.New(domainerr.CodeValidation, "payment_id does not match reservation")
		}
		if res.Status == ResConfirmed {
			// Idempotent: ensure usage row exists.
			return s.repo.InsertUsage(ctx, tx, res.ConsentID, res.PaymentID, res.AmountPence)
		}
		if res.Status == ResReleased {
			return domainerr.New(domainerr.CodeConflict, "reservation already released")
		}
		if res.Status != ResHeld {
			return domainerr.New(domainerr.CodeConflict, "reservation is not held")
		}

		updated, err := s.repo.ConfirmReservation(ctx, tx, reservationID, paymentID)
		if errors.Is(err, pgx.ErrNoRows) {
			return domainerr.New(domainerr.CodeConflict, "reservation could not be confirmed")
		}
		if err != nil {
			return domainerr.Wrap(domainerr.CodeInternal, "confirm reservation", err)
		}
		if err := s.repo.InsertUsage(ctx, tx, updated.ConsentID, updated.PaymentID, updated.AmountPence); err != nil {
			return domainerr.Wrap(domainerr.CodeInternal, "insert usage", err)
		}
		return nil
	})
}

func (s *Service) ReleaseReservation(ctx context.Context, reservationID, _ string) error {
	if reservationID == "" {
		return domainerr.New(domainerr.CodeValidation, "reservation_id is required")
	}

	return pgx.BeginFunc(ctx, s.repo.Pool(), func(tx pgx.Tx) error {
		res, err := s.repo.GetReservation(ctx, tx, reservationID)
		if errors.Is(err, pgx.ErrNoRows) {
			return domainerr.New(domainerr.CodeNotFound, "reservation not found")
		}
		if err != nil {
			return domainerr.Wrap(domainerr.CodeInternal, "get reservation", err)
		}
		if res.Status == ResReleased {
			return nil // idempotent
		}
		if res.Status == ResConfirmed {
			return domainerr.New(domainerr.CodeConflict, "reservation already confirmed")
		}
		if res.Status != ResHeld {
			return domainerr.New(domainerr.CodeConflict, "reservation is not held")
		}

		_, err = s.repo.ReleaseReservation(ctx, tx, reservationID)
		if errors.Is(err, pgx.ErrNoRows) {
			return domainerr.New(domainerr.CodeConflict, "reservation could not be released")
		}
		if err != nil {
			return domainerr.Wrap(domainerr.CodeInternal, "release reservation", err)
		}
		return nil
	})
}

func (s *Service) GetRollingUsage(ctx context.Context, consentID string) (*UsageResult, error) {
	if consentID == "" {
		return nil, domainerr.New(domainerr.CodeValidation, "consent_id is required")
	}
	c, err := s.repo.GetConsent(ctx, consentID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domainerr.New(domainerr.CodeNotFound, "consent not found")
	}
	if err != nil {
		return nil, domainerr.Wrap(domainerr.CodeInternal, "get consent", err)
	}
	used, held, err := s.repo.RollingUsagePool(ctx, consentID)
	if err != nil {
		return nil, domainerr.Wrap(domainerr.CodeInternal, "rolling usage", err)
	}
	txCount, err := s.repo.TxCountThisMonth(ctx, consentID)
	if err != nil {
		return nil, domainerr.Wrap(domainerr.CodeInternal, "tx count", err)
	}
	remaining := c.MaxMonthlyPence - used - held
	if remaining < 0 {
		remaining = 0
	}
	return &UsageResult{
		UsedPence:      used + held,
		RemainingPence: remaining,
		Currency:       c.Currency,
		TxCount:        txCount,
	}, nil
}

func cacheKey(id string) string { return "consent:" + id }

func (s *Service) cacheGet(ctx context.Context, id string) *ConsentRow {
	if s.rdb == nil {
		return nil
	}
	val, err := s.rdb.Get(ctx, cacheKey(id)).Bytes()
	if err != nil {
		return nil
	}
	var c ConsentRow
	if err := json.Unmarshal(val, &c); err != nil {
		return nil
	}
	return &c
}

func (s *Service) cacheSet(ctx context.Context, c *ConsentRow) {
	if s.rdb == nil || c == nil {
		return
	}
	b, err := json.Marshal(c)
	if err != nil {
		return
	}
	if err := s.rdb.Set(ctx, cacheKey(c.ID), b, cacheTTL).Err(); err != nil {
		slog.Debug("consent cache set failed", "err", err)
	}
}

func (s *Service) cacheDel(ctx context.Context, id string) {
	if s.rdb == nil {
		return
	}
	if err := s.rdb.Del(ctx, cacheKey(id)).Err(); err != nil {
		slog.Debug("consent cache del failed", "err", err)
	}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return false
}
