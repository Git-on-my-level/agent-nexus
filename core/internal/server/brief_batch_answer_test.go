package server

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

// Answering the brief's batch inbox id closes every open member in one
// transaction and replays on the same idempotency key.
func TestBriefBatchAnswerClosesEveryMember(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	testBriefFiledBatchAnswer(t)
	testBriefCurationBatchAnswer(t)
}

func testBriefFiledBatchAnswer(t *testing.T) {
	t.Helper()
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	ctx := context.Background()
	agent := seedNotificationTestAgent(t, env, "filedanswer.agent")
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "filedanswer-human", "filedanswer-human-actor", "filedanswer-human", "filedanswer-human-token")
	store := env.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, agent.ActorID, map[string]any{"title": "Filed batch board"})
	if err != nil {
		t.Fatal(err)
	}
	card := func(title string) string {
		t.Helper()
		work, err := store.CreateWork(ctx, agent.ActorID, anyString(board["id"]), map[string]any{"title": title})
		if err != nil {
			t.Fatal(err)
		}
		return anyString(work["ref"])
	}
	cardA, cardB, cardC := card("Alpha"), card("Bravo"), card("Charlie")
	threadID := integrationSeedThreadWithStore(t, env.primitiveStore, nil, agent.ActorID, map[string]any{
		"title": "Filed answer thread", "type": "incident", "status": "active", "priority": "p2",
		"tags": []any{}, "cadence": "daily", "current_summary": "summary", "next_actions": []any{}, "key_artifacts": []any{},
		"provenance": map[string]any{"sources": []any{"inferred"}},
	})
	ask := func(title, subject string, blocks []any) map[string]any {
		payload := map[string]any{
			"kind": "ask", "title": title, "subject_ref": subject,
			"requester_actor_id": agent.ActorID, "requester_label": agent.Username,
			"response_proposals": []any{"Ship it", "Wait"}, "delivery": "none",
		}
		if len(blocks) > 0 {
			payload["blocks"] = blocks
		}
		return map[string]any{
			"type": "human_attention_requested", "thread_id": threadID, "summary": title,
			"refs":       []string{"thread:" + threadID, subject},
			"payload":    payload,
			"provenance": map[string]any{"sources": []any{"event:cli.ask"}},
		}
	}
	status, result := hostHTTP(t, http.MethodPost, env.server.URL+"/asks/batch", agent.AccessToken, map[string]any{
		"request_key": "brief-answer-batch",
		"asks": []map[string]any{
			ask("Filed one", cardA, []any{cardB}),
			ask("Filed two", cardB, nil),
			ask("Filed three", cardC, nil),
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("file ask batch: %d %#v", status, result)
	}
	batchID := anyString(result["batch_id"])
	anchor, askIDs := oldestBatchEvent(t, result["events"])
	if len(askIDs) != 3 || batchID == "" {
		t.Fatalf("batch filing: %#v", result)
	}
	wantID := primitives.HumanAttentionInboxItemID("ask", threadID, batchID, anchor)
	if err := refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: env.primitiveStore}, threadID, time.Now().UTC(), agent.ActorID); err != nil {
		t.Fatal(err)
	}
	items, err := env.primitiveStore.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: threadID})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != wantID {
		got := make([]string, len(items))
		for i, item := range items {
			got[i] = item.ID
		}
		t.Fatalf("derived batch id = %v, want %s", got, wantID)
	}

	briefStatus, brief := hostHTTP(t, http.MethodGet, env.server.URL+"/brief", human.AccessToken, nil)
	if briefStatus != http.StatusOK {
		t.Fatalf("brief: %d %#v", briefStatus, brief)
	}
	decision := oneBatchDecision(t, brief)
	if anyString(decision["inbox_item_id"]) != wantID || anyString(decision["ref"]) != wantID {
		t.Fatalf("brief respond id = %v %v, want %s", decision["inbox_item_id"], decision["ref"], wantID)
	}
	members, _ := decision["batch"].(map[string]any)["members"].([]any)
	if len(members) != 3 {
		t.Fatalf("members: %#v", decision["batch"])
	}
	subjects := map[string]bool{}
	blocked := false
	for _, raw := range members {
		member, _ := raw.(map[string]any)
		id := anyString(member["inbox_item_id"])
		if id == "" || id == wantID {
			t.Fatalf("member inbox id = %q (batch %s)", id, wantID)
		}
		subjects[anyString(member["subject_ref"])] = true
		for _, block := range anySlice(member["blocks"]) {
			if anyString(asMap(block)["ref"]) == cardB {
				blocked = true
			}
		}
	}
	for _, ref := range []string{cardA, cardB, cardC} {
		if !subjects[ref] {
			t.Fatalf("member subjects %v missing %s", subjects, ref)
		}
	}
	if !blocked {
		t.Fatalf("member blocks missing %s: %#v", cardB, members)
	}

	body := map[string]any{
		"idempotency_key": "filed-batch-answer",
		"response_text":   "Ship it",
		"outcome":         "answered",
		"notify_mode":     "none",
	}
	answerAndReplayBatch(t, env.server.URL, human.AccessToken, env.workspace.DB(), wantID, askIDs, body)
	briefStatus, brief = hostHTTP(t, http.MethodGet, env.server.URL+"/brief", human.AccessToken, nil)
	if briefStatus != http.StatusOK {
		t.Fatalf("brief after answer: %d %#v", briefStatus, brief)
	}
	for _, raw := range anySlice(brief["decisions"]) {
		title := anyString(asMap(raw)["title"])
		if strings.HasPrefix(title, "Filed ") {
			t.Fatalf("answered member still open: %#v", raw)
		}
	}
}

// Withdrawing the oldest member must not move the brief's respond id. The
// inbox projection keeps that withdrawn member as the anchor; answering the
// brief id closes the two members that stay open.
func TestBriefFiledBatchAnswerAfterOldestWithdrawn(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	ctx := context.Background()
	agent := seedNotificationTestAgent(t, env, "filedwithdraw.agent")
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "filedwithdraw-human", "filedwithdraw-human-actor", "filedwithdraw-human", "filedwithdraw-human-token")
	store := env.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, agent.ActorID, map[string]any{"title": "Filed withdraw board"})
	if err != nil {
		t.Fatal(err)
	}
	card := func(title string) string {
		t.Helper()
		work, err := store.CreateWork(ctx, agent.ActorID, anyString(board["id"]), map[string]any{"title": title})
		if err != nil {
			t.Fatal(err)
		}
		return anyString(work["ref"])
	}
	cardA, cardB, cardC := card("Alpha"), card("Bravo"), card("Charlie")
	threadID := integrationSeedThreadWithStore(t, env.primitiveStore, nil, agent.ActorID, map[string]any{
		"title": "Filed withdraw thread", "type": "incident", "status": "active", "priority": "p2",
		"tags": []any{}, "cadence": "daily", "current_summary": "summary", "next_actions": []any{}, "key_artifacts": []any{},
		"provenance": map[string]any{"sources": []any{"inferred"}},
	})
	ask := func(title, subject string) map[string]any {
		return map[string]any{
			"type": "human_attention_requested", "thread_id": threadID, "summary": title,
			"refs": []string{"thread:" + threadID, subject},
			"payload": map[string]any{
				"kind": "ask", "title": title, "subject_ref": subject,
				"requester_actor_id": agent.ActorID, "requester_label": agent.Username,
				"response_proposals": []any{"Ship it", "Wait"}, "delivery": "none",
			},
			"provenance": map[string]any{"sources": []any{"event:cli.ask"}},
		}
	}
	status, result := hostHTTP(t, http.MethodPost, env.server.URL+"/asks/batch", agent.AccessToken, map[string]any{
		"request_key": "brief-withdraw-batch",
		"asks":        []map[string]any{ask("Filed one", cardA), ask("Filed two", cardB), ask("Filed three", cardC)},
	})
	if status != http.StatusCreated {
		t.Fatalf("file ask batch: %d %#v", status, result)
	}
	batchID := anyString(result["batch_id"])
	anchor, askIDs := oldestBatchEvent(t, result["events"])
	wantID := primitives.HumanAttentionInboxItemID("ask", threadID, batchID, anchor)
	status, withdrawn := hostHTTP(t, http.MethodPost, env.server.URL+"/events", agent.AccessToken, map[string]any{
		"event": map[string]any{
			"type": "human_attention_withdrawn", "thread_id": threadID, "summary": "Filed one",
			"refs": []string{"event:" + anchor},
			"payload": map[string]any{
				"request_event_id": anchor, "request_event_ref": "event:" + anchor,
				"withdrawn_by_actor_id": agent.ActorID, "reason": "Duplicate of the kept ask",
			},
			"provenance": map[string]any{"sources": []string{"event:cli.ask"}},
		},
	})
	if status != http.StatusCreated {
		t.Fatalf("withdraw oldest: %d %#v", status, withdrawn)
	}
	if err := refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: env.primitiveStore}, threadID, time.Now().UTC(), agent.ActorID); err != nil {
		t.Fatal(err)
	}
	items, err := env.primitiveStore.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: threadID})
	if err != nil || len(items) != 1 || items[0].ID != wantID {
		t.Fatalf("inbox id after withdrawal = %#v, want %s (%v)", items, wantID, err)
	}

	briefStatus, brief := hostHTTP(t, http.MethodGet, env.server.URL+"/brief", human.AccessToken, nil)
	if briefStatus != http.StatusOK {
		t.Fatalf("brief: %d %#v", briefStatus, brief)
	}
	decision := oneBatchDecision(t, brief)
	if anyString(decision["inbox_item_id"]) != wantID || anyString(decision["ref"]) != wantID {
		t.Fatalf("brief respond id = %v %v, want %s", decision["inbox_item_id"], decision["ref"], wantID)
	}
	members, _ := decision["batch"].(map[string]any)["members"].([]any)
	if len(members) != 2 {
		t.Fatalf("open members = %d, want 2: %#v", len(members), decision["batch"])
	}

	remaining := make([]string, 0, 2)
	for _, id := range askIDs {
		if id != anchor {
			remaining = append(remaining, id)
		}
	}
	answerAndReplayBatch(t, env.server.URL, human.AccessToken, env.workspace.DB(), anyString(decision["inbox_item_id"]), remaining, map[string]any{
		"idempotency_key": "filed-batch-answer-after-withdraw",
		"response_text":   "Ship it",
		"outcome":         "answered",
		"notify_mode":     "none",
	})
	if state := askState(t, env.workspace.DB(), anchor); state != "withdrawn" {
		t.Fatalf("oldest member state = %s, want withdrawn", state)
	}
}

func testBriefCurationBatchAnswer(t *testing.T) {
	t.Helper()
	f := newCurationFixture(t, "batchanswer")
	subject, _ := f.trackerCard("Shared subject", "batch-answer-subject")
	blocked, _ := f.trackerCard("Blocked work", "batch-answer-blocked")
	post := func(title string, blocks []any) string {
		t.Helper()
		refs := []string{"thread:" + f.thread, subject}
		payload := map[string]any{
			"kind": "ask", "title": title, "subject_ref": subject,
			"requester_actor_id": f.agent.ActorID, "response_proposals": []any{"Ship it", "Wait"},
			"delivery": "none",
		}
		if len(blocks) > 0 {
			payload["blocks"] = blocks
			for _, block := range blocks {
				refs = append(refs, fmt.Sprint(block))
			}
		}
		out := f.call(http.MethodPost, "/events", f.agent.AccessToken, map[string]any{
			"actor_id": f.agent.ActorID,
			"event": map[string]any{
				"type": "human_attention_requested", "thread_id": f.thread, "refs": refs, "summary": title,
				"payload":    payload,
				"provenance": map[string]any{"sources": []any{"inferred"}},
			},
		}, http.StatusCreated)
		return anyString(out["event"].(map[string]any)["id"])
	}
	first := post("Curation one", []any{blocked})
	second := post("Curation two", nil)
	batches := candidatesByAction(f.candidates(f.human.AccessToken), "batch")
	if len(batches) != 1 {
		t.Fatalf("want one batch candidate: %#v", batches)
	}
	created := f.call(http.MethodPost, "/curation/proposals", f.agent.AccessToken, map[string]any{
		"action": "batch", "targets": batches[0]["targets"], "rationale": "Same subject; decide together.",
	}, http.StatusCreated)
	proposal := created["proposal"].(map[string]any)
	key := anyString(proposal["key"])
	f.respond(f.thread, anyString(proposal["event_id"]), "Batch", "answered")

	decisions := f.briefDecisions(f.human.AccessToken)
	decision := map[string]any{}
	for _, d := range decisions {
		if d["batch"] != nil {
			decision = d
		}
	}
	wantID := primitives.CurationBatchInboxItemID(key)
	if anyString(decision["inbox_item_id"]) != wantID {
		t.Fatalf("curation respond id = %#v, want %s in %#v", decision["inbox_item_id"], wantID, decisions)
	}
	members, _ := decision["batch"].(map[string]any)["members"].([]any)
	if len(members) != 2 {
		t.Fatalf("members: %#v", decision["batch"])
	}
	sawBlock := false
	for _, raw := range members {
		member, _ := raw.(map[string]any)
		if anyString(member["inbox_item_id"]) == "" || anyString(member["inbox_item_id"]) == wantID {
			t.Fatalf("member id %#v", member)
		}
		if anyString(member["subject_ref"]) != subject {
			t.Fatalf("subject_ref = %#v", member["subject_ref"])
		}
		for _, block := range anySlice(member["blocks"]) {
			if anyString(asMap(block)["ref"]) == blocked {
				sawBlock = true
			}
		}
	}
	if !sawBlock {
		t.Fatalf("member blocks missing %s: %#v", blocked, members)
	}

	body := map[string]any{
		"idempotency_key": "curation-batch-answer",
		"response_text":   "Ship it",
		"outcome":         "answered",
		"notify_mode":     "none",
	}
	answerAndReplayBatch(t, f.env.server.URL, f.human.AccessToken, f.env.workspace.DB(), wantID, []string{first, second}, body)
	left := 0
	for _, d := range f.briefDecisions(f.human.AccessToken) {
		if strings.HasPrefix(anyString(d["title"]), "Curation ") {
			left++
		}
	}
	if left != 0 {
		t.Fatalf("answered curation members still open")
	}
}

func answerAndReplayBatch(t *testing.T, base, token string, db *sql.DB, inboxID string, askIDs []string, body map[string]any) {
	t.Helper()
	status, result := hostHTTP(t, http.MethodPost, base+"/inbox/"+url.PathEscape(inboxID)+"/respond", token, body)
	if status != http.StatusCreated {
		t.Fatalf("batch respond %s: %d %#v", inboxID, status, result)
	}
	ids := answerEventIDs(t, result)
	if len(ids) != len(askIDs) {
		t.Fatalf("answers = %#v, want %d", result["answers"], len(askIDs))
	}
	notify, _ := result["notify"].(map[string]any)
	if notify["requested"] != false {
		t.Fatalf("notify_mode none still requested a wake: %#v", notify)
	}
	for _, askID := range askIDs {
		if state := askState(t, db, askID); state != "answered" {
			t.Fatalf("ask %s state = %s", askID, state)
		}
		if n := resolutionCount(t, db, askID); n != 1 {
			t.Fatalf("ask %s resolutions = %d", askID, n)
		}
	}

	status, replay := hostHTTP(t, http.MethodPost, base+"/inbox/"+url.PathEscape(inboxID)+"/respond", token, body)
	if status != http.StatusCreated {
		t.Fatalf("batch replay %s: %d %#v", inboxID, status, replay)
	}
	if strings.Join(answerEventIDs(t, replay), ",") != strings.Join(ids, ",") {
		t.Fatalf("replay answers = %#v, want %v", replay["answers"], ids)
	}
	for _, askID := range askIDs {
		if n := resolutionCount(t, db, askID); n != 1 {
			t.Fatalf("replay added a resolution for %s (%d)", askID, n)
		}
	}

	again := map[string]any{}
	for k, v := range body {
		again[k] = v
	}
	again["idempotency_key"] = anyString(body["idempotency_key"]) + "-other"
	status, conflict := hostHTTP(t, http.MethodPost, base+"/inbox/"+url.PathEscape(inboxID)+"/respond", token, again)
	if status != http.StatusConflict {
		t.Fatalf("second answer %s: %d %#v", inboxID, status, conflict)
	}
}

func oldestBatchEvent(t *testing.T, raw any) (string, []string) {
	t.Helper()
	type member struct {
		id string
		ts time.Time
	}
	var members []member
	var ids []string
	for _, item := range anySlice(raw) {
		ev := asMap(item)
		id := anyString(ev["id"])
		if id == "" {
			t.Fatalf("batch event missing id: %#v", ev)
		}
		ids = append(ids, id)
		ts, _ := time.Parse(time.RFC3339Nano, anyString(ev["ts"]))
		members = append(members, member{id, ts})
	}
	sort.Slice(members, func(i, j int) bool {
		if !members[i].ts.Equal(members[j].ts) {
			return members[i].ts.Before(members[j].ts)
		}
		return members[i].id < members[j].id
	})
	if len(members) == 0 {
		t.Fatal("batch has no events")
	}
	return members[0].id, ids
}

func oneBatchDecision(t *testing.T, brief map[string]any) map[string]any {
	t.Helper()
	var found map[string]any
	for _, raw := range anySlice(brief["decisions"]) {
		d := asMap(raw)
		if d["batch"] == nil {
			continue
		}
		if found != nil {
			t.Fatalf("more than one batch decision: %#v", brief["decisions"])
		}
		found = d
	}
	if found == nil {
		t.Fatalf("no batch decision in %#v", brief["decisions"])
	}
	return found
}

func answerEventIDs(t *testing.T, result map[string]any) []string {
	t.Helper()
	ids := []string{}
	for _, raw := range anySlice(result["answers"]) {
		answer := asMap(raw)
		event := asMap(answer["event"])
		id := anyString(event["id"])
		if id == "" {
			t.Fatalf("answer missing event id: %#v", answer)
		}
		if anyString(asMap(event["payload"])["response_text"]) != "Ship it" {
			t.Fatalf("response_text = %#v", event["payload"])
		}
		ids = append(ids, id)
	}
	return ids
}

func askState(t *testing.T, db *sql.DB, askID string) string {
	t.Helper()
	var state string
	if err := db.QueryRow(`SELECT state FROM brief_asks WHERE ask_id=?`, askID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

func resolutionCount(t *testing.T, db *sql.DB, askID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM human_attention_request_resolutions WHERE request_event_id=?`, askID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func asMap(raw any) map[string]any {
	m, _ := raw.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

func anySlice(raw any) []any {
	switch typed := raw.(type) {
	case []any:
		return typed
	default:
		return nil
	}
}
