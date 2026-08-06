package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func writeJSON(w http.ResponseWriter, statusCode int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, r *http.Request, statusCode int, code, message string) {
	writeJSON(w, statusCode, ErrorBody{
		Code:      code,
		Message:   message,
		RequestID: RequestIDFrom(r.Context()),
	})
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func mapGRPCError(w http.ResponseWriter, r *http.Request, err error) {
	st, ok := status.FromError(err)
	if !ok {
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", "unexpected error")
		return
	}

	msg := st.Message()
	code := grpcBusinessCode(st.Code(), msg)

	switch st.Code() {
	case codes.InvalidArgument:
		writeError(w, r, http.StatusBadRequest, code, msg)
	case codes.NotFound:
		writeError(w, r, http.StatusNotFound, code, msg)
	case codes.AlreadyExists:
		writeError(w, r, http.StatusConflict, code, msg)
	case codes.FailedPrecondition:
		writeError(w, r, http.StatusUnprocessableEntity, code, stripCodePrefix(msg))
	case codes.Unauthenticated:
		writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", msg)
	case codes.PermissionDenied:
		writeError(w, r, http.StatusForbidden, "FORBIDDEN", msg)
	case codes.ResourceExhausted:
		writeError(w, r, http.StatusTooManyRequests, "RATE_LIMITED", msg)
	case codes.Unavailable:
		writeError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", msg)
	case codes.Aborted:
		writeError(w, r, http.StatusConflict, code, msg)
	default:
		writeError(w, r, http.StatusInternalServerError, "INTERNAL_ERROR", msg)
	}
}

func grpcBusinessCode(c codes.Code, msg string) string {
	// domainerr.ToGRPC prefixes FailedPrecondition with "CODE: message"
	if c == codes.FailedPrecondition {
		if i := strings.Index(msg, ": "); i > 0 {
			prefix := msg[:i]
			if isBusinessCode(prefix) {
				return prefix
			}
		}
	}
	switch c {
	case codes.InvalidArgument:
		return "VALIDATION_ERROR"
	case codes.NotFound:
		return "NOT_FOUND"
	case codes.AlreadyExists:
		return "ALREADY_EXISTS"
	case codes.Aborted:
		return "CONFLICT"
	default:
		return "INTERNAL_ERROR"
	}
}

func stripCodePrefix(msg string) string {
	if i := strings.Index(msg, ": "); i > 0 && isBusinessCode(msg[:i]) {
		return strings.TrimSpace(msg[i+2:])
	}
	return msg
}

func isBusinessCode(s string) bool {
	switch s {
	case "VALIDATION_ERROR", "NOT_FOUND", "ALREADY_EXISTS", "CONSENT_LIMIT_EXCEEDED",
		"CONSENT_EXPIRED", "CONSENT_REVOKED", "CONSENT_INACTIVE", "RISK_DECLINED",
		"BANK_REJECTED", "BANK_UNAVAILABLE", "MERCHANT_SUSPENDED",
		"DUPLICATE_IDEMPOTENCY_KEY", "CONFLICT", "INTERNAL_ERROR":
		return true
	default:
		return false
	}
}
