package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testsql"
)

func readBriefV1(t *testing.T, store *primitives.Store) map[string]any {
	t.Helper()
	out := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/brief", nil)
	attachResourceAccessScope(req, handlerOptions{primitiveStore: store})
	handleGetBriefV1(out, req, handlerOptions{primitiveStore: store})
	if out.Code != 200 {
		t.Fatal(out.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(out.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestBriefV1PromisesAsksAndBoundedReads(t *testing.T) {
	t.Parallel()
	h := newPrimitivesTestServer(t)
	ctx := context.Background()
	db, counter := testsql.Open("file:" + h.workspace.Layout().DatabasePath)
	t.Cleanup(func() { db.Close() })
	store := primitives.NewTestStore(db, h.workspace.Layout().ArtifactContentDir)
	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{"title": "Portfolio"})
	if err != nil {
		t.Fatal(err)
	}
	work := func(title string) map[string]any {
		w, err := store.CreateWork(ctx, "actor-1", anyString(board["id"]), map[string]any{"title": title})
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	battery, report, done := work("Battery"), work("Report"), work("Finished")
	note := func(card map[string]any, payload map[string]any) {
		payload["text"] = "progress"
		if _, err := store.AppendEvent(ctx, "agent-1", map[string]any{"type": "message_posted", "thread_id": card["thread_id"], "refs": []string{anyString(card["ref"]), "thread:" + anyString(card["thread_id"])}, "payload": payload}); err != nil {
			t.Fatal(err)
		}
	}
	note(battery, map[string]any{"next": map[string]any{"at": "2026-01-01", "what": "bench data"}})
	note(battery, map[string]any{"next": map[string]any{"at": "2026-01-02", "what": "bench data"}})
	note(report, map[string]any{"next": map[string]any{"at": "30d", "what": "draft"}, "evidence_refs": []string{"https://example.test/pr/1"}})
	if _, err = store.AppendEvent(ctx, "agent-1", map[string]any{"type": "human_attention_requested", "thread_id": battery["thread_id"], "refs": []string{"thread:" + anyString(battery["thread_id"])}, "summary": "Merge calibration?",
		"payload": map[string]any{"kind": "ask", "title": "Merge calibration?", "subject_ref": battery["ref"], "requester_actor_id": "agent-1", "response_proposals": []any{"Merge", "Hold"},
			"by": "2026-01-01", "blocks": []any{report["ref"], done["ref"]}, "priority": "p1"}}); err != nil {
		t.Fatal(err)
	}
	if _, err = store.MoveBoardCard(ctx, "actor-1", anyString(board["id"]), anyString(done["id"]), primitives.MoveBoardCardInput{ColumnKey: "done"}); err != nil {
		t.Fatalf("move done card: %v", err)
	}
	body := readBriefV1(t, store)
	if body["schema"] != "brief.v1" {
		t.Fatal(body)
	}
	decisions := body["decisions"].([]any)
	if len(decisions) != 1 {
		t.Fatalf("decisions = %v", decisions)
	}
	d := decisions[0].(map[string]any)
	if d["decide_by"] != "2026-01-01T00:00:00Z" || d["overdue_for_seconds"].(float64) <= 0 || d["priority"] != "p1" || d["kind"] != "ask" {
		t.Fatalf("decision = %v", d)
	}
	if d["proposals"].([]any)[0] != "Merge" || d["subject"].(map[string]any)["title"] != "Battery" {
		t.Fatalf("decision = %v", d)
	}
	states := map[string]map[string]any{}
	for _, raw := range body["initiatives"].([]any) {
		item := raw.(map[string]any)
		states[anyString(item["title"])] = item
	}
	b := states["Battery"]
	if b["state"] != "past_promise" || b["promise_moves"].(float64) != 1 || b["promise"].(map[string]any)["source"] != "note" || b["waiting_on_you_count"].(float64) != 1 {
		t.Fatalf("battery = %v", b)
	}
	r := states["Report"]
	if r["state"] != "moving" || r["last_evidence_at"] == nil || r["blocked_count"].(float64) != 1 {
		t.Fatalf("report = %v", r)
	}
	if f, ok := states["Finished"]; ok && f["state"] != "done" {
		t.Fatalf("finished = %v", f)
	}
	if d["blocks_count"].(float64) != 1 {
		t.Fatalf("terminal blocks ref should clear: %v", d)
	}
	// Bounded: query count does not grow with the workspace.
	counter.Reset()
	readBriefV1(t, store)
	small := counter.Count()
	for i := 0; i < 30; i++ {
		w := work(fmt.Sprintf("Extra %d", i))
		note(w, map[string]any{"next": map[string]any{"at": "3d", "what": "x"}})
	}
	readBriefV1(t, store) // drains the fold queue
	counter.Reset()
	readBriefV1(t, store)
	if large := counter.Count(); large > small+2*primitives.BriefSectionLimit+4 {
		t.Fatalf("brief queries grew with the workspace: %d -> %d", small, large)
	}
}
