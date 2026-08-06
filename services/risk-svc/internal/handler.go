package internal

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/types/known/timestamppb"

	riskv1 "github.com/hozgan/vrp-demo/gen/risk/v1"
	"github.com/hozgan/vrp-demo/pkg/shared/domainerr"
)

// Handler implements risk.v1.RiskService.
type Handler struct {
	riskv1.UnimplementedRiskServiceServer
	Engine *Engine
	RDB    *redis.Client
}

func NewHandler(engine *Engine, rdb *redis.Client) *Handler {
	return &Handler{Engine: engine, RDB: rdb}
}

func (h *Handler) Score(ctx context.Context, req *riskv1.ScoreRequest) (*riskv1.ScoreResponse, error) {
	if req == nil {
		return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "request is required"))
	}
	if strings.TrimSpace(req.GetConsumerId()) == "" {
		return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "consumer_id is required"))
	}
	if strings.TrimSpace(req.GetMerchantId()) == "" {
		return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "merchant_id is required"))
	}

	result, err := h.Engine.Score(ctx, scoreInputFromProto(req))
	if err != nil {
		return nil, domainerr.ToGRPC(domainerr.Wrap(domainerr.CodeInternal, "score failed", err))
	}

	return &riskv1.ScoreResponse{
		Score:          int32(result.Score),
		Decision:       DecisionToProto(result.Decision),
		Reason:         result.Reason,
		RulesTriggered: result.RulesTriggered,
	}, nil
}

func (h *Handler) AddToBlocklist(ctx context.Context, req *riskv1.BlocklistRequest) (*riskv1.BlocklistEntry, error) {
	if req == nil {
		return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "request is required"))
	}
	typ, err := NormalizeBlocklistType(req.GetType())
	if err != nil {
		return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, err.Error()))
	}
	value := strings.TrimSpace(req.GetValue())
	if value == "" {
		return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "value is required"))
	}
	if err := AddToBlocklist(ctx, h.RDB, typ, value); err != nil {
		return nil, domainerr.ToGRPC(domainerr.Wrap(domainerr.CodeInternal, "add to blocklist", err))
	}
	now := time.Now().UTC()
	return &riskv1.BlocklistEntry{
		Id:        uuid.NewString(),
		Type:      strings.ToUpper(typ),
		Value:     value,
		Reason:    req.GetReason(),
		CreatedAt: timestamppb.New(now),
	}, nil
}

func (h *Handler) RemoveFromBlocklist(ctx context.Context, req *riskv1.BlocklistRequest) (*riskv1.BlocklistEntry, error) {
	if req == nil {
		return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "request is required"))
	}
	typ, err := NormalizeBlocklistType(req.GetType())
	if err != nil {
		return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, err.Error()))
	}
	value := strings.TrimSpace(req.GetValue())
	if value == "" {
		return nil, domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "value is required"))
	}
	if err := RemoveFromBlocklist(ctx, h.RDB, typ, value); err != nil {
		return nil, domainerr.ToGRPC(domainerr.Wrap(domainerr.CodeInternal, "remove from blocklist", err))
	}
	return &riskv1.BlocklistEntry{
		Id:        uuid.NewString(),
		Type:      strings.ToUpper(typ),
		Value:     value,
		Reason:    req.GetReason(),
		CreatedAt: timestamppb.Now(),
	}, nil
}
