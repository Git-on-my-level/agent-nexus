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
	t.Parallel()
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
	t.Parallel()
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
	if out.Code != 200 {
		t.Fatal(out.Body.String())
	}
	var batch struct {
		Items []primitives.RefPreview `json:"items"`
	}
	if err = json.Unmarshal(out.Body.Bytes(), &batch); err != nil || len(batch.Items) != 1 || batch.Items[0].Resolvable || batch.Items[0].Title != "" {
		t.Fatalf("private HTTP preview: %s error=%v", out.Body.String(), err)
	}
	previews, err := store.ResolveRefs(req.Context(), []string{anyString(hidden["ref"])}, planVisibility(req, opts), time.Now())
	if err != nil || len(previews) != 1 || previews[0].Resolvable || previews[0].Title != "" {
		t.Fatalf("private store preview: %+v error=%v", previews, err)
	}
	if err = store.EnrichCardPlans(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "reader"}), []map[string]any{parent}, planVisibility(req, opts), time.Now()); err != nil {
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
	t.Parallel()
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

func TestPlanRefLimits(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	for _, req := range []any{map[string]any{"refs": make([]string, 201)}, map[string]any{}, map[string]any{"refs": []string{strings.Repeat("a", 2049)}}} {
		planRequest(t, "POST", h.baseURL+"/refs/resolve", req, 400)
	}
}

func TestPromiseHealthAndPossiblyMootEndToEnd(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"One","created_at":"2026-03-04T10:00:00Z"}}`, 201)

	// The overtaken-by-events setup: a decision card parked in Ready asks a
	// human to authorize a merge, but both linked sources already finished —
	// one workspace card done, one PR merged through its ingested observation
	// evidence. Nothing auto-closes; the flag carries the evidence.
	store := h.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{"title": "Health"})
	if err != nil {
		t.Fatal(err)
	}
	// A linked card whose tracker observation reports done: the terminal
	// state arrives through the stored observation cache, never a read-time
	// fetch.
	landed, err := store.CreateWork(ctx, "actor-1", anyString(board["id"]), map[string]any{"title": "Landed work", "source": map[string]any{"authority": "tracker", "connection_id": "c", "native_id": "done-1"}})
	if err != nil {
		t.Fatal(err)
	}
	observation := map[string]any{"idempotency_key": "landed", "reader_id": "adapter", "reader_revision": "1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "status": "reported", "facts": map[string]any{"phase": "done"}, "evidence": []any{map[string]any{"kind": "issue", "url": "https://tracker.test/done-1", "knowledge": "reported"}}}
	if _, err = store.SubmitWorkObservation(ctx, "actor-1", anyString(landed["id"]), observation); err != nil {
		t.Fatal(err)
	}
	workPostJSON(t, h.baseURL+"/work", `{"actor_id":"actor-1","title":"PR carrier","source_refs":[{"authority":"github","connection_id":"c","native_id":"123","url":"https://github.com/example/repo/pull/123","status":"merged","phase":"done"}]}`, 201)
	decision := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"actor-1","title":"Ship the candidates?","phase":"ready"}`, 201)["work"].(map[string]any)
	endpoint := h.baseURL + "/cards/" + anyString(decision["ref"]) + "/plan"
	before := workGetJSON(t, endpoint, 200)
	// The human question itself stays open — an unlinked step — so the plan
	// is not done even though everything around it is.
	put := planRequest(t, "PUT", endpoint, map[string]any{"actor_id": "actor-1", "if_updated_at": before["if_updated_at"], "plan": map[string]any{"steps": []any{
		map[string]any{"id": "pr", "title": "Watch PR", "ref": "https://github.com/example/repo/pull/123"},
		map[string]any{"id": "landed", "title": "Landed", "ref": anyString(landed["ref"])},
		map[string]any{"id": "decide", "title": "Decide whether to ship"},
	}}}, 200)
	moot := put["possibly_moot"].(map[string]any)
	if moot["flagged"] != true || moot["sources"] != float64(2) || anyString(moot["reason"]) == "" {
		t.Fatal("plan response lost the moot flag", put)
	}
	// Nothing is promised on the decision card; the moot flag is what speaks
	// up here.
	if put["plan_health"].(map[string]any)["state"] != "no_promise" {
		t.Fatal("an undated decision card reads no_promise", put)
	}
	read := workGetJSON(t, h.baseURL+"/work/"+anyString(decision["ref"]), 200)["work"].(map[string]any)
	if read["possibly_moot"].(map[string]any)["flagged"] != true {
		t.Fatal("work read lost the moot flag", read)
	}
	previews := planRequest(t, "POST", h.baseURL+"/refs/resolve", map[string]any{"refs": []string{anyString(decision["ref"])}}, 200)["items"].([]any)
	if previews[0].(map[string]any)["possibly_moot"].(map[string]any)["flagged"] != true {
		t.Fatal("ref preview lost the moot flag", previews)
	}
	// An active card with one open source is not moot even with a done sibling.
	open := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"actor-1","title":"Still open","phase":"ready"}`, 201)["work"].(map[string]any)
	openEndpoint := h.baseURL + "/cards/" + anyString(open["ref"]) + "/plan"
	openBefore := workGetJSON(t, openEndpoint, 200)
	planRequest(t, "PUT", openEndpoint, map[string]any{"actor_id": "actor-1", "if_updated_at": openBefore["if_updated_at"], "plan": map[string]any{"steps": []any{
		map[string]any{"id": "pr", "title": "Watch PR", "ref": "https://github.com/example/repo/pull/123"},
		map[string]any{"id": "open", "title": "Open work", "ref": anyString(decision["ref"])},
	}}}, 200)
	openRead := workGetJSON(t, h.baseURL+"/work/"+anyString(open["ref"]), 200)["work"].(map[string]any)
	openMoot := openRead["possibly_moot"].(map[string]any)
	if openMoot["flagged"] != false || openMoot["sources"] != float64(2) {
		t.Fatal("an open linked source must block the flag", openMoot)
	}
	// Rule (b): one observed-merged PR and one URL ANX has never observed —
	// the merged observation cannot speak for the unobserved source, so the
	// card is not flagged and the evidence names nothing.
	half := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"actor-1","title":"Half observed","phase":"ready"}`, 201)["work"].(map[string]any)
	halfEndpoint := h.baseURL + "/cards/" + anyString(half["ref"]) + "/plan"
	halfBefore := workGetJSON(t, halfEndpoint, 200)
	halfPut := planRequest(t, "PUT", halfEndpoint, map[string]any{"actor_id": "actor-1", "if_updated_at": halfBefore["if_updated_at"], "plan": map[string]any{"steps": []any{
		map[string]any{"id": "seen", "title": "Observed PR", "ref": "https://github.com/example/repo/pull/123"},
		map[string]any{"id": "unseen", "title": "Unobserved PR", "ref": "https://github.com/example/repo/other/pull/999"},
	}}}, 200)
	halfMoot := halfPut["possibly_moot"].(map[string]any)
	if halfMoot["flagged"] != false || halfMoot["sources"] != float64(0) || anyString(halfMoot["reason"]) != "" {
		t.Fatal("an unobserved source must withhold the flag entirely", halfMoot)
	}

	// Promise sequencing on a fresh active card: nothing dated reads
	// no_promise, a past wake date is a missed promise, a future due moves it.
	quiet := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"actor-1","title":"Quiet initiative","phase":"in_progress"}`, 201)["work"].(map[string]any)
	quietRead := workGetJSON(t, h.baseURL+"/work/"+anyString(quiet["ref"]), 200)["work"].(map[string]any)
	if state := quietRead["plan_health"].(map[string]any)["state"]; state != "no_promise" {
		t.Fatal("nothing dated must read no_promise", quietRead["plan_health"])
	}
	version := int64(quietRead["version"].(float64))
	patchAndRead := func(patch map[string]any, status int) map[string]any {
		body, err := json.Marshal(map[string]any{"actor_id": "actor-1", "if_version": version, "patch": patch})
		if err != nil {
			t.Fatal(err)
		}
		payload := workPostJSON(t, h.baseURL+"/work/"+anyString(quiet["ref"]), string(body), status)
		if out, ok := payload["work"].(map[string]any); ok && status == 200 {
			version = int64(out["version"].(float64))
		}
		return workGetJSON(t, h.baseURL+"/work/"+anyString(quiet["ref"]), 200)["work"].(map[string]any)
	}
	wakeDate := time.Now().UTC().Add(-3 * 24 * time.Hour).Format("2006-01-02")
	health := patchAndRead(map[string]any{"wake_condition": "check the metric " + wakeDate}, 200)["plan_health"].(map[string]any)
	if health["state"] != "past_promise" || health["since"] != wakeDate+"T00:00:00Z" || health["promise"].(map[string]any)["source"] != "wake_condition" || health["past_promise_for_seconds"] == nil {
		t.Fatal("a past wake date is a missed promise", health)
	}
	dueDate := time.Now().UTC().Add(3 * 24 * time.Hour).Format(time.RFC3339)
	health = patchAndRead(map[string]any{"wake_condition": "", "due_at": dueDate}, 200)["plan_health"].(map[string]any)
	if health["state"] != "moving" || health["promise"].(map[string]any)["source"] != "due_at" {
		t.Fatal("a future due date is a moving promise", health)
	}
}

func TestInboxOpenAskCarriesPossiblyMoot(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	s := h.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	doneSubject, err := s.CreateWork(ctx, "actor-1", "", map[string]any{"title": "Finished subject", "source": map[string]any{"authority": "tracker", "connection_id": "c", "native_id": "done-2"}})
	if err != nil {
		t.Fatal(err)
	}
	observation := map[string]any{"idempotency_key": "landed-2", "reader_id": "adapter", "reader_revision": "1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "status": "reported", "facts": map[string]any{"phase": "done"}, "evidence": []any{map[string]any{"kind": "issue", "url": "https://tracker.test/done-2", "knowledge": "reported"}}}
	if _, err = s.SubmitWorkObservation(ctx, "actor-1", anyString(doneSubject["id"]), observation); err != nil {
		t.Fatal(err)
	}
	openSubject, err := s.CreateWork(ctx, "actor-1", "", map[string]any{"title": "Open subject"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateWork(ctx, "actor-1", "", map[string]any{"title": "PR carrier", "source_refs": []any{map[string]any{"authority": "github", "connection_id": "c", "native_id": "123", "url": "https://github.com/example/repo/pull/123", "status": "merged", "phase": "done"}}}); err != nil {
		t.Fatal(err)
	}
	// The page has already been authorized; the moot evaluation must use the
	// same scoped resolver and read only cached evidence.
	req := httptest.NewRequest("GET", "/inbox", nil).WithContext(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "reader"}))
	items := []map[string]any{
		{"kind": "ask", "status": "open", "subject_ref": anyString(doneSubject["ref"]), "related_refs": []any{"https://github.com/example/repo/pull/123"}},
		{"kind": "ask", "status": "open", "subject_ref": anyString(openSubject["ref"]), "related_refs": []any{"https://github.com/example/repo/pull/123"}},
		{"kind": "ask", "status": "answered", "subject_ref": anyString(doneSubject["ref"]), "related_refs": []any{"https://github.com/example/repo/pull/123"}},
		// Rule (b): the observed-merged PR cannot speak for a URL this
		// workspace never observed, so the ask gets no flag at all.
		{"kind": "ask", "status": "open", "subject_ref": anyString(doneSubject["ref"]), "related_refs": []any{"https://github.com/example/repo/other/pull/999"}},
	}
	if err = enrichInboxCardSummaries(req, handlerOptions{primitiveStore: s}, items); err != nil {
		t.Fatal(err)
	}
	moot := items[0]["possibly_moot"].(primitives.PossiblyMoot)
	if !moot.Flagged || moot.Sources != 2 || moot.Reason == "" {
		t.Fatalf("open ask over finished work must be moot: %+v", items[0])
	}
	moot = items[1]["possibly_moot"].(primitives.PossiblyMoot)
	if moot.Flagged || moot.Sources != 2 {
		t.Fatalf("open subject must block the flag: %+v", items[1])
	}
	// Answered asks are not evaluated, so they carry no flag at all.
	if _, evaluated := items[2]["possibly_moot"]; evaluated {
		t.Fatalf("answered asks must not carry the flag: %+v", items[2])
	}
	if _, evaluated := items[3]["possibly_moot"]; evaluated {
		t.Fatalf("an unobserved source must withhold the flag entirely: %+v", items[3])
	}
}
