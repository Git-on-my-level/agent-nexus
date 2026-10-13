//go:build integration

package integration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Acceptance for oss#389: a resume-mode ask answered while the asking session
// is closed fires the registered executor through a live `anx bridge run`.
func TestResumeDeliveryFiresExecutorAfterAskingSessionCloses(t *testing.T) {
	h := newPasskeyLiveCoreHarness(t)
	h.enrollHost(t, "worker")
	h.registerHumanPasskey(t, "operator", "S6 Operator", h.createHumanInviteToken(t))
	id := mustStringPath(t, h.runCLIExpectOK(t, "worker", nil, "auth", "whoami").Payload, "result.agent.id")
	stmt := fmt.Sprintf("UPDATE agents SET metadata_json=json_set(metadata_json,'$.identity_kind','derived','$.host_id','host-s6','$.host_slug','s6','$.name','worker') WHERE id='%s';", id)
	if out, err := runLegacySQLFixture(t, h, stmt); err != nil {
		t.Fatalf("seed derived identity: %v %s", err, out)
	}

	execDir := t.TempDir()
	scriptPath := filepath.Join(execDir, "fake-executor.sh")
	script := fmt.Sprintf("#!/bin/sh\ncat > %s/answer.json\nenv | grep '^ANX_' | sort > %s/answer-env.txt\n", execDir, execDir)
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	// Start the answer bridge for worker; it must outlive the asking CLI.
	bridgeCmd := exec.Command(h.cliBin, "--json", "--base-url", h.baseURL, "--as", "worker", "bridge", "run")
	bridgeCmd.Env = append(os.Environ(),
		"HOME="+h.homeDir,
		"XDG_CONFIG_HOME="+filepath.Join(h.homeDir, ".config"),
		"AGENTCTL_EXECUTION_ID=",
		"AGENTCTL_ADAPTER=",
		"ANX_SKILLS_AUTO_SYNC=0",
	)
	bridgeLog, err := os.Create(filepath.Join(execDir, "bridge.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer bridgeLog.Close()
	bridgeCmd.Stdout = bridgeLog
	bridgeCmd.Stderr = bridgeLog
	bridgeCmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := bridgeCmd.Start(); err != nil {
		t.Fatalf("start bridge run: %v", err)
	}
	t.Cleanup(func() {
		if bridgeCmd.Process != nil {
			_ = syscall.Kill(-bridgeCmd.Process.Pid, syscall.SIGTERM)
			done := make(chan struct{})
			go func() { _, _ = bridgeCmd.Process.Wait(); close(done) }()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				_ = syscall.Kill(-bridgeCmd.Process.Pid, syscall.SIGKILL)
			}
		}
	})

	board := h.runCLIExpectOK(t, "worker", map[string]any{"board": map[string]any{"title": "S6 delivery", "document_refs": []any{}, "pinned_refs": []any{}, "provenance": map[string]any{"sources": []any{"inferred"}}}}, "boards", "create")
	boardRef := mustStringPath(t, board.Payload, "result.board.ref")
	created := h.runCLIExpectOK(t, "worker", map[string]any{"board_ref": boardRef, "title": "Delivery task"}, "work", "create", "--from-file", "-")
	ref := mustStringPath(t, created.Payload, "result.work.ref")

	// File the resume ask, retrying until the bridge has taken the registry
	// lock: explicit --delivery resume must fail while nothing can deliver.
	var ask cliResult
	deadline := time.Now().Add(15 * time.Second)
	for {
		ask = h.runCLI(t, "worker", nil, "ask", "Ship the delivery?", "--subject-ref", ref, "--recommend", "Yes", "--delivery", "resume", "--on-answer", scriptPath)
		if ask.ExitCode == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if ask.ExitCode != 0 {
		t.Fatalf("resume ask never became fileable: %s", ask.Stdout)
	}
	askID := mustStringPath(t, ask.Payload, "result.ask_id")
	if got := mustStringPath(t, ask.Payload, "result.delivery.mode"); got != "resume" {
		t.Fatalf("ask delivery mode=%q body=%s", got, ask.Stdout)
	}

	// The asking session is done; the human answers anyway.
	h.runCLIExpectOK(t, "operator", map[string]any{
		"inbox_item_id": findAskInboxID(t, h, askID),
		"response_text": "Ship it",
		"outcome":       "answered",
	}, "debug", "inbox", "respond", "--from-file", "-")

	deadline = time.Now().Add(30 * time.Second)
	for {
		answer, readErr := os.ReadFile(filepath.Join(execDir, "answer.json"))
		if readErr == nil && len(answer) > 0 {
			if !strings.Contains(string(answer), "Ship it") {
				t.Fatalf("executor got unexpected answer payload: %s", answer)
			}
			envText, envErr := os.ReadFile(filepath.Join(execDir, "answer-env.txt"))
			if envErr != nil || !strings.Contains(string(envText), "ANX_ASK_ID="+askID) || !strings.Contains(string(envText), "ANX_OUTCOME=answered") {
				t.Fatalf("executor env incomplete: %s err=%v", envText, envErr)
			}
			return
		}
		if time.Now().After(deadline) {
			logTail, _ := os.ReadFile(filepath.Join(execDir, "bridge.log"))
			t.Fatalf("executor never fired for %s; bridge log tail: %s; rows: %s", askID, logTail, debugDeliveryRows(t, h))
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func debugDeliveryRows(t *testing.T, h *liveCoreHarness) string {
	t.Helper()
	query := `SELECT 'sub', id, ask_id, kind, enabled FROM ask_subscriptions;
SELECT 'delivery', id, subscription_id, ask_id, kind, state, attempts FROM ask_deliveries;
SELECT 'wakeup', wakeup_id, status, target_actor_id FROM agent_wakeups;
SELECT 'resolution', request_event_id, resolution_event_id, resolution_type FROM human_attention_request_resolutions;`
	out, err := exec.Command("sqlite3", filepath.Join(h.workspace, "state.sqlite"), query).CombinedOutput()
	if err != nil {
		return "sqlite debug failed: " + err.Error() + ": " + string(out)
	}
	return string(out)
}
