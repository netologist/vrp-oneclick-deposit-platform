package domainerr

import (
	"errors"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Code string

const (
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
	return CodeInternal
}

func ToGRPC(err error) error {
	if err == nil {
		return nil
	}
	var de *Error
	if !errors.As(err, &de) {
		return status.Error(codes.Internal, err.Error())
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
		return status.Error(codes.FailedPrecondition, string(de.Code)+": "+de.Message)
	case CodeBankUnavailable:
		return status.Error(codes.Unavailable, de.Message)
	default:
		return status.Error(codes.Internal, de.Message)
	}
}
