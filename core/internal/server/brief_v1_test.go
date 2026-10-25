package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testsql"
)

func readBriefV1(t *testing.T, store *primitives.Store) map[string]any {
	t.Helper()
	// The maintenance loop folds queued cards; drain it as the loop would.
	for drained := false; !drained; {
		var err error
		if drained, err = store.RefreshBriefInitiatives(context.Background(), 200); err != nil {
			t.Fatal(err)
		}
	}
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
	askEvt, err := store.AppendEvent(ctx, "agent-1", map[string]any{"type": "human_attention_requested", "thread_id": battery["thread_id"], "refs": []string{"thread:" + anyString(battery["thread_id"])}, "summary": "Merge calibration?",
		"payload": map[string]any{"kind": "ask", "title": "Merge calibration?", "subject_ref": battery["ref"], "requester_actor_id": "agent-1", "response_proposals": []any{"Merge", "Hold"},
			"by": "2026-01-01", "blocks": []any{report["ref"], done["ref"]}, "priority": "p1"}})
	if err != nil {
		t.Fatal(err)
	}
	proof, err := store.AppendEvent(ctx, "actor-1", map[string]any{"type": "message_posted", "thread_id": done["thread_id"], "refs": []string{anyString(done["ref"]), "thread:" + anyString(done["thread_id"])}, "payload": map[string]any{"text": "shipped"}})
	if err != nil {
		t.Fatal(err)
	}
	resolution, resolutionRefs := "done", []string{"event:" + anyString(proof["id"])}
	if _, err = store.MoveBoardCard(ctx, "actor-1", anyString(board["id"]), anyString(done["id"]), primitives.MoveBoardCardInput{ColumnKey: "done", Resolution: &resolution, ResolutionRefs: &resolutionRefs}); err != nil {
		t.Fatalf("move done card: %v", err)
	}
	parked := work("Parked")
	if err = store.SetCardPlan(ctx, "actor-1", anyString(parked["id"]), anyString(parked["updated_at"]), plans.Plan{Steps: []plans.Step{{ID: "ship", Title: "Ship", Ref: anyString(done["ref"]), After: []string{}}}}); err != nil {
		t.Fatal(err)
	}
	body := readBriefV1(t, store)
	if body["schema"] != "brief.v1" {
		t.Fatal(body)
	}
	assertBriefMatchesFixture(t, body)
	decisions := body["decisions"].([]any)
	if len(decisions) != 1 {
		t.Fatalf("decisions = %v", decisions)
	}
	d := decisions[0].(map[string]any)
	if d["event_ref"] != "event:"+anyString(askEvt["id"]) {
		t.Fatalf("decision event_ref = %v, want event:%s", d["event_ref"], anyString(askEvt["id"]))
	}
	if d["decide_by"] != "2026-01-01T23:59:59Z" || d["overdue_for_seconds"].(float64) <= 0 || d["priority"] != "p1" || d["kind"] != "ask" {
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
	if p := states["Parked"]; p["placement"] != "backlog" || p["possibly_moot"].(map[string]any)["flagged"] != true {
		t.Fatalf("parked = %v", p)
	}
	if f, ok := states["Finished"]; ok && f["state"] != "done" {
		t.Fatalf("finished = %v", f)
	}
	if d["blocks_count"].(float64) != 1 {
		t.Fatalf("terminal blocks ref should clear: %v", d)
	}
	// Bounded: query count does not grow with the workspace.
	readBriefV1(t, store)
	counter.Reset()
	readBriefV1(t, store)
	small := counter.Count()
	for i := 0; i < 30; i++ {
		w := work(fmt.Sprintf("Extra %d", i))
		note(w, map[string]any{"next": map[string]any{"at": "3d", "what": "x"}})
	}
	for drained := false; !drained; {
		if drained, err = store.RefreshBriefInitiatives(ctx, 200); err != nil {
			t.Fatal(err)
		}
	}
	counter.Reset()
	readBriefV1(t, store)
	if large := counter.Count(); large > small+2 {
		t.Fatalf("brief queries grew with the workspace: %d -> %d", small, large)
	}
}

func TestBriefV1AgentInitiativeOwnership(t *testing.T) {
	t.Parallel()
	h := newPrimitivesTestServer(t)
	ctx := context.Background()
	db, _ := testsql.Open("file:" + h.workspace.Layout().DatabasePath)
	t.Cleanup(func() { db.Close() })
	store := primitives.NewTestStore(db, h.workspace.Layout().ArtifactContentDir)
	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{"title": "Portfolio"})
	if err != nil {
		t.Fatal(err)
	}
	boardID := anyString(board["id"])
	assigned, err := store.CreateWork(ctx, "actor-1", boardID, map[string]any{"title": "Assigned", "owner": "actor:agent-1"})
	if err != nil {
		t.Fatal(err)
	}
	promised, err := store.CreateWork(ctx, "actor-1", boardID, map[string]any{"title": "Promised"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(ctx, "agent-1", map[string]any{
		"type": "message_posted", "thread_id": promised["thread_id"],
		"refs":    []string{anyString(promised["ref"]), "thread:" + anyString(promised["thread_id"])},
		"payload": map[string]any{"text": "on it", "next": map[string]any{"at": "2d", "what": "land PR"}},
	}); err != nil {
		t.Fatal(err)
	}
	unrelated, err := store.CreateWork(ctx, "actor-1", boardID, map[string]any{"title": "Unrelated", "owner": "actor:agent-2"})
	if err != nil {
		t.Fatal(err)
	}
	participating, err := store.CreateWork(ctx, "actor-1", boardID, map[string]any{"title": "Participating"})
	if err != nil {
		t.Fatal(err)
	}
	for _, sqlStmt := range []string{
		`INSERT INTO agents(id,username,actor_id,created_at,updated_at,metadata_json) VALUES('ag-1','worker-1','agent-1','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','{}'),('ag-2','worker-2','agent-2','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','{}')`,
		`INSERT INTO agent_sessions(id,agent_id,actor_id,provider,host_scope,native_session_id,native_session_id_kind,capabilities_json,activity,sequence,request_json,created_at,last_seen_at,expires_at) VALUES('s-ag-1','ag-1','agent-1','codex','h1','n1','opaque','{"resume":"unsupported","history":"unsupported","logs":"unsupported"}','active',1,'{}','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','2026-12-31T00:00:00Z'),('s-ag-2','ag-2','agent-2','codex','h1','n2','opaque','{"resume":"unsupported","history":"unsupported","logs":"unsupported"}','active',1,'{}','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z','2026-12-31T00:00:00Z')`,
	} {
		if _, err := h.workspace.DB().ExecContext(ctx, sqlStmt); err != nil {
			t.Fatal(err)
		}
	}
	seq := int64(1)
	if _, err := store.UpsertWorkParticipant(ctx, "ag-1", anyString(participating["id"]), primitives.WorkParticipantRegistration{SessionID: "s-ag-1", Activity: "active", Sequence: &seq}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertWorkParticipant(ctx, "ag-2", anyString(unrelated["id"]), primitives.WorkParticipantRegistration{SessionID: "s-ag-2", Activity: "active", Sequence: &seq}); err != nil {
		t.Fatal(err)
	}
	for drained := false; !drained; {
		if drained, err = store.RefreshBriefInitiatives(ctx, 200); err != nil {
			t.Fatal(err)
		}
	}
	brief, err := store.Brief(ctx, primitives.BriefReader{ActorID: "agent-1", Kind: "agent", Display: "worker"}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	yoursByRef := map[string]bool{}
	owners := map[string]any{}
	for _, raw := range brief["initiatives"].([]map[string]any) {
		yoursByRef[anyString(raw["ref"])] = raw["yours"] == true
		owners[anyString(raw["ref"])] = raw["owner"]
	}
	if owners[anyString(assigned["ref"])] != "actor:agent-1" || owners[anyString(unrelated["ref"])] != "actor:agent-2" || owners[anyString(promised["ref"])] != "actor:actor-1" || owners[anyString(participating["ref"])] != "actor:actor-1" {
		t.Fatalf("owner must be the explicit or implicit assignee, not promise author: %#v", owners)
	}
	if !yoursByRef[anyString(assigned["ref"])] {
		t.Fatalf("assigned card should have yours=true: %#v", brief["initiatives"])
	}
	if !yoursByRef[anyString(promised["ref"])] {
		t.Fatalf("promised card should have yours=true: %#v", brief["initiatives"])
	}
	if !yoursByRef[anyString(participating["ref"])] {
		t.Fatalf("participating card should have yours=true: %#v", brief["initiatives"])
	}
	if yoursByRef[anyString(unrelated["ref"])] {
		t.Fatalf("unrelated card must not have yours=true: %#v", brief["initiatives"])
	}
}

// I7 (core side): the live brief uses only fields the shared fixture that
// Decide, Watch and orient render from also uses, plus the optional ones.
func assertBriefMatchesFixture(t *testing.T, body map[string]any) {
	t.Helper()
	raw, err := os.ReadFile("../../../contracts/fixtures/brief/brief.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture map[string]any
	if err = json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	optional := map[string]bool{"body": true, "priority": true, "subject": true, "curation": true, "pm_decision": true, "outcome": true, "proposed": true, "yours": true}
	for key := range body {
		if _, ok := fixture[key]; !ok {
			t.Errorf("brief field %q is not in the shared fixture", key)
		}
	}
	for _, section := range []string{"decisions", "initiatives", "daily_digest", "your_asks", "answered_for_you"} {
		known := map[string]bool{}
		for _, item := range fixture[section].([]any) {
			for k := range item.(map[string]any) {
				known[k] = true
			}
		}
		if section == "your_asks" {
			for _, item := range fixture["decisions"].([]any) {
				for k := range item.(map[string]any) {
					known[k] = true
				}
			}
		}
		items, _ := body[section].([]any)
		for _, item := range items {
			for k := range item.(map[string]any) {
				if !known[k] && !optional[k] {
					t.Errorf("%s field %q is not in the shared fixture", section, k)
				}
			}
		}
	}
}

func TestBriefV1UndatedAskCandidatePriorityAndAgentSummaryRecency(t *testing.T) {
	t.Parallel()
	h := newPrimitivesTestServer(t)
	ctx := context.Background()
	store := h.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{"title": "Portfolio"})
	if err != nil {
		t.Fatal(err)
	}
	subjectCard, err := store.CreateWork(ctx, "actor-1", anyString(board["id"]), map[string]any{"title": "Subject Card"})
	if err != nil {
		t.Fatal(err)
	}
	blockedCard, err := store.CreateWork(ctx, "actor-1", anyString(board["id"]), map[string]any{"title": "Blocked Initiative"})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Seed 65 older low-priority undated asks that each reference a completed blocker card
	// (testing both asks filed before completion and asks filed after completion).
	completedBeforeAsk, err := store.CreateWork(ctx, "actor-1", anyString(board["id"]), map[string]any{"title": "Completed Before Ask"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.workspace.DB().Exec(`UPDATE cards SET column_key='done' WHERE id=?`, completedBeforeAsk["id"]); err != nil {
		t.Fatal(err)
	}
	completedAfterAsk, err := store.CreateWork(ctx, "actor-1", anyString(board["id"]), map[string]any{"title": "Completed After Ask"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 65; i++ {
		title := fmt.Sprintf("Old stale-blocker ask %02d", i)
		staleRef := completedBeforeAsk["ref"]
		if i%2 == 1 {
			staleRef = completedAfterAsk["ref"]
		}
		if _, err := store.AppendEvent(ctx, "agent-1", map[string]any{
			"type":      "human_attention_requested",
			"thread_id": subjectCard["thread_id"],
			"refs":      []string{"thread:" + anyString(subjectCard["thread_id"])},
			"summary":   title,
			"payload": map[string]any{
				"kind":               "ask",
				"title":              title,
				"subject_ref":        subjectCard["ref"],
				"requester_actor_id": "agent-1",
				"priority":           "low",
				"blocks":             []any{staleRef},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.workspace.DB().Exec(`UPDATE cards SET column_key='done' WHERE id=?`, completedAfterAsk["id"]); err != nil {
		t.Fatal(err)
	}

	// Seed newer critical/blocking and high-priority undated asks after the 65 stale-blocker asks.
	if _, err := store.AppendEvent(ctx, "agent-1", map[string]any{
		"type":      "human_attention_requested",
		"thread_id": subjectCard["thread_id"],
		"refs":      []string{"thread:" + anyString(subjectCard["thread_id"])},
		"summary":   "Urgent non-blocking ask",
		"payload": map[string]any{
			"kind":               "ask",
			"title":              "Urgent non-blocking ask",
			"subject_ref":        subjectCard["ref"],
			"requester_actor_id": "agent-1",
			"priority":           "critical",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AppendEvent(ctx, "agent-1", map[string]any{
		"type":      "human_attention_requested",
		"thread_id": subjectCard["thread_id"],
		"refs":      []string{"thread:" + anyString(subjectCard["thread_id"])},
		"summary":   "Critical blocking ask",
		"payload": map[string]any{
			"kind":               "ask",
			"title":              "Critical blocking ask",
			"subject_ref":        subjectCard["ref"],
			"requester_actor_id": "agent-1",
			"priority":           "low",
			"blocks":             []any{blockedCard["ref"]},
		},
	}); err != nil {
		t.Fatal(err)
	}

	body := readBriefV1(t, store)
	decisions := body["decisions"].([]any)
	if len(decisions) != primitives.BriefSectionLimit {
		t.Fatalf("decisions len = %d, want %d", len(decisions), primitives.BriefSectionLimit)
	}
	firstTitle := anyString(decisions[0].(map[string]any)["title"])
	secondTitle := anyString(decisions[1].(map[string]any)["title"])
	if firstTitle != "Critical blocking ask" || secondTitle != "Urgent non-blocking ask" {
		t.Fatalf("top decisions = [%q, %q], want [\"Critical blocking ask\", \"Urgent non-blocking ask\"]", firstTitle, secondTitle)
	}

	// 2. Seed 60 assigned cards where lexicographically larger IDs have newer updated_at timestamps.
	for i := 0; i < 60; i++ {
		cardID := fmt.Sprintf("card-order-%02d", i)
		updatedAt := fmt.Sprintf("2026-10-16T00:%02d:00Z", i)
		if _, err := h.workspace.DB().Exec(
			`INSERT INTO cards(id,handle,board_id,rank,title,summary,column_key,assignee,created_at,created_by,updated_at,updated_by) VALUES(?,?,?,'',?,'','in_progress','agent-recency',?,'actor-1',?,'actor-1')`,
			cardID, cardID, board["id"], cardID, updatedAt, updatedAt,
		); err != nil {
			t.Fatal(err)
		}
	}
	summaryCards, truncated, err := store.AgentSummaryCards(ctx, "actor:agent-recency", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated || len(summaryCards) != 50 {
		t.Fatalf("AgentSummaryCards len=%d truncated=%v, want len=50 truncated=true", len(summaryCards), truncated)
	}
	if got := anyString(summaryCards[0]["id"]); got != "card-order-59" {
		t.Fatalf("first summary card = %q, want card-order-59 (most recently updated)", got)
	}
	if got := anyString(summaryCards[len(summaryCards)-1]["id"]); got != "card-order-10" {
		t.Fatalf("last summary card = %q, want card-order-10", got)
	}
}

// I7 (core side): the live brief uses only fields the shared fixture that
// Decide, Watch and orient render from also uses, plus the optional ones.

// The shared wire fixture must cover normal defaults and both daily routes,
// rather than merely accepting an empty digest from an ordinary brief.
func TestBriefTrackingDefaultsMatchSharedFixture(t *testing.T) {
	t.Parallel()
	h := newPrimitivesTestServer(t)
	ctx := context.Background()
	store := h.primitiveStore.(*primitives.Store)
	create := func(title string) map[string]any {
		t.Helper()
		card, err := store.CreateWork(ctx, "actor-1", "", map[string]any{"title": title})
		if err != nil {
			t.Fatal(err)
		}
		return card
	}
	normal := create("Default checkpoint")
	lapsed := create("Lapsed default")
	ended := create("Ended lease")
	now := time.Now().UTC()
	created := now.Add(-13 * time.Hour)
	if _, err := h.workspace.DB().Exec(`UPDATE cards SET created_at=? WHERE id=?`, created.Format(time.RFC3339Nano), lapsed["id"]); err != nil {
		t.Fatal(err)
	}
	if _, err := h.workspace.DB().Exec(`UPDATE work_tracking SET default_at=? WHERE card_id=?`, created.Add(12*time.Hour).Format(time.RFC3339Nano), lapsed["id"]); err != nil {
		t.Fatal(err)
	}
	if err := store.MaintainWorkTracking(ctx, now); err != nil {
		t.Fatal(err)
	}
	store.SetTrackingLead(func() string { return "actor-1" })
	if _, err := store.AcquireCardLease(ctx, "actor-1", anyString(ended["id"]), primitives.CardLeaseAcquireInput{Owner: "actor:actor-1", TTL: time.Hour}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ReleaseCardLease(ctx, "actor-1", anyString(ended["id"]), primitives.CardLeaseReleaseInput{Owner: "actor:actor-1"}); err != nil {
		t.Fatal(err)
	}
	for done := false; !done; {
		var err error
		done, err = store.RefreshBriefInitiatives(ctx, 200)
		if err != nil {
			t.Fatal(err)
		}
	}
	raw, err := store.Brief(ctx, primitives.BriefReader{ActorID: "actor-1", Kind: "human"}, now)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err = json.Unmarshal(encoded, &body); err != nil {
		t.Fatal(err)
	}
	assertBriefMatchesFixture(t, body)
	initiatives := body["initiatives"].([]any)
	if len(initiatives) != 1 {
		t.Fatalf("digest work leaked into normal initiatives: %+v", initiatives)
	}
	row := initiatives[0].(map[string]any)
	promise := row["promise"].(map[string]any)
	if row["ref"] != normal["ref"] || row["owner"] != "actor:actor-1" || row["state"] != "moving" || promise["source"] != "default" {
		t.Fatalf("default contract lost: %+v", row)
	}
	reasons := map[string]string{}
	for _, value := range body["daily_digest"].([]any) {
		row := value.(map[string]any)
		reasons[anyString(row["ref"])] = anyString(row["reason"])
	}
	if len(reasons) != 2 || reasons[anyString(lapsed["ref"])] != "default_promise_lapsed" || reasons[anyString(ended["ref"])] != "awaiting_owner" {
		t.Fatalf("digest contract lost: %+v", reasons)
	}
}
