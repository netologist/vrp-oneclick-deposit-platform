package auth_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/hozgan/vrp-demo/pkg/shared/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenService_IssueParse(t *testing.T) {
	t.Parallel()

	svc := auth.NewTokenService("super-secret-key", time.Hour)
	token, expiresAt, err := svc.Issue("merchant_123")
	require.NoError(t, err)
	require.NotEmpty(t, token)
	assert.WithinDuration(t, time.Now().Add(time.Hour), expiresAt, 2*time.Second)

	claims, err := svc.Parse(token)
	require.NoError(t, err)
	assert.Equal(t, "merchant_123", claims.MerchantID)
	assert.Equal(t, "merchant_123", claims.Subject)
	assert.NotEmpty(t, claims.ID)
	require.NotNil(t, claims.ExpiresAt)
	assert.WithinDuration(t, expiresAt, claims.ExpiresAt.Time, time.Second)
}

func TestNewTokenService_DefaultTTL(t *testing.T) {
	t.Parallel()

	svc := auth.NewTokenService("secret", 0)
	token, expiresAt, err := svc.Issue("m1")
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(time.Hour), expiresAt, 2*time.Second)

	claims, err := svc.Parse(token)
	require.NoError(t, err)
	assert.Equal(t, "m1", claims.MerchantID)
}

func TestTokenService_ParseRejects(t *testing.T) {
	t.Parallel()

	svc := auth.NewTokenService("correct-secret", time.Hour)
	good, _, err := svc.Issue("merchant_abc")
	require.NoError(t, err)

	other := auth.NewTokenService("wrong-secret", time.Hour)

	expiredSvc := auth.NewTokenService("correct-secret", -time.Hour)
	expired, _, err := expiredSvc.Issue("merchant_abc")
	require.NoError(t, err)

	// HS384 signed token with same secret — Parse requires HS256.
	wrongAlgClaims := auth.Claims{
		MerchantID: "merchant_abc",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "merchant_abc",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	wrongAlgTok := jwt.NewWithClaims(jwt.SigningMethodHS384, wrongAlgClaims)
	wrongAlg, err := wrongAlgTok.SignedString([]byte("correct-secret"))
	require.NoError(t, err)

	tests := []struct {
		name  string
		token string
	}{
		{name: "empty", token: ""},
		{name: "garbage", token: "not.a.jwt"},
		{name: "wrong secret", token: mustIssue(t, other, "merchant_abc")},
		{name: "expired", token: expired},
		{name: "wrong algorithm", token: wrongAlg},
		{name: "tampered payload", token: good + "x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			claims, err := svc.Parse(tt.token)
			require.Error(t, err)
			assert.Nil(t, claims)
		})
	}
}

func mustIssue(t *testing.T, svc *auth.TokenService, merchantID string) string {
	t.Helper()
	tok, _, err := svc.Issue(merchantID)
	require.NoError(t, err)
	return tok
}
