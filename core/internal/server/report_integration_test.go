package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	reports "agent-nexus-visualreport"
)

func createReportFixture(t *testing.T, h primitivesTestHarness, structured bool, kinds ...string) string {
	t.Helper()
	panels := []map[string]any{}
	if len(kinds) == 0 {
		kinds = []string{"live-initiatives", "live-asks", "live-work-mix", "live-activity"}
	}
	for _, kind := range kinds {
		panels = append(panels, map[string]any{"id": kind, "type": kind, "data": map[string]any{}, "project_id": "workspace", "title": kind, "author": "ANX", "provenance": "reported", "observed_at": nil, "freshness": "unavailable", "source_ids": []any{}})
	}
	report := map[string]any{"kind": "anx.visual-report", "schema_version": 1, "panels": panels, "title": "Dashboard", "summary": "Workspace report", "generated_at": "2026-10-04T10:00:00Z", "sources": []any{}, "projects": []any{map[string]any{"id": "workspace", "title": "Workspace", "summary": "Current work", "outcome": "Ship"}}}
	var content any = report
	contentType := "structured"
	if !structured {
		raw, _ := json.Marshal(report)
		content = string(raw)
		contentType = "text"
	}
	body, _ := json.Marshal(map[string]any{"actor_id": "actor-1", "document": map[string]any{"title": "Dashboard"}, "content": content, "content_type": contentType})
	created := workPostJSON(t, h.baseURL+"/docs", string(body), http.StatusCreated)
	return anyString(created["document"].(map[string]any)["ref"])
}

func reportPanelByType(t *testing.T, response map[string]any, kind string) map[string]any {
	t.Helper()
	for _, raw := range response["panels"].([]any) {
		panel := raw.(map[string]any)
		if panel["type"] == kind {
			return panel
		}
	}
	t.Fatalf("missing %s: %#v", kind, response)
	return nil
}

func TestReportLiveWorkAndArchiveBoundary(t *testing.T) {
	requireIntegrationTest(t)
	for _, structured := range []bool{false, true} {
		t.Run(fmt.Sprint(structured), func(t *testing.T) {
			h := newPrimitivesTestServer(t)
			workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"One","created_at":"2026-03-04T10:00:00Z"}}`, 201)
			board := workPostJSON(t, h.baseURL+"/boards", `{"actor_id":"actor-1","board":{"title":"Initiatives"}}`, 201)["board"].(map[string]any)
			boardRef := anyString(board["ref"])
			cardBody, _ := json.Marshal(map[string]any{"actor_id": "actor-1", "board_ref": boardRef, "title": "Ship the launch", "summary": "Launch on Friday\n- [X] Reviewed\n- [ ] Deliver\nNeeds Alex: choose the date", "priority": "p1"})
			workPostJSON(t, h.baseURL+"/work", string(cardBody), 201)
			docRef := createReportFixture(t, h, structured)
			endpoint := h.baseURL + "/docs/" + docRef + "/report"
			before := workGetJSON(t, h.baseURL+"/docs/"+docRef, 200)
			response := workGetJSON(t, endpoint, 200)
			previewBody, _ := json.Marshal(map[string]any{"report": before["revision"].(map[string]any)["content"]})
			preview := workPostJSON(t, h.baseURL+"/reports/preview", string(previewBody), 200)
			if len(preview["panels"].([]any)) != 4 || preview["observed_at"] == nil {
				t.Fatalf("unsaved report preview omitted live panels: %#v", preview)
			}
			if _, persisted := preview["document_ref"]; persisted {
				t.Fatal("unsaved report preview claimed a saved document")
			}
			panel := reportPanelByType(t, response, "live-initiatives")
			if panel["status"] != "ok" || panel["truncated"] != false {
				t.Fatalf("%#v", panel)
			}
			items := panel["data"].(map[string]any)["items"].([]any)
			if len(items) != 1 {
				t.Fatalf("items=%#v", items)
			}
			item := items[0].(map[string]any)
			progress := item["progress"].(map[string]any)
			if item["summary"] != "Launch on Friday" || progress["done"] != float64(1) || progress["total"] != float64(2) || len(item["needs"].([]any)) != 1 || item["priority"] != "p1" {
				t.Fatalf("item=%#v", item)
			}
			workPostJSON(t, h.baseURL+"/work", fmt.Sprintf(`{"actor_id":"actor-1","board_ref":%q,"title":"New since the report was saved"}`, boardRef), 201)
			response = workGetJSON(t, endpoint, 200)
			mix := reportPanelByType(t, response, "live-work-mix")["data"].(map[string]any)
			if mix["total"] != float64(2) {
				t.Fatalf("not live: %#v", mix)
			}
			after := workGetJSON(t, h.baseURL+"/docs/"+docRef, 200)
			if before["revision"].(map[string]any)["revision_id"] != after["revision"].(map[string]any)["revision_id"] {
				t.Fatal("render changed report revision")
			}
			workPostJSON(t, h.baseURL+"/boards/"+boardRef+"/archive", `{"actor_id":"actor-1"}`, 200)
			response = workGetJSON(t, endpoint, 200)
			mix = reportPanelByType(t, response, "live-work-mix")["data"].(map[string]any)
			if mix["total"] != float64(0) {
				t.Fatalf("archived work counted: %#v", mix)
			}
		})
	}
}

type reportWorkFailure struct{ *primitives.Store }

func (s reportWorkFailure) ListReportWork(context.Context, primitives.ReportWorkFilter) (primitives.ReportWorkPage, error) {
	return primitives.ReportWorkPage{}, fmt.Errorf("secret backend detail")
}

func TestReportPartialAndUnavailablePanels(t *testing.T) {
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"One","created_at":"2026-03-04T10:00:00Z"}}`, 201)
	ref := createReportFixture(t, h, false)
	store := reportWorkFailure{h.primitiveStore.(*primitives.Store)}
	req := httptest.NewRequest("GET", "/docs/"+ref+"/report", nil)
	out := httptest.NewRecorder()
	handleRenderReport(out, req, handlerOptions{primitiveStore: store}, ref)
	if out.Code != 200 || strings.Contains(out.Body.String(), "secret backend detail") {
		t.Fatalf("%d %s", out.Code, out.Body.String())
	}
	var response map[string]any
	_ = json.Unmarshal(out.Body.Bytes(), &response)
	if reportPanelByType(t, response, "live-initiatives")["status"] != "unavailable" || reportPanelByType(t, response, "live-activity")["status"] != "ok" {
		t.Fatalf("%#v", response)
	}
	first, err := h.primitiveStore.(*primitives.Store).CreateWork(context.Background(), "actor-1", "", map[string]any{"title": "First"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := h.primitiveStore.(*primitives.Store).CreateWork(context.Background(), "actor-1", "", map[string]any{"title": "Second"})
	if err != nil {
		t.Fatal(err)
	}
	reader := reportReader{r: req, opts: handlerOptions{primitiveStore: h.primitiveStore}, now: time.Now(), work: []map[string]any{
		{"id": first["id"], "ref": "card:first", "phase": "ready", "board_ref": "board:b", "plan_state": map[string]any{"shape": "chain", "health": "on_track", "steps": []any{map[string]any{"id": "design", "title": "Design", "status": "done"}}}, "assignee_refs": []string{"actor:one"}},
		{"id": second["id"], "ref": "card:second", "phase": "ready", "board_ref": "board:b"},
	}, boards: map[string]map[string]any{"board:b": {"title": "Work"}}}
	cacheKey, _ := json.Marshal(primitives.ReportWorkFilter{Limit: reports.MaxRows})
	reader.workScopes = map[string]reportWorkRead{string(cacheKey): {work: reader.work}}
	data, partial, err := reader.materialize(reports.Panel{Type: "live-initiatives", Query: reports.Query{Limit: 1, Sort: "title"}})
	if err != nil || !partial || len(data["items"].([]map[string]any)) != 1 {
		t.Fatalf("%#v %v %v", data, partial, err)
	}
	item := data["items"].([]map[string]any)[0]
	if item["plan_state"].(map[string]any)["shape"] != "chain" || item["assignee_refs"].([]string)[0] != "actor:one" {
		t.Fatalf("initiative plan/assignee fields were dropped: %#v", item)
	}
	reader.workScopes[string(cacheKey)] = reportWorkRead{work: reader.work, partial: true}
	_, partial, err = reader.materialize(reports.Panel{Type: "live-work-mix", Query: reports.Query{GroupBy: "phase"}})
	if err != nil || !partial {
		t.Fatal("lost source truncation")
	}
}

func TestReportAskAgeAnswersAndPrivateEvents(t *testing.T) {
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"One","created_at":"2026-03-04T10:00:00Z"}}`, 201)
	board := workPostJSON(t, h.baseURL+"/boards", `{"actor_id":"actor-1","board":{"title":"Asks"}}`, 201)["board"].(map[string]any)
	thread := anyString(board["thread_id"])
	boardRef := anyString(board["ref"])
	card := workPostJSON(t, h.baseURL+"/work", fmt.Sprintf(`{"actor_id":"actor-1","board_ref":%q,"title":"Launch readiness"}`, boardRef), 201)["work"].(map[string]any)
	cardRef := anyString(card["ref"])
	event := createHumanAttentionEvent(t, h.baseURL, thread, "ask", "Pick the date", cardRef, nil, map[string]any{"body": "Friday?"})
	createHumanAttentionEvent(t, h.baseURL, thread, "ask", "Open question", cardRef, nil, map[string]any{"body": "Still open"})
	item, _ := deriveHumanAttentionInboxItem(event)
	reader := reportReader{r: httptest.NewRequest("GET", "/", nil), opts: handlerOptions{primitiveStore: h.primitiveStore}, now: time.Now().Add(time.Hour), visibility: map[string]bool{}}
	data, _, err := reader.asks(reports.Query{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	items := data["items"].([]map[string]any)
	if len(items) != 2 || items[0]["age_seconds"].(int64) < 3590 {
		t.Fatalf("%#v", items)
	}
	response := map[string]any{"id": "response", "type": "human_attention_responded", "thread_id": thread, "refs": []string{"inbox:" + item.ID}, "ts": time.Now().UTC().Format(time.RFC3339Nano), "payload": map[string]any{"response_text": "Friday", "inbox_item_id": item.ID}}
	reader.events = append([]map[string]any{response}, reader.events...)
	data, _, _ = reader.asks(reports.Query{Limit: 10})
	openItems := data["items"].([]map[string]any)
	if len(openItems) != 1 || openItems[0]["status"] != "open" {
		t.Fatalf("answered ask remained open or unrelated open ask disappeared: %#v", openItems)
	}
	data, _, _ = reader.asks(reports.Query{Limit: 10, IncludeAnswered: true, AnsweredWithinHours: 168})
	allItems := data["items"].([]map[string]any)
	if len(allItems) != 2 || allItems[0]["status"] != "open" || allItems[1]["id"] != "completed:response" {
		t.Fatalf("recent answer or open ask missing: %#v", allItems)
	}
	data, _, err = reader.asks(reports.Query{Limit: 10, AnsweredOnly: true, AnsweredWithinHours: 168, CardRef: cardRef})
	if err != nil || len(data["items"].([]map[string]any)) != 1 {
		t.Fatalf("answered-only query included open asks or omitted the answer: %#v %v", data, err)
	}
	data, _, err = reader.asks(reports.Query{Limit: 10, AnsweredOnly: true, AnsweredWithinHours: 168, CardRef: "card:other"})
	if err != nil || len(data["items"].([]map[string]any)) != 0 {
		t.Fatalf("decision history leaked across card scope: %#v %v", data, err)
	}
	private := map[string]any{"id": "private", "type": "human_attention_requested", "actor_id": "someone-else", "payload": map[string]any{"pm_turn_id": "private-turn"}, "summary": "secret"}
	if len(filterAccessibleEvents(reader.r, reader.opts, []map[string]any{private})) != 0 {
		t.Fatal("private PM event visible")
	}
	workPostJSON(t, h.baseURL+"/boards/"+boardRef+"/archive", `{"actor_id":"actor-1"}`, 200)
	reader.visibility = map[string]bool{}
	data, _, _ = reader.asks(reports.Query{Limit: 10, IncludeAnswered: true, AnsweredWithinHours: 168})
	if len(data["items"].([]map[string]any)) != 0 {
		t.Fatal("archived ask visible")
	}
}

func TestReportActivityCollapsesBoardEditBursts(t *testing.T) {
	requireIntegrationTest(t)
	now := time.Now().UTC()
	reader := reportReader{now: now, eventsRead: true, visibility: map[string]bool{"board:launch": true}, events: []map[string]any{}}
	for i := range 5 {
		reader.events = append(reader.events, map[string]any{"ref": fmt.Sprintf("event:e%d", i), "type": "card_updated", "actor_id": "agent", "refs": []string{"board:launch"}, "ts": now.Add(-time.Duration(i) * time.Minute).Format(time.RFC3339Nano), "summary": "Edit"})
	}
	data, partial, err := reader.activity(reports.Query{Limit: 10})
	if err != nil || partial {
		t.Fatalf("%v %v", partial, err)
	}
	items := data["items"].([]map[string]any)
	if len(items) != 1 || items[0]["count"] != 5 {
		t.Fatalf("%#v", items)
	}
}

func TestReportActivityIncludesAuthorizedDecisions(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "report-human", "report-human-actor", "report-human", "report-token")
	store := env.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Launch"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateWork(ctx, owner.ActorID, anyString(board["id"]), map[string]any{"title": "Launch readiness"})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewPMRuntime(env.workspace.DB(), store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.Service.ProposeDecision(ctx, pm.Principal{WorkspaceID: "ws_main", ActorID: owner.ActorID, Human: true}, pm.DecisionInput{RequestKey: "dashboard", WorkRef: anyString(work["ref"]), Instruction: `{"next_action":"private instruction"}`, Scope: "work.annotate", TargetRevision: "1.1"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/", nil)
	cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: owner.ActorID, AgentID: owner.AgentID, PrincipalKind: string(auth.PrincipalKindHuman)})
	reader := reportReader{r: req, opts: handlerOptions{primitiveStore: store, pmRuntime: runtime}, now: time.Now(), visibility: map[string]bool{}}
	data, _, err := reader.activity(reports.Query{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	items := data["items"].([]map[string]any)
	if len(items) < 1 || items[0]["type"] != "decision_proposed" || !strings.Contains(anyString(items[0]["summary"]), "Launch readiness") {
		t.Fatalf("%#v", items)
	}
	raw, _ := json.Marshal(data)
	if strings.Contains(string(raw), "private instruction") {
		t.Fatal("decision activity copied an instruction")
	}
	if _, err := env.workspace.DB().ExecContext(ctx, `UPDATE agents SET revoked_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339Nano), owner.AgentID); err != nil {
		t.Fatal(err)
	}
	reader = reportReader{r: req, opts: reader.opts, now: time.Now(), visibility: map[string]bool{}}
	if _, _, err := reader.activity(reports.Query{Limit: 10}); err == nil {
		t.Fatal("revoked reader retained decision access")
	}
}
