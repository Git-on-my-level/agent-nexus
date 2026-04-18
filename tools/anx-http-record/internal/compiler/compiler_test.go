package compiler

import (
	"strings"
	"testing"

	"agent-nexus-tools-anx-http-record/internal/recorder"
)

func TestCompileEntriesReplacesResponseDerivedIDsWithPlaceholders(t *testing.T) {
	t.Parallel()

	run, err := CompileEntries([]recorder.Entry{
		{
			Seq:                  1,
			Method:               "POST",
			Path:                 "/topics",
			RequestBodyEncoding:  "json",
			RequestBody:          `{"actor_id":"actor-ops","topic":{"id":"thread-alpha","title":"Alpha"}}`,
			StatusCode:           201,
			ResponseBodyEncoding: "json",
			ResponseBody:         `{"topic":{"id":"topic_123","thread_id":"thread_999"}}`,
		},
		{
			Seq:                  2,
			Method:               "POST",
			Path:                 "/artifacts",
			RequestBodyEncoding:  "json",
			RequestBody:          `{"actor_id":"actor-ops","artifact":{"id":"artifact-a","thread_id":"thread_999","refs":["topic:topic_123"]}}`,
			StatusCode:           201,
			ResponseBodyEncoding: "json",
			ResponseBody:         `{"artifact":{"id":"artifact-a"}}`,
		},
		{
			Seq:                  3,
			Method:               "POST",
			Path:                 "/docs",
			RequestBodyEncoding:  "json",
			RequestBody:          `{"actor_id":"actor-ops","document":{"id":"doc-alpha","title":"Alpha"},"content":"one","content_type":"text"}`,
			StatusCode:           201,
			ResponseBodyEncoding: "json",
			ResponseBody:         `{"document":{"id":"document_111"},"revision":{"revision_id":"rev_aaa"}}`,
		},
		{
			Seq:                  4,
			Method:               "POST",
			Path:                 "/docs/document_111/revisions",
			RequestBodyEncoding:  "json",
			RequestBody:          `{"actor_id":"actor-ops","if_base_revision":"rev_aaa","content":"two","content_type":"text"}`,
			StatusCode:           201,
			ResponseBodyEncoding: "json",
			ResponseBody:         `{"revision":{"revision_id":"rev_bbb"}}`,
		},
	}, Options{SourceRecording: "fixture.jsonl"})
	if err != nil {
		t.Fatal(err)
	}

	if len(run.Exchanges) != 4 {
		t.Fatalf("expected 4 exchanges, got %d", len(run.Exchanges))
	}
	artifactBody := run.Exchanges[1].RequestBody.(map[string]any)
	artifact := artifactBody["artifact"].(map[string]any)
	if got := artifact["thread_id"]; got != "{{thread_thread_alpha}}" {
		t.Fatalf("expected thread placeholder, got %#v", got)
	}
	refs := artifact["refs"].([]any)
	if got := refs[0]; got != "topic:{{topic_thread_alpha}}" {
		t.Fatalf("expected topic ref placeholder, got %#v", got)
	}

	if got := run.Exchanges[3].Path; got != "/docs/{{document_doc_alpha}}/revisions" {
		t.Fatalf("unexpected revisions path: %s", got)
	}
	revisionsBody := run.Exchanges[3].RequestBody.(map[string]any)
	if got := revisionsBody["if_base_revision"]; got != "{{revision_document_doc_alpha_000}}" {
		t.Fatalf("expected revision placeholder, got %#v", got)
	}
}

func TestCompileEntriesSkipsPrincipalSessionMutations(t *testing.T) {
	t.Parallel()

	run, err := CompileEntries([]recorder.Entry{
		{
			Seq:                  1,
			Method:               "PATCH",
			Path:                 "/agents/me",
			RequestBodyEncoding:  "json",
			RequestBody:          `{"registration":{"handle":"pi","actor_id":"actor-1"}}`,
			StatusCode:           200,
			ResponseBodyEncoding: "json",
			ResponseBody:         `{"agent":{"agent_id":"agent-1","username":"pi"}}`,
		},
		{
			Seq:                  2,
			Method:               "POST",
			Path:                 "/topics",
			RequestBodyEncoding:  "json",
			RequestBody:          `{"actor_id":"actor-ops","topic":{"id":"thread-alpha","title":"Alpha"}}`,
			StatusCode:           201,
			ResponseBodyEncoding: "json",
			ResponseBody:         `{"topic":{"id":"topic_123","thread_id":"thread_999"}}`,
		},
	}, Options{SourceRecording: "fixture.jsonl"})
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Exchanges) != 1 {
		t.Fatalf("expected 1 exchange (topics only), got %d", len(run.Exchanges))
	}
	if got := run.Exchanges[0].Path; got != "/topics" {
		t.Fatalf("expected /topics, got %q", got)
	}
}

func TestCompileEntriesRejectsTruncatedRequestBody(t *testing.T) {
	t.Parallel()

	_, err := CompileEntries([]recorder.Entry{{
		Seq:                  1,
		Method:               "POST",
		Path:                 "/topics",
		RequestBodyEncoding:  "json_fragment",
		RequestBody:          `{"actor_id":"actor-ops"`,
		TruncatedRequestBody: true,
		StatusCode:           201,
		ResponseBodyEncoding: "json",
		ResponseBody:         `{"topic":{"id":"topic_123","thread_id":"thread_999"}}`,
	}}, Options{})
	if err == nil || !strings.Contains(err.Error(), "truncated request body") {
		t.Fatalf("expected truncated request body error, got %v", err)
	}
}
