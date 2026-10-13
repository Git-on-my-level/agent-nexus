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
	awaitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// Terminal state is durable. Read it before creating a write-side subscription
	// or competing for SSE admission, especially when writers are contending.
	askPath := "/asks/" + url.PathEscape(strings.TrimPrefix(target, "event:"))
	current, err := a.invokeRawJSON(awaitCtx, cfg, "await outcome", http.MethodGet, askPath, nil)
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
	sub, err := a.invokeRawJSON(awaitCtx, cfg, "await subscribe", "POST", "/asks/"+url.PathEscape(strings.TrimPrefix(target, "event:"))+"/subscriptions", map[string]any{"kind": "await", "label": "live await"})
	if err != nil {
		if errors.Is(awaitCtx.Err(), context.DeadlineExceeded) {
			return nil, errnorm.New(errnorm.KindNetwork, "timeout", "await timed out")
		}
		return nil, err
	}
	subscription = anyString(commandResultBody(sub)["id"])
	backoff := 250 * time.Millisecond
	for awaitCtx.Err() == nil {
		// Token refresh stays inside the await budget; the parent ctx is kept
		// only for the final durable read after the budget ends.
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
		resp, err := client.OpenStream(awaitCtx, httpclient.RawRequest{Method: http.MethodGet, Path: "/stream/asks/" + url.PathEscape(strings.TrimPrefix(target, "event:")), Headers: map[string]string{"Accept": "text/event-stream"}})
		if err == nil {
			if resp.StatusCode >= 400 {
				b, _ := io.ReadAll(io.LimitReader(resp.Body, 65536))
				resp.Body.Close()
				return nil, errnorm.FromHTTPFailure(resp.StatusCode, b)
			}
			reader := bufio.NewReader(resp.Body)
			for awaitCtx.Err() == nil {
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
				_, ackErr := a.invokeRawJSON(awaitCtx, cfg, "await delivery", "POST", "/asks/"+url.PathEscape(strings.TrimPrefix(target, "event:"))+"/delivery", map[string]any{"subscription_id": subscription, "state": "delivered", "attempts": 1})
				if ackErr != nil {
					out["delivery_acknowledged"] = false
				} else {
					out["delivery_acknowledged"] = true
				}
				return awaitAskOutcomeResult(target, out)
			}
			resp.Body.Close()
		}
		timer := time.NewTimer(backoff)
		select {
		case <-awaitCtx.Done():
			timer.Stop()
		case <-timer.C:
		}
		if backoff < 4*time.Second {
			backoff *= 2
		}
	}
	// The stream can starve under writer contention even after the answer is
	// durable. One final bounded read decides between the answer and the
	// timeout instead of reporting a timeout over an answer that already exists.
	finalRead := 2 * time.Second
	if timeout < 2*finalRead {
		finalRead = timeout / 2
	}
	finalCtx, finalCancel := context.WithTimeout(ctx, finalRead)
	defer finalCancel()
	if current, err := a.invokeRawJSON(finalCtx, cfg, "await outcome", http.MethodGet, askPath, nil); err == nil {
		if out := commandResultBody(current); anyString(out["status"]) != "open" {
			return awaitAskOutcomeResult(target, out)
		}
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
	case "needs_context", "withdrawn", "expired":
		return nil, errnorm.WithDetails(errnorm.New(errnorm.KindRemote, anyString(out["status"]), "ask ended: "+anyString(out["status"])), out)
	default:
		return nil, errnorm.New(errnorm.KindRemote, "invalid_response_outcome", "unknown ask state")
	}
}
