package mockbank

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockBankServer_InitiatePayment(t *testing.T) {
	srv := New("127.0.0.1:0")
	handler := srv.Handler()

	t.Run("happy path initiate", func(t *testing.T) {
		body := map[string]any{
			"payment_id":       "pay-123",
			"bank_consent_ref": "ob-consent-456",
			"consumer_id":      "cons-789",
			"amount_pence":     5000,
			"currency":        "GBP",
			"description":     "Deposit",
		}
		b, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/bank/payments", bytes.NewReader(b))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		var resp map[string]any
		err = json.Unmarshal(rec.Body.Bytes(), &resp)
		require.NoError(t, err)

		assert.Equal(t, "SETTLED", resp["status"])
		assert.Contains(t, resp["bank_payment_ref"], "FPS-")
	})

	t.Run("invalid payload returns 400", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/bank/payments", bytes.NewBufferString("invalid json"))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}
