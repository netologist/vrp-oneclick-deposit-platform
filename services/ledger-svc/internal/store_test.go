package internal

import (
	"testing"

	ledgerv1 "github.com/hozgan/vrp-demo/gen/ledger/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAccountTypeConverters(t *testing.T) {
	tests := []struct {
		input    ledgerv1.AccountType
		dbStr    string
		hasError bool
	}{
		{input: ledgerv1.AccountType_CONSUMER_ESCROW, dbStr: "CONSUMER_ESCROW"},
		{input: ledgerv1.AccountType_MERCHANT_ESCROW, dbStr: "MERCHANT_ESCROW"},
		{input: ledgerv1.AccountType_PLATFORM_FEE, dbStr: "PLATFORM_FEE"},
		{input: ledgerv1.AccountType_ACCOUNT_TYPE_UNSPECIFIED, hasError: true},
	}

	for _, tt := range tests {
		got, err := accountTypeToDB(tt.input)
		if tt.hasError {
			assert.Error(t, err)
		} else {
			require.NoError(t, err)
			assert.Equal(t, tt.dbStr, got)
			assert.Equal(t, tt.input, accountTypeFromDB(got))
		}
	}
}

func TestDirectionConverters(t *testing.T) {
	tests := []struct {
		input    ledgerv1.Direction
		dbStr    string
		hasError bool
	}{
		{input: ledgerv1.Direction_DR, dbStr: "DR"},
		{input: ledgerv1.Direction_CR, dbStr: "CR"},
		{input: ledgerv1.Direction_DIRECTION_UNSPECIFIED, hasError: true},
	}

	for _, tt := range tests {
		got, err := directionToDB(tt.input)
		if tt.hasError {
			assert.Error(t, err)
		} else {
			require.NoError(t, err)
			assert.Equal(t, tt.dbStr, got)
			assert.Equal(t, tt.input, directionFromDB(got))
		}
	}
}
