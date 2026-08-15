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
			name:        "non-alpha currency rejected",
			amountPence: 100,
			currency:    "1GB",
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

func TestMoney_IsZeroAndIsPositive(t *testing.T) {
	t.Parallel()

	assert.True(t, money.Money{}.IsZero())
	assert.True(t, money.Money{AmountPence: 0, Currency: "GBP"}.IsZero())
	assert.False(t, money.Money{AmountPence: 1, Currency: "GBP"}.IsZero())

	assert.False(t, money.Money{AmountPence: 0, Currency: "GBP"}.IsPositive())
	assert.True(t, money.Money{AmountPence: 50, Currency: "GBP"}.IsPositive())
	assert.False(t, money.Money{AmountPence: -10, Currency: "GBP"}.IsPositive())
}

func TestMoney_EqualAndSameCurrency(t *testing.T) {
	t.Parallel()

	m1 := money.Money{AmountPence: 100, Currency: "GBP"}
	m2 := money.Money{AmountPence: 100, Currency: "GBP"}
	m3 := money.Money{AmountPence: 200, Currency: "GBP"}
	m4 := money.Money{AmountPence: 100, Currency: "EUR"}

	assert.True(t, m1.Equal(m2))
	assert.False(t, m1.Equal(m3))
	assert.False(t, m1.Equal(m4))

	assert.True(t, m1.SameCurrency(m3))
	assert.False(t, m1.SameCurrency(m4))
}

func TestMoney_Add(t *testing.T) {
	t.Parallel()

	m1 := money.Money{AmountPence: 1000, Currency: "GBP"}
	m2 := money.Money{AmountPence: 550, Currency: "GBP"}
	mOther := money.Money{AmountPence: 550, Currency: "EUR"}

	sum, err := m1.Add(m2)
	require.NoError(t, err)
	assert.Equal(t, money.Money{AmountPence: 1550, Currency: "GBP"}, sum)

	_, err = m1.Add(mOther)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "currency mismatch")
}

func TestMoney_Sub(t *testing.T) {
	t.Parallel()

	m1 := money.Money{AmountPence: 1000, Currency: "GBP"}
	m2 := money.Money{AmountPence: 450, Currency: "GBP"}
	mBigger := money.Money{AmountPence: 1500, Currency: "GBP"}
	mOther := money.Money{AmountPence: 450, Currency: "USD"}

	diff, err := m1.Sub(m2)
	require.NoError(t, err)
	assert.Equal(t, money.Money{AmountPence: 550, Currency: "GBP"}, diff)

	_, err = m1.Sub(mBigger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "results in negative amount")

	_, err = m1.Sub(mOther)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "currency mismatch")
}
