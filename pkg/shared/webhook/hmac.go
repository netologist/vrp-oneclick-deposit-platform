package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"
)

const (
	// DefaultMaxAge is the maximum allowed age of a webhook signature to prevent replay attacks (5 minutes).
	DefaultMaxAge = 5 * time.Minute

	sigPrefix = "sha256="
	sigLen    = len(sigPrefix) + (sha256.Size * 2) // 7 + 64 = 71
)

// Sign creates a sha256= prefixed HMAC-SHA256 signature over "<timestamp>.<raw_body>".
// It uses stack buffers to avoid heap allocations.
func Sign(secret string, timestamp time.Time, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))

	var tsBuf [20]byte
	tsBytes := strconv.AppendInt(tsBuf[:0], timestamp.Unix(), 10)

	_, _ = mac.Write(tsBytes)
	_, _ = mac.Write([]byte{'.'})
	_, _ = mac.Write(body)

	var digest [sha256.Size]byte
	sum := mac.Sum(digest[:0])

	var out [sigLen]byte
	copy(out[:len(sigPrefix)], sigPrefix)
	hex.Encode(out[len(sigPrefix):], sum)

	return string(out[:])
}

// Verify verifies the HMAC signature using the default 5-minute freshness tolerance window.
func Verify(secret, signature string, timestamp time.Time, body []byte) bool {
	return VerifyWithTolerance(secret, signature, timestamp, body, DefaultMaxAge)
}

// VerifyWithTolerance verifies the HMAC signature and ensures the timestamp is within maxAge of now.
// If maxAge <= 0, timestamp freshness check is disabled.
func VerifyWithTolerance(secret, signature string, timestamp time.Time, body []byte, maxAge time.Duration) bool {
	if maxAge > 0 {
		age := time.Since(timestamp)
		if age < 0 {
			age = -age
		}
		if age > maxAge {
			return false
		}
	}
	expected := Sign(secret, timestamp, body)
	return hmac.Equal([]byte(expected), []byte(signature))
}

func TimestampHeader(t time.Time) string {
	return strconv.FormatInt(t.Unix(), 10)
}
