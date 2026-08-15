package domainerr_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/domainerr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestError_ErrorAndUnwrap(t *testing.T) {
	t.Parallel()

	t.Run("without cause", func(t *testing.T) {
		err := domainerr.New(domainerr.CodeNotFound, "user missing")
		assert.Equal(t, "NOT_FOUND: user missing", err.Error())
		assert.Nil(t, err.Unwrap())
	})

	t.Run("with cause", func(t *testing.T) {
		cause := errors.New("db failure")
		err := domainerr.Wrap(domainerr.CodeInternal, "failed to query", cause)
		assert.Equal(t, "INTERNAL_ERROR: failed to query: db failure", err.Error())
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
			name: "exact match",
			err:  domainerr.New(domainerr.CodeNotFound, "x"),
			code: domainerr.CodeNotFound,
			want: true,
		},
		{
			name: "code mismatch",
			err:  domainerr.New(domainerr.CodeNotFound, "x"),
			code: domainerr.CodeValidation,
			want: false,
		},
		{
			name: "wrapped match",
			err:  fmt.Errorf("wrap: %w", domainerr.New(domainerr.CodeConsentExpired, "x")),
			code: domainerr.CodeConsentExpired,
			want: true,
		},
		{
			name: "plain error is not domain code",
			err:  errors.New("plain"),
			code: domainerr.CodeInternal,
			want: false,
		},
		{
			name: "nil error is false",
			err:  nil,
			code: domainerr.CodeNotFound,
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
			err:  domainerr.New(domainerr.CodeRiskDeclined, "risk"),
			want: domainerr.CodeRiskDeclined,
		},
		{
			name: "wrapped domain error",
			err:  fmt.Errorf("wrap: %w", domainerr.New(domainerr.CodeBankRejected, "rejected")),
			want: domainerr.CodeBankRejected,
		},
		{
			name: "gRPC error with ErrorInfo",
			err:  domainerr.ToGRPC(domainerr.New(domainerr.CodeConsentLimitExceeded, "over limit")),
			want: domainerr.CodeConsentLimitExceeded,
		},
		{
			name: "plain error defaults internal",
			err:  errors.New("something"),
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
		name        string
		err         error
		wantCode    codes.Code
		wantMsg     string
		wantDetails string // expected reason in ErrorInfo
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
			name:        "consent limit exceeded",
			err:         domainerr.New(domainerr.CodeConsentLimitExceeded, "over limit"),
			wantCode:    codes.FailedPrecondition,
			wantMsg:     "over limit",
			wantDetails: string(domainerr.CodeConsentLimitExceeded),
		},
		{
			name:        "consent expired",
			err:         domainerr.New(domainerr.CodeConsentExpired, "old"),
			wantCode:    codes.FailedPrecondition,
			wantMsg:     "old",
			wantDetails: string(domainerr.CodeConsentExpired),
		},
		{
			name:        "consent revoked",
			err:         domainerr.New(domainerr.CodeConsentRevoked, "revoked"),
			wantCode:    codes.FailedPrecondition,
			wantMsg:     "revoked",
			wantDetails: string(domainerr.CodeConsentRevoked),
		},
		{
			name:        "risk declined",
			err:         domainerr.New(domainerr.CodeRiskDeclined, "risk"),
			wantCode:    codes.FailedPrecondition,
			wantMsg:     "risk",
			wantDetails: string(domainerr.CodeRiskDeclined),
		},
		{
			name:        "bank rejected",
			err:         domainerr.New(domainerr.CodeBankRejected, "bank"),
			wantCode:    codes.FailedPrecondition,
			wantMsg:     "bank",
			wantDetails: string(domainerr.CodeBankRejected),
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

			if tt.wantDetails != "" {
				require.NotEmpty(t, st.Details())
				info, ok := st.Details()[0].(*errdetails.ErrorInfo)
				require.True(t, ok)
				assert.Equal(t, domainerr.DomainPlatform, info.Domain)
				assert.Equal(t, tt.wantDetails, info.Reason)
			}
		})
	}
}

func TestFromGRPC(t *testing.T) {
	t.Parallel()

	t.Run("roundtrip with ToGRPC", func(t *testing.T) {
		orig := domainerr.New(domainerr.CodeConsentLimitExceeded, "maximum periodic limit reached")
		grpcErr := domainerr.ToGRPC(orig)

		parsed := domainerr.FromGRPC(grpcErr)
		require.NotNil(t, parsed)
		assert.Equal(t, domainerr.CodeConsentLimitExceeded, parsed.Code)
		assert.Equal(t, "maximum periodic limit reached", parsed.Message)
	})

	t.Run("parses legacy string prefix", func(t *testing.T) {
		legacyErr := status.Error(codes.FailedPrecondition, "CONSENT_REVOKED: user cancelled in bank app")
		parsed := domainerr.FromGRPC(legacyErr)
		require.NotNil(t, parsed)
		assert.Equal(t, domainerr.CodeConsentRevoked, parsed.Code)
		assert.Equal(t, "user cancelled in bank app", parsed.Message)
	})

	t.Run("parses standard gRPC status codes", func(t *testing.T) {
		notFoundErr := status.Error(codes.NotFound, "item not found")
		parsed := domainerr.FromGRPC(notFoundErr)
		require.NotNil(t, parsed)
		assert.Equal(t, domainerr.CodeNotFound, parsed.Code)
		assert.Equal(t, "item not found", parsed.Message)
	})

	t.Run("handles nil", func(t *testing.T) {
		assert.Nil(t, domainerr.FromGRPC(nil))
	})
}
