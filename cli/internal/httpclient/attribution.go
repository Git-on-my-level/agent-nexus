package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// Run attribution never blocks a write.
//
// Inside an agentctl run the CLI tags writes with X-ANX-Run-Id. Core refuses
// the header (400 run_attribution_invalid) before handling the request when it
// cannot attribute the write: the run was first used by a different agent (a
// delegated agent or subagent inheriting AGENTCTL_EXECUTION_ID but writing as
// another --as), the identity is not a host-derived agent, or the header is
// malformed. Delegated agents could then write nothing. Because the refusal
// happens before any mutation, the transport resends the same request once
// without the header, and reports the skipped attribution to the observer so
// the command can warn with the reason and the fix.

const runHeader = "X-ANX-Run-Id"

// maxAttributionReplayBody bounds what is buffered to make the resend
// possible; larger bodies keep the refusal.
const maxAttributionReplayBody = 4 << 20

type attributionObserverKey struct{}

// WithRunAttributionObserver receives details for each write whose run
// attribution core refused and the CLI resent unattributed.
func WithRunAttributionObserver(ctx context.Context, observe func(map[string]any)) context.Context {
	return context.WithValue(ctx, attributionObserverKey{}, observe)
}

type attributionFallback struct{ base http.RoundTripper }

func (t attributionFallback) RoundTrip(req *http.Request) (*http.Response, error) {
	run := req.Header.Get(runHeader)
	if run == "" || req.Method == http.MethodGet || req.Method == http.MethodHead {
		return t.base.RoundTrip(req)
	}
	var payload []byte
	if req.Body != nil && req.Body != http.NoBody {
		if req.ContentLength < 0 || req.ContentLength > maxAttributionReplayBody {
			return t.base.RoundTrip(req)
		}
		var err error
		payload, err = io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, err
		}
	}
	send := func(withRun bool) (*http.Response, error) {
		clone := req.Clone(req.Context())
		if !withRun {
			clone.Header.Del(runHeader)
		}
		if payload != nil {
			clone.Body = io.NopCloser(bytes.NewReader(payload))
			clone.ContentLength = int64(len(payload))
		}
		return t.base.RoundTrip(clone)
	}
	resp, err := send(true)
	if err != nil || resp.StatusCode != http.StatusBadRequest {
		return resp, err
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	if readErr != nil {
		return resp, nil
	}
	var refusal struct {
		Error struct {
			Code    string         `json:"code"`
			Message string         `json:"message"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &refusal) != nil || refusal.Error.Code != "run_attribution_invalid" {
		return resp, nil
	}
	retried, err := send(false)
	if err != nil {
		return nil, err
	}
	// Only a resend the server accepted wrote anything unattributed; a failed
	// resend reports its own error and must not claim a write was saved.
	if retried.StatusCode >= 300 {
		return retried, nil
	}
	if observe, ok := req.Context().Value(attributionObserverKey{}).(func(map[string]any)); ok && observe != nil {
		details := map[string]any{"run": run, "message": strings.TrimSpace(refusal.Error.Message)}
		for key, value := range refusal.Error.Details {
			if _, taken := details[key]; !taken {
				details[key] = value
			}
		}
		observe(details)
	}
	return retried, nil
}
