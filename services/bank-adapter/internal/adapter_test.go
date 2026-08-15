package internal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	bankv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/bank/v1"
	commonv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/common/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMapStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  bankv1.BankPaymentStatus
		err   bool
	}{
		{"SETTLED", bankv1.BankPaymentStatus_SETTLED, false},
		{"REJECTED", bankv1.BankPaymentStatus_REJECTED, false},
		{"AUTHORISED", bankv1.BankPaymentStatus_AUTHORISED, false},
		{"AUTHORIZED", bankv1.BankPaymentStatus_AUTHORISED, false},
		{"PENDING", bankv1.BankPaymentStatus_PENDING, false},
		{"UNKNOWN_STATUS", bankv1.BankPaymentStatus_BANK_PAYMENT_STATUS_UNSPECIFIED, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := mapStatus(tt.input)
			if tt.err {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

func TestAdapter_Validation(t *testing.T) {
	t.Parallel()

	adapter := NewAdapter("http://localhost:18080")

	t.Run("InitiatePayment nil request", func(t *testing.T) {
		_, err := adapter.InitiatePayment(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("InitiatePayment empty payment_id", func(t *testing.T) {
		_, err := adapter.InitiatePayment(context.Background(), &bankv1.InitiateRequest{
			PaymentId:      "",
			BankConsentRef: "ref-1",
			Amount:         &commonv1.Money{AmountPence: 100, Currency: "GBP"},
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("InitiatePayment nil amount", func(t *testing.T) {
		_, err := adapter.InitiatePayment(context.Background(), &bankv1.InitiateRequest{
			PaymentId:      "pay-1",
			BankConsentRef: "ref-1",
			Amount:         nil,
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("GetPaymentStatus nil request", func(t *testing.T) {
		_, err := adapter.GetPaymentStatus(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("GetPaymentStatus empty bank_payment_ref", func(t *testing.T) {
		_, err := adapter.GetPaymentStatus(context.Background(), &bankv1.StatusRequest{
			BankPaymentRef: "",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("ReversePayment nil request", func(t *testing.T) {
		_, err := adapter.ReversePayment(context.Background(), nil)
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("ReversePayment empty bank_payment_ref", func(t *testing.T) {
		_, err := adapter.ReversePayment(context.Background(), &bankv1.ReverseRequest{
			BankPaymentRef: "",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestAdapter_HTTPIntegration(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/bank/payments":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"bank_payment_ref": "FPS-TEST-999",
				"status":           "SETTLED",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/bank/payments/FPS-TEST-999":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"bank_payment_ref": "FPS-TEST-999",
				"status":           "SETTLED",
				"updated_at":       "2026-08-14T12:00:00Z",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	adapter := NewAdapter(ts.URL)

	t.Run("InitiatePayment success", func(t *testing.T) {
		resp, err := adapter.InitiatePayment(context.Background(), &bankv1.InitiateRequest{
			PaymentId:      "pay-123",
			BankConsentRef: "ref-456",
			Amount:         &commonv1.Money{AmountPence: 5000, Currency: "GBP"},
			Description:    "Deposit",
		})
		require.NoError(t, err)
		assert.Equal(t, "FPS-TEST-999", resp.GetBankPaymentRef())
		assert.Equal(t, bankv1.BankPaymentStatus_SETTLED, resp.GetStatus())
	})

	t.Run("GetPaymentStatus success", func(t *testing.T) {
		resp, err := adapter.GetPaymentStatus(context.Background(), &bankv1.StatusRequest{
			BankPaymentRef: "FPS-TEST-999",
		})
		require.NoError(t, err)
		assert.Equal(t, "FPS-TEST-999", resp.GetBankPaymentRef())
		assert.Equal(t, bankv1.BankPaymentStatus_SETTLED, resp.GetStatus())
		assert.NotNil(t, resp.GetUpdatedAt())
	})
}
