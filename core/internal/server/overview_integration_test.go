package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-visualreport"
)

func TestDashboardEndpointsPreserveHeadRevisionIdentity(t *testing.T) {
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	store := h.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	report := `{"kind":"anx.visual-report","schema_version":1,"title":"Launch","summary":"Progress","generated_at":"2026-10-04T12:00:00Z","projects":[{"id":"launch","title":"Launch","summary":"Ship","outcome":"Delivery"}],"sources":[],"panels":[{"id":"progress","project_id":"launch","type":"live-initiatives","title":"Original labels","author":"Test","provenance":"reported","observed_at":null,"freshness":"unknown","source_ids":[],"data":{}}]}`
	doc, _, err := store.CreateDocument(ctx, "executive", map[string]any{"title": "Launch", "handle": "launch-dashboard"}, report, "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, revision, err := store.GetDocument(ctx, anyString(doc["id"]))
	if err != nil {
		t.Fatal(err)
	}
	check := func(title string) {
		t.Helper()
		for _, path := range []string{"/overview", "/workspace/dashboard/reports"} {
			resp, err := http.Get(h.baseURL + path)
			if err != nil {
				t.Fatal(err)
			}
			var body map[string]any
			err = json.NewDecoder(resp.Body).Decode(&body)
			resp.Body.Close()
			if err != nil || resp.StatusCode != 200 {
				t.Fatalf("%s: status=%d body=%v error=%v", path, resp.StatusCode, body, err)
			}
			if path == "/overview" {
				body = body["dashboard"].(map[string]any)
			}
			entry := body["reports"].([]any)[0].(map[string]any)
			if entry["revision_ref"] != revision["ref"] || entry["revision_ref"] == "" {
				t.Fatalf("%s lost selected head identity: %v; want %v", path, entry, revision["ref"])
			}
			panel := entry["report"].(map[string]any)["panels"].([]any)[0].(map[string]any)
			if panel["title"] != title {
				t.Fatalf("%s definition disagrees with selected revision: %v", path, panel)
			}
		}
	}
	check("Original labels")
	previous := revision["ref"]
	_, _, err = store.UpdateDocument(ctx, "executive", anyString(doc["id"]), nil, anyString(previous), strings.Replace(report, "Original labels", "Revised labels", 1), "text", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, revision, err = store.GetDocument(ctx, anyString(doc["id"]))
	if err != nil {
		t.Fatal(err)
	}
	if revision["ref"] == previous {
		t.Fatal("fixture did not advance the revision")
	}
	check("Revised labels")
}

func TestOverviewArchivePinAndInitiativeProjection(t *testing.T) {
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"executive","display_name":"Alex","created_at":"2026-10-04T12:00:00Z","tags":["human"]}}`, 201).Body.Close()
	ctx := context.Background()
	store := h.primitiveStore.(*primitives.Store)
	active, err := store.CreateBoard(ctx, "executive", map[string]any{"title": "Demo · Initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	archived, err := store.CreateBoard(ctx, "executive", map[string]any{"title": "Old backlog"})
	if err != nil {
		t.Fatal(err)
	}
	var subject map[string]any
	for i := 0; i < 25; i++ {
		board := active
		if i >= 7 {
			board = archived
		}
		work, createErr := store.CreateWork(ctx, "executive", anyString(board["id"]), map[string]any{"title": fmt.Sprintf("Initiative %d", i), "summary": "Ship a useful outcome\n- [x] Design\n- [ ] Build\n```markdown\n- [x] Example\nNeeds ghost: example\n```\nNeeds Alex: choose a launch date\nNeeds Alice: approve\n- [ ]", "priority": "p1"})
		if createErr != nil {
			t.Fatal(createErr)
		}
		if i == 0 {
			subject = work
		}
	}
	if _, err = store.ArchiveBoard(ctx, "executive", anyString(archived["id"])); err != nil {
		t.Fatal(err)
	}
	topic, err := store.CreateTopic(ctx, "executive", map[string]any{"title": "Retired project", "summary": "Archived project context"})
	if err != nil {
		t.Fatal(err)
	}
	retired, err := store.CreateWork(ctx, "executive", anyString(active["id"]), map[string]any{"title": "Retired initiative", "project_ref": topic.Topic["ref"], "next_actor": "human"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.ArchiveTopic(ctx, "executive", anyString(topic.Topic["id"])); err != nil {
		t.Fatal(err)
	}
	hidden, err := store.HiddenSubjectRefs(ctx)
	if err != nil || !hidden[anyString(retired["ref"])] || !hidden["card:"+anyString(retired["id"])] || !hidden["thread:"+anyString(retired["thread_id"])] {
		t.Fatalf("retired project leaked: %v %v", retired, err)
	}
	reader := reportReader{r: httptest.NewRequest("GET", "/", nil), opts: handlerOptions{primitiveStore: store}, visibility: map[string]bool{}}
	if reader.activeRef(anyString(retired["ref"])) {
		t.Fatal("archived project card in live dashboard activity")
	}
	reader.loadEvents()
	if reader.eventsErr != nil {
		t.Fatal(reader.eventsErr)
	}
	if reader.activeEvent(map[string]any{"thread_id": retired["thread_id"]}) {
		t.Fatal("archived project backing thread in live dashboard activity")
	}
	live, _, err := reader.materialize(visualreport.Panel{Type: "live-initiatives", Query: visualreport.Query{Limit: 10, Sort: "priority"}})
	if err != nil || len(live["items"].([]map[string]any)) != 7 {
		t.Fatalf("archived project leaked into shared initiatives: %v %v", live, err)
	}
	report := `{"kind":"anx.visual-report","schema_version":1,"title":"Demo dashboard","summary":"Seven initiatives","generated_at":"2026-10-04T12:00:00Z","projects":[{"id":"demo","title":"Demo","summary":"Build","outcome":"Launch"}],"sources":[],"panels":[{"id":"note","project_id":"demo","type":"explanation","title":"Progress","author":"Test","provenance":"reported","observed_at":null,"freshness":"unavailable","source_ids":[],"data":{"text":"3 of 7 ready"}}]}`
	older, _, err := store.CreateDocument(ctx, "executive", map[string]any{"title": "Pinned dashboard"}, report, "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	newer, _, err := store.CreateDocument(ctx, "executive", map[string]any{"title": "Newest report"}, report, "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	broken := strings.Replace(report, `"projects":[{"id":"demo","title":"Demo","summary":"Build","outcome":"Launch"}]`, `"projects":[{}]`, 1)
	if _, _, err = store.CreateDocument(ctx, "executive", map[string]any{"title": "Invalid newest report"}, broken, "text", nil); err == nil {
		t.Fatal("stored an invalid visual report")
	}
	var reportErr *primitives.VisualReportValidationError
	if !errors.As(err, &reportErr) || len(reportErr.Errors) == 0 {
		t.Fatalf("invalid visual report write: %v", err)
	}
	read := func() map[string]any {
		t.Helper()
		resp, e := http.Get(h.baseURL + "/overview")
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		var body map[string]any
		if e = json.NewDecoder(resp.Body).Decode(&body); e != nil {
			t.Fatal(e)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("overview %d: %v", resp.StatusCode, body)
		}
		return body
	}
	for i := 0; i < 2; i++ {
		thread := anyString(active["thread_id"])
		event := map[string]any{"actor_id": "executive", "event": map[string]any{"type": "human_attention_requested", "thread_id": thread, "refs": []string{"thread:" + thread, anyString(active["ref"])}, "summary": fmt.Sprintf("Ask %d", i), "payload": map[string]any{"kind": "ask", "subject_ref": subject["ref"], "requester_actor_id": "executive", "response_proposals": []string{"Approve"}, "title": fmt.Sprintf("Ask %d", i), "request_id": fmt.Sprintf("ask-%d", i)}, "provenance": map[string]any{"sources": []string{"inferred"}}}}
		raw, _ := json.Marshal(event)
		postJSONExpectStatus(t, h.baseURL+"/events", string(raw), 201).Body.Close()
	}
	body := read()
	if body["needs_you"].(map[string]any)["count"] != float64(2) {
		t.Fatalf("asks absent: %v", body["needs_you"])
	}
	if got := body["work"].(map[string]any)["total"]; got != float64(7) {
		t.Fatalf("active count: %v", got)
	}
	initiatives := body["initiatives"].(map[string]any)["items"].([]any)
	if len(initiatives) != 7 {
		t.Fatal(initiatives)
	}
	first := initiatives[0].(map[string]any)
	progress := first["progress"].(map[string]any)
	if progress["done"] != float64(1) || progress["total"] != float64(3) || len(first["needs"].([]any)) != 2 || first["needs"].([]any)[0] != "Needs Alex: choose a launch date" || first["needs"].([]any)[1] != "Needs Alice: approve" {
		t.Fatal(first)
	}
	dashboard := func(b map[string]any) map[string]any {
		return b["dashboard"].(map[string]any)["reports"].([]any)[0].(map[string]any)
	}
	if dashboard(body)["id"] != newer["id"] {
		t.Fatalf("fallback is not newest: %v", body)
	}
	response, err := http.Get(h.baseURL + "/workspace/dashboard/reports")
	if err != nil {
		t.Fatal(err)
	}
	var candidates map[string]any
	if err = json.NewDecoder(response.Body).Decode(&candidates); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 || len(candidates["reports"].([]any)) != 2 {
		t.Fatalf("selector includes invalid report: %v", candidates)
	}
	pin := func(ref any, status int) {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"document_ref": ref, "actor_id": "executive"})
		req, _ := http.NewRequest(http.MethodPut, h.baseURL+"/workspace/dashboard", bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		resp, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		defer resp.Body.Close()
		if resp.StatusCode != status {
			var b any
			json.NewDecoder(resp.Body).Decode(&b)
			t.Fatalf("pin: %d %v", resp.StatusCode, b)
		}
	}
	pin(older["ref"], 200)
	if dashboard(read())["id"] != older["id"] {
		t.Fatal("pin was not persisted")
	}
	ordinary, _, err := store.CreateDocument(ctx, "executive", map[string]any{"title": "Notes"}, "Not a visual report", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	pin(ordinary["ref"], 400)
	if _, _, err = store.ArchiveDocument(ctx, "executive", anyString(older["id"])); err != nil {
		t.Fatal(err)
	}
	if dashboard(read())["id"] != newer["id"] {
		t.Fatal("archived pin did not fall back")
	}
	pin(nil, 200)
	cards, err := store.ListCards(ctx, primitives.CardListFilter{States: []string{"archived"}})
	if err != nil || len(cards) != 19 {
		t.Fatalf("archive lost cards: %d %v", len(cards), err)
	}
	groups, _, err := store.ListHomeUnread(ctx, "executive")
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range groups {
		if strings.Contains(g.DisplayName, "Old backlog") {
			t.Fatal("archived board in Watching")
		}
		for _, event := range g.Events {
			refs, _ := extractStringSlice(event["refs"])
			for _, ref := range refs {
				if hidden[ref] {
					t.Fatalf("archived project in Watching: %v", event)
				}
			}
		}
	}
}

/*
The morning brief, through the route that serves it.

The unit tests in overview_brief_test.go cover the ranking and the reasons.
What only the handler can prove is that the brief is assembled from the
sections this request actually produced: a decision it ranks is a needs_you
row the caller was already given, and a planless initiative arrives at the
client saying so instead of reporting on_track.
*/
func TestOverviewBriefRanksOnlyAuthorizedRowsAndTellsTheTruthAboutPlans(t *testing.T) {
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"executive","display_name":"Alex","created_at":"2026-10-04T12:00:00Z","tags":["human"]}}`, 201).Body.Close()
	ctx := context.Background()
	store := h.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, "executive", map[string]any{"title": "Initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.UpdateBoard(ctx, "executive", anyString(board["id"]), map[string]any{"role": "initiatives"}, nil); err != nil {
		t.Fatal(err)
	}
	gate, err := store.CreateWork(ctx, "executive", anyString(board["id"]), map[string]any{"title": "Approve the pricing change", "next_actor": "human", "next_action": "Pick a price", "priority": "p1"})
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Build the page", "Wire the API"} {
		if _, err = store.CreateWork(ctx, "executive", anyString(board["id"]), map[string]any{"title": title, "relations": []any{map[string]any{"kind": "depends_on", "ref": anyString(gate["ref"])}}}); err != nil {
			t.Fatal(err)
		}
	}
	resp, err := http.Get(h.baseURL + "/overview")
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	err = json.NewDecoder(resp.Body).Decode(&payload)
	resp.Body.Close()
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("status=%d error=%v", resp.StatusCode, err)
	}
	brief, ok := payload["brief"].(map[string]any)
	if !ok {
		t.Fatalf("overview has no brief: %v", payload)
	}

	// Every ranked decision is a row the caller was already handed. A brief
	// row with no needs_you row behind it would be a row the caller was not
	// authorized to see.
	authorized := map[string]bool{}
	for _, raw := range payload["needs_you"].(map[string]any)["rows"].([]any) {
		authorized[anyString(raw.(map[string]any)["id"])] = true
	}
	decisions := brief["decisions"].(map[string]any)["items"].([]any)
	if len(decisions) == 0 {
		t.Fatalf("brief ranked nothing from %d needs_you rows", len(authorized))
	}
	for _, raw := range decisions {
		item := raw.(map[string]any)
		if !authorized[anyString(item["id"])] {
			t.Fatalf("brief invented a decision row: %v", item)
		}
	}
	top := decisions[0].(map[string]any)
	if !strings.Contains(anyString(top["reason"]), "blocks 2 cards") {
		t.Fatalf("the row two cards wait on did not rank first with its reason: %v", top)
	}

	// The health lie, end to end: no plan must not arrive as on_track in
	// either the legacy status field or the brief.
	for _, raw := range payload["initiatives"].(map[string]any)["items"].([]any) {
		health := raw.(map[string]any)["health"].(map[string]any)
		if anyString(health["state"]) == "no_plan" && anyString(health["status"]) != "no_plan" {
			t.Fatalf("planless initiative still reports %q: %v", health["status"], health)
		}
	}
	byState := brief["initiatives"].(map[string]any)["by_state"].(map[string]any)
	if byState["no_plan"] == nil {
		t.Fatalf("brief does not count the planless initiatives: %v", byState)
	}
	if byState["on_track"] != nil {
		t.Fatalf("an unplanned workspace reported healthy initiatives: %v", byState)
	}
	machine := brief["machine"].(map[string]any)
	if anyString(machine["status"]) != "ok" || machine["finished_24h"] == nil {
		t.Fatalf("machine section is incomplete: %v", machine)
	}

	/*
	 * The brief gets its own Server-Timing phase, beside projection, roster,
	 * changes, inbox, decisions and visit. Hosted reads these per request, so
	 * a section that never reports its own cost is a section nobody can hold
	 * to the "no new per-request work" claim this change makes.
	 */
	phases := map[string]bool{}
	for _, value := range resp.Header.Values("Server-Timing") {
		for _, entry := range strings.Split(value, ",") {
			name, _, _ := strings.Cut(strings.TrimSpace(entry), ";")
			phases[name] = true
		}
	}
	if !phases["brief"] {
		t.Fatalf("no brief phase in Server-Timing: %v", resp.Header.Values("Server-Timing"))
	}
	for _, name := range []string{"projection", "roster", "changes", "inbox", "decisions"} {
		if !phases[name] {
			t.Fatalf("brief timing replaced the existing %s phase: %v", name, resp.Header.Values("Server-Timing"))
		}
	}
	t.Logf("Server-Timing: %v", resp.Header.Values("Server-Timing"))
}
