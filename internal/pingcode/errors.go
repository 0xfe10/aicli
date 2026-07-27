package pingcode

import (
	"errors"
	"fmt"
	"strings"

	"github.com/0xfe10/aicli/internal/redact"
)

// Stable error codes required by the CLI contract.
const (
	CodeInvalidInput          = "INVALID_INPUT"
	CodeInvalidArgument       = "INVALID_ARGUMENT"
	CodeConfigMissing         = "CONFIG_MISSING"
	CodeAuthRequired          = "AUTH_REQUIRED"
	CodeAuthExpired           = "AUTH_EXPIRED"
	CodeForbidden             = "FORBIDDEN"
	CodeNotFound              = "NOT_FOUND"
	CodeAmbiguousName         = "AMBIGUOUS_NAME"
	CodeExpectedStateMismatch = "EXPECTED_STATE_MISMATCH"
	CodeReadonly              = "READONLY"
	CodeRateLimited           = "RATE_LIMITED"
	CodeUpstreamTimeout       = "UPSTREAM_TIMEOUT"
	CodeUpstreamError         = "UPSTREAM_ERROR"
	CodeInternalError         = "INTERNAL_ERROR"
	CodeUnknownCommand        = "UNKNOWN_COMMAND"
)

// Error is a classified CLI error.
type Error struct {
	Code    string
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// NewError constructs a classified error.
func NewError(code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// WrapError classifies an existing error with a stable code.
func WrapError(code, message string, cause error) *Error {
	return &Error{Code: code, Message: message, Cause: cause}
}

// Classify converts arbitrary errors into stable CLI error codes.
func Classify(err error) *Error {
	if err == nil {
		return nil
	}
	var pe *Error
	if errors.As(err, &pe) {
		return pe
	}
	var coded interface{ ErrorCode() string }
	if errors.As(err, &coded) {
		return WrapError(coded.ErrorCode(), redact.String(err.Error()), err)
	}
	var api *APIError
	if errors.As(err, &api) {
		switch {
		case api.Status == 401:
			return WrapError(CodeAuthExpired, api.Message, api)
		case api.Status == 403:
			return WrapError(CodeForbidden, api.Message, api)
		case api.Status == 404:
			return WrapError(CodeNotFound, api.Message, api)
		case api.Status == 429:
			return WrapError(CodeRateLimited, api.Message, api)
		default:
			return WrapError(CodeUpstreamError, api.Message, api)
		}
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "超时") || strings.Contains(strings.ToLower(msg), "timeout") || strings.Contains(strings.ToLower(msg), "deadline exceeded"):
		return WrapError(CodeUpstreamTimeout, msg, err)
	default:
		return WrapError(CodeInternalError, msg, err)
	}
}

// Redact removes tokens, secrets, authorization codes, and bearer headers.
func Redact(text string) string {
	return redact.String(text)
}

// APIError is a PingCode HTTP failure with redacted serialization.
type APIError struct {
	Message      string
	Status       int
	ResponseText string
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *APIError) String() string {
	return fmt.Sprintf("PingCodeApiError status=%d message=%s", e.Status, e.Message)
}
