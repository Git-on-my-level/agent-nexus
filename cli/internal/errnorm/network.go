package errnorm

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"strings"
	"syscall"
)

// TransportClass inspects typed causes, never URL/error text that may contain credentials.
func TransportClass(err error) string {
	var classified interface{ FailureClass() string }
	if errors.As(err, &classified) {
		return classified.FailureClass()
	}
	var dns *net.DNSError
	var cert *tls.CertificateVerificationError
	var unknownCA x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var record tls.RecordHeaderError
	var op *net.OpError
	var network net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.As(err, &dns):
		return "dns"
	case errors.As(err, &cert), errors.As(err, &unknownCA), errors.As(err, &hostname), errors.As(err, &invalid), errors.As(err, &record):
		return "tls"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection_refused"
	case errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE):
		return "connection_reset"
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "connection_closed"
	case errors.As(err, &network) && network.Timeout():
		return "timeout"
	case errors.As(err, &op):
		return "connection_error"
	default:
		return "network_error"
	}
}

func enrichTransport(err *Error) {
	class := TransportClass(err.Cause)
	details, _ := err.Details.(map[string]any)
	if details == nil {
		details = map[string]any{}
	}
	details["failure_class"] = class
	// The shared client supplies only numeric durations, counts and phase names.
	var info interface{ TransportDetails() map[string]any }
	if errors.As(err.Cause, &info) {
		for key, value := range info.TransportDetails() {
			details[key] = value
		}
	}
	err.Details = details
	err.Message += ": " + strings.ReplaceAll(class, "_", " ")
	// Preserve uncertain-write advice: classification must never authorize a replay.
	if err.Code == "outcome_unknown" {
		return
	}
	retryable := class != "tls" && class != "canceled"
	err.Recoverable = &retryable
	switch class {
	case "timeout":
		err.Hint = "The network request timed out. Retry the read or increase --timeout (total request budget)."
	case "dns":
		err.Hint = "DNS resolution failed. Check the workspace hostname and DNS connectivity."
	case "tls":
		err.Hint = "TLS verification or handshake failed. Check the server certificate and local trust configuration."
	case "canceled":
		err.Hint = "The request was canceled."
	default:
		err.Hint = "Check network and server availability, then retry the read."
	}
}
