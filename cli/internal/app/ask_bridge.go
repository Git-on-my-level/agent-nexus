package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/httpclient"
	"agent-nexus-cli/internal/streaming"
)

func (a *App) runAskBridge(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("bridge run")
	maxAttempts := fs.Int("max-attempts", 5, "Maximum command attempts (1..20)")
	timeout := fs.Duration("command-timeout", 30*time.Minute, "Maximum resume command duration")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) != 0 || *maxAttempts < 1 || *maxAttempts > 20 || *timeout <= 0 {
		return nil, errnorm.Usage("invalid_args", "bridge run accepts --max-attempts 1..20 and a positive --command-timeout")
	}
	cfg, err := a.resolveHostAgent(ctx, cfg)
	if err != nil {
		return nil, err
	}
	dir, err := a.askRegistryDir(cfg)
	if err != nil {
		return nil, err
	}
	unlock, err := lockAskBridge(filepath.Join(dir, "bridge.lock"))
	if err != nil {
		return nil, err
	}
	defer unlock()
	backoff := time.Second
	for ctx.Err() == nil {
		err = a.consumeAskWakes(ctx, cfg, dir, *maxAttempts, *timeout)
		if ctx.Err() != nil {
			break
		}
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
		if errors.Is(err, context.DeadlineExceeded) {
			backoff = time.Second
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
	return &commandResult{Data: map[string]any{"stopped": true}}, nil
}
func (a *App) consumeAskWakes(ctx context.Context, cfg config.Resolved, dir string, maxAttempts int, timeout time.Duration) error {
	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, file.Name())
		entry, e := readAskRegistration(path)
		if e != nil {
			return e
		}
		if entry.Subscription == "" && entry.State != "registration_failed" && entry.BaseURL == cfg.BaseURL && entry.ActorID == cfg.ActorID {
			sub, e := a.invokeRawJSON(ctx, cfg, "bridge subscribe", "POST", "/asks/"+url.PathEscape(strings.TrimPrefix(entry.AskID, "event:"))+"/subscriptions", map[string]any{"kind": "bridge", "label": "host resume"})
			if e != nil {
				// A rejected/never-published ask cannot poison other local registrations.
				// Keep the file for inspection; retryable publication ambiguity remains pending.
				if typed, ok := e.(*errnorm.Error); ok && (typed.Code == "not_found" || typed.Code == "forbidden") {
					entry.State = "registration_failed"
					_ = writeAskRegistration(path, entry)
				}
				continue
			}
			entry.Subscription = anyString(commandResultBody(sub)["id"])
			if e = writeAskRegistration(path, entry); e != nil {
				return e
			}
		}
	}
	auth, err := a.cfgWithResolvedAuthToken(ctx, cfg)
	if err != nil {
		return err
	}
	auth.Timeout = 0
	client, err := httpclient.New(auth)
	if err != nil {
		return err
	}
	cursor, _ := os.ReadFile(filepath.Join(dir, "cursor"))
	streamCtx, stopStream := context.WithTimeout(ctx, 30*time.Second)
	defer stopStream()
	resp, err := client.OpenStream(streamCtx, httpclient.RawRequest{Method: http.MethodGet, Path: "/stream/agent-wakeups", Headers: map[string]string{"Accept": "text/event-stream", "Last-Event-ID": strings.TrimSpace(string(cursor))}})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("wake stream status %d", resp.StatusCode)
	}
	reader := bufio.NewReader(resp.Body)
	nextCursor := ""
	for ctx.Err() == nil {
		if nextCursor != "" {
			if err = os.WriteFile(filepath.Join(dir, "cursor"), []byte(nextCursor), 0600); err != nil {
				return err
			}
		}
		ev, err := streaming.ReadEvent(reader)
		if err != nil {
			return err
		}
		if ev.ID != "" {
			nextCursor = ev.ID
		}
		if ev.Type != "notification_receipt" {
			continue
		}
		var data map[string]any
		if json.Unmarshal([]byte(ev.Data), &data) != nil {
			continue
		}
		receipt := asMap(data["receipt"])
		if anyString(receipt["target_actor_id"]) != cfg.ActorID {
			continue
		}
		host, err := a.resolvedHost(cfg)
		if err != nil {
			return err
		}
		instance := "answer-" + host.ID + "-" + filepath.Base(dir)
		if !strings.HasPrefix(anyString(receipt["wakeup_id"]), "ask-delivery-") {
			continue
		}
		status := anyString(receipt["delivery_status"])
		if status != "requested" && !(status == "claimed" && anyString(receipt["bridge_instance_id"]) == instance) {
			continue
		}
		refs := map[string]bool{}
		for _, ref := range asSlice(receipt["related_refs"]) {
			refs[anyString(ref)] = true
		}
		files, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		entries := map[string]askResumeRegistration{}
		for _, file := range files {
			if !strings.HasSuffix(file.Name(), ".json") {
				continue
			}
			path := filepath.Join(dir, file.Name())
			entry, e := readAskRegistration(path)
			if e != nil {
				return e
			}
			if entry.BaseURL == cfg.BaseURL && entry.ActorID == cfg.ActorID && !entry.Acknowledged && (refs[entry.AskID] || refs[entry.AskRef]) {
				entries[path] = entry
			}
		}
		if len(entries) == 0 {
			continue
		}
		wake := anyString(receipt["wakeup_id"])
		body := map[string]any{"wakeup_id": wake, "bridge_instance_id": instance}
		if _, err = a.hostBridgeCall(ctx, cfg, host.ID, "wakeup-claim", "/agent-wakeups/claim", body); err != nil {
			return err
		}
		failed := false
		for path, entry := range entries {
			result, e := a.invokeRawJSON(ctx, cfg, "bridge outcome", "GET", "/asks/"+url.PathEscape(strings.TrimPrefix(entry.AskID, "event:")), nil)
			if e != nil {
				return e
			}
			out := commandResultBody(result)
			if anyString(out["status"]) == "open" {
				return fmt.Errorf("wake arrived before terminal ask outcome")
			}
			raw, e := json.Marshal(out)
			if e != nil {
				return e
			}
			if entry.Subscription == "" {
				sub, e := a.invokeRawJSON(ctx, cfg, "bridge subscribe", "POST", "/asks/"+url.PathEscape(strings.TrimPrefix(entry.AskID, "event:"))+"/subscriptions", map[string]any{"kind": "bridge", "label": "host resume"})
				if e != nil {
					return e
				}
				entry.Subscription = anyString(commandResultBody(sub)["id"])
			}
			entry.Wakeup = wake
			for entry.State != "delivered" && entry.State != "failed" && entry.Attempts < maxAttempts {
				entry.Attempts++
				entry.State = "pending"
				if e = writeAskRegistration(path, entry); e != nil {
					return e
				}
				runCtx, cancel := context.WithTimeout(ctx, timeout)
				e = runRegisteredAnswer(runCtx, entry, raw, out)
				cancel()
				if e == nil {
					entry.State = "delivered"
					break
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if entry.Attempts < maxAttempts {
					timer := time.NewTimer(time.Duration(1<<uint(entry.Attempts)) * time.Second)
					select {
					case <-ctx.Done():
						timer.Stop()
						return ctx.Err()
					case <-timer.C:
					}
				}
			}
			reason := ""
			if entry.State != "delivered" {
				entry.State = "failed"
				reason = "resume_command_failed"
				failed = true
			}
			// Completion is durable before publishing either remote receipt. Reconnect
			// retries acknowledgement without invoking a successful local command again.
			if e = writeAskRegistration(path, entry); e != nil {
				return e
			}
			_, e = a.invokeRawJSON(ctx, cfg, "bridge delivery", "POST", "/asks/"+url.PathEscape(strings.TrimPrefix(entry.AskID, "event:"))+"/delivery", map[string]any{"subscription_id": entry.Subscription, "state": entry.State, "attempts": entry.Attempts, "reason": reason})
			if e != nil {
				return e
			}
			entries[path] = entry
		}
		action := "complete"
		if failed {
			action = "fail"
			body["error"] = "resume_command_failed"
		}
		if _, err = a.hostBridgeCall(ctx, cfg, host.ID, "wakeup-"+action, "/agent-wakeups/"+action, body); err != nil {
			return err
		}
		for path, entry := range entries {
			entry.Acknowledged = true
			if err = writeAskRegistration(path, entry); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}
func runRegisteredAnswer(ctx context.Context, entry askResumeRegistration, raw []byte, out map[string]any) error {
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", entry.Command)
	configureAnswerCommand(cmd)
	cmd.Dir = entry.Dir
	cmd.Stdin = strings.NewReader(string(raw))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	env := []string{}
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "ANX_ASK_ID=") && !strings.HasPrefix(e, "ANX_CARD_REF=") && !strings.HasPrefix(e, "ANX_OUTCOME=") && !strings.HasPrefix(e, "ANX_RESPONSE_EVENT_ID=") {
			env = append(env, e)
		}
	}
	response := asMap(out["response"])
	cmd.Env = append(env, "ANX_ASK_ID="+entry.AskID, "ANX_CARD_REF="+anyString(out["subject_ref"]), "ANX_OUTCOME="+anyString(response["outcome"]), "ANX_RESPONSE_EVENT_ID="+anyString(response["response_event_id"]))
	return cmd.Run()
}
