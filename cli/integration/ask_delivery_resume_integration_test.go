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
	// Publish env before the answer, and only via rename, so a visible
	// answer.json means answer-env.txt is already complete. A plain redirect
	// creates the destination immediately, which the poll can observe empty.
	script := fmt.Sprintf(`#!/bin/sh
set -e
env | grep '^ANX_' | sort > %q
mv %q %q
cat > %q
mv %q %q
`,
		filepath.Join(execDir, "answer-env.txt.tmp"),
		filepath.Join(execDir, "answer-env.txt.tmp"),
		filepath.Join(execDir, "answer-env.txt"),
		filepath.Join(execDir, "answer.json.tmp"),
		filepath.Join(execDir, "answer.json.tmp"),
		filepath.Join(execDir, "answer.json"),
	)
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	h.runCLIExpectOK(t, "worker", nil, "bridge", "add", "--as", "worker")
	// Start the single host bridge; it must outlive the asking CLI.
	bridgeCmd := exec.Command(h.cliBin, "--json", "bridge", "run")
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
	answerPath := filepath.Join(execDir, "answer.json")
	envPath := filepath.Join(execDir, "answer-env.txt")
	for {
		answer, answerErr := os.ReadFile(answerPath)
		envText, envErr := os.ReadFile(envPath)
		answerReady := answerErr == nil && strings.Contains(string(answer), "Ship it")
		envReady := envErr == nil && strings.Contains(string(envText), "ANX_ASK_ID="+askID) && strings.Contains(string(envText), "ANX_OUTCOME=answered")
		if answerReady && envReady {
			return
		}
		// The executor renames answer.json into place only after the body is
		// fully written, so a visible payload that is not the answer is a real
		// mismatch rather than a partial write.
		if answerErr == nil && len(answer) > 0 && !answerReady {
			t.Fatalf("executor got unexpected answer payload: %s", answer)
		}
		if time.Now().After(deadline) {
			logTail, _ := os.ReadFile(filepath.Join(execDir, "bridge.log"))
			t.Fatalf("executor output incomplete for %s; answer=%q err=%v env=%q err=%v; bridge log tail: %s; rows: %s", askID, answer, answerErr, envText, envErr, logTail, debugDeliveryRows(t, h))
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
