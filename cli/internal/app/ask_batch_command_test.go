package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func askBatchFixture(t *testing.T, asks string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "asks.json")
	if err := os.WriteFile(path, []byte(asks), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func askBatchServer(t *testing.T, captured *atomic.Pointer[map[string]any], requests *atomic.Int64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/asks/batch" {
			http.NotFound(w, r)
			return
		}
		requests.Add(1)
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode batch body: %v", err)
		}
		captured.Store(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"batch_id":"askbatch_test123","events":[{"id":"ev1"},{"id":"ev2"},{"id":"ev3"}]}`))
	}))
}

func TestAskBatchFilesOneAtomicRequest(t *testing.T) {
	t.Parallel()

	var captured atomic.Pointer[map[string]any]
	var requests atomic.Int64
	server := askBatchServer(t, &captured, &requests)
	defer server.Close()

	home := t.TempDir()
	writeDerivedAgentFixture(t, home, "agent-a", `{"agent":"agent-a","username":"agent.alpha","actor_id":"actor_asker","access_token":"token-a","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	path := askBatchFixture(t, `{"asks":[
		{"title":"Ship the flag?","body":"Decision body.","subject_ref":"card:launch","thread_id":"thr1","situation":"A launch decision is waiting.","impact":"The team cannot finish until we choose.","recommendation":{"action":"Ship","why":"This is the quickest safe approach."},"recommend":"Ship","alts":["Wait"],"coverage_hint":"thin"},
		{"title":"Roll back first?","subject_ref":"card:launch","thread_id":"thr1","situation":"A launch decision is waiting.","impact":"The team cannot finish until we choose.","recommendation":{"action":"No","why":"This is the quickest safe approach."},"recommend":"No"},
		{"title":"Notify the field team?","subject_ref":"card:launch","thread_id":"thr1","situation":"A launch decision is waiting.","impact":"The team cannot finish until we choose.","recommendation":{"action":"Yes","why":"This is the quickest safe approach."},"recommend":"Yes","delivery":"none"}
	]}`)

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json", "--base-url", server.URL, "--as", "agent-a",
		"ask", "batch", "--from-file", path,
	})
	envelope := assertEnvelopeOK(t, raw)

	loaded := captured.Load()
	if loaded == nil {
		t.Fatal("batch request never reached the server")
	}
	body := *loaded
	asks, _ := body["asks"].([]any)
	if len(asks) != 3 {
		t.Fatalf("expected three members in one request, got %d", len(asks))
	}
	threads := map[string]bool{}
	for _, rawAsk := range asks {
		ask, _ := rawAsk.(map[string]any)
		payload, _ := ask["payload"].(map[string]any)
		threads[anyStringValue(ask["thread_id"])] = true
		if anyStringValue(payload["delivery"]) == "" {
			t.Fatalf("member lost its delivery mode: %#v", payload)
		}
		if anyStringValue(payload["batch_id"]) != "" {
			t.Fatalf("client must not assign batch ids: %#v", payload)
		}
		if proposals, _ := payload["response_proposals"].([]any); len(proposals) == 0 {
			t.Fatalf("member lost its proposals: %#v", payload)
		}
	}
	if len(threads) != 1 {
		t.Fatalf("members must share one thread: %#v", threads)
	}

	resultBody, _ := envelope["result"].(map[string]any)
	if anyStringValue(resultBody["batch_id"]) != "askbatch_test123" {
		t.Fatalf("result lost the batch id: %#v", resultBody)
	}
	views, _ := resultBody["asks"].([]any)
	if len(views) != 3 {
		t.Fatalf("result must list every ask: %#v", resultBody)
	}
	third, _ := views[2].(map[string]any)
	delivery, _ := third["delivery"].(map[string]any)
	if anyStringValue(delivery["mode"]) != "none" {
		t.Fatalf("per-ask delivery override lost: %#v", third)
	}
}

func TestAskBatchDryRunSendsNothing(t *testing.T) {
	t.Parallel()

	var captured atomic.Pointer[map[string]any]
	var requests atomic.Int64
	server := askBatchServer(t, &captured, &requests)
	defer server.Close()

	home := t.TempDir()
	writeDerivedAgentFixture(t, home, "agent-a", `{"agent":"agent-a","username":"agent.alpha","actor_id":"actor_asker","access_token":"token-a","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	path := askBatchFixture(t, `{"asks":[
		{"title":"One?","subject_ref":"card:x","thread_id":"thr1","situation":"A launch decision is waiting.","impact":"The team cannot finish until we choose.","recommendation":{"action":"Yes","why":"This is the quickest safe approach."},"recommend":"Yes"},
		{"title":"Two?","subject_ref":"card:x","thread_id":"thr1","situation":"A launch decision is waiting.","impact":"The team cannot finish until we choose.","recommendation":{"action":"No","why":"This is the quickest safe approach."},"recommend":"No"}
	]}`)
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json", "--base-url", server.URL, "--as", "agent-a",
		"ask", "batch", "--from-file", path, "--dry-run",
	})
	envelope := assertEnvelopeOK(t, raw)
	if requests.Load() != 0 {
		t.Fatal("dry-run must not send anything")
	}
	resultBody, _ := envelope["result"].(map[string]any)
	if resultBody == nil || resultBody["batch_size"] != float64(2) {
		t.Fatalf("dry-run lost the batch size: %#v", envelope)
	}
}

func anyMap(value any) map[string]any {
	m, _ := value.(map[string]any)
	return m
}

func TestAskBatchRejectsUnderSizedBatchBeforeNetwork(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := askBatchServer(t, &atomic.Pointer[map[string]any]{}, &requests)
	defer server.Close()

	home := t.TempDir()
	writeDerivedAgentFixture(t, home, "agent-a", `{"agent":"agent-a","username":"agent.alpha","actor_id":"actor_asker","access_token":"token-a","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	path := askBatchFixture(t, `{"asks":[{"title":"Solo?","subject_ref":"card:x","thread_id":"thr1","situation":"A launch decision is waiting.","impact":"The team cannot finish until we choose.","recommendation":{"action":"Yes","why":"This is the quickest safe approach."},"recommend":"Yes"}]}`)
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json", "--base-url", server.URL, "--as", "agent-a",
		"ask", "batch", "--from-file", path,
	})
	var envelope map[string]any
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		t.Fatalf("raw output: %s", raw)
	}
	if ok, _ := envelope["ok"].(bool); ok {
		t.Fatalf("single-ask batch must fail: %s", raw)
	}
	if requests.Load() != 0 {
		t.Fatal("rejected batches must not reach the server")
	}
}

func TestAskBatchAppliesAuthoringLintToEveryAsk(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := askBatchServer(t, &atomic.Pointer[map[string]any]{}, &requests)
	defer server.Close()

	home := t.TempDir()
	writeDerivedAgentFixture(t, home, "agent-a", `{"agent":"agent-a","username":"agent.alpha","actor_id":"actor_asker","access_token":"token-a","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	// The second member is pointer-only (no evidence), which the authoring
	// lint rejects exactly as it would for `anx ask`.
	path := askBatchFixture(t, `{"asks":[
		{"title":"Fine?","body":"see docs","subject_ref":"card:x","thread_id":"thr1","situation":"A launch decision is waiting.","impact":"The team cannot finish until we choose.","recommendation":{"action":"Yes","why":"This is the quickest safe approach."},"recommend":"Yes"},
		{"title":"Pointer only?","body":"see the doc","subject_ref":"card:x","thread_id":"thr1","situation":"A launch decision is waiting.","impact":"The team cannot finish until we choose.","recommendation":{"action":"No","why":"This is the quickest safe approach."},"recommend":"No"}
	]}`)
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json", "--base-url", server.URL, "--as", "agent-a",
		"ask", "batch", "--from-file", path,
	})
	if !strings.Contains(raw, "ask_authoring") {
		t.Fatalf("lint failure must surface as ask_authoring: %s", raw)
	}
	if requests.Load() != 0 {
		t.Fatal("lint failures must not reach the server")
	}

	// --force --reason applies to the whole batch, as recorded overrides.
	rawForced := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json", "--base-url", server.URL, "--as", "agent-a",
		"ask", "batch", "--from-file", path, "--force", "--reason", "references are obvious",
	})
	envelope := assertEnvelopeOK(t, rawForced)
	_ = envelope
	if requests.Load() != 1 {
		t.Fatalf("forced batch must file exactly once, got %d", requests.Load())
	}
}

func TestAskBatchAppliesTitleLint(t *testing.T) {
	t.Parallel()

	var requests atomic.Int64
	server := askBatchServer(t, &atomic.Pointer[map[string]any]{}, &requests)
	defer server.Close()

	home := t.TempDir()
	writeDerivedAgentFixture(t, home, "agent-a", `{"agent":"agent-a","username":"agent.alpha","actor_id":"actor_asker","access_token":"token-a","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	path := askBatchFixture(t, `{"asks":[
		{"title":"`+strings.Repeat("界", 101)+`","subject_ref":"card:x","thread_id":"thr1","situation":"A launch decision is waiting.","impact":"The team cannot finish until we choose.","recommendation":{"action":"Yes","why":"This is the quickest safe approach."},"recommend":"Yes"},
		{"title":"Short?","subject_ref":"card:x","thread_id":"thr1","situation":"A launch decision is waiting.","impact":"The team cannot finish until we choose.","recommendation":{"action":"No","why":"This is the quickest safe approach."},"recommend":"No"}
	]}`)
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json", "--base-url", server.URL, "--as", "agent-a",
		"ask", "batch", "--from-file", path,
	})
	if !strings.Contains(raw, "ask_authoring") {
		t.Fatalf("titles over 80 characters must surface as ask_authoring warnings: %s", raw)
	}
	if requests.Load() != 1 {
		t.Fatal("title warnings should allow the ask to be filed")
	}

	rawForced := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json", "--base-url", server.URL, "--as", "agent-a",
		"ask", "batch", "--from-file", path, "--force", "--reason", "Deliberate exception",
	})
	assertEnvelopeOK(t, rawForced)
	if requests.Load() != 2 {
		t.Fatalf("forced title lint must file exactly once more, got %d", requests.Load())
	}
}
