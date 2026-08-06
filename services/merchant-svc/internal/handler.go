package internal

import (
	"context"

	merchantv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/merchant/v1"
	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/domainerr"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Handler struct {
	merchantv1.UnimplementedMerchantServiceServer
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) RegisterMerchant(ctx context.Context, req *merchantv1.RegisterMerchantRequest) (*merchantv1.RegisterMerchantResponse, error) {
	res, err := h.svc.RegisterMerchant(ctx, RegisterInput{
		Name:         req.GetName(),
		WebhookURL:   req.GetWebhookUrl(),
		ContactEmail: req.GetContactEmail(),
	})
	if err != nil {
		return nil, domainerr.ToGRPC(err)
	}
	return &merchantv1.RegisterMerchantResponse{
		Merchant: toProtoMerchant(res.Merchant),
		ApiKey:   res.APIKey,
	}, nil
}

func (h *Handler) GetMerchant(ctx context.Context, req *merchantv1.GetMerchantRequest) (*merchantv1.Merchant, error) {
	m, err := h.svc.GetMerchant(ctx, req.GetMerchantId())
	if err != nil {
		return nil, domainerr.ToGRPC(err)
	}
	return toProtoMerchant(m), nil
}

func (h *Handler) SuspendMerchant(ctx context.Context, req *merchantv1.SuspendMerchantRequest) (*merchantv1.Merchant, error) {
	m, err := h.svc.SuspendMerchant(ctx, req.GetMerchantId(), req.GetReason())
	if err != nil {
		return nil, domainerr.ToGRPC(err)
	}
	return toProtoMerchant(m), nil
}

func (h *Handler) GetMerchantByApiKey(ctx context.Context, req *merchantv1.GetMerchantByApiKeyRequest) (*merchantv1.Merchant, error) {
	m, err := h.svc.GetMerchantByAPIKey(ctx, req.GetApiKey())
	if err != nil {
		return nil, domainerr.ToGRPC(err)
	}
	return toProtoMerchant(m), nil
}

func (h *Handler) GetWebhookConfig(ctx context.Context, req *merchantv1.GetWebhookConfigRequest) (*merchantv1.WebhookConfig, error) {
	cfg, err := h.svc.GetWebhookConfig(ctx, req.GetMerchantId())
	if err != nil {
		return nil, domainerr.ToGRPC(err)
	}
	return &merchantv1.WebhookConfig{
		MerchantId: cfg.MerchantID,
		WebhookUrl: cfg.WebhookURL,
		HmacSecret: cfg.HMACSecret,
	}, nil
}

func toProtoMerchant(m *Merchant) *merchantv1.Merchant {
	if m == nil {
		return nil
	}
	return &merchantv1.Merchant{
		Id:         m.ID,
		Name:       m.Name,
		KybStatus:  m.KYBStatus,
		Status:     m.Status,
		WebhookUrl: m.WebhookURL,
		CreatedAt:  timestamppb.New(m.CreatedAt),
		UpdatedAt:  timestamppb.New(m.UpdatedAt),
	}
}
