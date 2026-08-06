package internal

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	commonv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/common/v1"
)

func TestHighValueRule(t *testing.T) {
	t.Parallel()
	rule := &HighValueRule{}
	ctx := context.Background()

	tests := []struct {
		name       string
		pence      int64
		wantDelta  int
		wantDec    Decision
		wantName   string
		wantReason bool
	}{
		{name: "below threshold", pence: 50_000, wantDelta: 0, wantDec: DecisionAllow, wantName: ""},
		{name: "exactly threshold", pence: 50_000, wantDelta: 0, wantDec: DecisionAllow, wantName: ""},
		{name: "just over", pence: 50_001, wantDelta: 30, wantDec: DecisionReview, wantName: "HIGH_VALUE", wantReason: true},
		{name: "large amount", pence: 1_000_000, wantDelta: 30, wantDec: DecisionReview, wantName: "HIGH_VALUE", wantReason: true},
		{name: "zero", pence: 0, wantDelta: 0, wantDec: DecisionAllow, wantName: ""},
		{name: "nil amount", pence: -1, wantDelta: 0, wantDec: DecisionAllow, wantName: ""},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var req ScoreInput
			if tt.pence >= 0 {
				req.Amount = &commonv1.Money{AmountPence: tt.pence, Currency: "GBP"}
			}
			delta, dec, name, reason, err := rule.Evaluate(ctx, req)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if delta != tt.wantDelta {
				t.Errorf("scoreDelta=%d want %d", delta, tt.wantDelta)
			}
			if dec != tt.wantDec {
				t.Errorf("decision=%s want %s", dec, tt.wantDec)
			}
			if name != tt.wantName {
				t.Errorf("ruleName=%q want %q", name, tt.wantName)
			}
			if tt.wantReason && reason == "" {
				t.Error("expected non-empty reason")
			}
			if !tt.wantReason && reason != "" {
				t.Errorf("unexpected reason %q", reason)
			}
		})
	}
}

func TestMergeResults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		results    []RuleResult
		wantScore  int
		wantDec    Decision
		wantRules  []string
		wantReason string
	}{
		{
			name:      "empty defaults allow",
			results:   nil,
			wantScore: 0,
			wantDec:   DecisionAllow,
		},
		{
			name: "sum scores cap 100",
			results: []RuleResult{
				{ScoreDelta: 40, Decision: DecisionReview, RuleName: "VELOCITY_EXCEEDED", Reason: "vel"},
				{ScoreDelta: 30, Decision: DecisionReview, RuleName: "HIGH_VALUE", Reason: "hv"},
				{ScoreDelta: 50, Decision: DecisionReview, RuleName: "EXTRA", Reason: "x"},
			},
			wantScore: 100,
			wantDec:   DecisionReview,
			wantRules: []string{"VELOCITY_EXCEEDED", "HIGH_VALUE", "EXTRA"},
		},
		{
			name: "decline beats review",
			results: []RuleResult{
				{ScoreDelta: 30, Decision: DecisionReview, RuleName: "HIGH_VALUE", Reason: "high"},
				{ScoreDelta: 100, Decision: DecisionDecline, RuleName: "BLOCKLIST", Reason: "blocked"},
			},
			wantScore: 100,
			wantDec:   DecisionDecline,
			wantRules: []string{"HIGH_VALUE", "BLOCKLIST"},
		},
		{
			name: "review beats allow",
			results: []RuleResult{
				{ScoreDelta: 0, Decision: DecisionAllow},
				{ScoreDelta: 40, Decision: DecisionReview, RuleName: "VELOCITY_EXCEEDED", Reason: "fast"},
			},
			wantScore: 40,
			wantDec:   DecisionReview,
			wantRules: []string{"VELOCITY_EXCEEDED"},
		},
		{
			name: "allow only",
			results: []RuleResult{
				{ScoreDelta: 0, Decision: DecisionAllow},
			},
			wantScore: 0,
			wantDec:   DecisionAllow,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := mergeResults(tt.results)
			if got.Score != tt.wantScore {
				t.Errorf("score=%d want %d", got.Score, tt.wantScore)
			}
			if got.Decision != tt.wantDec {
				t.Errorf("decision=%s want %s", got.Decision, tt.wantDec)
			}
			if len(got.RulesTriggered) != len(tt.wantRules) {
				t.Fatalf("rules=%v want %v", got.RulesTriggered, tt.wantRules)
			}
			for i := range tt.wantRules {
				if got.RulesTriggered[i] != tt.wantRules[i] {
					t.Errorf("rules[%d]=%s want %s", i, got.RulesTriggered[i], tt.wantRules[i])
				}
			}
		})
	}
}

func TestBlocklistAndVelocityWithMiniredis(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	ctx := context.Background()
	engine := NewEngine(rdb)

	t.Run("allow clean request", func(t *testing.T) {
		got, err := engine.Score(ctx, ScoreInput{
			MerchantID: "m1",
			ConsumerID: "c-clean",
			Amount:     &commonv1.Money{AmountPence: 1000, Currency: "GBP"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.Decision != DecisionAllow || got.Score != 0 {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("blocklisted consumer declines without velocity incr", func(t *testing.T) {
		if err := AddToBlocklist(ctx, rdb, "CONSUMER", "bad-actor"); err != nil {
			t.Fatal(err)
		}
		// seed velocity so we can assert it was not incremented
		vkey := velocityKey("bad-actor")
		if err := rdb.Set(ctx, vkey, 3, 0).Err(); err != nil {
			t.Fatal(err)
		}

		got, err := engine.Score(ctx, ScoreInput{
			MerchantID: "m1",
			ConsumerID: "bad-actor",
			Amount:     &commonv1.Money{AmountPence: 100, Currency: "GBP"},
			IPAddress:  "1.2.3.4",
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.Decision != DecisionDecline {
			t.Fatalf("decision=%s want DECLINE", got.Decision)
		}
		if got.Score != 100 {
			t.Fatalf("score=%d want 100", got.Score)
		}
		if len(got.RulesTriggered) != 1 || got.RulesTriggered[0] != "BLOCKLIST" {
			t.Fatalf("rules=%v", got.RulesTriggered)
		}
		n, err := rdb.Get(ctx, vkey).Int()
		if err != nil {
			t.Fatal(err)
		}
		if n != 3 {
			t.Fatalf("velocity incremented on decline: got %d", n)
		}
	})

	t.Run("high value review still increments velocity", func(t *testing.T) {
		consumer := "c-hv"
		got, err := engine.Score(ctx, ScoreInput{
			MerchantID: "m1",
			ConsumerID: consumer,
			Amount:     &commonv1.Money{AmountPence: 60_000, Currency: "GBP"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.Decision != DecisionReview {
			t.Fatalf("decision=%s want REVIEW", got.Decision)
		}
		if got.Score != 30 {
			t.Fatalf("score=%d want 30", got.Score)
		}
		n, err := rdb.Get(ctx, velocityKey(consumer)).Int()
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("velocity=%d want 1", n)
		}
	})

	t.Run("velocity exceeded after 6th payment", func(t *testing.T) {
		consumer := "c-vel"
		var last ScoreResult
		for range 6 {
			var err error
			last, err = engine.Score(ctx, ScoreInput{
				MerchantID: "m1",
				ConsumerID: consumer,
				Amount:     &commonv1.Money{AmountPence: 500, Currency: "GBP"},
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		if last.Decision != DecisionReview {
			t.Fatalf("decision=%s want REVIEW", last.Decision)
		}
		if last.Score != 40 {
			t.Fatalf("score=%d want 40", last.Score)
		}
		found := false
		for _, r := range last.RulesTriggered {
			if r == "VELOCITY_EXCEEDED" {
				found = true
			}
		}
		if !found {
			t.Fatalf("rules=%v missing VELOCITY_EXCEEDED", last.RulesTriggered)
		}
	})

	t.Run("velocity plus high value sums scores", func(t *testing.T) {
		consumer := "c-both"
		// push velocity over threshold
		for range 5 {
			if _, err := engine.Score(ctx, ScoreInput{
				MerchantID: "m1",
				ConsumerID: consumer,
				Amount:     &commonv1.Money{AmountPence: 100, Currency: "GBP"},
			}); err != nil {
				t.Fatal(err)
			}
		}
		got, err := engine.Score(ctx, ScoreInput{
			MerchantID: "m1",
			ConsumerID: consumer,
			Amount:     &commonv1.Money{AmountPence: 75_000, Currency: "GBP"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.Decision != DecisionReview {
			t.Fatalf("decision=%s", got.Decision)
		}
		if got.Score != 70 { // 30 + 40
			t.Fatalf("score=%d want 70", got.Score)
		}
	})

	t.Run("remove from blocklist allows again", func(t *testing.T) {
		id := "temp-blocked"
		if err := AddToBlocklist(ctx, rdb, "ip", "9.9.9.9"); err != nil {
			t.Fatal(err)
		}
		got, err := engine.Score(ctx, ScoreInput{
			MerchantID: "m1",
			ConsumerID: id,
			IPAddress:  "9.9.9.9",
			Amount:     &commonv1.Money{AmountPence: 100, Currency: "GBP"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.Decision != DecisionDecline {
			t.Fatalf("expected decline, got %s", got.Decision)
		}
		if err := RemoveFromBlocklist(ctx, rdb, "IP", "9.9.9.9"); err != nil {
			t.Fatal(err)
		}
		got, err = engine.Score(ctx, ScoreInput{
			MerchantID: "m1",
			ConsumerID: id + "-2",
			IPAddress:  "9.9.9.9",
			Amount:     &commonv1.Money{AmountPence: 100, Currency: "GBP"},
		})
		if err != nil {
			t.Fatal(err)
		}
		if got.Decision != DecisionAllow {
			t.Fatalf("expected allow after remove, got %s", got.Decision)
		}
	})
}

func TestNormalizeBlocklistType(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"CONSUMER", "consumer", false},
		{"Merchant", "merchant", false},
		{"ip", "ip", false},
		{"", "", true},
		{"DEVICE", "", true},
	}
	for _, tt := range tests {
		got, err := NormalizeBlocklistType(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("%q: expected error", tt.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("%q => %q want %q", tt.in, got, tt.want)
		}
	}
}

func TestDecisionToProto(t *testing.T) {
	t.Parallel()
	if DecisionToProto(DecisionAllow).String() != "ALLOW" {
		t.Fatal(DecisionToProto(DecisionAllow))
	}
	if DecisionToProto(DecisionReview).String() != "REVIEW" {
		t.Fatal(DecisionToProto(DecisionReview))
	}
	if DecisionToProto(DecisionDecline).String() != "DECLINE" {
		t.Fatal(DecisionToProto(DecisionDecline))
	}
}
