package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestTypedThreadCommandsGolden(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads":
			if got := r.URL.Query().Get("status"); got != "active" {
				t.Fatalf("expected status query active, got %q", got)
			}
			_, _ = w.Write([]byte(`{"threads":[{"id":"thread_1","title":"Alpha","status":"active"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/threads":
			body, _ := io.ReadAll(r.Body)
			if !bytes.Contains(body, []byte(`"title":"Alpha"`)) {
				t.Fatalf("unexpected create body: %s", string(body))
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"thread":{"id":"thread_1","title":"Alpha","status":"active"}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/threads/thread_1":
			body, _ := io.ReadAll(r.Body)
			if !bytes.Contains(body, []byte(`"status":"resolved"`)) {
				t.Fatalf("unexpected update body: %s", string(body))
			}
			_, _ = w.Write([]byte(`{"thread":{"id":"thread_1","title":"Alpha","status":"resolved"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}

	listOut := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "threads", "list", "--status", "active"})
	assertGolden(t, "threads_list.golden.json", listOut)

	createOut := runCLIForTest(t, home, env, strings.NewReader(`{"thread":{"title":"Alpha"}}`), []string{"--json", "--base-url", server.URL, "threads", "create"})
	assertGolden(t, "threads_create.golden.json", createOut)

	updateOut := runCLIForTest(t, home, env, strings.NewReader(`{"thread":{"status":"resolved"}}`), []string{"--json", "--base-url", server.URL, "threads", "update", "--thread-id", "thread_1"})
	assertGolden(t, "threads_update.golden.json", updateOut)
}

func TestTypedWorkflowCommands(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/threads":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"thread":{"id":"thread_flow_1","title":"Flow Thread","status":"active"}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/threads/thread_flow_1":
			_, _ = w.Write([]byte(`{"thread":{"id":"thread_flow_1","title":"Flow Thread","status":"resolved"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/commitments":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"commitment":{"id":"commitment_flow_1","thread_id":"thread_flow_1","status":"open"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/work_orders":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"artifact":{"id":"artifact_wo_1"},"event":{"id":"event_wo_1"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/receipts":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"artifact":{"id":"artifact_receipt_1"},"event":{"id":"event_receipt_1"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/reviews":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"artifact":{"id":"artifact_review_1"},"event":{"id":"event_review_1"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/inbox":
			_, _ = w.Write([]byte(`{"items":[{"id":"inbox:1","thread_id":"thread_flow_1"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/inbox/ack":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"event":{"id":"event_ack_1"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}

	assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"thread":{"title":"Flow Thread"}}`), []string{"--json", "--base-url", server.URL, "threads", "create"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"thread":{"status":"resolved"}}`), []string{"--json", "--base-url", server.URL, "threads", "update", "thread_flow_1"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"commitment":{"thread_id":"thread_flow_1","title":"Do work"}}`), []string{"--json", "--base-url", server.URL, "commitments", "create"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"work_order":{"thread_id":"thread_flow_1"}}`), []string{"--json", "--base-url", server.URL, "work-orders", "create"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"receipt":{"thread_id":"thread_flow_1"}}`), []string{"--json", "--base-url", server.URL, "receipts", "create"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"review":{"thread_id":"thread_flow_1"}}`), []string{"--json", "--base-url", server.URL, "reviews", "create"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "inbox", "list"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "inbox", "ack", "--thread-id", "thread_flow_1", "--inbox-item-id", "inbox:1"}))
}

func TestArtifactContentRaw(t *testing.T) {
	t.Parallel()

	expected := []byte{0x00, 0x01, 0x02, 'A', '\n', 0xff}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/artifacts/artifact-raw/content" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(expected)
	}))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}
	out := runCLIForTest(t, home, env, nil, []string{"--base-url", server.URL, "artifacts", "content", "--artifact-id", "artifact-raw"})
	if !bytes.Equal([]byte(out), expected) {
		t.Fatalf("unexpected artifact bytes: got=%v want=%v", []byte(out), expected)
	}
}

func TestEventsTailReconnect(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requests := make([]string, 0, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/events/stream" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		requests = append(requests, r.URL.RawQuery)
		count := len(requests)
		mu.Unlock()

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if count == 1 {
			_, _ = io.WriteString(w, "id: e-1\nevent: event\ndata: {\"event\":{\"id\":\"e-1\"}}\n\n")
			return
		}
		if count == 2 {
			if got := r.URL.Query().Get("last_event_id"); got != "e-1" {
				t.Fatalf("expected reconnect with last_event_id=e-1, got %q", got)
			}
			_, _ = io.WriteString(w, "id: e-2\nevent: event\ndata: {\"event\":{\"id\":\"e-2\"}}\n\n")
			return
		}
		_, _ = io.WriteString(w, ": keepalive\n\n")
	}))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}
	raw := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "events", "tail", "--max-events", "2"})

	decoder := json.NewDecoder(strings.NewReader(raw))
	events := make([]map[string]any, 0, 2)
	for decoder.More() {
		var envelope map[string]any
		if err := decoder.Decode(&envelope); err != nil {
			t.Fatalf("decode stream envelope: %v\nraw=%s", err, raw)
		}
		events = append(events, envelope)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 stream envelopes, got %d raw=%s", len(events), raw)
	}
	firstData, _ := events[0]["data"].(map[string]any)
	secondData, _ := events[1]["data"].(map[string]any)
	if firstData["id"] != "e-1" || secondData["id"] != "e-2" {
		t.Fatalf("unexpected stream ids: first=%v second=%v", firstData["id"], secondData["id"])
	}
}

func assertGolden(t *testing.T, goldenFile string, actual string) {
	t.Helper()
	path := filepath.Join("testdata", goldenFile)
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	if string(expected) != actual {
		t.Fatalf("golden mismatch for %s\n--- expected ---\n%s\n--- actual ---\n%s", goldenFile, string(expected), actual)
	}
}

func TestInboxTailReconnect(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/inbox/stream" {
			http.NotFound(w, r)
			return
		}
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		if calls == 1 {
			_, _ = io.WriteString(w, "id: inbox:1@a1\nevent: inbox_item\ndata: {\"item\":{\"id\":\"inbox:1\"}}\n\n")
			return
		}
		if got := r.URL.Query().Get("last_event_id"); got != "inbox:1@a1" {
			t.Fatalf("expected reconnect last_event_id=inbox:1@a1 got %q", got)
		}
		_, _ = io.WriteString(w, "id: inbox:2@b2\nevent: inbox_item\ndata: {\"item\":{\"id\":\"inbox:2\"}}\n\n")
	}))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}
	raw := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "inbox", "tail", "--max-events", "2"})
	if !strings.Contains(raw, `"id": "inbox:1@a1"`) || !strings.Contains(raw, `"id": "inbox:2@b2"`) {
		t.Fatalf("unexpected inbox stream output: %s", raw)
	}
}

func TestEventsStreamDefaultNoFollow(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/events/stream" {
			http.NotFound(w, r)
			return
		}
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "id: e-1\nevent: event\ndata: {\"event\":{\"id\":\"e-1\"}}\n\n")
	}))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}
	raw := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "events", "stream"})
	if calls != 1 {
		t.Fatalf("expected single stream request without --follow, got %d", calls)
	}
	if !strings.Contains(raw, `"id": "e-1"`) {
		t.Fatalf("unexpected stream output: %s", raw)
	}
}

func TestTypedCommandUsageFailures(t *testing.T) {
	t.Parallel()

	cli := New()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return t.TempDir(), nil }
	cli.ReadFile = func(path string) ([]byte, error) {
		return nil, &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
	}

	exitCode := cli.Run([]string{"--json", "threads", "update", "--thread-id", "thread_1"})
	if exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
}

func Example_oarThreadsList() {
	fmt.Println("oar --json threads list --status active")
	// Output: oar --json threads list --status active
}
