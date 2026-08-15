package internal

import (
	"context"
	"testing"
	"time"

	merchantv1 "github.com/netologist/vrp-oneclick-deposit-platform/gen/merchant/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMerchantHandler_Validation(t *testing.T) {
	t.Parallel()

	svc := NewService(nil) // nil repo; validation happens before repo calls
	handler := NewHandler(svc)

	t.Run("RegisterMerchant empty name", func(t *testing.T) {
		_, err := handler.RegisterMerchant(context.Background(), &merchantv1.RegisterMerchantRequest{
			Name:       "",
			WebhookUrl: "https://example.com/webhook",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("RegisterMerchant empty webhook_url", func(t *testing.T) {
		_, err := handler.RegisterMerchant(context.Background(), &merchantv1.RegisterMerchantRequest{
			Name:       "Test Merchant",
			WebhookUrl: "",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("GetMerchant empty id", func(t *testing.T) {
		_, err := handler.GetMerchant(context.Background(), &merchantv1.GetMerchantRequest{
			MerchantId: "",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("SuspendMerchant empty id", func(t *testing.T) {
		_, err := handler.SuspendMerchant(context.Background(), &merchantv1.SuspendMerchantRequest{
			MerchantId: "",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("GetMerchantByApiKey empty key", func(t *testing.T) {
		_, err := handler.GetMerchantByApiKey(context.Background(), &merchantv1.GetMerchantByApiKeyRequest{
			ApiKey: "",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("GetWebhookConfig empty id", func(t *testing.T) {
		_, err := handler.GetWebhookConfig(context.Background(), &merchantv1.GetWebhookConfigRequest{
			MerchantId: "",
		})
		require.Error(t, err)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})
}

func TestToProtoMerchant(t *testing.T) {
	t.Parallel()

	assert.Nil(t, toProtoMerchant(nil))

	now := time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC)
	m := &Merchant{
		ID:         "m-100",
		Name:       "Acme Corp",
		KYBStatus:  KYBApproved,
		Status:     StatusActive,
		WebhookURL: "https://example.com/hook",
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	proto := toProtoMerchant(m)
	require.NotNil(t, proto)
	assert.Equal(t, "KYB_APPROVED", proto.GetKybStatus())
	assert.Equal(t, "ACTIVE", proto.GetStatus())
	assert.Equal(t, "https://example.com/hook", proto.GetWebhookUrl())
}
