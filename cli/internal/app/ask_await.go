package app

import (
	"bufio"
	"context"
	"encoding/json"
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
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	subscription := ""
	sub, err := a.invokeRawJSON(ctx, cfg, "await subscribe", "POST", "/asks/"+url.PathEscape(strings.TrimPrefix(target, "event:"))+"/subscriptions", map[string]any{"kind": "await", "label": "live await"})
	if err != nil {
		return nil, err
	}
	subscription = anyString(commandResultBody(sub)["id"])
	backoff := 250 * time.Millisecond
	for ctx.Err() == nil {
		auth, err := a.cfgWithResolvedAuthToken(ctx, cfg)
		if err != nil {
			return nil, err
		}
		cfg = auth
		auth.Timeout = 0
		client, err := httpclient.New(auth)
		if err != nil {
			return nil, err
		}
		resp, err := client.OpenStream(ctx, httpclient.RawRequest{Method: http.MethodGet, Path: "/stream/asks/" + url.PathEscape(strings.TrimPrefix(target, "event:")), Headers: map[string]string{"Accept": "text/event-stream"}})
		if err == nil {
			if resp.StatusCode >= 400 {
				b, _ := io.ReadAll(io.LimitReader(resp.Body, 65536))
				resp.Body.Close()
				return nil, errnorm.FromHTTPFailure(resp.StatusCode, b)
			}
			reader := bufio.NewReader(resp.Body)
			for ctx.Err() == nil {
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
				_, ackErr := a.invokeRawJSON(ctx, cfg, "await delivery", "POST", "/asks/"+url.PathEscape(strings.TrimPrefix(target, "event:"))+"/delivery", map[string]any{"subscription_id": subscription, "state": "delivered", "attempts": 1})
				if ackErr != nil {
					out["delivery_acknowledged"] = false
				} else {
					out["delivery_acknowledged"] = true
				}
				switch status {
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
					return nil, errnorm.WithDetails(errnorm.New(errnorm.KindRemote, status, "ask ended: "+status), out)
				default:
					return nil, errnorm.New(errnorm.KindRemote, "invalid_response_outcome", "unknown ask state")
				}
			}
			resp.Body.Close()
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
		if backoff < 4*time.Second {
			backoff *= 2
		}
	}
	return nil, errnorm.New(errnorm.KindNetwork, "timeout", "await timed out")
}
