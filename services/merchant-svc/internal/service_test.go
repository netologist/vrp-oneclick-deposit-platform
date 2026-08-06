package internal

import (
	"context"
	"testing"

	"github.com/hozgan/vrp-demo/pkg/shared/domainerr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterMerchant_Validation(t *testing.T) {
	svc := NewService(nil)

	tests := []struct {
		name    string
		input   RegisterInput
		errCode domainerr.Code
	}{
		{
			name:    "empty name",
			input:   RegisterInput{Name: "", WebhookURL: "https://example.com/wh"},
			errCode: domainerr.CodeValidation,
		},
		{
			name:    "empty webhook url",
			input:   RegisterInput{Name: "Bet365", WebhookURL: ""},
			errCode: domainerr.CodeValidation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.RegisterMerchant(context.Background(), tt.input)
			require.Error(t, err)
			assert.True(t, domainerr.Is(err, tt.errCode))
		})
	}
}

func TestGenerateAPIKey_Format(t *testing.T) {
	key, err := generateAPIKey()
	require.NoError(t, err)
	assert.True(t, len(key) > 10)
	assert.Equal(t, "vrp_", key[:4])
}
