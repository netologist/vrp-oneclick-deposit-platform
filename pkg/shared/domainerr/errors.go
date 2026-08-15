package domainerr

import (
	"errors"
	"fmt"
	"strings"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Code string

const (
	DomainPlatform = "vrp.platform"

	CodeValidation           Code = "VALIDATION_ERROR"
	CodeNotFound             Code = "NOT_FOUND"
	CodeAlreadyExists        Code = "ALREADY_EXISTS"
	CodeConsentLimitExceeded Code = "CONSENT_LIMIT_EXCEEDED"
	CodeConsentExpired       Code = "CONSENT_EXPIRED"
	CodeConsentRevoked       Code = "CONSENT_REVOKED"
	CodeConsentInactive      Code = "CONSENT_INACTIVE"
	CodeRiskDeclined         Code = "RISK_DECLINED"
	CodeBankRejected         Code = "BANK_REJECTED"
	CodeBankUnavailable      Code = "BANK_UNAVAILABLE"
	CodeMerchantSuspended    Code = "MERCHANT_SUSPENDED"
	CodeDuplicateIdempotency Code = "DUPLICATE_IDEMPOTENCY_KEY"
	CodeConflict             Code = "CONFLICT"
	CodeInternal             Code = "INTERNAL_ERROR"
)

type Error struct {
	Code    Code
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Err }

func New(code Code, msg string) *Error {
	return &Error{Code: code, Message: msg}
}

func Wrap(code Code, msg string, err error) *Error {
	return &Error{Code: code, Message: msg, Err: err}
}

func Is(err error, code Code) bool {
	var de *Error
	if errors.As(err, &de) {
		return de.Code == code
	}
	return false
}

func CodeOf(err error) Code {
	var de *Error
	if errors.As(err, &de) {
		return de.Code
	}
	if err == nil {
		return CodeInternal
	}
	// Try parsing from gRPC error
	if parsed := FromGRPC(err); parsed != nil {
		return parsed.Code
	}
	return CodeInternal
}

func MessageOf(err error) string {
	var de *Error
	if errors.As(err, &de) {
		return de.Message
	}
	if parsed := FromGRPC(err); parsed != nil {
		return parsed.Message
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

// ToGRPC converts a domain error into a gRPC status error.
// Business errors are converted with google.rpc.ErrorInfo details to avoid fragile string parsing.
func ToGRPC(err error) error {
	if err == nil {
		return nil
	}
	var de *Error
	if !errors.As(err, &de) {
		return status.Error(codes.Internal, err.Error())
	}

	makeFailedPrecondition := func() error {
		st := status.New(codes.FailedPrecondition, de.Message)
		stWithDetails, err := st.WithDetails(&errdetails.ErrorInfo{
			Reason: string(de.Code),
			Domain: DomainPlatform,
		})
		if err == nil {
			return stWithDetails.Err()
		}
		// Fallback to legacy string prefix if WithDetails fails
		return status.Error(codes.FailedPrecondition, string(de.Code)+": "+de.Message)
	}

	switch de.Code {
	case CodeValidation:
		return status.Error(codes.InvalidArgument, de.Message)
	case CodeNotFound:
		return status.Error(codes.NotFound, de.Message)
	case CodeAlreadyExists, CodeDuplicateIdempotency:
		return status.Error(codes.AlreadyExists, de.Message)
	case CodeConsentLimitExceeded, CodeConsentExpired, CodeConsentRevoked,
		CodeConsentInactive, CodeRiskDeclined, CodeBankRejected,
		CodeMerchantSuspended, CodeConflict:
		return makeFailedPrecondition()
	case CodeBankUnavailable:
		return status.Error(codes.Unavailable, de.Message)
	default:
		return status.Error(codes.Internal, de.Message)
	}
}

// FromGRPC extracts domain code and message from a gRPC status error.
// It prioritizes typed errdetails.ErrorInfo metadata, falling back to legacy prefix parsing.
func FromGRPC(err error) *Error {
	if err == nil {
		return nil
	}
	var de *Error
	if errors.As(err, &de) {
		return de
	}

	st, ok := status.FromError(err)
	if !ok {
		return New(CodeInternal, err.Error())
	}

	// 1. Check if structured ErrorInfo detail exists
	for _, detail := range st.Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok && info.Domain == DomainPlatform {
			return New(Code(info.Reason), st.Message())
		}
	}

	// 2. Check for legacy code: message prefix in FailedPrecondition
	msg := st.Message()
	if idx := strings.Index(msg, ": "); idx != -1 {
		potentialCode := Code(msg[:idx])
		if isKnownCode(potentialCode) {
			return New(potentialCode, msg[idx+2:])
		}
	}

	// 3. Map standard gRPC codes
	switch st.Code() {
	case codes.InvalidArgument:
		return New(CodeValidation, msg)
	case codes.NotFound:
		return New(CodeNotFound, msg)
	case codes.AlreadyExists:
		return New(CodeAlreadyExists, msg)
	case codes.Unavailable:
		return New(CodeBankUnavailable, msg)
	default:
		return New(CodeInternal, msg)
	}
}

func isKnownCode(c Code) bool {
	switch c {
	case CodeValidation, CodeNotFound, CodeAlreadyExists, CodeConsentLimitExceeded,
		CodeConsentExpired, CodeConsentRevoked, CodeConsentInactive, CodeRiskDeclined,
		CodeBankRejected, CodeBankUnavailable, CodeMerchantSuspended,
		CodeDuplicateIdempotency, CodeConflict, CodeInternal:
		return true
	default:
		return false
	}
}
