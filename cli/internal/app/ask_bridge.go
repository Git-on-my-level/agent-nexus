package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	hostbridge "agent-nexus-cli/internal/bridge"
	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/httpclient"
	"agent-nexus-cli/internal/integrations"
	"agent-nexus-cli/internal/streaming"
)

func (a *App) consumeAskWakes(ctx context.Context, cfg config.Resolved, dir string, maxAttempts int, timeout time.Duration) error {
	return a.consumeRegisteredWakes(ctx, cfg, []string{dir}, maxAttempts, timeout, true, nil, nil)
}
func (a *App) consumeHostWakes(ctx context.Context, cfg config.Resolved, agent hostbridge.Agent, executors chan struct{}) error {
	runtime := agent.Runtime
	if !agent.Jobs["wake"] {
		runtime = nil
	}
	return a.consumeRegisteredWakes(ctx, cfg, agent.RegistryDirs, agent.Policy.MaxAttempts, agent.Policy.Timeout, agent.Jobs["answers"], runtime, executors)
}
func resumePaths(dirs []string) ([]string, error) {
	paths := []string{}
	for _, dir := range dirs {
		files, e := os.ReadDir(dir)
		if e != nil {
			return nil, e
		}
		if len(files) > 10000 {
			return nil, fmt.Errorf("resume registry capacity exceeded")
		}
		for _, f := range files {
			if strings.HasSuffix(f.Name(), ".json") {
				paths = append(paths, filepath.Join(dir, f.Name()))
			}
		}
	}
	return paths, nil
}
func (a *App) consumeRegisteredWakes(ctx context.Context, cfg config.Resolved, dirs []string, maxAttempts int, timeout time.Duration, answers bool, runtime *hostbridge.Runtime, executors chan struct{}) error {
	if len(dirs) == 0 {
		return fmt.Errorf("missing registered namespace")
	}
	dir := dirs[0]

	files, err := resumePaths(dirs)
	if err != nil {
		return err
	}
	for _, path := range files {
		if !answers {
			break
		}
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
			if runtime != nil {
				if err = a.deliverRuntimeWake(ctx, cfg, dir, *runtime, receipt, executors, timeout); err != nil {
					return err
				}
			}
			continue
		}
		if !answers {
			continue
		}
		status := anyString(receipt["delivery_status"])
		if status != "requested" && status != "claimed" {
			continue
		}
		refs := map[string]bool{}
		for _, ref := range asSlice(receipt["related_refs"]) {
			refs[anyString(ref)] = true
		}
		files, err := resumePaths(dirs)
		if err != nil {
			return err
		}
		entries := map[string]askResumeRegistration{}
		for _, path := range files {
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
		originalInstance := ""
		for _, entry := range entries {
			if entry.Wakeup == wake && entry.Instance != "" {
				if originalInstance != "" && originalInstance != entry.Instance {
					return fmt.Errorf("conflicting retained wake claims")
				}
				originalInstance = entry.Instance
			}
		}
		if originalInstance != "" {
			instance = originalInstance
		}
		if status == "claimed" && anyString(receipt["bridge_instance_id"]) != instance {
			continue
		}
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
				entry.State = "executing"
				entry.Instance = instance
				if e = writeAskRegistration(path, entry); e != nil {
					return e
				}
				if executors != nil {
					select {
					case executors <- struct{}{}:
					case <-ctx.Done():
						return ctx.Err()
					}
				}
				runCtx, cancel := context.WithTimeout(ctx, timeout)
				callbackCfg := cfg
				callbackCfg.ConfigDir = filepath.Dir(filepath.Dir(filepath.Dir(path)))
				e = runRegisteredAnswerFor(runCtx, entry, raw, out, callbackCfg)
				timedOut := runCtx.Err() != nil
				cancel()
				if executors != nil {
					<-executors
				}
				if timedOut {
					entry.State = "uncertain"
					if saveErr := writeAskRegistration(path, entry); saveErr != nil {
						return saveErr
					}
					return fmt.Errorf("resume execution interrupted; reconcile retained outcome")
				}
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
	return runRegisteredAnswerFor(ctx, entry, raw, out, config.Resolved{})
}
func runRegisteredAnswerFor(ctx context.Context, entry askResumeRegistration, raw []byte, out map[string]any, cfg config.Resolved) error {
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", entry.Command)
	configureAnswerCommand(cmd)
	cmd.Dir = entry.Dir
	cmd.Stdin = strings.NewReader(string(raw))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	env := []string{}
	for _, v := range integrations.GitEnvironment(os.Environ()) {
		key, _, _ := strings.Cut(v, "=")
		if !strings.HasPrefix(key, "ANX_") && !strings.HasPrefix(key, "AGENTCTL_") {
			env = append(env, v)
		}
	}
	if cfg.BaseURL != "" {
		env = append(env, "ANX_BASE_URL="+cfg.BaseURL, "ANX_AS="+cfg.As, "ANX_CONFIG_DIR="+cfg.ConfigDir)
	}
	response := asMap(out["response"])
	cmd.Env = append(env, "ANX_ASK_ID="+entry.AskID, "ANX_CARD_REF="+anyString(out["subject_ref"]), "ANX_OUTCOME="+anyString(response["outcome"]), "ANX_RESPONSE_EVENT_ID="+anyString(response["response_event_id"]))
	return cmd.Run()
}
