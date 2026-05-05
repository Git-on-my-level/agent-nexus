package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"

	"agent-nexus-cli/internal/config"
)

func TestListCommandsAcceptPaginationFlags(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads":
			if got := r.URL.Query().Get("q"); got != "launch" {
				t.Fatalf("expected thread q=launch, got %q", got)
			}
			if got := r.URL.Query().Get("limit"); got != "25" {
				t.Fatalf("expected thread limit=25, got %q", got)
			}
			if got := r.URL.Query().Get("cursor"); got != "cursor-threads" {
				t.Fatalf("expected thread cursor=cursor-threads, got %q", got)
			}
			_, _ = w.Write([]byte(`{"threads":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/boards":
			if got := r.URL.Query().Get("q"); got != "roadmap" {
				t.Fatalf("expected board q=roadmap, got %q", got)
			}
			if got := r.URL.Query().Get("limit"); got != "30" {
				t.Fatalf("expected board limit=30, got %q", got)
			}
			if got := r.URL.Query().Get("cursor"); got != "cursor-boards" {
				t.Fatalf("expected board cursor=cursor-boards, got %q", got)
			}
			_, _ = w.Write([]byte(`{"boards":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/docs":
			if got := r.URL.Query().Get("q"); got != "constitution" {
				t.Fatalf("expected docs q=constitution, got %q", got)
			}
			if got := r.URL.Query().Get("limit"); got != "40" {
				t.Fatalf("expected docs limit=40, got %q", got)
			}
			if got := r.URL.Query().Get("cursor"); got != "cursor-docs" {
				t.Fatalf("expected docs cursor=cursor-docs, got %q", got)
			}
			_, _ = w.Write([]byte(`{"documents":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}

	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{
		"--json", "--base-url", server.URL,
		"threads", "list",
		"--q", "launch",
		"--limit", "25",
		"--cursor", "cursor-threads",
	}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{
		"--json", "--base-url", server.URL,
		"boards", "list",
		"--q", "roadmap",
		"--limit", "30",
		"--cursor", "cursor-boards",
	}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{
		"--json", "--base-url", server.URL,
		"docs", "list",
		"--q", "constitution",
		"--limit", "40",
		"--cursor", "cursor-docs",
	}))
}

func TestListCommandsRejectInvalidPaginationLimit(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	env := map[string]string{}

	testCases := []struct {
		name string
		args []string
	}{
		{
			name: "threads",
			args: []string{"--json", "threads", "list", "--limit", "0"},
		},
		{
			name: "boards",
			args: []string{"--json", "boards", "list", "--limit", "1001"},
		},
		{
			name: "docs",
			args: []string{"--json", "docs", "list", "--limit", "-1"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			payload := assertEnvelopeError(t, runCLIForTest(t, home, env, nil, tc.args))
			errObj, _ := payload["error"].(map[string]any)
			if got := anyStringValue(errObj["code"]); got != "invalid_request" {
				t.Fatalf("expected invalid_request, got %#v", payload)
			}
			if got := anyStringValue(errObj["message"]); !strings.Contains(got, "limit must be between 1 and 1000") {
				t.Fatalf("expected limit validation message, got %#v", payload)
			}
		})
	}
}

func TestRefEdgesListUsesTypedRefQueryShape(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/ref-edges" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("target_ref"); got != "card:card_123" {
			t.Fatalf("expected target_ref=card:card_123, got %q", got)
		}
		if got := r.URL.Query().Get("relation"); got != "board_card" {
			t.Fatalf("expected relation=board_card, got %q", got)
		}
		if got := r.URL.Query().Get("target_type"); got != "" || r.URL.Query().Get("target_id") != "" || r.URL.Query().Get("edge_type") != "" {
			t.Fatalf("expected no legacy ref-edge query params, got %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ref_edges":[]}`))
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json", "--base-url", server.URL,
		"ref-edges", "list",
		"--target-ref", "card:card_123",
		"--relation", "board_card",
	})
	assertEnvelopeOK(t, raw)
}

func TestRefEdgesListRequiresExactlyOneSelector(t *testing.T) {
	t.Parallel()

	home := t.TempDir()

	payload := assertEnvelopeError(t, runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json", "ref-edges", "list",
	}))
	errObj, _ := payload["error"].(map[string]any)
	if got := anyStringValue(errObj["code"]); got != "invalid_request" {
		t.Fatalf("expected invalid_request for missing selector, got %#v", payload)
	}

	payload = assertEnvelopeError(t, runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json", "ref-edges", "list",
		"--source-ref", "topic:topic_123",
		"--target-ref", "card:card_123",
	}))
	errObj, _ = payload["error"].(map[string]any)
	if got := anyStringValue(errObj["code"]); got != "invalid_request" {
		t.Fatalf("expected invalid_request for ambiguous selector, got %#v", payload)
	}
}

func TestReadCommandsAcceptANXURLs(t *testing.T) {
	t.Parallel()

	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/cards/card_123":
			_, _ = w.Write([]byte(`{"card":{"id":"card_123","title":"Card URL","summary":"body"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/docs/doc_123":
			_, _ = w.Write([]byte(`{"document":{"id":"doc_123","title":"Doc URL"},"revision":{"id":"rev_123","content":"doc body"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/artifacts/art_123":
			_, _ = w.Write([]byte(`{"artifact":{"id":"art_123","kind":"text/plain","summary":"Artifact URL"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/artifacts/art_123/content":
			_, _ = w.Write([]byte(`artifact body`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	base := server.URL
	cardURL := "https://anx.example/o/org/w/workspace/boards/board_123?card=card_123"
	docURL := "https://anx.example/o/org/w/workspace/docs/doc_123"
	artifactURL := "https://anx.example/o/org/w/workspace/artifacts/art_123"

	assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, []string{"--json", "--base-url", base, "cards", "get", cardURL}))
	assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, []string{"--json", "--base-url", base, "docs", "content", docURL}))
	assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, []string{"--json", "--base-url", base, "artifacts", "inspect", artifactURL}))

	joined := strings.Join(paths, "\n")
	for _, expected := range []string{"/cards/card_123", "/docs/doc_123", "/artifacts/art_123", "/artifacts/art_123/content"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("expected request path %s, got:\n%s", expected, joined)
		}
	}
}

func TestReadDispatchesURLsAndTypedRefs(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/cards/card_123":
			_, _ = w.Write([]byte(`{"card":{"id":"card_123","title":"Card URL","summary":"body"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/docs/doc_123":
			_, _ = w.Write([]byte(`{"document":{"id":"doc_123","title":"Doc URL"},"revision":{"id":"rev_123","content":"doc body"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	cardURL := "https://anx.example/o/org/w/workspace/boards/board_123?card=card_123"

	cardPayload := assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, []string{"--json", "--base-url", server.URL, "read", cardURL}))
	if got := anyStringValue(cardPayload["command"]); got != "read" {
		t.Fatalf("expected read command envelope, got %#v", cardPayload)
	}

	docPayload := assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, []string{"--json", "--base-url", server.URL, "read", "document:doc_123"}))
	data := asMap(docPayload["data"])
	if got := anyStringValue(data["content"]); got != "doc body" {
		t.Fatalf("expected docs content body, got %#v", docPayload)
	}
}

func TestReadRejectsAmbiguousPlainID(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	payload := assertEnvelopeError(t, runCLIForTest(t, home, nil, nil, []string{"--json", "read", "abc123"}))
	errObj := asMap(payload["error"])
	if got := anyStringValue(errObj["code"]); got != "invalid_request" {
		t.Fatalf("expected invalid_request, got %#v", payload)
	}
	if got := anyStringValue(errObj["message"]); !strings.Contains(got, "requires an ANX URL or typed ref") {
		t.Fatalf("expected URL or typed ref guidance, got %#v", payload)
	}
}

func TestCreateCommandsAppendShareableURL(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/boards"):
			_, _ = w.Write([]byte(`{"board":{"id":"board_123","title":"Board URL"}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/cards"):
			_, _ = w.Write([]byte(`{"card":{"id":"card_123","board_ref":"board:board_123","title":"Card URL"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	hostedBase := server.URL + "/ws/david-zhang/personal"
	boardOut := runCLIForTest(t, home, nil, nil, []string{"--base-url", hostedBase, "boards", "create", "--title", "Board URL"})
	if !strings.Contains(boardOut, "URL: "+server.URL+"/o/david-zhang/w/personal/boards/board_123") {
		t.Fatalf("expected board create URL, got:\n%s", boardOut)
	}

	contentFile := filepath.Join(home, "card.md")
	if err := os.WriteFile(contentFile, []byte("body\n"), 0o600); err != nil {
		t.Fatalf("write content file: %v", err)
	}
	cardOut := runCLIForTest(t, home, nil, nil, []string{"--base-url", hostedBase, "cards", "create", "--board", "board_123", "--title", "Card URL", "--content-file", contentFile})
	if !strings.Contains(cardOut, "URL: "+server.URL+"/o/david-zhang/w/personal/boards/board_123?card=card_123") {
		t.Fatalf("expected card create URL, got:\n%s", cardOut)
	}

	payload := assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, []string{"--json", "--base-url", hostedBase, "boards", "create", "--title", "Board URL"}))
	if got := anyStringValue(asMap(payload["data"])["url"]); got != server.URL+"/o/david-zhang/w/personal/boards/board_123" {
		t.Fatalf("expected structured URL, got %#v", payload)
	}
}

func TestURLCommandPrintsShareableURL(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/cards/card_123"):
			_, _ = w.Write([]byte(`{"card":{"id":"card_123","board_ref":"board:board_123","title":"Card URL"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	hostedBase := server.URL + "/ws/david-zhang/personal"
	out := strings.TrimSpace(runCLIForTest(t, home, nil, nil, []string{"--base-url", hostedBase, "url", "card", "card:card_123"}))
	expected := server.URL + "/o/david-zhang/w/personal/boards/board_123?card=card_123"
	if out != expected {
		t.Fatalf("expected %q, got %q", expected, out)
	}
}

func TestHumanAskCommandCreatesHumanAttentionRequestedEvent(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/events" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Fatalf("decode human ask body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"event":{"id":"event_ask_1","type":"human_attention_requested","thread_id":"thread_1"}}`))
	}))
	defer server.Close()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-a", `{"agent":"agent-a","username":"agent.alpha","actor_id":"actor_asker","access_token":"token-a","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json", "--base-url", server.URL, "--agent", "agent-a",
		"human", "ask", "Should we ship Friday?",
		"--thread-id", "thread_1",
		"--subject-ref", "topic:launch",
		"--ref", "artifact:receipt_1",
		"--coverage-hint", "thin - 0 decisions",
		"--recommended-response", "Ship Friday with a rollback plan ready.",
		"--proposal", "Delay until Monday for extra QA.",
	})
	payload := assertEnvelopeOK(t, raw)
	if got := anyStringValue(payload["command"]); got != "human ask" {
		t.Fatalf("expected human ask command, got %#v", payload)
	}
	if got := anyStringValue(payload["command_id"]); got != "events.create" {
		t.Fatalf("expected events.create command id, got %#v", payload)
	}

	if got := strings.TrimSpace(anyStringValue(captured["actor_id"])); got != "actor_asker" {
		t.Fatalf("expected actor_id from profile, got %#v", captured)
	}
	event, _ := captured["event"].(map[string]any)
	if got := strings.TrimSpace(anyStringValue(event["type"])); got != "human_attention_requested" {
		t.Fatalf("expected human_attention_requested type, got %#v", captured)
	}
	if got := strings.TrimSpace(anyStringValue(event["thread_id"])); got != "thread_1" {
		t.Fatalf("expected thread_1, got %#v", captured)
	}
	rawRefs, _ := event["refs"].([]any)
	refs := make([]string, 0, len(rawRefs))
	for _, raw := range rawRefs {
		refs = append(refs, strings.TrimSpace(anyStringValue(raw)))
	}
	if !hasString(refs, "thread:thread_1") || !hasString(refs, "topic:launch") || !hasString(refs, "artifact:receipt_1") {
		t.Fatalf("expected human refs to include thread/topic/artifact, got %#v", refs)
	}

	eventPayload, _ := event["payload"].(map[string]any)
	if got := strings.TrimSpace(anyStringValue(eventPayload["kind"])); got != "ask" {
		t.Fatalf("expected ask kind, got %#v", eventPayload)
	}
	if got := strings.TrimSpace(anyStringValue(eventPayload["title"])); got != "Should we ship Friday?" {
		t.Fatalf("expected title, got %#v", eventPayload)
	}
	if got := strings.TrimSpace(anyStringValue(eventPayload["requester_actor_id"])); got != "actor_asker" {
		t.Fatalf("expected requester_actor_id actor_asker, got %#v", eventPayload)
	}
	if got := strings.TrimSpace(anyStringValue(eventPayload["requester_agent_id"])); got != "agent-a" {
		t.Fatalf("expected requester_agent_id agent-a, got %#v", eventPayload)
	}
	rawProposals, _ := eventPayload["response_proposals"].([]any)
	if len(rawProposals) != 2 {
		t.Fatalf("expected two response_proposals, got %#v", eventPayload["response_proposals"])
	}
	if got := strings.TrimSpace(anyStringValue(rawProposals[0])); got != "Ship Friday with a rollback plan ready." {
		t.Fatalf("expected recommended proposal first, got %#v", rawProposals[0])
	}
}

func TestHumanCommandRequiresSubjectRef(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json", "human", "ask", "Need a decision",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if got := anyStringValue(errObj["code"]); got != "invalid_request" {
		t.Fatalf("expected invalid_request, got %#v", payload)
	}
	if got := anyStringValue(errObj["message"]); !strings.Contains(got, "--subject-ref is required") {
		t.Fatalf("expected subject ref validation message, got %#v", payload)
	}
}

func hasString(values []string, want string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == strings.TrimSpace(want) {
			return true
		}
	}
	return false
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
	if !strings.Contains(message, "valid subcommands: list, get, respond, stream, tail") {
		t.Fatalf("expected valid-subcommands guidance, got %q", message)
	}
	if !strings.Contains(message, "`anx inbox get --id <id-or-alias>`") || !strings.Contains(message, "`anx inbox respond --inbox-item-id <id-or-alias> --response-text <text>`") {
		t.Fatalf("expected concrete inbox examples, got %q", message)
	}
	if !strings.Contains(message, "did you mean `anx inbox get --id <id-or-alias>`?") {
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
		_, _ = w.Write([]byte(`{"items":[{"id":"inbox:1","thread_id":"thread_1","category":"action_needed"}]}`))
	}))
	defer server.Close()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-a", `{"agent":"agent-a","username":"agent.alpha","actor_id":"actor_123","access_token":"token-a","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "--base-url", server.URL, "--agent", "agent-a", "inbox", "get"})
	payload := assertEnvelopeOK(t, raw)
	if got := anyStringValue(payload["command"]); got != "inbox list" {
		t.Fatalf("expected alias to resolve to inbox list, got %q payload=%#v", got, payload)
	}
	data, _ := payload["data"].(map[string]any)
	viewingAs, _ := data["viewing_as"].(map[string]any)
	if got := anyStringValue(viewingAs["actor_id"]); got != "actor_123" {
		t.Fatalf("expected viewing_as actor_id actor_123, got %#v", payload)
	}
	categoryReference, _ := data["category_reference"].(map[string]any)
	if got := anyStringValue(categoryReference["action_needed"]); !strings.Contains(got, "take direct action") {
		t.Fatalf("expected category reference in alias response, got %#v", payload)
	}
	items, _ := data["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one inbox item, got %#v", payload)
	}
	item, _ := items[0].(map[string]any)
	if got := anyStringValue(item["category_description"]); !strings.Contains(got, "take direct action") {
		t.Fatalf("expected per-item category_description in alias response, got %#v", payload)
	}
}

func TestInboxListIncludesAliasesAndLinkedShortIDs(t *testing.T) {
	t.Parallel()

	const inboxID = "inbox:action_needed:thread_1234567890:none:event_1234567890"
	const threadID = "thread_1234567890"
	const eventID = "event_1234567890"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/inbox" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"` + inboxID + `","thread_id":"` + threadID + `","source_event_id":"` + eventID + `"}]}`))
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "--base-url", server.URL, "inbox", "list"})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	items, _ := data["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one item in inbox payload, got %#v", payload)
	}
	item, _ := items[0].(map[string]any)
	expectedAlias := inboxAliasByID([]string{inboxID})[inboxID]
	if got := anyStringValue(item["alias"]); got != expectedAlias {
		t.Fatalf("expected alias %q, got %q payload=%#v", expectedAlias, got, payload)
	}
	if got := anyStringValue(item["short_id"]); got != shortID(inboxID) {
		t.Fatalf("expected short_id %q, got %q payload=%#v", shortID(inboxID), got, payload)
	}
	if got := anyStringValue(item["thread_short_id"]); got != shortID(threadID) {
		t.Fatalf("expected thread_short_id %q, got %q payload=%#v", shortID(threadID), got, payload)
	}
	if got := anyStringValue(item["source_event_short_id"]); got != shortID(eventID) {
		t.Fatalf("expected source_event_short_id %q, got %q payload=%#v", shortID(eventID), got, payload)
	}
}

func TestInboxListSupportsClientSideThreadAndTypeFilters(t *testing.T) {
	t.Parallel()

	const matchingID = "inbox:action_needed:thread_1234567890:none:event_1234567890"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/inbox" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[
			{"id":"` + matchingID + `","thread_id":"thread_1234567890","type":"action_needed","summary":"needs approval"},
			{"id":"inbox:action_needed:thread_other:none:event_other","thread_id":"thread_other","type":"action_needed","summary":"other thread"},
			{"id":"inbox:review:thread_1234567890:none:event_review","thread_id":"thread_1234567890","type":"review_needed","summary":"other type"}
		]}`))
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"inbox", "list",
		"--thread-id", "thread_1234567890",
		"--type", "action_needed",
		"--full-id",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	if got := anyStringValue(data["thread_id"]); got != "thread_1234567890" {
		t.Fatalf("expected filtered thread_id, got %#v", data)
	}
	fullID, _ := data["full_id"].(bool)
	if !fullID {
		t.Fatalf("expected full_id=true, got %#v", data)
	}
	types := stringList(data["types"])
	if len(types) != 1 || types[0] != "action_needed" {
		t.Fatalf("expected filtered types, got %#v", data)
	}
	items, _ := data["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one filtered inbox item, got %#v", data)
	}
	item, _ := items[0].(map[string]any)
	if got := anyStringValue(item["id"]); got != matchingID {
		t.Fatalf("expected matching inbox item %q, got %#v", matchingID, data)
	}

	textOut := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"inbox", "list",
		"--thread-id", "thread_1234567890",
		"--type", "action_needed",
	})
	if !strings.Contains(textOut, "total_items: 3") || !strings.Contains(textOut, "returned_items: 1") {
		t.Fatalf("expected rendered inbox counts in default text output, got:\n%s", textOut)
	}
}

func TestInboxListTypeFilterRejectsLegacyCategoryAliases(t *testing.T) {
	t.Parallel()

	var (
		requestCount int
		mu           sync.Mutex
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		http.NotFound(w, r)
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"inbox", "list",
		"--thread-id", "thread_1234567890",
		"--type", "human_attention_requested",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "invalid_flags" {
		t.Fatalf("expected invalid_flags error, got %#v", payload)
	}
	if got := anyStringValue(errObj["message"]); !strings.Contains(got, "legacy inbox type/category aliases are no longer supported") || !strings.Contains(got, "human_attention_requested") {
		t.Fatalf("expected legacy alias rejection message, got %q", got)
	}
	mu.Lock()
	gotRequests := requestCount
	mu.Unlock()
	if gotRequests != 0 {
		t.Fatalf("expected no inbox request when validation fails, got %d", gotRequests)
	}
}

func TestInboxListIncludesViewingAsAndCategoryReference(t *testing.T) {
	t.Parallel()

	const inboxID = "inbox:action_needed:thread_123:none:event_123"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/inbox" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[{"id":"` + inboxID + `","thread_id":"thread_123","category":"action_needed","title":"Choose launch date"}]}`))
	}))
	defer server.Close()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-a", `{"agent":"agent-a","username":"agent.alpha","actor_id":"actor_123","access_token":"token-a","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"--agent", "agent-a",
		"inbox", "list",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	viewingAs, _ := data["viewing_as"].(map[string]any)
	if got := anyStringValue(viewingAs["profile"]); got != "agent-a" {
		t.Fatalf("expected viewing_as profile agent-a, got %#v", data)
	}
	if got := anyStringValue(viewingAs["username"]); got != "agent.alpha" {
		t.Fatalf("expected viewing_as username agent.alpha, got %#v", data)
	}
	if got := anyStringValue(viewingAs["actor_id"]); got != "actor_123" {
		t.Fatalf("expected viewing_as actor_id actor_123, got %#v", data)
	}
	categoryReference, _ := data["category_reference"].(map[string]any)
	if got := anyStringValue(categoryReference["action_needed"]); !strings.Contains(got, "take direct action") {
		t.Fatalf("expected action_needed category description, got %#v", data)
	}
	items, _ := data["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one inbox item, got %#v", data)
	}
	item, _ := items[0].(map[string]any)
	if got := anyStringValue(item["category_description"]); !strings.Contains(got, "take direct action") {
		t.Fatalf("expected item category_description, got %#v", item)
	}

	textOut := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"--agent", "agent-a",
		"inbox", "list",
	})
	if !strings.Contains(textOut, "viewing_as: profile=agent-a :: username=agent.alpha :: actor_id=actor_123") {
		t.Fatalf("expected viewing_as summary in default text output, got:\n%s", textOut)
	}
	if !strings.Contains(textOut, "category_reference:") || !strings.Contains(textOut, "action_needed: A responsible actor must take direct action or own the next step.") {
		t.Fatalf("expected category reference in default text output, got:\n%s", textOut)
	}
}

func TestInboxAliasStableAcrossListMembershipChanges(t *testing.T) {
	t.Parallel()

	const targetID = "inbox:action_needed:thread_target:none:event_target"
	const otherID = "inbox:action_needed:thread_other:none:event_other"

	aliasSingle := inboxAliasByID([]string{targetID})[targetID]
	aliasWithOther := inboxAliasByID([]string{targetID, otherID})[targetID]
	if aliasSingle != aliasWithOther {
		t.Fatalf("expected alias to remain stable across list membership changes, single=%q with_other=%q", aliasSingle, aliasWithOther)
	}
	if !strings.HasPrefix(aliasSingle, inboxAliasPrefix) {
		t.Fatalf("expected alias prefix %q, got %q", inboxAliasPrefix, aliasSingle)
	}
	if len(aliasSingle) != len(inboxAliasPrefix)+inboxAliasDigestLength {
		t.Fatalf("expected alias length %d, got %d alias=%q", len(inboxAliasPrefix)+inboxAliasDigestLength, len(aliasSingle), aliasSingle)
	}
}

func TestInboxRespondPostsGenericResponse(t *testing.T) {
	t.Parallel()

	const inboxID = "inbox:ask:thread_42:none:event_42"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/inbox/"+url.PathEscape(inboxID)+"/respond":
			body, _ := io.ReadAll(r.Body)
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("decode inbox respond body: %v body=%s", err, string(body))
			}
			if _, exists := payload["inbox_item_id"]; exists {
				t.Fatalf("expected inbox_item_id in path only, got body=%s", string(body))
			}
			if got := strings.TrimSpace(anyStringValue(payload["response_text"])); got != "Approved." {
				t.Fatalf("expected response_text Approved., got %q body=%s", got, string(body))
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"event":{"id":"event_response"}}`))
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
		"inbox", "respond",
		"--inbox-item-id", inboxID,
		"--response-text", "Approved.",
	})
	assertEnvelopeOK(t, raw)
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
	if !strings.Contains(message, "valid subcommands: list, get, create, validate, stream, tail, explain") {
		t.Fatalf("expected valid subcommands in message, got %q", message)
	}
	if !strings.Contains(message, "did you mean `anx events stream`?") {
		t.Fatalf("expected stream correction, got %q", message)
	}
	if !strings.Contains(message, "`anx events list --thread-id <thread-id> --type message_posted --mine --full-id`") || !strings.Contains(message, "`anx events tail --max-events 20`") {
		t.Fatalf("expected list/tail examples, got %q", message)
	}
}

func TestEventsListCommandFiltersAndLimits(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/threads/thread_1/timeline" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"events":[
				{"id":"event_1","thread_id":"thread_1","type":"message_posted","summary":"first"},
				{"id":"event_2","thread_id":"thread_1","type":"human_attention_requested","summary":"second"},
				{"id":"event_3","thread_id":"thread_1","type":"message_posted","summary":"third"}
			],
			"artifacts":{}
		}`))
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"events", "list",
		"--thread-id", "thread_1",
		"--type", "message_posted",
		"--max-events", "1",
	})
	payload := assertEnvelopeOK(t, raw)
	if got := anyStringValue(payload["command"]); got != "events list" {
		t.Fatalf("unexpected command label: %#v", payload)
	}

	data, _ := payload["data"].(map[string]any)
	if got := anyStringValue(data["thread_id"]); got != "thread_1" {
		t.Fatalf("expected thread id thread_1, got %#v", data)
	}
	totalEvents, _ := data["total_events"].(float64)
	if int(totalEvents) != 3 {
		t.Fatalf("expected total_events=3, got %#v", data)
	}
	returnedEvents, _ := data["returned_events"].(float64)
	if int(returnedEvents) != 1 {
		t.Fatalf("expected returned_events=1, got %#v", data)
	}
	events, _ := data["events"].([]any)
	if len(events) != 1 {
		t.Fatalf("expected one event after filtering/limit, got %#v", data)
	}
	event, _ := events[0].(map[string]any)
	if got := anyStringValue(event["id"]); got != "event_3" {
		t.Fatalf("expected most recent matching event event_3, got %#v", data)
	}

	textOut := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"events", "list",
		"--thread-id", "thread_1",
		"--type", "message_posted",
		"--max-events", "1",
	})
	if !strings.Contains(textOut, "types:") || !strings.Contains(textOut, "- message_posted") {
		t.Fatalf("expected default text output to include selected filter types, got:\n%s", textOut)
	}
}

func TestEventsListCommandAppliesTaxonomyFiltersToThreadTimeline(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/threads/thread_1/timeline" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"events":[
				{"id":"event_1","thread_id":"thread_1","type":"message_posted","summary":"standalone message"},
				{"id":"event_2","thread_id":"thread_1","type":"document_created","summary":"backing document"},
				{"id":"event_3","thread_id":"thread_1","type":"card_created","summary":"backing card"}
			],
			"artifacts":{"artifact_1":{"id":"artifact_1","kind":"doc"}}
		}`))
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"events", "list",
		"--thread-id", "thread_1",
		"--event-group", "documents",
		"--backing-scope", "backing_only",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	events, _ := data["events"].([]any)
	if len(events) != 1 {
		t.Fatalf("expected one event after event-group/backing-scope filters, got %#v", data)
	}
	event, _ := events[0].(map[string]any)
	if got := anyStringValue(event["id"]); got != "event_2" {
		t.Fatalf("expected document backing event, got %#v", data)
	}
	groups := stringList(data["event_groups"])
	if len(groups) != 1 || groups[0] != "documents" {
		t.Fatalf("expected event_groups=documents, got %#v", data)
	}
	if got := anyStringValue(data["backing_scope"]); got != "backing_only" {
		t.Fatalf("expected backing_scope=backing_only, got %#v", data)
	}
}

func TestEventsListMaxFlagAliasMatchesMaxEvents(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/threads/thread_1/timeline" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"events":[
				{"id":"event_1","thread_id":"thread_1","type":"message_posted","summary":"first"},
				{"id":"event_2","thread_id":"thread_1","type":"message_posted","summary":"second"}
			],
			"artifacts":{}
		}`))
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"events", "list",
		"--thread-id", "thread_1",
		"--type", "message_posted",
		"--max", "1",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	returnedEvents, _ := data["returned_events"].(float64)
	if int(returnedEvents) != 1 {
		t.Fatalf("expected returned_events=1 with --max alias, got %#v", data)
	}
}

func TestEventsListCommandSupportsMineActorFilterAndFullID(t *testing.T) {
	t.Parallel()

	const mineEventID = "event_1234567890abcdef"
	const mineActorID = "actor-profile-1"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/threads/thread_1/timeline" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"events":[
				{"id":"` + mineEventID + `","thread_id":"thread_1","type":"message_posted","actor_id":"` + mineActorID + `","payload":{"recommendation":"Ship Friday rescue scope"}},
				{"id":"event_other_actor","thread_id":"thread_1","type":"message_posted","actor_id":"actor-other","summary":"other recommendation"}
			],
			"artifacts":{}
		}`))
	}))
	defer server.Close()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-a", `{"agent":"agent-a","actor_id":"`+mineActorID+`","access_token":"token-a","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"--agent", "agent-a",
		"events", "list",
		"--thread-id", "thread_1",
		"--type", "message_posted",
		"--mine",
		"--full-id",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	if got := anyStringValue(data["actor_id"]); got != mineActorID {
		t.Fatalf("expected actor_id filter %q, got %#v", mineActorID, data)
	}
	if fullID, _ := data["full_id"].(bool); !fullID {
		t.Fatalf("expected full_id=true, got %#v", data)
	}
	events, _ := data["events"].([]any)
	if len(events) != 1 {
		t.Fatalf("expected one filtered event, got %#v", data)
	}
	event, _ := events[0].(map[string]any)
	if got := anyStringValue(event["id"]); got != mineEventID {
		t.Fatalf("unexpected event id after mine filter: %#v", data)
	}
	if got := anyStringValue(event["summary_preview"]); !strings.Contains(got, "Ship Friday rescue scope") {
		t.Fatalf("expected payload preview summary, got %#v", event)
	}

	textFull := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"--agent", "agent-a",
		"events", "list",
		"--thread-id", "thread_1",
		"--type", "message_posted",
		"--mine",
		"--full-id",
	})
	if !strings.Contains(textFull, mineEventID) || !strings.Contains(textFull, "Ship Friday rescue scope") {
		t.Fatalf("expected full id + preview in default text output, got:\n%s", textFull)
	}

	textShort := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"--agent", "agent-a",
		"events", "list",
		"--thread-id", "thread_1",
		"--type", "message_posted",
		"--mine",
	})
	if strings.Contains(textShort, mineEventID) {
		t.Fatalf("expected default short-id rendering without --full-id, got:\n%s", textShort)
	}
	if !strings.Contains(textShort, shortID(mineEventID)) {
		t.Fatalf("expected short id rendering by default, got:\n%s", textShort)
	}
}

func TestEventsListCommandSupportsMultipleThreadIDs(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requested := make([]string, 0, 2)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/timeline") {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		requested = append(requested, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/threads/thread_1/timeline":
			_, _ = w.Write([]byte(`{"thread_id":"thread_1","events":[
				{"id":"event_1","thread_id":"thread_1","type":"message_posted","summary":"first","ts":"2026-03-06T12:01:00Z","created_at":"2026-03-06T12:10:00Z"},
				{"id":"event_2","thread_id":"thread_1","type":"message_posted","summary":"second","ts":"2026-03-06T12:02:00Z","created_at":"2026-03-06T12:11:00Z"}
			],"artifacts":{"artifact_1":{"id":"artifact_1","kind":"note"}}}`))
		case "/threads/thread_2/timeline":
			_, _ = w.Write([]byte(`{"thread_id":"thread_2","events":[
				{"id":"event_3","thread_id":"thread_2","type":"message_posted","summary":"third","ts":"2026-03-06T12:03:00Z","created_at":"2026-03-06T12:00:00Z"},
				{"id":"event_4","thread_id":"thread_2","type":"message_posted","summary":"fourth","ts":"2026-03-06T12:04:00Z","created_at":"2026-03-06T12:01:00Z"}
			],"artifacts":{"artifact_2":{"id":"artifact_2","kind":"report"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"events", "list",
		"--thread-id", "thread_1",
		"--thread-id", "thread_2",
		"--type", "message_posted",
		"--max-events", "2",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	totalEvents, _ := data["total_events"].(float64)
	if int(totalEvents) != 4 {
		t.Fatalf("expected total_events=4, got %#v", data)
	}
	threadIDs := stringList(data["thread_ids"])
	if len(threadIDs) != 2 || threadIDs[0] != "thread_1" || threadIDs[1] != "thread_2" {
		t.Fatalf("expected thread_ids [thread_1 thread_2], got %#v", data)
	}
	events, _ := data["events"].([]any)
	if len(events) != 2 {
		t.Fatalf("expected max_events to cap result set, got %#v", data)
	}
	first, _ := events[0].(map[string]any)
	second, _ := events[1].(map[string]any)
	if anyStringValue(first["id"]) != "event_3" || anyStringValue(second["id"]) != "event_4" {
		t.Fatalf("expected most recent cross-thread events, got %#v", data)
	}
	artifacts, _ := data["artifacts"].(map[string]any)
	if len(artifacts) != 2 {
		t.Fatalf("expected merged timeline artifacts, got %#v", data["artifacts"])
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requested) != 2 {
		t.Fatalf("expected one timeline request per thread id, got %d (%v)", len(requested), requested)
	}
}

func TestDocsCommands(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/docs":
			states := r.URL.Query()["state"]
			if len(states) != 2 || states[0] != "active" || states[1] != "trashed" {
				t.Fatalf("expected state=active&state=trashed for --include-trashed, got %#v", states)
			}
			if got := strings.TrimSpace(r.URL.Query().Get("thread_id")); got != "thread_docs_1" {
				t.Fatalf("expected thread_id=thread_docs_1 query, got %q", got)
			}
			_, _ = w.Write([]byte(`{"documents":[{"id":"doc_1","head_revision_id":"rev_1"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/docs":
			body, _ := io.ReadAll(r.Body)
			if !bytes.Contains(body, []byte(`"document"`)) {
				t.Fatalf("unexpected docs create body: %s", string(body))
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"document":{"id":"doc_1","head_revision_id":"rev_1"},"revision":{"revision_id":"rev_1","revision_number":1}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/docs/doc_1":
			_, _ = w.Write([]byte(`{"document":{"id":"doc_1","head_revision_id":"rev_1"},"revision":{"revision_id":"rev_1","revision_number":1,"content":"initial","content_type":"text"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/docs/doc_1/revisions":
			body, _ := io.ReadAll(r.Body)
			if !bytes.Contains(body, []byte(`"if_base_revision":"rev_1"`)) || !bytes.Contains(body, []byte(`"content":"next"`)) || !bytes.Contains(body, []byte(`"content_type":"text"`)) {
				t.Fatalf("unexpected docs revise body: %s", string(body))
			}
			_, _ = w.Write([]byte(`{"document":{"id":"doc_1","head_revision_id":"rev_2"},"revision":{"revision_id":"rev_2","revision_number":2}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/docs/doc_1/revisions":
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

	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "docs", "list", "--thread-id", "thread_docs_1", "--include-trashed"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"document":{"id":"doc_1"},"content":"initial","content_type":"text"}`), []string{"--json", "--base-url", server.URL, "docs", "create"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "docs", "get", "--document-id", "doc_1"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"actor_id":"actor_test","if_base_revision":"rev_1","content":"next","content_type":"text"}`), []string{"--json", "--base-url", server.URL, "docs", "revise", "--apply", "--document-id", "doc_1"}))
	docsRevisionPayload := assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"actor_id":"actor_test","if_base_revision":"rev_1","content":"next","content_type":"text"}`), []string{"--json", "--base-url", server.URL, "docs", "revise", "--document-id", "doc_1"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "docs", "revise", "--apply", "--proposal-id", proposalIDFromEnvelope(t, docsRevisionPayload)}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "docs", "history", "--document-id", "doc_1"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "docs", "revision", "get", "--document-id", "doc_1", "--revision-id", "rev_1"}))
}

func TestDocsReviseInjectsActorIDFromProfile(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/docs/doc_1" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"document":{"id":"doc_1","head_revision_id":"rev_1"},"revision":{"revision_id":"rev_1","revision_number":1,"content":"initial","content_type":"text"}}`))
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/docs/doc_1/revisions" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode docs revise body: %v body=%s", err, string(body))
		}
		if got := strings.TrimSpace(anyStringValue(payload["actor_id"])); got != "actor-profile-docs" {
			t.Fatalf("expected actor_id from profile, got %q body=%s", got, string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"document":{"id":"doc_1","head_revision_id":"rev_2"},"revision":{"revision_id":"rev_2","revision_number":2}}`))
	}))
	defer server.Close()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-docs", `{"agent":"agent-docs","actor_id":"actor-profile-docs","access_token":"token-docs","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	raw := runCLIForTest(t, home, map[string]string{}, strings.NewReader(`{"if_base_revision":"rev_1","content":"next","content_type":"text"}`), []string{
		"--json",
		"--base-url", server.URL,
		"--agent", "agent-docs",
		"docs", "revise", "--apply",
		"--document-id", "doc_1",
	})
	assertEnvelopeOK(t, raw)
}

func TestDocsReviseRequiresActiveActorIdentity(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		http.NotFound(w, r)
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, strings.NewReader(`{"if_base_revision":"rev_1","content":"next","content_type":"text"}`), []string{
		"--json",
		"--base-url", server.URL,
		"docs", "revise", "--apply",
		"--document-id", "doc_1",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "invalid_request" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	message := anyStringValue(errObj["message"])
	if !strings.Contains(message, "No active actor identity") {
		t.Fatalf("expected missing actor identity guidance, got %q payload=%#v", message, payload)
	}
	if !strings.Contains(message, "anx auth register --username <name>") || !strings.Contains(message, "anx auth whoami") {
		t.Fatalf("expected actionable auth guidance, got %q payload=%#v", message, payload)
	}

	mu.Lock()
	gotRequests := requestCount
	mu.Unlock()
	if gotRequests != 0 {
		t.Fatalf("expected no HTTP requests when actor identity is missing, got %d", gotRequests)
	}
}

func TestProductManagerFlowRegisterThenDocsRevise(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	docsRevisionCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/meta/handshake":
			_, _ = w.Write([]byte(`{"core_instance_id":"fake-core","min_cli_version":"0.1.0"}`))
			return
		case r.Method == http.MethodPost && r.URL.Path == "/auth/agents/register":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"agent": map[string]any{
					"agent_id": "agent-product-manager",
					"actor_id": "actor-product-manager",
					"username": "pi-dogfood-agent-product-manager",
				},
				"key": map[string]any{
					"key_id": "key-product-manager",
				},
				"tokens": map[string]any{
					"access_token":  "token-product-manager",
					"refresh_token": "refresh-product-manager",
					"token_type":    "Bearer",
					"expires_in":    300,
				},
			})
			return
		case r.Method == http.MethodGet && r.URL.Path == "/docs/northwave-pilot-rescue-brief":
			_, _ = w.Write([]byte(`{"document":{"id":"northwave-pilot-rescue-brief","head_revision_id":"rev_1"},"revision":{"revision_id":"rev_1","revision_number":1,"content":"initial brief","content_type":"text"}}`))
			return
		case r.Method == http.MethodPost && r.URL.Path == "/docs/northwave-pilot-rescue-brief/revisions":
			if gotAuth := strings.TrimSpace(r.Header.Get("Authorization")); gotAuth != "Bearer token-product-manager" {
				t.Fatalf("expected auth bearer token, got %q", gotAuth)
			}
			body, _ := io.ReadAll(r.Body)
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("decode docs revise body: %v body=%s", err, string(body))
			}
			if got := strings.TrimSpace(anyStringValue(payload["actor_id"])); got != "actor-product-manager" {
				t.Fatalf("expected actor_id from registered profile, got %q body=%s", got, string(body))
			}
			mu.Lock()
			docsRevisionCalls++
			mu.Unlock()
			_, _ = w.Write([]byte(`{"document":{"id":"northwave-pilot-rescue-brief","head_revision_id":"rev_2"},"revision":{"revision_id":"rev_2","revision_number":2}}`))
			return
		default:
			http.NotFound(w, r)
			return
		}
	}))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}

	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{
		"--json",
		"--base-url", server.URL,
		"--agent", "agent-product-manager",
		"auth", "register",
		"--username", "pi-dogfood-agent-product-manager",
	}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"if_base_revision":"rev_1","content":"updated brief","content_type":"text"}`), []string{
		"--json",
		"--base-url", server.URL,
		"--agent", "agent-product-manager",
		"docs", "revise", "--apply",
		"--document-id", "northwave-pilot-rescue-brief",
	}))

	mu.Lock()
	gotCalls := docsRevisionCalls
	mu.Unlock()
	if gotCalls != 1 {
		t.Fatalf("expected one docs revise request, got %d", gotCalls)
	}
}

func TestDocsContentCommand(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/docs/doc_1" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"document":{"id":"doc_1","title":"Playbook"},
			"revision":{"revision_id":"rev_2","revision_number":2,"content_type":"text","content":"Line one\nLine two"}
		}`))
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"docs", "content",
		"--document-id", "doc_1",
	})
	payload := assertEnvelopeOK(t, raw)
	if got := anyStringValue(payload["command"]); got != "docs content" {
		t.Fatalf("unexpected command label: %#v", payload)
	}
	data, _ := payload["data"].(map[string]any)
	if got := anyStringValue(data["content"]); got != "Line one\nLine two" {
		t.Fatalf("expected document content, got %#v", data)
	}
}

func TestDocsMessagesCommand(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.NotFound(w, r)
			return
		}
		switch r.URL.Path {
		case "/docs/doc_1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"document":{"id":"doc_1","thread_id":"thread_1","title":"Spec"},
				"revision":{"revision_id":"rev_1","revision_number":1}
			}`))
		case "/threads/thread_1/timeline":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"thread_id":"thread_1",
				"events":[
					{
						"id":"ev_other","thread_id":"thread_1","ts":"2026-02-01T00:00:00Z",
						"type":"message_posted",
						"refs":["thread:thread_1","document:other","document_revision:rev_1"],
						"payload":{"text":"nope","kind":"document_text_comment",
							"document_comment":{"document_id":"other","selected_text":"x","revision_id":"r0"}
						}
					},
					{
						"id":"ev_1","thread_id":"thread_1","ts":"2026-02-02T00:00:00Z",
						"type":"message_posted",
						"actor_id":"act_1",
						"refs":["thread:thread_1","document:doc_1","document_revision:rev_1"],
						"payload":{
							"text":"Please expand",
							"kind":"document_text_comment",
							"document_comment":{
								"document_id":"doc_1",
								"revision_id":"rev_1",
								"content_hash":"ab",
								"selected_text":"Line two",
								"anchor_status":"current"
							}
						}
					},
					{
						"id":"ev_trash","thread_id":"thread_1","ts":"2026-02-03T00:00:00Z",
						"trashed_at":"2026-01-01T00:00:00Z",
						"type":"message_posted",
						"refs":["thread:thread_1","document:doc_1","document_revision:rev_1"],
						"payload":{"text":"gone","kind":"document_text_comment",
							"document_comment":{"document_id":"doc_1","selected_text":"t","revision_id":"rev_1"}}
					}
				],
				"artifacts":{}
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"docs", "messages",
		"--document-id", "doc_1",
	})
	payload := assertEnvelopeOK(t, raw)
	if got := anyStringValue(payload["command"]); got != "docs messages" {
		t.Fatalf("unexpected command label: %#v", payload)
	}
	flat, _ := payload["data"].(map[string]any)
	events, _ := flat["events"].([]any)
	if len(events) != 1 {
		t.Fatalf("expected 1 non-trashed document message, got %d: %#v", len(events), flat)
	}
	first, _ := events[0].(map[string]any)
	if anyStringValue(first["id"]) != "ev_1" {
		t.Fatalf("unexpected row: %#v", first)
	}

	// --include-trashed: expect trashed event as well
	raw2 := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"docs", "messages",
		"--document-id", "doc_1",
		"--include-trashed",
	})
	p2 := assertEnvelopeOK(t, raw2)
	f2, _ := p2["data"].(map[string]any)
	c2, _ := f2["events"].([]any)
	if len(c2) != 2 {
		t.Fatalf("expected 2 with include-trashed, got %d", len(c2))
	}

	textOut := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"docs", "messages",
		"doc_1",
	})
	if !strings.Contains(textOut, "ev_1") || !strings.Contains(textOut, "Document messages") {
		t.Fatalf("expected default text for docs messages, got:\n%s", textOut)
	}
}

func TestDocsMessageBuildsThreadScopedEventFromDocumentBackingThread(t *testing.T) {
	t.Parallel()

	const (
		documentID   = "doc_message_123456"
		threadID     = "thread_doc_message_123456"
		profileActor = "actor_profile_doc_message"
		eventID      = "event_doc_message_123456"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/docs/"+documentID:
			_, _ = w.Write([]byte(`{"document":{"id":"` + documentID + `","title":"Message doc","thread_id":"` + threadID + `"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/events":
			var posted map[string]any
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				t.Fatalf("decode docs message body: %v", err)
			}
			assertMessagePostedMutation(t, posted, profileActor, threadID, []string{"document:" + documentID, "thread:" + threadID}, "Reviewed via domain command.")
			event, _ := posted["event"].(map[string]any)
			payload, _ := event["payload"].(map[string]any)
			if got := anyStringValue(payload["kind"]); got != "document_message" {
				t.Fatalf("expected document_message payload kind, got %#v", posted)
			}
			if got := anyStringValue(payload["subject_ref"]); got != "document:"+documentID {
				t.Fatalf("expected document subject_ref, got %#v", posted)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"event":{"id":"` + eventID + `","type":"message_posted","thread_id":"` + threadID + `","refs":["document:` + documentID + `","thread:` + threadID + `"]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-doc-message", `{"agent":"agent-doc-message","actor_id":"`+profileActor+`","access_token":"token","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	raw := runCLIForTest(t, home, nil, nil, []string{
		"--json", "--base-url", server.URL, "--agent", "agent-doc-message",
		"docs", "message", documentID, "--body", "Reviewed via domain command.",
	})
	payload := assertEnvelopeOK(t, raw)
	if got := anyStringValue(payload["command"]); got != "docs message" {
		t.Fatalf("expected docs message command, got %#v", payload)
	}
	if got := anyStringValue(payload["command_id"]); got != "events.create" {
		t.Fatalf("expected events.create command_id, got %#v", payload)
	}
	data, _ := payload["data"].(map[string]any)
	if got := anyStringValue(data["thread_id"]); got != threadID {
		t.Fatalf("expected response thread_id %q, got %#v", threadID, data)
	}
}

func TestDocsCreateDryRunValidatesPayloadBeforeSuccess(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		http.NotFound(w, r)
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, strings.NewReader(`{}`), []string{
		"--json",
		"--base-url", server.URL,
		"docs", "create",
		"--dry-run",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "invalid_request" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	message := anyStringValue(errObj["message"])
	if !strings.Contains(message, "document is required") || !strings.Contains(message, "content is required") || !strings.Contains(message, "content_type") {
		t.Fatalf("expected docs create validation guidance, got %q payload=%#v", message, payload)
	}

	mu.Lock()
	gotRequests := requestCount
	mu.Unlock()
	if gotRequests != 0 {
		t.Fatalf("expected no HTTP request for invalid dry-run payload, got %d", gotRequests)
	}
}

func TestDocsCreateFromFlagsAndContentFile(t *testing.T) {
	t.Parallel()

	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/docs" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Fatalf("decode docs create body: %v body=%s", err, string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"document":{"id":"doc_1","head_revision_id":"rev_1"},"revision":{"revision_id":"rev_1","revision_number":1}}`))
	}))
	defer server.Close()

	home := t.TempDir()
	contentFile := filepath.Join(home, "runbook.md")
	if err := os.WriteFile(contentFile, []byte("# Runbook\n\nDurable context.\n"), 0o600); err != nil {
		t.Fatalf("write content file: %v", err)
	}

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"docs", "create",
		"--topic", "topic_1",
		"--title", "Runbook",
		"--summary", "Durable context",
		"--content-file", contentFile,
	})
	assertEnvelopeOK(t, raw)

	document, _ := gotBody["document"].(map[string]any)
	if got := anyStringValue(document["title"]); got != "Runbook" {
		t.Fatalf("expected title from flags, got %q body=%#v", got, gotBody)
	}
	if got := anyStringValue(document["subject_ref"]); got != "topic:topic_1" {
		t.Fatalf("expected subject_ref topic:topic_1, got %q body=%#v", got, gotBody)
	}
	if got := anyStringValue(gotBody["content_type"]); got != "text" {
		t.Fatalf("expected content_type=text, got %q body=%#v", got, gotBody)
	}
	if got := anyStringValue(gotBody["content"]); !strings.Contains(got, "Durable context.") {
		t.Fatalf("expected content from file, got %q body=%#v", got, gotBody)
	}
}

func TestDocsMutationNormalizesTextMarkdownContentType(t *testing.T) {
	t.Parallel()

	var gotCreate, gotRevise []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/docs":
			gotCreate, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"document":{"id":"doc_md","head_revision_id":"rev_1"},"revision":{"revision_id":"rev_1","revision_number":1}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/docs/doc_md/revisions":
			gotRevise, _ = io.ReadAll(r.Body)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"document":{"id":"doc_md","head_revision_id":"rev_2"},"revision":{"revision_id":"rev_2","revision_number":2}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}

	assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"document":{"id":"doc_md","title":"T"},"content":"# Hello","content_type":"text/markdown"}`), []string{"--json", "--base-url", server.URL, "docs", "create"}))
	var createBody map[string]any
	if err := json.Unmarshal(gotCreate, &createBody); err != nil {
		t.Fatalf("decode create body: %v", err)
	}
	if got := anyStringValue(createBody["content_type"]); got != "text" {
		t.Fatalf("docs create: expected content_type normalized to text, got %q", got)
	}

	assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"actor_id":"actor_test","if_base_revision":"rev_1","content":"next","content_type":"text/markdown; charset=utf-8"}`), []string{"--json", "--base-url", server.URL, "docs", "revise", "--apply", "--document-id", "doc_md"}))
	var reviseBody map[string]any
	if err := json.Unmarshal(gotRevise, &reviseBody); err != nil {
		t.Fatalf("decode revise body: %v", err)
	}
	if got := anyStringValue(reviseBody["content_type"]); got != "text" {
		t.Fatalf("docs revise: expected content_type normalized to text, got %q", got)
	}
}

func TestDocsReviseRejectsNullContentBeforeHTTP(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		http.NotFound(w, r)
	}))
	defer server.Close()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-docs-null-content", `{"agent":"agent-docs-null-content","actor_id":"actor-docs-null-content","access_token":"token-docs","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
	raw := runCLIForTest(t, home, map[string]string{}, strings.NewReader(`{"if_base_revision":"rev_1","content":null,"content_type":"text"}`), []string{
		"--json",
		"--base-url", server.URL,
		"--agent", "agent-docs-null-content",
		"docs", "revise", "--apply",
		"--document-id", "doc_1",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "invalid_request" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	if message := anyStringValue(errObj["message"]); !strings.Contains(message, "content is required") {
		t.Fatalf("expected content validation guidance, got %q payload=%#v", message, payload)
	}

	mu.Lock()
	gotRequests := requestCount
	mu.Unlock()
	if gotRequests != 0 {
		t.Fatalf("expected no HTTP request for invalid proposal payload, got %d", gotRequests)
	}
}

func TestDocsReviseWithContentFileUsesFetchedDocumentState(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	getCount := 0
	patchCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/docs/doc_1":
			getCount++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"document":{"id":"doc_1","head_revision_id":"rev_1"},"revision":{"revision_id":"rev_1","revision_number":1,"content":"old content","content_type":"text"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/docs/doc_1/revisions":
			patchCount++
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	updateFile := filepath.Join(home, "doc-update.json")
	contentFile := filepath.Join(home, "doc-content.md")
	if err := os.WriteFile(updateFile, []byte(`{"if_base_revision":"rev_1","content_type":"text"}`), 0o600); err != nil {
		t.Fatalf("write update file: %v", err)
	}
	content := "line 1\nline 2\n"
	if err := os.WriteFile(contentFile, []byte(content), 0o600); err != nil {
		t.Fatalf("write content file: %v", err)
	}
	writeAgentProfile(t, home, "agent-docs-content-file", `{"agent":"agent-docs-content-file","actor_id":"actor-docs-content-file","access_token":"token-docs","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"--agent", "agent-docs-content-file",
		"docs", "revise",
		"--document-id", "doc_1",
		"--from-file", updateFile,
		"--content-file", contentFile,
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	if got := anyStringValue(data["path"]); got != "/docs/doc_1/revisions" {
		t.Fatalf("expected path /docs/doc_1/revisions, got %q payload=%#v", got, payload)
	}
	body, _ := data["body"].(map[string]any)
	if got := anyStringValue(body["content"]); got != strings.TrimSpace(content) {
		t.Fatalf("expected content-file override in proposal content, got %q payload=%#v", got, payload)
	}
	diff, _ := data["diff"].(map[string]any)
	if diffText := anyStringValue(diff["text"]); !strings.Contains(diffText, "line 1") {
		t.Fatalf("expected unified diff text in proposal payload, got %#v", data)
	}

	mu.Lock()
	gotGets := getCount
	gotPatches := patchCount
	mu.Unlock()
	if gotGets != 1 {
		t.Fatalf("expected one docs get request during proposal staging, got %d", gotGets)
	}
	if gotPatches != 0 {
		t.Fatalf("expected no docs patch request during proposal staging, got %d", gotPatches)
	}
}

func TestDocsReviseWithOnlyContentFileDiscoversBaseRevision(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/docs/doc_1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"document":{"id":"doc_1","head_revision_id":"rev_1"},"revision":{"revision_id":"rev_1","revision_number":1,"content":"old content","content_type":"text"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	contentFile := filepath.Join(home, "doc-content.md")
	if err := os.WriteFile(contentFile, []byte("new content\n"), 0o600); err != nil {
		t.Fatalf("write content file: %v", err)
	}
	writeAgentProfile(t, home, "agent-docs-content-only", `{"agent":"agent-docs-content-only","actor_id":"actor-docs-content-only","access_token":"token-docs","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"--agent", "agent-docs-content-only",
		"docs", "revise",
		"--document-id", "doc_1",
		"--content-file", contentFile,
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	body, _ := data["body"].(map[string]any)
	if got := anyStringValue(body["if_base_revision"]); got != "rev_1" {
		t.Fatalf("expected discovered base revision, got %q payload=%#v", got, payload)
	}
	if got := anyStringValue(body["content"]); got != "new content" {
		t.Fatalf("expected content-file markdown, got %q payload=%#v", got, payload)
	}
}

func TestDocsRevisePreservesStructuredContentInDiff(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/docs/doc_structured":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"document":{"id":"doc_structured","head_revision_id":"rev_1"},
				"revision":{
					"revision_id":"rev_1",
					"revision_number":1,
					"content_type":"structured",
					"content":{"summary":"Initial brief","status":"draft","items":["alpha"]}
				}
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-docs-structured", `{"agent":"agent-docs-structured","actor_id":"actor-docs-structured","access_token":"token-docs","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	raw := runCLIForTest(t, home, map[string]string{}, strings.NewReader(`{
		"if_base_revision":"rev_1",
		"content_type":"structured",
		"content":{"summary":"Updated brief","status":"approved","items":["alpha","beta"]}
	}`), []string{
		"--json",
		"--base-url", server.URL,
		"--agent", "agent-docs-structured",
		"docs", "revise",
		"--document-id", "doc_structured",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	body, _ := data["body"].(map[string]any)
	content, _ := body["content"].(map[string]any)
	if got := anyStringValue(content["status"]); got != "approved" {
		t.Fatalf("expected structured content in staged proposal body, got %#v", body["content"])
	}
	diff, _ := data["diff"].(map[string]any)
	diffText := anyStringValue(diff["text"])
	if strings.Contains(diffText, "(no changes)") {
		t.Fatalf("expected structured proposal diff to show changes, got %q", diffText)
	}
	if !strings.Contains(diffText, `"status": "draft"`) || !strings.Contains(diffText, `"status": "approved"`) {
		t.Fatalf("expected structured proposal diff to preserve content changes, got %q", diffText)
	}
	if !strings.Contains(diffText, `"items": [`) || !strings.Contains(diffText, `"beta"`) {
		t.Fatalf("expected structured proposal diff to include nested array changes, got %q", diffText)
	}
}

func TestDocsReviseTextDiffFallsBackWhenRevisionContentEmpty(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/docs/doc_text_fallback":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"document":{"id":"doc_text_fallback","head_revision_id":"rev_1"},
				"content":"body fallback content",
				"revision":{
					"revision_id":"rev_1",
					"revision_number":1,
					"content_type":"text",
					"content":""
				}
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-docs-text-fallback", `{"agent":"agent-docs-text-fallback","actor_id":"actor-docs-text-fallback","access_token":"token-docs","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	raw := runCLIForTest(t, home, map[string]string{}, strings.NewReader(`{
		"if_base_revision":"rev_1",
		"content_type":"text",
		"content":"updated body content"
	}`), []string{
		"--json",
		"--base-url", server.URL,
		"--agent", "agent-docs-text-fallback",
		"docs", "revise",
		"--document-id", "doc_text_fallback",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	diff, _ := data["diff"].(map[string]any)
	diffText := anyStringValue(diff["text"])
	if !strings.Contains(diffText, "-body fallback content") || !strings.Contains(diffText, "+updated body content") {
		t.Fatalf("expected text proposal diff to fall back to body content when revision content is empty, got %q", diffText)
	}
}

func TestCommitmentsCommandsAreRemoved(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, strings.NewReader(`{"patch":{"resolution":"done"}}`), []string{
		"--json",
		"commitments", "patch",
		"--commitment-id", "commitment_1",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "unknown_command" {
		t.Fatalf("expected removed commitments patch command to fail, payload=%#v", payload)
	}

	raw = runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"commitments", "propose-patch",
		"--commitment-id", "commitment_1",
	})
	payload = assertEnvelopeError(t, raw)
	errObj, _ = payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "unknown_command" {
		t.Fatalf("expected removed commitments propose-patch command to fail, payload=%#v", payload)
	}

	raw = runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"commitments", "apply",
		"proposal_1",
	})
	payload = assertEnvelopeError(t, raw)
	errObj, _ = payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "unknown_command" {
		t.Fatalf("expected removed commitments apply command to fail, payload=%#v", payload)
	}
}

func TestEventsExplainListMode(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"events", "explain"})
	if !strings.Contains(raw, "Known event types") {
		t.Fatalf("expected list heading in explain output, got %q", raw)
	}
	if !strings.Contains(raw, "Communication: Direct communication or important non-structured information.") {
		t.Fatalf("expected communication group in explain output, got %q", raw)
	}
	if !strings.Contains(raw, "Inbox Lifecycle: Inbox lifecycle facts, usually emitted by higher-level commands.") {
		t.Fatalf("expected inbox lifecycle group in explain output, got %q", raw)
	}
	if !strings.Contains(raw, "- message_posted: Use for low-level communication records that belong on a backing thread; prefer topic/document/card message commands for ordinary discussion.") {
		t.Fatalf("expected message_posted communication guidance in explain output, got %q", raw)
	}
	if !strings.Contains(raw, "- human_attention_requested: Use the human command group to ask for operator attention, review, or escalation.") {
		t.Fatalf("expected human_attention_requested guidance in explain output, got %q", raw)
	}
	if !strings.Contains(raw, "anx events explain <event-type>") {
		t.Fatalf("expected follow-up hint in explain output, got %q", raw)
	}
}

func TestEventsExplainListModeJSON(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "events", "explain"})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	items, _ := data["known_event_types"].([]any)
	if len(items) == 0 {
		t.Fatalf("expected known_event_types in JSON output, payload=%#v", payload)
	}

	foundMessagePosted := false
	foundInterventionNeeded := false
	for _, item := range items {
		entry, _ := item.(map[string]any)
		switch anyStringValue(entry["type"]) {
		case "message_posted":
			foundMessagePosted = true
			if got := anyStringValue(entry["group"]); got != "Communication" {
				t.Fatalf("expected message_posted group Communication, got %q entry=%#v", got, entry)
			}
		case "human_attention_requested":
			foundInterventionNeeded = true
			if got := anyStringValue(entry["group"]); got != "Inbox Lifecycle" {
				t.Fatalf("expected human_attention_requested group Inbox Lifecycle, got %q entry=%#v", got, entry)
			}
		}
	}
	if !foundMessagePosted {
		t.Fatalf("expected message_posted in JSON output, payload=%#v", payload)
	}
	if !foundInterventionNeeded {
		t.Fatalf("expected human_attention_requested in JSON output, payload=%#v", payload)
	}
}

func TestEventsExplainSpecificTypeMode(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "events", "explain", "card_created"})
	payload := assertEnvelopeOK(t, raw)
	if got := anyStringValue(payload["command"]); got != "events explain" {
		t.Fatalf("unexpected command label: %#v", payload)
	}
	data, _ := payload["data"].(map[string]any)
	if got := anyStringValue(data["event_type"]); got != "card_created" {
		t.Fatalf("expected event_type card_created, got %q payload=%#v", got, payload)
	}
	constraints, _ := data["constraints"].([]any)
	foundArtifactConstraint := false
	for _, item := range constraints {
		if strings.Contains(anyStringValue(item), "card:") {
			foundArtifactConstraint = true
			break
		}
	}
	if !foundArtifactConstraint {
		t.Fatalf("expected card constraint guidance, payload=%#v", payload)
	}

	rawFlag := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "events", "explain", "--type", "card_created"})
	payloadFlag := assertEnvelopeOK(t, rawFlag)
	dataFlag, _ := payloadFlag["data"].(map[string]any)
	if got := anyStringValue(dataFlag["event_type"]); got != "card_created" {
		t.Fatalf("expected event_type card_created via --type, got %q payload=%#v", got, payloadFlag)
	}
}

func TestEventsExplainMessagePostedGuidance(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"events", "explain", "message_posted"})
	if !strings.Contains(raw, "Group: Communication") {
		t.Fatalf("expected group heading in explain output, got %q", raw)
	}
	if !strings.Contains(raw, "Usage hint: Use for low-level communication records that belong on a backing thread; prefer topic/document/card message commands for ordinary discussion.") {
		t.Fatalf("expected usage hint in explain output, got %q", raw)
	}
	if !strings.Contains(raw, "Use this type for messages, replies, or important non-structured information that should read like direct communication on a backing thread.") {
		t.Fatalf("expected direct communication guidance in explain output, got %q", raw)
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
	if !strings.Contains(message, "known types:") || !strings.Contains(message, "card_created") {
		t.Fatalf("expected known-types guidance in error message, got %q payload=%#v", message, payload)
	}
}

func TestEventsValidateCommand(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	eventFile := filepath.Join(home, "event.json")
	if err := os.WriteFile(eventFile, []byte(`{"event":{"type":"message_posted","summary":"hello","thread_id":"thread_1","refs":["thread:thread_1"],"provenance":{"sources":["artifact:source_1"]}}}`), 0o600); err != nil {
		t.Fatalf("write event file: %v", err)
	}

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "events", "validate", "--from-file", eventFile})
	payload := assertEnvelopeOK(t, raw)
	if got := anyStringValue(payload["command"]); got != "events validate" {
		t.Fatalf("unexpected command label: %#v", payload)
	}
	data, _ := payload["data"].(map[string]any)
	if validated, _ := data["validated"].(bool); !validated {
		t.Fatalf("expected validated=true payload=%#v", payload)
	}
	if got := anyStringValue(data["command_id"]); got != "events.create" {
		t.Fatalf("expected command_id events.create, got %q payload=%#v", got, payload)
	}
	if got := anyStringValue(data["path"]); got != "/events" {
		t.Fatalf("expected path /events, got %q payload=%#v", got, payload)
	}
}

func TestEventsValidateInvalidJSONIncludesLocation(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	eventFile := filepath.Join(home, "event-invalid.json")
	if err := os.WriteFile(eventFile, []byte("{\n  \"event\": {\n    \"type\": \"message_posted\",\n    \"summary\": \"hello\",\n  }\n}\n"), 0o600); err != nil {
		t.Fatalf("write invalid event file: %v", err)
	}

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "events", "validate", "--from-file", eventFile})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "invalid_json" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	message := anyStringValue(errObj["message"])
	if !strings.Contains(message, "line") || !strings.Contains(message, "column") {
		t.Fatalf("expected line/column parse guidance, got %q payload=%#v", message, payload)
	}
}

func TestEventsCreateDryRunSkipsHTTP(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"event":{"id":"event_unexpected"}}`))
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, strings.NewReader(`{"event":{"type":"message_posted","summary":"hello","thread_id":"thread_1","refs":["thread:thread_1"],"provenance":{"sources":["artifact:source_1"]}}}`), []string{
		"--json",
		"--base-url", server.URL,
		"events", "create",
		"--dry-run",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	if dryRun, _ := data["dry_run"].(bool); !dryRun {
		t.Fatalf("expected dry_run=true payload=%#v", payload)
	}
	if got := anyStringValue(data["method"]); got != "POST" {
		t.Fatalf("expected method POST, got %q payload=%#v", got, payload)
	}
	if got := anyStringValue(data["path"]); got != "/events" {
		t.Fatalf("expected path /events, got %q payload=%#v", got, payload)
	}

	mu.Lock()
	gotRequests := requestCount
	mu.Unlock()
	if gotRequests != 0 {
		t.Fatalf("expected no HTTP request for dry-run, got %d", gotRequests)
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
	raw := runCLIForTest(t, home, map[string]string{}, strings.NewReader(`{"event":{"type":"card_created","summary":"review done","refs":["artifact:review_1","artifact:receipt_1"],"provenance":{"sources":["artifact:source_1"]},"payload":{"subject_ref":"card:card_1"}}}`), []string{
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
	if !strings.Contains(message, "card_created") || !strings.Contains(message, "card") {
		t.Fatalf("expected actionable refs guidance, got message=%q payload=%#v", message, payload)
	}

	mu.Lock()
	gotRequests := requestCount
	mu.Unlock()
	if gotRequests != 0 {
		t.Fatalf("expected no HTTP request for invalid local payload, got %d", gotRequests)
	}
}

func TestNormalizeMutationBodyIDsSkipsNestedStructuredDocContent(t *testing.T) {
	t.Parallel()

	app := &App{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads":
			_, _ = w.Write([]byte(`{"threads":[{"id":"thread_1234567890"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	body := map[string]any{
		"document":     map[string]any{"id": "doc_1"},
		"content_type": "structured",
		"refs":         []any{"thread:thread_123"},
		"content": map[string]any{
			"thread_id": "thread_12345",
			"nested": map[string]any{
				"refs": []any{"thread:9a61af8e-d2c"},
			},
		},
	}

	normalizedAny, err := app.normalizeMutationBodyIDs(context.Background(), config.Resolved{BaseURL: server.URL}, "docs.revisions.create", nil, body)
	if err != nil {
		t.Fatalf("normalize docs.revisions.create body: %v", err)
	}
	normalized, _ := normalizedAny.(map[string]any)
	refs := asSlice(normalized["refs"])
	if len(refs) != 1 || anyStringValue(refs[0]) != "thread:thread_1234567890" {
		t.Fatalf("expected top-level docs refs to be normalized, got %#v", normalized)
	}
	content := asMap(normalized["content"])
	if got := anyStringValue(content["thread_id"]); got != "thread_12345" {
		t.Fatalf("expected structured content.thread_id to remain untouched, got %#v", normalized)
	}
	nested := asMap(content["nested"])
	nestedRefs := asSlice(nested["refs"])
	if len(nestedRefs) != 1 || anyStringValue(nestedRefs[0]) != "thread:9a61af8e-d2c" {
		t.Fatalf("expected nested structured refs to remain untouched, got %#v", normalized)
	}
}

func TestNormalizeMutationBodyIDsPreservesUnsupportedTypedRefsVerbatim(t *testing.T) {
	t.Parallel()

	app := &App{}
	body := map[string]any{
		"event": map[string]any{
			"type":      "message_posted",
			"thread_id": "thread_123456789",
			"refs":      []any{"CuStOmType:ABC123"},
		},
	}

	normalizedAny, err := app.normalizeMutationBodyIDs(context.Background(), config.Resolved{}, "events.create", nil, body)
	if err != nil {
		t.Fatalf("normalize events.create body: %v", err)
	}
	normalized, _ := normalizedAny.(map[string]any)
	event := asMap(normalized["event"])
	refs := asSlice(event["refs"])
	if len(refs) != 1 || anyStringValue(refs[0]) != "CuStOmType:ABC123" {
		t.Fatalf("expected unsupported typed ref to remain verbatim, got %#v", normalized)
	}
}

func TestNormalizeMutationBodyIDsHandlesInboxRespondRelatedRefs(t *testing.T) {
	t.Parallel()

	app := &App{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads":
			_, _ = w.Write([]byte(`{"threads":[{"id":"thread_1234567890"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	body := map[string]any{
		"inbox_item_id": "inbox:ask:thread_1234567890:none:event_1",
		"response_text": "Approved.",
		"related_refs":  []any{"topic:launch"},
	}

	normalizedAny, err := app.normalizeMutationBodyIDs(
		context.Background(),
		config.Resolved{BaseURL: server.URL},
		"inbox.respond",
		nil,
		body,
	)
	if err != nil {
		t.Fatalf("normalize inbox.respond body: %v", err)
	}
	normalized, _ := normalizedAny.(map[string]any)
	refs, _ := normalized["related_refs"].([]any)
	if len(refs) != 1 || anyStringValue(refs[0]) != "topic:launch" {
		t.Fatalf("expected normalized related_refs, got %#v", normalized)
	}
}

func TestCommitmentsGetTextOutputIsRemoved(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := New()
	cli.Stdout = stdout
	cli.Stderr = stderr
	cli.Stdin = strings.NewReader("")
	cli.StdinIsTTY = func() bool { return true }
	cli.UserHomeDir = func() (string, error) { return home, nil }
	cli.ReadFile = os.ReadFile
	exitCode := cli.Run([]string{"commitments", "get", "--commitment-id", "commitment_1"})
	if exitCode == 0 {
		t.Fatalf("expected removed commitments get command to fail, stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "unknown command \"commitments\"") {
		t.Fatalf("expected unknown command error, stdout=%s stderr=%s", stdout.String(), stderr.String())
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
		_, _ = w.Write([]byte(`{"thread":{"id":"thread_1"},"recent_events":[],"key_artifacts":[],"open_cards":[]}`))
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
	data, _ := payload["data"].(map[string]any)
	collaboration, _ := data["collaboration_summary"].(map[string]any)
	if collaboration == nil {
		t.Fatalf("expected collaboration_summary in context payload, got %#v", data)
	}
}

func TestThreadsContextIncludesCollaborationSummarySections(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/threads/thread_1/context" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"thread":{"id":"thread_1","title":"Pilot Rescue"},
			"recent_events":[
				{"id":"event_actor_1","type":"message_posted","summary":"support recommends Friday launch"},
				{"id":"event_need_1","type":"human_attention_requested","summary":"pick launch day"},
				{"id":"event_done_1","type":"human_attention_responded","summary":"launch Friday"}
			],
			"key_artifacts":[
				{"ref":"artifact:brief_1","artifact":{"id":"artifact_1","kind":"attachment","summary":"Pilot rescue brief"}}
			],
			"open_cards":[
				{"id":"card_1","status":"open","title":"Publish launch brief"}
			]
		}`))
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"threads", "context",
		"--thread-id", "thread_1",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	collaboration, _ := data["collaboration_summary"].(map[string]any)
	if collaboration == nil {
		t.Fatalf("expected collaboration_summary, got %#v", data)
	}
	if got := intValue(collaboration["artifact_count"]); got != 1 {
		t.Fatalf("expected artifact_count=1, got %#v", collaboration)
	}
	if _, ok := collaboration["recommendation_count"]; ok {
		t.Fatalf("expected simplified collaboration_summary without recommendation_count, got %#v", collaboration)
	}

	textOut := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"threads", "context",
		"--thread-id", "thread_1",
	})
	if !strings.Contains(textOut, "recent_events (3):") || !strings.Contains(textOut, "message_posted") || !strings.Contains(textOut, "human_attention_requested") || !strings.Contains(textOut, "human_attention_responded") {
		t.Fatalf("expected collaboration sections in default text output, got:\n%s", textOut)
	}
}

func TestCommitmentsListIsRemoved(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "commitments", "list", "--thread-id", "thread_canon"})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "unknown_command" {
		t.Fatalf("expected removed commitments list command to fail, payload=%#v", payload)
	}
}

func TestCardsTimelineDispatchesToAPI(t *testing.T) {
	t.Parallel()

	const cardID = "card_timeline_test_123456"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/cards/"+cardID+"/timeline" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"card":{"id":"` + cardID + `","title":"Example","thread_id":"thread_1"},
			"events":[{"id":"event_1","type":"card_updated","occurred_at":"2026-04-05T00:00:00Z"}],
			"artifacts":[],
			"cards":[],
			"documents":[],
			"threads":[]
		}`))
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json", "--base-url", server.URL,
		"cards", "timeline", "--card-id", cardID,
	})
	payload := assertEnvelopeOK(t, raw)
	if got := anyStringValue(payload["command"]); got != "cards timeline" {
		t.Fatalf("expected command cards timeline, got %q payload=%#v", got, payload)
	}
	if got := anyStringValue(payload["command_id"]); got != "cards.timeline" {
		t.Fatalf("expected command_id cards.timeline, got %q payload=%#v", got, payload)
	}
	data, _ := payload["data"].(map[string]any)
	card, _ := data["card"].(map[string]any)
	if got := anyStringValue(card["id"]); got != cardID {
		t.Fatalf("expected card id in envelope data, got %#v", payload)
	}
	events, _ := data["events"].([]any)
	if len(events) != 1 {
		t.Fatalf("expected one event in timeline data, got %#v", data)
	}

	textOut := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"cards", "timeline", "--card-id", cardID,
	})
	if !strings.Contains(textOut, shortID(cardID)) || !strings.Contains(textOut, "events: 1") {
		t.Fatalf("expected card short id and event count in default text output, got:\n%s", textOut)
	}
}

func TestCardsFileFirstWorkflowCommands(t *testing.T) {
	t.Parallel()

	const (
		boardID      = "board_cards_workflow_123456"
		cardID       = "card_cards_workflow_123456"
		revisionID   = "card_rev_cards_workflow_1"
		revisionID2  = "card_rev_cards_workflow_2"
		cardUpdated  = "2026-04-20T00:00:00Z"
		boardUpdated = "2026-04-20T00:05:00Z"
		profileActor = "actor_cards_profile"
	)

	var createSeen, reviseSeen, historySeen, revisionGetSeen, assignSeen, moveSeen, resolveSeen, reopenSeen bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/cards":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode cards create body: %v", err)
			}
			if got := anyStringValue(payload["board_id"]); got != boardID {
				t.Fatalf("expected board_id %q, got %#v", boardID, payload)
			}
			if got := anyStringValue(payload["actor_id"]); got != profileActor {
				t.Fatalf("expected profile actor_id %q, got %#v", profileActor, payload)
			}
			card, _ := payload["card"].(map[string]any)
			if got := anyStringValue(card["title"]); got != "Implement login" {
				t.Fatalf("expected create title, got %#v", payload)
			}
			if got := anyStringValue(card["summary"]); got != "Card body from disk" {
				t.Fatalf("expected create summary from file, got %#v", payload)
			}
			if got := anyStringValue(card["topic_ref"]); got != "topic:topic_cards_123" {
				t.Fatalf("expected normalized topic ref, got %#v", payload)
			}
			createSeen = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"card":{"id":"` + cardID + `","board_id":"` + boardID + `","title":"Implement login","summary":"Card body from disk\n","column_key":"backlog","head_revision_ref":"card_revision:` + revisionID + `","head_revision_number":1,"updated_at":"` + cardUpdated + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/cards/"+cardID:
			_, _ = w.Write([]byte(`{"card":{"id":"` + cardID + `","board_ref":"board:` + boardID + `","title":"Implement login","summary":"Old body","column_key":"backlog","head_revision_ref":"card_revision:` + revisionID + `","head_revision_number":1,"updated_at":"` + cardUpdated + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/cards/"+cardID+"/revisions":
			historySeen = true
			_, _ = w.Write([]byte(`{"card_id":"` + cardID + `","revisions":[{"revision_id":"` + revisionID + `","revision_number":1},{"revision_id":"` + revisionID2 + `","revision_number":2}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/cards/"+cardID+"/revisions/"+revisionID:
			revisionGetSeen = true
			_, _ = w.Write([]byte(`{"card_id":"` + cardID + `","revision":{"revision_id":"` + revisionID + `","revision_number":1,"summary":"Old body"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/boards/"+boardID:
			_, _ = w.Write([]byte(`{"board":{"id":"` + boardID + `","updated_at":"` + boardUpdated + `"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/cards/"+cardID+"/revisions":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode cards revise body: %v", err)
			}
			if got := anyStringValue(payload["if_base_revision"]); got != revisionID {
				t.Fatalf("expected discovered base revision %q, got %#v", revisionID, payload)
			}
			if got := anyStringValue(payload["actor_id"]); got != profileActor {
				t.Fatalf("expected profile actor_id %q, got %#v", profileActor, payload)
			}
			revision, _ := payload["revision"].(map[string]any)
			if got := anyStringValue(revision["summary"]); got != "Revised card body" {
				t.Fatalf("expected revised summary, got %#v", payload)
			}
			reviseSeen = true
			_, _ = w.Write([]byte(`{"card":{"id":"` + cardID + `","board_id":"` + boardID + `","updated_at":"2026-04-20T00:10:00Z","head_revision_ref":"card_revision:` + revisionID2 + `","head_revision_number":2},"revision":{"revision_id":"` + revisionID2 + `","revision_number":2}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/cards/"+cardID:
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode cards patch body: %v", err)
			}
			if got := anyStringValue(payload["if_updated_at"]); got != cardUpdated {
				t.Fatalf("expected discovered card token %q, got %#v", cardUpdated, payload)
			}
			if got := anyStringValue(payload["actor_id"]); got != profileActor {
				t.Fatalf("expected profile actor_id %q, got %#v", profileActor, payload)
			}
			patch, _ := payload["patch"].(map[string]any)
			switch {
			case len(asSlice(patch["assignee_refs"])) == 1 && anyStringValue(asSlice(patch["assignee_refs"])[0]) == "actor:actor_owner":
				assignSeen = true
			default:
				t.Fatalf("unexpected cards patch payload %#v", payload)
			}
			_, _ = w.Write([]byte(`{"card":{"id":"` + cardID + `","board_id":"` + boardID + `","updated_at":"2026-04-20T00:10:00Z"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/cards/"+cardID+"/move":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode cards move body: %v", err)
			}
			if got := anyStringValue(payload["if_board_updated_at"]); got != boardUpdated {
				t.Fatalf("expected discovered board token %q, got %#v", boardUpdated, payload)
			}
			if got := anyStringValue(payload["actor_id"]); got != profileActor {
				t.Fatalf("expected profile actor_id %q, got %#v", profileActor, payload)
			}
			switch anyStringValue(payload["column_key"]) {
			case "review":
				moveSeen = true
			case "done":
				if got := anyStringValue(payload["resolution"]); got != "done" {
					t.Fatalf("expected done resolution, got %#v", payload)
				}
				refs := asSlice(payload["resolution_refs"])
				if len(refs) != 1 || anyStringValue(refs[0]) != "event:event_done" {
					t.Fatalf("expected resolution evidence, got %#v", payload)
				}
				resolveSeen = true
			case "ready":
				reopenSeen = true
			default:
				t.Fatalf("unexpected move payload %#v", payload)
			}
			_, _ = w.Write([]byte(`{"board":{"id":"` + boardID + `","updated_at":"2026-04-20T00:15:00Z"},"card":{"id":"` + cardID + `","board_id":"` + boardID + `"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-cards", `{"agent":"agent-cards","actor_id":"`+profileActor+`","access_token":"token","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
	cardFile := filepath.Join(home, "card.md")
	if err := os.WriteFile(cardFile, []byte("Card body from disk\n"), 0o600); err != nil {
		t.Fatalf("write card file: %v", err)
	}
	revisedFile := filepath.Join(home, "card-revised.md")
	if err := os.WriteFile(revisedFile, []byte("Revised card body\n"), 0o600); err != nil {
		t.Fatalf("write revised card file: %v", err)
	}

	createPayload := assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, []string{
		"--json", "--base-url", server.URL, "--agent", "agent-cards",
		"cards", "create", "--board", boardID, "--topic", "topic_cards_123", "--title", "Implement login", "--content-file", cardFile,
	}))
	if got := anyStringValue(createPayload["command_id"]); got != "cards.create" {
		t.Fatalf("expected cards.create command_id, got %#v", createPayload)
	}

	revisePayload := assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, []string{
		"--json", "--base-url", server.URL, "--agent", "agent-cards",
		"cards", "revise", cardID, "--content-file", revisedFile,
	}))
	if got := anyStringValue(revisePayload["command"]); got != "cards revise" {
		t.Fatalf("expected cards revise command, got %#v", revisePayload)
	}
	if got := anyStringValue(revisePayload["command_id"]); got != "cards.revisions.create" {
		t.Fatalf("expected cards.revisions.create command_id, got %#v", revisePayload)
	}

	historyPayload := assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, []string{
		"--json", "--base-url", server.URL, "--agent", "agent-cards",
		"cards", "history", "--card-id", cardID,
	}))
	if got := anyStringValue(historyPayload["command_id"]); got != "cards.revisions.list" {
		t.Fatalf("expected cards.revisions.list command_id, got %#v", historyPayload)
	}

	revisionPayload := assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, []string{
		"--json", "--base-url", server.URL, "--agent", "agent-cards",
		"cards", "revision", "get", "--card-id", cardID, "--revision-id", revisionID,
	}))
	if got := anyStringValue(revisionPayload["command_id"]); got != "cards.revisions.get" {
		t.Fatalf("expected cards.revisions.get command_id, got %#v", revisionPayload)
	}

	assignPayload := assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, []string{
		"--json", "--base-url", server.URL, "--agent", "agent-cards",
		"cards", "assign", cardID, "--assignee-ref", "actor:actor_owner",
	}))
	if got := anyStringValue(assignPayload["command"]); got != "cards assign" {
		t.Fatalf("expected cards assign command, got %#v", assignPayload)
	}

	movePayload := assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, []string{
		"--json", "--base-url", server.URL, "--agent", "agent-cards",
		"cards", "move", cardID, "--column", "review",
	}))
	if got := anyStringValue(movePayload["command"]); got != "cards move" {
		t.Fatalf("expected cards move command, got %#v", movePayload)
	}

	resolvePayload := assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, []string{
		"--json", "--base-url", server.URL, "--agent", "agent-cards",
		"cards", "resolve", cardID, "--resolution-ref", "event:event_done",
	}))
	if got := anyStringValue(resolvePayload["command"]); got != "cards resolve" {
		t.Fatalf("expected cards resolve command, got %#v", resolvePayload)
	}
	if got := anyStringValue(resolvePayload["command_id"]); got != "cards.move" {
		t.Fatalf("expected cards.move command_id, got %#v", resolvePayload)
	}

	reopenPayload := assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, []string{
		"--json", "--base-url", server.URL, "--agent", "agent-cards",
		"cards", "reopen", cardID,
	}))
	if got := anyStringValue(reopenPayload["command"]); got != "cards reopen" {
		t.Fatalf("expected cards reopen command, got %#v", reopenPayload)
	}

	for name, seen := range map[string]bool{
		"create":       createSeen,
		"revise":       reviseSeen,
		"history":      historySeen,
		"revision_get": revisionGetSeen,
		"assign":       assignSeen,
		"move":         moveSeen,
		"resolve":      resolveSeen,
		"reopen":       reopenSeen,
	} {
		if !seen {
			t.Fatalf("expected %s request to be observed", name)
		}
	}
}

func TestCardsResolveBodyPostsEvidenceBeforeMove(t *testing.T) {
	t.Parallel()

	const (
		cardID       = "card_resolve_body_123456"
		boardID      = "board_resolve_body_123456"
		threadID     = "thread_resolve_body_123456"
		eventID      = "event_resolve_body_123456"
		cardUpdated  = "2026-04-21T00:00:00Z"
		boardUpdated = "2026-04-21T00:05:00Z"
		profileActor = "actor_resolve_body"
	)

	var postedEvidence, moved bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/cards/"+cardID:
			_, _ = w.Write([]byte(`{"card":{"id":"` + cardID + `","board_id":"` + boardID + `","board_ref":"board:` + boardID + `","title":"Resolve body","thread_id":"` + threadID + `","updated_at":"` + cardUpdated + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/boards/"+boardID:
			_, _ = w.Write([]byte(`{"board":{"id":"` + boardID + `","updated_at":"` + boardUpdated + `"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/events":
			if moved {
				t.Fatalf("expected evidence event before card move")
			}
			var posted map[string]any
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				t.Fatalf("decode evidence body: %v", err)
			}
			assertMessagePostedMutation(t, posted, profileActor, threadID, []string{"card:" + cardID, "thread:" + threadID, "board:" + boardID}, "Evidence from disk.")
			postedEvidence = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"event":{"id":"` + eventID + `","type":"message_posted","thread_id":"` + threadID + `"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/cards/"+cardID+"/move":
			if !postedEvidence {
				t.Fatalf("expected card resolve to post evidence before move")
			}
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode move body: %v", err)
			}
			refs := asSlice(payload["resolution_refs"])
			if len(refs) != 1 || anyStringValue(refs[0]) != "event:"+eventID {
				t.Fatalf("expected posted evidence ref, got %#v", payload)
			}
			if got := anyStringValue(payload["if_board_updated_at"]); got != boardUpdated {
				t.Fatalf("expected discovered board token %q, got %#v", boardUpdated, payload)
			}
			moved = true
			_, _ = w.Write([]byte(`{"board":{"id":"` + boardID + `","updated_at":"2026-04-21T00:10:00Z"},"card":{"id":"` + cardID + `","board_id":"` + boardID + `","column_key":"done","resolution":"done","resolution_refs":["event:` + eventID + `"]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-resolve-body", `{"agent":"agent-resolve-body","actor_id":"`+profileActor+`","access_token":"token","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
	evidenceFile := filepath.Join(home, "evidence.md")
	if err := os.WriteFile(evidenceFile, []byte("Evidence from disk.\n"), 0o600); err != nil {
		t.Fatalf("write evidence file: %v", err)
	}

	payload := assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, []string{
		"--json", "--base-url", server.URL, "--agent", "agent-resolve-body",
		"cards", "resolve", cardID, "--body-file", evidenceFile,
	}))
	if got := anyStringValue(payload["command_id"]); got != "cards.move" {
		t.Fatalf("expected final cards.move command_id, got %#v", payload)
	}
	if !postedEvidence || !moved {
		t.Fatalf("expected evidence post and move")
	}
}

func TestTopicsMessageAcceptsBackingThreadAlias(t *testing.T) {
	t.Parallel()

	const (
		threadID     = "thread_topic_alias_123456"
		profileActor = "actor_topic_alias"
	)

	var posted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/events":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode topics thread message body: %v", err)
			}
			assertMessagePostedMutation(t, payload, profileActor, threadID, []string{"thread:" + threadID}, "Thread-scoped reply path.")
			posted = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"event":{"id":"event_topic_alias_123456","type":"message_posted","thread_id":"` + threadID + `"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-topic-alias", `{"agent":"agent-topic-alias","actor_id":"`+profileActor+`","access_token":"token","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	payload := assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, []string{
		"--json", "--base-url", server.URL, "--agent", "agent-topic-alias",
		"topics", "message", "--thread", threadID, "--body", "Thread-scoped reply path.",
	}))
	if got := anyStringValue(payload["command_id"]); got != "events.create" {
		t.Fatalf("expected events.create command_id, got %#v", payload)
	}
	if !posted {
		t.Fatalf("expected event post")
	}
}

func TestTopicsBoardsNoJSONAndMessageWorkflow(t *testing.T) {
	t.Parallel()

	const (
		topicID      = "topic_cli_model_123456"
		topicThread  = "thread_topic_cli_model_123456"
		boardID      = "board_cli_model_123456"
		profileActor = "actor_cli_model_profile"
	)

	var topicCreateSeen, boardCreateSeen, messageSeen bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/topics":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode topics create body: %v", err)
			}
			topic, _ := payload["topic"].(map[string]any)
			if got := anyStringValue(topic["title"]); got != "CLI ergonomics" {
				t.Fatalf("expected topic title, got %#v", payload)
			}
			if got := anyStringValue(topic["summary"]); got != "Coordinate CLI work" {
				t.Fatalf("expected topic summary, got %#v", payload)
			}
			if refs := asSlice(topic["related_refs"]); len(refs) != 1 || anyStringValue(refs[0]) != "document:doc_model" {
				t.Fatalf("expected topic related ref, got %#v", payload)
			}
			topicCreateSeen = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"topic":{"id":"` + topicID + `","thread_id":"` + topicThread + `","title":"CLI ergonomics","summary":"Coordinate CLI work"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/boards":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode boards create body: %v", err)
			}
			board, _ := payload["board"].(map[string]any)
			if got := anyStringValue(board["title"]); got != "CLI board" {
				t.Fatalf("expected board title, got %#v", payload)
			}
			if got := anyStringValue(board["primary_topic_ref"]); got != "topic:"+topicID {
				t.Fatalf("expected primary topic ref, got %#v", payload)
			}
			boardCreateSeen = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"board":{"id":"` + boardID + `","title":"CLI board","primary_topic_ref":"topic:` + topicID + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/topics/"+topicID:
			_, _ = w.Write([]byte(`{"topic":{"id":"` + topicID + `","thread_id":"` + topicThread + `","title":"CLI ergonomics"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/events":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode topics message event body: %v", err)
			}
			event, _ := payload["event"].(map[string]any)
			if got := anyStringValue(event["type"]); got != "message_posted" {
				t.Fatalf("expected message_posted, got %#v", payload)
			}
			assertMessagePostedMutation(t, payload, profileActor, topicThread, []string{"topic:" + topicID, "thread:" + topicThread}, "Discussion from disk")
			messageSeen = true
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"event":{"id":"event_discuss_1","type":"message_posted","thread_id":"` + topicThread + `"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-model", `{"agent":"agent-model","actor_id":"`+profileActor+`","access_token":"token","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
	messageFile := filepath.Join(home, "message.md")
	if err := os.WriteFile(messageFile, []byte("Discussion from disk\n"), 0o600); err != nil {
		t.Fatalf("write message file: %v", err)
	}

	dryRun := assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, []string{
		"--json", "--base-url", server.URL, "--agent", "agent-model",
		"topics", "create", "--title", "Preview", "--summary", "Preview summary", "--dry-run",
	}))
	data, _ := dryRun["data"].(map[string]any)
	if got, _ := data["dry_run"].(bool); !got {
		t.Fatalf("expected topics create dry_run data, got %#v", dryRun)
	}

	topicPayload := assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, []string{
		"--json", "--base-url", server.URL, "--agent", "agent-model",
		"topics", "create", "--title", "CLI ergonomics", "--summary", "Coordinate CLI work", "--ref", "document:doc_model",
	}))
	if got := anyStringValue(topicPayload["command_id"]); got != "topics.create" {
		t.Fatalf("expected topics.create command_id, got %#v", topicPayload)
	}

	boardPayload := assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, []string{
		"--json", "--base-url", server.URL, "--agent", "agent-model",
		"boards", "create", "--topic", topicID, "--title", "CLI board", "--summary", "Active work",
	}))
	if got := anyStringValue(boardPayload["command_id"]); got != "boards.create" {
		t.Fatalf("expected boards.create command_id, got %#v", boardPayload)
	}

	messagePayload := assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, []string{
		"--json", "--base-url", server.URL, "--agent", "agent-model",
		"topics", "message", topicID, "--body-file", messageFile, "--actor-id", profileActor,
	}))
	if got := anyStringValue(messagePayload["command"]); got != "topics message" {
		t.Fatalf("expected topics message command, got %#v", messagePayload)
	}
	if got := anyStringValue(messagePayload["command_id"]); got != "events.create" {
		t.Fatalf("expected events.create command_id, got %#v", messagePayload)
	}

	for name, seen := range map[string]bool{"topic_create": topicCreateSeen, "board_create": boardCreateSeen, "message": messageSeen} {
		if !seen {
			t.Fatalf("expected %s request to be observed", name)
		}
	}
}

func TestTopicLifecycleCommandsAvoidRequiredJSONBody(t *testing.T) {
	t.Parallel()

	const (
		topicID      = "topic_lifecycle_123456"
		profileActor = "actor_lifecycle_profile"
	)

	seen := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/topics/"+topicID+"/archive":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode topics archive body: %v", err)
			}
			if got := anyStringValue(payload["actor_id"]); got != profileActor {
				t.Fatalf("expected archive actor_id %q, got %#v", profileActor, payload)
			}
			seen["archive"] = true
			_, _ = w.Write([]byte(`{"topic":{"id":"` + topicID + `","title":"Lifecycle","updated_at":"2026-05-05T00:00:00Z"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/topics/"+topicID+"/unarchive":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode topics unarchive body: %v", err)
			}
			if got := anyStringValue(payload["actor_id"]); got != profileActor {
				t.Fatalf("expected unarchive actor_id %q, got %#v", profileActor, payload)
			}
			seen["unarchive"] = true
			_, _ = w.Write([]byte(`{"topic":{"id":"` + topicID + `","title":"Lifecycle","updated_at":"2026-05-05T00:00:00Z"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/topics/"+topicID+"/restore":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode topics restore body: %v", err)
			}
			if got := anyStringValue(payload["actor_id"]); got != profileActor {
				t.Fatalf("expected restore actor_id %q, got %#v", profileActor, payload)
			}
			seen["restore"] = true
			_, _ = w.Write([]byte(`{"topic":{"id":"` + topicID + `","title":"Lifecycle","updated_at":"2026-05-05T00:00:00Z"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/topics/"+topicID+"/trash":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode topics trash body: %v", err)
			}
			if got := anyStringValue(payload["reason"]); got != "cleanup" {
				t.Fatalf("expected trash reason, got %#v", payload)
			}
			if got := anyStringValue(payload["actor_id"]); got != profileActor {
				t.Fatalf("expected trash actor_id %q, got %#v", profileActor, payload)
			}
			seen["trash"] = true
			_, _ = w.Write([]byte(`{"topic":{"id":"` + topicID + `","title":"Lifecycle","updated_at":"2026-05-05T00:00:00Z"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-lifecycle", `{"agent":"agent-lifecycle","actor_id":"`+profileActor+`","access_token":"token","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
	for _, args := range [][]string{
		{"topics", "archive", topicID},
		{"topics", "unarchive", topicID},
		{"topics", "restore", topicID},
		{"topics", "trash", topicID, "--reason", "cleanup"},
	} {
		payload := assertEnvelopeOK(t, runCLIForTest(t, home, nil, nil, append([]string{"--json", "--base-url", server.URL, "--agent", "agent-lifecycle"}, args...)))
		if got := anyStringValue(payload["command"]); got == "" {
			t.Fatalf("expected command in payload %#v", payload)
		}
	}
	for _, name := range []string{"archive", "unarchive", "restore", "trash"} {
		if !seen[name] {
			t.Fatalf("expected %s request", name)
		}
	}
}

func TestCardsMessageBuildsThreadScopedEvent(t *testing.T) {
	t.Parallel()

	const (
		cardID       = "card_message_123456"
		boardID      = "board_message_123456"
		threadID     = "thread_card_message_123456"
		profileActor = "actor_profile_message"
		eventID      = "event_message_123456"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/cards/"+cardID:
			_, _ = w.Write([]byte(`{"card":{"id":"` + cardID + `","title":"Message card","thread_id":"` + threadID + `","board_ref":"board:` + boardID + `"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/events":
			var posted map[string]any
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				t.Fatalf("decode cards message body: %v", err)
			}
			assertMessagePostedMutation(t, posted, profileActor, threadID, []string{"card:" + cardID, "thread:" + threadID, "board:" + boardID}, "Implemented via domain command.")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"event":{"id":"` + eventID + `","type":"message_posted","thread_id":"` + threadID + `","refs":["card:` + cardID + `","thread:` + threadID + `","board:` + boardID + `"]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-message", `{"agent":"agent-message","actor_id":"`+profileActor+`","access_token":"token","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	raw := runCLIForTest(t, home, nil, nil, []string{
		"--json", "--base-url", server.URL, "--agent", "agent-message",
		"cards", "message", cardID, "--body", "Implemented via domain command.",
	})
	payload := assertEnvelopeOK(t, raw)
	if got := anyStringValue(payload["command"]); got != "cards message" {
		t.Fatalf("expected cards message command, got %#v", payload)
	}
	if got := anyStringValue(payload["command_id"]); got != "events.create" {
		t.Fatalf("expected events.create command_id, got %#v", payload)
	}
	data, _ := payload["data"].(map[string]any)
	if got := anyStringValue(data["card_id"]); got != cardID {
		t.Fatalf("expected card_id in response data, got %#v", data)
	}
	if got := anyStringValue(data["thread_id"]); got != threadID {
		t.Fatalf("expected thread_id in response data, got %#v", data)
	}

	textOut := runCLIForTest(t, home, nil, nil, []string{
		"--base-url", server.URL, "--agent", "agent-message",
		"cards", "message", cardID, "--body", "Implemented via domain command.",
	})
	if !strings.Contains(textOut, "Message posted.") || !strings.Contains(textOut, "Card: Message card") || !strings.Contains(textOut, "Thread: "+threadID) {
		t.Fatalf("expected domain text output, got:\n%s", textOut)
	}
}

func TestCardsMessagesListsMessageEventsForCardThread(t *testing.T) {
	t.Parallel()

	const (
		cardID   = "card_messages_123456"
		threadID = "thread_messages_123456"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/cards/"+cardID:
			_, _ = w.Write([]byte(`{"card":{"id":"` + cardID + `","title":"List messages","thread_id":"` + threadID + `","board_ref":"board:board_messages"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads/"+threadID+"/timeline":
			_, _ = w.Write([]byte(`{"thread_id":"` + threadID + `","events":[
				{"id":"event_1","thread_id":"` + threadID + `","type":"message_posted","summary":"first"},
				{"id":"event_2","thread_id":"` + threadID + `","type":"card_moved","summary":"moved"},
				{"id":"event_3","thread_id":"` + threadID + `","type":"message_posted","summary":"second","payload":{"reply_to_event_id":"event_1"}}
			],"artifacts":{}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, nil, nil, []string{
		"--json", "--base-url", server.URL,
		"cards", "messages", cardID, "--max-events", "1",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	if got := anyStringValue(data["card_id"]); got != cardID {
		t.Fatalf("expected card_id, got %#v", data)
	}
	events, _ := data["events"].([]any)
	if len(events) != 1 {
		t.Fatalf("expected one message event after filtering/limit, got %#v", data)
	}
	event, _ := events[0].(map[string]any)
	if got := anyStringValue(event["id"]); got != "event_3" {
		t.Fatalf("expected latest message event, got %#v", data)
	}

	textOut := runCLIForTest(t, home, nil, nil, []string{
		"--base-url", server.URL,
		"cards", "messages", cardID, "--max-events", "1",
	})
	if !strings.Contains(textOut, "reply_to: "+shortID("event_1")) {
		t.Fatalf("expected reply target in message list text, got:\n%s", textOut)
	}
}

func TestCardsReplyRequiresTargetMessageOnCardThread(t *testing.T) {
	t.Parallel()

	const (
		cardID       = "card_reply_123456"
		threadID     = "thread_reply_123456"
		profileActor = "actor_profile_reply"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/cards/"+cardID:
			_, _ = w.Write([]byte(`{"card":{"id":"` + cardID + `","title":"Reply card","thread_id":"` + threadID + `","board_ref":"board:board_reply"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads/"+threadID+"/timeline":
			_, _ = w.Write([]byte(`{"thread_id":"` + threadID + `","events":[{"id":"event_parent_123456","thread_id":"` + threadID + `","type":"message_posted","summary":"parent"}],"artifacts":{}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/events":
			var posted map[string]any
			if err := json.NewDecoder(r.Body).Decode(&posted); err != nil {
				t.Fatalf("decode cards reply body: %v", err)
			}
			event, _ := posted["event"].(map[string]any)
			payload, _ := event["payload"].(map[string]any)
			if got := anyStringValue(payload["reply_to_event_id"]); got != "event_parent_123456" {
				t.Fatalf("expected reply_to_event_id, got %#v", posted)
			}
			if !containsString(stringList(event["refs"]), "event:event_parent_123456") {
				t.Fatalf("expected parent event ref, got %#v", posted)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"event":{"id":"event_reply_123456","type":"message_posted","thread_id":"` + threadID + `"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-reply", `{"agent":"agent-reply","actor_id":"`+profileActor+`","access_token":"token","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
	raw := runCLIForTest(t, home, nil, nil, []string{
		"--json", "--base-url", server.URL, "--agent", "agent-reply",
		"cards", "reply", cardID, "--to", "event_parent_123456", "--body", "Reply from CLI",
	})
	assertEnvelopeOK(t, raw)
}

func TestEventsValidateRejectsThreadRefOnlyMessage(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, nil, strings.NewReader(`{"event":{"type":"message_posted","summary":"hello","thread_ref":"thread:thread_1","refs":["thread:thread_1"],"provenance":{"sources":["manual"]}}}`), []string{
		"--json",
		"events", "validate",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if got := anyStringValue(errObj["message"]); !strings.Contains(got, "event.thread_id is required") {
		t.Fatalf("expected thread_id validation message, got %#v", payload)
	}
	if got := anyStringValue(errObj["hint"]); !strings.Contains(got, "anx cards message") {
		t.Fatalf("expected domain command hint, got %#v", payload)
	}
}

func TestMessageCommandInvalidFlagsBeatAmbiguousProfileResolution(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-a", `{"agent":"agent-a","actor_id":"actor_a","base_url":"http://127.0.0.1:1","access_token":"token","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
	writeAgentProfile(t, home, "agent-b", `{"agent":"agent-b","actor_id":"actor_b","base_url":"http://127.0.0.1:1","access_token":"token","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	raw := runCLIForTest(t, home, nil, nil, []string{
		"--json",
		"topics", "message", "topic_123", "--message-file", "note.md",
	})
	payload := assertEnvelopeError(t, raw)
	if got := anyStringValue(payload["command"]); got != "topics message" {
		t.Fatalf("expected topics message error command, got %#v", payload)
	}
	errObj, _ := payload["error"].(map[string]any)
	if got := anyStringValue(errObj["code"]); got != "invalid_flags" {
		t.Fatalf("expected invalid_flags before config resolution, got %#v", payload)
	}
	if got := anyStringValue(errObj["message"]); !strings.Contains(got, "message-file") {
		t.Fatalf("expected removed flag in error message, got %#v", payload)
	}
}

func TestPreConfigUsagePreflightBeatsAmbiguousProfileResolution(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-a", `{"agent":"agent-a","actor_id":"actor_a","base_url":"http://127.0.0.1:1","access_token":"token","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
	writeAgentProfile(t, home, "agent-b", `{"agent":"agent-b","actor_id":"actor_b","base_url":"http://127.0.0.1:1","access_token":"token","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	tests := []struct {
		name        string
		args        []string
		command     string
		code        string
		messagePart string
	}{
		{
			name:        "generated list invalid flag",
			args:        []string{"boards", "list", "--definitely-not-a-flag"},
			command:     "boards list",
			code:        "invalid_flags",
			messagePart: "definitely-not-a-flag",
		},
		{
			name:        "local helper lifecycle conflict",
			args:        []string{"events", "list", "--include-archived", "--archived-only"},
			command:     "events list",
			code:        "invalid_flags",
			messagePart: "include-archived",
		},
		{
			name:        "nested domain unknown subcommand",
			args:        []string{"topics", "frobnicate"},
			command:     "topics",
			code:        "unknown_subcommand",
			messagePart: "frobnicate",
		},
		{
			name:        "threads unknown subcommand",
			args:        []string{"threads", "frobnicate"},
			command:     "threads",
			code:        "unknown_subcommand",
			messagePart: "frobnicate",
		},
		{
			name:        "unknown root command",
			args:        []string{"frobnicate"},
			command:     "frobnicate",
			code:        "unknown_command",
			messagePart: "frobnicate",
		},
		{
			name:        "import execute invalid flag",
			args:        []string{"import", "apply", "--plan", "plan.json", "--execute", "--unknown"},
			command:     "import apply",
			code:        "invalid_flags",
			messagePart: "unknown",
		},
		{
			name:        "docs content unsupported document alias",
			args:        []string{"docs", "content", "--document", "doc_123"},
			command:     "docs content",
			code:        "invalid_flags",
			messagePart: "document",
		},
		{
			name:        "boards workspace unsupported board alias",
			args:        []string{"boards", "workspace", "--board", "board_123"},
			command:     "boards workspace",
			code:        "invalid_flags",
			messagePart: "board",
		},
		{
			name:        "boards cards list unsupported board alias",
			args:        []string{"boards", "cards", "list", "--board", "board_123"},
			command:     "boards cards list",
			code:        "invalid_flags",
			messagePart: "board",
		},
		{
			name:        "config lenient invalid flag",
			args:        []string{"config", "use", "agent-a", "--unknown"},
			command:     "config use",
			code:        "invalid_flags",
			messagePart: "unknown",
		},
		{
			name:        "notifications list unknown flag",
			args:        []string{"notifications", "list", "--unknown"},
			command:     "notifications list",
			code:        "invalid_flags",
			messagePart: "unknown",
		},
		{
			name:        "notifications list missing status value",
			args:        []string{"notifications", "list", "--status"},
			command:     "notifications list",
			code:        "invalid_flags",
			messagePart: "status",
		},
		{
			name:        "notifications read missing wakeup id value",
			args:        []string{"notifications", "read", "--wakeup-id"},
			command:     "notifications read",
			code:        "invalid_flags",
			messagePart: "wakeup-id",
		},
		{
			name:        "notifications dismiss missing wakeup id value",
			args:        []string{"notifications", "dismiss", "--wakeup-id"},
			command:     "notifications dismiss",
			code:        "invalid_flags",
			messagePart: "wakeup-id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			raw := runCLIForTest(t, home, nil, nil, append([]string{"--json"}, tt.args...))
			payload := assertEnvelopeError(t, raw)
			if got := anyStringValue(payload["command"]); got != tt.command {
				t.Fatalf("expected command %q, got %#v", tt.command, payload)
			}
			errObj, _ := payload["error"].(map[string]any)
			if got := anyStringValue(errObj["code"]); got != tt.code {
				t.Fatalf("expected %s before config resolution, got %#v", tt.code, payload)
			}
			if got := anyStringValue(errObj["message"]); !strings.Contains(got, tt.messagePart) {
				t.Fatalf("expected message to contain %q, got %#v", tt.messagePart, payload)
			}
		})
	}
}

func TestPreConfigUsagePreflightAcceptsBridgeRestartParserFlags(t *testing.T) {
	t.Parallel()

	command, err := preflightConfigIndependentUsage([]string{
		"bridge", "restart",
		"--config", "./bridge.toml",
		"--install-dir", "/tmp/anx-bridge",
		"--bin-dir", "/tmp/anx-bin",
		"--timeout-seconds", "5",
		"--force",
	})
	if err != nil {
		t.Fatalf("expected bridge restart parser flags to pass preflight, got %v", err)
	}
	if command != "bridge restart" {
		t.Fatalf("expected bridge restart command, got %q", command)
	}
}

func TestPreConfigUsagePreflightMirrorsGoFlagSpelling(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-a", `{"agent":"agent-a","actor_id":"actor_a","base_url":"http://127.0.0.1:1","access_token":"token","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
	writeAgentProfile(t, home, "agent-b", `{"agent":"agent-b","actor_id":"actor_b","base_url":"http://127.0.0.1:1","access_token":"token","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	raw := runCLIForTest(t, home, nil, nil, []string{
		"--json",
		"boards", "list", "-limit", "10",
	})
	payload := assertEnvelopeError(t, raw)
	if got := anyStringValue(payload["command"]); got != "boards list" {
		t.Fatalf("expected boards list config error command, got %#v", payload)
	}
	errObj, _ := payload["error"].(map[string]any)
	if got := anyStringValue(errObj["code"]); got != "config_resolution_failed" {
		t.Fatalf("expected single-dash flag to pass preflight and reach config resolution, got %#v", payload)
	}
}

func TestConfigResolutionErrorsUsePreflightCommandIdentity(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-a", `{"agent":"agent-a","actor_id":"actor_a","base_url":"http://127.0.0.1:1","access_token":"token","access_token_expires_at":"2099-01-01T00:00:00Z"}`)
	writeAgentProfile(t, home, "agent-b", `{"agent":"agent-b","actor_id":"actor_b","base_url":"http://127.0.0.1:1","access_token":"token","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	raw := runCLIForTest(t, home, nil, nil, []string{
		"--json",
		"events", "list", "--max-events", "1",
	})
	payload := assertEnvelopeError(t, raw)
	if got := anyStringValue(payload["command"]); got != "events list" {
		t.Fatalf("expected events list config error command, got %#v", payload)
	}
	errObj, _ := payload["error"].(map[string]any)
	if got := anyStringValue(errObj["code"]); got != "config_resolution_failed" {
		t.Fatalf("expected config_resolution_failed, got %#v", payload)
	}
}

func TestBoardCommands(t *testing.T) {
	t.Parallel()

	const (
		boardID       = "board_product_launch_123456"
		cardID        = "card_launch_123456"
		cardThreadID  = "thread_card_123456"
		peerCardID    = "card_peer_654321"
		updatedAt     = "2026-03-08T00:00:00Z"
		nextUpdatedAt = "2026-03-08T00:05:00Z"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/boards":
			if got := r.URL.Query().Get("state"); got != "active" {
				t.Fatalf("expected state query active, got %q", got)
			}
			if got := r.URL.Query()["owner"]; len(got) != 1 || got[0] != "actor_1" {
				t.Fatalf("expected owner query [actor_1], got %#v", got)
			}
			_, _ = w.Write([]byte(`{"boards":[{"board":{"id":"` + boardID + `","title":"Launch","state":"active"},"summary":{"card_count":1,"cards_by_column":{"backlog":1,"ready":0,"in_progress":0,"blocked":0,"review":0,"done":0},"unresolved_card_count":1,"document_count":1,"latest_activity_at":"` + updatedAt + `","has_document_refs":true}}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/boards":
			body, _ := io.ReadAll(r.Body)
			if !bytes.Contains(body, []byte(`"title":"Launch"`)) {
				t.Fatalf("unexpected boards create body: %s", string(body))
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"board":{"id":"` + boardID + `","title":"Launch","state":"active","updated_at":"` + updatedAt + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/boards/"+boardID:
			_, _ = w.Write([]byte(`{"board":{"id":"` + boardID + `","title":"Launch","state":"active","updated_at":"` + updatedAt + `"}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/boards/"+boardID:
			body, _ := io.ReadAll(r.Body)
			if !bytes.Contains(body, []byte(`"if_updated_at":"`+updatedAt+`"`)) {
				t.Fatalf("unexpected boards patch body: %s", string(body))
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"board":{"id":"` + boardID + `","title":"Launch Updated","state":"active","updated_at":"` + nextUpdatedAt + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/boards/"+boardID+"/workspace":
			_, _ = w.Write([]byte(`{"board_id":"` + boardID + `","board":{"id":"` + boardID + `","title":"Launch","state":"active","updated_at":"` + updatedAt + `"},"cards":{"items":[{"card":{"board_id":"` + boardID + `","thread_id":"` + cardThreadID + `","column_key":"backlog","rank":"a"},"thread":{"id":"` + cardThreadID + `","title":"Card"},"summary":{"related_topic_count":1,"decision_request_count":0,"decision_count":0,"recommendation_count":0,"document_count":1,"inbox_count":0,"latest_activity_at":"` + updatedAt + `","stale":false},"pinned_document":null}],"count":1},"documents":{"items":[],"count":0},"inbox":{"items":[],"count":0},"board_summary":{"card_count":1,"cards_by_column":{"backlog":1,"ready":0,"in_progress":0,"blocked":0,"review":0,"done":0},"unresolved_card_count":1,"document_count":1,"latest_activity_at":"` + updatedAt + `","has_document_refs":true},"warnings":{"items":[],"count":0},"section_kinds":{"board":"canonical","cards":"canonical","documents":"derived","topics":"derived","inbox":"derived","warnings":"derived"},"generated_at":"` + updatedAt + `"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/boards/"+boardID+"/cards":
			_, _ = w.Write([]byte(`{"board_id":"` + boardID + `","cards":[{"id":"` + cardID + `","board_id":"` + boardID + `","thread_id":"` + cardThreadID + `","parent_thread":"` + cardThreadID + `","title":"Launch task","body":"","version":1,"column_key":"backlog","rank":"a","assignee":null,"priority":null,"status":"todo","pinned_document_id":null,"created_at":"` + updatedAt + `","created_by":"actor_1","updated_at":"` + updatedAt + `","updated_by":"actor_1","provenance":{"sources":["inferred"]}}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/boards/"+boardID+"/cards":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode boards cards create body: %v", err)
			}
			if _, hasParent := payload["parent_thread"]; hasParent {
				t.Fatalf("unexpected parent_thread in create payload %#v", payload)
			}
			if got := anyStringValue(payload["request_key"]); got != "req-1" {
				t.Fatalf("expected create request_key req-1, got %#v", payload)
			}
			if got := anyStringValue(payload["document_ref"]); got != "document:doc_1" {
				t.Fatalf("expected create document_ref document:doc_1, got %#v", payload)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"board":{"id":"` + boardID + `","updated_at":"` + nextUpdatedAt + `"},"card":{"id":"` + cardID + `","board_id":"` + boardID + `","thread_id":"` + cardThreadID + `","parent_thread":"` + cardThreadID + `","title":"Launch task","body":"","version":1,"column_key":"backlog","rank":"a","assignee":null,"priority":null,"status":"todo","pinned_document_id":"doc_1","created_at":"` + updatedAt + `","created_by":"actor_1","updated_at":"` + nextUpdatedAt + `","updated_by":"actor_1","provenance":{"sources":["inferred"]}}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/boards/"+boardID+"/cards/"+cardID:
			_, _ = w.Write([]byte(`{"card":{"id":"` + cardID + `","board_id":"` + boardID + `","thread_id":"` + cardThreadID + `","parent_thread":"` + cardThreadID + `","title":"Launch task","summary":"","version":2,"column_key":"done","rank":"a","assignee":"actor_1","priority":"high","resolution":"","pinned_document_id":"doc_1","created_at":"` + updatedAt + `","created_by":"actor_1","updated_at":"` + nextUpdatedAt + `","updated_by":"actor_1","provenance":{"sources":["inferred"]}}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/cards/"+cardID:
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode boards cards patch body: %v", err)
			}
			if got := anyStringValue(payload["if_updated_at"]); got != updatedAt {
				t.Fatalf("expected card update concurrency token %q, got %#v", updatedAt, payload)
			}
			patch, _ := payload["patch"].(map[string]any)
			if got := anyStringValue(patch["resolution"]); got != "done" {
				t.Fatalf("expected resolution done, got %#v", payload)
			}
			_, _ = w.Write([]byte(`{"card":{"id":"` + cardID + `","board_id":"` + boardID + `","thread_id":"` + cardThreadID + `","parent_thread":"` + cardThreadID + `","title":"Launch task","summary":"","version":2,"column_key":"done","rank":"a","assignee":null,"priority":null,"resolution":"done","pinned_document_id":"doc_1","created_at":"` + updatedAt + `","created_by":"actor_1","updated_at":"` + nextUpdatedAt + `","updated_by":"actor_1","provenance":{"sources":["inferred"]}}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/cards/"+cardID+"/move":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode boards cards move body: %v", err)
			}
			if got := anyStringValue(payload["column_key"]); got != "review" {
				t.Fatalf("expected move column review, got %#v", payload)
			}
			if got := anyStringValue(payload["after_card_id"]); got != peerCardID {
				t.Fatalf("expected move after_card_id %q, got %#v", peerCardID, payload)
			}
			_, _ = w.Write([]byte(`{"board":{"id":"` + boardID + `","updated_at":"` + nextUpdatedAt + `"},"card":{"id":"` + cardID + `","board_id":"` + boardID + `","thread_id":"` + cardThreadID + `","parent_thread":"` + cardThreadID + `","title":"Launch task","body":"","version":2,"column_key":"review","rank":"b","assignee":null,"priority":null,"status":"done","pinned_document_id":"doc_1","created_at":"` + updatedAt + `","created_by":"actor_1","updated_at":"` + nextUpdatedAt + `","updated_by":"actor_1","provenance":{"sources":["inferred"]}}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/cards/"+cardID+"/archive":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode boards cards archive body: %v", err)
			}
			if got := anyStringValue(payload["if_board_updated_at"]); got != updatedAt {
				t.Fatalf("expected archive concurrency token %q, got %#v", updatedAt, payload)
			}
			_, _ = w.Write([]byte(`{"board":{"id":"` + boardID + `","updated_at":"` + nextUpdatedAt + `"},"card":{"id":"` + cardID + `","board_id":"` + boardID + `","thread_id":"` + cardThreadID + `","parent_thread":"` + cardThreadID + `","title":"Launch task","body":"","version":2,"column_key":"review","rank":"b","assignee":null,"priority":null,"status":"done","pinned_document_id":"doc_1","created_at":"` + updatedAt + `","created_by":"actor_1","updated_at":"` + nextUpdatedAt + `","updated_by":"actor_1","archived_at":"` + nextUpdatedAt + `","archived_by":"actor_1","provenance":{"sources":["inferred"]}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}

	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "boards", "list", "--state", "active", "--owner", "actor_1"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"board":{"title":"Launch","thread_id":"thread_primary_1","state":"active"}}`), []string{"--json", "--base-url", server.URL, "boards", "create"}))

	getPayload := assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "boards", "get", "--board-id", boardID}))
	if got := anyStringValue(getPayload["command_id"]); got != "boards.get" {
		t.Fatalf("expected boards.get command_id, got %#v", getPayload)
	}

	updatePayload := assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"if_updated_at":"`+updatedAt+`","patch":{"title":"Launch Updated"}}`), []string{"--json", "--base-url", server.URL, "boards", "patch", "--board-id", boardID}))
	if got := anyStringValue(updatePayload["command_id"]); got != "boards.patch" {
		t.Fatalf("expected boards.patch command_id, got %#v", updatePayload)
	}

	workspacePayload := assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "boards", "workspace", "--board-id", boardID}))
	if got := anyStringValue(workspacePayload["command_id"]); got != "boards.workspace" {
		t.Fatalf("expected boards.workspace command_id, got %#v", workspacePayload)
	}

	cardsListPayload := assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "boards", "cards", "list", "--board-id", boardID}))
	if got := anyStringValue(cardsListPayload["command_id"]); got != "boards.cards.list" {
		t.Fatalf("expected boards.cards.list command_id, got %#v", cardsListPayload)
	}

	createPayload := assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "boards", "cards", "create", "--board-id", boardID, "--column", "backlog", "--request-key", "req-1", "--document-ref", "document:doc_1"}))
	if got := anyStringValue(createPayload["command_id"]); got != "boards.cards.create" {
		t.Fatalf("expected boards.cards.create command_id, got %#v", createPayload)
	}

	getCardPayload := assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "boards", "cards", "get", "--board-id", boardID, "--card-id", cardID}))
	if got := anyStringValue(getCardPayload["command_id"]); got != "boards.cards.get" {
		t.Fatalf("expected boards.cards.get command_id, got %#v", getCardPayload)
	}

	updateCardPayload := assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "boards", "cards", "patch", "--card-id", cardID, "--if-updated-at", updatedAt, "--resolution", "done"}))
	if got := anyStringValue(updateCardPayload["command_id"]); got != "boards.cards.patch" {
		t.Fatalf("expected boards.cards.patch command_id, got %#v", updateCardPayload)
	}

	movePayload := assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "boards", "cards", "move", "--board-id", boardID, "--card-id", cardID, "--if-board-updated-at", updatedAt, "--column", "review", "--after-card-id", peerCardID}))
	if got := anyStringValue(movePayload["command_id"]); got != "boards.cards.move" {
		t.Fatalf("expected boards.cards.move command_id, got %#v", movePayload)
	}

	archivePayload := assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "boards", "cards", "archive", "--card-id", cardID, "--if-board-updated-at", updatedAt}))
	if got := anyStringValue(archivePayload["command_id"]); got != "boards.cards.archive" {
		t.Fatalf("expected boards.cards.archive command_id, got %#v", archivePayload)
	}
}

func TestBoardsListAddsNestedShortIDAndWorkspaceResolvesShortBoardID(t *testing.T) {
	t.Parallel()

	const canonicalID = "board_1234567890abcdef"
	prefixID := shortID(canonicalID)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/boards":
			_, _ = w.Write([]byte(`{"boards":[{"board":{"id":"` + canonicalID + `","title":"Ops Board","state":"active"},"summary":{"card_count":0,"cards_by_column":{"backlog":0,"ready":0,"in_progress":0,"blocked":0,"review":0,"done":0},"unresolved_card_count":0,"document_count":0,"latest_activity_at":null,"has_document_refs":false}}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/boards/"+prefixID+"/workspace":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"board not found"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/boards/"+canonicalID+"/workspace":
			_, _ = w.Write([]byte(`{"board_id":"` + canonicalID + `","board":{"id":"` + canonicalID + `","title":"Ops Board","state":"active"},"cards":{"items":[],"count":0},"documents":{"items":[],"count":0},"inbox":{"items":[],"count":0},"board_summary":{"card_count":0,"cards_by_column":{"backlog":0,"ready":0,"in_progress":0,"blocked":0,"review":0,"done":0},"unresolved_card_count":0,"document_count":0,"latest_activity_at":null,"has_document_refs":false},"warnings":{"items":[],"count":0},"section_kinds":{"board":"canonical","cards":"canonical","documents":"derived","topics":"derived","inbox":"derived","warnings":"derived"},"generated_at":"2026-03-08T00:00:00Z"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()

	listPayload := assertEnvelopeOK(t, runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"boards", "list",
	}))
	data, _ := listPayload["data"].(map[string]any)
	items, _ := data["boards"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected one board list item, got %#v", listPayload)
	}
	item, _ := items[0].(map[string]any)
	board, _ := item["board"].(map[string]any)
	if got := anyStringValue(board["short_id"]); got != prefixID {
		t.Fatalf("expected board short_id %q, got %#v", prefixID, listPayload)
	}

	workspacePayload := assertEnvelopeOK(t, runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"boards", "workspace",
		"--board-id", prefixID,
	}))
	workspaceData, _ := workspacePayload["data"].(map[string]any)
	if got := anyStringValue(workspaceData["board_id"]); got != canonicalID {
		t.Fatalf("expected canonical board_id %q, got %#v", canonicalID, workspacePayload)
	}
}

func TestBoardCardsListAddsCardShortIDAndGetResolvesShortCardID(t *testing.T) {
	t.Parallel()

	const canonicalBoardID = "board_1234567890abcdef"
	const shortBoardID = "board_1234"
	const canonicalCardID = "card_1234567890abcdef"
	const shortCardID = "card_12345"
	const threadID = "thread_1234567890abcdef"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/boards":
			_, _ = w.Write([]byte(`{"boards":[{"board":{"id":"` + canonicalBoardID + `","title":"Ops Board","state":"active"},"summary":{"card_count":1}}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/boards/"+canonicalBoardID+"/cards":
			_, _ = w.Write([]byte(`{"board_id":"` + canonicalBoardID + `","cards":[{"id":"` + canonicalCardID + `","board_id":"` + canonicalBoardID + `","thread_id":"` + threadID + `","title":"Fix CLI card discovery","column_key":"backlog","rank":"a"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/boards/"+canonicalBoardID+"/cards/"+canonicalCardID:
			_, _ = w.Write([]byte(`{"card":{"id":"` + canonicalCardID + `","board_id":"` + canonicalBoardID + `","thread_id":"` + threadID + `","title":"Fix CLI card discovery","column_key":"backlog"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	listPayload := assertEnvelopeOK(t, runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"boards", "cards", "list",
		shortBoardID,
	}))
	data, _ := listPayload["data"].(map[string]any)
	cards, _ := data["cards"].([]any)
	if len(cards) != 1 {
		t.Fatalf("expected one card, got %#v", listPayload)
	}
	card, _ := cards[0].(map[string]any)
	if got := anyStringValue(card["short_id"]); got != shortCardID {
		t.Fatalf("expected card short_id %q from canonical card id, got %#v", shortCardID, card)
	}

	getByPositionals := assertEnvelopeOK(t, runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"boards", "cards", "get",
		shortBoardID,
		shortCardID,
	}))
	getData, _ := getByPositionals["data"].(map[string]any)
	gotCard, _ := getData["card"].(map[string]any)
	if got := anyStringValue(gotCard["id"]); got != canonicalCardID {
		t.Fatalf("expected canonical card id %q, got %#v", canonicalCardID, getByPositionals)
	}

	assertEnvelopeOK(t, runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"boards", "cards", "get",
		"--board-id", shortBoardID,
		"--card-id", shortCardID,
	}))
}

func TestBoardCardsGetResolvesThreadIDFallbackAndRejectsAmbiguousThreadPrefix(t *testing.T) {
	t.Parallel()

	const canonicalBoardID = "board_1234567890abcdef"
	const shortBoardID = "board_1234"
	const cardA = "card_alpha_1234567890"
	const cardB = "card_beta_1234567890"
	const threadA = "thread_alpha_1234567890"
	const threadB = "thread_alpine_1234567890"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/boards":
			_, _ = w.Write([]byte(`{"boards":[{"board":{"id":"` + canonicalBoardID + `","title":"Ops Board","state":"active"},"summary":{"card_count":2}}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/boards/"+canonicalBoardID+"/cards":
			_, _ = w.Write([]byte(`{"board_id":"` + canonicalBoardID + `","cards":[{"id":"` + cardA + `","board_id":"` + canonicalBoardID + `","thread_id":"` + threadA + `","title":"Alpha","column_key":"backlog"},{"id":"` + cardB + `","board_id":"` + canonicalBoardID + `","thread_id":"` + threadB + `","title":"Beta","column_key":"review"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/boards/"+canonicalBoardID+"/cards/"+cardA:
			_, _ = w.Write([]byte(`{"card":{"id":"` + cardA + `","board_id":"` + canonicalBoardID + `","thread_id":"` + threadA + `","title":"Alpha","column_key":"backlog"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	payload := assertEnvelopeOK(t, runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"boards", "cards", "get",
		shortBoardID,
		threadA,
	}))
	data, _ := payload["data"].(map[string]any)
	card, _ := data["card"].(map[string]any)
	if got := anyStringValue(card["id"]); got != cardA {
		t.Fatalf("expected thread fallback to resolve %q, got %#v", cardA, payload)
	}

	errPayload := assertEnvelopeError(t, runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"boards", "cards", "get",
		shortBoardID,
		"thread_al",
	}))
	errObj, _ := errPayload["error"].(map[string]any)
	if message := anyStringValue(errObj["message"]); !strings.Contains(message, "ambiguous") || !strings.Contains(message, "boards cards list") {
		t.Fatalf("expected ambiguous error with list guidance, got %#v", errPayload)
	}
}

func TestWorkspaceSummaryTextAndJSON(t *testing.T) {
	t.Parallel()

	const boardID = "board_1234567890abcdef"
	const updatedAt = "2026-04-30T00:00:00Z"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/boards":
			_, _ = w.Write([]byte(`{"boards":[{"board":{"id":"` + boardID + `","title":"Launch","state":"active"},"summary":{"card_count":2,"unresolved_card_count":1,"document_count":1,"latest_activity_at":"` + updatedAt + `"}}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/cards":
			_, _ = w.Write([]byte(`{"cards":[{"id":"card_1"},{"id":"card_2"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/docs":
			_, _ = w.Write([]byte(`{"documents":[{"id":"doc_1"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/inbox":
			_, _ = w.Write([]byte(`{"items":[{"id":"inbox_1"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	text := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"workspace", "summary",
	})
	if !strings.Contains(text, "Workspace summary") || !strings.Contains(text, "counts: boards=1 cards=2 documents=1 inbox_items=1") || !strings.Contains(text, "Launch") {
		t.Fatalf("unexpected workspace summary text:\n%s", text)
	}
	bareText := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"workspace",
	})
	if !strings.Contains(bareText, "Workspace summary") || !strings.Contains(bareText, "counts: boards=1 cards=2 documents=1 inbox_items=1") {
		t.Fatalf("expected bare workspace to default to summary, got:\n%s", bareText)
	}

	payload := assertEnvelopeOK(t, runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"workspace", "summary",
	}))
	if got := anyStringValue(payload["command_id"]); got != "workspace.summary" {
		t.Fatalf("expected workspace.summary command_id, got %#v", payload)
	}
	data, _ := payload["data"].(map[string]any)
	counts, _ := data["counts"].(map[string]any)
	if got := intValue(counts["cards"]); got != 2 {
		t.Fatalf("expected cards count 2, got %#v", payload)
	}
	if strings.TrimSpace(anyStringValue(data["generated_at"])) == "" {
		t.Fatalf("expected generated_at, got %#v", payload)
	}
}

func TestWorkspaceSummaryEmptyStateIncludesNextStepHint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/boards":
			_, _ = w.Write([]byte(`{"boards":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/cards":
			_, _ = w.Write([]byte(`{"cards":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/docs":
			_, _ = w.Write([]byte(`{"documents":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/inbox":
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	text := runCLIForTest(t, t.TempDir(), map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"workspace",
	})
	if !strings.Contains(text, "Next: create coordination with `anx topics create --title <title>`") {
		t.Fatalf("expected empty workspace next-step hint, got:\n%s", text)
	}
}

func TestCreateCommandsResolveShortBoardAndTopicIDs(t *testing.T) {
	t.Parallel()

	const canonicalBoardID = "board_1234567890abcdef"
	const shortBoardID = "board_1234"
	const canonicalTopicID = "topic_1234567890abcdef"
	const shortTopicID = "topic_1234"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/boards":
			_, _ = w.Write([]byte(`{"boards":[{"board":{"id":"` + canonicalBoardID + `","title":"Ops Board","state":"active"},"summary":{"card_count":0}}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/topics":
			_, _ = w.Write([]byte(`{"topics":[{"id":"` + canonicalTopicID + `","title":"Ops Topic","state":"active"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/boards":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode board create body: %v", err)
			}
			board, _ := payload["board"].(map[string]any)
			wantRef := "topic:" + canonicalTopicID
			if got := anyStringValue(board["primary_topic_ref"]); got != wantRef {
				t.Fatalf("expected resolved primary_topic_ref %q, got %#v", wantRef, payload)
			}
			refs := asSlice(board["pinned_refs"])
			if len(refs) != 1 || anyStringValue(refs[0]) != wantRef {
				t.Fatalf("expected resolved pinned topic ref %q, got %#v", wantRef, payload)
			}
			_, _ = w.Write([]byte(`{"board":{"id":"board_created","title":"CLI board","primary_topic_ref":"` + wantRef + `"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/cards":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode card create body: %v", err)
			}
			if got := anyStringValue(payload["board_id"]); got != canonicalBoardID {
				t.Fatalf("expected resolved board_id %q, got %#v", canonicalBoardID, payload)
			}
			_, _ = w.Write([]byte(`{"card":{"id":"card_created","board_id":"` + canonicalBoardID + `","title":"Card","column_key":"backlog"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	contentFile := filepath.Join(home, "card.md")
	if err := os.WriteFile(contentFile, []byte("Card body\n"), 0o600); err != nil {
		t.Fatalf("write card content file: %v", err)
	}

	assertEnvelopeOK(t, runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"boards", "create",
		"--topic", shortTopicID,
		"--title", "CLI board",
	}))
	assertEnvelopeOK(t, runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"cards", "create",
		"--board", shortBoardID,
		"--title", "Card",
		"--content-file", contentFile,
	}))
}

func TestGlobalCardGetResolvesShortIDOnBoardCardNotFound(t *testing.T) {
	t.Parallel()

	const canonicalCardID = "card_1234567890abcdef"
	const shortCardID = "card_12345"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/cards/"+shortCardID:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"board card not found"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/cards":
			_, _ = w.Write([]byte(`{"cards":[{"id":"` + canonicalCardID + `","title":"Fix CLI card discovery","column_key":"backlog"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/cards/"+canonicalCardID:
			_, _ = w.Write([]byte(`{"card":{"id":"` + canonicalCardID + `","title":"Fix CLI card discovery","column_key":"backlog"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	payload := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"cards", "get", shortCardID,
	}))
	data, _ := payload["data"].(map[string]any)
	card, _ := data["card"].(map[string]any)
	if got := anyStringValue(card["id"]); got != canonicalCardID {
		t.Fatalf("expected short card id to resolve to %q, got %#v", canonicalCardID, payload)
	}
}

func TestWorkspaceSummaryAllowsPartialOptionalReadFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/boards":
			_, _ = w.Write([]byte(`{"boards":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/cards":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"code":"internal_error","message":"cards unavailable"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/docs":
			_, _ = w.Write([]byte(`{"documents":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/inbox":
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	payload := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"workspace", "summary",
	}))
	data, _ := payload["data"].(map[string]any)
	warnings, _ := data["warnings"].([]any)
	if len(warnings) != 1 {
		t.Fatalf("expected one warning for cards failure, got %#v", payload)
	}
	counts, _ := data["counts"].(map[string]any)
	if counts["cards"] != nil {
		t.Fatalf("expected unknown cards count after partial failure, got %#v", payload)
	}
}

func TestDocumentedShortIDResourcesHaveRegressionCoverage(t *testing.T) {
	t.Parallel()

	documented := []resourceIDLookupSpec{
		threadIDLookupSpec,
		topicIDLookupSpec,
		cardIDLookupSpec,
		artifactIDLookupSpec,
		boardIDLookupSpec,
		boardCardIDLookupSpec,
		documentIDLookupSpec,
		eventIDLookupSpec,
	}
	covered := map[string]bool{
		"threads.list":      true,
		"topics.list":       true,
		"cards.list":        true,
		"artifacts.list":    true,
		"boards.list":       true,
		"boards.cards.list": true,
		"docs.list":         true,
		"events.list":       true,
	}
	for _, spec := range documented {
		if !covered[spec.listCommandID] {
			t.Fatalf("short_id-capable %s lacks resolver regression coverage entry", spec.listCommandID)
		}
	}
	if !strings.Contains(agentGuideText(), "short_id") {
		t.Fatalf("agent guide no longer documents short_id behavior; update coverage assumptions")
	}
}

func TestBoardCardsMoveRejectsBeforeAndAfterFlags(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"boards", "cards", "move",
		"--board-id", "board_1234567890abcdef",
		"--card-id", "card_1234567890abcdef",
		"--if-board-updated-at", "2026-03-08T00:00:00Z",
		"--column", "review",
		"--before-card-id", "card_a_1234567890abcdef",
		"--after-card-id", "card_b_1234567890abcdef",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "invalid_request" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	if message := anyStringValue(errObj["message"]); !strings.Contains(message, "--before-card-id and --after-card-id cannot be combined") {
		t.Fatalf("expected placement flag guidance, got %q", message)
	}
}

func TestBoardCardMutationsResolveShortThreadIDsInBodies(t *testing.T) {
	t.Parallel()

	const canonicalBoardID = "board_1234567890abcdef"
	const shortBoardID = "board_1234"
	const canonicalCardThreadID = "thread_1234567890abcdef"
	const shortCardThreadID = "thread_123"
	const canonicalAnchorThreadID = "thread_anchor_1234567890"
	const shortAnchorThreadID = "thread_anc"
	const updatedAt = "2026-03-08T00:00:00Z"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/boards":
			_, _ = w.Write([]byte(`{"boards":[{"board":{"id":"` + canonicalBoardID + `","title":"Ops Board","state":"active"},"summary":{"card_count":0,"cards_by_column":{"backlog":0,"ready":0,"in_progress":0,"blocked":0,"review":0,"done":0},"unresolved_card_count":0,"document_count":0,"latest_activity_at":null,"has_document_refs":false}}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads":
			_, _ = w.Write([]byte(`{"threads":[{"id":"` + canonicalCardThreadID + `","title":"Execution Track"},{"id":"` + canonicalAnchorThreadID + `","title":"Review Anchor"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/boards/"+canonicalBoardID+"/cards":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode create body: %v", err)
			}
			if got := anyStringValue(payload["thread_id"]); got != canonicalCardThreadID {
				t.Fatalf("expected canonical create thread_id %q, got %#v", canonicalCardThreadID, payload)
			}
			if got := anyStringValue(payload["after_thread_id"]); got != canonicalAnchorThreadID {
				t.Fatalf("expected canonical create after_thread_id %q, got %#v", canonicalAnchorThreadID, payload)
			}
			_, _ = w.Write([]byte(`{"board":{"id":"` + canonicalBoardID + `","updated_at":"` + updatedAt + `"},"card":{"id":"card_123","board_id":"` + canonicalBoardID + `","thread_id":"` + canonicalCardThreadID + `","parent_thread":"` + canonicalCardThreadID + `","title":"Execution Track","body":"","version":1,"column_key":"ready","rank":"a","assignee":null,"priority":null,"status":"todo","pinned_document_id":null,"created_at":"` + updatedAt + `","created_by":"actor_1","updated_at":"` + updatedAt + `","updated_by":"actor_1","provenance":{"sources":["inferred"]}}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/cards/card_123/move":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode move body: %v", err)
			}
			if got := anyStringValue(payload["after_thread_id"]); got != canonicalAnchorThreadID {
				t.Fatalf("expected canonical move after_thread_id %q, got %#v", canonicalAnchorThreadID, payload)
			}
			_, _ = w.Write([]byte(`{"board":{"id":"` + canonicalBoardID + `","updated_at":"` + updatedAt + `"},"card":{"board_id":"` + canonicalBoardID + `","thread_id":"` + canonicalCardThreadID + `","column_key":"review","rank":"b","created_at":"` + updatedAt + `","created_by":"actor_1","updated_at":"` + updatedAt + `","updated_by":"actor_1"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	addFile := filepath.Join(home, "board-add.json")
	if err := os.WriteFile(addFile, []byte(`{"thread_id":"`+shortCardThreadID+`","column_key":"ready","after_thread_id":"`+shortAnchorThreadID+`"}`), 0o600); err != nil {
		t.Fatalf("write add file: %v", err)
	}

	assertEnvelopeOK(t, runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"boards", "cards", "create",
		"--board-id", shortBoardID,
		"--from-file", addFile,
	}))

	moveFile := filepath.Join(home, "board-thread-move.json")
	if err := os.WriteFile(moveFile, []byte(`{"if_board_updated_at":"`+updatedAt+`","column_key":"review","after_thread_id":"`+shortAnchorThreadID+`"}`), 0o600); err != nil {
		t.Fatalf("write move file: %v", err)
	}

	assertEnvelopeOK(t, runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"boards", "cards", "move",
		"--board-id", shortBoardID,
		"--card-id", "card_123",
		"--from-file", moveFile,
	}))
}

func TestBoardCardMoveResolvesShortAfterCardID(t *testing.T) {
	t.Parallel()

	const canonicalBoardID = "board_1234567890abcdef"
	const shortBoardID = "board_1234"
	const movingCardID = "card_moving_1234567890ab"
	const afterCardID = "card_afterxx_1234567890ab"
	const shortAfterCardID = "card_after"
	const updatedAt = "2026-03-08T00:00:00Z"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/boards":
			_, _ = w.Write([]byte(`{"boards":[{"board":{"id":"` + canonicalBoardID + `","title":"Ops Board","state":"active"},"summary":{"card_count":0,"cards_by_column":{"backlog":0,"ready":0,"in_progress":0,"blocked":0,"review":0,"done":0},"unresolved_card_count":0,"document_count":0,"latest_activity_at":null,"has_document_refs":false}}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/boards/"+canonicalBoardID+"/cards":
			_, _ = w.Write([]byte(`{"board_id":"` + canonicalBoardID + `","cards":[
				{"id":"` + movingCardID + `","board_id":"` + canonicalBoardID + `","column_key":"ready","rank":"a","title":"Moving","body":"","version":1,"parent_thread":null,"thread_id":null,"pinned_document_id":null,"assignee":null,"priority":null,"status":"todo","created_at":"` + updatedAt + `","created_by":"actor_1","updated_at":"` + updatedAt + `","updated_by":"actor_1","provenance":{"sources":["inferred"]}},
				{"id":"` + afterCardID + `","board_id":"` + canonicalBoardID + `","column_key":"ready","rank":"b","title":"Anchor","body":"","version":1,"parent_thread":null,"thread_id":null,"pinned_document_id":null,"assignee":null,"priority":null,"status":"todo","created_at":"` + updatedAt + `","created_by":"actor_1","updated_at":"` + updatedAt + `","updated_by":"actor_1","provenance":{"sources":["inferred"]}}
			]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/cards/"+movingCardID+"/move":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode move body: %v", err)
			}
			if got := anyStringValue(payload["after_card_id"]); got != afterCardID {
				t.Fatalf("expected canonical move after_card_id %q, got %#v", afterCardID, payload)
			}
			_, _ = w.Write([]byte(`{"board":{"id":"` + canonicalBoardID + `","updated_at":"` + updatedAt + `"},"card":{"id":"` + movingCardID + `","board_id":"` + canonicalBoardID + `","column_key":"review","rank":"c","title":"Moving","body":"","version":1,"parent_thread":null,"thread_id":null,"pinned_document_id":null,"assignee":null,"priority":null,"status":"todo","created_at":"` + updatedAt + `","created_by":"actor_1","updated_at":"` + updatedAt + `","updated_by":"actor_1","provenance":{"sources":["inferred"]}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	assertEnvelopeOK(t, runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"boards", "cards", "move",
		"--board-id", shortBoardID,
		"--card-id", movingCardID,
		"--if-board-updated-at", updatedAt,
		"--column", "review",
		"--after-card-id", shortAfterCardID,
	}))
}

func TestBoardCardMoveResolvesShortAfterCardIDFromFile(t *testing.T) {
	t.Parallel()

	const canonicalBoardID = "board_1234567890abcdef"
	const shortBoardID = "board_1234"
	const movingCardID = "card_moving_1234567890ab"
	const afterCardID = "card_afterxx_1234567890ab"
	const shortAfterCardID = "card_after"
	const updatedAt = "2026-03-08T00:00:00Z"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/boards":
			_, _ = w.Write([]byte(`{"boards":[{"board":{"id":"` + canonicalBoardID + `","title":"Ops Board","state":"active"},"summary":{"card_count":0,"cards_by_column":{"backlog":0,"ready":0,"in_progress":0,"blocked":0,"review":0,"done":0},"unresolved_card_count":0,"document_count":0,"latest_activity_at":null,"has_document_refs":false}}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/boards/"+canonicalBoardID+"/cards":
			_, _ = w.Write([]byte(`{"board_id":"` + canonicalBoardID + `","cards":[
				{"id":"` + movingCardID + `","board_id":"` + canonicalBoardID + `","column_key":"ready","rank":"a","title":"Moving","body":"","version":1,"parent_thread":null,"thread_id":null,"pinned_document_id":null,"assignee":null,"priority":null,"status":"todo","created_at":"` + updatedAt + `","created_by":"actor_1","updated_at":"` + updatedAt + `","updated_by":"actor_1","provenance":{"sources":["inferred"]}},
				{"id":"` + afterCardID + `","board_id":"` + canonicalBoardID + `","column_key":"ready","rank":"b","title":"Anchor","body":"","version":1,"parent_thread":null,"thread_id":null,"pinned_document_id":null,"assignee":null,"priority":null,"status":"todo","created_at":"` + updatedAt + `","created_by":"actor_1","updated_at":"` + updatedAt + `","updated_by":"actor_1","provenance":{"sources":["inferred"]}}
			]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/cards/"+movingCardID+"/move":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode move body: %v", err)
			}
			if got := anyStringValue(payload["after_card_id"]); got != afterCardID {
				t.Fatalf("expected canonical move after_card_id %q, got %#v", afterCardID, payload)
			}
			_, _ = w.Write([]byte(`{"board":{"id":"` + canonicalBoardID + `","updated_at":"` + updatedAt + `"},"card":{"id":"` + movingCardID + `","board_id":"` + canonicalBoardID + `","column_key":"review","rank":"c","title":"Moving","body":"","version":1,"parent_thread":null,"thread_id":null,"pinned_document_id":null,"assignee":null,"priority":null,"status":"todo","created_at":"` + updatedAt + `","created_by":"actor_1","updated_at":"` + updatedAt + `","updated_by":"actor_1","provenance":{"sources":["inferred"]}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	moveFile := filepath.Join(home, "board-card-move-after-card.json")
	if err := os.WriteFile(moveFile, []byte(`{"if_board_updated_at":"`+updatedAt+`","column_key":"review","after_card_id":"`+shortAfterCardID+`"}`), 0o600); err != nil {
		t.Fatalf("write move file: %v", err)
	}

	assertEnvelopeOK(t, runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"boards", "cards", "move",
		"--board-id", shortBoardID,
		"--card-id", movingCardID,
		"--from-file", moveFile,
	}))
}

func TestBoardCardUpdateAndMoveAllowJSONBodyWithoutConcurrencyFlags(t *testing.T) {
	t.Parallel()

	const canonicalBoardID = "board_1234567890abcdef"
	const canonicalCardID = "card_1234567890abcdef"
	const canonicalAnchorThreadID = "thread_anchor_1234567890"
	const shortAnchorThreadID = "thread_anc"
	const updatedAt = "2026-03-08T00:00:00Z"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads":
			_, _ = w.Write([]byte(`{"threads":[{"id":"` + canonicalAnchorThreadID + `","title":"Review Anchor"}]}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/cards/"+canonicalCardID:
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode update body: %v", err)
			}
			if got := anyStringValue(payload["if_board_updated_at"]); got != updatedAt {
				t.Fatalf("expected update concurrency token %q, got %#v", updatedAt, payload)
			}
			patch, _ := payload["patch"].(map[string]any)
			if got := anyStringValue(patch["resolution"]); got != "done" {
				t.Fatalf("expected update patch resolution done, got %#v", payload)
			}
			_, _ = w.Write([]byte(`{"board":{"id":"` + canonicalBoardID + `","updated_at":"` + updatedAt + `"},"card":{"id":"` + canonicalCardID + `","board_id":"` + canonicalBoardID + `","column_key":"done","resolution":"done"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/cards/"+canonicalCardID+"/move":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode move body: %v", err)
			}
			if got := anyStringValue(payload["if_board_updated_at"]); got != updatedAt {
				t.Fatalf("expected move concurrency token %q, got %#v", updatedAt, payload)
			}
			if got := anyStringValue(payload["after_thread_id"]); got != canonicalAnchorThreadID {
				t.Fatalf("expected canonical move after_thread_id %q, got %#v", canonicalAnchorThreadID, payload)
			}
			_, _ = w.Write([]byte(`{"board":{"id":"` + canonicalBoardID + `","updated_at":"` + updatedAt + `"},"card":{"id":"` + canonicalCardID + `","board_id":"` + canonicalBoardID + `","column_key":"review"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	updateFile := filepath.Join(home, "board-card-update.json")
	if err := os.WriteFile(updateFile, []byte(`{"if_board_updated_at":"`+updatedAt+`","patch":{"resolution":"done"}}`), 0o600); err != nil {
		t.Fatalf("write update file: %v", err)
	}
	moveFile := filepath.Join(home, "board-card-move.json")
	if err := os.WriteFile(moveFile, []byte(`{"if_board_updated_at":"`+updatedAt+`","column_key":"review","after_thread_id":"`+shortAnchorThreadID+`"}`), 0o600); err != nil {
		t.Fatalf("write move file: %v", err)
	}

	assertEnvelopeOK(t, runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"boards", "cards", "patch",
		"--card-id", canonicalCardID,
		"--from-file", updateFile,
	}))

	assertEnvelopeOK(t, runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"boards", "cards", "move",
		"--board-id", canonicalBoardID,
		"--card-id", canonicalCardID,
		"--from-file", moveFile,
	}))
}

func TestThreadsContextRejectsMixedSelectionModesWithActionableGuidance(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"threads", "context",
		"--thread-id", "thread_1",
		"--state", "active",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "invalid_request" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	message := anyStringValue(errObj["message"])
	if !strings.Contains(message, "--thread-id cannot be combined with discovery filters") || !strings.Contains(message, "anx threads workspace --thread-id <thread-id>") || !strings.Contains(message, "anx threads inspect --thread-id <thread-id>") || !strings.Contains(message, "anx topics workspace") || !strings.Contains(message, "anx threads inspect --state active") {
		t.Fatalf("expected actionable threads context guidance, got %#v", payload)
	}
}

func TestThreadsContextAggregatesAcrossMultipleThreads(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_1/context":
			_, _ = w.Write([]byte(`{
				"thread":{"id":"thread_1","title":"Pilot Rescue","state":"active"},
				"recent_events":[
					{"id":"event_actor_1","type":"message_posted","summary":"support recommends Friday launch","created_at":"2026-03-06T10:00:00Z"},
					{"id":"event_need_1","type":"human_attention_requested","summary":"pick launch day","created_at":"2026-03-06T10:01:00Z"}
				],
				"key_artifacts":[{"id":"artifact_1","kind":"attachment"}],
				"open_cards":[{"id":"card_1","status":"open","title":"Publish brief"}]
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_2/context":
			_, _ = w.Write([]byte(`{
				"thread":{"id":"thread_2","title":"Delivery Readiness","state":"active"},
				"recent_events":[
					{"id":"event_actor_2","type":"message_posted","summary":"delivery recommends staged rollout","created_at":"2026-03-06T10:05:00Z"},
					{"id":"event_done_2","type":"human_attention_responded","summary":"ship Friday scope","created_at":"2026-03-06T10:10:00Z"}
				],
				"key_artifacts":[{"id":"artifact_2","kind":"attachment"}],
				"open_cards":[{"id":"card_2","status":"open","title":"Prep release runbook"}]
			}`))
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
		"--thread-id", "thread_1",
		"--thread-id", "thread_2",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	contexts, _ := data["contexts"].([]any)
	if len(contexts) != 2 {
		t.Fatalf("expected 2 contexts, got %#v", data)
	}
	collaboration, _ := data["collaboration_summary"].(map[string]any)
	if collaboration == nil {
		t.Fatalf("expected collaboration summary, got %#v", data)
	}
	if got := intValue(collaboration["artifact_count"]); got != 2 {
		t.Fatalf("expected artifact_count=2, got %#v", collaboration)
	}
	if _, ok := collaboration["recommendation_count"]; ok {
		t.Fatalf("expected simplified collaboration_summary without recommendation_count, got %#v", collaboration)
	}

	textOut := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"threads", "context",
		"--thread-id", "thread_1",
		"--thread-id", "thread_2",
	})
	if !strings.Contains(textOut, "Thread contexts (2):") || !strings.Contains(textOut, "recent_events (4):") || !strings.Contains(textOut, "human_attention_requested") {
		t.Fatalf("expected aggregate context sections in default text output, got:\n%s", textOut)
	}
}

func TestThreadsContextDiscoversByState(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	contextRequests := make([]string, 0, 2)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads":
			if got := r.URL.Query().Get("state"); got != "active" {
				t.Fatalf("expected state=active, got %q", got)
			}
			if tags := r.URL.Query()["tag"]; len(tags) > 0 {
				t.Fatalf("unexpected tag query param: %q", tags)
			}
			_, _ = w.Write([]byte(`{"threads":[
				{"id":"thread_init_1","type":"initiative","state":"active"},
				{"id":"thread_case_1","type":"case","state":"active"},
				{"id":"thread_init_2","type":"initiative","state":"active"}
			]}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/context"):
			mu.Lock()
			contextRequests = append(contextRequests, r.URL.Path)
			mu.Unlock()
			switch r.URL.Path {
			case "/threads/thread_init_1/context":
				_, _ = w.Write([]byte(`{"thread":{"id":"thread_init_1","type":"initiative"},"recent_events":[],"key_artifacts":[],"open_cards":[]}`))
			case "/threads/thread_case_1/context":
				_, _ = w.Write([]byte(`{"thread":{"id":"thread_case_1","type":"case"},"recent_events":[],"key_artifacts":[],"open_cards":[]}`))
			case "/threads/thread_init_2/context":
				_, _ = w.Write([]byte(`{"thread":{"id":"thread_init_2","type":"initiative"},"recent_events":[],"key_artifacts":[],"open_cards":[]}`))
			default:
				http.NotFound(w, r)
			}
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
		"--state", "active",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	threadIDs := stringList(data["thread_ids"])
	if len(threadIDs) != 3 || threadIDs[0] != "thread_init_1" || threadIDs[1] != "thread_case_1" || threadIDs[2] != "thread_init_2" {
		t.Fatalf("expected active thread_ids [thread_init_1 thread_case_1 thread_init_2], got %#v", data)
	}

	mu.Lock()
	gotRequests := append([]string(nil), contextRequests...)
	mu.Unlock()
	if len(gotRequests) != 3 {
		t.Fatalf("expected exactly 3 context requests, got %v", gotRequests)
	}
}

func TestThreadsContextSupportsFullIDForEventSections(t *testing.T) {
	t.Parallel()

	const eventID = "event_1234567890abcdef"
	const artifactID = "artifact_1234567890abcdef"
	const cardID = "card_1234567890abcdef"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/threads/thread_1/context" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"thread":{"id":"thread_1","title":"Pilot Rescue"},
			"recent_events":[
				{"id":"` + eventID + `","type":"message_posted","summary":"ship Friday rescue scope"}
			],
			"key_artifacts":[{"id":"` + artifactID + `","kind":"attachment","summary":"Launch brief"}],
			"open_cards":[{"id":"` + cardID + `","status":"open","title":"Publish launch brief"}]
		}`))
	}))
	defer server.Close()

	home := t.TempDir()
	textFull := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"threads", "context",
		"--thread-id", "thread_1",
		"--full-id",
	})
	if !strings.Contains(textFull, eventID) {
		t.Fatalf("expected full event id in output, got:\n%s", textFull)
	}
	if !strings.Contains(textFull, artifactID) {
		t.Fatalf("expected full artifact id in output, got:\n%s", textFull)
	}
	if !strings.Contains(textFull, cardID) {
		t.Fatalf("expected full card id in output, got:\n%s", textFull)
	}

	textShort := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"threads", "context",
		"--thread-id", "thread_1",
	})
	if strings.Contains(textShort, eventID) {
		t.Fatalf("expected short-id rendering without --full-id, got:\n%s", textShort)
	}
	if !strings.Contains(textShort, shortID(eventID)) {
		t.Fatalf("expected short event id in default output, got:\n%s", textShort)
	}
	if strings.Contains(textShort, artifactID) || !strings.Contains(textShort, shortID(artifactID)) {
		t.Fatalf("expected short artifact id in default output, got:\n%s", textShort)
	}
	if strings.Contains(textShort, cardID) || !strings.Contains(textShort, shortID(cardID)) {
		t.Fatalf("expected short card id in default output, got:\n%s", textShort)
	}
}

func TestThreadsInspectBuildsCoordinationView(t *testing.T) {
	t.Parallel()

	const eventID = "event_1234567890abcdef"
	const inboxID = "inbox:action_needed:thread_1:none:event_1234567890abcdef"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_1/context":
			_, _ = w.Write([]byte(`{
				"thread":{"id":"thread_1","title":"Pilot Rescue","state":"active","type":"initiative"},
				"recent_events":[
					{"id":"` + eventID + `","thread_id":"thread_1","type":"message_posted","summary":"ship Friday rescue scope"},
					{"id":"event_need_1","thread_id":"thread_1","type":"human_attention_requested","summary":"approve launch date"}
				],
				"key_artifacts":[{"id":"artifact_1","kind":"attachment"}],
				"open_cards":[{"id":"card_1","status":"open","title":"Publish brief"}]
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/inbox":
			_, _ = w.Write([]byte(`{"items":[
				{"id":"` + inboxID + `","thread_id":"thread_1","type":"action_needed","summary":"launch date still needs acknowledgement"},
				{"id":"inbox:action_needed:thread_2:none:event_other","thread_id":"thread_2","type":"action_needed","summary":"other thread"}
			]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"threads", "inspect",
		"--thread-id", "thread_1",
	})
	payload := assertEnvelopeOK(t, raw)
	if got := anyStringValue(payload["command"]); got != "threads inspect" {
		t.Fatalf("expected threads inspect command, got %#v", payload)
	}
	if got := anyStringValue(payload["command_id"]); got != "threads.inspect" {
		t.Fatalf("expected threads.inspect command_id, got %#v", payload)
	}
	data, _ := payload["data"].(map[string]any)
	thread, _ := data["thread"].(map[string]any)
	if got := anyStringValue(thread["id"]); got != "thread_1" {
		t.Fatalf("expected thread_1, got %#v", data)
	}
	contextBody, _ := data["context"].(map[string]any)
	recentEvents, _ := contextBody["recent_events"].([]any)
	if len(recentEvents) != 2 {
		t.Fatalf("expected 2 recent events, got %#v", data)
	}
	collaboration, _ := data["collaboration"].(map[string]any)
	if got := intValue(collaboration["artifact_count"]); got != 1 {
		t.Fatalf("expected artifact_count=1, got %#v", collaboration)
	}
	inbox, _ := data["inbox"].(map[string]any)
	items, _ := inbox["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 inbox item for thread_1, got %#v", data)
	}
	item, _ := items[0].(map[string]any)
	if got := anyStringValue(item["id"]); got != inboxID {
		t.Fatalf("expected inbox item %q, got %#v", inboxID, data)
	}

	textFull := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"threads", "inspect",
		"--thread-id", "thread_1",
		"--full-id",
	})
	if !strings.Contains(textFull, eventID) {
		t.Fatalf("expected full event id in inspect output, got:\n%s", textFull)
	}
	if !strings.Contains(textFull, "inbox_items (1):") {
		t.Fatalf("expected inbox section in inspect output, got:\n%s", textFull)
	}
}

func TestThreadsInspectDiscoveryRequiresSingleThread(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"threads":[
				{"id":"thread_init_1","type":"initiative","state":"active"},
				{"id":"thread_init_2","type":"initiative","state":"active"}
			]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"threads", "inspect",
		"--state", "active",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "invalid_request" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	msg := anyStringValue(errObj["message"])
	if !strings.Contains(msg, "exactly one thread") || !strings.Contains(msg, "anx topics workspace") || !strings.Contains(msg, "anx threads workspace") {
		t.Fatalf("expected single-thread guidance, got %#v", payload)
	}
}

func TestThreadsInspectRejectsMixedSelectionModes(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"threads", "inspect",
		"--thread-id", "thread_1",
		"--state", "active",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "invalid_request" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	message := anyStringValue(errObj["message"])
	if !strings.Contains(message, "--thread-id cannot be combined with discovery filters") || !strings.Contains(message, "anx threads inspect --thread-id <thread-id>") || !strings.Contains(message, "anx threads inspect --state active") {
		t.Fatalf("expected shared-selection validation message, got %#v", payload)
	}
}

func TestThreadsWorkspaceJSONIncludesShortIDs(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/threads/thread_ws_main/workspace" {
			_, _ = w.Write([]byte(`{
				"thread_id":"thread_ws_main",
				"thread":{"id":"thread_ws_main","title":"T"},
				"context":{
					"recent_events":[{"id":"event_ws_1","thread_id":"thread_ws_main","type":"message_posted"}],
					"key_artifacts":[],
					"open_cards":[],
					"documents":[]
				},
				"collaboration":{
					"key_artifacts":[],
					"open_cards":[],
					"artifact_count":0,
					"open_card_count":0
				},
				"inbox":{"thread_id":"thread_ws_main","items":[],"count":0},
				"pending_attention":{"thread_id":"thread_ws_main","items":[],"count":0},
				"related_threads":{"count":1,"items":[{"thread":{"id":"thread_ws_related","title":"R"},"match_reason":"ref"}]},
				"total_review_items":0
			}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json", "--base-url", server.URL,
		"threads", "workspace", "--thread-id", "thread_ws_main",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)

	mainID := "thread_ws_main"
	threadObj := asMap(data["thread"])
	if threadObj == nil {
		t.Fatalf("expected thread object in data, got %#v", data)
	}
	if got := anyStringValue(threadObj["short_id"]); got != shortID(mainID) {
		t.Fatalf("thread.short_id: want %q got %q", shortID(mainID), got)
	}

	ctx := asMap(data["context"])
	if ctx == nil {
		t.Fatalf("expected context, got %#v", data)
	}
	events, _ := ctx["recent_events"].([]any)
	ev0, _ := events[0].(map[string]any)
	if got := anyStringValue(ev0["short_id"]); got != shortID("event_ws_1") {
		t.Fatalf("context.recent_events[0].short_id: want %q got %q", shortID("event_ws_1"), got)
	}

	rt := asMap(data["related_threads"])
	rtItems, _ := rt["items"].([]any)
	rt0, _ := rtItems[0].(map[string]any)
	rtThread := asMap(rt0["thread"])
	if got := anyStringValue(rtThread["short_id"]); got != shortID("thread_ws_related") {
		t.Fatalf("related_threads[0].thread.short_id: want %q got %q", shortID("thread_ws_related"), got)
	}

}

func TestThreadsContextTextOutputIsPayloadFirst(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/threads/thread_1/context" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"thread":{"id":"thread_1","title":"Pilot Rescue","state":"active","priority":"p1","current_summary":"Need a launch decision today."},
			"recent_events":[
				{"id":"event_1","type":"human_attention_requested","summary":"Need support and delivery recommendations"},
				{"id":"event_2","type":"human_attention_responded","summary":"Ship the Friday rescue scope"}
			],
			"key_artifacts":[
				{"id":"artifact_1","kind":"attachment","summary":"NorthWave pilot rescue brief"}
			],
			"open_cards":[
				{"id":"card_1","status":"open","title":"Publish rescue brief"}
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
	if !strings.Contains(out, "human_attention_requested") || !strings.Contains(out, "attachment") || !strings.Contains(out, "Publish rescue brief") {
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
		_, _ = w.Write([]byte(`{"thread":{"id":"thread_1"},"recent_events":[],"key_artifacts":[],"open_cards":[]}`))
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
		_, _ = w.Write([]byte(`{"thread":{"id":"thread_1","title":"Pilot Rescue"},"recent_events":[],"key_artifacts":[],"open_cards":[]}`))
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
			_, _ = w.Write([]byte(`{"thread":{"id":"` + canonicalID + `"},"recent_events":[],"key_artifacts":[],"open_cards":[]}`))
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
	thread, _ := data["thread"].(map[string]any)
	if got := anyStringValue(thread["id"]); got != canonicalID {
		t.Fatalf("expected canonical thread id %q, got %q payload=%#v", canonicalID, got, payload)
	}
}

func TestThreadsContextDeduplicatesResolvedDuplicateIDs(t *testing.T) {
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
			_, _ = w.Write([]byte(`{"threads":[{"id":"` + canonicalID + `"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads/"+canonicalID+"/context":
			_, _ = w.Write([]byte(`{
				"thread":{"id":"` + canonicalID + `","title":"Pilot Rescue"},
				"recent_events":[{"id":"event_actor_1","type":"message_posted","summary":"ship Friday scope"}],
				"key_artifacts":[],
				"open_cards":[]
			}`))
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
		"--thread-id", canonicalID,
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	threadIDs := stringList(data["thread_ids"])
	if len(threadIDs) != 1 || threadIDs[0] != canonicalID {
		t.Fatalf("expected one canonical thread_id %q, got %#v", canonicalID, data)
	}
	threadCount, _ := data["thread_count"].(float64)
	if got := int(threadCount); got != 1 {
		t.Fatalf("expected thread_count=1, got %#v", data)
	}
	contexts, _ := data["contexts"].([]any)
	if len(contexts) != 1 {
		t.Fatalf("expected one deduplicated context, got %#v", data)
	}
	recentEvents, _ := data["recent_events"].([]any)
	if len(recentEvents) != 1 {
		t.Fatalf("expected one deduplicated recent event, got %#v", data)
	}
	collaboration, _ := data["collaboration_summary"].(map[string]any)
	if got := intValue(collaboration["artifact_count"]); got != 0 {
		t.Fatalf("expected artifact_count=0 after dedupe, got %#v", collaboration)
	}
	if _, ok := collaboration["recommendation_count"]; ok {
		t.Fatalf("expected simplified collaboration_summary after dedupe, got %#v", collaboration)
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
		_, _ = w.Write([]byte(`{"threads":[{"id":"` + canonicalID + `","title":"Alpha","state":"active"}]}`))
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
	threads, _ := data["threads"].([]any)
	if len(threads) != 1 {
		t.Fatalf("expected one thread in list payload, got %#v", payload)
	}
	thread, _ := threads[0].(map[string]any)
	if got := anyStringValue(thread["short_id"]); got != shortID(canonicalID) {
		t.Fatalf("expected short_id %q, got %q payload=%#v", shortID(canonicalID), got, payload)
	}
}

func TestInboxRespondActorIDMeAliasFromProfile(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/inbox/"+url.PathEscape("inbox:1")+"/respond" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode inbox respond body: %v body=%s", err, string(body))
		}
		if _, exists := payload["inbox_item_id"]; exists {
			t.Fatalf("expected inbox_item_id in path only, got body=%s", string(body))
		}
		if got := strings.TrimSpace(anyStringValue(payload["actor_id"])); got != "actor-profile-1" {
			t.Fatalf("expected actor_id from profile, got %q body=%s", got, string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"event":{"id":"event_response_profile"}}`))
	}))
	defer server.Close()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-a", `{"agent":"agent-a","actor_id":"actor-profile-1","access_token":"token-a","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"--agent", "agent-a",
		"inbox", "respond",
		"--inbox-item-id", "inbox:1",
		"--response-text", "Approved.",
		"--actor-id", "me",
	})
	assertEnvelopeOK(t, raw)
}

// failingStdinReader causes Read to error if stdin is ever probed (non-TTY path).
type failingStdinReader struct{}

func (failingStdinReader) Read(p []byte) (int, error) {
	return 0, io.ErrUnexpectedEOF
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func assertMessagePostedMutation(t *testing.T, posted map[string]any, wantActorID string, wantThreadID string, wantRefs []string, wantText string) {
	t.Helper()
	event, _ := posted["event"].(map[string]any)
	if got := anyStringValue(event["type"]); got != "message_posted" {
		t.Fatalf("expected message_posted, got %#v", posted)
	}
	if got := anyStringValue(event["actor_id"]); got != wantActorID {
		t.Fatalf("expected actor_id %q, got %#v", wantActorID, posted)
	}
	if got := anyStringValue(event["thread_id"]); got != wantThreadID {
		t.Fatalf("expected thread_id %q, got %#v", wantThreadID, posted)
	}
	refs := stringList(event["refs"])
	for _, want := range wantRefs {
		if !containsString(refs, want) {
			t.Fatalf("expected ref %q in %#v", want, refs)
		}
	}
	payload, _ := event["payload"].(map[string]any)
	if got := anyStringValue(payload["text"]); got != wantText {
		t.Fatalf("expected message text %q, got %#v", wantText, posted)
	}
}

func TestInboxRespondSkipsStdinWhenInboxItemIDFromFlags(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/inbox/"+url.PathEscape("inbox:1")+"/respond":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"event":{"id":"event_response_stdin_skip"}}`))
			return
		default:
			http.NotFound(w, r)
			return
		}
	}))
	defer server.Close()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-a", `{"agent":"agent-a","actor_id":"actor-a1","access_token":"token-a","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	raw := runCLIForTest(t, home, map[string]string{}, failingStdinReader{}, []string{
		"--json",
		"--base-url", server.URL,
		"--agent", "agent-a",
		"inbox", "respond",
		"--inbox-item-id", "inbox:1",
		"--response-text", "OK.",
	})
	assertEnvelopeOK(t, raw)
}

func TestInboxRespondRequiresResponseText(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"inbox", "respond",
		"inbox:ask:thread_42:none:event_1",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if got := anyStringValue(errObj["code"]); got != "invalid_request" {
		t.Fatalf("expected invalid_request, got %#v", payload)
	}
	if got := anyStringValue(errObj["message"]); !strings.Contains(got, "response_text is required") {
		t.Fatalf("expected response_text guidance, got %#v", payload)
	}
}

func TestInboxRespondActorIDMeRequiresProfileActorID(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-a", `{}`)

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--agent", "agent-a",
		"inbox", "respond",
		"--inbox-item-id", "inbox:1",
		"--response-text", "OK.",
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

func TestBoardCardsCreateBatchActorIDFromProfileAndFlagOverrides(t *testing.T) {
	t.Parallel()

	const boardID = "board_batch_test_123"
	const profileActor = "actor_batch_profile"
	const flagActor = "actor_batch_flag"

	t.Run("profile_defaults_actor_id", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/boards/"+boardID+"/cards/batch" {
				http.NotFound(w, r)
				return
			}
			body, _ := io.ReadAll(r.Body)
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("decode batch body: %v", err)
			}
			if got := strings.TrimSpace(anyStringValue(payload["actor_id"])); got != profileActor {
				t.Fatalf("expected actor_id %q, got %q body=%s", profileActor, got, string(body))
			}
			items, _ := payload["items"].([]any)
			if len(items) != 1 {
				t.Fatalf("expected 1 item, got %#v", payload)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"board":{"id":"` + boardID + `"},"cards":[]}`))
		}))
		defer server.Close()

		home := t.TempDir()
		writeAgentProfile(t, home, "agent-b", `{"agent":"agent-b","actor_id":"`+profileActor+`","access_token":"t","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

		stdin := strings.NewReader(`{"items":[{"title":"A"}]}`)
		raw := runCLIForTest(t, home, map[string]string{}, stdin, []string{
			"--json", "--base-url", server.URL, "--agent", "agent-b",
			"boards", "cards", "create-batch", "--board-id", boardID,
		})
		assertEnvelopeOK(t, raw)
	})

	t.Run("flags_override_json_envelope", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/boards/"+boardID+"/cards/batch" {
				http.NotFound(w, r)
				return
			}
			body, _ := io.ReadAll(r.Body)
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("decode batch body: %v", err)
			}
			if got := strings.TrimSpace(anyStringValue(payload["actor_id"])); got != flagActor {
				t.Fatalf("expected actor_id %q, got %q body=%s", flagActor, got, string(body))
			}
			if got := strings.TrimSpace(anyStringValue(payload["request_key"])); got != "req_cli" {
				t.Fatalf("expected request_key req_cli, got %q body=%s", got, string(body))
			}
			if got := strings.TrimSpace(anyStringValue(payload["if_board_updated_at"])); got != "2026-04-12T00:00:00Z" {
				t.Fatalf("expected if_board_updated_at, got %q body=%s", got, string(body))
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"board":{"id":"` + boardID + `"},"cards":[]}`))
		}))
		defer server.Close()

		home := t.TempDir()
		writeAgentProfile(t, home, "agent-b", `{"agent":"agent-b","actor_id":"`+profileActor+`","access_token":"t","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

		stdin := strings.NewReader(`{"items":[{"title":"A"}],"actor_id":"actor_from_json","request_key":"req_json","if_board_updated_at":"1999-01-01T00:00:00Z"}`)
		raw := runCLIForTest(t, home, map[string]string{}, stdin, []string{
			"--json", "--base-url", server.URL, "--agent", "agent-b",
			"boards", "cards", "create-batch", "--board-id", boardID,
			"--actor-id", flagActor,
			"--request-key", "req_cli",
			"--if-board-updated-at", "2026-04-12T00:00:00Z",
		})
		assertEnvelopeOK(t, raw)
	})
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

func TestArtifactContentOutputFile(t *testing.T) {
	t.Parallel()

	expected := []byte("artifact file body")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/artifacts/artifact-file/content" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write(expected)
	}))
	defer server.Close()

	home := t.TempDir()
	outPath := filepath.Join(t.TempDir(), "artifact.txt")
	out := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"artifacts", "content", "--artifact-id", "artifact-file", "-o", outPath,
	})
	if !strings.Contains(out, "wrote 18 bytes") {
		t.Fatalf("expected write summary, got %q", out)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	if !bytes.Equal(got, expected) {
		t.Fatalf("unexpected file bytes: got=%q want=%q", got, expected)
	}
}

func TestArtifactContentJSONOutputFile(t *testing.T) {
	t.Parallel()

	expected := []byte("json file body")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/artifacts/artifact-json-file/content" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write(expected)
	}))
	defer server.Close()

	home := t.TempDir()
	outPath := filepath.Join(t.TempDir(), "artifact.txt")
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json", "--base-url", server.URL,
		"artifacts", "content", "--artifact-id", "artifact-json-file", "--output", outPath,
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	if got := anyStringValue(data["output_path"]); got != outPath {
		t.Fatalf("expected output_path %q, got %#v", outPath, data)
	}
	if got := int(data["bytes_written"].(float64)); got != len(expected) {
		t.Fatalf("expected bytes_written %d, got %#v", len(expected), data)
	}
	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output file: %v", err)
	}
	if !bytes.Equal(got, expected) {
		t.Fatalf("unexpected file bytes: got=%q want=%q", got, expected)
	}
}

func TestArtifactsInspectCommand(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/artifacts/artifact_1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"artifact":{"id":"artifact_1","kind":"attachment","summary":"Brief"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/artifacts/artifact_1/content":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("artifact body"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"artifacts", "inspect",
		"--artifact-id", "artifact_1",
	})
	payload := assertEnvelopeOK(t, raw)
	if got := anyStringValue(payload["command"]); got != "artifacts inspect" {
		t.Fatalf("unexpected command label: %#v", payload)
	}
	data, _ := payload["data"].(map[string]any)
	artifact, _ := data["artifact"].(map[string]any)
	content, _ := data["content"].(map[string]any)
	if got := anyStringValue(artifact["id"]); got != "artifact_1" {
		t.Fatalf("expected artifact id artifact_1, got %#v", data)
	}
	if got := anyStringValue(content["body_text"]); got != "artifact body" {
		t.Fatalf("expected artifact content text, got %#v", data)
	}
}

func TestCommitmentsInspectAliasIsRemoved(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "commitments", "inspect", "--commitment-id", "commitment_1"})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "unknown_command" {
		t.Fatalf("expected removed commitments inspect alias to fail, payload=%#v", payload)
	}
}

func TestEventsTailReconnect(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	requests := make([]string, 0, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stream/events" {
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
	raw := runCLIForTest(t, home, env, nil, []string{
		"--json",
		"--base-url", server.URL,
		"events", "tail",
		"--types", "message_posted,document_created",
		"--max-events", "2",
	})

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
	mu.Lock()
	capturedRequests := append([]string(nil), requests...)
	mu.Unlock()
	for _, rawQuery := range capturedRequests {
		query, err := url.ParseQuery(rawQuery)
		if err != nil {
			t.Fatalf("parse query %q: %v", rawQuery, err)
		}
		if got := query["types"]; len(got) != 0 {
			t.Fatalf("expected no legacy types query parameter, got %q", rawQuery)
		}
		gotTypes := query["type"]
		if len(gotTypes) != 2 || gotTypes[0] != "message_posted" || gotTypes[1] != "document_created" {
			t.Fatalf("expected repeated type query values, got %q", rawQuery)
		}
	}
	firstData, _ := events[0]["data"].(map[string]any)
	secondData, _ := events[1]["data"].(map[string]any)
	if firstData["id"] != "e-1" || secondData["id"] != "e-2" {
		t.Fatalf("unexpected stream ids: first=%v second=%v", firstData["id"], secondData["id"])
	}
}

var (
	goldenInboxAliasPattern   = regexp.MustCompile(`"alias": "ibx_[^"]+"`)
	goldenInboxItemShortIDPat = regexp.MustCompile(`"short_id": "inbox:[^"]+"`)
)

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

func stableMachineInboxJSON(s string) string {
	s = goldenInboxAliasPattern.ReplaceAllString(s, `"alias": "ibx_STABLE"`)
	return goldenInboxItemShortIDPat.ReplaceAllString(s, `"short_id": "inbox:STABLE"`)
}

// assertGoldenStabilizedInboxAliases compares thread machine JSON but normalizes non-deterministic
// inbox item fields (per-run ibx_ alias, truncated inbox: short_id).
func assertGoldenStabilizedInboxAliases(t *testing.T, goldenFile string, actual string) {
	t.Helper()
	path := filepath.Join("testdata", goldenFile)
	expected, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	if stableMachineInboxJSON(string(expected)) != stableMachineInboxJSON(actual) {
		t.Fatalf("golden mismatch for %s\n--- expected ---\n%s\n--- actual ---\n%s", goldenFile, string(expected), actual)
	}
}

func proposalIDFromEnvelope(t *testing.T, payload map[string]any) string {
	t.Helper()
	data, _ := payload["data"].(map[string]any)
	proposalID := anyStringValue(data["proposal_id"])
	if strings.TrimSpace(proposalID) == "" {
		t.Fatalf("expected proposal_id in payload=%#v", payload)
	}
	return proposalID
}

func normalizeProposalEnvelopeForGolden(t *testing.T, raw string) string {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode proposal envelope json: %v raw=%s", err, raw)
	}
	data, _ := payload["data"].(map[string]any)
	if data == nil {
		return raw
	}
	proposalID := strings.TrimSpace(anyStringValue(data["proposal_id"]))
	if proposalID == "" {
		return raw
	}
	data["proposal_id"] = "draft-PLACEHOLDER"
	if path := strings.TrimSpace(anyStringValue(data["proposal_path"])); path != "" {
		data["proposal_path"] = filepath.Join("/tmp", "draft-PLACEHOLDER.json")
	}
	if applyCommand := strings.TrimSpace(anyStringValue(data["apply_command"])); applyCommand != "" {
		data["apply_command"] = strings.ReplaceAll(applyCommand, proposalID, "draft-PLACEHOLDER")
	}
	payload["data"] = data
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatalf("encode normalized proposal envelope: %v payload=%#v", err, payload)
	}
	return string(encoded) + "\n"
}

func TestInboxTailReconnect(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stream/inbox" {
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
		if r.URL.Path != "/stream/events" {
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

func TestMachineFacingTargetedCommandGoldens(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_123/timeline":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"thread_id":"thread_123",
				"events":[
					{"id":"event_100","thread_id":"thread_123","type":"message_posted","created_at":"2026-03-07T00:00:00Z","summary":"ship machine-facing fixes"},
					{"id":"event_101","thread_id":"thread_123","type":"human_attention_requested","created_at":"2026-03-07T00:01:00Z","summary":"confirm frame shape"}
				],
				"artifacts":{}
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/events/event_456":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"event":{"id":"event_456","thread_id":"thread_123","type":"message_posted","summary":"canonical event payload"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_123/context":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
					"thread":{"id":"thread_123","title":"Machine-facing consistency"},
				"recent_events":[
					{"id":"event_ctx_1","thread_id":"thread_123","type":"message_posted","summary":"normalize frame shape"},
					{"id":"event_ctx_2","thread_id":"thread_123","type":"human_attention_requested","summary":"confirm canonical command labels"}
				],
				"key_artifacts":[{"id":"artifact_ctx_1","kind":"attachment"}],
				"open_cards":[{"id":"card_ctx_1","status":"open"}],
					"documents":[
						{"id":"doc_ctx_1","title":"Runbook","state":"active","updated_at":"2026-03-07T00:02:00Z","head_revision":{"revision_id":"rev_ctx_1","revision_number":3,"content_type":"text","artifact_id":"artifact_doc_ctx_1","created_at":"2026-03-07T00:02:00Z"}}
					]
				}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_123/workspace":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
					"thread_id":"thread_123",
					"thread":{"id":"thread_123","title":"Machine-facing consistency"},
					"context":{
						"recent_events":[
							{"id":"event_ctx_1","thread_id":"thread_123","type":"message_posted","summary":"normalize frame shape"},
							{"id":"event_ctx_2","thread_id":"thread_123","type":"human_attention_requested","summary":"confirm canonical command labels"}
						],
						"key_artifacts":[{"id":"artifact_ctx_1","kind":"attachment"}],
						"open_cards":[{"id":"card_ctx_1","status":"open"}],
						"documents":[
							{"id":"doc_ctx_1","title":"Runbook","state":"active","updated_at":"2026-03-07T00:02:00Z","head_revision":{"revision_id":"rev_ctx_1","revision_number":3,"content_type":"text","artifact_id":"artifact_doc_ctx_1","created_at":"2026-03-07T00:02:00Z"}}
						]
					},
					"collaboration":{
						"key_artifacts":[{"id":"artifact_ctx_1","kind":"attachment"}],
						"open_cards":[{"id":"card_ctx_1","status":"open"}],
						"artifact_count":1,
						"open_card_count":1
					},
					"inbox":{
						"thread_id":"thread_123",
						"items":[
							{"id":"inbox:action_needed:thread_123:none:event_ctx_2","thread_id":"thread_123","type":"action_needed","summary":"confirm canonical command labels"}
						],
						"count":1
					},
					"pending_attention":{
						"thread_id":"thread_123",
						"items":[
							{"id":"inbox:action_needed:thread_123:none:event_ctx_2","thread_id":"thread_123","type":"action_needed","summary":"confirm canonical command labels"}
						],
						"count":1
					},
					"related_threads":{"count":0,"items":[]},
					"total_review_items":1,
					"follow_up":{
						"context_refresh_command":"anx threads context --thread-id thread_123 --include-artifact-content --full-id --json",
						"events_get_examples":[
							"anx events get --event-id event_ctx_1 --json",
							"anx events get --event-id event_ctx_2 --json"
						],
						"events_get_template":"anx events get --event-id <event-id> --json"
					},
					"context_source":"threads.context",
					"inbox_source":"inbox.list"
				}`))
		case r.Method == http.MethodGet && r.URL.Path == "/boards":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"boards":[
					{
						"board":{"id":"board_1234567890abcdef","title":"Machine Board","state":"active"},
						"summary":{
							"card_count":1,
							"cards_by_column":{"backlog":0,"ready":0,"in_progress":1,"blocked":0,"review":0,"done":0},
							"unresolved_card_count":2,
							"document_count":1,
							"latest_activity_at":"2026-03-07T00:03:00Z",
							"has_document_refs":true
						}
					}
				]
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/boards/board_1234567890abcdef/workspace":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"board_id":"board_1234567890abcdef",
				"board":{"id":"board_1234567890abcdef","title":"Machine Board","state":"active","updated_at":"2026-03-07T00:03:00Z"},
				"cards":{
					"items":[
						{
							"card":{"board_id":"board_1234567890abcdef","thread_id":"thread_123","column_key":"in_progress","rank":"m","pinned_document_id":null,"created_at":"2026-03-07T00:00:00Z","created_by":"actor_1","updated_at":"2026-03-07T00:03:00Z","updated_by":"actor_1"},
							"thread":{"id":"thread_123","title":"Machine-facing consistency"},
							"summary":{"related_topic_count":1,"decision_request_count":1,"decision_count":0,"recommendation_count":1,"document_count":1,"inbox_count":1,"latest_activity_at":"2026-03-07T00:03:00Z","stale":false},
							"pinned_document":null
						}
					],
					"count":1
				},
				"documents":{"items":[{"id":"doc_ctx_1","title":"Runbook","state":"active"}],"count":1},
				"inbox":{"items":[{"id":"inbox:action_needed:thread_123:none:event_ctx_2","thread_id":"thread_123","type":"action_needed"}],"count":1},
				"board_summary":{
					"card_count":1,
					"cards_by_column":{"backlog":0,"ready":0,"in_progress":1,"blocked":0,"review":0,"done":0},
					"unresolved_card_count":1,
					"document_count":1,
					"latest_activity_at":"2026-03-07T00:03:00Z",
					"has_document_refs":true
				},
				"warnings":{"items":[],"count":0},
				"section_kinds":{"board":"canonical","cards":"canonical","documents":"derived","topics":"derived","inbox":"derived","warnings":"derived"},
				"generated_at":"2026-03-07T00:03:00Z"
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/inbox":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
					"items":[
					{"id":"inbox:action_needed:thread_123:none:event_ctx_2","thread_id":"thread_123","type":"action_needed","summary":"confirm canonical command labels"},
					{"id":"inbox:action_needed:thread_other:none:event_other","thread_id":"thread_other","type":"action_needed","summary":"ignore other thread"}
				]
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/stream/events":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "id: es_1\nevent: event\ndata: {\"event\":{\"id\":\"event_stream_1\",\"type\":\"message_posted\"}}\n\n")
		case r.Method == http.MethodGet && r.URL.Path == "/stream/inbox":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "id: ibx_1\nevent: inbox_item\ndata: {\"item\":{\"id\":\"inbox:1\",\"thread_id\":\"thread_123\"}}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}

	eventsListOut := runCLIForTest(t, home, env, nil, []string{
		"--json",
		"--base-url", server.URL,
		"events", "list",
		"--thread-id", "thread_123",
		"--type", "message_posted",
	})
	assertGolden(t, "events_list_machine.golden.json", eventsListOut)

	eventsGetOut := runCLIForTest(t, home, env, nil, []string{
		"--json",
		"--base-url", server.URL,
		"events", "get",
		"--event-id", "event_456",
	})
	assertGolden(t, "events_get_machine.golden.json", eventsGetOut)

	threadsContextOut := runCLIForTest(t, home, env, nil, []string{
		"--json",
		"--base-url", server.URL,
		"threads", "context",
		"--thread-id", "thread_123",
	})
	assertGolden(t, "threads_context_machine.golden.json", threadsContextOut)

	threadsInspectOut := runCLIForTest(t, home, env, nil, []string{
		"--json",
		"--base-url", server.URL,
		"threads", "inspect",
		"--thread-id", "thread_123",
	})
	assertGoldenStabilizedInboxAliases(t, "threads_inspect_machine.golden.json", threadsInspectOut)
	threadsInspectPayload := assertEnvelopeOK(t, threadsInspectOut)
	if got := anyStringValue(threadsInspectPayload["command"]); got != "threads inspect" {
		t.Fatalf("expected threads inspect command label, got %#v", threadsInspectPayload)
	}
	if got := anyStringValue(threadsInspectPayload["command_id"]); got != "threads.inspect" {
		t.Fatalf("expected threads.inspect command_id, got %#v", threadsInspectPayload)
	}
	threadsInspectData, _ := threadsInspectPayload["data"].(map[string]any)
	if _, ok := threadsInspectData["thread"].(map[string]any); !ok {
		t.Fatalf("expected thread section in inspect payload, got %#v", threadsInspectData)
	}
	if _, ok := threadsInspectData["context"].(map[string]any); !ok {
		t.Fatalf("expected context section in inspect payload, got %#v", threadsInspectData)
	}
	if _, ok := threadsInspectData["collaboration"].(map[string]any); !ok {
		t.Fatalf("expected collaboration section in inspect payload, got %#v", threadsInspectData)
	}
	if _, ok := threadsInspectData["inbox"].(map[string]any); !ok {
		t.Fatalf("expected inbox section in inspect payload, got %#v", threadsInspectData)
	}

	threadsWorkspaceOut := runCLIForTest(t, home, env, nil, []string{
		"--json",
		"--base-url", server.URL,
		"threads", "workspace",
		"--thread-id", "thread_123",
	})
	assertGoldenStabilizedInboxAliases(t, "threads_workspace_machine.golden.json", threadsWorkspaceOut)
	threadsWorkspacePayload := assertEnvelopeOK(t, threadsWorkspaceOut)
	if got := anyStringValue(threadsWorkspacePayload["command"]); got != "threads workspace" {
		t.Fatalf("expected threads workspace command label, got %#v", threadsWorkspacePayload)
	}
	if got := anyStringValue(threadsWorkspacePayload["command_id"]); got != "threads.workspace" {
		t.Fatalf("expected threads.workspace command_id, got %#v", threadsWorkspacePayload)
	}
	threadsWorkspaceData, _ := threadsWorkspacePayload["data"].(map[string]any)
	if _, ok := threadsWorkspaceData["context"].(map[string]any); !ok {
		t.Fatalf("expected context section in workspace payload, got %#v", threadsWorkspaceData)
	}
	if _, ok := threadsWorkspaceData["related_threads"].(map[string]any); !ok {
		t.Fatalf("expected related_threads section in workspace payload, got %#v", threadsWorkspaceData)
	}
	if _, ok := threadsWorkspaceData["pending_attention"].(map[string]any); !ok {
		t.Fatalf("expected pending_attention section in workspace payload, got %#v", threadsWorkspaceData)
	}

	boardsListOut := runCLIForTest(t, home, env, nil, []string{
		"--json",
		"--base-url", server.URL,
		"boards", "list",
	})
	assertGolden(t, "boards_list_machine.golden.json", boardsListOut)

	boardsWorkspaceOut := runCLIForTest(t, home, env, nil, []string{
		"--json",
		"--base-url", server.URL,
		"boards", "workspace",
		"--board-id", "board_1234567890abcdef",
	})
	assertGolden(t, "boards_workspace_machine.golden.json", boardsWorkspaceOut)

	threadsReviewOut := runCLIForTest(t, home, env, nil, []string{
		"--json",
		"--base-url", server.URL,
		"threads", "review",
		"--thread-id", "thread_123",
	})
	threadsReviewPayload := assertEnvelopeOK(t, threadsReviewOut)
	if got := anyStringValue(threadsReviewPayload["command"]); got != "threads review" {
		t.Fatalf("expected threads review command label, got %#v", threadsReviewPayload)
	}
	if got := anyStringValue(threadsReviewPayload["command_id"]); got != "threads.review" {
		t.Fatalf("expected threads.review command_id, got %#v", threadsReviewPayload)
	}
	threadsReviewData, _ := threadsReviewPayload["data"].(map[string]any)
	if got := anyBoolValue(threadsReviewData["review_mode"]); !got {
		t.Fatalf("expected review_mode marker in review payload, got %#v", threadsReviewData)
	}

	threadsRecommendationsOut := runCLIForTest(t, home, env, nil, []string{
		"--json",
		"--base-url", server.URL,
		"threads", "workspace",
		"--thread-id", "thread_123",
	})
	assertGoldenStabilizedInboxAliases(t, "threads_workspace_machine.golden.json", threadsRecommendationsOut)
	threadsRecommendationsPayload := assertEnvelopeOK(t, threadsRecommendationsOut)
	if got := anyStringValue(threadsRecommendationsPayload["command"]); got != "threads workspace" {
		t.Fatalf("expected threads workspace command label, got %#v", threadsRecommendationsPayload)
	}
	if got := anyStringValue(threadsRecommendationsPayload["command_id"]); got != "threads.workspace" {
		t.Fatalf("expected threads.workspace command_id, got %#v", threadsRecommendationsPayload)
	}
	threadsRecommendationsData, _ := threadsRecommendationsPayload["data"].(map[string]any)
	if _, ok := threadsRecommendationsData["collaboration"].(map[string]any); !ok {
		t.Fatalf("expected collaboration section in payload, got %#v", threadsRecommendationsData)
	}
	if _, ok := threadsRecommendationsData["pending_attention"].(map[string]any); !ok {
		t.Fatalf("expected pending_attention section in payload, got %#v", threadsRecommendationsData)
	}
	if _, ok := threadsRecommendationsData["follow_up"].(map[string]any); !ok {
		t.Fatalf("expected follow_up section in payload, got %#v", threadsRecommendationsData)
	}

	eventsStreamOut := runCLIForTest(t, home, env, nil, []string{
		"--json",
		"--base-url", server.URL,
		"events", "stream",
		"--max-events", "1",
	})
	assertGolden(t, "events_stream_machine.golden.json", eventsStreamOut)

	inboxStreamOut := runCLIForTest(t, home, env, nil, []string{
		"--json",
		"--base-url", server.URL,
		"inbox", "stream",
		"--max-events", "1",
	})
	assertGolden(t, "inbox_stream_machine.golden.json", inboxStreamOut)
}

func TestStreamAliasCommandsUseCanonicalMachineIdentity(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/stream/events":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "id: e-1\nevent: event\ndata: {\"event\":{\"id\":\"event_1\"}}\n\n")
		case "/stream/inbox":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "id: i-1\nevent: inbox_item\ndata: {\"item\":{\"id\":\"inbox:1\"}}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}

	eventsTail := assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{
		"--json",
		"--base-url", server.URL,
		"events", "tail",
		"--max-events", "1",
	}))
	if got := anyStringValue(eventsTail["command"]); got != "events stream" {
		t.Fatalf("expected canonical command events stream, got %q payload=%#v", got, eventsTail)
	}
	if got := anyStringValue(eventsTail["command_id"]); got != "events.stream" {
		t.Fatalf("expected command_id events.stream, got %q payload=%#v", got, eventsTail)
	}
	eventsTailData, _ := eventsTail["data"].(map[string]any)
	if got := anyStringValue(eventsTailData["payload_key"]); got != "event" {
		t.Fatalf("expected payload_key=event, got %#v", eventsTailData)
	}
	if _, ok := eventsTailData["event"].(map[string]any); !ok {
		t.Fatalf("expected explicit event payload key, got %#v", eventsTailData)
	}

	inboxTail := assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{
		"--json",
		"--base-url", server.URL,
		"inbox", "tail",
		"--max-events", "1",
	}))
	if got := anyStringValue(inboxTail["command"]); got != "inbox stream" {
		t.Fatalf("expected canonical command inbox stream, got %q payload=%#v", got, inboxTail)
	}
	if got := anyStringValue(inboxTail["command_id"]); got != "inbox.stream" {
		t.Fatalf("expected command_id inbox.stream, got %q payload=%#v", got, inboxTail)
	}
	inboxTailData, _ := inboxTail["data"].(map[string]any)
	if got := anyStringValue(inboxTailData["payload_key"]); got != "item" {
		t.Fatalf("expected payload_key=item, got %#v", inboxTailData)
	}
	if _, ok := inboxTailData["item"].(map[string]any); !ok {
		t.Fatalf("expected explicit item payload key, got %#v", inboxTailData)
	}

	eventsErr := assertEnvelopeError(t, runCLIForTest(t, home, env, nil, []string{
		"--json",
		"--base-url", server.URL,
		"events", "tail",
		"--max-events", "-1",
	}))
	if got := anyStringValue(eventsErr["command"]); got != "events stream" {
		t.Fatalf("expected canonical error command events stream, got %q payload=%#v", got, eventsErr)
	}
	if got := anyStringValue(eventsErr["command_id"]); got != "events.stream" {
		t.Fatalf("expected error command_id events.stream, got %q payload=%#v", got, eventsErr)
	}

	inboxErr := assertEnvelopeError(t, runCLIForTest(t, home, env, nil, []string{
		"--json",
		"--base-url", server.URL,
		"inbox", "tail",
		"--max-events", "-1",
	}))
	if got := anyStringValue(inboxErr["command"]); got != "inbox stream" {
		t.Fatalf("expected canonical error command inbox stream, got %q payload=%#v", got, inboxErr)
	}
	if got := anyStringValue(inboxErr["command_id"]); got != "inbox.stream" {
		t.Fatalf("expected error command_id inbox.stream, got %q payload=%#v", got, inboxErr)
	}
}

func TestMachineFacingNonStreamErrorsIncludeCommandIdentity(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	env := map[string]string{}

	eventsListErr := assertEnvelopeError(t, runCLIForTest(t, home, env, nil, []string{
		"--json",
		"events", "list",
		"--max-events", "-1",
	}))
	if got := anyStringValue(eventsListErr["command"]); got != "events list" {
		t.Fatalf("expected events list error command, got %q payload=%#v", got, eventsListErr)
	}
	if got := anyStringValue(eventsListErr["command_id"]); got != "events.list" {
		t.Fatalf("expected events.list command_id, got %q payload=%#v", got, eventsListErr)
	}

	eventsListLifecycleErr := assertEnvelopeError(t, runCLIForTest(t, home, env, nil, []string{
		"--json",
		"events", "list",
		"--include-trashed",
	}))
	if got := anyStringValue(eventsListLifecycleErr["command"]); got != "events list" {
		t.Fatalf("expected events list lifecycle error command, got %q payload=%#v", got, eventsListLifecycleErr)
	}
	if got := anyStringValue(eventsListLifecycleErr["command_id"]); got != "events.list" {
		t.Fatalf("expected events.list lifecycle command_id, got %q payload=%#v", got, eventsListLifecycleErr)
	}
	errObj, _ := eventsListLifecycleErr["error"].(map[string]any)
	if !strings.Contains(anyStringValue(errObj["message"]), "require --thread-id") {
		t.Fatalf("expected lifecycle error to mention --thread-id, got %#v", eventsListLifecycleErr)
	}

	eventsGetErr := assertEnvelopeError(t, runCLIForTest(t, home, env, nil, []string{
		"--json",
		"events", "get",
	}))
	if got := anyStringValue(eventsGetErr["command"]); got != "events get" {
		t.Fatalf("expected events get error command, got %q payload=%#v", got, eventsGetErr)
	}
	if got := anyStringValue(eventsGetErr["command_id"]); got != "events.get" {
		t.Fatalf("expected events.get command_id, got %q payload=%#v", got, eventsGetErr)
	}

	threadsContextErr := assertEnvelopeError(t, runCLIForTest(t, home, env, nil, []string{
		"--json",
		"threads", "context",
	}))
	if got := anyStringValue(threadsContextErr["command"]); got != "threads context" {
		t.Fatalf("expected threads context error command, got %q payload=%#v", got, threadsContextErr)
	}
	if got := anyStringValue(threadsContextErr["command_id"]); got != "threads.context" {
		t.Fatalf("expected threads.context command_id, got %q payload=%#v", got, threadsContextErr)
	}

	threadsRecommendationsErr := assertEnvelopeError(t, runCLIForTest(t, home, env, nil, []string{
		"--json",
		"threads", "workspace",
	}))
	if got := anyStringValue(threadsRecommendationsErr["command"]); got != "threads workspace" {
		t.Fatalf("expected threads workspace error command, got %q payload=%#v", got, threadsRecommendationsErr)
	}
	if got := anyStringValue(threadsRecommendationsErr["command_id"]); got != "threads.workspace" {
		t.Fatalf("expected threads.workspace command_id, got %q payload=%#v", got, threadsRecommendationsErr)
	}
}

func TestEventsStreamFallbackPayloadForNonWrapperJSON(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stream/events" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "id: e-fallback\nevent: event\ndata: {\"id\":\"event_raw_1\",\"type\":\"message_posted\"}\n\n")
	}))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}
	payload := assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{
		"--json",
		"--base-url", server.URL,
		"events", "stream",
		"--max-events", "1",
	}))
	data, _ := payload["data"].(map[string]any)
	if got := anyStringValue(data["payload_key"]); got != "data" {
		t.Fatalf("expected fallback payload_key=data, got %#v", data)
	}
	fallbackPayload, _ := data["payload"].(map[string]any)
	if got := anyStringValue(fallbackPayload["id"]); got != "event_raw_1" {
		t.Fatalf("expected fallback payload id event_raw_1, got %#v", data)
	}
	if _, hasEvent := data["event"]; hasEvent {
		t.Fatalf("expected no explicit event key for non-wrapper payload, got %#v", data)
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
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("parse json: %v stdout=%s", err, stdout.String())
	}
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "unsupported_command" {
		t.Fatalf("expected unsupported_command error payload=%#v", payload)
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
	if !strings.Contains(message, "`anx docs revision get <document-id> <revision-id>`") {
		t.Fatalf("expected usage examples in required message, got %q", message)
	}
}

func TestFilterEventsByLifecycleState(t *testing.T) {
	t.Parallel()

	active := map[string]any{"id": "evt_a", "type": "message_posted"}
	archived := map[string]any{"id": "evt_b", "archived_at": "2024-01-01T00:00:00Z"}
	tomb := map[string]any{"id": "evt_c", "trashed_at": "2024-01-02T00:00:00Z"}
	archivedAndTomb := map[string]any{"id": "evt_d", "archived_at": "2024-01-01T00:00:00Z", "trashed_at": "2024-01-02T00:00:00Z"}

	events := []any{active, archived, tomb, archivedAndTomb}

	def := filterEventsByLifecycleState(events, false, false, false, false)
	if len(def) != 1 || anyString(asMap(def[0])["id"]) != "evt_a" {
		t.Fatalf("default filter: want active only, got %#v", def)
	}

	incAll := filterEventsByLifecycleState(events, true, false, true, false)
	if len(incAll) != 4 {
		t.Fatalf("include both: want all four, got %d %#v", len(incAll), incAll)
	}

	tombOnly := filterEventsByLifecycleState(events, false, false, false, true)
	if len(tombOnly) != 2 {
		t.Fatalf("trashed-only: want 2, got %#v", tombOnly)
	}

	archOnly := filterEventsByLifecycleState(events, false, true, false, false)
	if len(archOnly) != 1 || anyString(asMap(archOnly[0])["id"]) != "evt_b" {
		t.Fatalf("archived-only: want evt_b, got %#v", archOnly)
	}
}

func Example_anxThreadsList() {
	fmt.Println("anx threads list --state active")
	// Output: anx threads list --state active
}

func writeAgentProfile(t *testing.T, home string, agent string, profileJSON string) {
	t.Helper()
	profilesDir := filepath.Join(home, ".config", "anx", "profiles")
	if err := os.MkdirAll(profilesDir, 0o700); err != nil {
		t.Fatalf("mkdir profiles dir: %v", err)
	}
	profilePath := filepath.Join(profilesDir, agent+".json")
	if err := os.WriteFile(profilePath, []byte(profileJSON), 0o600); err != nil {
		t.Fatalf("write profile: %v", err)
	}
}

func anyBoolValue(raw any) bool {
	value, _ := raw.(bool)
	return value
}

func TestEnsureEmptyListDefaults(t *testing.T) {
	t.Parallel()

	t.Run("injects_missing_fields", func(t *testing.T) {
		t.Parallel()
		body := map[string]any{
			"topic": map[string]any{
				"title": "test",
			},
		}
		ensureEmptyListDefaults(body, "topic", []string{"owner_refs", "document_refs", "board_refs", "related_refs"})
		topic := body["topic"].(map[string]any)
		for _, field := range []string{"owner_refs", "document_refs", "board_refs", "related_refs"} {
			val, ok := topic[field].([]any)
			if !ok || len(val) != 0 {
				t.Fatalf("expected %s to be empty slice, got %#v", field, topic[field])
			}
		}
	})

	t.Run("preserves_existing_values", func(t *testing.T) {
		t.Parallel()
		body := map[string]any{
			"card": map[string]any{
				"title":         "test",
				"assignee_refs": []any{"actor:abc"},
				"related_refs":  []any{},
			},
		}
		ensureEmptyListDefaults(body, "card", []string{"assignee_refs", "resolution_refs", "related_refs"})
		card := body["card"].(map[string]any)
		if got := card["assignee_refs"].([]any); len(got) != 1 || got[0] != "actor:abc" {
			t.Fatalf("expected assignee_refs preserved, got %#v", got)
		}
		if got := card["related_refs"].([]any); len(got) != 0 {
			t.Fatalf("expected related_refs preserved as empty, got %#v", got)
		}
		if got := card["resolution_refs"].([]any); len(got) != 0 {
			t.Fatalf("expected resolution_refs defaulted to empty, got %#v", got)
		}
	})

	t.Run("no_op_when_nest_missing", func(t *testing.T) {
		t.Parallel()
		body := map[string]any{"title": "test"}
		ensureEmptyListDefaults(body, "topic", []string{"owner_refs"})
		if _, exists := body["owner_refs"]; exists {
			t.Fatal("should not inject at root level")
		}
	})

	t.Run("no_op_when_nil", func(t *testing.T) {
		t.Parallel()
		ensureEmptyListDefaults(nil, "topic", []string{"owner_refs"})
	})
}

func TestCreateCommandsDefaultEmptyListFields(t *testing.T) {
	t.Parallel()

	t.Run("topics_create", func(t *testing.T) {
		t.Parallel()
		var receivedBody map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/topics" {
				body, _ := io.ReadAll(r.Body)
				json.Unmarshal(body, &receivedBody)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"topic":{"id":"t1","state":"active","title":"T","summary":"S","owner_refs":[],"document_refs":[],"board_refs":[],"related_refs":[],"provenance":{"sources":[]}}}`))
				return
			}
			http.NotFound(w, r)
		}))
		defer server.Close()

		home := t.TempDir()
		env := map[string]string{}
		input := `{"topic":{"title":"T","summary":"S","owner_refs":[],"document_refs":[],"board_refs":[],"related_refs":[],"provenance":{"sources":[]}}}`
		result := runCLIForTest(t, home, env, strings.NewReader(input), []string{"--json", "--base-url", server.URL, "topics", "create"})
		assertEnvelopeOK(t, result)

		topic := receivedBody["topic"].(map[string]any)
		for _, field := range []string{"owner_refs", "document_refs", "board_refs", "related_refs"} {
			val, ok := topic[field].([]any)
			if !ok || len(val) != 0 {
				t.Fatalf("expected %s to be empty slice, got %#v", field, topic[field])
			}
		}
	})

	t.Run("boards_create", func(t *testing.T) {
		t.Parallel()
		var receivedBody map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/boards" {
				body, _ := io.ReadAll(r.Body)
				json.Unmarshal(body, &receivedBody)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"board":{"id":"b1","title":"B","state":"active","document_refs":[],"pinned_refs":[],"provenance":{"sources":[]}}}`))
				return
			}
			http.NotFound(w, r)
		}))
		defer server.Close()

		home := t.TempDir()
		env := map[string]string{}
		input := `{"board":{"title":"B","provenance":{"sources":[]}}}`
		result := runCLIForTest(t, home, env, strings.NewReader(input), []string{"--json", "--base-url", server.URL, "boards", "create"})
		assertEnvelopeOK(t, result)

		board := receivedBody["board"].(map[string]any)
		for _, field := range []string{"document_refs", "pinned_refs"} {
			val, ok := board[field].([]any)
			if !ok || len(val) != 0 {
				t.Fatalf("expected %s to be empty slice, got %#v", field, board[field])
			}
		}
	})

	t.Run("cards_create", func(t *testing.T) {
		t.Parallel()
		var receivedBody map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/cards" {
				body, _ := io.ReadAll(r.Body)
				json.Unmarshal(body, &receivedBody)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"card":{"id":"c1","board_id":"board_1","title":"C","summary":"S","column_key":"backlog","assignee_refs":[],"resolution_refs":[],"related_refs":[],"provenance":{"sources":[]}}}`))
				return
			}
			http.NotFound(w, r)
		}))
		defer server.Close()

		home := t.TempDir()
		env := map[string]string{}
		input := `{"board_ref":"board:board_1","card":{"title":"C","summary":"S","column_key":"backlog","provenance":{"sources":[]}}}`
		result := runCLIForTest(t, home, env, strings.NewReader(input), []string{"--json", "--base-url", server.URL, "cards", "create"})
		assertEnvelopeOK(t, result)

		card := receivedBody["card"].(map[string]any)
		for _, field := range []string{"assignee_refs", "resolution_refs", "related_refs"} {
			val, ok := card[field].([]any)
			if !ok || len(val) != 0 {
				t.Fatalf("expected %s to be empty slice, got %#v", field, card[field])
			}
		}
	})

	t.Run("boards_cards_create", func(t *testing.T) {
		t.Parallel()
		var receivedBody map[string]any
		boardID := "board_1"
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/boards/"+boardID+"/cards" {
				body, _ := io.ReadAll(r.Body)
				json.Unmarshal(body, &receivedBody)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"board":{"id":"` + boardID + `","updated_at":"2026-04-12T00:00:00Z"},"card":{"id":"c1","board_id":"` + boardID + `","title":"C","column_key":"backlog","assignee_refs":[],"resolution_refs":[],"related_refs":[],"provenance":{"sources":[]}}}`))
				return
			}
			http.NotFound(w, r)
		}))
		defer server.Close()

		home := t.TempDir()
		env := map[string]string{}
		input := `{"card":{"title":"C","column_key":"backlog","provenance":{"sources":[]}}}`
		result := runCLIForTest(t, home, env, strings.NewReader(input), []string{"--json", "--base-url", server.URL, "boards", "cards", "create", "--board-id", boardID})
		assertEnvelopeOK(t, result)

		card := receivedBody["card"].(map[string]any)
		for _, field := range []string{"assignee_refs", "resolution_refs", "related_refs"} {
			val, ok := card[field].([]any)
			if !ok || len(val) != 0 {
				t.Fatalf("expected %s to be empty slice, got %#v", field, card[field])
			}
		}
	})

	t.Run("explicit_values_preserved", func(t *testing.T) {
		t.Parallel()
		var receivedBody map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && r.URL.Path == "/topics" {
				body, _ := io.ReadAll(r.Body)
				json.Unmarshal(body, &receivedBody)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"topic":{"id":"t1"}}`))
				return
			}
			http.NotFound(w, r)
		}))
		defer server.Close()

		home := t.TempDir()
		env := map[string]string{}
		input := `{"topic":{"title":"T","summary":"S","owner_refs":["actor:x"],"document_refs":[],"board_refs":["board:y"],"related_refs":[],"provenance":{"sources":[]}}}`
		result := runCLIForTest(t, home, env, strings.NewReader(input), []string{"--json", "--base-url", server.URL, "topics", "create"})
		assertEnvelopeOK(t, result)

		topic := receivedBody["topic"].(map[string]any)
		if refs := topic["owner_refs"].([]any); len(refs) != 1 || refs[0] != "actor:x" {
			t.Fatalf("expected owner_refs preserved, got %#v", refs)
		}
		if refs := topic["document_refs"].([]any); len(refs) != 0 {
			t.Fatalf("expected document_refs preserved as empty, got %#v", refs)
		}
		if refs := topic["board_refs"].([]any); len(refs) != 1 || refs[0] != "board:y" {
			t.Fatalf("expected board_refs preserved, got %#v", refs)
		}
	})
}
