//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHostHeadlessTokenAndRuns(t *testing.T) {
	h := newLiveCoreHarness(t)
	h.registerAgentBootstrap(t, "codex", "ignored")
	first := h.runCLIExpectOK(t, "codex", nil, "host", "token", "--as", "codex")
	second := h.runCLIExpectOK(t, "codex", nil, "host", "token", "--as", "codex")
	token := mustStringPath(t, first.Payload, "result.token")
	if token == "" || token != mustStringPath(t, second.Payload, "result.token") {
		t.Fatalf("token cache did not reuse short-lived grant")
	}
	host := h.runCLIExpectOK(t, "codex", nil, "host", "status")
	hostID := mustStringPath(t, host.Payload, "result.host.id")
	h.runCLIExpectOK(t, "codex", nil, "host", "list")
	h.runCLIExpectOK(t, "codex", nil, "host", "exclude", "cursor")
	denied := h.runCLI(t, "cursor", nil, "host", "token", "--as", "cursor")
	if denied.ExitCode == 0 || mustStringPath(t, denied.Payload, "error.code") != "agent_excluded" {
		t.Fatalf("excluded token accepted: %s", denied.Stdout)
	}
	h.runCLIExpectOK(t, "codex", nil, "host", "include", "cursor")
	h.runCLIExpectOK(t, "cursor", nil, "host", "token", "--as", "cursor")
	envelope := map[string]any{"schema_version": 1, "id": "exec-alpha-bravo-charlie-delta-echo-foxtrot", "origin_host_id": "host-alpha-bravo-charlie-delta-echo-foxtrot", "adapter": "codex", "observation": map[string]any{"observed_at": time.Now().UTC().Format(time.RFC3339Nano)}, "state": "running", "liveness": "alive", "labels": []string{"anx.card.task"}}
	// agentctl's journal host differs between CI hosts; this execution is absent
	// from that journal, so the callback host is accepted as external provenance.
	_ = hostID
	firstRun := h.runCLIExpectOK(t, "codex", envelope, "runs", "ingest")
	secondRun := h.runCLIExpectOK(t, "codex", envelope, "runs", "ingest")
	runID := mustStringPath(t, firstRun.Payload, "result.run.id")
	if mustStringPath(t, secondRun.Payload, "result.run.id") != runID {
		t.Fatalf("idempotent replay changed run")
	}
	callback := map[string]any{
		"schema_version": 1, "delivery_id": "delivery-alpha-bravo-charlie-delta-echo-foxtrot", "subscription_id": "sub-alpha-bravo-charlie-delta-echo-foxtrot", "event_id": "event-alpha-bravo-charlie-delta-echo-foxtrot", "event_dedupe_key": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "attempt": 1,
		"sent_at": time.Now().UTC().Format(time.RFC3339Nano), "expires_at": time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano), "nonce": "local-test",
		"event": map[string]any{"schema_version": 1, "id": "event-alpha-bravo-charlie-delta-echo-foxtrot", "execution_id": "exec-alpha-bravo-charlie-delta-echo-foxtrot", "origin_host_id": "host-alpha-bravo-charlie-delta-echo-foxtrot", "adapter": "codex", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "state": "running", "kind": "progress", "authority": "native", "ordering": "observation", "sequence": 1, "dedupe_key": "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "dedupe_version": 1, "occurred_at": nil, "payload": map[string]any{}},
	}
	callbackJSON, err := json.Marshal(callback)
	if err != nil {
		t.Fatal(err)
	}
	eventPath := filepath.Join(h.homeDir, "callback.json")
	if err := os.WriteFile(eventPath, callbackJSON, 0600); err != nil {
		t.Fatal(err)
	}
	configDir := filepath.Join(h.homeDir, ".config", "anx")
	// agentctl command destinations append the event path and expose exactly
	// PATH and LANG to the child: no HOME, stdin, or run-context variables.
	ingest := exec.Command(h.cliBin, "--json", "--config-dir", configDir, "runs", "ingest", eventPath)
	ingest.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
	callbackOutput, err := ingest.CombinedOutput()
	if err != nil {
		t.Fatalf("callback ingest with agentctl environment failed: %v %s", err, callbackOutput)
	}
	var callbackPayload map[string]any
	if err := json.Unmarshal(callbackOutput, &callbackPayload); err != nil {
		t.Fatal(err)
	}
	if mustStringPath(t, callbackPayload, "result.run.id") != runID {
		t.Fatalf("callback replay changed run")
	}
	badPath := filepath.Join(h.homeDir, "bad-callback.json")
	if err := os.WriteFile(badPath, []byte("{invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	bad := exec.Command(h.cliBin, "--json", "--config-dir", configDir, "runs", "ingest", badPath)
	bad.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
	if output, err := bad.CombinedOutput(); err == nil {
		t.Fatalf("invalid callback succeeded: %s", output)
	}
	logPath := filepath.Join(configDir, "logs", "runs-ingest.log")
	logData, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(logData), "invalid_envelope") {
		t.Fatalf("missing safe callback failure log: %v %s", err, logData)
	}
	if stat, err := os.Stat(logPath); err != nil || stat.Mode().Perm() != 0600 {
		t.Fatalf("unsafe callback log: %v %v", stat, err)
	}
	h.runCLIExpectOK(t, "codex", nil, "runs", "get", runID)
	h.runCLIExpectOK(t, "codex", nil, "runs", "list", "--host-id", hostID)
	cmd := exec.Command(h.cliBin, "--json", "--base-url", h.baseURL, "--as", "codex", "api", "call", "--method", "PATCH", "--path", "/agents/me/presence", "--from-file", "-")
	cmd.Env = append(os.Environ(), "HOME="+h.homeDir, "ANX_ACCESS_TOKEN=", "AGENTCTL_ADAPTER=codex", "AGENTCTL_EXECUTION_ID=exec-attrib-bravo-charlie-delta-echo-foxtrot", "AGENTCTL_HOST_ID=host-alpha-bravo-charlie-delta-echo-foxtrot")
	cmd.Stdin = strings.NewReader(`{"note":"attribution test"}`)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("attributed write failed: %v %s", err, output)
	}
	runs := h.runCLIExpectOK(t, "codex", nil, "runs", "list")
	rows, _ := runs.Payload["result"].(map[string]any)["runs"].([]any)
	found := false
	for _, row := range rows {
		if row.(map[string]any)["external_id"] == "exec-attrib-bravo-charlie-delta-echo-foxtrot" {
			found = true
		}
	}
	if !found {
		t.Fatalf("run attribution did not create provisional run: %s", runs.Stdout)
	}
}

func TestHostInteractiveEnrollment(t *testing.T) {
	verificationURL := "http://127.0.0.1:5291/o/local/w/local/access/hosts/enroll"
	h := newLiveCoreHarnessEnv(t, []string{"ANX_PUBLIC_WEB_UI_WORKSPACE_URL=http://127.0.0.1:5291/o/local/w/local"})
	admin := h.postCore(t, "/auth/passkey/dev/register", "", map[string]any{"display_name": "Interactive admin", "bootstrap_token": h.bootstrapToken})
	bearer := mustStringPath(t, admin, "tokens.access_token")
	cmd := exec.Command(h.cliBin, "--base-url", h.baseURL, "host", "enroll", "--name", "interactive-host")
	cmd.Env = append(os.Environ(), "HOME="+h.homeDir, "ANX_AS=codex")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var id string
	for i := 0; i < 50; i++ {
		req, _ := http.NewRequest("GET", h.baseURL+"/auth/hosts/enrollments/pending", nil)
		req.Header.Set("Authorization", "Bearer "+bearer)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var pending map[string]any
		_ = json.Unmarshal(raw, &pending)
		rows, _ := pending["enrollments"].([]any)
		if len(rows) > 0 {
			id, _ = rows[0].(map[string]any)["id"].(string)
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if id == "" {
		_ = cmd.Process.Kill()
		t.Fatal("interactive enrollment not pending")
	}
	h.postCore(t, "/auth/hosts/enrollments/"+id+"/approve", bearer, map[string]any{})
	if err := cmd.Wait(); err != nil {
		t.Fatalf("enroll: %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "user_code=") || !strings.Contains(stdout.String(), "verification_url="+verificationURL) || strings.Contains(stdout.String(), h.baseURL+"/access/hosts/enroll") {
		t.Fatalf("missing interactive instructions: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	dirs, err := os.ReadDir(filepath.Join(h.homeDir, ".config", "anx", "hosts"))
	if err != nil || len(dirs) != 1 {
		t.Fatalf("host files missing: %v %v", dirs, err)
	}
	key := filepath.Join(h.homeDir, ".config", "anx", "hosts", dirs[0].Name(), "host.ed25519")
	stat, err := os.Stat(key)
	if err != nil || stat.Mode().Perm() != 0600 {
		t.Fatalf("unsafe host key: %v %v", stat, err)
	}
}
