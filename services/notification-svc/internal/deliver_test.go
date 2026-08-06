package internal

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/webhook"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebhookSigningAndDelivery(t *testing.T) {
	secret := "test-hmac-secret"
	received := false
	var receivedSig string
	var receivedTime string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true
		receivedSig = r.Header.Get("X-PC-Signature")
		receivedTime = r.Header.Get("X-PC-Timestamp")

		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	}))
	defer ts.Close()

	payload := map[string]any{
		"event_type":   "payment.settled",
		"payment_id":   "pay-123",
		"amount_pence": 5000,
	}
	bodyBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	now := time.Now()
	sig := webhook.Sign(secret, now, bodyBytes)

	req, err := http.NewRequest("POST", ts.URL, bytes.NewReader(bodyBytes))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-PC-Signature", sig)
	req.Header.Set("X-PC-Timestamp", webhook.TimestampHeader(now))

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.True(t, received)
	assert.Equal(t, sig, receivedSig)
	assert.NotEmpty(t, receivedTime)
}
