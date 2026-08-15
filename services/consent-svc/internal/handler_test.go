package internal

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	commonv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/common/v1"
	consentv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/consent/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestMoneyFromProto(t *testing.T) {
	t.Parallel()
	t.Run("nil money", func(t *testing.T) {
		_, _, err := moneyFromProto(nil)
		require.Error(t, err)
	})

	t.Run("valid money", func(t *testing.T) {
		amt, cur, err := moneyFromProto(&commonv1.Money{AmountPence: 5000, Currency: "GBP"})
		require.NoError(t, err)
		assert.Equal(t, int64(5000), amt)
		assert.Equal(t, "GBP", cur)
	})

	t.Run("invalid currency length", func(t *testing.T) {
		_, _, err := moneyFromProto(&commonv1.Money{AmountPence: 5000, Currency: "GB"})
		require.Error(t, err)
	})

	t.Run("negative amount", func(t *testing.T) {
		_, _, err := moneyFromProto(&commonv1.Money{AmountPence: -100, Currency: "GBP"})
		require.Error(t, err)
	})
}

func TestMapStatus(t *testing.T) {
	t.Parallel()

	assert.Equal(t, consentv1.ConsentStatus_PENDING, mapStatus("PENDING"))
	assert.Equal(t, consentv1.ConsentStatus_ACTIVE, mapStatus("ACTIVE"))
	assert.Equal(t, consentv1.ConsentStatus_REVOKED, mapStatus("REVOKED"))
	assert.Equal(t, consentv1.ConsentStatus_EXPIRED, mapStatus("EXPIRED"))
	assert.Equal(t, consentv1.ConsentStatus_CONSENT_STATUS_UNSPECIFIED, mapStatus("OTHER"))
}

func TestToProtoConsent(t *testing.T) {
	t.Parallel()

	assert.Nil(t, toProtoConsent(nil))

	cID := uuid.NewString()
	mID := uuid.NewString()
	future := time.Now().Add(24 * time.Hour).UTC()
	past := time.Now().Add(-24 * time.Hour).UTC()

	t.Run("active consent in future", func(t *testing.T) {
		row := &ConsentRow{
			ID:              cID,
			MerchantID:      mID,
			ConsumerID:      "cons-1",
			BankConsentRef:  "ref-1",
			Status:          "ACTIVE",
			MaxAmountPence:  5000,
			MaxMonthlyPence: 20000,
			Currency:        "GBP",
			ValidFrom:       past,
			ValidUntil:      future,
			CreatedAt:       past,
			UpdatedAt:       past,
		}
		proto := toProtoConsent(row)
		require.NotNil(t, proto)
		assert.Equal(t, cID, proto.GetId())
		assert.Equal(t, consentv1.ConsentStatus_ACTIVE, proto.GetStatus())
		assert.Equal(t, int64(5000), proto.GetMaxPerTransaction().GetAmountPence())
	})

	t.Run("expired consent past validUntil", func(t *testing.T) {
		row := &ConsentRow{
			ID:              cID,
			MerchantID:      mID,
			ConsumerID:      "cons-1",
			BankConsentRef:  "ref-1",
			Status:          "ACTIVE",
			MaxAmountPence:  5000,
			MaxMonthlyPence: 20000,
			Currency:        "GBP",
			ValidFrom:       past.Add(-48 * time.Hour),
			ValidUntil:      past,
			CreatedAt:       past,
			UpdatedAt:       past,
		}
		proto := toProtoConsent(row)
		require.NotNil(t, proto)
		assert.Equal(t, consentv1.ConsentStatus_EXPIRED, proto.GetStatus())
	})
}

func TestConsentHandler_Validation(t *testing.T) {
	t.Parallel()

	svc := NewService(nil, nil)
	handler := NewHandler(svc)

	t.Run("CreateConsent empty consumer_id", func(t *testing.T) {
		_, err := handler.CreateConsent(context.Background(), &consentv1.CreateConsentRequest{
			MerchantId:        uuid.NewString(),
			ConsumerId:        "",
			BankConsentRef:    "ref-1",
			MaxPerTransaction: &commonv1.Money{AmountPence: 1000, Currency: "GBP"},
			MaxPerMonth:       &commonv1.Money{AmountPence: 5000, Currency: "GBP"},
			ValidUntil:        timestamppb.Now(),
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("CreateConsent currency mismatch", func(t *testing.T) {
		_, err := handler.CreateConsent(context.Background(), &consentv1.CreateConsentRequest{
			MerchantId:        uuid.NewString(),
			ConsumerId:        "cons-1",
			BankConsentRef:    "ref-1",
			MaxPerTransaction: &commonv1.Money{AmountPence: 1000, Currency: "GBP"},
			MaxPerMonth:       &commonv1.Money{AmountPence: 5000, Currency: "EUR"},
			ValidUntil:        timestamppb.Now(),
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}
