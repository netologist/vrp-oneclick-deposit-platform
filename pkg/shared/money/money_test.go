package money_test

import (
	"testing"

	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/money"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		amountPence int64
		currency    string
		want        money.Money
		wantErr     string
	}{
		{
			name:        "valid GBP",
			amountPence: 1050,
			currency:    "GBP",
			want:        money.Money{AmountPence: 1050, Currency: "GBP"},
		},
		{
			name:        "trims and uppercases currency",
			amountPence: 1,
			currency:    " gbp ",
			want:        money.Money{AmountPence: 1, Currency: "GBP"},
		},
		{
			name:        "zero amount rejected",
			amountPence: 0,
			currency:    "GBP",
			wantErr:     "amount must be positive",
		},
		{
			name:        "negative amount rejected",
			amountPence: -1,
			currency:    "GBP",
			wantErr:     "amount must be positive",
		},
		{
			name:        "short currency rejected",
			amountPence: 100,
			currency:    "GB",
			wantErr:     "currency must be 3-letter ISO code",
		},
		{
			name:        "long currency rejected",
			amountPence: 100,
			currency:    "GBPX",
			wantErr:     "currency must be 3-letter ISO code",
		},
		{
			name:        "empty currency rejected",
			amountPence: 100,
			currency:    "",
			wantErr:     "currency must be 3-letter ISO code",
		},
		{
			name:        "whitespace-only currency rejected",
			amountPence: 100,
			currency:    "   ",
			wantErr:     "currency must be 3-letter ISO code",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := money.New(tt.amountPence, tt.currency)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				assert.Equal(t, money.Money{}, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMoney_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		m    money.Money
		want string
	}{
		{
			name: "whole pounds",
			m:    money.Money{AmountPence: 1000, Currency: "GBP"},
			want: "GBP 10.00",
		},
		{
			name: "with pence",
			m:    money.Money{AmountPence: 1050, Currency: "GBP"},
			want: "GBP 10.50",
		},
		{
			name: "single pence",
			m:    money.Money{AmountPence: 1, Currency: "GBP"},
			want: "GBP 0.01",
		},
		{
			name: "zero",
			m:    money.Money{AmountPence: 0, Currency: "GBP"},
			want: "GBP 0.00",
		},
		{
			name: "negative amount formats absolute minor",
			m:    money.Money{AmountPence: -105, Currency: "USD"},
			want: "USD -1.05",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.m.String())
		})
	}
}

func TestMoney_IsZero(t *testing.T) {
	t.Parallel()

	assert.True(t, money.Money{}.IsZero())
	assert.True(t, money.Money{AmountPence: 0, Currency: "GBP"}.IsZero())
	assert.False(t, money.Money{AmountPence: 1, Currency: "GBP"}.IsZero())
}
