package internal

import (
	"context"
	"strconv"
	"strings"
	"time"

	commonv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/common/v1"
	consentv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/consent/v1"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/domainerr"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Handler struct {
	consentv1.UnimplementedConsentServiceServer
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) CreateConsent(ctx context.Context, req *consentv1.CreateConsentRequest) (*consentv1.Consent, error) {
	maxTx, cur, err := moneyFromProto(req.GetMaxPerTransaction())
	if err != nil {
		return nil, domainerr.ToGRPC(err)
	}
	maxMo, cur2, err := moneyFromProto(req.GetMaxPerMonth())
	if err != nil {
		return nil, domainerr.ToGRPC(err)
	}
	if cur == "" {
		cur = cur2
	}
	var until time.Time
	if ts := req.GetValidUntil(); ts != nil {
		until = ts.AsTime()
	}
	row, err := h.svc.CreateConsent(ctx, CreateInput{
		MerchantID:      req.GetMerchantId(),
		ConsumerID:      req.GetConsumerId(),
		BankConsentRef:  req.GetBankConsentRef(),
		MaxAmountPence:  maxTx,
		MaxMonthlyPence: maxMo,
		Currency:        cur,
		ValidUntil:      until,
	})
	if err != nil {
		return nil, domainerr.ToGRPC(err)
	}
	return toProtoConsent(row), nil
}

func (h *Handler) GetConsent(ctx context.Context, req *consentv1.GetConsentRequest) (*consentv1.Consent, error) {
	row, err := h.svc.GetConsent(ctx, req.GetConsentId(), req.GetMerchantId())
	if err != nil {
		return nil, domainerr.ToGRPC(err)
	}
	return toProtoConsent(row), nil
}

func (h *Handler) RevokeConsent(ctx context.Context, req *consentv1.RevokeConsentRequest) (*consentv1.Consent, error) {
	row, err := h.svc.RevokeConsent(ctx, req.GetConsentId(), req.GetMerchantId(), req.GetReason())
	if err != nil {
		return nil, domainerr.ToGRPC(err)
	}
	return toProtoConsent(row), nil
}

func (h *Handler) ListConsents(ctx context.Context, req *consentv1.ListConsentsRequest) (*consentv1.ListConsentsResponse, error) {
	limit := int32(20)
	offset := 0
	if p := req.GetPage(); p != nil {
		if p.GetPageSize() > 0 {
			limit = p.GetPageSize()
		}
		if tok := strings.TrimSpace(p.GetPageToken()); tok != "" {
			n, err := strconv.Atoi(tok)
			if err != nil || n < 0 {
				return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "page_token must be an integer offset"))
			}
			offset = n
		}
	}

	status := ""
	switch req.GetStatus() {
	case consentv1.ConsentStatus_PENDING:
		status = StatusPending
	case consentv1.ConsentStatus_ACTIVE:
		status = StatusActive
	case consentv1.ConsentStatus_REVOKED:
		status = StatusRevoked
	case consentv1.ConsentStatus_EXPIRED:
		status = StatusExpired
	}

	rows, total, err := h.svc.ListConsents(ctx, req.GetConsumerId(), req.GetMerchantId(), status, int(limit), offset)
	if err != nil {
		return nil, domainerr.ToGRPC(err)
	}

	out := make([]*consentv1.Consent, 0, len(rows))
	for i := range rows {
		out = append(out, toProtoConsent(&rows[i]))
	}

	next := ""
	if offset+len(rows) < total {
		next = strconv.Itoa(offset + len(rows))
	}
	return &consentv1.ListConsentsResponse{
		Consents: out,
		Page: &commonv1.PageResponse{
			NextPageToken: next,
			TotalCount:    int32(total),
		},
	}, nil
}

func (h *Handler) ValidateAndReserve(ctx context.Context, req *consentv1.ReserveRequest) (*consentv1.ReserveResponse, error) {
	amt, cur, err := moneyFromProto(req.GetAmount())
	if err != nil {
		return nil, domainerr.ToGRPC(err)
	}
	res, err := h.svc.ValidateAndReserve(ctx, ReserveInput{
		ConsentID:   req.GetConsentId(),
		PaymentID:   req.GetPaymentId(),
		AmountPence: amt,
		Currency:    cur,
	})
	if err != nil {
		return nil, domainerr.ToGRPC(err)
	}
	return &consentv1.ReserveResponse{
		ReservationId: res.ReservationID,
		RemainingMonthly: &commonv1.Money{
			AmountPence: res.RemainingMonthly,
			Currency:    res.Currency,
		},
		ConsumerId:     res.ConsumerID,
		BankConsentRef: res.BankConsentRef,
	}, nil
}

func (h *Handler) ConfirmReservation(ctx context.Context, req *consentv1.ConfirmRequest) (*emptypb.Empty, error) {
	if err := h.svc.ConfirmReservation(ctx, req.GetReservationId(), req.GetPaymentId()); err != nil {
		return nil, domainerr.ToGRPC(err)
	}
	return &emptypb.Empty{}, nil
}

func (h *Handler) ReleaseReservation(ctx context.Context, req *consentv1.ReleaseRequest) (*emptypb.Empty, error) {
	if err := h.svc.ReleaseReservation(ctx, req.GetReservationId(), req.GetReason()); err != nil {
		return nil, domainerr.ToGRPC(err)
	}
	return &emptypb.Empty{}, nil
}

func (h *Handler) GetRollingUsage(ctx context.Context, req *consentv1.UsageRequest) (*consentv1.UsageResponse, error) {
	res, err := h.svc.GetRollingUsage(ctx, req.GetConsentId())
	if err != nil {
		return nil, domainerr.ToGRPC(err)
	}
	return &consentv1.UsageResponse{
		UsedThisMonth: &commonv1.Money{
			AmountPence: res.UsedPence,
			Currency:    res.Currency,
		},
		RemainingThisMonth: &commonv1.Money{
			AmountPence: res.RemainingPence,
			Currency:    res.Currency,
		},
		TxCountThisMonth: res.TxCount,
	}, nil
}

func moneyFromProto(m *commonv1.Money) (int64, string, error) {
	if m == nil {
		return 0, "", domainerr.New(domainerr.CodeValidation, "money is required")
	}
	cur := strings.ToUpper(strings.TrimSpace(m.GetCurrency()))
	if cur == "" {
		cur = defaultCurrency
	}
	if len(cur) != 3 {
		return 0, "", domainerr.New(domainerr.CodeValidation, "currency must be 3-letter ISO code")
	}
	if m.GetAmountPence() <= 0 {
		return 0, "", domainerr.New(domainerr.CodeValidation, "amount must be positive")
	}
	return m.GetAmountPence(), cur, nil
}

func toProtoConsent(c *ConsentRow) *consentv1.Consent {
	if c == nil {
		return nil
	}
	status := mapStatus(c.Status)
	// Soft-expire for API visibility if still ACTIVE past valid_until.
	if c.Status == StatusActive && !c.ValidUntil.After(time.Now()) {
		status = consentv1.ConsentStatus_EXPIRED
	}
	return &consentv1.Consent{
		Id:             c.ID,
		MerchantId:     c.MerchantID,
		ConsumerId:     c.ConsumerID,
		BankConsentRef: c.BankConsentRef,
		Status:         status,
		MaxPerTransaction: &commonv1.Money{
			AmountPence: c.MaxAmountPence,
			Currency:    c.Currency,
		},
		MaxPerMonth: &commonv1.Money{
			AmountPence: c.MaxMonthlyPence,
			Currency:    c.Currency,
		},
		ValidFrom:  timestamppb.New(c.ValidFrom),
		ValidUntil: timestamppb.New(c.ValidUntil),
		CreatedAt:  timestamppb.New(c.CreatedAt),
		UpdatedAt:  timestamppb.New(c.UpdatedAt),
	}
}

func mapStatus(s string) consentv1.ConsentStatus {
	switch s {
	case StatusPending:
		return consentv1.ConsentStatus_PENDING
	case StatusActive:
		return consentv1.ConsentStatus_ACTIVE
	case StatusRevoked:
		return consentv1.ConsentStatus_REVOKED
	case StatusExpired:
		return consentv1.ConsentStatus_EXPIRED
	default:
		return consentv1.ConsentStatus_CONSENT_STATUS_UNSPECIFIED
	}
}
