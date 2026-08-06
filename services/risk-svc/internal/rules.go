package internal

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	commonv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/common/v1"
	riskv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/risk/v1"
)

// Decision is the outcome of a single rule or the merged engine result.
type Decision string

const (
	DecisionAllow   Decision = "ALLOW"
	DecisionReview  Decision = "REVIEW"
	DecisionDecline Decision = "DECLINE"
)

const (
	maxScore            = 100
	velocityWindow      = 60 * time.Second
	velocityThreshold   = 5
	velocityScoreDelta  = 40
	highValuePence      = 50_000 // £500
	highValueScoreDelta = 30
	blocklistScoreDelta = 100
)

// ScoreInput is the rule-engine view of a scoring request.
type ScoreInput struct {
	MerchantID string
	ConsumerID string
	ConsentID  string
	PaymentID  string
	Amount     *commonv1.Money
	IPAddress  string
	UserAgent  string
}

func scoreInputFromProto(req *riskv1.ScoreRequest) ScoreInput {
	if req == nil {
		return ScoreInput{}
	}
	return ScoreInput{
		MerchantID: req.GetMerchantId(),
		ConsumerID: req.GetConsumerId(),
		ConsentID:  req.GetConsentId(),
		PaymentID:  req.GetPaymentId(),
		Amount:     req.GetAmount(),
		IPAddress:  req.GetIpAddress(),
		UserAgent:  req.GetUserAgent(),
	}
}

// RuleResult is the outcome of evaluating one rule.
type RuleResult struct {
	ScoreDelta int
	Decision   Decision
	RuleName   string
	Reason     string
}

// Rule evaluates a single risk signal.
// Evaluate returns scoreDelta, decision (ALLOW|REVIEW|DECLINE), ruleName, reason.
type Rule interface {
	Evaluate(ctx context.Context, req ScoreInput) (scoreDelta int, decision Decision, ruleName, reason string, err error)
}

// BlocklistRule declines when consumer, merchant, or IP is blocklisted.
// Redis: SISMEMBER risk:blocklist:{type}:{value} {value}  (type lowercased).
type BlocklistRule struct {
	RDB *redis.Client
}

func (r *BlocklistRule) Evaluate(ctx context.Context, req ScoreInput) (int, Decision, string, string, error) {
	checks := []struct {
		typ, value string
	}{
		{"consumer", req.ConsumerID},
		{"merchant", req.MerchantID},
		{"ip", req.IPAddress},
	}
	for _, c := range checks {
		if c.value == "" {
			continue
		}
		key := blocklistKey(c.typ, c.value)
		ok, err := r.RDB.SIsMember(ctx, key, c.value).Result()
		if err != nil {
			return 0, DecisionAllow, "", "", fmt.Errorf("blocklist sismember %s: %w", key, err)
		}
		if ok {
			return blocklistScoreDelta, DecisionDecline, "BLOCKLIST",
				fmt.Sprintf("%s %q is blocklisted", c.typ, c.value), nil
		}
	}
	return 0, DecisionAllow, "", "", nil
}

// VelocityRule increments a per-consumer counter (TTL 60s). Count > 5 → REVIEW +40.
// Callers should only invoke this when the overall decision is not already DECLINE.
type VelocityRule struct {
	RDB *redis.Client
}

func (r *VelocityRule) Evaluate(ctx context.Context, req ScoreInput) (int, Decision, string, string, error) {
	if req.ConsumerID == "" {
		return 0, DecisionAllow, "", "", nil
	}
	key := velocityKey(req.ConsumerID)
	n, err := r.RDB.Incr(ctx, key).Result()
	if err != nil {
		return 0, DecisionAllow, "", "", fmt.Errorf("velocity incr: %w", err)
	}
	if n == 1 {
		if err := r.RDB.Expire(ctx, key, velocityWindow).Err(); err != nil {
			return 0, DecisionAllow, "", "", fmt.Errorf("velocity expire: %w", err)
		}
	}
	if n > velocityThreshold {
		return velocityScoreDelta, DecisionReview, "VELOCITY_EXCEEDED",
			fmt.Sprintf("consumer exceeded %d payments in %s (count=%d)", velocityThreshold, velocityWindow, n), nil
	}
	return 0, DecisionAllow, "", "", nil
}

// HighValueRule flags amounts above £500 for review (+30).
type HighValueRule struct{}

func (r *HighValueRule) Evaluate(_ context.Context, req ScoreInput) (int, Decision, string, string, error) {
	var pence int64
	if req.Amount != nil {
		pence = req.Amount.GetAmountPence()
	}
	if pence > highValuePence {
		return highValueScoreDelta, DecisionReview, "HIGH_VALUE",
			fmt.Sprintf("amount %d pence exceeds %d pence", pence, highValuePence), nil
	}
	return 0, DecisionAllow, "", "", nil
}

// Engine merges rule outcomes: max severity (DECLINE > REVIEW > ALLOW), sum scores capped at 100.
// Velocity is incremented only when the pre-velocity decision is not DECLINE.
type Engine struct {
	Blocklist *BlocklistRule
	HighValue *HighValueRule
	Velocity  *VelocityRule
	// Rules are extra pre-velocity rules (optional).
	Rules []Rule
}

// NewEngine wires the default rule set against rdb.
func NewEngine(rdb *redis.Client) *Engine {
	return &Engine{
		Blocklist: &BlocklistRule{RDB: rdb},
		HighValue: &HighValueRule{},
		Velocity:  &VelocityRule{RDB: rdb},
	}
}

// ScoreResult is the merged engine output.
type ScoreResult struct {
	Score          int
	Decision       Decision
	Reason         string
	RulesTriggered []string
}

// Score runs rules, merges decisions/scores, and increments velocity when not declined.
func (e *Engine) Score(ctx context.Context, req ScoreInput) (ScoreResult, error) {
	var results []RuleResult

	pre := []Rule{e.Blocklist, e.HighValue}
	pre = append(pre, e.Rules...)
	for _, rule := range pre {
		if rule == nil {
			continue
		}
		delta, dec, name, reason, err := rule.Evaluate(ctx, req)
		if err != nil {
			return ScoreResult{}, err
		}
		if name != "" || dec != DecisionAllow || delta != 0 {
			results = append(results, RuleResult{
				ScoreDelta: delta,
				Decision:   dec,
				RuleName:   name,
				Reason:     reason,
			})
		}
	}

	merged := mergeResults(results)
	// After successful score that is not DECLINE, still increment velocity.
	if merged.Decision != DecisionDecline && e.Velocity != nil {
		delta, dec, name, reason, err := e.Velocity.Evaluate(ctx, req)
		if err != nil {
			return ScoreResult{}, err
		}
		if name != "" || dec != DecisionAllow || delta != 0 {
			results = append(results, RuleResult{
				ScoreDelta: delta,
				Decision:   dec,
				RuleName:   name,
				Reason:     reason,
			})
			merged = mergeResults(results)
		}
	}

	if merged.Decision == "" {
		merged.Decision = DecisionAllow
	}
	return merged, nil
}

// mergeResults picks max severity and sums score deltas (capped at 100).
func mergeResults(results []RuleResult) ScoreResult {
	out := ScoreResult{
		Score:    0,
		Decision: DecisionAllow,
		Reason:   "all rules passed",
	}
	var reasons []string
	for _, r := range results {
		if r.Decision == DecisionAllow && r.ScoreDelta == 0 && r.RuleName == "" {
			continue
		}
		out.Score += r.ScoreDelta
		if r.RuleName != "" {
			out.RulesTriggered = append(out.RulesTriggered, r.RuleName)
		}
		if r.Reason != "" {
			reasons = append(reasons, r.Reason)
		}
		if severity(r.Decision) > severity(out.Decision) {
			out.Decision = r.Decision
		}
	}
	if out.Score > maxScore {
		out.Score = maxScore
	}
	if out.Score < 0 {
		out.Score = 0
	}
	if len(reasons) > 0 {
		out.Reason = strings.Join(reasons, "; ")
	}
	return out
}

func severity(d Decision) int {
	switch d {
	case DecisionDecline:
		return 3
	case DecisionReview:
		return 2
	case DecisionAllow:
		return 1
	default:
		return 0
	}
}

// blocklistKey returns risk:blocklist:{type}:{value} with type lowercased.
func blocklistKey(typ, value string) string {
	return "risk:blocklist:" + strings.ToLower(typ) + ":" + value
}

func velocityKey(consumerID string) string {
	return "risk:velocity:" + consumerID
}

// NormalizeBlocklistType validates and lowercases CONSUMER|MERCHANT|IP.
func NormalizeBlocklistType(typ string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(typ)) {
	case "CONSUMER", "MERCHANT", "IP":
		return strings.ToLower(strings.TrimSpace(typ)), nil
	default:
		return "", fmt.Errorf("type must be CONSUMER, MERCHANT, or IP")
	}
}

// AddToBlocklist SADD risk:blocklist:{type}:{value} {value}.
func AddToBlocklist(ctx context.Context, rdb *redis.Client, typ, value string) error {
	t, err := NormalizeBlocklistType(typ)
	if err != nil {
		return err
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("value is required")
	}
	return rdb.SAdd(ctx, blocklistKey(t, value), value).Err()
}

// RemoveFromBlocklist SREM risk:blocklist:{type}:{value} {value}.
func RemoveFromBlocklist(ctx context.Context, rdb *redis.Client, typ, value string) error {
	t, err := NormalizeBlocklistType(typ)
	if err != nil {
		return err
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("value is required")
	}
	return rdb.SRem(ctx, blocklistKey(t, value), value).Err()
}

// DecisionToProto maps engine decision to protobuf enum.
func DecisionToProto(d Decision) riskv1.RiskDecision {
	switch d {
	case DecisionReview:
		return riskv1.RiskDecision_REVIEW
	case DecisionDecline:
		return riskv1.RiskDecision_DECLINE
	default:
		return riskv1.RiskDecision_ALLOW
	}
}
