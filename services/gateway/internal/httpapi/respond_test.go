package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/netologist/vrp-oneclick-deposit-platform/pkg/shared/domainerr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestMapGRPCError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		err          error
		expectedCode int
		expectedBody string // expected error_code in JSON
	}{
		{
			name:         "domain validation error",
			err:          domainerr.ToGRPC(domainerr.New(domainerr.CodeValidation, "invalid input")),
			expectedCode: http.StatusBadRequest,
			expectedBody: "VALIDATION_ERROR",
		},
		{
			name:         "domain not found",
			err:          domainerr.ToGRPC(domainerr.New(domainerr.CodeNotFound, "merchant not found")),
			expectedCode: http.StatusNotFound,
			expectedBody: "NOT_FOUND",
		},
		{
			name:         "domain consent limit exceeded",
			err:          domainerr.ToGRPC(domainerr.New(domainerr.CodeConsentLimitExceeded, "limit reached")),
			expectedCode: http.StatusUnprocessableEntity,
			expectedBody: "CONSENT_LIMIT_EXCEEDED",
		},
		{
			name:         "domain risk declined",
			err:          domainerr.ToGRPC(domainerr.New(domainerr.CodeRiskDeclined, "high risk score")),
			expectedCode: http.StatusUnprocessableEntity,
			expectedBody: "RISK_DECLINED",
		},
		{
			name:         "domain bank unavailable",
			err:          domainerr.ToGRPC(domainerr.New(domainerr.CodeBankUnavailable, "bank down")),
			expectedCode: http.StatusServiceUnavailable,
			expectedBody: "BANK_UNAVAILABLE",
		},
		{
			name:         "raw grpc unauthenticated",
			err:          status.Error(codes.Unauthenticated, "bad token"),
			expectedCode: http.StatusUnauthorized,
			expectedBody: "UNAUTHORIZED",
		},
		{
			name:         "raw grpc permission denied",
			err:          status.Error(codes.PermissionDenied, "forbidden"),
			expectedCode: http.StatusForbidden,
			expectedBody: "FORBIDDEN",
		},
		{
			name:         "raw non-grpc plain error",
			err:          errors.New("connection reset"),
			expectedCode: http.StatusInternalServerError,
			expectedBody: "INTERNAL_ERROR",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/test", nil)

			mapGRPCError(rec, req, tt.err)

			assert.Equal(t, tt.expectedCode, rec.Code)
			var body ErrorBody
			err := json.Unmarshal(rec.Body.Bytes(), &body)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedBody, body.Code)
		})
	}
}

func TestStripCodePrefix(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "user limit reached", stripCodePrefix("CONSENT_LIMIT_EXCEEDED: user limit reached"))
	assert.Equal(t, "plain message", stripCodePrefix("plain message"))
	assert.Equal(t, "UNKNOWN_CODE: message", stripCodePrefix("UNKNOWN_CODE: message"))
}
