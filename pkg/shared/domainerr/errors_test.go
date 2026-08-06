package domainerr_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/hozgan/vrp-demo/pkg/shared/domainerr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestError_ErrorAndUnwrap(t *testing.T) {
	t.Parallel()

	t.Run("without cause", func(t *testing.T) {
		t.Parallel()
		err := domainerr.New(domainerr.CodeNotFound, "missing")
		assert.Equal(t, "NOT_FOUND: missing", err.Error())
		assert.Nil(t, err.Unwrap())
	})

	t.Run("with cause", func(t *testing.T) {
		t.Parallel()
		cause := errors.New("db down")
		err := domainerr.Wrap(domainerr.CodeInternal, "query failed", cause)
		assert.Equal(t, "INTERNAL_ERROR: query failed: db down", err.Error())
		assert.Equal(t, cause, err.Unwrap())
		assert.True(t, errors.Is(err, cause))
	})
}

func TestIs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		code domainerr.Code
		want bool
	}{
		{
			name: "direct match",
			err:  domainerr.New(domainerr.CodeValidation, "bad"),
			code: domainerr.CodeValidation,
			want: true,
		},
		{
			name: "direct mismatch",
			err:  domainerr.New(domainerr.CodeValidation, "bad"),
			code: domainerr.CodeNotFound,
			want: false,
		},
		{
			name: "wrapped match",
			err:  fmt.Errorf("outer: %w", domainerr.New(domainerr.CodeRiskDeclined, "no")),
			code: domainerr.CodeRiskDeclined,
			want: true,
		},
		{
			name: "plain error",
			err:  errors.New("plain"),
			code: domainerr.CodeInternal,
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			code: domainerr.CodeInternal,
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, domainerr.Is(tt.err, tt.code))
		})
	}
}

func TestCodeOf(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want domainerr.Code
	}{
		{
			name: "domain error",
			err:  domainerr.New(domainerr.CodeBankRejected, "no"),
			want: domainerr.CodeBankRejected,
		},
		{
			name: "wrapped domain error",
			err:  fmt.Errorf("wrap: %w", domainerr.New(domainerr.CodeConsentExpired, "old")),
			want: domainerr.CodeConsentExpired,
		},
		{
			name: "plain error defaults internal",
			err:  errors.New("boom"),
			want: domainerr.CodeInternal,
		},
		{
			name: "nil defaults internal",
			err:  nil,
			want: domainerr.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, domainerr.CodeOf(tt.err))
		})
	}
}

func TestToGRPC(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		wantCode codes.Code
		wantMsg  string
	}{
		{
			name: "nil",
			err:  nil,
		},
		{
			name:     "validation",
			err:      domainerr.New(domainerr.CodeValidation, "bad field"),
			wantCode: codes.InvalidArgument,
			wantMsg:  "bad field",
		},
		{
			name:     "not found",
			err:      domainerr.New(domainerr.CodeNotFound, "gone"),
			wantCode: codes.NotFound,
			wantMsg:  "gone",
		},
		{
			name:     "already exists",
			err:      domainerr.New(domainerr.CodeAlreadyExists, "dup"),
			wantCode: codes.AlreadyExists,
			wantMsg:  "dup",
		},
		{
			name:     "duplicate idempotency",
			err:      domainerr.New(domainerr.CodeDuplicateIdempotency, "key used"),
			wantCode: codes.AlreadyExists,
			wantMsg:  "key used",
		},
		{
			name:     "consent limit exceeded",
			err:      domainerr.New(domainerr.CodeConsentLimitExceeded, "over"),
			wantCode: codes.FailedPrecondition,
			wantMsg:  "CONSENT_LIMIT_EXCEEDED: over",
		},
		{
			name:     "consent expired",
			err:      domainerr.New(domainerr.CodeConsentExpired, "old"),
			wantCode: codes.FailedPrecondition,
			wantMsg:  "CONSENT_EXPIRED: old",
		},
		{
			name:     "consent revoked",
			err:      domainerr.New(domainerr.CodeConsentRevoked, "revoked"),
			wantCode: codes.FailedPrecondition,
			wantMsg:  "CONSENT_REVOKED: revoked",
		},
		{
			name:     "consent inactive",
			err:      domainerr.New(domainerr.CodeConsentInactive, "off"),
			wantCode: codes.FailedPrecondition,
			wantMsg:  "CONSENT_INACTIVE: off",
		},
		{
			name:     "risk declined",
			err:      domainerr.New(domainerr.CodeRiskDeclined, "risk"),
			wantCode: codes.FailedPrecondition,
			wantMsg:  "RISK_DECLINED: risk",
		},
		{
			name:     "bank rejected",
			err:      domainerr.New(domainerr.CodeBankRejected, "bank"),
			wantCode: codes.FailedPrecondition,
			wantMsg:  "BANK_REJECTED: bank",
		},
		{
			name:     "merchant suspended",
			err:      domainerr.New(domainerr.CodeMerchantSuspended, "suspended"),
			wantCode: codes.FailedPrecondition,
			wantMsg:  "MERCHANT_SUSPENDED: suspended",
		},
		{
			name:     "conflict",
			err:      domainerr.New(domainerr.CodeConflict, "clash"),
			wantCode: codes.FailedPrecondition,
			wantMsg:  "CONFLICT: clash",
		},
		{
			name:     "bank unavailable",
			err:      domainerr.New(domainerr.CodeBankUnavailable, "down"),
			wantCode: codes.Unavailable,
			wantMsg:  "down",
		},
		{
			name:     "internal domain",
			err:      domainerr.New(domainerr.CodeInternal, "oops"),
			wantCode: codes.Internal,
			wantMsg:  "oops",
		},
		{
			name:     "plain error",
			err:      errors.New("raw failure"),
			wantCode: codes.Internal,
			wantMsg:  "raw failure",
		},
		{
			name:     "wrapped domain error",
			err:      fmt.Errorf("layer: %w", domainerr.New(domainerr.CodeNotFound, "missing")),
			wantCode: codes.NotFound,
			wantMsg:  "missing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := domainerr.ToGRPC(tt.err)
			if tt.err == nil {
				assert.Nil(t, got)
				return
			}
			st, ok := status.FromError(got)
			require.True(t, ok)
			assert.Equal(t, tt.wantCode, st.Code())
			assert.Equal(t, tt.wantMsg, st.Message())
		})
	}
}
