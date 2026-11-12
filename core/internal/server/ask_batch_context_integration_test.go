package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

type contextBatchEnv struct {
	env      authIntegrationEnv
	agent    notificationTestAgent
	human    lockoutPrincipalSeed
	threadID string
}

func newContextBatchEnv(t *testing.T, name string) contextBatchEnv {
	t.Helper()
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	agent := seedNotificationTestAgent(t, env, name+".agent")
	human := seedHumanPrincipalForLockoutTest(t, context.Background(), env.workspace.DB(), name+"-human", name+"-human-actor", name+"-human", name+"-human-token")
	threadID := integrationSeedThreadWithStore(t, env.primitiveStore, nil, agent.ActorID, map[string]any{
		"title": "Context batch thread", "type": "incident", "status": "active", "priority": "p2",
		"tags": []any{"asks"}, "cadence": "daily", "next_check_in_at": "2026-03-06T00:00:00Z",
		"current_summary": "summary", "next_actions": []any{"check"}, "key_artifacts": []any{},
		"provenance": map[string]any{"sources": []any{"inferred"}},
	})
	return contextBatchEnv{env: env, agent: agent, human: human, threadID: threadID}
}

// question is a batch member with no situation or impact of its own: only
// the question, its recommendation and alternatives.
func (c contextBatchEnv) question(n int, extra map[string]any) map[string]any {
	title := fmt.Sprintf("Policy question %d?", n)
	payload := map[string]any{
		"kind": "ask", "title": title,
		"subject_ref":        "thread:" + c.threadID,
		"requester_actor_id": c.agent.ActorID,
		"requester_label":    c.agent.Username,
		"response_proposals": []any{"Yes", "No", "Later"},
		"recommendation":     map[string]any{"action": "Yes", "why": "It matches the shared plan."},
		"alternatives":       []any{map[string]any{"action": "No", "consequence": "Keeps today's cost."}},
		"delivery":           "batch",
	}
	for k, v := range extra {
		payload[k] = v
	}
	return map[string]any{
		"type": "human_attention_requested", "thread_id": c.threadID, "summary": title,
		"refs": []string{"thread:" + c.threadID}, "payload": payload,
		"provenance": map[string]any{"sources": []string{"event:cli.ask"}},
	}
}

var sharedBatchContext = map[string]any{
	"title":     "Log retention interview",
	"situation": "Every log is kept for 400 days and storage grows 9% a month.",
	"impact":    "Audits need a written policy before the next review.",
	"details":   "Numbers come from the November storage review.",
	"evidence":  []any{map[string]any{"label": "Storage review", "url": "https://example.com/review"}},
}

func (c contextBatchEnv) fileFive(t *testing.T) []string {
	t.Helper()
	asks := []map[string]any{}
	for n := 1; n <= 5; n++ {
		extra := map[string]any{}
		if n == 3 {
			extra["multi_select"] = true
			extra["note"] = "Pick every option that applies."
		}
		asks = append(asks, c.question(n, extra))
	}
	status, result := hostHTTP(t, http.MethodPost, c.env.server.URL+"/asks/batch", c.agent.AccessToken, map[string]any{
		"request_key": "context-batch", "context": sharedBatchContext, "asks": asks,
	})
	if status != http.StatusCreated {
		t.Fatalf("file context batch: %d %#v", status, result)
	}
	ids := []string{}
	for _, raw := range result["events"].([]any) {
		ids = append(ids, anyString(raw.(map[string]any)["id"]))
	}
	// An exact retry replays (the context is part of the request hash).
	status, replay := hostHTTP(t, http.MethodPost, c.env.server.URL+"/asks/batch", c.agent.AccessToken, map[string]any{
		"request_key": "context-batch", "context": sharedBatchContext, "asks": asks,
	})
	if status != http.StatusCreated || replay["batch_id"] != result["batch_id"] {
		t.Fatalf("context batch replay: %d %#v", status, replay)
	}
	return ids
}

func (c contextBatchEnv) batchItem(t *testing.T, reader ...string) (primitives.DerivedInboxItem, bool) {
	t.Helper()
	ctx := context.Background()
	if err := refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: c.env.primitiveStore}, c.threadID, time.Now().UTC(), c.agent.ActorID); err != nil {
		t.Fatalf("refresh projection: %v", err)
	}
	filter := primitives.DerivedInboxListFilter{ThreadID: c.threadID}
	if len(reader) > 0 {
		filter.RecipientActorID = reader[0]
	}
	items, err := c.env.primitiveStore.ListDerivedInboxItems(ctx, filter)
	if err != nil || len(items) > 1 {
		t.Fatalf("expected at most one batch item, got %d (%v)", len(items), err)
	}
	if len(items) == 0 {
		return primitives.DerivedInboxItem{}, false
	}
	return items[0], true
}

func sectionsOf(item primitives.DerivedInboxItem) []map[string]any {
	return batchSectionsFromPayload(item.Data["batch"].(map[string]any)["asks"])
}

// Acceptance (oss#538): a 5-question batch with one shared context files
// without per-ask situation text and derives one item stating the context
// once, questions numbered in filing order, question 3 multi-select.
func TestAskBatchSharedContextDerivesOneOrderedItem(t *testing.T) {
	c := newContextBatchEnv(t, "ctxbatch")
	ids := c.fileFive(t)
	item, ok := c.batchItem(t)
	if !ok {
		t.Fatal("no batch item")
	}
	batch := item.Data["batch"].(map[string]any)
	ctxBlock, _ := batch["context"].(map[string]any)
	if ctxBlock["situation"] != sharedBatchContext["situation"] || ctxBlock["impact"] != sharedBatchContext["impact"] {
		t.Fatalf("batch context = %#v", batch["context"])
	}
	if item.Data["title"] != "Log retention interview" {
		t.Fatalf("item title = %#v", item.Data["title"])
	}
	sections := sectionsOf(item)
	if len(sections) != 5 {
		t.Fatalf("sections = %d", len(sections))
	}
	for i, section := range sections {
		if section["request_event_ref"] != "event:"+ids[i] || anyIntValue(section["position"]) != i+1 {
			t.Fatalf("section %d out of order: %#v", i, section)
		}
		if _, has := section["situation"]; has {
			t.Fatalf("section %d repeats context: %#v", i, section)
		}
		if multi := section["multi_select"] == true; multi != (i == 2) {
			t.Fatalf("section %d multi_select = %v", i, section["multi_select"])
		}
	}
	if sections[2]["note"] != "Pick every option that applies." {
		t.Fatalf("note lost: %#v", sections[2])
	}

	body := readBriefV1(t, c.env.primitiveStore.(*primitives.Store))
	found := false
	for _, raw := range body["decisions"].([]any) {
		d := raw.(map[string]any)
		briefBatch, _ := d["batch"].(map[string]any)
		if briefBatch == nil {
			continue
		}
		found = true
		if d["title"] != "Log retention interview" || briefBatch["context"].(map[string]any)["situation"] != sharedBatchContext["situation"] {
			t.Fatalf("brief batch = %#v", d)
		}
		members := briefBatch["members"].([]any)
		for i, raw := range members {
			m := raw.(map[string]any)
			if m["event_ref"] != "event:"+ids[i] || anyIntValue(m["position"]) != i+1 || len(m["proposals"].([]any)) != 3 {
				t.Fatalf("brief member %d = %#v", i, m)
			}
		}
		if members[2].(map[string]any)["multi_select"] != true {
			t.Fatalf("brief lost multi_select: %#v", members[2])
		}
	}
	if !found {
		t.Fatalf("brief has no batch decision: %#v", body["decisions"])
	}
}

func TestAskBatchContextValidation(t *testing.T) {
	c := newContextBatchEnv(t, "ctxvalid")
	for name, tc := range map[string]struct {
		context any
		extra   map[string]any
		want    string
	}{
		"no situation":      {map[string]any{"impact": "Only impact."}, nil, "context.situation is required"},
		"unknown key":       {map[string]any{"situation": "S", "why": "x"}, nil, "context.why is not a context field"},
		"bad evidence":      {map[string]any{"situation": "S", "evidence": []any{map[string]any{"label": "x"}}}, nil, "context.evidence[0]"},
		"client position":   {nil, map[string]any{"batch_position": 1}, "batch_position is assigned by the server"},
		"client context":    {nil, map[string]any{"batch_context": map[string]any{"situation": "S"}}, "batch_context is assigned by the server"},
		"multi_select type": {nil, map[string]any{"multi_select": "yes"}, "multi_select must be a boolean"},
	} {
		t.Run(name, func(t *testing.T) {
			body := map[string]any{"asks": []map[string]any{c.question(1, tc.extra), c.question(2, nil)}}
			if tc.context != nil {
				body["context"] = tc.context
			}
			status, result := hostHTTP(t, http.MethodPost, c.env.server.URL+"/asks/batch", c.agent.AccessToken, body)
			if status != http.StatusBadRequest || !strings.Contains(fmt.Sprint(result), tc.want) {
				t.Fatalf("status %d %#v, want %q", status, result, tc.want)
			}
		})
	}
}

func batchAnswer(section map[string]any, text string) map[string]any {
	return map[string]any{"ask_ref": section["request_event_ref"], "response_text": text, "outcome": "answered"}
}

// Acceptance (oss#538): a partial submit records the answered questions,
// leaves the rest open on the same item, and each submission releases one
// wake for the asks it answered.
func TestAskBatchPartialSubmitLeavesRestOpen(t *testing.T) {
	c := newContextBatchEnv(t, "ctxpartial")
	ctx := context.Background()
	store := c.env.primitiveStore.(*primitives.Store)
	ids := c.fileFive(t)
	item, _ := c.batchItem(t)
	sections := sectionsOf(item)
	respond := func(body map[string]any) (int, map[string]any) {
		return hostHTTP(t, http.MethodPost, c.env.server.URL+"/inbox/"+url.PathEscape(item.ID)+"/respond", c.human.AccessToken, body)
	}

	first := map[string]any{"idempotency_key": "partial-1", "partial": true, "answers": []map[string]any{
		batchAnswer(sections[0], "Yes"), batchAnswer(sections[2], "Yes; Later"),
	}}
	status, result := respond(first)
	if status != http.StatusCreated || anyIntValue(result["open_ask_count"]) != 3 || len(result["answers"].([]any)) != 2 {
		t.Fatalf("partial respond: %d %#v", status, result)
	}
	after, ok := c.batchItem(t)
	if !ok || after.ID != item.ID {
		t.Fatalf("partial answer closed the batch item: ok=%v id=%s", ok, after.ID)
	}
	open := sectionsOf(after)
	if len(open) != 3 || open[0]["request_event_ref"] != "event:"+ids[1] || anyIntValue(open[1]["position"]) != 4 {
		t.Fatalf("remaining sections = %#v", open)
	}
	if after.Data["batch"].(map[string]any)["context"] == nil {
		t.Fatal("remaining batch lost its shared context")
	}
	// The brief keeps answering through the same item, now with three members.
	briefOK := false
	for _, raw := range readBriefV1(t, store)["decisions"].([]any) {
		d := raw.(map[string]any)
		if b, _ := d["batch"].(map[string]any); b != nil {
			briefOK = d["inbox_item_id"] == item.ID && len(b["members"].([]any)) == 3
		}
	}
	if !briefOK {
		t.Fatal("brief must keep the batch's respond id and list only the open questions")
	}
	// A retry of the same partial answer replays, although its asks have left
	// the open item.
	if status, replay := respond(first); status != http.StatusCreated || len(replay["answers"].([]any)) != 2 {
		t.Fatalf("partial replay: %d %#v", status, replay)
	}
	// Answering an already answered question in a new submission conflicts.
	if status, _ := respond(map[string]any{"partial": true, "answers": []map[string]any{batchAnswer(sections[0], "No")}}); status != http.StatusBadRequest && status != http.StatusConflict {
		t.Fatalf("re-answer of a closed question: %d", status)
	}

	maintainer := NewAnswerWakeMaintainer(AnswerWakeMaintainerConfig{PrimitiveStore: store, WorkspaceID: "test-workspace", FlushWhenNoOpenAsks: true})
	if err := maintainer.FlushTarget(ctx, c.agent.ActorID); err != nil {
		t.Fatalf("flush: %v", err)
	}
	wakes, err := store.ListAgentWakeups(ctx, primitives.AgentWakeupListFilter{TargetActorID: c.agent.ActorID})
	if err != nil || len(wakes) != 1 {
		t.Fatalf("first submission must release one wake, got %d (%v)", len(wakes), err)
	}
	refs := strings.Join(wakes[0].Refs, " ")
	if !strings.Contains(refs, "event:"+ids[0]) || !strings.Contains(refs, "event:"+ids[2]) || strings.Contains(refs, "event:"+ids[1]) {
		t.Fatalf("first wake refs = %s", refs)
	}

	// The rest close the batch in one more submission.
	status, result = respond(map[string]any{"answers": []map[string]any{
		batchAnswer(open[0], "No"), batchAnswer(open[1], "Write it down."), batchAnswer(open[2], "Yes"),
	}})
	if status != http.StatusCreated || result["open_ask_count"] != nil {
		t.Fatalf("closing respond: %d %#v", status, result)
	}
	if _, ok := c.batchItem(t); ok {
		t.Fatal("fully answered batch must leave the inbox")
	}
	if err := maintainer.FlushTarget(ctx, c.agent.ActorID); err != nil {
		t.Fatalf("flush: %v", err)
	}
	wakes, _ = store.ListAgentWakeups(ctx, primitives.AgentWakeupListFilter{TargetActorID: c.agent.ActorID})
	if len(wakes) != 2 {
		t.Fatalf("second submission must release exactly one more wake, got %d", len(wakes))
	}
	for _, id := range ids {
		resp := getJSONExpectStatusWithAuth(t, c.env.server.URL+"/asks/"+id, c.agent.AccessToken, http.StatusOK)
		resp.Body.Close()
	}
}

func TestAskBatchPartialFlagRules(t *testing.T) {
	c := newContextBatchEnv(t, "ctxrules")
	c.fileFive(t)
	item, _ := c.batchItem(t)
	sections := sectionsOf(item)
	path := c.env.server.URL + "/inbox/" + url.PathEscape(item.ID) + "/respond"
	// Without partial, a subset is still refused and nothing records.
	status, result := hostHTTP(t, http.MethodPost, path, c.human.AccessToken, map[string]any{"answers": []map[string]any{batchAnswer(sections[0], "Yes")}})
	if status != http.StatusBadRequest || !strings.Contains(fmt.Sprint(result), "set partial") {
		t.Fatalf("subset without partial: %d %#v", status, result)
	}
	status, _ = hostHTTP(t, http.MethodPost, path, c.human.AccessToken, map[string]any{"partial": true, "response_text": "Yes", "outcome": "answered"})
	if status != http.StatusBadRequest {
		t.Fatalf("flat partial: %d", status)
	}
	if after, _ := c.batchItem(t); len(sectionsOf(after)) != 5 {
		t.Fatal("refused requests must not record answers")
	}
}

func anyIntValue(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	}
	return 0
}

// Review (oss#538): a batch keeps per-ask recipient visibility. One shared
// recipient gates the batch item like a single addressed ask; asks addressed
// to different people cannot be filed together, and such a batch from before
// the rule splits into each member's own addressed item.
func TestAskBatchRecipientVisibility(t *testing.T) {
	c := newContextBatchEnv(t, "ctxrecip")
	ctx := context.Background()
	other := seedHumanPrincipalForLockoutTest(t, ctx, c.env.workspace.DB(), "ctxrecip-other", "ctxrecip-other-actor", "ctxrecip-other", "ctxrecip-other-token")
	to := func(n int, recipient string) map[string]any {
		return c.question(n, map[string]any{"recipient_actor_id": recipient})
	}
	file := func(asks ...map[string]any) (int, map[string]any) {
		return hostHTTP(t, http.MethodPost, c.env.server.URL+"/asks/batch", c.agent.AccessToken, map[string]any{"context": sharedBatchContext, "asks": asks})
	}
	if status, body := file(to(1, c.human.ActorID), to(2, other.ActorID)); status != http.StatusBadRequest || !strings.Contains(fmt.Sprint(body), "same recipient_actor_id") {
		t.Fatalf("mixed recipients filed: %d %#v", status, body)
	}
	if status, _ := file(to(1, c.human.ActorID), c.question(2, nil)); status != http.StatusBadRequest {
		t.Fatalf("addressed plus unaddressed filed: %d", status)
	}
	if status, body := file(to(1, c.human.ActorID), to(2, "actor:"+c.human.ActorID)); status != http.StatusCreated {
		t.Fatalf("one recipient batch: %d %#v", status, body)
	}
	if _, visible := c.batchItem(t); visible {
		t.Fatal("an addressed batch listed for no reader")
	}
	item, ok := c.batchItem(t, c.human.ActorID)
	if !ok || item.Data["recipient_actor_id"] != c.human.ActorID {
		t.Fatalf("batch item lost its recipient: %#v", item.Data["recipient_actor_id"])
	}
	inboxPath := c.env.server.URL + "/inbox/" + url.PathEscape(item.ID)
	listed := func(token string) int {
		_, page := hostHTTP(t, http.MethodGet, c.env.server.URL+"/inbox?limit=100", token, nil)
		n := 0
		for _, raw := range asSliceOf(page["items"]) {
			if row, _ := raw.(map[string]any); row != nil && row["batch"] != nil {
				n++
			}
		}
		return n
	}
	if status, _ := hostHTTP(t, http.MethodGet, inboxPath, other.AccessToken, nil); status != http.StatusNotFound {
		t.Fatalf("another human read the addressed batch: %d", status)
	}
	if listed(other.AccessToken) != 0 || listed(c.human.AccessToken) != 1 {
		t.Fatalf("inbox list must show the batch only to its recipient")
	}
	sections := sectionsOf(item)
	answers := []map[string]any{batchAnswer(sections[0], "Yes"), batchAnswer(sections[1], "No")}
	if status, _ := hostHTTP(t, http.MethodPost, inboxPath+"/respond", other.AccessToken, map[string]any{"answers": answers}); status != http.StatusNotFound {
		t.Fatalf("another human answered the addressed batch: %d", status)
	}
	if _, brief := hostHTTP(t, http.MethodGet, c.env.server.URL+"/brief", other.AccessToken, nil); strings.Contains(fmt.Sprint(brief["decisions"]), "Policy question") {
		t.Fatal("the brief showed an addressed batch to someone else")
	}
	if status, body := hostHTTP(t, http.MethodPost, inboxPath+"/respond", c.human.AccessToken, map[string]any{"answers": answers}); status != http.StatusCreated {
		t.Fatalf("recipient answer: %d %#v", status, body)
	}

	// A batch filed before the rule, with two recipients, is never one item.
	store := c.env.primitiveStore.(*primitives.Store)
	if _, _, err := store.AppendTaskAttentionEventBatch(ctx, c.agent.ActorID, "", "", []map[string]any{to(3, c.human.ActorID), to(4, other.ActorID)}); err != nil {
		t.Fatal(err)
	}
	if err := refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: c.env.primitiveStore}, c.threadID, time.Now().UTC(), c.agent.ActorID); err != nil {
		t.Fatal(err)
	}
	var items []primitives.DerivedInboxItem
	for _, reader := range []string{c.human.ActorID, other.ActorID} {
		mine, err := store.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: c.threadID, RecipientActorID: reader})
		if err != nil || len(mine) != 1 || mine[0].Data["recipient_actor_id"] != reader {
			t.Fatalf("legacy mixed batch: %s sees %d items (%v)", reader, len(mine), err)
		}
		items = append(items, mine...)
	}
	for _, split := range items {
		if split.Data["batch"] != nil || split.Data["recipient_actor_id"] == "" || split.Data["recipient_actor_id"] == nil {
			t.Fatalf("split item = %#v", split.Data)
		}
	}
	if listed(other.AccessToken) != 0 {
		t.Fatal("a split member must not reach anyone as a batch")
	}
	_, mine := hostHTTP(t, http.MethodGet, c.env.server.URL+"/brief", other.AccessToken, nil)
	for _, raw := range asSliceOf(mine["decisions"]) {
		d, _ := raw.(map[string]any)
		if d["batch"] != nil || d["title"] == "Policy question 3?" {
			t.Fatalf("other's brief shows someone else's ask or a collapsed mixed batch: %#v", d)
		}
	}
}

func asSliceOf(v any) []any { s, _ := v.([]any); return s }

// Review (oss#538): the projection kept a single ask's recipient only on the
// no-projection fallback, so another human could read and answer it.
func TestAddressedSingleAskStaysWithItsRecipient(t *testing.T) {
	c := newContextBatchEnv(t, "ctxsingle")
	other := seedHumanPrincipalForLockoutTest(t, context.Background(), c.env.workspace.DB(), "ctxsingle-other", "ctxsingle-other-actor", "ctxsingle-other", "ctxsingle-other-token")
	if status, body := hostHTTP(t, http.MethodPost, c.env.server.URL+"/events", c.agent.AccessToken, map[string]any{"event": c.question(1, map[string]any{"recipient_actor_id": c.human.ActorID})}); status != http.StatusCreated {
		t.Fatalf("file: %d %#v", status, body)
	}
	item, ok := c.batchItem(t, c.human.ActorID)
	if !ok || item.Data["recipient_actor_id"] != c.human.ActorID {
		t.Fatalf("single ask lost its recipient: %#v", item.Data)
	}
	path := c.env.server.URL + "/inbox/" + url.PathEscape(item.ID)
	if status, _ := hostHTTP(t, http.MethodGet, path, other.AccessToken, nil); status != http.StatusNotFound {
		t.Fatalf("another human read the addressed ask: %d", status)
	}
	if status, _ := hostHTTP(t, http.MethodPost, path+"/respond", other.AccessToken, map[string]any{"response_text": "No", "outcome": "answered", "notify_mode": "none"}); status != http.StatusNotFound {
		t.Fatalf("another human answered the addressed ask: %d", status)
	}
	if status, body := hostHTTP(t, http.MethodPost, path+"/respond", c.human.AccessToken, map[string]any{"response_text": "Yes", "outcome": "answered", "notify_mode": "none"}); status != http.StatusCreated {
		t.Fatalf("recipient answer: %d %#v", status, body)
	}
}
