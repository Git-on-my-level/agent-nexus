package replay

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"organization-autorunner-tools-oar-http-record/internal/compiled"
)

func TestReplayBindsCapturesAndRewritesLaterRequests(t *testing.T) {
	t.Parallel()

	var observedArtifactBody string
	var observedRevisionPath string
	var observedRevisionBase string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/topics":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"topic":{"id":"topic_runtime","thread_id":"thread_runtime"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/artifacts":
			raw, _ := ioReadAllString(r)
			observedArtifactBody = raw
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"artifact":{"id":"artifact_runtime"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/docs":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"document":{"id":"document_runtime"},"revision":{"revision_id":"revision_runtime_1"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/docs/document_runtime/revisions":
			observedRevisionPath = r.URL.Path
			raw, _ := ioReadAllString(r)
			observedRevisionBase = raw
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"revision":{"revision_id":"revision_runtime_2"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	run := compiled.Run{
		SchemaVersion: 1,
		Exchanges: []compiled.Exchange{
			{
				Seq:                1,
				Method:             http.MethodPost,
				Path:               "/topics",
				RequestBodyKind:    "json",
				RequestBody:        map[string]any{"actor_id": "actor-ops", "topic": map[string]any{"id": "thread-alpha", "title": "Alpha"}},
				ExpectedStatusCode: 200,
				Captures: []compiled.Capture{
					{Alias: "topic_thread_alpha", ResponsePointer: "/topic/id"},
					{Alias: "thread_thread_alpha", ResponsePointer: "/topic/thread_id"},
				},
			},
			{
				Seq:                2,
				Method:             http.MethodPost,
				Path:               "/artifacts",
				RequestBodyKind:    "json",
				RequestBody:        map[string]any{"artifact": map[string]any{"thread_id": "{{thread_thread_alpha}}", "refs": []any{"topic:{{topic_thread_alpha}}"}}},
				ExpectedStatusCode: 200,
			},
			{
				Seq:                3,
				Method:             http.MethodPost,
				Path:               "/docs",
				RequestBodyKind:    "json",
				RequestBody:        map[string]any{"document": map[string]any{"id": "doc-alpha"}, "content": "one", "content_type": "text"},
				ExpectedStatusCode: 200,
				Captures: []compiled.Capture{
					{Alias: "document_doc_alpha", ResponsePointer: "/document/id"},
					{Alias: "revision_document_doc_alpha_000", ResponsePointer: "/revision/revision_id"},
				},
			},
			{
				Seq:                4,
				Method:             http.MethodPost,
				Path:               "/docs/{{document_doc_alpha}}/revisions",
				RequestBodyKind:    "json",
				RequestBody:        map[string]any{"if_base_revision": "{{revision_document_doc_alpha_000}}", "content": "two", "content_type": "text"},
				ExpectedStatusCode: 200,
				Captures: []compiled.Capture{
					{Alias: "revision_document_doc_alpha_001", ResponsePointer: "/revision/revision_id"},
				},
			},
		},
	}

	bindings, err := Replay(context.Background(), run, Options{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(observedArtifactBody, `"thread_id":"thread_runtime"`) {
		t.Fatalf("artifact body did not contain substituted thread id: %s", observedArtifactBody)
	}
	if !strings.Contains(observedArtifactBody, `"topic:topic_runtime"`) {
		t.Fatalf("artifact body did not contain substituted topic ref: %s", observedArtifactBody)
	}
	if observedRevisionPath != "/docs/document_runtime/revisions" {
		t.Fatalf("unexpected revision path %q", observedRevisionPath)
	}
	if !strings.Contains(observedRevisionBase, `"if_base_revision":"revision_runtime_1"`) {
		t.Fatalf("revision body did not contain substituted base revision: %s", observedRevisionBase)
	}
	if got := bindings["revision_document_doc_alpha_001"]; got != "revision_runtime_2" {
		t.Fatalf("unexpected final binding: %q", got)
	}
}

func TestReplayRetriesTransientServerErrors(t *testing.T) {
	t.Parallel()

	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"try again"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"topic":{"id":"topic_runtime","thread_id":"thread_runtime"}}`))
	}))
	defer server.Close()

	run := compiled.Run{
		SchemaVersion: 1,
		Exchanges: []compiled.Exchange{{
			Seq:                1,
			Method:             http.MethodPost,
			Path:               "/topics",
			RequestBodyKind:    "json",
			RequestBody:        map[string]any{"topic": map[string]any{"id": "thread-alpha"}},
			ExpectedStatusCode: 200,
			Captures: []compiled.Capture{
				{Alias: "topic_thread_alpha", ResponsePointer: "/topic/id"},
			},
		}},
	}

	bindings, err := Replay(context.Background(), run, Options{BaseURL: server.URL, BaseDelay: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
	if got := bindings["topic_thread_alpha"]; got != "topic_runtime" {
		t.Fatalf("unexpected binding %q", got)
	}
}

func ioReadAllString(r *http.Request) (string, error) {
	defer r.Body.Close()
	buf := new(strings.Builder)
	_, err := io.Copy(buf, r.Body)
	return buf.String(), err
}
