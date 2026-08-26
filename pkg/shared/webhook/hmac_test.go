package webhook_test

import (
	"testing"
	"time"

	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/webhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignVerifyRoundtrip(t *testing.T) {
	t.Parallel()

	secret := "whsec_test_secret"
	body := []byte(`{"event":"payment.settled","id":"pay_1"}`)
	ts := time.Now().UTC()

	sig := webhook.Sign(secret, ts, body)
	require.NotEmpty(t, sig)
	assert.True(t, len(sig) > len("sha256="))
	assert.True(t, webhook.Verify(secret, sig, ts, body))
}

func TestVerifyRejectsTampering(t *testing.T) {
	t.Parallel()

	secret := "whsec_test_secret"
	body := []byte(`{"ok":true}`)
	ts := time.Now().UTC()
	sig := webhook.Sign(secret, ts, body)

	tests := []struct {
		name      string
		secret    string
		signature string
		timestamp time.Time
		body      []byte
	}{
		{
			name:      "wrong secret",
			secret:    "other",
			signature: sig,
			timestamp: ts,
			body:      body,
		},
		{
			name:      "wrong body",
			secret:    secret,
			signature: sig,
			timestamp: ts,
			body:      []byte(`{"ok":false}`),
		},
		{
			name:      "wrong timestamp",
			secret:    secret,
			signature: sig,
			timestamp: ts.Add(time.Second),
			body:      body,
		},
		{
			name:      "empty signature",
			secret:    secret,
			signature: "",
			timestamp: ts,
			body:      body,
		},
		{
			name:      "garbage signature",
			secret:    secret,
			signature: "sha256=deadbeef",
			timestamp: ts,
			body:      body,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.False(t, webhook.Verify(tt.secret, tt.signature, tt.timestamp, tt.body))
		})
	}
}

func TestVerifyReplayProtection(t *testing.T) {
	t.Parallel()

	secret := "whsec_test_secret"
	body := []byte(`{"event":"payment.settled"}`)

	t.Run("valid timestamp within default window", func(t *testing.T) {
		ts := time.Now().Add(-2 * time.Minute)
		sig := webhook.Sign(secret, ts, body)
		assert.True(t, webhook.Verify(secret, sig, ts, body))
	})

	t.Run("expired timestamp beyond default window", func(t *testing.T) {
		ts := time.Now().Add(-6 * time.Minute)
		sig := webhook.Sign(secret, ts, body)
		assert.False(t, webhook.Verify(secret, sig, ts, body), "expected 6-minute-old signature to be rejected")
	})

	t.Run("future timestamp beyond default window", func(t *testing.T) {
		ts := time.Now().Add(6 * time.Minute)
		sig := webhook.Sign(secret, ts, body)
		assert.False(t, webhook.Verify(secret, sig, ts, body), "expected future signature beyond window to be rejected")
	})

	t.Run("custom tolerance window", func(t *testing.T) {
		ts := time.Now().Add(-10 * time.Minute)
		sig := webhook.Sign(secret, ts, body)

		assert.False(t, webhook.VerifyWithTolerance(secret, sig, ts, body, 5*time.Minute))
		assert.True(t, webhook.VerifyWithTolerance(secret, sig, ts, body, 15*time.Minute))
	})

	t.Run("disabled tolerance check", func(t *testing.T) {
		oldTS := time.Unix(1_700_000_000, 0).UTC()
		sig := webhook.Sign(secret, oldTS, body)
		assert.True(t, webhook.VerifyWithTolerance(secret, sig, oldTS, body, 0))
	})
}

func TestSignDeterministic(t *testing.T) {
	t.Parallel()

	secret := "s"
	body := []byte("payload")
	ts := time.Unix(42, 0)

	assert.Equal(t, webhook.Sign(secret, ts, body), webhook.Sign(secret, ts, body))
}

func TestTimestampHeader(t *testing.T) {
	t.Parallel()

	ts := time.Unix(1_700_000_000, 123).UTC()
	assert.Equal(t, "1700000000", webhook.TimestampHeader(ts))
}
