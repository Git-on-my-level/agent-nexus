package httpclient

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"agent-nexus-cli/internal/errnorm"
	contractsclient "agent-nexus-contracts-go-client/client"
)

const connectTimeout = 10 * time.Second

type requestFailure struct {
	cause          error
	attempts       int
	phase          string
	total, attempt time.Duration
}

func (e *requestFailure) Error() string {
	return "network request failed: " + strings.ReplaceAll(e.FailureClass(), "_", " ")
}
func (e *requestFailure) Unwrap() error { return e.cause }
func (e *requestFailure) FailureClass() string {
	class := errnorm.TransportClass(e.cause)
	if e.phase == "tls" && (class == "network_error" || class == "connection_error") {
		return "tls"
	}
	return class
}
func (e *requestFailure) TransportDetails() map[string]any {
	return map[string]any{"attempts": e.attempts, "phase": e.phase, "timeout_ms": e.total.Milliseconds(), "attempt_timeout_ms": e.attempt.Milliseconds(), "connect_timeout_ms": min(connectTimeout, e.attempt).Milliseconds()}
}

// retry bounds the complete exchange, including response reads and backoff.
// Unsafe writes retain the whole budget and are never automatically replayed.
func (c *Client) retry(ctx context.Context, safe bool, call func(context.Context) (RawResponse, error)) (RawResponse, error) {
	ctx, cancel := WithTimeout(ctx, c.timeout)
	defer cancel()
	attemptTimeout := c.timeout
	maxAttempts := 1
	if safe {
		attemptTimeout = min(c.timeout, max(20*time.Second, c.timeout/3))
		maxAttempts = 3
	}
	backoff := min(200*time.Millisecond, c.timeout/10)
	var result RawResponse
	var err error
	phase := "connect"
	attempts := 0
	for attempts < maxAttempts {
		if ctx.Err() != nil {
			err = ctx.Err()
			break
		}
		attempts++
		attemptCtx, stop := WithTimeout(ctx, attemptTimeout)
		var current atomic.Value
		current.Store("connect")
		trace := &httptrace.ClientTrace{
			DNSStart:             func(httptrace.DNSStartInfo) { current.Store("dns") },
			ConnectStart:         func(string, string) { current.Store("connect") },
			TLSHandshakeStart:    func() { current.Store("tls") },
			GotConn:              func(httptrace.GotConnInfo) { current.Store("response_headers") },
			GotFirstResponseByte: func() { current.Store("response_body") },
		}
		result, err = call(httptrace.WithClientTrace(attemptCtx, trace))
		phase = current.Load().(string)
		stop()
		if err != nil && result.StatusCode < 400 {
			err = &requestFailure{cause: err, attempts: attempts, phase: phase, total: c.timeout, attempt: attemptTimeout}
		}
		if ctx.Err() != nil {
			err = ctx.Err()
			break
		}
		if attempts == maxAttempts || !safe || !shouldRetry(result, err) {
			break
		}
		delay := backoff * time.Duration(1<<(attempts-1))
		if value := result.Headers.Get("Retry-After"); value != "" {
			if seconds, parseErr := strconv.ParseInt(value, 10, 32); parseErr == nil && seconds >= 0 {
				delay = max(delay, time.Duration(seconds)*time.Second)
			} else if date, parseErr := http.ParseTime(value); parseErr == nil {
				delay = max(delay, time.Until(date))
			}
		}
		if deadline, ok := ctx.Deadline(); ok && delay >= time.Until(deadline) {
			break
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			err = ctx.Err()
		case <-timer.C:
			continue
		}
		break
	}
	if err != nil && (result.StatusCode < 400 || shouldRetry(result, err)) {
		if failure, ok := err.(*requestFailure); ok {
			return result, failure
		}
		return result, &requestFailure{cause: err, attempts: attempts, phase: phase, total: c.timeout, attempt: attemptTimeout}
	}
	return result, err
}

func shouldRetry(resp RawResponse, err error) bool {
	if resp.StatusCode >= 400 {
		return resp.StatusCode >= 500 && resp.StatusCode <= 599
	}
	if err == nil {
		return false
	}
	switch errnorm.TransportClass(err) {
	case "timeout", "connection_refused", "connection_reset", "connection_closed", "connection_error":
		return true
	case "dns":
		var dns *net.DNSError
		return errors.As(err, &dns) && (dns.IsTimeout || dns.IsTemporary)
	}
	return false
}

// Invoke retains generated request/response handling while applying the same
// bounded policy as raw reads. A request_key alone is not proof of server replay.
func (c *Client) Invoke(ctx context.Context, commandID string, params map[string]string, opts contractsclient.RequestOptions) (*http.Response, []byte, error) {
	safe := false
	for _, spec := range contractsclient.CommandRegistry {
		if spec.CommandID == commandID {
			safe = spec.Method == "GET" || spec.Method == "HEAD" || commandID == "refs.resolve"
			break
		}
	}
	var response *http.Response
	result, err := c.retry(ctx, safe, func(attempt context.Context) (RawResponse, error) {
		var body []byte
		var callErr error
		response, body, callErr = c.generated.Invoke(attempt, commandID, params, opts)
		r := RawResponse{Body: body}
		if response != nil {
			r.StatusCode = response.StatusCode
			r.Headers = response.Header
		}
		return r, callErr
	})
	return response, result.Body, err
}

// HostAssertion re-signs each token exchange. Core consumes each proof once;
// replaying the original signed body after a lost response would fail authentication.
func (c *Client) HostAssertion(ctx context.Context, sign func() []byte) (RawResponse, error) {
	return c.retry(ctx, true, func(attempt context.Context) (RawResponse, error) {
		return c.rawCallOnce(attempt, RawRequest{Method: "POST", Path: "/auth/token", Body: sign()})
	})
}
