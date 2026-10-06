package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
)

func planRequest(t *testing.T, method, endpoint string, body any, status int) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(method, endpoint, strings.NewReader(string(raw)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != status {
		t.Fatalf("%s %s status=%d want=%d: %s", method, endpoint, resp.StatusCode, status, data)
	}
	var out map[string]any
	if err = json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPlanWriteRollsBackWhenEventCannotBeStored(t *testing.T) {
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"One","created_at":"2026-03-04T10:00:00Z"}}`, 201)
	work := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"actor-1","title":"Atomic plan"}`, 201)["work"].(map[string]any)
	endpoint := h.baseURL + "/cards/" + anyString(work["ref"]) + "/plan"
	before := workGetJSON(t, endpoint, 200)
	if _, err := h.workspace.DB().Exec(`CREATE TRIGGER reject_plan_event BEFORE INSERT ON events WHEN json_extract(NEW.payload_json,'$.payload.plan') IS NOT NULL BEGIN SELECT RAISE(ABORT,'fixture event failure'); END`); err != nil {
		t.Fatal(err)
	}
	// The ledger maps SQLite constraint rejection to conflict. Regardless of
	// error classification, the plan and card token must roll back together.
	planRequest(t, "PUT", endpoint, map[string]any{"actor_id": "actor-1", "if_updated_at": before["if_updated_at"], "plan": map[string]any{"steps": []any{}}}, 409)
	after := workGetJSON(t, endpoint, 200)
	if after["plan"] != nil || after["if_updated_at"] != before["if_updated_at"] {
		t.Fatal("partial mutation survived:", after)
	}
}

func TestPlanAndBatchRefsRespectPrivateThreads(t *testing.T) {
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	ctx := context.Background()
	store := h.primitiveStore.(*primitives.Store)
	hidden, err := store.CreateWork(ctx, "actor-1", "", map[string]any{"title": "Private title", "phase": "blocked"})
	if err != nil {
		t.Fatal(err)
	}
	thread := anyString(hidden["thread_id"])
	if _, err = store.PatchThread(ctx, "actor-1", thread, map[string]any{"pm_actor_id": "private-owner"}, nil); err != nil {
		t.Fatal(err)
	}
	parent, err := store.CreateWork(ctx, "actor-1", "", map[string]any{"title": "Public initiative"})
	if err != nil {
		t.Fatal(err)
	}
	p := plans.Plan{Steps: []plans.Step{{ID: "private", Title: "Linked step", Ref: anyString(hidden["ref"])}}}
	if err = store.SetCardPlan(ctx, "actor-1", anyString(parent["id"]), anyString(parent["updated_at"]), p); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/refs/resolve", strings.NewReader(`{"refs":["`+anyString(hidden["ref"])+`"]}`))
	out := httptest.NewRecorder()
	opts := handlerOptions{primitiveStore: store}
	attachResourceAccessScope(req, opts)
	handleResolveRefs(out, req, opts)
	if out.Code != 404 {
		t.Fatal(out.Body.String())
	}
	previews, err := store.ResolveRefs(req.Context(), []string{anyString(hidden["ref"])}, planVisibility(req, opts), time.Now(), 0)
	if err != nil || len(previews) != 1 || previews[0].Resolvable || previews[0].Title != "" {
		t.Fatalf("private store preview: %+v error=%v", previews, err)
	}
	if err = store.EnrichCardPlans(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "reader"}), []map[string]any{parent}, planVisibility(req, opts), time.Now(), 0); err != nil {
		t.Fatal(err)
	}
	if parent["plan"] != nil || parent["plan_state"] != nil || parent["next_step"] != nil {
		t.Fatal("private plan leaked:", parent)
	}
	// A linked private card cannot be read or edited through the plan route.
	out = httptest.NewRecorder()
	cardReq := httptest.NewRequest("GET", "/", nil)
	attachResourceAccessScope(cardReq, opts)
	handleCardPlan(out, cardReq, opts, anyString(hidden["ref"]))
	if out.Code != 404 {
		t.Fatal(out.Code, out.Body.String())
	}
}

func TestPlanAPIProjectsCardsWorkReportsAndTimeline(t *testing.T) {
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"One","created_at":"2026-03-04T10:00:00Z"}}`, 201)
	board := workPostJSON(t, h.baseURL+"/boards", `{"actor_id":"actor-1","board":{"title":"Initiatives"}}`, 201)["board"].(map[string]any)
	work := planRequest(t, "POST", h.baseURL+"/work", map[string]any{"actor_id": "actor-1", "board_ref": board["ref"], "title": "Initiative", "summary": "- [x] Stale progress prose"}, 201)["work"].(map[string]any)
	ref := anyString(work["ref"])
	endpoint := h.baseURL + "/cards/" + ref + "/plan"
	initial := workGetJSON(t, endpoint, 200)
	if initial["plan_health"].(map[string]any)["state"] != "no_plan" || initial["next_step"] != nil {
		t.Fatal(initial)
	}
	token := initial["if_updated_at"]
	p := map[string]any{"steps": []any{map[string]any{"id": "build", "title": "Build", "status": "done"}, map[string]any{"id": "ship", "title": "Ship", "status": "blocked", "after": []string{"build"}}}}
	result := planRequest(t, "PUT", endpoint, map[string]any{"actor_id": "actor-1", "if_updated_at": token, "plan": p}, 200)
	state := result["plan_state"].(map[string]any)
	if result["plan_health"].(map[string]any)["state"] != "blocked" || result["status_mismatch"] != true || state["health"] != "blocked" || state["progress"].(map[string]any)["done"] != float64(1) {
		t.Fatal(result)
	}
	for _, path := range []string{"/cards/" + ref, "/work/" + ref} {
		read := workGetJSON(t, h.baseURL+path, 200)
		key := "card"
		if strings.HasPrefix(path, "/work") {
			key = "work"
		}
		card := read[key].(map[string]any)
		if card["plan_health"].(map[string]any)["state"] != "blocked" || card["status_mismatch"] != true || card["column_key"] != "backlog" && key == "card" || card["plan"] == nil || card["plan_state"].(map[string]any)["health"] != "blocked" {
			t.Fatal(read)
		}
	}
	planRequest(t, "PUT", endpoint, map[string]any{"actor_id": "actor-1", "if_updated_at": token, "plan": p}, 409)
	cycle := map[string]any{"steps": []any{map[string]any{"id": "a", "title": "A", "after": []string{"b"}}, map[string]any{"id": "b", "title": "B", "after": []string{"a"}}}}
	planRequest(t, "PUT", endpoint, map[string]any{"actor_id": "actor-1", "if_updated_at": result["if_updated_at"], "plan": cycle}, 400)
	if after := workGetJSON(t, endpoint, 200); after["if_updated_at"] != result["if_updated_at"] {
		t.Fatal("invalid edit changed state")
	}
	previews := planRequest(t, "POST", h.baseURL+"/refs/resolve", map[string]any{"refs": []string{ref, "card:missing", ref}}, 200)["items"].([]any)
	if len(previews) != 3 || previews[0].(map[string]any)["progress"].(map[string]any)["total"] != float64(2) || previews[1].(map[string]any)["resolvable"] != false || len(previews[1].(map[string]any)) != 2 {
		t.Fatal(previews)
	}
	doc := createReportFixture(t, h, true)
	report := workGetJSON(t, h.baseURL+"/docs/"+doc+"/report", 200)
	items := reportPanelByType(t, report, "live-initiatives")["data"].(map[string]any)["items"].([]any)
	item := items[0].(map[string]any)
	if item["progress"].(map[string]any)["total"] != float64(2) || item["health"] != "blocked" || item["needs"].([]any)[0] != "Ship" {
		t.Fatal(item)
	}
	planSteps := item["plan"].(map[string]any)["steps"].([]any)
	stateSteps := item["plan_state"].(map[string]any)["steps"].([]any)
	if planSteps[1].(map[string]any)["title"] != "Ship" || stateSteps[1].(map[string]any)["id"] != "ship" || stateSteps[1].(map[string]any)["status"] != "blocked" {
		t.Fatalf("report did not preserve the authored plan and computed state contracts: %#v", item)
	}
	// The plan event is visible in the existing card lifecycle timeline.
	timeline := workGetJSON(t, h.baseURL+"/cards/"+ref+"/timeline", 200)
	raw, _ := json.Marshal(timeline)
	if !strings.Contains(string(raw), "Initiative plan updated") || !strings.Contains(string(raw), "before_plan") {
		t.Fatal(timeline)
	}
}

func TestPlanThresholdAndRefLimits(t *testing.T) {
	requireIntegrationTest(t)
	t.Setenv("ANX_PLAN_STALLED_AFTER", "24h")
	if planStalledAfter().Hours() != 24 {
		t.Fatal("threshold ignored")
	}
	h := newPrimitivesTestServer(t)
	for _, req := range []any{map[string]any{"refs": make([]string, 201)}, map[string]any{}, map[string]any{"refs": []string{strings.Repeat("a", 2049)}}} {
		planRequest(t, "POST", h.baseURL+"/refs/resolve", req, 400)
	}
}
