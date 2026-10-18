package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	hostbridge "agent-nexus-cli/internal/bridge"
	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/integrations"
)

type runtimeWakeReceipt struct {
	State    string `json:"state"`
	Instance string `json:"instance"`
	Wake     string `json:"wake"`
}

func (a *App) deliverRuntimeWake(ctx context.Context, cfg config.Resolved, dir string, r hostbridge.Runtime, item map[string]any, workers chan struct{}, timeout time.Duration) error {
	wake := anyString(item["wakeup_id"])
	if wake == "" {
		return fmt.Errorf("missing wake identity")
	}
	host, e := a.resolvedHost(cfg)
	if e != nil {
		return e
	}
	instance := "runtime-" + host.ID + "-" + filepath.Base(dir)
	status := anyString(item["delivery_status"])
	if status != "requested" && status != "claimed" {
		return nil
	}
	path := filepath.Join(dir, "runtime-receipts", integrations.Digest(cfg.BaseURL+"\x00"+cfg.ActorID+"\x00"+wake)+".json")
	receipt := runtimeWakeReceipt{Instance: instance, Wake: wake}
	raw, e := integrations.PrivateRead(path)
	if e == nil {
		if e = json.Unmarshal(raw, &receipt); e != nil {
			return e
		}
		if receipt.Wake != wake || receipt.Instance == "" {
			return integrations.ErrIdentityChanged
		}
	} else if !os.IsNotExist(e) {
		return e
	}
	if receipt.State == "executing" || receipt.State == "uncertain" {
		return fmt.Errorf("runtime outcome uncertain; inspect retained runtime receipt")
	}
	instance = receipt.Instance
	if status == "claimed" && anyString(item["bridge_instance_id"]) != instance {
		receipt.Instance = anyString(item["bridge_instance_id"])
		receipt.State = "uncertain"
		if e = integrations.PrivateWrite(path, integrations.JSON(receipt)); e != nil {
			return e
		}
		return fmt.Errorf("retained wake claim belongs to an earlier consumer; reconcile before enabling runtime")
	}
	body := map[string]any{"wakeup_id": wake, "bridge_instance_id": instance}
	if _, e = a.hostBridgeCall(ctx, cfg, host.ID, "wakeup-claim", "/agent-wakeups/claim", body); e != nil {
		return e
	}
	if receipt.State == "" {
		if anyString(item["target_actor_id"]) != cfg.ActorID {
			return integrations.ErrIdentityChanged
		}
		packet, e := a.invokeRawJSON(ctx, cfg, "bridge packet", "GET", "/artifacts/"+url.PathEscape(wake)+"/content", nil)
		if e != nil {
			return e
		}
		p := commandResultBody(packet)
		prompt := fmt.Sprintf("Agent Nexus wake for %s\nSubject: %s\nThread: %s\nTrigger: %s\nUse anx to read context and post your response.\n", cfg.As, firstNonEmpty(anyString(p["subject_ref"]), anyString(asMap(p["resolved_subject"])["ref"])), anyString(item["thread_id"]), firstNonEmpty(anyString(asMap(p["trigger"])["text"]), anyString(item["trigger_text"])))
		if workers != nil {
			select {
			case workers <- struct{}{}:
				defer func() { <-workers }()
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		receipt.State = "executing"
		if e = integrations.PrivateWrite(path, integrations.JSON(receipt)); e != nil {
			return e
		}
		runCtx, done := context.WithTimeout(ctx, timeout)
		e = a.launchBridgeRuntime(runCtx, cfg, r, prompt, p)
		interrupted := runCtx.Err() != nil
		done()
		if interrupted {
			return fmt.Errorf("runtime execution interrupted; reconcile retained receipt")
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if e != nil {
			receipt.State = "failed"
		} else {
			receipt.State = "delivered"
		}
		if e = integrations.PrivateWrite(path, integrations.JSON(receipt)); e != nil {
			return e
		}
	}
	action := "complete"
	if receipt.State == "failed" {
		action = "fail"
		body["error"] = "runtime_failed"
	}
	if receipt.State == "acknowledged" {
		return nil
	}
	if _, e = a.hostBridgeCall(ctx, cfg, host.ID, "wakeup-"+action, "/agent-wakeups/"+action, body); e != nil {
		return e
	}
	receipt.State = "acknowledged"
	return integrations.PrivateWrite(path, integrations.JSON(receipt))
}
func (a *App) launchBridgeRuntime(ctx context.Context, cfg config.Resolved, r hostbridge.Runtime, prompt string, packet map[string]any) error {
	env := []string{}
	for _, v := range integrations.GitEnvironment(os.Environ()) {
		k, _, _ := strings.Cut(v, "=")
		if !strings.HasPrefix(k, "AGENTCTL_") && !strings.HasPrefix(k, "ANX_") {
			env = append(env, v)
		}
	}
	for k, v := range r.Env {
		if strings.HasPrefix(k, "ANX_") || strings.HasPrefix(k, "AGENTCTL_") {
			return fmt.Errorf("runtime environment cannot override identity")
		}
		env = append(env, k+"="+v)
	}
	env = append(env, "ANX_AS="+cfg.As, "ANX_BASE_URL="+cfg.BaseURL, "ANX_CONFIG_DIR="+cfg.ConfigDir)
	argv := r.Argv
	run := func(argv []string, input string, capture bool) ([]byte, error) {
		if len(argv) == 0 {
			return nil, fmt.Errorf("empty runtime argv")
		}
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		configureAnswerCommand(cmd)
		cmd.Dir = r.CWD
		cmd.Env = integrations.GitEnvironment(env)
		cmd.Stdin = strings.NewReader(input)
		cmd.Stderr = io.Discard
		var out bridgeControlOutput
		if capture {
			cmd.Stdout = &out
		} else {
			cmd.Stdout = io.Discard
		}
		e := cmd.Run()
		if out.Len() > 64<<10 {
			return nil, fmt.Errorf("runtime control output exceeds bound")
		}
		return out.Bytes(), e
	}
	if r.Agentctl != "" {
		bin, e := exec.LookPath(r.Agentctl)
		if e != nil {
			return e
		}
		raw, e := run([]string{bin, "id", "generate", "exec"}, "", true)
		if e != nil {
			return e
		}
		var doc map[string]any
		if e = json.Unmarshal(raw, &doc); e != nil {
			return e
		}
		id := anyString(asMap(doc["result"])["id"])
		if id == "" {
			return fmt.Errorf("missing execution ID")
		}
		exe, e := os.Executable()
		if e != nil {
			return e
		}
		sub := []string{bin, "subscribe", "create", "--execution", id, "--destination", "command", "--target", exe}
		for _, arg := range []string{"--as", cfg.As, "--config-dir", cfg.ConfigDir, "--base-url", cfg.BaseURL, "runs", "ingest"} {
			sub = append(sub, "--arg", arg)
		}
		if _, e = run(sub, "", false); e != nil {
			return e
		}
		command := []string{bin, "run", "--execution-id", id, "--adapter", firstNonEmpty(r.Adapter, "generic")}
		ref := firstNonEmpty(anyString(packet["subject_ref"]), anyString(asMap(packet["resolved_subject"])["ref"]))
		if strings.HasPrefix(ref, "card:") {
			command = append(command, "--label", "anx.card."+strings.TrimPrefix(ref, "card:"))
		}
		command = append(command, "--prompt-stdin", "--prompt-delivery", "stdin", "--")
		argv = append(command, argv...)
	}
	_, e := run(argv, prompt, false)
	return e
}

// Agentctl control replies have a hard output bound while they are read, not
// after an untrusted child has already filled memory.
type bridgeControlOutput struct{ buffer bytes.Buffer }

func (b *bridgeControlOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 64<<10 {
		return 0, fmt.Errorf("runtime control output exceeds bound")
	}
	return b.buffer.Write(p)
}

func (b *bridgeControlOutput) Len() int      { return b.buffer.Len() }
func (b *bridgeControlOutput) Bytes() []byte { return b.buffer.Bytes() }

// A killed/timeout child may have committed external effects. Keep this lane
// visibly unready until the owner reconciles its outcome; other jobs keep running.
func checkRuntimeReceipts(dir string) error {
	_, err := inspectRuntimeReceipts(dir, false)
	return err
}

func inspectRuntimeReceipts(dir string, live bool) (inProgress bool, err error) {
	dir = filepath.Join(dir, "runtime-receipts")
	files, e := os.ReadDir(dir)
	if os.IsNotExist(e) {
		return inProgress, nil
	}
	if e != nil {
		return false, e
	}
	if len(files) > 10000 {
		return false, fmt.Errorf("runtime receipt capacity exceeded")
	}
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".json") {
			continue
		}
		raw, e := integrations.PrivateRead(filepath.Join(dir, f.Name()))
		if e != nil {
			return false, e
		}
		var r runtimeWakeReceipt
		if e = json.Unmarshal(raw, &r); e != nil {
			return false, e
		}
		switch r.State {
		case "delivered", "failed", "acknowledged":
		case "executing":
			if live {
				inProgress = true
				continue
			}
			return false, fmt.Errorf("runtime outcome uncertain; reconcile retained runtime receipt")
		case "uncertain":
			return false, fmt.Errorf("runtime outcome uncertain; reconcile retained runtime receipt")
		default:
			return false, fmt.Errorf("unrecognized runtime receipt state")
		}
	}
	return inProgress, nil
}
