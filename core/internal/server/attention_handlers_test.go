package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

func TestAttentionHTTPPrincipalBoundaries(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	human := seedHumanPrincipalForLockoutTest(t, ctx, db, "attention-human", "attention-human-actor", "attention.human", "attention-human-token")
	agent := seedMachinePrincipalForLockoutTest(t, ctx, db, "attention-agent", "attention-agent-actor", "attention.agent", "attention-agent-token")
	other := seedMachinePrincipalForLockoutTest(t, ctx, db, "attention-other", "attention-other-actor", "attention.other", "attention-other-token")
	store := env.primitiveStore.(*primitives.Store)
	work, err := store.CreateWork(ctx, agent.ActorID, "", map[string]any{"title": "Attention target"})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := schema.Load("../../../contracts/anx-schema.yaml")
	if err != nil {
		t.Fatal(err)
	}
	handler := NewHandler("attention-test", WithSchemaContract(contract), WithPrimitiveStore(store), WithAuthStore(auth.NewStore(db)), WithActorRegistry(actors.NewStore(db)))
	call := func(token, method, path string, body any, want int) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		out := httptest.NewRecorder()
		handler.ServeHTTP(out, req)
		if out.Code != want {
			t.Fatalf("%s %s status=%d want=%d body=%s", method, path, out.Code, want, out.Body.String())
		}
		var result map[string]any
		if err = json.Unmarshal(out.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	place := map[string]any{"subject_ref": work["ref"], "for": "actor:" + human.ActorID, "to": "watch", "reason": "Progress worth seeing", "request_key": "http-placement"}
	call("", http.MethodGet, "/attention/items", nil, 401)
	originalPlacement := call(agent.AccessToken, http.MethodPost, "/attention/placements", place, 201)
	if _, err = db.Exec(`UPDATE cards SET title='Renamed attention target' WHERE id=?`, work["id"]); err != nil {
		t.Fatal(err)
	}
	if replay := call(agent.AccessToken, http.MethodPost, "/attention/placements", place, 201); !reflect.DeepEqual(originalPlacement, replay) {
		t.Fatalf("placement response changed after title edit: original=%v replay=%v", originalPlacement, replay)
	}
	place["to"] = "routine"
	call(agent.AccessToken, http.MethodPost, "/attention/placements", place, 409)
	place["to"] = "watch"
	call(human.AccessToken, http.MethodGet, "/attention/items?for=human:"+human.AgentID, nil, 200)
	call(agent.AccessToken, http.MethodGet, "/attention/items?for=agent:"+agent.AgentID, nil, 200)
	call(agent.AccessToken, http.MethodGet, "/attention/items?for=actor:"+human.ActorID, nil, 403)
	call(other.AccessToken, http.MethodGet, "/attention/items?for=actor:"+human.ActorID, nil, 403)
	setting := map[string]any{"attention_agent": "actor:" + agent.ActorID}
	call(agent.AccessToken, http.MethodPut, "/attention/agent", setting, 403)
	call(human.AccessToken, http.MethodPut, "/attention/agent", setting, 200)
	scope := map[string]any{"scope_ref": "builtin:initiatives"}
	call(agent.AccessToken, http.MethodPost, "/attention/visits", scope, 200)
	call(human.AccessToken, http.MethodPost, "/attention/visits", scope, 200)
	var before string
	if err = db.QueryRow(`SELECT visited_at FROM overview_visits WHERE principal_id=? AND scope_ref='builtin:initiatives'`, "actor:"+human.ActorID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	// Attempts to impersonate a human in input never change its marker.
	call(agent.AccessToken, http.MethodPost, "/attention/visits", map[string]any{"scope_ref": "builtin:initiatives", "principal_id": "actor:" + human.ActorID}, 400)
	call(agent.AccessToken, http.MethodGet, "/attention/changes?scope_ref=builtin:initiatives", nil, 200)
	call(agent.AccessToken, http.MethodGet, "/overview", nil, 200)
	var after string
	if err = db.QueryRow(`SELECT visited_at FROM overview_visits WHERE principal_id=? AND scope_ref='builtin:initiatives'`, "actor:"+human.ActorID).Scan(&after); err != nil || after != before {
		t.Fatal("agent moved human marker", before, after, err)
	}
	feedback := map[string]any{"subject_ref": work["ref"], "surface": "watch", "signal": "needed_sooner", "note": "Tell me before the deadline", "request_key": "http-feedback"}
	call(agent.AccessToken, http.MethodPost, "/attention/feedback", feedback, 403)
	a := call(human.AccessToken, http.MethodPost, "/attention/feedback", feedback, 201)
	b := call(human.AccessToken, http.MethodPost, "/attention/feedback", feedback, 201)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("feedback replay changed event")
	}
	feedback["signal"] = "more_like_this"
	call(human.AccessToken, http.MethodPost, "/attention/feedback", feedback, 409)
	feedback["signal"] = "needed_sooner"
	page := call(agent.AccessToken, http.MethodGet, "/attention/feedback?for=actor:"+human.ActorID, nil, 200)
	if len(page["items"].([]any)) != 1 {
		t.Fatal(page)
	}
	call(human.AccessToken, http.MethodPost, "/events", map[string]any{"event": map[string]any{"type": "attention_feedback", "summary": "Forged", "refs": []string{}, "provenance": map[string]any{"sources": []string{"inferred"}}, "payload": feedback}}, 403)
	// Resource privacy applies to both the subject and authored feedback note.
	hidden, err := store.CreateWork(ctx, human.ActorID, "", map[string]any{"title": "Private attention evidence"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, human.ActorID, anyString(hidden["thread_id"]), map[string]any{"pm_actor_id": human.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	feedback["note"] = anyString(hidden["ref"])
	feedback["request_key"] = "private-feedback"
	call(human.AccessToken, http.MethodPost, "/attention/feedback", feedback, 201)
	page = call(agent.AccessToken, http.MethodGet, "/attention/feedback", nil, 200)
	raw, _ := json.Marshal(page)
	if strings.Contains(string(raw), anyString(hidden["ref"])) || len(page["items"].([]any)) != 1 {
		t.Fatal("private feedback leaked", page)
	}
}

func TestAttentionDocumentSnapshotMergesPlanAndLoopMetric(t *testing.T) {
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	store := h.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	work, err := store.CreateWork(ctx, "alice", "", map[string]any{"title": "Measured initiative", "goal": map[string]any{"metric": "score", "direction": "up"}})
	if err != nil {
		t.Fatal(err)
	}
	plan := plans.Plan{Steps: []plans.Step{{ID: "build", Title: "Build", After: []string{}}}}
	if err = store.SetCardPlan(ctx, "alice", anyString(work["id"]), anyString(work["updated_at"]), plan); err != nil {
		t.Fatal(err)
	}
	run := func(id string, score float64) {
		t.Helper()
		if _, err := store.AppendEvent(ctx, "alice", map[string]any{"type": "run_recorded", "summary": "Measured", "refs": []string{anyString(work["ref"])}, "payload": map[string]any{"run_id": id, "outcome": "measured", "metrics": map[string]any{"score": score}}}); err != nil {
			t.Fatal(err)
		}
	}
	report := `{"kind":"anx.visual-report","schema_version":1,"title":"Launch","summary":"Progress","generated_at":"2026-10-04T12:00:00Z","projects":[{"id":"launch","title":"Launch","summary":"Ship","outcome":"Delivery"}],"sources":[],"panels":[{"id":"progress","project_id":"launch","type":"live-initiatives","title":"Progress","author":"Test","provenance":"reported","observed_at":null,"freshness":"unknown","source_ids":[],"data":{}},{"id":"loops","project_id":"launch","type":"live-loops","title":"Loops","author":"Test","provenance":"reported","observed_at":null,"freshness":"unknown","source_ids":[],"data":{}}]}`
	doc, _, err := store.CreateDocument(ctx, "alice", map[string]any{"title": "Measured dashboard"}, report, "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	run("before", 0.8)
	scope := "document:" + anyString(doc["id"])
	now := time.Now().UTC()
	request := httptest.NewRequest(http.MethodGet, "/attention/changes", nil)
	opts := handlerOptions{primitiveStore: store}
	before, truncated, err := attentionDashboardSnapshot(request, opts, store, scope, now)
	if err != nil || truncated || len(before) != 1 || before[anyString(work["ref"])].Metric != 0.8 || before[anyString(work["ref"])].Plan == nil {
		t.Fatalf("snapshot=%+v truncated=%v err=%v", before, truncated, err)
	}
	if err = store.RecordAttentionVisit(ctx, "actor:alice", scope, before, now); err != nil {
		t.Fatal(err)
	}
	run("after", 0.9)
	plan.Steps[0].Status = "done"
	raw, _ := json.Marshal(plan)
	if _, err = h.workspace.DB().Exec(`UPDATE card_plans SET body_json=? WHERE card_id=?`, string(raw), work["id"]); err != nil {
		t.Fatal(err)
	}
	after, truncated, err := attentionDashboardSnapshot(request, opts, store, scope, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	changes, err := store.AttentionVisitChanges(ctx, "actor:alice", scope, after, truncated)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]bool{}
	for _, change := range changes.Items {
		kinds[change.Kind] = true
		if change.Kind == "metric_moved" && (change.From != 0.8 || change.To != 0.9) {
			t.Fatal(change)
		}
	}
	if !kinds["metric_moved"] || !kinds["plan_changed"] {
		t.Fatal(changes)
	}
}

func TestAttentionDocumentVisitsHaveIndependentBaselines(t *testing.T) {
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	store := h.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	work, err := store.CreateWork(ctx, "alice", "", map[string]any{"title": "Dashboard initiative"})
	if err != nil {
		t.Fatal(err)
	}
	plan := plans.Plan{Steps: []plans.Step{{ID: "build", Title: "Build", After: []string{}}}}
	if err = store.SetCardPlan(ctx, "alice", anyString(work["id"]), anyString(work["updated_at"]), plan); err != nil {
		t.Fatal(err)
	}
	report := `{"kind":"anx.visual-report","schema_version":1,"title":"Launch","summary":"Progress","generated_at":"2026-10-04T12:00:00Z","projects":[{"id":"launch","title":"Launch","summary":"Ship","outcome":"Delivery"}],"sources":[],"panels":[{"id":"progress","project_id":"launch","type":"live-initiatives","title":"Progress","author":"Test","provenance":"reported","observed_at":null,"freshness":"unknown","source_ids":[],"data":{}}]}`
	docs := []map[string]any{}
	for _, name := range []string{"first", "second"} {
		doc, _, err := store.CreateDocument(ctx, "alice", map[string]any{"title": name}, report, "text", nil)
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, doc)
	}
	call := func(method, scope string) map[string]any {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"scope_ref": scope})
		path := "/attention/visits"
		if method == http.MethodGet {
			path = "/attention/changes?scope_ref=" + scope
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		cacheAuthenticatedPrincipal(req, &auth.Principal{AgentID: "alice", ActorID: "alice", PrincipalKind: "human"})
		opts := handlerOptions{primitiveStore: store}
		attachResourceAccessScope(req, opts)
		out := httptest.NewRecorder()
		if method == http.MethodGet {
			handleAttentionChanges(out, req, opts)
		} else {
			handleAttentionVisit(out, req, opts)
		}
		if out.Code != 200 {
			t.Fatalf("status=%d %s", out.Code, out.Body.String())
		}
		var value map[string]any
		if err = json.Unmarshal(out.Body.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	first, second := anyString(docs[0]["ref"]), anyString(docs[1]["ref"])
	call(http.MethodPost, first)
	if d := call(http.MethodGet, second); d["since"] != nil {
		t.Fatal("visit leaked across dashboards", d)
	}
	plan.Steps[0].Status = "done"
	raw, _ := json.Marshal(plan)
	if _, err = h.workspace.DB().Exec(`UPDATE card_plans SET body_json=?,updated_at='2026-10-23T12:00:00Z' WHERE card_id=?`, string(raw), work["id"]); err != nil {
		t.Fatal(err)
	}
	changed := call(http.MethodGet, first)
	if changed["since"] == nil || len(changed["changed_refs"].([]any)) != 1 {
		t.Fatal(changed)
	}
	again := call(http.MethodGet, first)
	if again["since"] != changed["since"] || len(again["items"].([]any)) == 0 {
		t.Fatal("read advanced baseline", again)
	}
	call(http.MethodPost, first)
	if d := call(http.MethodGet, first); len(d["items"].([]any)) != 0 {
		t.Fatal("seen changes repeated", d)
	}
}
