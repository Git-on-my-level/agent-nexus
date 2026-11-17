package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/httpclient"
	"agent-nexus-cli/internal/streaming"
)

func (a *App) runAwaitAsk(ctx context.Context, cfg config.Resolved, target string, timeout time.Duration) (*commandResult, error) {
	// The tail of the await budget is reserved for the durable decision read,
	// so a starved stream can never push the whole command past --timeout.
	reserve := 2 * time.Second
	if timeout < 2*reserve {
		reserve = timeout / 2
	}
	deadline := time.Now().Add(timeout)
	awaitCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	streamCtx, streamCancel := context.WithDeadline(ctx, deadline.Add(-reserve))
	defer streamCancel()
	// Terminal state is durable. Read it before creating a write-side subscription
	// or competing for SSE admission, especially when writers are contending.
	askPath := "/asks/" + url.PathEscape(strings.TrimPrefix(target, "event:"))
	current, err := retryTransientRead(awaitCtx, func() (*commandResult, error) {
		return a.invokeRawJSON(awaitCtx, cfg, "await outcome", http.MethodGet, askPath, nil)
	})
	if err != nil {
		if errors.Is(awaitCtx.Err(), context.DeadlineExceeded) {
			return nil, errnorm.New(errnorm.KindNetwork, "timeout", "await timed out")
		}
		return nil, err
	}
	if out := commandResultBody(current); anyString(out["status"]) != "open" {
		return awaitAskOutcomeResult(target, out)
	}
	subscription := ""
	// Subscribing is idempotent per await label, so it may be repeated through an outage.
	sub, err := retryTransientRead(awaitCtx, func() (*commandResult, error) {
		return a.invokeRawJSON(awaitCtx, cfg, "await subscribe", "POST", "/asks/"+url.PathEscape(strings.TrimPrefix(target, "event:"))+"/subscriptions", map[string]any{"kind": "await", "label": "live await"})
	})
	if err != nil {
		if errors.Is(awaitCtx.Err(), context.DeadlineExceeded) {
			return nil, errnorm.New(errnorm.KindNetwork, "timeout", "await timed out")
		}
		return nil, err
	}
	subscription = anyString(commandResultBody(sub)["id"])
	backoff := 250 * time.Millisecond
	for streamCtx.Err() == nil {
		// Token refresh stays inside the await deadline; a hung refresh must
		// not outlive --timeout either.
		auth, err := a.cfgWithResolvedAuthToken(awaitCtx, cfg)
		if err != nil {
			return nil, err
		}
		cfg = auth
		auth.Timeout = 0
		client, err := httpclient.New(auth)
		if err != nil {
			return nil, err
		}
		resp, err := client.OpenStream(streamCtx, httpclient.RawRequest{Method: http.MethodGet, Path: "/stream/asks/" + url.PathEscape(strings.TrimPrefix(target, "event:")), Headers: map[string]string{"Accept": "text/event-stream"}})
		if err == nil && transientStreamStatus(resp.StatusCode) {
			resp.Body.Close()
			err = errTransientStream
		}
		if err == nil {
			if resp.StatusCode >= 400 {
				b, _ := io.ReadAll(io.LimitReader(resp.Body, 65536))
				resp.Body.Close()
				return nil, errnorm.FromHTTPFailure(resp.StatusCode, b)
			}
			reader := bufio.NewReader(resp.Body)
			for streamCtx.Err() == nil {
				ev, e := streaming.ReadEvent(reader)
				if e != nil {
					break
				}
				if ev.Type == "error" {
					resp.Body.Close()
					return nil, errnorm.New(errnorm.KindRemote, "not_found", "ask is no longer accessible")
				}
				if ev.Type != "outcome" {
					continue
				}
				var out map[string]any
				if json.Unmarshal([]byte(ev.Data), &out) != nil {
					continue
				}
				status := anyString(out["status"])
				if status == "open" {
					continue
				}
				resp.Body.Close()
				// Reversible curation is visible to status readers, but owns no
				// answer delivery receipt or continuation acknowledgement.
				if status != "withdrawn_as_stale" {
					_, ackErr := a.invokeRawJSON(awaitCtx, cfg, "await delivery", "POST", "/asks/"+url.PathEscape(strings.TrimPrefix(target, "event:"))+"/delivery", map[string]any{"subscription_id": subscription, "state": "delivered", "attempts": 1})
					if ackErr != nil {
						out["delivery_acknowledged"] = false
					} else {
						out["delivery_acknowledged"] = true
					}
				}
				return awaitAskOutcomeResult(target, out)
			}
			resp.Body.Close()
		}
		timer := time.NewTimer(backoff)
		select {
		case <-streamCtx.Done():
			timer.Stop()
		case <-timer.C:
		}
		if backoff < 4*time.Second {
			backoff *= 2
		}
	}
	// The stream can starve under writer contention even after the answer is
	// durable. The reserved tail buys one durable read that decides between
	// the answer and the timeout; a read failure surfaces as its real error
	// instead of a timeout.
	finalCtx, finalCancel := context.WithDeadline(ctx, deadline)
	defer finalCancel()
	current, err = a.invokeRawJSON(finalCtx, cfg, "await outcome", http.MethodGet, askPath, nil)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(finalCtx.Err(), context.DeadlineExceeded) {
			return nil, errnorm.New(errnorm.KindNetwork, "timeout", "await timed out")
		}
		return nil, err
	}
	if out := commandResultBody(current); anyString(out["status"]) != "open" {
		return awaitAskOutcomeResult(target, out)
	}
	return nil, errnorm.New(errnorm.KindNetwork, "timeout", "await timed out")
}

func awaitAskOutcomeResult(target string, out map[string]any) (*commandResult, error) {
	switch anyString(out["status"]) {
	case "answered":
		response := asMap(out["response"])
		out["answer"] = response["response_text"]
		out["outcome"] = response["outcome"]
		out["responder"] = response["responding_actor_id"]
		if response["outcome"] == "rejected" {
			message := "ask rejected"
			if strings.HasPrefix(target, "access-request:") || anyString(out["access_request_ref"]) != "" {
				message = "access request denied"
			}
			return nil, errnorm.WithDetails(errnorm.New(errnorm.KindRemote, "rejected", message), out)
		}
		return &commandResult{Data: out}, nil
	case "needs_context", "withdrawn", "withdrawn_as_stale", "expired":
		return nil, errnorm.WithDetails(errnorm.New(errnorm.KindRemote, anyString(out["status"]), "ask ended: "+anyString(out["status"])), out)
	default:
		return nil, errnorm.New(errnorm.KindRemote, "invalid_response_outcome", "unknown ask state")
	}
}

// errTransientStream marks a stream open that hit a restart or overload; the
// await loops back off and reconnect instead of ending a long wait.
var errTransientStream = errors.New("transient stream status")

// transientStreamStatus reports statuses that mean "try again later" (429 and
// 5xx), as during a server restart, rather than a final answer about the ask.
func transientStreamStatus(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

// transientAwaitError reports a read that failed for the same reasons, so an
// await keeps waiting instead of exiting 6 mid-wait.
func transientAwaitError(err error) bool {
	var typed *errnorm.Error
	if !errors.As(err, &typed) {
		return false
	}
	if typed.Kind == errnorm.KindNetwork {
		// TLS and cancellation are marked unrecoverable; surface them now.
		return typed.Recoverable == nil || *typed.Recoverable
	}
	details, _ := typed.Details.(map[string]any)
	status, _ := details["status"].(int)
	return transientStreamStatus(status)
}

// retryTransientRead repeats an idempotent read through outages (429/5xx,
// recoverable network errors) with bounded backoff until ctx ends, so an
// await started while the server restarts waits instead of exiting.
func retryTransientRead[T any](ctx context.Context, read func() (T, error)) (T, error) {
	backoff := 250 * time.Millisecond
	for {
		out, err := read()
		if err == nil || !transientAwaitError(err) || ctx.Err() != nil {
			return out, err
		}
		select {
		case <-ctx.Done():
			return out, err
		case <-time.After(backoff):
		}
		if backoff < 4*time.Second {
			backoff *= 2
		}
	}
}
