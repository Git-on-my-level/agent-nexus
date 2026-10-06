package server

import (
	"agent-nexus-core/internal/auth"
	reports "agent-nexus-visualreport"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

func TestInboxDetailAndComposedWorkspacesAuthorizeSubjects(t *testing.T) {
	if testing.Short() {
		t.Skip("HTTP/storage integration")
	}
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "composed-owner", "composed-owner", "Owner", "composed-owner-token")
	reader := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "composed-reader", "composed-reader", "Reader", "composed-reader-token")
	store := env.primitiveStore.(*primitives.Store)
	topic, err := store.CreateTopic(ctx, owner.ActorID, map[string]any{"title": "Public topic", "summary": "Public context"})
	if err != nil {
		t.Fatal(err)
	}
	threadID := anyString(topic.Topic["thread_id"])
	board, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Private board"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateWork(ctx, owner.ActorID, anyString(board["id"]), map[string]any{"title": "Confidential card"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, owner.ActorID, anyString(board["thread_id"]), map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	items := []primitives.DerivedInboxItem{
		{ID: "visible-composed", ThreadID: threadID, Category: "ask", TriggerAt: now, Data: map[string]any{"kind": "ask", "title": "Visible ask", "subject_ref": topic.Topic["ref"]}},
		{ID: "hidden-composed", ThreadID: threadID, Category: "ask", TriggerAt: now, Data: map[string]any{"kind": "ask", "title": "Confidential ask", "body": "Confidential body", "subject_ref": card["ref"], "related_refs": []any{card["ref"]}}},
	}
	if err = store.ReplaceDerivedInboxItems(ctx, threadID, items); err != nil {
		t.Fatal(err)
	}
	if err = store.PutDerivedTopicProjection(ctx, primitives.DerivedTopicProjection{ThreadID: threadID, GeneratedAt: now, InboxCount: 2, PendingDecisionCount: 2, Data: map[string]any{"thread_id": threadID, "inbox_count": 2, "pending_decision_count": 2}}); err != nil {
		t.Fatal(err)
	}
	for _, who := range []struct {
		token string
		count int
	}{{reader.AccessToken, 1}, {owner.AccessToken, 2}} {
		for _, path := range []string{"/threads/" + threadID + "/workspace", "/topics/" + anyString(topic.Topic["id"]) + "/workspace"} {
			resp := getJSONExpectStatusWithAuth(t, env.server.URL+path, who.token, 200)
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			var body map[string]any
			if err = json.Unmarshal(raw, &body); err != nil {
				t.Fatal(err)
			}
			if who.count == 1 {
				for _, secret := range []string{"Confidential", anyString(card["ref"]), "hidden-composed"} {
					if strings.Contains(string(raw), secret) {
						t.Fatalf("%s leaked %s: %s", path, secret, raw)
					}
				}
			}
			if strings.HasPrefix(path, "/threads/") {
				inbox := body["inbox"].(map[string]any)
				summary := body["workspace_summary"].(map[string]any)
				attention := body["pending_attention"].(map[string]any)
				for name, value := range map[string]any{"inbox": inbox["count"], "summary inbox": summary["inbox_count"], "summary pending": summary["pending_decision_count"], "pending": attention["count"], "review": body["total_review_items"]} {
					if value != float64(who.count) {
						t.Fatalf("%s %s=%v want %d: %s", path, name, value, who.count, raw)
					}
				}
			} else if len(body["inbox"].([]any)) != who.count {
				t.Fatalf("topic count differs: %s", raw)
			}
		}
	}
	for _, path := range []string{"/inbox/hidden-composed"} {
		resp := getJSONExpectStatusWithAuth(t, env.server.URL+path, reader.AccessToken, 404)
		resp.Body.Close()
		resp = getJSONExpectStatusWithAuth(t, env.server.URL+path, owner.AccessToken, 200)
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if !strings.Contains(string(raw), "Confidential body") {
			t.Fatal(string(raw))
		}
	}
	resp := getJSONExpectStatusWithAuth(t, env.server.URL+"/inbox/visible-composed", reader.AccessToken, 200)
	resp.Body.Close()
	// Canonical projection remains complete for its owner and later readers.
	projection, err := store.GetDerivedTopicProjection(ctx, threadID)
	if err != nil || projection.InboxCount != 2 {
		t.Fatalf("canonical summary changed: %+v %v", projection, err)
	}
}

func TestInboxFreshnessLoadsOnlyAccessibleBackingThreads(t *testing.T) {
	if testing.Short() {
		t.Skip("HTTP/storage integration")
	}
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "fresh-owner", "fresh-owner", "Owner", "fresh-owner-token")
	reader := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "fresh-reader", "fresh-reader", "Reader", "fresh-reader-token")
	store := env.primitiveStore.(*primitives.Store)
	public, err := store.CreateThread(ctx, owner.ActorID, map[string]any{"title": "Public freshness"})
	if err != nil {
		t.Fatal(err)
	}
	board, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Private freshness"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateWork(ctx, owner.ActorID, anyString(board["id"]), map[string]any{"title": "Private freshness card"})
	if err != nil {
		t.Fatal(err)
	}
	boardThread, cardThread := anyString(board["thread_id"]), anyString(card["thread_id"])
	if _, err = store.PatchThread(ctx, owner.ActorID, boardThread, map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"archived_at", "trashed_at"} {
		inactive, e := store.CreateThread(ctx, owner.ActorID, map[string]any{"title": "Inactive freshness"})
		if e != nil {
			t.Fatal(e)
		}
		id := anyString(inactive.Thread["id"])
		if _, e = env.workspace.DB().Exec(`UPDATE threads SET `+field+`=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), id); e != nil {
			t.Fatal(e)
		}
		if e = store.PutDerivedTopicProjection(ctx, primitives.DerivedTopicProjection{ThreadID: id, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), Data: map[string]any{}}); e != nil {
			t.Fatal(e)
		}
		if _, e = env.workspace.DB().Exec(`UPDATE derived_topic_views SET data_json='broken inactive JSON' WHERE thread_id=?`, id); e != nil {
			t.Fatal(e)
		}
	}
	now := time.Now().UTC()
	for _, id := range []string{anyString(public.Thread["id"]), boardThread, cardThread} {
		if err = store.PutDerivedTopicProjection(ctx, primitives.DerivedTopicProjection{ThreadID: id, GeneratedAt: now.Format(time.RFC3339Nano), Data: map[string]any{}}); err != nil {
			t.Fatal(err)
		}
	}
	if err = store.MarkTopicProjectionsDirty(ctx, []string{boardThread, cardThread}, now); err != nil {
		t.Fatal(err)
	}
	// A corrupt private projection proves authorization precedes payload loading.
	if _, err = env.workspace.DB().Exec(`UPDATE derived_topic_views SET data_json='broken private JSON' WHERE thread_id=?`, cardThread); err != nil {
		t.Fatal(err)
	}
	resp := getJSONExpectStatusWithAuth(t, env.server.URL+"/inbox", reader.AccessToken, 200)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var body map[string]any
	if err = json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	fresh := body["projection_freshness"].(map[string]any)
	if fresh["status"] != "current" || fresh["thread_count"] != float64(1) {
		t.Fatalf("private freshness affects public aggregate: %s", raw)
	}
	for _, id := range []string{boardThread, cardThread} {
		if strings.Contains(string(raw), id) {
			t.Fatalf("private thread leaked: %s", raw)
		}
	}
	if _, err = env.workspace.DB().Exec(`UPDATE derived_topic_views SET data_json='{}' WHERE thread_id=?`, cardThread); err != nil {
		t.Fatal(err)
	}
	resp = getJSONExpectStatusWithAuth(t, env.server.URL+"/inbox", owner.AccessToken, 200)
	raw, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if err = json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	fresh = body["projection_freshness"].(map[string]any)
	if fresh["status"] != "pending" || fresh["thread_count"] != float64(3) {
		t.Fatalf("owner freshness missing: %s", raw)
	}
}

func TestOverviewAndChangesBatchMoreThan200DigestSubjects(t *testing.T) {
	if testing.Short() {
		t.Skip("HTTP/storage integration")
	}
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	principal := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "digest-reader", "digest-reader", "Reader", "digest-reader-token")
	store := env.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, principal.ActorID, map[string]any{"title": "Digest board"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	refs := []string{}
	tx, err := env.workspace.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 201; i++ {
		id := fmt.Sprintf("digest-subject-%03d", i)
		if _, err = tx.Exec(`INSERT INTO cards(id,board_id,title,created_at,created_by,updated_at,updated_by) VALUES(?,?,'Digest subject',?,'fixture',?,'fixture')`, id, board["id"], now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
			t.Fatal(err)
		}
		refs = append(refs, "card:"+id)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = store.RecordOverviewVisit(ctx, "human:"+principal.AgentID, nil, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	for i, batch := range [][]string{refs[:101], refs[101:]} {
		raw, _ := json.Marshal(batch)
		if _, err = env.workspace.DB().Exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json) VALUES(?,'human_attention_responded',?,'fixture',?,?)`, fmt.Sprintf("digest-answer-%d", i), now.Format(time.RFC3339Nano), board["thread_id"], string(raw)); err != nil {
			t.Fatal(err)
		}
	}
	// GET /overview records a new visit, so exercise /changes first.
	for _, path := range []string{"/overview/changes", "/overview"} {
		resp := getJSONExpectStatusWithAuth(t, env.server.URL+path, principal.AccessToken, 200)
		var body map[string]any
		if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if path == "/overview" {
			body = body["since_you_last_looked"].(map[string]any)
		}
		count := 0
		for _, raw := range body["items"].([]any) {
			if raw.(map[string]any)["kind"] == "ask_answered" {
				count++
			}
		}
		if count != 2 || body["truncated"] != false {
			t.Fatalf("digest batching failed for %s: %+v", path, body)
		}
	}
}

func TestReportEventBudgetsAuthorizePayloadSubjectsAndRelatedRefs(t *testing.T) {
	if testing.Short() {
		t.Skip("HTTP/storage integration")
	}
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "report-budget-owner", "report-budget-owner", "Owner", "report-budget-owner-token")
	principal := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "report-budget-reader", "report-budget-reader", "Reader", "report-budget-reader-token")
	store := env.primitiveStore.(*primitives.Store)
	thread, err := store.CreateThread(ctx, owner.ActorID, map[string]any{"title": "Public asks"})
	if err != nil {
		t.Fatal(err)
	}
	tid := anyString(thread.Thread["id"])
	board, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Private report board"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateWork(ctx, owner.ActorID, anyString(board["id"]), map[string]any{"title": "Confidential report card"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, owner.ActorID, anyString(board["thread_id"]), map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	appendAsk := func(id, subject, title string, related []string) map[string]any {
		t.Helper()
		e, err := store.AppendEvent(ctx, owner.ActorID, map[string]any{"id": id, "type": "human_attention_requested", "thread_id": tid, "refs": []string{"thread:" + tid}, "payload": map[string]any{"kind": "ask", "requester_actor_id": owner.ActorID, "request_id": id, "title": title, "body": title + " body", "subject_ref": subject, "related_refs": related, "response_proposals": []string{"Proceed", "Wait"}}})
		if err != nil {
			t.Fatal(err)
		}
		return e
	}
	appendAsk("visible-report-ask", "thread:"+tid, "Visible ask", nil)
	hidden := appendAsk("related-report-ask", "thread:"+tid, "Confidential related ask", []string{anyString(card["ref"])})
	template := appendAsk("private-report-ask", anyString(card["ref"]), "Confidential subject ask", nil)
	tx, err := env.workspace.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	// Clone a validated canonical event to exceed the report's 2,000-row cap.
	for i := 0; i < 2001; i++ {
		if _, err = tx.Exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) SELECT ?,type,?,actor_id,thread_id,refs_json,payload_json FROM events WHERE id=?`, fmt.Sprintf("hidden-report-%04d", i), time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano), template["id"]); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/", nil)
	cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: principal.ActorID, AgentID: principal.AgentID, PrincipalKind: string(auth.PrincipalKindHuman)})
	attachResourceAccessScope(req, handlerOptions{primitiveStore: store})
	r := reportReader{r: req, opts: handlerOptions{primitiveStore: store}, now: time.Now(), visibility: map[string]bool{}}
	data, partial, err := r.materialize(reports.Panel{Type: "live-asks", Query: reports.Query{Limit: 10}})
	if err != nil {
		t.Fatal(err)
	}
	rows := data["items"].([]map[string]any)
	if partial || len(rows) != 1 || rows[0]["title"] != "Visible ask" || len(r.events) != 1 {
		t.Fatalf("private events consume report budget: %+v partial=%v events=%d", data, partial, len(r.events))
	}
	raw, _ := json.Marshal(data)
	if strings.Contains(string(raw), "Confidential") || strings.Contains(string(raw), anyString(card["ref"])) {
		t.Fatal(string(raw))
	}
	// Cached/custom-store events retain the same related-reference defense.
	if r.activeEvent(hidden) {
		t.Fatal("cached event ignored private related reference")
	}
	ownerCtx := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: owner.ActorID})
	page, err := store.ListEventsPage(ownerCtx, primitives.EventListFilter{ReportSubjects: true, Types: []string{"human_attention_requested"}, Limit: 1})
	if err != nil || len(page.Events) != 1 || page.NextCursor == "" {
		t.Fatalf("owner lost authorized events: %+v %v", page, err)
	}
}
