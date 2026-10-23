package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
	reports "agent-nexus-visualreport"
)

func TestWatchCollectionsScopeAndHealth(t *testing.T) {
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	ctx := context.Background()
	s := h.primitiveStore.(*primitives.Store)
	board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Initiatives", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateTopic(ctx, "owner", map[string]any{"title": "Project", "summary": "Ship"})
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateWork(ctx, "owner", anyString(board["id"]), map[string]any{"title": "Public loop", "project_ref": project.Topic["ref"], "owner": "actor:owner", "next_actor": "actor:viewer", "cadence": map[string]any{"every_seconds": 3600}, "goal": map[string]any{"metric": "score", "label": "F1", "unit": "%", "direction": "up"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetCardPlan(ctx, "owner", anyString(w["id"]), anyString(w["updated_at"]), plans.Plan{Steps: []plans.Step{{ID: "a", Title: "Ready", Status: "done", After: []string{}}, {ID: "b", Title: "Ship", After: []string{"a"}}}}); err != nil {
		t.Fatal(err)
	}
	private, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Confidential", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", anyString(private["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateWork(ctx, "owner", anyString(private["id"]), map[string]any{"title": "Secret loop", "cadence": map[string]any{"every_seconds": 3600}}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		_, err = s.AppendEvent(ctx, "owner", map[string]any{"type": "run_recorded", "summary": "Measured", "ts": time.Now().UTC().Format(time.RFC3339Nano), "thread_id": w["thread_id"], "refs": []string{anyString(w["ref"])}, "payload": map[string]any{"run_id": fmt.Sprint(i), "outcome": "acted", "metrics": map[string]any{"score": 0.9 + float64(i)*0.04}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, viewer := range []string{"viewer", "other-agent"} {
		req := httptest.NewRequest("GET", "/", nil)
		kind := "human"
		if viewer == "other-agent" {
			kind = "agent"
		}
		cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: viewer, AgentID: viewer, PrincipalKind: kind})
		req = req.WithContext(primitives.WithRequestAccessScope(req.Context(), primitives.AccessScope{ActorID: viewer}))
		for _, collection := range []string{"initiatives", "loops"} {
			reader := reportReader{r: req, opts: handlerOptions{primitiveStore: s}, now: time.Now(), visibility: map[string]bool{}}
			data, partial, err := reader.collection(reports.Query{Collection: collection, GroupBy: "project", Limit: 24})
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(data)
			if strings.Contains(string(raw), "Secret") || strings.Contains(string(raw), "Confidential") || partial {
				t.Fatal(string(raw), partial)
			}
			groups := data["groups"].([]map[string]any)
			if len(groups) != 1 || groups[0]["label"] != "Project" {
				t.Fatal(string(raw))
			}
			rows := groups[0]["rows"].([]map[string]any)
			if len(rows) != 1 {
				t.Fatal(string(raw))
			}
			row := rows[0]
			if len(row["activity"].([]int)) != 14 {
				t.Fatal(row)
			}
			if collection == "initiatives" {
				expected := "stalled"
				if viewer == "viewer" {
					expected = "waiting_on_you"
				}
				if row["state"] != expected {
					t.Fatal(row)
				}
				if reportMap(row["progress"])["kind"] != "segments" {
					t.Fatal(row)
				}
			} else {
				if reportMap(row["progress"])["kind"] != "spark" || !strings.Contains(anyString(reportMap(row["progress"])["label"]), "F1 0.94 %") {
					t.Fatal(row)
				}
			}
		}
	}
}

func TestWatchScopedActivityAndPanelSelection(t *testing.T) {
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	ctx := context.Background()
	s := h.primitiveStore.(*primitives.Store)
	board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Selected", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateWork(ctx, "owner", anyString(board["id"]), map[string]any{"title": "Selected card"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetCardPlan(ctx, "owner", anyString(w["id"]), anyString(w["updated_at"]), plans.Plan{Steps: []plans.Step{{ID: "ship", Title: "Ship", After: []string{}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AppendEvent(ctx, "owner", map[string]any{"type": "human_attention_requested", "thread_id": board["thread_id"], "refs": []string{anyString(w["ref"])}, "payload": map[string]any{"subject_ref": w["ref"], "kind": "ask", "requester_actor_id": "owner", "recipient_actor_id": "viewer", "title": "Choose launch", "response_proposals": []string{"Proceed"}}}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/", nil).WithContext(primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "viewer"}))
	for _, q := range []reports.Query{{SubjectRefs: []string{anyString(w["ref"])}, Limit: 10}, {Collection: "initiatives", Limit: 10}} {
		reader := reportReader{r: req, opts: handlerOptions{primitiveStore: s}, now: time.Now(), visibility: map[string]bool{}}
		data, _, err := reader.activity(q)
		if err != nil {
			t.Fatal(err)
		}
		if len(data["items"].([]map[string]any)) == 0 {
			t.Fatalf("scoped activity empty: %v", q)
		}
		for _, item := range data["items"].([]map[string]any) {
			if reportMap(item["subject"])["ref"] != w["ref"] {
				t.Fatal(item)
			}
		}
	}
	panels := []reports.Panel{{ID: "one"}, {ID: "two"}}
	for _, query := range []string{"panels=missing", "panels=one,one", "panels=", "panels=one&panels=two"} {
		if _, err := selectReportPanels(httptest.NewRequest("GET", "/?"+query, nil), panels); err == nil {
			t.Fatal(query)
		}
	}
	selected, err := selectReportPanels(httptest.NewRequest("GET", "/?panels=two", nil), panels)
	if err != nil || len(selected) != 1 || selected[0].ID != "two" {
		t.Fatal(selected, err)
	}
}

func TestWatchPanelCacheScopeAndInvalidation(t *testing.T) {
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	ctx := context.Background()
	s := h.primitiveStore.(*primitives.Store)
	board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Private", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateWork(ctx, "owner", anyString(board["id"]), map[string]any{"title": "Confidential", "cadence": map[string]any{"every_seconds": 3600}})
	if err != nil {
		t.Fatal(err)
	}
	opts := handlerOptions{primitiveStore: s, reportPanels: &reportPanelCache{}}
	panel := reports.Panel{ID: "loops", Type: "live-collection", Query: reports.Query{Collection: "loops", Limit: 24, GroupBy: "none"}}
	read := func(actor string) string {
		req := httptest.NewRequest("GET", "/docs/dashboard/report", nil).WithContext(primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: actor}))
		_, results := materializeReportPanels(req, opts, []reports.Panel{panel})
		if results[0]["status"] != "ok" {
			t.Fatal(results)
		}
		raw, _ := json.Marshal(results)
		return string(raw)
	}
	if !strings.Contains(read("viewer"), "Confidential") {
		t.Fatal("public row missing")
	}
	if _, err = s.PatchThread(ctx, "owner", anyString(board["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(read("viewer"), "Confidential") {
		t.Fatal("cache survived visibility epoch change")
	}
	if !strings.Contains(read("owner"), "Confidential") {
		t.Fatal("owner row missing")
	}
	if strings.Contains(read("other-agent"), "Confidential") {
		t.Fatal("cache crossed principal")
	}
}

func TestWatchPinsReplayPreservesNewerSelection(t *testing.T) {
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"One","created_at":"2026-10-04T10:00:00Z"}}`, 201)
	first := createReportFixture(t, h, true, "live-initiatives")
	second := createReportFixture(t, h, true, "live-initiatives")
	s := h.primitiveStore.(*primitives.Store)
	set := func(key, ref string) int {
		raw, _ := json.Marshal(map[string]any{"request_key": key, "document_refs": []string{ref}})
		req := httptest.NewRequest("PUT", "/workspace/dashboard/pins", bytes.NewReader(raw))
		cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: "owner", AgentID: "owner", PrincipalKind: "human"})
		req = req.WithContext(primitives.WithRequestAccessScope(req.Context(), primitives.AccessScope{ActorID: "owner"}))
		response := httptest.NewRecorder()
		handleDashboardPins(response, req, handlerOptions{primitiveStore: s})
		return response.Code
	}
	if set("first", first) != 200 || set("second", second) != 200 || set("first", first) != 200 {
		t.Fatal("pin mutation/replay failed")
	}
	pins, err := s.DashboardPins(primitives.WithAccessScope(context.Background(), primitives.AccessScope{ActorID: "owner"}))
	if err != nil || len(pins["document_refs"].([]string)) != 1 || pins["document_refs"].([]string)[0] != second {
		t.Fatal("replay overwrote newer selection", pins, err)
	}
	if set("first", second) != 409 {
		t.Fatal("changed replay body was accepted")
	}
}
