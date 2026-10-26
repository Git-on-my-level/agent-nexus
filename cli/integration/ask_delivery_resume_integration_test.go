//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	testLiveResumeDelivery(t, false)
}

func TestResumeDeliveryRetireUndoKeepsContinuationArmed(t *testing.T) {
	testLiveResumeDelivery(t, true)
}

func testLiveResumeDelivery(t *testing.T, retireBeforeAnswer bool) {
	t.Helper()
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
printf 'ran\n' >> %q
env | grep '^ANX_' | sort > %q
mv %q %q
cat > %q
mv %q %q
`,
		filepath.Join(execDir, "runs"),
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
		ask = h.runCLI(t, "worker", nil, "ask", "Ship the delivery?", "--subject-ref", ref, "--situation", "A launch decision is waiting.", "--impact", "The team cannot finish until we choose.", "--why", "This is the quickest safe approach.", "--recommend", "Yes", "--delivery", "resume", "--on-answer", scriptPath)
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

	if retireBeforeAnswer {
		// Retire under a different principal, with both evidence and an actual
		// X-ANX-Run-Id tied to that curator's private board/card.
		h.runCLIExpectOK(t, "worker", nil, "host", "include", "curator")
		curator := h.runCLIExpectOK(t, "curator", nil, "auth", "whoami")
		curatorID := mustStringPath(t, curator.Payload, "result.agent.id")
		curatorActor := h.actorID(t, "curator")
		if out, err := runLegacySQLFixture(t, h, fmt.Sprintf(`UPDATE agents SET metadata_json=json_set(metadata_json,'$.auth_admin',json('true')) WHERE id='%s';`, curatorID)); err != nil {
			t.Fatal(err, out)
		}
		privateBoard := h.runCLIExpectOK(t, "curator", map[string]any{"board": map[string]any{"title": "Private retirement evidence"}}, "boards", "create")
		privateBoardRef := mustStringPath(t, privateBoard.Payload, "result.board.ref")
		privateBoardThread := mustStringPath(t, privateBoard.Payload, "result.board.thread_id")
		if out, err := runLegacySQLFixture(t, h, fmt.Sprintf(`UPDATE threads SET body_json=json_set(body_json,'$.pm_actor_id','%s') WHERE id='%s';`, curatorActor, privateBoardThread)); err != nil {
			t.Fatal(err, out)
		}
		privateCard := h.runCLIExpectOK(t, "curator", map[string]any{"board_ref": privateBoardRef, "title": "Private curator run"}, "work", "create", "--from-file", "-")
		privateCardRef := mustStringPath(t, privateCard.Payload, "result.work.ref")
		run := h.runCLIExpectOK(t, "curator", map[string]any{"schema_version": 1, "id": "exec-private-retirement", "origin_host_id": "host-private-retirement", "adapter": "codex", "observation": map[string]any{"observed_at": time.Now().UTC().Format(time.RFC3339Nano)}, "state": "running", "liveness": "alive", "labels": []string{"anx.card." + strings.TrimPrefix(privateCardRef, "card:")}}, "runs", "ingest")
		runID := mustStringPath(t, run.Payload, "result.run.id")
		if got := h.runCLI(t, "worker", nil, "runs", "get", runID); got.ExitCode != 3 {
			t.Fatalf("requester could read private run fixture: %s", got.Stdout)
		}
		token := mustStringPath(t, h.runCLIExpectOK(t, "curator", nil, "host", "token", "--as", "curator").Payload, "result.token")
		raw, _ := json.Marshal(map[string]any{"request_key": "private-retirement", "reason": "The original request is stale", "evidence": []string{privateCardRef}})
		req, err := http.NewRequest("POST", h.baseURL+"/asks/"+url.PathEscape(askID)+"/retire", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-ANX-Run-Id", "agentctl/exec-private-retirement")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var retired map[string]any
		err = json.NewDecoder(resp.Body).Decode(&retired)
		resp.Body.Close()
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("retire with private attribution: %d %#v %v", resp.StatusCode, retired, err)
		}
		noticeRef := mustStringPath(t, retired, "notification_event_ref")
		retirementRef := mustStringPath(t, retired, "retirement_event_ref")
		auditRef := mustStringPath(t, retired, "audit_event_ref")
		for _, publicRef := range []string{noticeRef, retirementRef} {
			got := h.runCLIExpectOK(t, "worker", nil, "debug", "events", "get", "--event-id", publicRef)
			if strings.Contains(got.Stdout, runID) || strings.Contains(got.Stdout, privateCardRef) || strings.Contains(got.Stdout, "run_attribution") {
				t.Fatalf("private provenance leaked into public record: %s", got.Stdout)
			}
		}
		if got := h.runCLI(t, "worker", nil, "debug", "events", "get", "--event-id", auditRef); got.ExitCode != 3 {
			t.Fatalf("private curator audit readable: %s", got.Stdout)
		}
		audit := h.runCLIExpectOK(t, "curator", nil, "debug", "events", "get", "--event-id", auditRef)
		if !strings.Contains(audit.Stdout, runID) || !strings.Contains(audit.Stdout, privateCardRef) {
			t.Fatalf("curator audit lost attribution/evidence: %s", audit.Stdout)
		}
		await := h.runCLI(t, "worker", nil, "await", askID, "--timeout", "2s")
		if await.ExitCode != 11 || mustStringPath(t, await.Payload, "error.details.status") != "withdrawn_as_stale" {
			t.Fatalf("private audit broke requester await: %s", await.Stdout)
		}
		assertArmed := func() {
			t.Helper()
			files, err := filepath.Glob(filepath.Join(h.homeDir, ".config", "anx", "answer-resumes", "*", "*.json"))
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, path := range files {
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var entry map[string]any
				if err = json.Unmarshal(raw, &entry); err != nil {
					t.Fatal(err)
				}
				if entry["ask_id"] == askID || entry["ask_ref"] == askID {
					found = true
					if entry["acknowledged"] != false || entry["attempts"] != float64(0) || entry["state"] != "pending" {
						t.Fatalf("retirement consumed continuation: %s", raw)
					}
				}
			}
			if !found {
				t.Fatal("resume registration missing")
			}
			if raw, err := os.ReadFile(filepath.Join(execDir, "runs")); !os.IsNotExist(err) {
				t.Fatalf("retirement ran callback: %q err=%v", raw, err)
			}
			query := fmt.Sprintf(`SELECT count(*) FROM agent_wakeups WHERE wakeup_id LIKE 'ask-%%' OR trigger_event_id IN ('%s','%s')`, strings.TrimPrefix(retirementRef, "event:"), strings.TrimPrefix(noticeRef, "event:"))
			out, err := exec.Command("sqlite3", filepath.Join(h.workspace, "state.sqlite"), query).CombinedOutput()
			if err != nil || strings.TrimSpace(string(out)) != "0" {
				t.Fatalf("retirement left wake: %s %v", out, err)
			}
		}
		assertArmed()
		query := fmt.Sprintf(`SELECT payload_json FROM events WHERE id='%s' AND type='message_posted'`, strings.TrimPrefix(noticeRef, "event:"))
		out, err := exec.Command("sqlite3", filepath.Join(h.workspace, "state.sqlite"), query).CombinedOutput()
		if err != nil {
			t.Fatal(err, string(out))
		}
		var notice map[string]any
		if err = json.Unmarshal(out, &notice); err != nil {
			t.Fatal(err, string(out))
		}
		payload := notice["payload"].(map[string]any)
		if payload["recipient_actor_id"] != h.actorID(t, "worker") || !strings.Contains(fmt.Sprint(payload["text"]), "original request is stale") {
			t.Fatal(notice)
		}
		h.runCLIExpectOK(t, "worker", nil, "ask", "retire", askID, "--undo", "--if-retirement", retirementRef)
		assertArmed()
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
			// Wait for the real bridge's remote completion, rather than recording
			// a delivery receipt in the test. A duplicate callback appends twice.
			out, err := exec.Command("sqlite3", filepath.Join(h.workspace, "state.sqlite"), `SELECT count(*) FROM agent_wakeups WHERE wakeup_id LIKE 'ask-delivery-%' AND status='completed'`).CombinedOutput()
			if err != nil {
				t.Fatal(err, string(out))
			}
			if strings.TrimSpace(string(out)) != "1" {
				if time.Now().After(deadline) {
					t.Fatalf("bridge did not complete: %s rows: %s", out, debugDeliveryRows(t, h))
				}
				time.Sleep(25 * time.Millisecond)
				continue
			}
			runs, err := os.ReadFile(filepath.Join(execDir, "runs"))
			if err != nil || string(runs) != "ran\n" {
				t.Fatalf("continuation not exactly once: %q %v", runs, err)
			}
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
