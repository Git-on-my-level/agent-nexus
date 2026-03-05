package errnorm

import (
	"encoding/json"
	"errors"
	"fmt"
)

type Kind string

const (
	KindUsage    Kind = "usage"
	KindLocal    Kind = "local"
	KindNetwork  Kind = "network"
	KindRemote   Kind = "remote"
	KindInternal Kind = "internal"
)

type Error struct {
	Kind    Kind
	Code    string
	Message string
	Details any
	Cause   error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return e.Message
	}
	if e.Message == "" {
		return e.Cause.Error()
	}
	return e.Message + ": " + e.Cause.Error()
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func New(kind Kind, code string, message string) *Error {
	return &Error{Kind: kind, Code: code, Message: message}
}

func Usage(code string, message string) *Error {
	return New(KindUsage, code, message)
}

func Local(code string, message string) *Error {
	return New(KindLocal, code, message)
}

func Network(code string, message string) *Error {
	return New(KindNetwork, code, message)
}

func Internal(code string, message string) *Error {
	return New(KindInternal, code, message)
}

func Wrap(kind Kind, code string, message string, cause error) *Error {
	return &Error{Kind: kind, Code: code, Message: message, Cause: cause}
}

func WithDetails(err *Error, details any) *Error {
	if err == nil {
		return nil
	}
	err.Details = details
	return err
}

func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var typed *Error
	if errors.As(err, &typed) && typed.Kind == KindUsage {
		return 2
	}
	return 1
}

func Normalize(err error) *Error {
	if err == nil {
		return nil
	}
	var typed *Error
	if errors.As(err, &typed) {
		if typed.Code == "" {
			typed.Code = "error"
		}
		if typed.Message == "" {
			typed.Message = typed.Error()
		}
		return typed
	}
	return &Error{
		Kind:    KindInternal,
		Code:    "internal_error",
		Message: err.Error(),
		Cause:   err,
	}
}

func FromHTTPFailure(status int, body []byte) *Error {
	code := "remote_error"
	message := fmt.Sprintf("request failed with status %d", status)
	payload := map[string]any{"status": status}

	if len(body) > 0 {
		payload["body"] = string(body)
	}

	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err == nil {
		if errObj, ok := parsed["error"].(map[string]any); ok {
			if v, ok := errObj["code"].(string); ok && v != "" {
				code = v
			}
			if v, ok := errObj["message"].(string); ok && v != "" {
				message = v
			}
		}
		payload["parsed"] = parsed
	}

	return &Error{
		Kind:    KindRemote,
		Code:    code,
		Message: message,
		Details: payload,
	}
}
