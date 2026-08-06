package money

import (
	"fmt"
	"strings"
)

// Money is always integer minor units + ISO 4217 currency. Never float64.
type Money struct {
	AmountPence int64
	Currency    string
}

func New(amountPence int64, currency string) (Money, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if amountPence <= 0 {
		return Money{}, fmt.Errorf("amount must be positive")
	}
	if len(currency) != 3 {
		return Money{}, fmt.Errorf("currency must be 3-letter ISO code")
	}
	return Money{AmountPence: amountPence, Currency: currency}, nil
}

func (m Money) String() string {
	major := m.AmountPence / 100
	minor := m.AmountPence % 100
	if minor < 0 {
		minor = -minor
	}
	return fmt.Sprintf("%s %d.%02d", m.Currency, major, minor)
}

func (m Money) IsZero() bool {
	return m.AmountPence == 0
}
