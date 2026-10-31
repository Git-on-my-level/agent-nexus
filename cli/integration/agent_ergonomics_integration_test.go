//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Keep the scenario and its full readbacks reproducible without using a shared
// workspace or credentials. Enrollment and the simulated human are fixtures.
func TestAgentErgonomicsAgainstCore(t *testing.T) {
	h := newPasskeyLiveCoreHarness(t)
	t.Cleanup(func() {
		if t.Failed() {
			logs, err := os.ReadFile(h.logPath)
			if err != nil {
				t.Log(err)
			} else {
				t.Logf("core log:\n%s", logs)
			}
		}
	})
	// Allow the identical scenario to measure a separately built baseline CLI.
	if binary := os.Getenv("ANX_ERGONOMICS_CLI_BINARY"); binary != "" {
		h.cliBin = binary
	}
	h.enrollHost(t, "worker")
	calls, failures, outputBytes := 0, 0, 0
	run := func(jsonMode bool, input any, args ...string) cliResult {
		t.Helper()
		argv := []string{"--base-url", h.baseURL, "--as", "worker", "--config-dir", filepath.Join(h.homeDir, ".config", "anx")}
		if jsonMode {
			argv = append(argv, "--json")
		}
		cmd := exec.Command(h.cliBin, append(argv, args...)...)
		cmd.Env = append(os.Environ(), "HOME="+h.homeDir, "XDG_CONFIG_HOME="+filepath.Join(h.homeDir, ".config"), "ANX_ACCESS_TOKEN=", "ANX_JSON=false", "ANX_SKILLS_AUTO_SYNC=0", "ANX_UPDATE_POLICY=off")
		if input != nil {
			raw, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			cmd.Stdin = bytes.NewReader(raw)
		}
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		code := 0
		if err := cmd.Run(); err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatal(err)
			}
			code = exit.ExitCode()
		}
		calls++
		outputBytes += stdout.Len() + stderr.Len()
		if code != 0 {
			failures++
		}
		t.Logf("argv=%q exit=%d stdout_bytes=%d stderr_bytes=%d\nstdout:\n%sstderr:\n%s", args, code, stdout.Len(), stderr.Len(), stdout.String(), stderr.String())
		res := cliResult{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: code}
		if jsonMode {
			res.Payload = mustParseJSON(t, res.Stdout)
		}
		return res
	}
	ok := func(jsonMode bool, input any, args ...string) cliResult {
		t.Helper()
		res := run(jsonMode, input, args...)
		if res.ExitCode != 0 {
			t.Fatalf("scenario command failed: %v", args)
		}
		return res
	}
	created := ok(true, map[string]any{"title": "Agent ergonomics scenario", "phase": "ready"}, "work", "create", "--from-file", "-", "--handle", "ergonomics-task")
	ref := mustStringPath(t, created.Payload, "result.work.ref")
	if ref != "card:ergonomics-task" {
		t.Fatalf("chosen handle ignored: %s", created.Stdout)
	}
	orient := ok(false, nil, "orient")
	if !strings.Contains(orient.Stdout, "source=flag:--base-url") {
		t.Fatalf("workspace selection missing: %s", orient.Stdout)
	}
	jsonOrient := ok(true, nil, "orient")
	if _, exists := jsonOrient.Payload["result"].(map[string]any)["workspace"]; exists {
		t.Fatalf("brief polluted with workspace metadata: %s", jsonOrient.Stdout)
	}
	if mustStringPath(t, jsonOrient.Payload, "workspace.base_url") != h.baseURL {
		t.Fatalf("workspace envelope missing: %s", jsonOrient.Stdout)
	}
	urlResult := ok(false, nil, "url", ref)
	legacyURL := ok(false, nil, "url", "card", ref)
	expectedURL := h.baseURL + "/boards/" + strings.TrimPrefix(mustStringPath(t, created.Payload, "result.work.board_ref"), "board:") + "?card=ergonomics-task\n"
	if urlResult.Stdout != legacyURL.Stdout || urlResult.Stdout != expectedURL {
		t.Fatalf("url output: %s", urlResult.Stdout)
	}
	noted := ok(false, nil, "note", ref, "Progress saved", "--next", "1d", "Acceptance checks", "--evidence", "https://example.test/verification")
	if !strings.Contains(noted.Stdout, "https://example.test/verification") || !strings.Contains(noted.Stdout, "saved note_ref=event:") || !strings.Contains(noted.Stdout, "next_at=") || !strings.Contains(noted.Stdout, `next_what="Acceptance checks"`) || strings.Contains(noted.Stdout, "Review the error") {
		t.Fatalf("note confirmation: %s", noted.Stdout)
	}
	met := ok(false, nil, "note", ref, "Acceptance checks finished", "--evidence", "https://example.test/verification", "--met")
	if strings.Contains(met.Stdout, "next_at=") {
		t.Fatalf("--met kept a promise: %s", met.Stdout)
	}
	noNext := ok(false, nil, "note", ref, "More progress")
	if strings.Contains(noNext.Stdout, "suggest.3=") || !strings.Contains(noNext.Stdout, "anx note "+ref+" --next") {
		t.Fatalf("note suggestion: %s", noNext.Stdout)
	}
	card := ok(false, nil, "cards", "create", "--board", mustStringPath(t, created.Payload, "result.work.board_ref"), "--title", "Ergonomics extra card", "--body", "Details", "--handle", "ergonomics-extra")
	if !strings.Contains(card.Stdout, "created ref=card:ergonomics-extra handle=ergonomics-extra url=") || strings.Contains(card.Stdout, "result.board.card_refs") {
		t.Fatalf("create confirmation: %s", card.Stdout)
	}
	conflict := run(true, map[string]any{"title": "Duplicate"}, "work", "create", "--from-file", "-", "--handle", "ergonomics-task")
	if conflict.ExitCode != 4 || !strings.Contains(conflict.Stdout, "handle is already reserved") {
		t.Fatalf("handle conflict: %s", conflict.Stdout)
	}
	ok(false, nil, "work", "start", ref)
	session := ok(true, map[string]any{"provider": "generic", "native_session_id": "ergonomics-scenario", "activity": "active", "sequence": 1, "capabilities": map[string]any{"resume": "unknown", "history": "unsupported", "logs": "unknown"}}, "sessions", "register", "--from-file", "-")
	ok(false, map[string]any{"session_id": mustStringPath(t, session.Payload, "result.session.session_id"), "activity": "active", "sequence": 1}, "work", "participants", "register", ref, "--from-file", "-")
	ok(false, nil, "cards", "message", ref, "--body", "Implemented the scenario; local evidence follows.")
	ask := ok(true, nil, "ask", "Proceed with verification?", "--subject-ref", ref, "--situation", "A launch decision is waiting.", "--impact", "The team cannot finish until we choose.", "--why", "This is the quickest safe approach.", "--recommend", "Proceed", "--force", "--reason", "Scripted local scenario")
	askID := mustStringPath(t, ask.Payload, "result.ask_id")
	// Human fixture answers through the same API used by the operator UI.
	inbox := h.runCLIExpectOK(t, "worker", nil, "debug", "inbox", "list")
	items, _ := getPathValue(inbox.Payload, "result.items")
	answered := false
	for _, raw := range items.([]any) {
		item := raw.(map[string]any)
		if item["request_event_ref"] == askID {
			h.postCore(t, "/inbox/"+item["id"].(string)+"/respond", h.adminToken, map[string]any{"inbox_item_id": item["id"], "response_text": "Proceed", "outcome": "answered"})
			answered = true
		}
	}
	if !answered {
		t.Fatalf("human fixture did not find ask %s: %s", askID, inbox.Stdout)
	}
	ok(false, nil, "await", askID, "--timeout", "10s")
	ok(false, nil, "work", "block", "Local dependency unavailable", ref)
	ok(false, nil, "work", "start", ref)
	invalidStatus := run(true, nil, "plan", "step", "add", ref, "--title", "Verify", "--status", "in_progress")
	if invalidStatus.ExitCode != 2 {
		t.Fatalf("invalid status exit=%d", invalidStatus.ExitCode)
	}
	if !strings.Contains(invalidStatus.Stdout, "not_started") {
		ok(false, nil, "help", "plan", "step", "add")
	}
	ok(false, nil, "plan", "step", "add", ref, "--title", "Verify", "--step-id", "verify", "--status", "active")
	ok(false, nil, "plan", "step", "update", ref, "verify", "--status", "done")
	ok(false, nil, "plan", "show", ref)
	ok(false, nil, "done", ref, "--evidence", "https://example.test/verification")
	topic := createReportTopic(t, h, "ergonomics")
	file := filepath.Join(t.TempDir(), "report.json")
	writeReportFixture(t, file, reportFixture(t, "Ergonomics report", "Scenario verified against an isolated core."))
	ok(false, nil, "report", "publish", file, "--topic", "topic:"+topic)
	t.Logf("SCENARIO calls=%d errors=%d output_bytes=%d (excludes enrollment and human/report-topic fixtures)", calls, failures, outputBytes)
}
