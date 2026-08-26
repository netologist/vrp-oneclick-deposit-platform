package httpapi

import (
	"testing"
	"time"

	commonv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/common/v1"
	consentv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/consent/v1"
	merchantv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/merchant/v1"
	paymentv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/payment/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestMoneyConverters(t *testing.T) {
	t.Parallel()

	t.Run("moneyToProto", func(t *testing.T) {
		m := Money{AmountPence: 2500, Currency: "GBP"}
		proto := moneyToProto(m)
		require.NotNil(t, proto)
		assert.Equal(t, int64(2500), proto.GetAmountPence())
		assert.Equal(t, "GBP", proto.GetCurrency())
	})

	t.Run("moneyFromProto nil guard", func(t *testing.T) {
		m := moneyFromProto(nil)
		assert.Equal(t, int64(0), m.AmountPence)
		assert.Equal(t, "", m.Currency)
	})

	t.Run("moneyFromProto valid", func(t *testing.T) {
		proto := &commonv1.Money{AmountPence: 1200, Currency: "EUR"}
		m := moneyFromProto(proto)
		assert.Equal(t, int64(1200), m.AmountPence)
		assert.Equal(t, "EUR", m.Currency)
	})
}

func TestTimeConverters(t *testing.T) {
	t.Parallel()

	t.Run("tsString nil timestamp", func(t *testing.T) {
		assert.Equal(t, "", tsString(nil))
	})

	t.Run("tsString valid timestamp", func(t *testing.T) {
		now := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
		ts := timestamppb.New(now)
		assert.Equal(t, "2026-08-14T12:00:00Z", tsString(ts))
	})

	t.Run("parseTime RFC3339", func(t *testing.T) {
		ts, err := parseTime("2026-08-14T12:00:00Z")
		require.NoError(t, err)
		assert.Equal(t, int64(1786708800), ts.GetSeconds())
	})

	t.Run("parseTime RFC3339Nano", func(t *testing.T) {
		ts, err := parseTime("2026-08-14T12:00:00.123456789Z")
		require.NoError(t, err)
		assert.Equal(t, int64(1786708800), ts.GetSeconds())
	})

	t.Run("parseTime empty string", func(t *testing.T) {
		ts, err := parseTime("")
		require.NoError(t, err)
		assert.Nil(t, ts)
	})

	t.Run("parseTime invalid format", func(t *testing.T) {
		_, err := parseTime("invalid-date-format")
		require.Error(t, err)
	})
}

func TestConsentStatusConverters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		enum consentv1.ConsentStatus
		str  string
	}{
		{consentv1.ConsentStatus_PENDING, "PENDING"},
		{consentv1.ConsentStatus_ACTIVE, "ACTIVE"},
		{consentv1.ConsentStatus_REVOKED, "REVOKED"},
		{consentv1.ConsentStatus_EXPIRED, "EXPIRED"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.str, consentStatusString(tt.enum))
		assert.Equal(t, tt.enum, parseConsentStatus(tt.str))
		assert.Equal(t, tt.enum, parseConsentStatus("  "+tt.str+"  "))
	}

	assert.Equal(t, consentv1.ConsentStatus_CONSENT_STATUS_UNSPECIFIED, parseConsentStatus("UNKNOWN_STATUS"))
}

func TestPaymentStatusConverters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		enum paymentv1.PaymentStatus
		str  string
	}{
		{paymentv1.PaymentStatus_INITIATED, "INITIATED"},
		{paymentv1.PaymentStatus_CONSENT_RESERVED, "CONSENT_RESERVED"},
		{paymentv1.PaymentStatus_RISK_PASSED, "RISK_PASSED"},
		{paymentv1.PaymentStatus_AUTHORISING, "AUTHORISING"},
		{paymentv1.PaymentStatus_SETTLED, "SETTLED"},
		{paymentv1.PaymentStatus_FAILED, "FAILED"},
		{paymentv1.PaymentStatus_MANUAL_REVIEW, "MANUAL_REVIEW"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.str, paymentStatusString(tt.enum))
	}
}

func TestEntityFromProto(t *testing.T) {
	t.Parallel()

	t.Run("merchantFromProto", func(t *testing.T) {
		m := merchantFromProto(nil)
		assert.Equal(t, "", m.ID)

		proto := &merchantv1.Merchant{
			Id:         "m-123",
			Name:       "Test Merchant",
			WebhookUrl: "https://example.com/webhook",
			KybStatus:  "APPROVED",
			Status:     "ACTIVE",
			CreatedAt:  timestamppb.Now(),
			UpdatedAt:  timestamppb.Now(),
		}
		resp := merchantFromProto(proto)
		assert.Equal(t, "m-123", resp.ID)
		assert.Equal(t, "Test Merchant", resp.Name)
		assert.Equal(t, "APPROVED", resp.KYBStatus)
		assert.NotEmpty(t, resp.CreatedAt)
	})

	t.Run("consentFromProto", func(t *testing.T) {
		c := consentFromProto(nil)
		assert.Equal(t, "", c.ID)

		proto := &consentv1.Consent{
			Id:                "c-1",
			MerchantId:        "m-1",
			ConsumerId:        "cons-1",
			BankConsentRef:    "bank-ref-1",
			MaxPerTransaction: &commonv1.Money{AmountPence: 5000, Currency: "GBP"},
			MaxPerMonth:       &commonv1.Money{AmountPence: 20000, Currency: "GBP"},
			Status:            consentv1.ConsentStatus_ACTIVE,
			ValidUntil:        timestamppb.Now(),
		}
		resp := consentFromProto(proto)
		assert.Equal(t, "c-1", resp.ID)
		assert.Equal(t, "ACTIVE", resp.Status)
		assert.Equal(t, int64(5000), resp.MaxPerTransaction.AmountPence)
	})

	t.Run("paymentFromProto", func(t *testing.T) {
		p := paymentFromProto(nil)
		assert.Equal(t, "", p.ID)

		proto := &paymentv1.Payment{
			Id:             "p-1",
			MerchantId:     "m-1",
			ConsentId:      "c-1",
			ConsumerId:     "cons-1",
			Amount:         &commonv1.Money{AmountPence: 3000, Currency: "GBP"},
			Status:         paymentv1.PaymentStatus_SETTLED,
			BankPaymentRef: "FPS-123",
			InitiatedAt:    timestamppb.Now(),
			SettledAt:      timestamppb.Now(),
		}
		resp := paymentFromProto(proto)
		assert.Equal(t, "p-1", resp.ID)
		assert.Equal(t, "SETTLED", resp.Status)
		assert.Equal(t, "FPS-123", resp.BankPaymentRef)
	})
}
