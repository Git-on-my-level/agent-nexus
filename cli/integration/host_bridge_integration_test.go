//go:build integration && !windows

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	hostbridge "agent-nexus-cli/internal/bridge"
	"agent-nexus-cli/internal/integrations"
)

// Service-manager replacement is exercised separately with fake launchctl and
// systemctl. Here one real CLI process serves the resulting three registrations
// against three fresh local cores. No OS service is installed or inspected.
func TestOneHostBridgeServesThreeWorkspacesAgainstCore(t *testing.T) {
	harnesses := []*liveCoreHarness{}
	roots := []string{}
	for i := 0; i < 3; i++ {
		h := newPasskeyLiveCoreHarness(t)
		h.enrollHost(t, "worker")
		h.registerHumanPasskey(t, "operator", "Operator", h.createHumanInviteToken(t))
		roots = append(roots, filepath.Join(h.homeDir, ".config", "anx"))
		harnesses = append(harnesses, h)
	}
	home := harnesses[0].homeDir
	receipts := []string{}
	asks := []string{}
	hostIDs := []string{}
	for i, h := range harnesses {
		h.homeDir = home
		registration := h.runCLIExpectOK(t, "worker", nil, "--config-dir", roots[i], "bridge", "add", "--as", "worker")
		hostIDs = append(hostIDs, mustStringPath(t, registration.Payload, "result.host_id"))
		work := h.runCLIExpectOK(t, "worker", map[string]any{"title": fmt.Sprintf("Host bridge %d", i), "source": map[string]any{"authority": "nexus"}}, "--config-dir", roots[i], "work", "create", "--from-file", "-")
		ref := mustStringPath(t, work.Payload, "result.work.ref")
		path := filepath.Join(t.TempDir(), "calls")
		receipts = append(receipts, path)
		script := filepath.Join(filepath.Dir(path), "resume.sh")
		source := fmt.Sprintf("#!/bin/sh\nset -eu\nprintf '%%s|%%s|%%s\\n' \"$ANX_BASE_URL\" \"$ANX_AS\" \"$ANX_CONFIG_DIR\" >> %s\ncat >/dev/null\n", integrations.ShellQuote(path))
		if e := os.WriteFile(script, []byte(source), 0700); e != nil {
			t.Fatal(e)
		}
		ask := h.runCLIExpectOK(t, "worker", nil, "--config-dir", roots[i], "ask", fmt.Sprintf("Proceed %d?", i), "--subject-ref", ref, "--recommend", "Proceed", "--on-answer", script)
		asks = append(asks, mustStringPath(t, ask.Payload, "result.ask_id"))
	}
	r, e := hostbridge.Load(hostbridge.Root(home))
	if e != nil || len(r.Workspaces) != 3 {
		t.Fatal(r, e)
	}
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(runCtx, harnesses[0].cliBin, "--json", "bridge", "run")
	cmd.Env = append(integrations.GitEnvironment(os.Environ()), "HOME="+home, "ANX_CONFIG_DIR=", "ANX_AS=", "ANX_BASE_URL=", "ANX_ACCESS_TOKEN=", "ANX_UPDATE_POLICY=off", "ANX_SKILLS_AUTO_SYNC=0", "AGENTCTL_EXECUTION_ID=parent-integration", "AGENTCTL_ADAPTER=generic")
	logPath := filepath.Join(t.TempDir(), "bridge.log")
	log, e := os.Create(logPath)
	if e != nil {
		t.Fatal(e)
	}
	defer log.Close()
	cmd.Stdout = log
	cmd.Stderr = log
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("host process did not stop")
		}
	})
	for i, h := range harnesses {
		h.runCLIExpectOK(t, "operator", map[string]any{"inbox_item_id": findAskInboxID(t, h, asks[i]), "response_text": "Proceed", "outcome": "answered"}, "debug", "inbox", "respond", "--from-file", "-")
	}
	deadline := time.Now().Add(40 * time.Second)
	for {
		complete := true
		for i, path := range receipts {
			raw, e := os.ReadFile(path)
			if os.IsNotExist(e) {
				complete = false
				continue
			}
			if e != nil {
				t.Fatal(e)
			}
			want := harnesses[i].baseURL + "|worker|" + roots[i] + "\n"
			if string(raw) != want {
				t.Fatalf("wrong callback identity or repeated execution: %q want %q", raw, want)
			}
		}
		if complete {
			break
		}
		if time.Now().After(deadline) {
			raw, _ := os.ReadFile(logPath)
			state, _ := os.ReadFile(filepath.Join(hostbridge.Root(home), "status.json"))
			t.Fatal("callbacks not delivered", string(raw), "state", string(state))
		}
		select {
		case e := <-done:
			t.Fatalf("host exited: %v", e)
		case <-time.After(100 * time.Millisecond):
		}
	}
	for i, h := range harnesses {
		status := h.runCLIExpectOK(t, "operator", map[string]any{"host_ids": []string{hostIDs[i]}}, "host", "bridge-status", "--from-file", "-")
		raw := status.Stdout
		if !strings.Contains(raw, "last_seen_at") || !strings.Contains(raw, "answers") {
			t.Fatal("missing signed presence", raw)
		}
		agents := h.runCLIExpectOK(t, "worker", nil, "--config-dir", roots[i], "host", "status")
		if strings.Contains(agents.Stdout, `"bridge_online":true`) || strings.Contains(agents.Stdout, `"bridge_online": true`) {
			t.Fatal("answer consumer implied wake routing", agents.Stdout)
		}
	}
	// SSE replay/reconnect must acknowledge the confirmed command without a repeat.
	time.Sleep(3500 * time.Millisecond)
	for i, path := range receipts {
		raw, e := os.ReadFile(path)
		if e != nil || strings.Count(string(raw), "\n") != 1 {
			t.Fatal("repeated callback", i, string(raw), e)
		}
	}
}

func TestHostBridgeRoutesOrdinaryWakeAgainstCore(t *testing.T) {
	h := newPasskeyLiveCoreHarness(t)
	h.enrollHost(t, "worker")
	h.registerHumanPasskey(t, "operator", "Operator", h.createHumanInviteToken(t))
	root := t.TempDir()
	script := filepath.Join(root, "runtime.sh")
	calls := filepath.Join(root, "calls")
	source := "#!/bin/sh\nset -eu\nprintf '%s|%s\\n' \"$ANX_BASE_URL\" \"$ANX_AS\" >> " + integrations.ShellQuote(calls) + "\ncat > " + integrations.ShellQuote(filepath.Join(root, "prompt")) + "\n"
	if e := os.WriteFile(script, []byte(source), 0700); e != nil {
		t.Fatal(e)
	}
	runtimeFile := filepath.Join(root, "runtime.json")
	raw, e := json.Marshal(hostbridge.Runtime{Argv: []string{script}, CWD: root})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(runtimeFile, raw, 0600); e != nil {
		t.Fatal(e)
	}
	h.runCLIExpectOK(t, "worker", nil, "bridge", "add", "--as", "worker", "--runtime", runtimeFile, "--answers=false")
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cmd := exec.CommandContext(runCtx, h.cliBin, "--json", "bridge", "run")
	cmd.Env = append(integrations.GitEnvironment(os.Environ()), "HOME="+h.homeDir, "ANX_CONFIG_DIR=", "ANX_AS=", "ANX_BASE_URL=", "ANX_ACCESS_TOKEN=", "ANX_UPDATE_POLICY=off", "ANX_SKILLS_AUTO_SYNC=0", "AGENTCTL_EXECUTION_ID=", "AGENTCTL_ADAPTER=")
	logPath := filepath.Join(root, "bridge.log")
	log, e := os.Create(logPath)
	if e != nil {
		t.Fatal(e)
	}
	defer log.Close()
	cmd.Stdout = log
	cmd.Stderr = log
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("foreground bridge did not stop")
		}
	})
	// Once the lane is ready, a registration revision requests an immediate signed
	// snapshot, keeping this fixture independent of the minute heartbeat interval.
	waitFor := func(what string, ready func() bool) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for !ready() {
			if time.Now().After(deadline) {
				raw, _ := os.ReadFile(logPath)
				t.Fatal(what, string(raw))
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	statusPath := filepath.Join(hostbridge.Root(h.homeDir), "status.json")
	waitFor("runtime not locally ready", func() bool {
		raw, _ := os.ReadFile(statusPath)
		var state struct {
			Ready map[string]bool `json:"ready"`
		}
		if json.Unmarshal(raw, &state) != nil {
			return false
		}
		for k, v := range state.Ready {
			if v && strings.HasSuffix(k, "\x00wake") {
				return true
			}
		}
		return false
	})
	h.runCLIExpectOK(t, "worker", nil, "bridge", "add", "--as", "worker")
	waitFor("runtime not advertised", func() bool {
		status := h.runCLI(t, "worker", nil, "host", "status")
		return status.ExitCode == 0 && (strings.Contains(status.Stdout, `"bridge_online":true`) || strings.Contains(status.Stdout, `"bridge_online": true`))
	})
	work := h.runCLIExpectOK(t, "operator", map[string]any{"title": "Ordinary wake", "source": map[string]any{"authority": "nexus"}}, "work", "create", "--from-file", "-")
	ref := mustStringPath(t, work.Payload, "result.work.ref")
	h.runCLIExpectOK(t, "operator", nil, "cards", "message", ref, "--body", "@worker.integration-host please inspect this work", "--actor-id", h.actorID(t, "operator"))
	waitFor("runtime was not called", func() bool { raw, _ := os.ReadFile(calls); return len(raw) > 0 })
	raw, e = os.ReadFile(calls)
	if e != nil || string(raw) != h.baseURL+"|worker\n" {
		t.Fatal(string(raw), e)
	}
	waitFor("no durable completion", func() bool {
		files, _ := filepath.Glob(filepath.Join(h.homeDir, ".config", "anx", "answer-resumes", "*", "runtime-receipts", "*.json"))
		for _, p := range files {
			raw, _ := os.ReadFile(p)
			if strings.Contains(string(raw), `"state":"acknowledged"`) || strings.Contains(string(raw), `"state": "acknowledged"`) {
				return true
			}
		}
		return false
	})
	raw, e = os.ReadFile(filepath.Join(root, "prompt"))
	if e != nil || !strings.Contains(string(raw), "Subject: "+ref) || !strings.Contains(string(raw), "please inspect") {
		t.Fatal(string(raw), e)
	}
}
