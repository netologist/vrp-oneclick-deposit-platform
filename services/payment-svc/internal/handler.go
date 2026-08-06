package internal

import (
	"context"
	"strings"

	"github.com/google/uuid"
	commonv1 "github.com/hozgan/vrp-demo/gen/common/v1"
	paymentv1 "github.com/hozgan/vrp-demo/gen/payment/v1"
	"github.com/hozgan/vrp-demo/pkg/shared/domainerr"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Handler struct {
	paymentv1.UnimplementedPaymentServiceServer
	orch *Orchestrator
	repo *Repo
}

func NewHandler(orch *Orchestrator, repo *Repo) *Handler {
	return &Handler{orch: orch, repo: repo}
}

func (h *Handler) InitiatePayment(ctx context.Context, req *paymentv1.InitiatePaymentRequest) (*paymentv1.Payment, error) {
	if req == nil {
		return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "request required"))
	}
	amount := req.GetAmount()
	if amount == nil {
		return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "amount is required"))
	}

	p, err := h.orch.Initiate(ctx, InitiateInput{
		IdempotencyKey: req.GetIdempotencyKey(),
		MerchantID:     req.GetMerchantId(),
		ConsentID:      req.GetConsentId(),
		AmountPence:    amount.GetAmountPence(),
		Currency:       amount.GetCurrency(),
		Description:    req.GetDescription(),
	})
	if p != nil {
		// Return payment even on terminal saga failure so clients see FAILED status.
		if err != nil && isTerminalPayment(p) {
			return toProto(p), nil
		}
		if err != nil {
			return nil, domainerr.ToGRPC(err)
		}
		return toProto(p), nil
	}
	return nil, domainerr.ToGRPC(err)
}

func (h *Handler) GetPayment(ctx context.Context, req *paymentv1.GetPaymentRequest) (*paymentv1.Payment, error) {
	if req == nil || strings.TrimSpace(req.GetPaymentId()) == "" {
		return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "payment_id is required"))
	}
	id, err := uuid.Parse(req.GetPaymentId())
	if err != nil {
		return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "invalid payment_id"))
	}

	var p *Payment
	if mid := strings.TrimSpace(req.GetMerchantId()); mid != "" {
		merchantID, perr := uuid.Parse(mid)
		if perr != nil {
			return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "invalid merchant_id"))
		}
		p, err = h.repo.GetByIDAndMerchant(ctx, id, merchantID)
	} else {
		p, err = h.repo.GetByID(ctx, id)
	}
	if err != nil {
		return nil, domainerr.ToGRPC(err)
	}
	return toProto(p), nil
}

func (h *Handler) RetryPayment(ctx context.Context, req *paymentv1.RetryPaymentRequest) (*paymentv1.Payment, error) {
	if req == nil || strings.TrimSpace(req.GetPaymentId()) == "" {
		return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "payment_id is required"))
	}
	id, err := uuid.Parse(req.GetPaymentId())
	if err != nil {
		return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "invalid payment_id"))
	}

	var p *Payment
	if mid := strings.TrimSpace(req.GetMerchantId()); mid != "" {
		merchantID, perr := uuid.Parse(mid)
		if perr != nil {
			return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "invalid merchant_id"))
		}
		p, err = h.repo.GetByIDAndMerchant(ctx, id, merchantID)
		if err != nil {
			return nil, domainerr.ToGRPC(err)
		}
	} else {
		p, err = h.repo.GetByID(ctx, id)
		if err != nil {
			return nil, domainerr.ToGRPC(err)
		}
	}

	if p.Status != StatusFailed {
		return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeConflict, "only FAILED payments can be retried"))
	}

	out, err := h.orch.Initiate(ctx, InitiateInput{
		SkipIdempotency: true,
		Existing:        p,
		IdempotencyKey:  p.IdempotencyKey,
		MerchantID:      p.MerchantID.String(),
		ConsentID:       p.ConsentID.String(),
		AmountPence:     p.AmountPence,
		Currency:        p.Currency,
		Description:     p.Description,
	})
	if out != nil {
		if err != nil && isTerminalPayment(out) {
			return toProto(out), nil
		}
		if err != nil {
			return nil, domainerr.ToGRPC(err)
		}
		return toProto(out), nil
	}
	return nil, domainerr.ToGRPC(err)
}

func isTerminalPayment(p *Payment) bool {
	switch p.Status {
	case StatusFailed, StatusSettled, StatusManualReview:
		return true
	default:
		return false
	}
}

func toProto(p *Payment) *paymentv1.Payment {
	if p == nil {
		return nil
	}
	out := &paymentv1.Payment{
		Id:             p.ID.String(),
		IdempotencyKey: p.IdempotencyKey,
		MerchantId:     p.MerchantID.String(),
		ConsentId:      p.ConsentID.String(),
		ConsumerId:     p.ConsumerID,
		Amount: &commonv1.Money{
			AmountPence: p.AmountPence,
			Currency:    p.Currency,
		},
		Status:         statusToProto(p.Status),
		BankPaymentRef: p.BankPaymentRef,
		RiskDecision:   p.RiskDecision,
		FailureReason:  p.FailureReason,
		Description:    p.Description,
		InitiatedAt:    timestamppb.New(p.InitiatedAt),
		UpdatedAt:      timestamppb.New(p.UpdatedAt),
	}
	if p.RiskScore != nil {
		out.RiskScore = *p.RiskScore
	}
	if p.SettledAt != nil {
		out.SettledAt = timestamppb.New(*p.SettledAt)
	}
	return out
}

func statusToProto(s string) paymentv1.PaymentStatus {
	if v, ok := paymentv1.PaymentStatus_value[s]; ok {
		return paymentv1.PaymentStatus(v)
	}
	return paymentv1.PaymentStatus_PAYMENT_STATUS_UNSPECIFIED
}
