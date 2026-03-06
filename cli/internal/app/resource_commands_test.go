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
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_1/timeline":
			_, _ = w.Write([]byte(`{"events":[],"snapshots":{},"artifacts":{}}`))
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

	patchOut := runCLIForTest(t, home, env, strings.NewReader(`{"thread":{"status":"resolved"}}`), []string{"--json", "--base-url", server.URL, "threads", "patch", "--thread-id", "thread_1"})
	assertGolden(t, "threads_patch.golden.json", patchOut)

	timelineOut := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "threads", "timeline", "--thread-id", "thread_1"})
	timelinePayload := assertEnvelopeOK(t, timelineOut)
	if got := timelinePayload["command"]; got != "threads timeline" {
		t.Fatalf("expected threads timeline command label, got %#v", got)
	}
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
	assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"thread":{"status":"resolved"}}`), []string{"--json", "--base-url", server.URL, "threads", "patch", "thread_flow_1"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"commitment":{"thread_id":"thread_flow_1","title":"Do work"}}`), []string{"--json", "--base-url", server.URL, "commitments", "create"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"work_order":{"thread_id":"thread_flow_1"}}`), []string{"--json", "--base-url", server.URL, "work-orders", "create"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"receipt":{"thread_id":"thread_flow_1"}}`), []string{"--json", "--base-url", server.URL, "receipts", "create"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"review":{"thread_id":"thread_flow_1"}}`), []string{"--json", "--base-url", server.URL, "reviews", "create"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "inbox", "list"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "inbox", "ack", "--thread-id", "thread_flow_1", "--inbox-item-id", "inbox:1"}))
}

func TestInboxUnknownSubcommandGuidance(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "inbox", "10"})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "unknown_subcommand" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	message := anyStringValue(errObj["message"])
	if !strings.Contains(message, "valid subcommands: list, ack, stream, tail") {
		t.Fatalf("expected valid-subcommands guidance, got %q", message)
	}
	if !strings.Contains(message, "`oar inbox list`") || !strings.Contains(message, "`oar inbox ack --thread-id <thread-id> --inbox-item-id <inbox-item-id>`") {
		t.Fatalf("expected concrete inbox examples, got %q", message)
	}
	if !strings.Contains(message, "did you mean `oar inbox ack --thread-id <thread-id> --inbox-item-id <inbox-item-id>`?") {
		t.Fatalf("expected corrective suggestion, got %q", message)
	}
}

func TestInboxGetAliasMapsToList(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/inbox" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"inbox:1","thread_id":"thread_1"}]}`))
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "--base-url", server.URL, "inbox", "get"})
	payload := assertEnvelopeOK(t, raw)
	if got := anyStringValue(payload["command"]); got != "inbox list" {
		t.Fatalf("expected alias to resolve to inbox list, got %q payload=%#v", got, payload)
	}
}

func TestEventsUnknownSubcommandGuidance(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "events", "streem"})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "unknown_subcommand" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	message := anyStringValue(errObj["message"])
	if !strings.Contains(message, "valid subcommands: get, create, stream, tail, explain") {
		t.Fatalf("expected valid subcommands in message, got %q", message)
	}
	if !strings.Contains(message, "did you mean `oar events stream`?") {
		t.Fatalf("expected stream correction, got %q", message)
	}
	if !strings.Contains(message, "`oar events stream --thread-id <thread-id> --follow`") || !strings.Contains(message, "`oar events tail --max-events 20`") {
		t.Fatalf("expected stream/tail examples, got %q", message)
	}
}

func TestDocsCommands(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/docs":
			body, _ := io.ReadAll(r.Body)
			if !bytes.Contains(body, []byte(`"document"`)) {
				t.Fatalf("unexpected docs create body: %s", string(body))
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"document":{"id":"doc_1","head_revision_id":"rev_1"},"revision":{"revision_id":"rev_1","revision_number":1}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/docs/doc_1":
			_, _ = w.Write([]byte(`{"document":{"id":"doc_1","head_revision_id":"rev_2"},"revision":{"revision_id":"rev_2","revision_number":2}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/docs/doc_1":
			body, _ := io.ReadAll(r.Body)
			if !bytes.Contains(body, []byte(`"if_base_revision":"rev_1"`)) {
				t.Fatalf("unexpected docs update body: %s", string(body))
			}
			_, _ = w.Write([]byte(`{"document":{"id":"doc_1","head_revision_id":"rev_2"},"revision":{"revision_id":"rev_2","revision_number":2}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/docs/doc_1/history":
			_, _ = w.Write([]byte(`{"document_id":"doc_1","revisions":[{"revision_id":"rev_1"},{"revision_id":"rev_2"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/docs/doc_1/revisions/rev_1":
			_, _ = w.Write([]byte(`{"revision":{"revision_id":"rev_1","content":"initial"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}

	assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"document":{"id":"doc_1"},"content":"initial","content_type":"text"}`), []string{"--json", "--base-url", server.URL, "docs", "create"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "docs", "get", "--document-id", "doc_1"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"if_base_revision":"rev_1","content":"next","content_type":"text"}`), []string{"--json", "--base-url", server.URL, "docs", "update", "--document-id", "doc_1"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "docs", "history", "--document-id", "doc_1"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "docs", "revision", "get", "--document-id", "doc_1", "--revision-id", "rev_1"}))
}

func TestEventsExplainListMode(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"events", "explain"})
	if !strings.Contains(raw, "Known event types") {
		t.Fatalf("expected list heading in explain output, got %q", raw)
	}
	if !strings.Contains(raw, "review_completed") {
		t.Fatalf("expected review_completed in explain output, got %q", raw)
	}
	if !strings.Contains(raw, "oar events explain <event-type>") {
		t.Fatalf("expected follow-up hint in explain output, got %q", raw)
	}
}

func TestEventsExplainSpecificTypeMode(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "events", "explain", "review_completed"})
	payload := assertEnvelopeOK(t, raw)
	if got := anyStringValue(payload["command"]); got != "events explain" {
		t.Fatalf("unexpected command label: %#v", payload)
	}
	data, _ := payload["data"].(map[string]any)
	if got := anyStringValue(data["event_type"]); got != "review_completed" {
		t.Fatalf("expected event_type review_completed, got %q payload=%#v", got, payload)
	}
	constraints, _ := data["constraints"].([]any)
	foundArtifactConstraint := false
	for _, item := range constraints {
		if strings.Contains(anyStringValue(item), "artifact:") {
			foundArtifactConstraint = true
			break
		}
	}
	if !foundArtifactConstraint {
		t.Fatalf("expected artifact constraint guidance, payload=%#v", payload)
	}

	rawFlag := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "events", "explain", "--type", "review_completed"})
	payloadFlag := assertEnvelopeOK(t, rawFlag)
	dataFlag, _ := payloadFlag["data"].(map[string]any)
	if got := anyStringValue(dataFlag["event_type"]); got != "review_completed" {
		t.Fatalf("expected event_type review_completed via --type, got %q payload=%#v", got, payloadFlag)
	}
}

func TestEventsExplainUnknownTypeFailure(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "events", "explain", "--type", "totally_unknown"})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "invalid_request" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	message := anyStringValue(errObj["message"])
	if !strings.Contains(message, "known types:") || !strings.Contains(message, "review_completed") {
		t.Fatalf("expected known-types guidance in error message, got %q payload=%#v", message, payload)
	}
}

func TestEventsCreateReviewCompletedInvalidRefsFailsLocally(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"event":{"id":"event_unexpected"}}`))
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, strings.NewReader(`{"event":{"type":"review_completed","thread_id":"thread_1","summary":"review done","refs":["artifact:work_order_1","artifact:receipt_1","thread:thread_1"]}}`), []string{
		"--json",
		"--base-url", server.URL,
		"events", "create",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "invalid_request" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	message := anyStringValue(errObj["message"])
	if !strings.Contains(message, `event.type "review_completed"`) || !strings.Contains(message, "artifact:") || !strings.Contains(message, "oar events explain review_completed") {
		t.Fatalf("expected actionable artifact refs guidance, got message=%q payload=%#v", message, payload)
	}

	mu.Lock()
	gotRequests := requestCount
	mu.Unlock()
	if gotRequests != 0 {
		t.Fatalf("expected no HTTP request for invalid local payload, got %d", gotRequests)
	}
}

func TestEventsCreateReviewCompletedValidRefsCallsHTTP(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/events" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		requestCount++
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"event":{"id":"event_review_completed_1"}}`))
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, strings.NewReader(`{"event":{"type":"review_completed","thread_id":"thread_1","summary":"review done","refs":["artifact:work_order_1","artifact:receipt_1","artifact:review_1"]}}`), []string{
		"--json",
		"--base-url", server.URL,
		"events", "create",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	body, _ := data["body"].(map[string]any)
	event, _ := body["event"].(map[string]any)
	if got := anyStringValue(event["id"]); got != "event_review_completed_1" {
		t.Fatalf("unexpected event id in response payload: %#v", payload)
	}

	mu.Lock()
	gotRequests := requestCount
	mu.Unlock()
	if gotRequests != 1 {
		t.Fatalf("expected one HTTP request for valid payload, got %d", gotRequests)
	}
}

func TestThreadsContextCommand(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/threads/thread_1/context" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("max_events"); got != "2" {
			t.Fatalf("expected max_events=2, got %q", got)
		}
		if got := r.URL.Query().Get("include_artifact_content"); got != "true" {
			t.Fatalf("expected include_artifact_content=true, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"thread":{"id":"thread_1"},"recent_events":[],"key_artifacts":[],"open_commitments":[]}`))
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"threads", "context",
		"--thread-id", "thread_1",
		"--max-events", "2",
		"--include-artifact-content",
	})
	payload := assertEnvelopeOK(t, raw)
	if got := anyStringValue(payload["command"]); got != "threads context" {
		t.Fatalf("unexpected command label: %#v", payload)
	}
}

func TestThreadsContextHumanOutputIsPayloadFirst(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/threads/thread_1/context" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"thread":{"id":"thread_1","title":"Pilot Rescue","status":"active","priority":"p1","current_summary":"Need a launch decision today."},
			"recent_events":[
				{"id":"event_1","type":"decision_needed","summary":"Need support and delivery recommendations"},
				{"id":"event_2","type":"decision_made","summary":"Ship the Friday rescue scope"}
			],
			"key_artifacts":[
				{"id":"artifact_1","kind":"gtm-brief","summary":"NorthWave pilot rescue brief"}
			],
			"open_commitments":[
				{"id":"commitment_1","status":"open","title":"Publish rescue brief"}
			]
		}`))
	}))
	defer server.Close()

	home := t.TempDir()
	out := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"threads", "context",
		"--thread-id", "thread_1",
	})

	if !strings.Contains(out, "Thread thread_1") || !strings.Contains(out, "recent_events (2):") {
		t.Fatalf("expected thread context summary, got:\n%s", out)
	}
	if !strings.Contains(out, "decision_needed") || !strings.Contains(out, "gtm-brief") || !strings.Contains(out, "Publish rescue brief") {
		t.Fatalf("expected actionable summary sections, got:\n%s", out)
	}
	if strings.Contains(out, "status: 200") || strings.Contains(out, "header Content-Type:") || strings.Contains(out, `"thread":`) {
		t.Fatalf("expected payload-first output without transport framing, got:\n%s", out)
	}
}

func TestThreadsContextVerboseShowsFullBodyWithoutHeaders(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/threads/thread_1/context" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"thread":{"id":"thread_1"},"recent_events":[],"key_artifacts":[],"open_commitments":[]}`))
	}))
	defer server.Close()

	home := t.TempDir()
	out := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"--verbose",
		"threads", "context",
		"--thread-id", "thread_1",
	})

	if !strings.Contains(out, `"thread": {`) || !strings.Contains(out, `"recent_events": []`) {
		t.Fatalf("expected verbose JSON body, got:\n%s", out)
	}
	if strings.Contains(out, "status: 200") || strings.Contains(out, "header Content-Type:") {
		t.Fatalf("expected verbose output without transport headers, got:\n%s", out)
	}
}

func TestThreadsContextHeadersShowTransportMetadataOnOptIn(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/threads/thread_1/context" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"thread":{"id":"thread_1","title":"Pilot Rescue"},"recent_events":[],"key_artifacts":[],"open_commitments":[]}`))
	}))
	defer server.Close()

	home := t.TempDir()
	out := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"--headers",
		"threads", "context",
		"--thread-id", "thread_1",
	})

	if !strings.Contains(out, "status: 200") || !strings.Contains(out, "header Content-Type: application/json") {
		t.Fatalf("expected transport metadata with --headers, got:\n%s", out)
	}
	if !strings.Contains(out, "Thread thread_1") {
		t.Fatalf("expected payload summary to remain visible, got:\n%s", out)
	}
}

func TestThreadsContextCommandResolvesUniquePrefix(t *testing.T) {
	t.Parallel()

	const canonicalID = "fff63e25-084b-4598-af8f-b6d0a4fbf001"
	const shortPrefix = "fff63e25-084b-4598-af8f"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads/"+shortPrefix+"/context":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"thread not found"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads":
			_, _ = w.Write([]byte(`{"threads":[{"id":"` + canonicalID + `"},{"id":"thread_2"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads/"+canonicalID+"/context":
			_, _ = w.Write([]byte(`{"thread":{"id":"` + canonicalID + `"},"recent_events":[],"key_artifacts":[],"open_commitments":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"threads", "context",
		"--thread-id", shortPrefix,
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	body, _ := data["body"].(map[string]any)
	thread, _ := body["thread"].(map[string]any)
	if got := anyStringValue(thread["id"]); got != canonicalID {
		t.Fatalf("expected canonical thread id %q, got %q payload=%#v", canonicalID, got, payload)
	}
}

func TestThreadsContextCommandAmbiguousPrefixShowsGuidance(t *testing.T) {
	t.Parallel()

	const ambiguousPrefix = "fff63e25"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads/"+ambiguousPrefix+"/context":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"thread not found"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads":
			_, _ = w.Write([]byte(`{"threads":[{"id":"fff63e25-084b-4598-af8f-b6d0a4fbf001"},{"id":"fff63e25-9999-4598-af8f-b6d0a4fbf002"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"threads", "context",
		"--thread-id", ambiguousPrefix,
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "invalid_request" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	message := anyStringValue(errObj["message"])
	if !strings.Contains(message, "ambiguous") || !strings.Contains(message, "short_id=") {
		t.Fatalf("expected ambiguity guidance message, got %q payload=%#v", message, payload)
	}
}

func TestThreadsContextCommandMissingIDShowsGuidance(t *testing.T) {
	t.Parallel()

	const missingID = "does-not-exist"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads/"+missingID+"/context":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"thread not found"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads":
			_, _ = w.Write([]byte(`{"threads":[{"id":"fff63e25-084b-4598-af8f-b6d0a4fbf001"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"threads", "context",
		"--thread-id", missingID,
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "invalid_request" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	message := anyStringValue(errObj["message"])
	if !strings.Contains(message, "is missing") || !strings.Contains(message, "truncated") {
		t.Fatalf("expected missing-id guidance message, got %q payload=%#v", message, payload)
	}
}

func TestThreadsContextCommandEndpointNotFoundDoesNotAttemptIDResolution(t *testing.T) {
	t.Parallel()

	const rawID = "fff63e25-084b-4598-af8f"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads/"+rawID+"/context":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"endpoint not found"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads":
			t.Fatalf("did not expect fallback list call when endpoint is missing")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"threads", "context",
		"--thread-id", rawID,
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "not_found" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	if got := anyStringValue(errObj["message"]); got != "endpoint not found" {
		t.Fatalf("expected endpoint-not-found passthrough, got %q payload=%#v", got, payload)
	}
}

func TestThreadsListIncludesShortID(t *testing.T) {
	t.Parallel()

	const canonicalID = "fff63e25-084b-4598-af8f-b6d0a4fbf001"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/threads" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"threads":[{"id":"` + canonicalID + `","title":"Alpha","status":"active"}]}`))
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"threads", "list",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	body, _ := data["body"].(map[string]any)
	threads, _ := body["threads"].([]any)
	if len(threads) != 1 {
		t.Fatalf("expected one thread in list payload, got %#v", payload)
	}
	thread, _ := threads[0].(map[string]any)
	if got := anyStringValue(thread["short_id"]); got != canonicalID[:12] {
		t.Fatalf("expected short_id %q, got %q payload=%#v", canonicalID[:12], got, payload)
	}
}

func TestInboxAckActorIDMeAliasFromProfile(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/inbox/ack" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode inbox ack body: %v body=%s", err, string(body))
		}
		if got := strings.TrimSpace(anyStringValue(payload["actor_id"])); got != "actor-profile-1" {
			t.Fatalf("expected actor_id from profile, got %q body=%s", got, string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"event":{"id":"event_ack_profile"}}`))
	}))
	defer server.Close()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-a", `{"agent":"agent-a","actor_id":"actor-profile-1","access_token":"token-a","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"--agent", "agent-a",
		"inbox", "ack",
		"--thread-id", "thread_1",
		"--inbox-item-id", "inbox:1",
		"--actor-id", "me",
	})
	assertEnvelopeOK(t, raw)
}

func TestInboxAckPositionalInboxItemIDResolvesThreadFromInboxList(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/inbox":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items":[{"id":"inbox:decision_needed:thread_42:none:event_1","thread_id":"thread_42"}],"generated_at":"2026-03-05T00:00:00Z"}`))
			return
		case r.Method == http.MethodPost && r.URL.Path == "/inbox/ack":
			body, _ := io.ReadAll(r.Body)
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("decode inbox ack body: %v body=%s", err, string(body))
			}
			if got := strings.TrimSpace(anyStringValue(payload["thread_id"])); got != "thread_42" {
				t.Fatalf("expected resolved thread_id thread_42, got %q body=%s", got, string(body))
			}
			if got := strings.TrimSpace(anyStringValue(payload["inbox_item_id"])); got != "inbox:decision_needed:thread_42:none:event_1" {
				t.Fatalf("unexpected inbox_item_id %q body=%s", got, string(body))
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"event":{"id":"event_ack_positional"}}`))
			return
		default:
			http.NotFound(w, r)
			return
		}
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"inbox", "ack",
		"inbox:decision_needed:thread_42:none:event_1",
	})
	assertEnvelopeOK(t, raw)
}

func TestInboxAckActorIDMeRequiresProfileActorID(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-a", `{}`)

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--agent", "agent-a",
		"inbox", "ack",
		"--thread-id", "thread_1",
		"--inbox-item-id", "inbox:1",
		"--actor-id", "me",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "invalid_request" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	if !strings.Contains(anyStringValue(errObj["message"]), "requires actor_id") {
		t.Fatalf("expected actor_id guidance, payload=%#v", payload)
	}
}

func TestDerivedRebuildActorIDMeAliasFromProfile(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/derived/rebuild" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode derived rebuild body: %v body=%s", err, string(body))
		}
		if got := strings.TrimSpace(anyStringValue(payload["actor_id"])); got != "actor-profile-2" {
			t.Fatalf("expected actor_id from profile, got %q body=%s", got, string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-b", `{"agent":"agent-b","actor_id":"actor-profile-2","access_token":"token-b","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"--agent", "agent-b",
		"derived", "rebuild",
		"--actor-id", "me",
	})
	assertEnvelopeOK(t, raw)
}

func TestDerivedRebuildActorIDMeRequiresProfileActorID(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-b", `{}`)

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--agent", "agent-b",
		"derived", "rebuild",
		"--actor-id", "me",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "invalid_request" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	if !strings.Contains(anyStringValue(errObj["message"]), "requires actor_id") {
		t.Fatalf("expected actor_id guidance, payload=%#v", payload)
	}
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

	exitCode := cli.Run([]string{"--json", "threads", "patch", "--thread-id", "thread_1"})
	if exitCode != 2 {
		t.Fatalf("expected exit code 2, got %d stdout=%s stderr=%s", exitCode, stdout.String(), stderr.String())
	}
}

func TestDocsRevisionSubcommandRequiredGuidance(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "docs", "revision"})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "subcommand_required" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	message := anyStringValue(errObj["message"])
	if !strings.Contains(message, "expected one of: get") {
		t.Fatalf("expected valid subcommands in required message, got %q", message)
	}
	if !strings.Contains(message, "`oar docs revision get --document-id <document-id> --revision-id <revision-id>`") {
		t.Fatalf("expected usage examples in required message, got %q", message)
	}
}

func Example_oarThreadsList() {
	fmt.Println("oar --json threads list --status active")
	// Output: oar --json threads list --status active
}

func writeAgentProfile(t *testing.T, home string, agent string, profileJSON string) {
	t.Helper()
	profilesDir := filepath.Join(home, ".config", "oar", "profiles")
	if err := os.MkdirAll(profilesDir, 0o700); err != nil {
		t.Fatalf("mkdir profiles dir: %v", err)
	}
	profilePath := filepath.Join(profilesDir, agent+".json")
	if err := os.WriteFile(profilePath, []byte(profileJSON), 0o600); err != nil {
		t.Fatalf("write profile: %v", err)
	}
}
