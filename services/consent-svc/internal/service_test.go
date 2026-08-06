package internal

import (
	"context"
	"testing"
	"time"

	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/domainerr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateConsent_Validation(t *testing.T) {
	svc := NewService(nil, nil)

	tests := []struct {
		name    string
		input   CreateInput
		errCode domainerr.Code
	}{
		{
			name: "empty consumer id",
			input: CreateInput{
				ConsumerID:       "",
				BankConsentRef:   "ob-123",
				MaxAmountPence:   5000,
				MaxMonthlyPence:  50000,
				Currency:         "GBP",
				ValidUntil:       time.Now().Add(24 * time.Hour),
			},
			errCode: domainerr.CodeValidation,
		},
		{
			name: "empty bank consent ref",
			input: CreateInput{
				ConsumerID:       "cons-123",
				BankConsentRef:   "",
				MaxAmountPence:   5000,
				MaxMonthlyPence:  50000,
				Currency:         "GBP",
				ValidUntil:       time.Now().Add(24 * time.Hour),
			},
			errCode: domainerr.CodeValidation,
		},
		{
			name: "invalid max amount",
			input: CreateInput{
				ConsumerID:       "cons-123",
				BankConsentRef:   "ob-123",
				MaxAmountPence:   0,
				MaxMonthlyPence:  50000,
				Currency:         "GBP",
				ValidUntil:       time.Now().Add(24 * time.Hour),
			},
			errCode: domainerr.CodeValidation,
		},
		{
			name: "valid_until in past",
			input: CreateInput{
				ConsumerID:       "cons-123",
				BankConsentRef:   "ob-123",
				MaxAmountPence:   5000,
				MaxMonthlyPence:  50000,
				Currency:         "GBP",
				ValidUntil:       time.Now().Add(-1 * time.Hour),
			},
			errCode: domainerr.CodeValidation,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.CreateConsent(context.Background(), tt.input)
			require.Error(t, err)
			assert.True(t, domainerr.Is(err, tt.errCode))
		})
	}
}
