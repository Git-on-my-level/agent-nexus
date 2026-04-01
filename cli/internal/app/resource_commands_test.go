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
	"strings"
	"sync"
	"testing"

	"organization-autorunner-cli/internal/config"
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
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_1":
			_, _ = w.Write([]byte(`{"thread":{"id":"thread_1","title":"Alpha","status":"active"}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/threads/thread_1":
			body, _ := io.ReadAll(r.Body)
			if !bytes.Contains(body, []byte(`"patch":{"status":"resolved"}`)) {
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

	patchOut := runCLIForTest(t, home, env, strings.NewReader(`{"patch":{"status":"resolved"}}`), []string{"--json", "--base-url", server.URL, "threads", "patch", "--thread-id", "thread_1"})
	assertGolden(t, "threads_patch.golden.json", patchOut)

	proposeOut := runCLIForTest(t, home, env, strings.NewReader(`{"patch":{"status":"resolved"}}`), []string{"--json", "--base-url", server.URL, "threads", "propose-patch", "--thread-id", "thread_1"})
	assertGolden(t, "threads_propose_patch.golden.json", normalizeProposalEnvelopeForGolden(t, proposeOut))
	proposalID := proposalIDFromEnvelope(t, assertEnvelopeOK(t, proposeOut))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "threads", "apply", "--proposal-id", proposalID}))

	timelineOut := runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "threads", "timeline", "--thread-id", "thread_1"})
	timelinePayload := assertEnvelopeOK(t, timelineOut)
	if got := timelinePayload["command"]; got != "threads timeline" {
		t.Fatalf("expected threads timeline command label, got %#v", got)
	}
}

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

func TestActorsCommands(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/actors":
			if got := r.URL.Query().Get("q"); got != "bot" {
				t.Fatalf("expected actors q=bot, got %q", got)
			}
			if got := r.URL.Query().Get("limit"); got != "50" {
				t.Fatalf("expected actors limit=50, got %q", got)
			}
			if got := r.URL.Query().Get("cursor"); got != "cursor-actors" {
				t.Fatalf("expected actors cursor=cursor-actors, got %q", got)
			}
			_, _ = w.Write([]byte(`{"actors":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/actors":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode actors register body: %v", err)
			}
			actor, _ := body["actor"].(map[string]any)
			if anyStringValue(actor["id"]) != "bot-1" || anyStringValue(actor["display_name"]) != "Bot 1" {
				t.Fatalf("unexpected actor register body: %#v", body)
			}
			if anyStringValue(actor["created_at"]) != "2026-03-04T10:00:00Z" {
				t.Fatalf("unexpected actor created_at: %#v", body)
			}
			tags, _ := actor["tags"].([]any)
			if len(tags) != 2 || anyStringValue(tags[0]) != "human" || anyStringValue(tags[1]) != "ops" {
				t.Fatalf("unexpected actor tags: %#v", body)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"actor":{"id":"bot-1","display_name":"Bot 1","created_at":"2026-03-04T10:00:00Z","tags":["human","ops"]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}

	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{
		"--json", "--base-url", server.URL,
		"actors", "list",
		"--q", "bot",
		"--limit", "50",
		"--cursor", "cursor-actors",
	}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{
		"--json", "--base-url", server.URL,
		"actors", "register",
		"--id", "bot-1",
		"--display-name", "Bot 1",
		"--created-at", "2026-03-04T10:00:00Z",
		"--tag", "human",
		"--tag", "ops",
	}))
}

func TestTypedWorkflowCommands(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/threads":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"thread":{"id":"thread_flow_1","title":"Flow Thread","status":"active"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_flow_1":
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
	assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"patch":{"status":"resolved"}}`), []string{"--json", "--base-url", server.URL, "threads", "patch", "thread_flow_1"}))
	threadPatchProposal := assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"patch":{"status":"resolved"}}`), []string{"--json", "--base-url", server.URL, "threads", "propose-patch", "thread_flow_1"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "threads", "apply", proposalIDFromEnvelope(t, threadPatchProposal)}))
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
	if !strings.Contains(message, "valid subcommands: list, get, ack, stream, tail") {
		t.Fatalf("expected valid-subcommands guidance, got %q", message)
	}
	if !strings.Contains(message, "`oar inbox get --id <id-or-alias>`") || !strings.Contains(message, "`oar inbox ack --inbox-item-id <id-or-alias>`") {
		t.Fatalf("expected concrete inbox examples, got %q", message)
	}
	if !strings.Contains(message, "did you mean `oar inbox ack --inbox-item-id <id-or-alias>`?") {
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

func TestInboxListIncludesAliasesAndLinkedShortIDs(t *testing.T) {
	t.Parallel()

	const inboxID = "inbox:decision_needed:thread_1234567890:none:event_1234567890"
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
	if got := anyStringValue(item["short_id"]); got != inboxID[:12] {
		t.Fatalf("expected short_id %q, got %q payload=%#v", inboxID[:12], got, payload)
	}
	if got := anyStringValue(item["thread_short_id"]); got != threadID[:12] {
		t.Fatalf("expected thread_short_id %q, got %q payload=%#v", threadID[:12], got, payload)
	}
	if got := anyStringValue(item["source_event_short_id"]); got != eventID[:12] {
		t.Fatalf("expected source_event_short_id %q, got %q payload=%#v", eventID[:12], got, payload)
	}
}

func TestInboxListSupportsClientSideThreadAndTypeFilters(t *testing.T) {
	t.Parallel()

	const matchingID = "inbox:decision_needed:thread_1234567890:none:event_1234567890"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/inbox" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"items":[
			{"id":"` + matchingID + `","thread_id":"thread_1234567890","type":"decision_needed","summary":"needs approval"},
			{"id":"inbox:decision_needed:thread_other:none:event_other","thread_id":"thread_other","type":"decision_needed","summary":"other thread"},
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
		"--type", "decision_needed",
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
	if len(types) != 1 || types[0] != "decision_needed" {
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

	human := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"inbox", "list",
		"--thread-id", "thread_1234567890",
		"--type", "decision_needed",
	})
	if !strings.Contains(human, "total_items: 3") || !strings.Contains(human, "returned_items: 1") {
		t.Fatalf("expected rendered inbox counts in human output, got:\n%s", human)
	}
}

func TestInboxAliasStableAcrossListMembershipChanges(t *testing.T) {
	t.Parallel()

	const targetID = "inbox:decision_needed:thread_target:none:event_target"
	const otherID = "inbox:decision_needed:thread_other:none:event_other"

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

func TestInboxGetByAliasTargetsSingleItem(t *testing.T) {
	t.Parallel()

	const firstID = "inbox:decision_needed:thread_aaa:none:event_aaa"
	const secondID = "inbox:decision_needed:thread_bbb:none:event_bbb"
	alias := inboxAliasByID([]string{firstID, secondID})[secondID]

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/inbox":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items":[{"id":"` + firstID + `","thread_id":"thread_aaa"},{"id":"` + secondID + `","thread_id":"thread_bbb"}]}`))
			return
		case r.Method == http.MethodGet && r.URL.Path == "/inbox/"+url.PathEscape(secondID):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"item":{"id":"` + secondID + `","thread_id":"thread_bbb"},"generated_at":"2026-03-06T00:00:00Z"}`))
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
		"inbox", "get",
		"--id", alias,
	})
	payload := assertEnvelopeOK(t, raw)
	if got := anyStringValue(payload["command"]); got != "inbox get" {
		t.Fatalf("expected command inbox get, got %q payload=%#v", got, payload)
	}
	data, _ := payload["data"].(map[string]any)
	item, _ := data["item"].(map[string]any)
	if got := anyStringValue(item["id"]); got != secondID {
		t.Fatalf("expected inbox item %q, got %q payload=%#v", secondID, got, payload)
	}
	if got := anyStringValue(item["alias"]); got != alias {
		t.Fatalf("expected inbox alias %q, got %q payload=%#v", alias, got, payload)
	}
}

func TestInboxGetAcceptsInboxIDAliasFlag(t *testing.T) {
	t.Parallel()

	const inboxID = "inbox:decision_needed:thread_abc:none:event_abc"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/inbox":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items":[{"id":"` + inboxID + `","thread_id":"thread_abc"}]}`))
			return
		case r.Method == http.MethodGet && r.URL.Path == "/inbox/"+url.PathEscape(inboxID):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"item":{"id":"` + inboxID + `","thread_id":"thread_abc"}}`))
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
		"inbox", "get",
		"--inbox-id", inboxID,
	})
	payload := assertEnvelopeOK(t, raw)
	if got := anyStringValue(payload["command"]); got != "inbox get" {
		t.Fatalf("expected command inbox get, got %#v", payload)
	}
}

func TestInboxAckAliasResolvesCanonicalAndThreadFromInboxList(t *testing.T) {
	t.Parallel()

	const inboxID = "inbox:decision_needed:thread_42:none:event_42"
	alias := inboxAliasByID([]string{inboxID})[inboxID]

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/inbox":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"items":[{"id":"` + inboxID + `","thread_id":"thread_42"}]}`))
			return
		case r.Method == http.MethodPost && r.URL.Path == "/inbox/ack":
			body, _ := io.ReadAll(r.Body)
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("decode inbox ack body: %v body=%s", err, string(body))
			}
			if got := strings.TrimSpace(anyStringValue(payload["inbox_item_id"])); got != inboxID {
				t.Fatalf("expected canonical inbox_item_id %q, got %q body=%s", inboxID, got, string(body))
			}
			if got := strings.TrimSpace(anyStringValue(payload["thread_id"])); got != "thread_42" {
				t.Fatalf("expected resolved thread_id thread_42, got %q body=%s", got, string(body))
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"event":{"id":"event_ack_alias"}}`))
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
		"--inbox-item-id", alias,
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
	if !strings.Contains(message, "did you mean `oar events stream`?") {
		t.Fatalf("expected stream correction, got %q", message)
	}
	if !strings.Contains(message, "`oar events list --thread-id <thread-id> --type actor_statement --mine --full-id`") || !strings.Contains(message, "`oar events tail --max-events 20`") {
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
				{"id":"event_1","thread_id":"thread_1","type":"actor_statement","summary":"first"},
				{"id":"event_2","thread_id":"thread_1","type":"decision_needed","summary":"second"},
				{"id":"event_3","thread_id":"thread_1","type":"actor_statement","summary":"third"}
			],
			"snapshots":{},
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
		"--type", "actor_statement",
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

	human := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"events", "list",
		"--thread-id", "thread_1",
		"--type", "actor_statement",
		"--max-events", "1",
	})
	if !strings.Contains(human, "types:") || !strings.Contains(human, "- actor_statement") {
		t.Fatalf("expected human output to include selected filter types, got:\n%s", human)
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
				{"id":"` + mineEventID + `","thread_id":"thread_1","type":"actor_statement","actor_id":"` + mineActorID + `","payload":{"recommendation":"Ship Friday rescue scope"}},
				{"id":"event_other_actor","thread_id":"thread_1","type":"actor_statement","actor_id":"actor-other","summary":"other recommendation"}
			],
			"snapshots":{},
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
		"--type", "actor_statement",
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

	humanFull := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"--agent", "agent-a",
		"events", "list",
		"--thread-id", "thread_1",
		"--type", "actor_statement",
		"--mine",
		"--full-id",
	})
	if !strings.Contains(humanFull, mineEventID) || !strings.Contains(humanFull, "Ship Friday rescue scope") {
		t.Fatalf("expected full id + preview in human output, got:\n%s", humanFull)
	}

	humanShort := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"--agent", "agent-a",
		"events", "list",
		"--thread-id", "thread_1",
		"--type", "actor_statement",
		"--mine",
	})
	if strings.Contains(humanShort, mineEventID) {
		t.Fatalf("expected default short-id rendering without --full-id, got:\n%s", humanShort)
	}
	if !strings.Contains(humanShort, mineEventID[:12]) {
		t.Fatalf("expected short id rendering by default, got:\n%s", humanShort)
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
				{"id":"event_1","thread_id":"thread_1","type":"actor_statement","summary":"first","ts":"2026-03-06T12:01:00Z","created_at":"2026-03-06T12:10:00Z"},
				{"id":"event_2","thread_id":"thread_1","type":"actor_statement","summary":"second","ts":"2026-03-06T12:02:00Z","created_at":"2026-03-06T12:11:00Z"}
			],"snapshots":{"snapshot_1":{"id":"snapshot_1","title":"Snap One"}},"artifacts":{"artifact_1":{"id":"artifact_1","kind":"note"}}}`))
		case "/threads/thread_2/timeline":
			_, _ = w.Write([]byte(`{"thread_id":"thread_2","events":[
				{"id":"event_3","thread_id":"thread_2","type":"actor_statement","summary":"third","ts":"2026-03-06T12:03:00Z","created_at":"2026-03-06T12:00:00Z"},
				{"id":"event_4","thread_id":"thread_2","type":"actor_statement","summary":"fourth","ts":"2026-03-06T12:04:00Z","created_at":"2026-03-06T12:01:00Z"}
			],"snapshots":{"snapshot_2":{"id":"snapshot_2","title":"Snap Two"}},"artifacts":{"artifact_2":{"id":"artifact_2","kind":"report"}}}`))
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
		"--type", "actor_statement",
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
	snapshots, _ := data["snapshots"].(map[string]any)
	if len(snapshots) != 2 {
		t.Fatalf("expected merged timeline snapshots, got %#v", data["snapshots"])
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
			if got := strings.TrimSpace(r.URL.Query().Get("include_tombstoned")); got != "true" {
				t.Fatalf("expected include_tombstoned=true query, got %q", got)
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

	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "docs", "list", "--thread-id", "thread_docs_1", "--include-tombstoned"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"document":{"id":"doc_1"},"content":"initial","content_type":"text"}`), []string{"--json", "--base-url", server.URL, "docs", "create"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "docs", "get", "--document-id", "doc_1"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"actor_id":"actor_test","if_base_revision":"rev_1","content":"next","content_type":"text"}`), []string{"--json", "--base-url", server.URL, "docs", "update", "--document-id", "doc_1"}))
	docsUpdatePayload := assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"actor_id":"actor_test","if_base_revision":"rev_1","content":"next","content_type":"text"}`), []string{"--json", "--base-url", server.URL, "docs", "propose-update", "--document-id", "doc_1"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "docs", "apply", "--proposal-id", proposalIDFromEnvelope(t, docsUpdatePayload)}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "docs", "history", "--document-id", "doc_1"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "docs", "revision", "get", "--document-id", "doc_1", "--revision-id", "rev_1"}))
}

func TestDocsUpdateInjectsActorIDFromProfile(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/docs/doc_1" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"document":{"id":"doc_1","head_revision_id":"rev_1"},"revision":{"revision_id":"rev_1","revision_number":1,"content":"initial","content_type":"text"}}`))
			return
		}
		if r.Method != http.MethodPatch || r.URL.Path != "/docs/doc_1" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode docs update body: %v body=%s", err, string(body))
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
		"docs", "update",
		"--document-id", "doc_1",
	})
	assertEnvelopeOK(t, raw)
}

func TestDocsUpdateRequiresActiveActorIdentity(t *testing.T) {
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
		"docs", "update",
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
	if !strings.Contains(message, "oar auth register --username <name>") || !strings.Contains(message, "oar auth whoami") {
		t.Fatalf("expected actionable auth guidance, got %q payload=%#v", message, payload)
	}

	mu.Lock()
	gotRequests := requestCount
	mu.Unlock()
	if gotRequests != 0 {
		t.Fatalf("expected no HTTP requests when actor identity is missing, got %d", gotRequests)
	}
}

func TestProductManagerFlowRegisterThenDocsUpdate(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	docsUpdateCalls := 0
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
		case r.Method == http.MethodPatch && r.URL.Path == "/docs/northwave-pilot-rescue-brief":
			if gotAuth := strings.TrimSpace(r.Header.Get("Authorization")); gotAuth != "Bearer token-product-manager" {
				t.Fatalf("expected auth bearer token, got %q", gotAuth)
			}
			body, _ := io.ReadAll(r.Body)
			var payload map[string]any
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("decode docs update body: %v body=%s", err, string(body))
			}
			if got := strings.TrimSpace(anyStringValue(payload["actor_id"])); got != "actor-product-manager" {
				t.Fatalf("expected actor_id from registered profile, got %q body=%s", got, string(body))
			}
			mu.Lock()
			docsUpdateCalls++
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
		"docs", "update",
		"--document-id", "northwave-pilot-rescue-brief",
	}))

	mu.Lock()
	gotCalls := docsUpdateCalls
	mu.Unlock()
	if gotCalls != 1 {
		t.Fatalf("expected one docs update request, got %d", gotCalls)
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

func TestDocsValidateUpdateRequiresBaseRevision(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, strings.NewReader(`{"content":"next","content_type":"text"}`), []string{
		"--json",
		"docs", "validate-update",
		"--document-id", "doc_1",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "invalid_request" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	if message := anyStringValue(errObj["message"]); !strings.Contains(message, "if_base_revision") {
		t.Fatalf("expected if_base_revision guidance, got %q payload=%#v", message, payload)
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

func TestDocsValidateUpdateWithContentFile(t *testing.T) {
	t.Parallel()

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

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"docs", "validate-update",
		"--document-id", "doc_1",
		"--from-file", updateFile,
		"--content-file", contentFile,
	})
	payload := assertEnvelopeOK(t, raw)
	if got := anyStringValue(payload["command"]); got != "docs validate-update" {
		t.Fatalf("unexpected command label: %#v", payload)
	}
	data, _ := payload["data"].(map[string]any)
	body, _ := data["body"].(map[string]any)
	if got := anyStringValue(body["content"]); got != strings.TrimSpace(content) {
		t.Fatalf("expected content from file in validation payload, got %q payload=%#v", got, payload)
	}
}

func TestDocsValidateUpdateRejectsNullContent(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, strings.NewReader(`{"if_base_revision":"rev_1","content":null,"content_type":"text"}`), []string{
		"--json",
		"docs", "validate-update",
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
}

func TestDocsUpdateRejectsNullContentBeforeHTTP(t *testing.T) {
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
	raw := runCLIForTest(t, home, map[string]string{}, strings.NewReader(`{"if_base_revision":"rev_1","content":null,"content_type":"text"}`), []string{
		"--json",
		"--base-url", server.URL,
		"docs", "update",
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

func TestDocsProposeUpdateWithContentFileUsesFetchedDocumentState(t *testing.T) {
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
		case r.Method == http.MethodPatch && r.URL.Path == "/docs/doc_1":
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
		"docs", "propose-update",
		"--document-id", "doc_1",
		"--from-file", updateFile,
		"--content-file", contentFile,
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	if got := anyStringValue(data["path"]); got != "/docs/doc_1" {
		t.Fatalf("expected path /docs/doc_1, got %q payload=%#v", got, payload)
	}
	body, _ := data["body"].(map[string]any)
	if got := anyStringValue(body["content"]); got != strings.TrimSpace(content) {
		t.Fatalf("expected content-file override in proposal payload, got %q payload=%#v", got, payload)
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

func TestDocsProposeUpdatePreservesStructuredContentInDiff(t *testing.T) {
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
		"docs", "propose-update",
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

func TestDocsProposeUpdateTextDiffFallsBackWhenRevisionContentEmpty(t *testing.T) {
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
		"docs", "propose-update",
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

func TestCommitmentsPatchWritesImmediatelyAndProposePatchStages(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	getCalls := 0
	patchCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/commitments/commitment_1":
			mu.Lock()
			getCalls++
			mu.Unlock()
			_, _ = w.Write([]byte(`{"commitment":{"id":"commitment_1","thread_id":"thread_1","title":"Publish note","status":"open"}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/commitments/commitment_1":
			mu.Lock()
			patchCalls++
			mu.Unlock()
			body, _ := io.ReadAll(r.Body)
			if !bytes.Contains(body, []byte(`"patch":{"status":"done"}`)) {
				t.Fatalf("unexpected commitments patch body: %s", string(body))
			}
			_, _ = w.Write([]byte(`{"commitment":{"id":"commitment_1","thread_id":"thread_1","title":"Publish note","status":"done"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	assertEnvelopeOK(t, runCLIForTest(t, home, map[string]string{}, strings.NewReader(`{"patch":{"status":"done"}}`), []string{
		"--json",
		"--base-url", server.URL,
		"commitments", "patch",
		"--commitment-id", "commitment_1",
	}))

	mu.Lock()
	gotGetsAfterPatch := getCalls
	gotPatchesAfterPatch := patchCalls
	mu.Unlock()
	if gotGetsAfterPatch != 0 {
		t.Fatalf("expected no commitments get during direct patch, got %d", gotGetsAfterPatch)
	}
	if gotPatchesAfterPatch != 1 {
		t.Fatalf("expected one commitments patch during direct patch, got %d", gotPatchesAfterPatch)
	}

	updatePayload := assertEnvelopeOK(t, runCLIForTest(t, home, map[string]string{}, strings.NewReader(`{"patch":{"status":"done"}}`), []string{
		"--json",
		"--base-url", server.URL,
		"commitments", "propose-patch",
		"--commitment-id", "commitment_1",
	}))

	mu.Lock()
	gotGetsAfterStage := getCalls
	gotPatchesAfterStage := patchCalls
	mu.Unlock()
	if gotGetsAfterStage != 1 {
		t.Fatalf("expected one commitments get during proposal staging, got %d", gotGetsAfterStage)
	}
	if gotPatchesAfterStage != 1 {
		t.Fatalf("expected direct patch count to remain unchanged during proposal staging, got %d", gotPatchesAfterStage)
	}

	applyPayload := assertEnvelopeOK(t, runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"commitments", "apply",
		proposalIDFromEnvelope(t, updatePayload),
	}))
	if got := anyStringValue(applyPayload["command_id"]); got != "commitments.patch.apply" {
		t.Fatalf("expected commitments.patch.apply command_id, got %#v", applyPayload)
	}

	mu.Lock()
	gotPatches := patchCalls
	mu.Unlock()
	if gotPatches != 2 {
		t.Fatalf("expected two commitments patches after direct patch plus apply, got %d", gotPatches)
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
	if !strings.Contains(raw, "- message_posted: Use for direct communication between entities on a thread.") {
		t.Fatalf("expected message_posted communication guidance in explain output, got %q", raw)
	}
	if !strings.Contains(raw, "- review_completed: prefer `oar reviews create`") {
		t.Fatalf("expected preferred command guidance in explain output, got %q", raw)
	}
	if !strings.Contains(raw, "oar events explain <event-type>") {
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
	foundReviewCompleted := false
	for _, item := range items {
		entry, _ := item.(map[string]any)
		switch anyStringValue(entry["type"]) {
		case "message_posted":
			foundMessagePosted = true
			if got := anyStringValue(entry["group"]); got != "Communication" {
				t.Fatalf("expected message_posted group Communication, got %q entry=%#v", got, entry)
			}
		case "review_completed":
			foundReviewCompleted = true
			if got := anyStringValue(entry["preferred_command"]); got != "oar reviews create" {
				t.Fatalf("expected review_completed preferred command, got %q entry=%#v", got, entry)
			}
		}
	}
	if !foundMessagePosted {
		t.Fatalf("expected message_posted in JSON output, payload=%#v", payload)
	}
	if !foundReviewCompleted {
		t.Fatalf("expected review_completed in JSON output, payload=%#v", payload)
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

func TestEventsExplainMessagePostedGuidance(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"events", "explain", "message_posted"})
	if !strings.Contains(raw, "Group: Communication") {
		t.Fatalf("expected group heading in explain output, got %q", raw)
	}
	if !strings.Contains(raw, "Usage hint: Use for direct communication between entities on a thread.") {
		t.Fatalf("expected usage hint in explain output, got %q", raw)
	}
	if !strings.Contains(raw, "Use this type for messages, replies, or important non-structured information that should read like direct communication.") {
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
	if !strings.Contains(message, "known types:") || !strings.Contains(message, "review_completed") {
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
	raw := runCLIForTest(t, home, map[string]string{}, strings.NewReader(`{"event":{"type":"review_completed","thread_id":"thread_1","summary":"review done","refs":["artifact:work_order_1","artifact:receipt_1","thread:thread_1"],"provenance":{"sources":["artifact:source_1"]}}}`), []string{
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
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/events":
			mu.Lock()
			requestCount++
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"event":{"id":"event_review_completed_1"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads":
			_, _ = w.Write([]byte(`{"threads":[{"id":"thread_1"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/artifacts":
			_, _ = w.Write([]byte(`{"artifacts":[{"id":"artifact:ignored"},{"id":"work_order_1"},{"id":"receipt_1"},{"id":"review_1"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, strings.NewReader(`{"event":{"type":"review_completed","thread_id":"thread_1","summary":"review done","refs":["artifact:work_order_1","artifact:receipt_1","artifact:review_1"],"provenance":{"sources":["artifact:source_1"]}}}`), []string{
		"--json",
		"--base-url", server.URL,
		"events", "create",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	event, _ := data["event"].(map[string]any)
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

func TestEventsCreateNormalizesThreadIDAndSupportedTypedRefs(t *testing.T) {
	t.Parallel()

	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/events":
			if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"event":{"id":"event_created_1"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads":
			_, _ = w.Write([]byte(`{"threads":[{"id":"thread_1234567890"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/artifacts":
			_, _ = w.Write([]byte(`{"artifacts":[{"id":"artifact_1234567890"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, strings.NewReader(`{"event":{"type":"message_posted","summary":"hello","thread_id":"thread_12345","refs":["thread:thread_12345","artifact:artifact_123","event:event_short"],"provenance":{"sources":["artifact:source_1"]}}}`), []string{
		"--json",
		"--base-url", server.URL,
		"events", "create",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	event, _ := data["event"].(map[string]any)
	if got := anyStringValue(event["id"]); got != "event_created_1" {
		t.Fatalf("unexpected event id payload=%#v", payload)
	}

	requestEvent := asMap(captured["event"])
	if got := anyStringValue(requestEvent["thread_id"]); got != "thread_1234567890" {
		t.Fatalf("expected canonical thread_id, got %#v", captured)
	}
	refs := stringList(requestEvent["refs"])
	if len(refs) != 3 {
		t.Fatalf("expected refs to be preserved, got %#v", captured)
	}
	if refs[0] != "thread:thread_1234567890" || refs[1] != "artifact:artifact_1234567890" || refs[2] != "event:event_short" {
		t.Fatalf("expected supported typed refs canonicalized and unsupported refs preserved, got %#v", refs)
	}
}

func TestNormalizeMutationBodyIDsSkipsNestedStructuredDocContent(t *testing.T) {
	t.Parallel()

	app := &App{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/artifacts":
			_, _ = w.Write([]byte(`{"artifacts":[{"id":"artifact_1234567890"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	body := map[string]any{
		"document":     map[string]any{"id": "doc_1"},
		"content_type": "structured",
		"refs":         []any{"artifact:artifact_123"},
		"content": map[string]any{
			"thread_id": "9a61af8e-d2c",
			"nested": map[string]any{
				"refs": []any{"thread:9a61af8e-d2c"},
			},
		},
	}

	normalizedAny, err := app.normalizeMutationBodyIDs(context.Background(), config.Resolved{BaseURL: server.URL}, "docs.update", body)
	if err != nil {
		t.Fatalf("normalize docs.update body: %v", err)
	}
	normalized, _ := normalizedAny.(map[string]any)
	refs := asSlice(normalized["refs"])
	if len(refs) != 1 || anyStringValue(refs[0]) != "artifact:artifact_1234567890" {
		t.Fatalf("expected top-level docs refs to be normalized, got %#v", normalized)
	}
	content := asMap(normalized["content"])
	if got := anyStringValue(content["thread_id"]); got != "9a61af8e-d2c" {
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
			"type":      "actor_statement",
			"thread_id": "thread_1",
			"refs":      []any{"CuStOmType:ABC123"},
		},
	}

	normalizedAny, err := app.normalizeMutationBodyIDs(context.Background(), config.Resolved{}, "events.create", body)
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

func TestEventsCreateMissingThreadIDFailsLocally(t *testing.T) {
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
	raw := runCLIForTest(t, home, map[string]string{}, strings.NewReader(`{"event":{"type":"message_posted","summary":"hello","refs":["thread:thread_1"],"provenance":{"sources":["artifact:source_1"]}}}`), []string{
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
	if !strings.Contains(message, "event.thread_id is required for event.type=\"message_posted\"") {
		t.Fatalf("expected thread requirement validation message, got %q payload=%#v", message, payload)
	}

	mu.Lock()
	gotRequests := requestCount
	mu.Unlock()
	if gotRequests != 0 {
		t.Fatalf("expected no HTTP request for invalid local payload, got %d", gotRequests)
	}
}

func TestCommitmentsGetHumanOutputPrefersLinks(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/commitments/commitment_1" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"commitment":{
			"id":"commitment_1",
			"title":"Publish launch brief",
			"status":"open",
			"thread_id":"thread_1",
			"owner":"actor_1",
			"due_at":"2026-03-07T12:00:00Z",
			"links":["artifact:artifact_launch_brief","url:https://example.com/launch"],
			"refs":["artifact:legacy_ref_should_not_render"]
		}}`))
	}))
	defer server.Close()

	home := t.TempDir()
	out := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"commitments", "get",
		"--commitment-id", "commitment_1",
	})
	if !strings.Contains(out, "links:") || !strings.Contains(out, "artifact:artifact_launch_brief") {
		t.Fatalf("expected human output to render commitment links, got:\n%s", out)
	}
	if strings.Contains(out, "\nrefs:") {
		t.Fatalf("expected human output to avoid non-canonical refs label, got:\n%s", out)
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
				{"id":"event_actor_1","type":"actor_statement","summary":"support recommends Friday launch"},
				{"id":"event_need_1","type":"decision_needed","summary":"pick launch day"},
				{"id":"event_done_1","type":"decision_made","summary":"launch Friday"}
			],
			"key_artifacts":[
				{"ref":"artifact:brief_1","artifact":{"id":"artifact_1","kind":"gtm-brief","summary":"Pilot rescue brief"}}
			],
			"open_commitments":[
				{"id":"commitment_1","status":"open","title":"Publish launch brief"}
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
	recommendationCount, _ := collaboration["recommendation_count"].(float64)
	if got := int(recommendationCount); got != 1 {
		t.Fatalf("expected recommendation_count=1, got %#v", collaboration)
	}
	decisionRequestCount, _ := collaboration["decision_request_count"].(float64)
	if got := int(decisionRequestCount); got != 1 {
		t.Fatalf("expected decision_request_count=1, got %#v", collaboration)
	}
	decisionCount, _ := collaboration["decision_count"].(float64)
	if got := int(decisionCount); got != 1 {
		t.Fatalf("expected decision_count=1, got %#v", collaboration)
	}

	human := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"threads", "context",
		"--thread-id", "thread_1",
	})
	if !strings.Contains(human, "recommendations (1):") || !strings.Contains(human, "decision_requests (1):") || !strings.Contains(human, "decisions (1):") {
		t.Fatalf("expected collaboration sections in human output, got:\n%s", human)
	}
}

func TestCommitmentsListResolvesShortThreadIDFilter(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads":
			_, _ = w.Write([]byte(`{"threads":[{"id":"thread_canonical_123"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/commitments":
			if got := r.URL.Query().Get("thread_id"); got != "thread_canonical_123" {
				t.Fatalf("expected canonical thread_id query, got %q", got)
			}
			_, _ = w.Write([]byte(`{"commitments":[{"id":"commitment_1","thread_id":"thread_canonical_123","title":"Do work"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"commitments", "list",
		"--thread-id", "thread_canon",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	items := asSlice(data["commitments"])
	if len(items) != 1 {
		t.Fatalf("expected one commitment after short thread id resolution, got %#v", data)
	}
}

func TestArtifactsListResolvesShortThreadIDFilter(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads":
			_, _ = w.Write([]byte(`{"threads":[{"id":"thread_canonical_123"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/artifacts":
			if got := r.URL.Query().Get("thread_id"); got != "thread_canonical_123" {
				t.Fatalf("expected canonical thread_id query, got %q", got)
			}
			_, _ = w.Write([]byte(`{"artifacts":[{"id":"artifact_1","thread_id":"thread_canonical_123","kind":"doc"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"artifacts", "list",
		"--thread-id", "thread_canon",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	items := asSlice(data["artifacts"])
	if len(items) != 1 {
		t.Fatalf("expected one artifact after short thread id resolution, got %#v", data)
	}
}

func TestBoardCommands(t *testing.T) {
	t.Parallel()

	const (
		boardID         = "board_product_launch_123456"
		cardThreadID    = "thread_card_123456"
		secondaryThread = "thread_card_654321"
		updatedAt       = "2026-03-08T00:00:00Z"
		nextUpdatedAt   = "2026-03-08T00:05:00Z"
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/boards":
			if got := r.URL.Query().Get("status"); got != "active" {
				t.Fatalf("expected status query active, got %q", got)
			}
			if got := r.URL.Query()["label"]; len(got) != 1 || got[0] != "ops" {
				t.Fatalf("expected label query [ops], got %#v", got)
			}
			if got := r.URL.Query()["owner"]; len(got) != 1 || got[0] != "actor_1" {
				t.Fatalf("expected owner query [actor_1], got %#v", got)
			}
			_, _ = w.Write([]byte(`{"boards":[{"board":{"id":"` + boardID + `","title":"Launch","status":"active"},"summary":{"card_count":1,"cards_by_column":{"backlog":1,"ready":0,"in_progress":0,"blocked":0,"review":0,"done":0},"open_commitment_count":1,"document_count":1,"latest_activity_at":"` + updatedAt + `","has_primary_document":true}}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/boards":
			body, _ := io.ReadAll(r.Body)
			if !bytes.Contains(body, []byte(`"title":"Launch"`)) {
				t.Fatalf("unexpected boards create body: %s", string(body))
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"board":{"id":"` + boardID + `","title":"Launch","status":"active","updated_at":"` + updatedAt + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/boards/"+boardID:
			_, _ = w.Write([]byte(`{"board":{"id":"` + boardID + `","title":"Launch","status":"active","updated_at":"` + updatedAt + `"}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/boards/"+boardID:
			body, _ := io.ReadAll(r.Body)
			if !bytes.Contains(body, []byte(`"if_updated_at":"`+updatedAt+`"`)) {
				t.Fatalf("unexpected boards update body: %s", string(body))
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"board":{"id":"` + boardID + `","title":"Launch Updated","status":"active","updated_at":"` + nextUpdatedAt + `"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/boards/"+boardID+"/workspace":
			_, _ = w.Write([]byte(`{"board_id":"` + boardID + `","board":{"id":"` + boardID + `","title":"Launch","status":"active","updated_at":"` + updatedAt + `"},"primary_thread":{"id":"thread_primary_1","title":"Primary"},"primary_document":{"id":"doc_primary_1","title":"Plan"},"cards":{"items":[{"card":{"board_id":"` + boardID + `","thread_id":"` + cardThreadID + `","column_key":"backlog","rank":"a"},"thread":{"id":"` + cardThreadID + `","title":"Card"},"summary":{"open_commitment_count":1,"decision_request_count":0,"decision_count":0,"recommendation_count":0,"document_count":1,"inbox_count":0,"latest_activity_at":"` + updatedAt + `","stale":false},"pinned_document":null}],"count":1},"documents":{"items":[],"count":0},"commitments":{"items":[],"count":0},"inbox":{"items":[],"count":0},"board_summary":{"card_count":1,"cards_by_column":{"backlog":1,"ready":0,"in_progress":0,"blocked":0,"review":0,"done":0},"open_commitment_count":1,"document_count":1,"latest_activity_at":"` + updatedAt + `","has_primary_document":true},"warnings":{"items":[],"count":0},"section_kinds":{"board":"canonical","cards":"canonical","documents":"derived","commitments":"derived","inbox":"derived","warnings":"derived"},"generated_at":"` + updatedAt + `"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/boards/"+boardID+"/cards":
			_, _ = w.Write([]byte(`{"board_id":"` + boardID + `","cards":[{"board_id":"` + boardID + `","thread_id":"` + cardThreadID + `","column_key":"backlog","rank":"a","pinned_document_id":null,"created_at":"` + updatedAt + `","created_by":"actor_1","updated_at":"` + updatedAt + `","updated_by":"actor_1"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/boards/"+boardID+"/cards":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode boards cards add body: %v", err)
			}
			if got := anyStringValue(payload["thread_id"]); got != cardThreadID {
				t.Fatalf("expected add thread_id %q, got %#v", cardThreadID, payload)
			}
			if got := anyStringValue(payload["request_key"]); got != "req-1" {
				t.Fatalf("expected add request_key req-1, got %#v", payload)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"board":{"id":"` + boardID + `","updated_at":"` + nextUpdatedAt + `"},"card":{"board_id":"` + boardID + `","thread_id":"` + cardThreadID + `","column_key":"backlog","rank":"a","pinned_document_id":"doc_1","created_at":"` + updatedAt + `","created_by":"actor_1","updated_at":"` + nextUpdatedAt + `","updated_by":"actor_1"}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/boards/"+boardID+"/cards/"+cardThreadID:
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode boards cards update body: %v", err)
			}
			if got := anyStringValue(payload["if_board_updated_at"]); got != updatedAt {
				t.Fatalf("expected card update concurrency token %q, got %#v", updatedAt, payload)
			}
			patch, _ := payload["patch"].(map[string]any)
			if got := anyStringValue(patch["pinned_document_id"]); got != "doc_2" {
				t.Fatalf("expected pinned_document_id doc_2, got %#v", payload)
			}
			_, _ = w.Write([]byte(`{"board":{"id":"` + boardID + `","updated_at":"` + nextUpdatedAt + `"},"card":{"board_id":"` + boardID + `","thread_id":"` + cardThreadID + `","column_key":"backlog","rank":"a","pinned_document_id":"doc_2","created_at":"` + updatedAt + `","created_by":"actor_1","updated_at":"` + nextUpdatedAt + `","updated_by":"actor_1"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/boards/"+boardID+"/cards/"+cardThreadID+"/move":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode boards cards move body: %v", err)
			}
			if got := anyStringValue(payload["column_key"]); got != "review" {
				t.Fatalf("expected move column review, got %#v", payload)
			}
			if got := anyStringValue(payload["after_thread_id"]); got != secondaryThread {
				t.Fatalf("expected move after_thread_id %q, got %#v", secondaryThread, payload)
			}
			_, _ = w.Write([]byte(`{"board":{"id":"` + boardID + `","updated_at":"` + nextUpdatedAt + `"},"card":{"board_id":"` + boardID + `","thread_id":"` + cardThreadID + `","column_key":"review","rank":"b","pinned_document_id":"doc_2","created_at":"` + updatedAt + `","created_by":"actor_1","updated_at":"` + nextUpdatedAt + `","updated_by":"actor_1"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/boards/"+boardID+"/cards/"+cardThreadID+"/remove":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode boards cards remove body: %v", err)
			}
			if got := anyStringValue(payload["if_board_updated_at"]); got != updatedAt {
				t.Fatalf("expected remove concurrency token %q, got %#v", updatedAt, payload)
			}
			_, _ = w.Write([]byte(`{"board":{"id":"` + boardID + `","updated_at":"` + nextUpdatedAt + `"},"removed_thread_id":"` + cardThreadID + `"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}

	assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "boards", "list", "--status", "active", "--label", "ops", "--owner", "actor_1"}))
	assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"board":{"title":"Launch","primary_thread_id":"thread_primary_1","status":"active"}}`), []string{"--json", "--base-url", server.URL, "boards", "create"}))

	getPayload := assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "boards", "get", "--board-id", boardID}))
	if got := anyStringValue(getPayload["command_id"]); got != "boards.get" {
		t.Fatalf("expected boards.get command_id, got %#v", getPayload)
	}

	updatePayload := assertEnvelopeOK(t, runCLIForTest(t, home, env, strings.NewReader(`{"if_updated_at":"`+updatedAt+`","patch":{"title":"Launch Updated"}}`), []string{"--json", "--base-url", server.URL, "boards", "update", "--board-id", boardID}))
	if got := anyStringValue(updatePayload["command_id"]); got != "boards.update" {
		t.Fatalf("expected boards.update command_id, got %#v", updatePayload)
	}

	workspacePayload := assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "boards", "workspace", "--board-id", boardID}))
	if got := anyStringValue(workspacePayload["command_id"]); got != "boards.workspace" {
		t.Fatalf("expected boards.workspace command_id, got %#v", workspacePayload)
	}

	cardsListPayload := assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "boards", "cards", "list", "--board-id", boardID}))
	if got := anyStringValue(cardsListPayload["command_id"]); got != "boards.cards.list" {
		t.Fatalf("expected boards.cards.list command_id, got %#v", cardsListPayload)
	}

	addPayload := assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "boards", "cards", "add", "--board-id", boardID, "--thread-id", cardThreadID, "--column", "backlog", "--request-key", "req-1", "--pinned-document-id", "doc_1"}))
	if got := anyStringValue(addPayload["command_id"]); got != "boards.cards.add" {
		t.Fatalf("expected boards.cards.add command_id, got %#v", addPayload)
	}

	updateCardPayload := assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "boards", "cards", "update", "--board-id", boardID, "--thread-id", cardThreadID, "--if-board-updated-at", updatedAt, "--pinned-document-id", "doc_2"}))
	if got := anyStringValue(updateCardPayload["command_id"]); got != "boards.cards.update" {
		t.Fatalf("expected boards.cards.update command_id, got %#v", updateCardPayload)
	}

	movePayload := assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "boards", "cards", "move", "--board-id", boardID, "--thread-id", cardThreadID, "--if-board-updated-at", updatedAt, "--column", "review", "--after", secondaryThread}))
	if got := anyStringValue(movePayload["command_id"]); got != "boards.cards.move" {
		t.Fatalf("expected boards.cards.move command_id, got %#v", movePayload)
	}

	removePayload := assertEnvelopeOK(t, runCLIForTest(t, home, env, nil, []string{"--json", "--base-url", server.URL, "boards", "cards", "remove", "--board-id", boardID, "--thread-id", cardThreadID, "--if-board-updated-at", updatedAt}))
	if got := anyStringValue(removePayload["command_id"]); got != "boards.cards.remove" {
		t.Fatalf("expected boards.cards.remove command_id, got %#v", removePayload)
	}
}

func TestBoardsListAddsNestedShortIDAndWorkspaceResolvesShortBoardID(t *testing.T) {
	t.Parallel()

	const canonicalID = "board_1234567890abcdef"
	const shortID = "board_123456"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/boards":
			_, _ = w.Write([]byte(`{"boards":[{"board":{"id":"` + canonicalID + `","title":"Ops Board","status":"active"},"summary":{"card_count":0,"cards_by_column":{"backlog":0,"ready":0,"in_progress":0,"blocked":0,"review":0,"done":0},"open_commitment_count":0,"document_count":0,"latest_activity_at":null,"has_primary_document":false}}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/boards/"+shortID+"/workspace":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"board not found"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/boards/"+canonicalID+"/workspace":
			_, _ = w.Write([]byte(`{"board_id":"` + canonicalID + `","board":{"id":"` + canonicalID + `","title":"Ops Board","status":"active"},"primary_thread":{"id":"thread_1"},"primary_document":null,"cards":{"items":[],"count":0},"documents":{"items":[],"count":0},"commitments":{"items":[],"count":0},"inbox":{"items":[],"count":0},"board_summary":{"card_count":0,"cards_by_column":{"backlog":0,"ready":0,"in_progress":0,"blocked":0,"review":0,"done":0},"open_commitment_count":0,"document_count":0,"latest_activity_at":null,"has_primary_document":false},"warnings":{"items":[],"count":0},"section_kinds":{"board":"canonical","cards":"canonical","documents":"derived","commitments":"derived","inbox":"derived","warnings":"derived"},"generated_at":"2026-03-08T00:00:00Z"}`))
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
	if got := anyStringValue(board["short_id"]); got != shortID {
		t.Fatalf("expected board short_id %q, got %#v", shortID, listPayload)
	}

	workspacePayload := assertEnvelopeOK(t, runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"boards", "workspace",
		"--board-id", shortID,
	}))
	workspaceData, _ := workspacePayload["data"].(map[string]any)
	if got := anyStringValue(workspaceData["board_id"]); got != canonicalID {
		t.Fatalf("expected canonical board_id %q, got %#v", canonicalID, workspacePayload)
	}
}

func TestBoardCardsMoveRejectsBeforeAndAfterFlags(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"boards", "cards", "move",
		"--board-id", "board_1234567890abcdef",
		"--thread-id", "thread_1234567890abcdef",
		"--if-board-updated-at", "2026-03-08T00:00:00Z",
		"--column", "review",
		"--before", "thread_a",
		"--after", "thread_b",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "invalid_request" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	if message := anyStringValue(errObj["message"]); !strings.Contains(message, "--before and --after cannot be combined") {
		t.Fatalf("expected placement flag guidance, got %q", message)
	}
}

func TestBoardCardMutationsResolveShortThreadIDsInBodies(t *testing.T) {
	t.Parallel()

	const canonicalBoardID = "board_1234567890abcdef"
	const shortBoardID = "board_123456"
	const canonicalCardThreadID = "thread_1234567890abcdef"
	const shortCardThreadID = "thread_12345"
	const canonicalAnchorThreadID = "thread_anchor_1234567890"
	const shortAnchorThreadID = "thread_ancho"
	const updatedAt = "2026-03-08T00:00:00Z"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/boards":
			_, _ = w.Write([]byte(`{"boards":[{"board":{"id":"` + canonicalBoardID + `","title":"Ops Board","status":"active"},"summary":{"card_count":0,"cards_by_column":{"backlog":0,"ready":0,"in_progress":0,"blocked":0,"review":0,"done":0},"open_commitment_count":0,"document_count":0,"latest_activity_at":null,"has_primary_document":false}}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads":
			_, _ = w.Write([]byte(`{"threads":[{"id":"` + canonicalCardThreadID + `","title":"Execution Track"},{"id":"` + canonicalAnchorThreadID + `","title":"Review Anchor"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/boards/"+canonicalBoardID+"/cards":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode add body: %v", err)
			}
			if got := anyStringValue(payload["thread_id"]); got != canonicalCardThreadID {
				t.Fatalf("expected canonical add thread_id %q, got %#v", canonicalCardThreadID, payload)
			}
			if got := anyStringValue(payload["after_thread_id"]); got != canonicalAnchorThreadID {
				t.Fatalf("expected canonical add after_thread_id %q, got %#v", canonicalAnchorThreadID, payload)
			}
			_, _ = w.Write([]byte(`{"board":{"id":"` + canonicalBoardID + `","updated_at":"` + updatedAt + `"},"card":{"board_id":"` + canonicalBoardID + `","thread_id":"` + canonicalCardThreadID + `","column_key":"ready","rank":"a","created_at":"` + updatedAt + `","created_by":"actor_1","updated_at":"` + updatedAt + `","updated_by":"actor_1"}}`))
		case r.Method == http.MethodPost && r.URL.Path == "/boards/"+canonicalBoardID+"/cards/"+canonicalCardThreadID+"/move":
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
		"boards", "cards", "add",
		"--board-id", shortBoardID,
		"--from-file", addFile,
	}))

	assertEnvelopeOK(t, runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"boards", "cards", "move",
		"--board-id", shortBoardID,
		"--thread-id", shortCardThreadID,
		"--if-board-updated-at", updatedAt,
		"--column", "review",
		"--after", shortAnchorThreadID,
	}))
}

func TestArtifactsListIncludesTombstonedQueryFlag(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/artifacts" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("include_tombstoned"); got != "true" {
			t.Fatalf("expected include_tombstoned=true, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"artifacts":[]}`))
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"artifacts", "list",
		"--include-tombstoned",
	})
	assertEnvelopeOK(t, raw)
}

func TestArtifactsTombstoneActorIDMeAliasFromProfile(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/artifacts/artifact_1/tombstone" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode artifacts tombstone body: %v body=%s", err, string(body))
		}
		if got := strings.TrimSpace(anyStringValue(payload["actor_id"])); got != "actor-profile-artifacts" {
			t.Fatalf("expected actor_id from profile, got %q body=%s", got, string(body))
		}
		if got := strings.TrimSpace(anyStringValue(payload["reason"])); got != "superseded" {
			t.Fatalf("expected reason superseded, got %q body=%s", got, string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"artifact":{"id":"artifact_1","tombstoned_at":"2026-03-10T10:00:00Z"}}`))
	}))
	defer server.Close()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-artifacts", `{"agent":"agent-artifacts","actor_id":"actor-profile-artifacts","access_token":"token-artifacts","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"--agent", "agent-artifacts",
		"artifacts", "tombstone",
		"--artifact-id", "artifact_1",
		"--actor-id", "me",
		"--reason", "superseded",
	})
	assertEnvelopeOK(t, raw)
}

func TestDocsTombstoneActorIDMeAliasFromProfile(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/docs/doc_1/tombstone" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("decode docs tombstone body: %v body=%s", err, string(body))
		}
		if got := strings.TrimSpace(anyStringValue(payload["actor_id"])); got != "actor-profile-docs-tombstone" {
			t.Fatalf("expected actor_id from profile, got %q body=%s", got, string(body))
		}
		if got := strings.TrimSpace(anyStringValue(payload["reason"])); got != "replaced" {
			t.Fatalf("expected reason replaced, got %q body=%s", got, string(body))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"document":{"id":"doc_1","tombstoned_at":"2026-03-10T10:00:00Z"},"revision":{"revision_id":"rev_1"}}`))
	}))
	defer server.Close()

	home := t.TempDir()
	writeAgentProfile(t, home, "agent-docs-tombstone", `{"agent":"agent-docs-tombstone","actor_id":"actor-profile-docs-tombstone","access_token":"token-docs","access_token_expires_at":"2099-01-01T00:00:00Z"}`)

	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"--agent", "agent-docs-tombstone",
		"docs", "tombstone",
		"--document-id", "doc_1",
		"--actor-id", "me",
		"--reason", "replaced",
	})
	assertEnvelopeOK(t, raw)
}

func TestInboxListResolvesShortThreadIDFilter(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads":
			_, _ = w.Write([]byte(`{"threads":[{"id":"thread_canonical_123"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/inbox":
			_, _ = w.Write([]byte(`{"items":[{"id":"inbox:1","thread_id":"thread_canonical_123","title":"Need decision","category":"decision_needed"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"inbox", "list",
		"--thread-id", "thread_canon",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	items := asSlice(data["items"])
	if len(items) != 1 {
		t.Fatalf("expected one inbox item after short thread id resolution, got %#v", data)
	}
	if got := anyStringValue(data["thread_id"]); got != "thread_canonical_123" {
		t.Fatalf("expected canonical filtered thread_id, got %#v", data)
	}
}

func TestThreadsContextRejectsMixedSelectionModesWithActionableGuidance(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"threads", "context",
		"--thread-id", "thread_1",
		"--status", "active",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "invalid_request" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	message := anyStringValue(errObj["message"])
	if !strings.Contains(message, "--thread-id cannot be combined with discovery filters") || !strings.Contains(message, "oar threads workspace --thread-id <thread-id>") || !strings.Contains(message, "oar threads context --thread-id <thread-id>") || !strings.Contains(message, "oar threads context --status active") {
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
				"thread":{"id":"thread_1","title":"Pilot Rescue","status":"active"},
				"recent_events":[
					{"id":"event_actor_1","type":"actor_statement","summary":"support recommends Friday launch","created_at":"2026-03-06T10:00:00Z"},
					{"id":"event_need_1","type":"decision_needed","summary":"pick launch day","created_at":"2026-03-06T10:01:00Z"}
				],
				"key_artifacts":[{"id":"artifact_1","kind":"brief"}],
				"open_commitments":[{"id":"commitment_1","status":"open","title":"Publish brief"}]
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_2/context":
			_, _ = w.Write([]byte(`{
				"thread":{"id":"thread_2","title":"Delivery Readiness","status":"active"},
				"recent_events":[
					{"id":"event_actor_2","type":"actor_statement","summary":"delivery recommends staged rollout","created_at":"2026-03-06T10:05:00Z"},
					{"id":"event_done_2","type":"decision_made","summary":"ship Friday scope","created_at":"2026-03-06T10:10:00Z"}
				],
				"key_artifacts":[{"id":"artifact_2","kind":"plan"}],
				"open_commitments":[{"id":"commitment_2","status":"open","title":"Prep release runbook"}]
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
	recommendationCount, _ := collaboration["recommendation_count"].(float64)
	if got := int(recommendationCount); got != 2 {
		t.Fatalf("expected recommendation_count=2, got %#v", collaboration)
	}
	decisionRequestCount, _ := collaboration["decision_request_count"].(float64)
	if got := int(decisionRequestCount); got != 1 {
		t.Fatalf("expected decision_request_count=1, got %#v", collaboration)
	}
	decisionCount, _ := collaboration["decision_count"].(float64)
	if got := int(decisionCount); got != 1 {
		t.Fatalf("expected decision_count=1, got %#v", collaboration)
	}

	human := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"threads", "context",
		"--thread-id", "thread_1",
		"--thread-id", "thread_2",
	})
	if !strings.Contains(human, "Thread contexts (2):") || !strings.Contains(human, "recommendations (2):") {
		t.Fatalf("expected aggregate context sections in human output, got:\n%s", human)
	}
}

func TestThreadsContextDiscoversByFiltersAndType(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	contextRequests := make([]string, 0, 2)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads":
			if got := r.URL.Query().Get("status"); got != "active" {
				t.Fatalf("expected status=active, got %q", got)
			}
			if got := r.URL.Query().Get("tag"); got != "pilot" {
				t.Fatalf("expected tag=pilot, got %q", got)
			}
			_, _ = w.Write([]byte(`{"threads":[
				{"id":"thread_init_1","type":"initiative","status":"active"},
				{"id":"thread_case_1","type":"case","status":"active"},
				{"id":"thread_init_2","type":"initiative","status":"active"}
			]}`))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/context"):
			mu.Lock()
			contextRequests = append(contextRequests, r.URL.Path)
			mu.Unlock()
			switch r.URL.Path {
			case "/threads/thread_init_1/context":
				_, _ = w.Write([]byte(`{"thread":{"id":"thread_init_1","type":"initiative"},"recent_events":[],"key_artifacts":[],"open_commitments":[]}`))
			case "/threads/thread_init_2/context":
				_, _ = w.Write([]byte(`{"thread":{"id":"thread_init_2","type":"initiative"},"recent_events":[],"key_artifacts":[],"open_commitments":[]}`))
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
		"--status", "active",
		"--tag", "pilot",
		"--type", "initiative",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	threadIDs := stringList(data["thread_ids"])
	if len(threadIDs) != 2 || threadIDs[0] != "thread_init_1" || threadIDs[1] != "thread_init_2" {
		t.Fatalf("expected initiative thread_ids [thread_init_1 thread_init_2], got %#v", data)
	}

	mu.Lock()
	gotRequests := append([]string(nil), contextRequests...)
	mu.Unlock()
	if len(gotRequests) != 2 {
		t.Fatalf("expected exactly 2 context requests, got %v", gotRequests)
	}
}

func TestThreadsContextSupportsFullIDForEventSections(t *testing.T) {
	t.Parallel()

	const eventID = "event_1234567890abcdef"
	const artifactID = "artifact_1234567890abcdef"
	const commitmentID = "commitment_1234567890abcdef"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/threads/thread_1/context" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"thread":{"id":"thread_1","title":"Pilot Rescue"},
			"recent_events":[
				{"id":"` + eventID + `","type":"actor_statement","summary":"ship Friday rescue scope"}
			],
			"key_artifacts":[{"id":"` + artifactID + `","kind":"brief","summary":"Launch brief"}],
			"open_commitments":[{"id":"` + commitmentID + `","status":"open","title":"Publish launch brief"}]
		}`))
	}))
	defer server.Close()

	home := t.TempDir()
	humanFull := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"threads", "context",
		"--thread-id", "thread_1",
		"--full-id",
	})
	if !strings.Contains(humanFull, eventID) {
		t.Fatalf("expected full event id in output, got:\n%s", humanFull)
	}
	if !strings.Contains(humanFull, artifactID) {
		t.Fatalf("expected full artifact id in output, got:\n%s", humanFull)
	}
	if !strings.Contains(humanFull, commitmentID) {
		t.Fatalf("expected full commitment id in output, got:\n%s", humanFull)
	}

	humanShort := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"threads", "context",
		"--thread-id", "thread_1",
	})
	if strings.Contains(humanShort, eventID) {
		t.Fatalf("expected short-id rendering without --full-id, got:\n%s", humanShort)
	}
	if !strings.Contains(humanShort, eventID[:12]) {
		t.Fatalf("expected short event id in default output, got:\n%s", humanShort)
	}
	if strings.Contains(humanShort, artifactID) || !strings.Contains(humanShort, artifactID[:12]) {
		t.Fatalf("expected short artifact id in default output, got:\n%s", humanShort)
	}
	if strings.Contains(humanShort, commitmentID) || !strings.Contains(humanShort, commitmentID[:12]) {
		t.Fatalf("expected short commitment id in default output, got:\n%s", humanShort)
	}
}

func TestThreadsInspectBuildsCoordinationView(t *testing.T) {
	t.Parallel()

	const eventID = "event_1234567890abcdef"
	const inboxID = "inbox:decision_needed:thread_1:none:event_1234567890abcdef"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_1/context":
			_, _ = w.Write([]byte(`{
				"thread":{"id":"thread_1","title":"Pilot Rescue","status":"active","type":"initiative"},
				"recent_events":[
					{"id":"` + eventID + `","thread_id":"thread_1","type":"actor_statement","summary":"ship Friday rescue scope"},
					{"id":"event_need_1","thread_id":"thread_1","type":"decision_needed","summary":"approve launch date"}
				],
				"key_artifacts":[{"id":"artifact_1","kind":"brief"}],
				"open_commitments":[{"id":"commitment_1","status":"open","title":"Publish brief"}]
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/inbox":
			_, _ = w.Write([]byte(`{"items":[
				{"id":"` + inboxID + `","thread_id":"thread_1","type":"decision_needed","summary":"launch date still needs acknowledgement"},
				{"id":"inbox:decision_needed:thread_2:none:event_other","thread_id":"thread_2","type":"decision_needed","summary":"other thread"}
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
	recommendationCount, _ := collaboration["recommendation_count"].(float64)
	if got := int(recommendationCount); got != 1 {
		t.Fatalf("expected recommendation_count=1, got %#v", collaboration)
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

	humanFull := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"threads", "inspect",
		"--thread-id", "thread_1",
		"--full-id",
	})
	if !strings.Contains(humanFull, eventID) {
		t.Fatalf("expected full event id in inspect output, got:\n%s", humanFull)
	}
	if !strings.Contains(humanFull, "inbox_items (1):") {
		t.Fatalf("expected inbox section in inspect output, got:\n%s", humanFull)
	}
}

func TestThreadsInspectDiscoveryRequiresSingleThread(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"threads":[
				{"id":"thread_init_1","type":"initiative","status":"active"},
				{"id":"thread_init_2","type":"initiative","status":"active"}
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
		"--status", "active",
		"--type", "initiative",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "invalid_request" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	if !strings.Contains(anyStringValue(errObj["message"]), "exactly one thread") || !strings.Contains(anyStringValue(errObj["message"]), "oar threads context") {
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
		"--status", "active",
	})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "invalid_request" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	message := anyStringValue(errObj["message"])
	if !strings.Contains(message, "--thread-id cannot be combined with discovery filters") || !strings.Contains(message, "oar threads inspect --thread-id <thread-id>") || !strings.Contains(message, "oar threads context --status active") {
		t.Fatalf("expected shared-selection validation message, got %#v", payload)
	}
}

func TestThreadsRecommendationsBuildsFocusedReview(t *testing.T) {
	t.Parallel()

	const recommendationID = "event_rec_1234567890abcdef"
	const decisionNeededID = "event_need_1"
	const decisionMadeID = "event_done_1"
	const inboxID = "inbox:decision_needed:thread_1:none:event_need_1"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_1/context":
			_, _ = w.Write([]byte(`{
				"thread":{"id":"thread_1","title":"Pilot Rescue","status":"active","type":"initiative"},
				"recent_events":[
					{"id":"` + recommendationID + `","thread_id":"thread_1","type":"actor_statement","actor_id":"agent-pm","created_at":"2026-03-07T12:00:00Z","summary":"Ship Friday rescue scope","provenance":{"sources":["seed:pilot-rescue"]}},
					{"id":"` + decisionNeededID + `","thread_id":"thread_1","type":"decision_needed","actor_id":"agent-lead","created_at":"2026-03-07T12:02:00Z","summary":"Need approval on launch date"},
					{"id":"` + decisionMadeID + `","thread_id":"thread_1","type":"decision_made","actor_id":"agent-pm","created_at":"2026-03-07T12:05:00Z","summary":"Approved Friday launch"}
				],
				"key_artifacts":[],
				"open_commitments":[]
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/inbox":
			_, _ = w.Write([]byte(`{"items":[
				{"id":"` + inboxID + `","thread_id":"thread_1","type":"decision_needed","summary":"launch date still needs acknowledgement"},
				{"id":"inbox:decision_needed:thread_2:none:event_other","thread_id":"thread_2","type":"decision_needed","summary":"other thread"}
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
		"threads", "recommendations",
		"--thread-id", "thread_1",
	})
	payload := assertEnvelopeOK(t, raw)
	if got := anyStringValue(payload["command"]); got != "threads recommendations" {
		t.Fatalf("expected threads recommendations command, got %#v", payload)
	}
	if got := anyStringValue(payload["command_id"]); got != "threads.recommendations" {
		t.Fatalf("expected threads.recommendations command_id, got %#v", payload)
	}

	data, _ := payload["data"].(map[string]any)
	recommendations, _ := data["recommendations"].(map[string]any)
	recommendationCount, _ := recommendations["count"].(float64)
	if got := int(recommendationCount); got != 1 {
		t.Fatalf("expected recommendation count=1, got %#v", data)
	}
	recItems, _ := recommendations["items"].([]any)
	if len(recItems) != 1 {
		t.Fatalf("expected one recommendation item, got %#v", recommendations)
	}
	rec, _ := recItems[0].(map[string]any)
	if got := anyStringValue(rec["actor_id"]); got != "agent-pm" {
		t.Fatalf("expected actor_id agent-pm, got %#v", rec)
	}
	if got := anyStringValue(rec["created_at"]); got != "2026-03-07T12:00:00Z" {
		t.Fatalf("expected created_at to be preserved, got %#v", rec)
	}
	sources := stringList(rec["provenance_sources"])
	if len(sources) != 1 || sources[0] != "seed:pilot-rescue" {
		t.Fatalf("expected provenance_sources, got %#v", rec)
	}

	pending, _ := data["pending_decisions"].(map[string]any)
	pendingCount, _ := pending["count"].(float64)
	if got := int(pendingCount); got != 1 {
		t.Fatalf("expected pending decision count=1, got %#v", pending)
	}
	totalReviewItems, _ := data["total_review_items"].(float64)
	if got := int(totalReviewItems); got != 4 {
		t.Fatalf("expected total_review_items=4 to include pending decisions, got %#v", data)
	}
	followUp, _ := data["follow_up"].(map[string]any)
	examples := stringList(followUp["events_get_examples"])
	if len(examples) == 0 || !strings.Contains(examples[0], "oar events get --event-id") {
		t.Fatalf("expected events_get_examples follow-up commands, got %#v", followUp)
	}

	human := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"threads", "recommendations",
		"--thread-id", "thread_1",
		"--full-id",
	})
	if !strings.Contains(human, "actor=agent-pm") || !strings.Contains(human, "at=2026-03-07T12:00:00Z") {
		t.Fatalf("expected provenance fields in human output, got:\n%s", human)
	}
	if !strings.Contains(human, "follow_up:") || !strings.Contains(human, "events_get_template: oar events get --event-id <event-id> --json") {
		t.Fatalf("expected follow-up guidance in human output, got:\n%s", human)
	}
}

func TestThreadsRecommendationsIncludesRelatedThreadReview(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_main/workspace":
			_, _ = w.Write([]byte(`{
				"thread_id":"thread_main",
				"thread":{"id":"thread_main","title":"Main Pilot Rescue","status":"active","type":"initiative"},
				"context":{
					"recent_events":[
						{"id":"event_main_1","thread_id":"thread_main","type":"actor_statement","summary":"Main recommendation","refs":["thread:thread_related"]}
					],
					"key_artifacts":[],
					"open_commitments":[],
					"documents":[]
				},
				"collaboration":{
					"recommendations":[
						{"id":"event_main_1","thread_id":"thread_main","type":"actor_statement","summary":"Main recommendation","refs":["thread:thread_related"]}
					],
					"decision_requests":[],
					"decisions":[],
					"key_artifacts":[],
					"open_commitments":[],
					"recommendation_count":1,
					"decision_request_count":0,
					"decision_count":0,
					"artifact_count":0,
					"open_commitment_count":0
				},
				"inbox":{"thread_id":"thread_main","items":[],"count":0},
				"pending_decisions":{"thread_id":"thread_main","items":[],"count":0},
				"related_threads":{
					"count":1,
					"items":[
						{"thread":{"id":"thread_related","title":"Related Feedback Thread","status":"active","type":"case"},"match_reason":"thread_ref"}
					]
				},
				"related_recommendations":{
					"count":1,
					"items":[
						{
							"thread":{"id":"thread_related","title":"Related Feedback Thread","status":"active","type":"case"},
							"event":{
								"id":"event_related_1",
								"thread_id":"thread_related",
								"type":"actor_statement",
								"summary":"Related recommendation",
								"payload":{"recommendation":"Document the staged artifact follow-up"}
							}
						}
					]
				},
				"related_decision_requests":{"count":0,"items":[]},
				"related_decisions":{"count":0,"items":[]},
				"total_review_items":2,
				"follow_up":{"workspace_refresh_command":"oar threads workspace --thread-id thread_main --include-artifact-content --full-id --json"},
				"section_kinds":{
					"thread":"canonical",
					"context":"canonical",
					"collaboration":"derived",
					"inbox":"derived",
					"pending_decisions":"derived",
					"related_threads":"derived",
					"related_recommendations":"derived",
					"related_decision_requests":"derived",
					"related_decisions":"derived",
					"follow_up":"convenience"
				},
				"context_source":"threads.workspace",
				"inbox_source":"threads.workspace",
				"related_event_content_enabled":true,
				"related_event_content_count":1
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_main/context":
			_, _ = w.Write([]byte(`{
				"thread":{"id":"thread_main","title":"Main Pilot Rescue","status":"active","type":"initiative"},
					"recent_events":[
						{"id":"event_main_1","thread_id":"thread_main","type":"actor_statement","actor_id":"agent-main","created_at":"2026-03-07T12:00:00Z","summary":"Main recommendation","refs":["thread:thread_related"]}
					],
					"key_artifacts":[],
					"open_commitments":[
						{"id":"commit_main_1","thread_id":"thread_main","title":"Coordinate related work","links":["thread:thread_main","thread:thread_related"]}
					]
				}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_related/context":
			_, _ = w.Write([]byte(`{
				"thread":{"id":"thread_related","title":"Related Feedback Thread","status":"active","type":"case"},
				"recent_events":[
					{"id":"event_related_1","thread_id":"thread_related","type":"actor_statement","actor_id":"agent-related","created_at":"2026-03-07T12:05:00Z","summary":"Related recommendation"}
				],
				"key_artifacts":[],
				"open_commitments":[]
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/inbox":
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"threads", "recommendations",
		"--thread-id", "thread_main",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)

	relatedThreads, _ := data["related_threads"].(map[string]any)
	relatedThreadCount, _ := relatedThreads["count"].(float64)
	if got := int(relatedThreadCount); got != 1 {
		t.Fatalf("expected one related thread, got %#v", data)
	}
	relatedRecommendations, _ := data["related_recommendations"].(map[string]any)
	relatedRecommendationCount, _ := relatedRecommendations["count"].(float64)
	if got := int(relatedRecommendationCount); got != 1 {
		t.Fatalf("expected one related recommendation, got %#v", data)
	}
	relatedItems, _ := relatedRecommendations["items"].([]any)
	if len(relatedItems) != 1 {
		t.Fatalf("expected one related recommendation item, got %#v", relatedRecommendations)
	}
	relatedEvent, _ := relatedItems[0].(map[string]any)
	if got := anyStringValue(relatedEvent["source_thread_id"]); got != "thread_related" {
		t.Fatalf("expected source_thread_id to annotate related thread, got %#v", relatedEvent)
	}
	if got := anyStringValue(relatedEvent["source_thread_title"]); got != "Related Feedback Thread" {
		t.Fatalf("expected source_thread_title annotation, got %#v", relatedEvent)
	}
	totalReviewItems, _ := data["total_review_items"].(float64)
	if got := int(totalReviewItems); got != 2 {
		t.Fatalf("expected total_review_items to include related recommendation, got %#v", data)
	}
}

func TestThreadsRecommendationsSkipsMissingRelatedThreadsWithWarnings(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_main/context":
			_, _ = w.Write([]byte(`{
				"thread":{"id":"thread_main","title":"Main Pilot Rescue","status":"active","type":"initiative"},
				"recent_events":[
					{"id":"event_main_1","thread_id":"thread_main","type":"actor_statement","summary":"Main recommendation","refs":["thread:thread_missing","thread:thread_related"]}
				],
				"key_artifacts":[],
				"open_commitments":[]
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_related/context":
			_, _ = w.Write([]byte(`{
				"thread":{"id":"thread_related","title":"Related Feedback Thread","status":"active","type":"case"},
				"recent_events":[
					{"id":"event_related_1","thread_id":"thread_related","type":"actor_statement","summary":"Related recommendation"}
				],
				"key_artifacts":[],
				"open_commitments":[]
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_missing/context":
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/inbox":
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"threads", "recommendations",
		"--thread-id", "thread_main",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)

	relatedRecommendations, _ := data["related_recommendations"].(map[string]any)
	relatedRecommendationCount, _ := relatedRecommendations["count"].(float64)
	if got := int(relatedRecommendationCount); got != 1 {
		t.Fatalf("expected one successful related recommendation despite missing thread, got %#v", data)
	}

	warnings, _ := data["warnings"].(map[string]any)
	warningCount, _ := warnings["count"].(float64)
	if got := int(warningCount); got != 1 {
		t.Fatalf("expected one warning for skipped related thread, got %#v", data)
	}
	warningItems, _ := warnings["items"].([]any)
	if len(warningItems) != 1 {
		t.Fatalf("expected warning item, got %#v", warnings)
	}
	warning, _ := warningItems[0].(map[string]any)
	if got := anyStringValue(warning["thread_id"]); got != "thread_missing" {
		t.Fatalf("expected skipped thread id in warning, got %#v", warning)
	}
	if !strings.Contains(anyStringValue(warning["message"]), "skipped related thread thread_missing") {
		t.Fatalf("expected skipped related thread warning, got %#v", warning)
	}

	human := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"threads", "recommendations",
		"--thread-id", "thread_main",
	})
	if !strings.Contains(human, "warnings:") || !strings.Contains(human, "thread_missing") {
		t.Fatalf("expected warning section in human output, got:\n%s", human)
	}
}

func TestThreadsRecommendationsCanHydrateRelatedEventContent(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_main/context":
			_, _ = w.Write([]byte(`{
				"thread":{"id":"thread_main","title":"Main Pilot Rescue","status":"active","type":"initiative"},
				"recent_events":[
					{"id":"event_main_1","thread_id":"thread_main","type":"actor_statement","summary":"Main recommendation","refs":["thread:thread_related"]}
				],
				"key_artifacts":[],
				"open_commitments":[]
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_related/context":
			_, _ = w.Write([]byte(`{
				"thread":{"id":"thread_related","title":"Related Feedback Thread","status":"active","type":"case"},
				"recent_events":[
					{"id":"event_related_1","thread_id":"thread_related","type":"actor_statement","actor_id":"agent-related","created_at":"2026-03-07T12:05:00Z","summary":"Related recommendation"}
				],
				"key_artifacts":[],
				"open_commitments":[]
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/events/event_related_1":
			_, _ = w.Write([]byte(`{
				"event":{
					"id":"event_related_1",
					"type":"actor_statement",
					"summary":"Related recommendation",
					"payload":{"recommendation":"Ship the digest owner field first","evidence":["customer quote"]}
				}
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/inbox":
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"threads", "recommendations",
		"--thread-id", "thread_main",
		"--include-related-event-content",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	if !asBool(data["related_event_content_enabled"]) {
		t.Fatalf("expected related_event_content_enabled=true, got %#v", data)
	}
	if got := intValue(data["related_event_content_count"]); got != 1 {
		t.Fatalf("expected one hydrated related event, got %#v", data)
	}
	relatedRecommendations, _ := data["related_recommendations"].(map[string]any)
	relatedItems, _ := relatedRecommendations["items"].([]any)
	if len(relatedItems) != 1 {
		t.Fatalf("expected one related recommendation item, got %#v", relatedRecommendations)
	}
	relatedEvent, _ := relatedItems[0].(map[string]any)
	fullEvent, _ := relatedEvent["event"].(map[string]any)
	payloadMap, _ := fullEvent["payload"].(map[string]any)
	if got := anyStringValue(payloadMap["recommendation"]); got != "Ship the digest owner field first" {
		t.Fatalf("expected hydrated related event payload, got %#v", relatedEvent)
	}
}

func TestThreadsWorkspaceCanHydrateRelatedEventContent(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_main/workspace":
			_, _ = w.Write([]byte(`{
				"thread_id":"thread_main",
				"thread":{"id":"thread_main","title":"Main Pilot Rescue","status":"active","type":"initiative"},
				"context":{
					"recent_events":[
						{"id":"event_main_1","thread_id":"thread_main","type":"actor_statement","summary":"Main recommendation","refs":["thread:thread_related"]}
					],
					"key_artifacts":[],
					"open_commitments":[],
					"documents":[]
				},
				"collaboration":{
					"recommendations":[
						{"id":"event_main_1","thread_id":"thread_main","type":"actor_statement","summary":"Main recommendation","refs":["thread:thread_related"]}
					],
					"decision_requests":[],
					"decisions":[],
					"key_artifacts":[],
					"open_commitments":[],
					"recommendation_count":1,
					"decision_request_count":0,
					"decision_count":0,
					"artifact_count":0,
					"open_commitment_count":0
				},
				"inbox":{"thread_id":"thread_main","items":[],"count":0},
				"pending_decisions":{"thread_id":"thread_main","items":[],"count":0},
				"related_threads":{
					"count":1,
					"items":[
						{"thread":{"id":"thread_related","title":"Related Feedback Thread","status":"active","type":"case"},"match_reason":"thread_ref"}
					]
				},
				"related_recommendations":{
					"count":1,
					"items":[
						{
							"thread":{"id":"thread_related","title":"Related Feedback Thread","status":"active","type":"case"},
							"event":{
								"id":"event_related_1",
								"thread_id":"thread_related",
								"type":"actor_statement",
								"summary":"Related recommendation",
								"payload":{"recommendation":"Document the staged artifact follow-up"}
							}
						}
					]
				},
				"related_decision_requests":{"count":0,"items":[]},
				"related_decisions":{"count":0,"items":[]},
				"total_review_items":2,
				"follow_up":{"workspace_refresh_command":"oar threads workspace --thread-id thread_main --include-artifact-content --full-id --json"},
				"section_kinds":{
					"thread":"canonical",
					"context":"canonical",
					"collaboration":"derived",
					"inbox":"derived",
					"pending_decisions":"derived",
					"related_threads":"derived",
					"related_recommendations":"derived",
					"related_decision_requests":"derived",
					"related_decisions":"derived",
					"follow_up":"convenience"
				},
				"context_source":"threads.workspace",
				"inbox_source":"threads.workspace",
				"related_event_content_enabled":true,
				"related_event_content_count":1
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_main/context":
			_, _ = w.Write([]byte(`{
				"thread":{"id":"thread_main","title":"Main Pilot Rescue","status":"active","type":"initiative"},
				"recent_events":[
					{"id":"event_main_1","thread_id":"thread_main","type":"actor_statement","summary":"Main recommendation","refs":["thread:thread_related"]}
				],
				"key_artifacts":[],
				"open_commitments":[]
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_related/context":
			_, _ = w.Write([]byte(`{
				"thread":{"id":"thread_related","title":"Related Feedback Thread","status":"active","type":"case"},
				"recent_events":[
					{"id":"event_related_1","thread_id":"thread_related","type":"actor_statement","summary":"Related recommendation"}
				],
				"key_artifacts":[],
				"open_commitments":[]
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/events/event_related_1":
			_, _ = w.Write([]byte(`{
				"event":{
					"id":"event_related_1",
					"type":"actor_statement",
					"summary":"Related recommendation",
					"payload":{"recommendation":"Document the staged artifact follow-up"}
				}
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/inbox":
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"threads", "workspace",
		"--thread-id", "thread_main",
		"--include-related-event-content",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	if !asBool(data["related_event_content_enabled"]) {
		t.Fatalf("expected related_event_content_enabled=true, got %#v", data)
	}
	if got := intValue(data["related_event_content_count"]); got != 1 {
		t.Fatalf("expected one hydrated related event, got %#v", data)
	}
	relatedRecommendations, _ := data["related_recommendations"].(map[string]any)
	relatedItems, _ := relatedRecommendations["items"].([]any)
	relatedEvent, _ := relatedItems[0].(map[string]any)
	fullEvent, _ := relatedEvent["event"].(map[string]any)
	payloadMap, _ := fullEvent["payload"].(map[string]any)
	if got := anyStringValue(payloadMap["recommendation"]); got != "Document the staged artifact follow-up" {
		t.Fatalf("expected hydrated related event payload in workspace output, got %#v", relatedEvent)
	}
}

func TestThreadsRecommendationsWarnsWhenRelatedEventHydrationFails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_main/context":
			_, _ = w.Write([]byte(`{
				"thread":{"id":"thread_main","title":"Main Pilot Rescue","status":"active","type":"initiative"},
				"recent_events":[
					{"id":"event_main_1","thread_id":"thread_main","type":"actor_statement","summary":"Main recommendation","refs":["thread:thread_related"]}
				],
				"key_artifacts":[],
				"open_commitments":[]
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_related/context":
			_, _ = w.Write([]byte(`{
				"thread":{"id":"thread_related","title":"Related Feedback Thread","status":"active","type":"case"},
				"recent_events":[
					{"id":"event_related_1","thread_id":"thread_related","type":"actor_statement","summary":"Related recommendation"}
				],
				"key_artifacts":[],
				"open_commitments":[]
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/events/event_related_1":
			http.NotFound(w, r)
		case r.Method == http.MethodGet && r.URL.Path == "/inbox":
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"threads", "recommendations",
		"--thread-id", "thread_main",
		"--include-related-event-content",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	if got := intValue(data["related_event_content_count"]); got != 0 {
		t.Fatalf("expected no hydrated related events when events get fails, got %#v", data)
	}
	warnings, _ := data["warnings"].(map[string]any)
	warningItems, _ := warnings["items"].([]any)
	if len(warningItems) != 1 {
		t.Fatalf("expected one hydration warning, got %#v", warnings)
	}
	warning, _ := warningItems[0].(map[string]any)
	if got := anyStringValue(warning["event_id"]); got != "event_related_1" {
		t.Fatalf("expected event_id in hydration warning, got %#v", warning)
	}
	if !strings.Contains(anyStringValue(warning["message"]), "kept summary-only related event event_related_1") {
		t.Fatalf("expected hydration warning message, got %#v", warning)
	}
}

func TestThreadsRecommendationsFullSummaryToggle(t *testing.T) {
	t.Parallel()

	longSummary := "Recommendation narrative starts here and stays intentionally long to trigger preview truncation while keeping the terminal marker hidden until full summary mode tail-marker-xyz"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_1/context":
			_, _ = w.Write([]byte(`{
				"thread":{"id":"thread_1","title":"Pilot Rescue","status":"active","type":"initiative"},
				"recent_events":[
					{"id":"event_rec_1","thread_id":"thread_1","type":"actor_statement","actor_id":"agent-pm","created_at":"2026-03-07T12:00:00Z","summary":"` + longSummary + `"}
				],
				"key_artifacts":[],
				"open_commitments":[]
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/inbox":
			_, _ = w.Write([]byte(`{"items":[]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	defaultOut := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"threads", "recommendations",
		"--thread-id", "thread_1",
	})
	if strings.Contains(defaultOut, "tail-marker-xyz") {
		t.Fatalf("expected default summary preview to truncate tail marker, got:\n%s", defaultOut)
	}

	fullOut := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"threads", "recommendations",
		"--thread-id", "thread_1",
		"--full-summary",
	})
	if !strings.Contains(fullOut, "tail-marker-xyz") {
		t.Fatalf("expected --full-summary output to include full summary, got:\n%s", fullOut)
	}
}

func TestThreadsRecommendationsCountsPendingDecisionsInTotalWhenRecentWindowExcludesReviewEvents(t *testing.T) {
	t.Parallel()

	const pendingInboxID = "inbox:decision_needed:thread_1:none:event_pending_1"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_1/context":
			_, _ = w.Write([]byte(`{
				"thread":{"id":"thread_1","title":"Pilot Rescue","status":"active","type":"initiative"},
				"recent_events":[
					{"id":"event_noise_1","thread_id":"thread_1","type":"status_changed","created_at":"2026-03-07T12:06:00Z","summary":"status moved to active"}
				],
				"key_artifacts":[],
				"open_commitments":[]
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/inbox":
			_, _ = w.Write([]byte(`{"items":[
				{"id":"` + pendingInboxID + `","thread_id":"thread_1","type":"decision_needed","summary":"pending approval remains"}
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
		"threads", "recommendations",
		"--thread-id", "thread_1",
		"--max-events", "1",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)

	pending, _ := data["pending_decisions"].(map[string]any)
	pendingCount, _ := pending["count"].(float64)
	if got := int(pendingCount); got != 1 {
		t.Fatalf("expected pending decision count=1, got %#v", pending)
	}
	totalReviewItems, _ := data["total_review_items"].(float64)
	if got := int(totalReviewItems); got != 1 {
		t.Fatalf("expected total_review_items=1 when only pending decisions are present, got %#v", data)
	}
}

func TestThreadsRecommendationsSelectionValidation(t *testing.T) {
	t.Parallel()

	home := t.TempDir()

	mixedSelection := assertEnvelopeError(t, runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"threads", "recommendations",
		"--thread-id", "thread_1",
		"--status", "active",
	}))
	if got := anyStringValue(mixedSelection["command"]); got != "threads recommendations" {
		t.Fatalf("expected threads recommendations error command, got %#v", mixedSelection)
	}
	if got := anyStringValue(mixedSelection["command_id"]); got != "threads.recommendations" {
		t.Fatalf("expected threads.recommendations command_id, got %#v", mixedSelection)
	}
	mixedErr, _ := mixedSelection["error"].(map[string]any)
	if mixedErr == nil || anyStringValue(mixedErr["code"]) != "invalid_request" {
		t.Fatalf("expected invalid_request for mixed selection, got %#v", mixedSelection)
	}
	message := anyStringValue(mixedErr["message"])
	if !strings.Contains(message, "--thread-id cannot be combined with discovery filters") || !strings.Contains(message, "oar threads recommendations --thread-id <thread-id>") || !strings.Contains(message, "oar threads context --status active") {
		t.Fatalf("expected mixed selection guidance, got %#v", mixedSelection)
	}

	negativeMaxEvents := assertEnvelopeError(t, runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"threads", "recommendations",
		"--thread-id", "thread_1",
		"--max-events", "-1",
	}))
	if got := anyStringValue(negativeMaxEvents["command"]); got != "threads recommendations" {
		t.Fatalf("expected threads recommendations error command, got %#v", negativeMaxEvents)
	}
	if got := anyStringValue(negativeMaxEvents["command_id"]); got != "threads.recommendations" {
		t.Fatalf("expected threads.recommendations command_id, got %#v", negativeMaxEvents)
	}
	maxErr, _ := negativeMaxEvents["error"].(map[string]any)
	if maxErr == nil || anyStringValue(maxErr["code"]) != "invalid_request" {
		t.Fatalf("expected invalid_request for max-events, got %#v", negativeMaxEvents)
	}
	if !strings.Contains(anyStringValue(maxErr["message"]), "--max-events must be >= 0") {
		t.Fatalf("expected max-events guidance, got %#v", negativeMaxEvents)
	}
}

func TestThreadsRecommendationsDiscoveryErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		threadsJSON    string
		wantMessageSub string
	}{
		{
			name:           "no matches",
			threadsJSON:    `{"threads":[]}`,
			wantMessageSub: "threads recommendations discovery returned no matching threads",
		},
		{
			name: "multiple matches",
			threadsJSON: `{"threads":[
				{"id":"thread_init_1","type":"initiative","status":"active"},
				{"id":"thread_init_2","type":"initiative","status":"active"}
			]}`,
			wantMessageSub: "threads recommendations requires exactly one thread",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/threads" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.threadsJSON))
			}))
			defer server.Close()

			home := t.TempDir()
			payload := assertEnvelopeError(t, runCLIForTest(t, home, map[string]string{}, nil, []string{
				"--json",
				"--base-url", server.URL,
				"threads", "recommendations",
				"--status", "active",
				"--type", "initiative",
			}))
			if got := anyStringValue(payload["command"]); got != "threads recommendations" {
				t.Fatalf("expected threads recommendations error command, got %#v", payload)
			}
			if got := anyStringValue(payload["command_id"]); got != "threads.recommendations" {
				t.Fatalf("expected threads.recommendations command_id, got %#v", payload)
			}
			errObj, _ := payload["error"].(map[string]any)
			if errObj == nil || anyStringValue(errObj["code"]) != "invalid_request" {
				t.Fatalf("expected invalid_request error payload, got %#v", payload)
			}
			if !strings.Contains(anyStringValue(errObj["message"]), tt.wantMessageSub) {
				t.Fatalf("expected message containing %q, got %#v", tt.wantMessageSub, payload)
			}
		})
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
				"recent_events":[{"id":"event_actor_1","type":"actor_statement","summary":"ship Friday scope"}],
				"key_artifacts":[],
				"open_commitments":[]
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
	recommendationCount, _ := collaboration["recommendation_count"].(float64)
	if got := int(recommendationCount); got != 1 {
		t.Fatalf("expected recommendation_count=1 after dedupe, got %#v", collaboration)
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
	threads, _ := data["threads"].([]any)
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

func TestDerivedRebuild(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/derived/rebuild" {
			body, _ := io.ReadAll(r.Body)
			if !bytes.Contains(body, []byte(`"force":true`)) {
				t.Fatalf("expected force=true in rebuild request: %s", string(body))
			}
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	home := t.TempDir()
	env := map[string]string{}
	out := runCLIForTest(t, home, env, strings.NewReader(`{"force":true}`), []string{"--json", "--base-url", server.URL, "derived", "rebuild"})
	var resp map[string]any
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("failed to parse JSON output: %v", err)
	}
	if resp["ok"] != true {
		t.Fatalf("expected ok=true, got %v", resp)
	}
}

func TestArtifactsInspectCommand(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/artifacts/artifact_1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"artifact":{"id":"artifact_1","kind":"packet","summary":"Brief"}}`))
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

func TestCommitmentsInspectAliasMapsToGet(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/commitments/commitment_1" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"commitment":{"id":"commitment_1","status":"open","title":"Publish"}}`))
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"commitments", "inspect",
		"--commitment-id", "commitment_1",
	})
	payload := assertEnvelopeOK(t, raw)
	if got := anyStringValue(payload["command"]); got != "commitments get" {
		t.Fatalf("expected commitments inspect alias to run get, payload=%#v", payload)
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

func TestMachineFacingTargetedCommandGoldens(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_123/timeline":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"thread_id":"thread_123",
				"events":[
					{"id":"event_100","thread_id":"thread_123","type":"actor_statement","created_at":"2026-03-07T00:00:00Z","summary":"ship machine-facing fixes"},
					{"id":"event_101","thread_id":"thread_123","type":"decision_needed","created_at":"2026-03-07T00:01:00Z","summary":"confirm frame shape"}
				],
				"snapshots":{},
				"artifacts":{}
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/events/event_456":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"event":{"id":"event_456","thread_id":"thread_123","type":"actor_statement","summary":"canonical event payload"}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_123/context":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
					"thread":{"id":"thread_123","title":"Machine-facing consistency"},
				"recent_events":[
					{"id":"event_ctx_1","thread_id":"thread_123","type":"actor_statement","summary":"normalize frame shape"},
					{"id":"event_ctx_2","thread_id":"thread_123","type":"decision_needed","summary":"confirm canonical command labels"}
				],
				"key_artifacts":[{"id":"artifact_ctx_1","kind":"work_order"}],
				"open_commitments":[{"id":"commitment_ctx_1","status":"open"}],
					"documents":[
						{"id":"doc_ctx_1","title":"Runbook","status":"active","updated_at":"2026-03-07T00:02:00Z","head_revision":{"revision_id":"rev_ctx_1","revision_number":3,"content_type":"text","artifact_id":"artifact_doc_ctx_1","created_at":"2026-03-07T00:02:00Z"}}
					]
				}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_123/workspace":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
					"thread_id":"thread_123",
					"thread":{"id":"thread_123","title":"Machine-facing consistency"},
					"context":{
						"recent_events":[
							{"id":"event_ctx_1","thread_id":"thread_123","type":"actor_statement","summary":"normalize frame shape"},
							{"id":"event_ctx_2","thread_id":"thread_123","type":"decision_needed","summary":"confirm canonical command labels"}
						],
						"key_artifacts":[{"id":"artifact_ctx_1","kind":"work_order"}],
						"open_commitments":[{"id":"commitment_ctx_1","status":"open"}],
						"documents":[
							{"id":"doc_ctx_1","title":"Runbook","status":"active","updated_at":"2026-03-07T00:02:00Z","head_revision":{"revision_id":"rev_ctx_1","revision_number":3,"content_type":"text","artifact_id":"artifact_doc_ctx_1","created_at":"2026-03-07T00:02:00Z"}}
						]
					},
					"collaboration":{
						"recommendations":[
							{"id":"event_ctx_1","thread_id":"thread_123","type":"actor_statement","summary":"normalize frame shape"}
						],
						"decision_requests":[
							{"id":"event_ctx_2","thread_id":"thread_123","type":"decision_needed","summary":"confirm canonical command labels"}
						],
						"decisions":[],
						"key_artifacts":[{"id":"artifact_ctx_1","kind":"work_order"}],
						"open_commitments":[{"id":"commitment_ctx_1","status":"open"}],
						"recommendation_count":1,
						"decision_request_count":1,
						"decision_count":0,
						"artifact_count":1,
						"open_commitment_count":1
					},
					"inbox":{
						"thread_id":"thread_123",
						"items":[
							{"id":"inbox:decision_needed:thread_123:none:event_ctx_2","thread_id":"thread_123","type":"decision_needed","summary":"confirm canonical command labels"}
						],
						"count":1
					},
					"pending_decisions":{
						"thread_id":"thread_123",
						"items":[
							{"id":"inbox:decision_needed:thread_123:none:event_ctx_2","thread_id":"thread_123","type":"decision_needed","summary":"confirm canonical command labels"}
						],
						"count":1
					},
					"related_threads":{"count":0,"items":[]},
					"related_recommendations":{"count":0,"items":[]},
					"related_decision_requests":{"count":0,"items":[]},
					"related_decisions":{"count":0,"items":[]},
					"total_review_items":3,
					"follow_up":{
						"context_refresh_command":"oar threads context --thread-id thread_123 --include-artifact-content --full-id --json",
						"decisions_list_command":"oar events list --thread-id thread_123 --type decision_needed --type decision_made --full-id --json",
						"events_get_examples":[
							"oar events get --event-id event_ctx_1 --json",
							"oar events get --event-id event_ctx_2 --json"
						],
						"events_get_template":"oar events get --event-id <event-id> --json",
						"recommendations_list_command":"oar events list --thread-id thread_123 --type actor_statement --full-id --json"
					},
					"context_source":"threads.context",
					"inbox_source":"inbox.list"
				}`))
		case r.Method == http.MethodGet && r.URL.Path == "/boards":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"boards":[
					{
						"board":{"id":"board_1234567890abcdef","title":"Machine Board","status":"active"},
						"summary":{
							"card_count":1,
							"cards_by_column":{"backlog":0,"ready":0,"in_progress":1,"blocked":0,"review":0,"done":0},
							"open_commitment_count":2,
							"document_count":1,
							"latest_activity_at":"2026-03-07T00:03:00Z",
							"has_primary_document":true
						}
					}
				]
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/boards/board_1234567890abcdef/workspace":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"board_id":"board_1234567890abcdef",
				"board":{"id":"board_1234567890abcdef","title":"Machine Board","status":"active","updated_at":"2026-03-07T00:03:00Z"},
				"primary_thread":{"id":"thread_123","title":"Machine-facing consistency"},
				"primary_document":{"id":"doc_ctx_1","title":"Runbook","status":"active"},
				"cards":{
					"items":[
						{
							"card":{"board_id":"board_1234567890abcdef","thread_id":"thread_123","column_key":"in_progress","rank":"m","pinned_document_id":null,"created_at":"2026-03-07T00:00:00Z","created_by":"actor_1","updated_at":"2026-03-07T00:03:00Z","updated_by":"actor_1"},
							"thread":{"id":"thread_123","title":"Machine-facing consistency"},
							"summary":{"open_commitment_count":1,"decision_request_count":1,"decision_count":0,"recommendation_count":1,"document_count":1,"inbox_count":1,"latest_activity_at":"2026-03-07T00:03:00Z","stale":false},
							"pinned_document":null
						}
					],
					"count":1
				},
				"documents":{"items":[{"id":"doc_ctx_1","title":"Runbook","status":"active"}],"count":1},
				"commitments":{"items":[{"id":"commitment_ctx_1","status":"open"}],"count":1},
				"inbox":{"items":[{"id":"inbox:decision_needed:thread_123:none:event_ctx_2","thread_id":"thread_123","type":"decision_needed"}],"count":1},
				"board_summary":{
					"card_count":1,
					"cards_by_column":{"backlog":0,"ready":0,"in_progress":1,"blocked":0,"review":0,"done":0},
					"open_commitment_count":1,
					"document_count":1,
					"latest_activity_at":"2026-03-07T00:03:00Z",
					"has_primary_document":true
				},
				"warnings":{"items":[],"count":0},
				"section_kinds":{"board":"canonical","cards":"canonical","documents":"derived","commitments":"derived","inbox":"derived","warnings":"derived"},
				"generated_at":"2026-03-07T00:03:00Z"
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/inbox":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
					"items":[
					{"id":"inbox:decision_needed:thread_123:none:event_ctx_2","thread_id":"thread_123","type":"decision_needed","summary":"confirm canonical command labels"},
					{"id":"inbox:decision_needed:thread_other:none:event_other","thread_id":"thread_other","type":"decision_needed","summary":"ignore other thread"}
				]
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "id: es_1\nevent: event\ndata: {\"event\":{\"id\":\"event_stream_1\",\"type\":\"actor_statement\"}}\n\n")
		case r.Method == http.MethodGet && r.URL.Path == "/inbox/stream":
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
		"--type", "actor_statement",
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
	assertGolden(t, "threads_inspect_machine.golden.json", threadsInspectOut)
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
	assertGolden(t, "threads_workspace_machine.golden.json", threadsWorkspaceOut)
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
	if _, ok := threadsWorkspaceData["pending_decisions"].(map[string]any); !ok {
		t.Fatalf("expected pending_decisions section in workspace payload, got %#v", threadsWorkspaceData)
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
	if got := anyBoolValue(threadsReviewData["related_event_content_enabled"]); !got {
		t.Fatalf("expected related_event_content_enabled marker in review payload, got %#v", threadsReviewData)
	}
	if got := anyBoolValue(threadsReviewData["full_summary"]); !got {
		t.Fatalf("expected full_summary enabled in review payload, got %#v", threadsReviewData)
	}

	threadsRecommendationsOut := runCLIForTest(t, home, env, nil, []string{
		"--json",
		"--base-url", server.URL,
		"threads", "recommendations",
		"--thread-id", "thread_123",
	})
	assertGolden(t, "threads_recommendations_machine.golden.json", threadsRecommendationsOut)
	threadsRecommendationsPayload := assertEnvelopeOK(t, threadsRecommendationsOut)
	if got := anyStringValue(threadsRecommendationsPayload["command"]); got != "threads recommendations" {
		t.Fatalf("expected threads recommendations command label, got %#v", threadsRecommendationsPayload)
	}
	if got := anyStringValue(threadsRecommendationsPayload["command_id"]); got != "threads.recommendations" {
		t.Fatalf("expected threads.recommendations command_id, got %#v", threadsRecommendationsPayload)
	}
	threadsRecommendationsData, _ := threadsRecommendationsPayload["data"].(map[string]any)
	if _, ok := threadsRecommendationsData["recommendations"].(map[string]any); !ok {
		t.Fatalf("expected recommendations section in payload, got %#v", threadsRecommendationsData)
	}
	if _, ok := threadsRecommendationsData["pending_decisions"].(map[string]any); !ok {
		t.Fatalf("expected pending_decisions section in payload, got %#v", threadsRecommendationsData)
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
		case "/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "id: e-1\nevent: event\ndata: {\"event\":{\"id\":\"event_1\"}}\n\n")
		case "/inbox/stream":
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
	}))
	if got := anyStringValue(eventsListErr["command"]); got != "events list" {
		t.Fatalf("expected events list error command, got %q payload=%#v", got, eventsListErr)
	}
	if got := anyStringValue(eventsListErr["command_id"]); got != "threads.timeline" {
		t.Fatalf("expected threads.timeline command_id, got %q payload=%#v", got, eventsListErr)
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
		"threads", "recommendations",
	}))
	if got := anyStringValue(threadsRecommendationsErr["command"]); got != "threads recommendations" {
		t.Fatalf("expected threads recommendations error command, got %q payload=%#v", got, threadsRecommendationsErr)
	}
	if got := anyStringValue(threadsRecommendationsErr["command_id"]); got != "threads.recommendations" {
		t.Fatalf("expected threads.recommendations command_id, got %q payload=%#v", got, threadsRecommendationsErr)
	}
}

func TestEventsStreamFallbackPayloadForNonWrapperJSON(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/events/stream" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "id: e-fallback\nevent: event\ndata: {\"id\":\"event_raw_1\",\"type\":\"actor_statement\"}\n\n")
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

func TestFilterEventsByLifecycleState(t *testing.T) {
	t.Parallel()

	active := map[string]any{"id": "evt_a", "type": "actor_statement"}
	archived := map[string]any{"id": "evt_b", "archived_at": "2024-01-01T00:00:00Z"}
	tomb := map[string]any{"id": "evt_c", "tombstoned_at": "2024-01-02T00:00:00Z"}
	archivedAndTomb := map[string]any{"id": "evt_d", "archived_at": "2024-01-01T00:00:00Z", "tombstoned_at": "2024-01-02T00:00:00Z"}

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
		t.Fatalf("tombstoned-only: want 2, got %#v", tombOnly)
	}

	archOnly := filterEventsByLifecycleState(events, false, true, false, false)
	if len(archOnly) != 1 || anyString(asMap(archOnly[0])["id"]) != "evt_b" {
		t.Fatalf("archived-only: want evt_b, got %#v", archOnly)
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

func anyBoolValue(raw any) bool {
	value, _ := raw.(bool)
	return value
}
