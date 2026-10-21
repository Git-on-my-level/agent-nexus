package app

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strconv"

	"agent-nexus-cli/internal/errnorm"
)

type updateHTTPStatusError struct {
	status int
}

func (e updateHTTPStatusError) Error() string {
	return "release request failed with HTTP status " + strconv.Itoa(e.status)
}

// updateFailureCategory deliberately returns only a small, non-secret
// diagnostic. Never persist the underlying error, which can include a URL,
// response body, or transport details.
func updateFailureCategory(err error) string {
	if err == nil {
		return "network"
	}
	var statusErr updateHTTPStatusError
	if errors.As(err, &statusErr) && validUpdateHTTPStatus(statusErr.status) {
		return "http_" + strconv.Itoa(statusErr.status)
	}
	for cause := err; cause != nil; cause = errors.Unwrap(cause) {
		if typed, ok := cause.(*errnorm.Error); ok {
			if details, ok := typed.Details.(map[string]any); ok {
				if status, ok := details["status"].(int); ok && validUpdateHTTPStatus(status) {
					return "http_" + strconv.Itoa(status)
				}
			}
		}
	}
	for cause := err; cause != nil; cause = errors.Unwrap(cause) {
		if typed, ok := cause.(*errnorm.Error); ok && typed.Code == "invalid_response" {
			return "invalid_response"
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	var verificationErr *tls.CertificateVerificationError
	var recordHeaderErr tls.RecordHeaderError
	var unknownAuthorityErr x509.UnknownAuthorityError
	var hostnameErr x509.HostnameError
	var invalidCertErr x509.CertificateInvalidError
	var alertErr tls.AlertError
	if errors.As(err, &verificationErr) || errors.As(err, &recordHeaderErr) ||
		errors.As(err, &unknownAuthorityErr) || errors.As(err, &hostnameErr) ||
		errors.As(err, &invalidCertErr) || errors.As(err, &alertErr) {
		return "tls"
	}
	var syntaxErr *json.SyntaxError
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &syntaxErr) || errors.As(err, &typeErr) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return "invalid_response"
	}
	return "network"
}

func validUpdateHTTPStatus(status int) bool {
	return status >= 100 && status <= 599
}

func validUpdateFailureCategory(category string) bool {
	switch category {
	case "timeout", "network", "tls", "invalid_response":
		return true
	}
	if len(category) < len("http_100") || len(category) > len("http_599") || category[:5] != "http_" {
		return false
	}
	status, err := strconv.Atoi(category[5:])
	return err == nil && validUpdateHTTPStatus(status) && category == "http_"+strconv.Itoa(status)
}
