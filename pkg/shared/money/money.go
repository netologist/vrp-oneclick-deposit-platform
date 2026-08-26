package money

import (
	"fmt"
	"strings"
	"unicode"
)

// Money represents a monetary value with integer minor units (e.g. pence, cents) and an ISO 4217 currency.
// Arithmetic operations are currency-safe and protected against float precision loss.
type Money struct {
	AmountPence int64
	Currency    string
}

// New creates a new positive Money value with currency validation.
func New(amountPence int64, currency string) (Money, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if amountPence <= 0 {
		return Money{}, fmt.Errorf("amount must be positive")
	}
	if len(currency) != 3 || !isAlpha(currency) {
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

func (m Money) IsPositive() bool {
	return m.AmountPence > 0
}

// SameCurrency checks whether two Money values share the same currency.
func (m Money) SameCurrency(other Money) bool {
	return m.Currency == other.Currency
}

// Equal returns true if both amount and currency are identical.
func (m Money) Equal(other Money) bool {
	return m.AmountPence == other.AmountPence && m.Currency == other.Currency
}

// Add sums two Money values with matching currency.
func (m Money) Add(other Money) (Money, error) {
	if m.Currency != "" && other.Currency != "" && !m.SameCurrency(other) {
		return Money{}, fmt.Errorf("cannot add %s to %s: currency mismatch", other.Currency, m.Currency)
	}
	cur := m.Currency
	if cur == "" {
		cur = other.Currency
	}
	return Money{AmountPence: m.AmountPence + other.AmountPence, Currency: cur}, nil
}

// Sub subtracts other from m with matching currency, ensuring the result is non-negative.
func (m Money) Sub(other Money) (Money, error) {
	if m.Currency != "" && other.Currency != "" && !m.SameCurrency(other) {
		return Money{}, fmt.Errorf("cannot subtract %s from %s: currency mismatch", other.Currency, m.Currency)
	}
	res := m.AmountPence - other.AmountPence
	if res < 0 {
		return Money{}, fmt.Errorf("subtraction results in negative amount: %d - %d", m.AmountPence, other.AmountPence)
	}
	cur := m.Currency
	if cur == "" {
		cur = other.Currency
	}
	return Money{AmountPence: res, Currency: cur}, nil
}

func isAlpha(s string) bool {
	for _, r := range s {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}
