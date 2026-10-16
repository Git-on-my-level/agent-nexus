package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
	reports "agent-nexus-visualreport"
)

func TestReportActivityImpact(t *testing.T) {
	cases := []struct {
		name, kind           string
		p                    map[string]any
		impact, verb, detail string
	}{
		{"completed", "card_moved", map[string]any{"from_column_key": "in_progress", "column_key": "done"}, "high", "completed", "In progress → Done"},
		{"cancelled", "card_updated", map[string]any{"changed_fields": []string{"resolution"}, "previous_resolution": "done", "resolution": "cancelled"}, "high", "cancelled", "Done → Cancelled"},
		{"reopened", "card_moved", map[string]any{"from_column_key": "done", "column_key": "ready"}, "high", "reopened", "Done → Ready"},
		{"resolution cleared", "card_updated", map[string]any{"changed_fields": []string{"resolution"}, "previous_resolution": "done", "resolution": nil}, "high", "reopened", "Resolution cleared"},
		{"move", "card_moved", map[string]any{"from_column_key": "ready", "column_key": "in_progress"}, "high", "moved", "Ready → In progress"},
		{"reorder", "card_moved", map[string]any{"from_column_key": "ready", "column_key": "ready"}, "low", "moved", "Reordered"},
		{"ask", "human_attention_requested", nil, "high", "asked", ""},
		{"answer", "human_attention_responded", map[string]any{"response_text": "Approved"}, "high", "answered", "Approved"},
		{"plan step", "card_updated", map[string]any{"plan": plans.Plan{Steps: []plans.Step{{ID: "one", Status: "done"}}}}, "high", "updated the plan of", "1 step done · 1/1"},
		{"plan edit", "card_updated", map[string]any{"plan": plans.Plan{Steps: []plans.Step{{ID: "one"}}}}, "normal", "updated the plan of", "0/1 steps done"},
		{"already done plan step", "card_updated", map[string]any{"before_plan": plans.Plan{Steps: []plans.Step{{ID: "one", Status: "done"}}}, "plan": plans.Plan{Steps: []plans.Step{{ID: "one", Status: "done", Title: "New name"}}}}, "normal", "updated the plan of", "1/1 steps done"},
		{"plan promise", "card_updated", map[string]any{"plan": plans.Plan{Steps: []plans.Step{{ID: "one", Due: "2026-10-21"}}}}, "high", "updated the plan of", "promise moved to 2026-10-21"},
		{"promise", "message_posted", map[string]any{"next": map[string]any{"at": "2026-10-21"}}, "high", "set a promise for", "promise set for 2026-10-21"},
		{"promise annotation", "card_updated", map[string]any{"patch": map[string]any{"due_at": "2026-10-21"}}, "high", "changed the promise of", "promise moved to 2026-10-21"},
		{"title", "card_updated", map[string]any{"changed_fields": []string{"title"}}, "normal", "edited", "title changed"},
		{"assignee", "card_updated", map[string]any{"changed_fields": []string{"assignee_refs"}}, "normal", "edited", "assignee refs changed"},
		{"priority", "card_updated", map[string]any{"patch": map[string]any{"priority": "p1"}}, "normal", "edited", "priority changed"},
		{"cosmetic", "card_updated", map[string]any{"changed_fields": []string{"rank"}}, "low", "edited", ""},
		{"board metadata", "board_updated", map[string]any{"changed_fields": []string{"title"}}, "low", "edited", "Board metadata changed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			verb, detail, impact := classifyReportActivity(tc.kind, tc.p)
			if verb != tc.verb || impact != tc.impact || detail != tc.detail {
				t.Fatalf("got %q %q %q", verb, detail, impact)
			}
		})
	}
}
func TestReportActivityCoalescingAndMinimumImpact(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	event := func(id, actor, ref, kind string, minutes int, p map[string]any) map[string]any {
		return map[string]any{"ref": "event:" + id, "type": kind, "actor_id": actor, "refs": []string{ref}, "ts": now.Add(-time.Duration(minutes) * time.Minute).Format(time.RFC3339Nano), "summary": "Changed", "payload": p}
	}
	reader := reportReader{eventsRead: true, now: now, visibility: map[string]bool{"card:a": true, "card:b": true}, labels: map[string]string{"card:a": "Launch", "card:b": "Rollout"}, events: []map[string]any{
		event("cosmetic", "a", "card:a", "card_updated", 0, nil),
		event("move", "a", "card:a", "card_moved", 20, map[string]any{"from_column_key": "ready", "column_key": "in_progress"}),
		event("other-subject", "a", "card:b", "card_updated", 21, map[string]any{"changed_fields": []string{"title"}}),
		event("other-actor", "b", "card:a", "card_updated", 22, nil),
		event("boundary", "a", "card:a", "card_updated", 30, nil),
		event("outside", "a", "card:a", "card_updated", 31, nil),
	}}
	data, _, err := reader.activity(reports.Query{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	items := data["items"].([]map[string]any)
	if len(items) != 4 || items[0]["count"] != 3 || items[0]["impact"] != "high" || items[0]["verb"] != "moved" || items[0]["detail"] != "Ready → In progress" {
		t.Fatalf("bad coalescing: %#v", items)
	}
	for _, tc := range []struct {
		min  string
		want int
	}{{"low", 4}, {"normal", 2}, {"high", 1}} {
		data, _, err = reader.activity(reports.Query{Limit: 10, MinImpact: tc.min})
		if err != nil || len(data["items"].([]map[string]any)) != tc.want {
			t.Fatalf("min %s: %#v %v", tc.min, data, err)
		}
	}
	data, partial, err := reader.activity(reports.Query{Limit: 1, MinImpact: "normal"})
	if err != nil || !partial || len(data["items"].([]map[string]any)) != 1 {
		t.Fatalf("filter/limit: %#v %v", data, err)
	}
}
func TestReportActivityAnswerIsShortPlainText(t *testing.T) {
	value := activityPlainText("<b>Approved</b> **ship** [release](https://example.test/release)\n"+strings.Repeat("界", 160), 140)
	if len([]rune(value)) > 140 || strings.ContainsAny(value, "<>*_[]\n") || strings.Contains(value, "https://") {
		t.Fatalf("not a plain excerpt: %q", value)
	}
}
func TestReportActivityPlanSubjectAndPrivateTitle(t *testing.T) {
	t.Parallel()
	h := newPrimitivesTestServer(t)
	store := h.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	board, err := store.CreateBoard(ctx, "actor", map[string]any{"title": "Portfolio", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateWork(ctx, "actor", anyString(board["id"]), map[string]any{"title": "Launch search"})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.SetCardPlan(ctx, "planner", anyString(card["id"]), anyString(card["updated_at"]), plans.Plan{Steps: []plans.Step{{ID: "ship", Title: "Ship", After: []string{}}}}); err != nil {
		t.Fatal(err)
	}
	registry := actors.NewStore(h.workspace.DB())
	if _, err = registry.Register(ctx, actors.Actor{ID: "planner", DisplayName: "Alex", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/", nil).WithContext(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "reader"}))
	reader := reportReader{r: request, opts: handlerOptions{primitiveStore: store, actorRegistry: registry}, now: time.Now(), visibility: map[string]bool{}}
	data, _, err := reader.activity(reports.Query{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	items := data["items"].([]map[string]any)
	if len(items) == 0 {
		t.Fatal("plan event missing")
	}
	found := false
	for _, item := range items {
		if item["verb"] == "updated the plan of" {
			found = true
			subject := activityMap(item["subject"])
			if subject["ref"] != card["ref"] || subject["title"] != "Launch search" || subject["kind"] != "initiative" || item["actor_label"] != "Alex" {
				t.Fatalf("plan subject: %#v", item)
			}
		}
	}
	if !found {
		t.Fatalf("plan event missing: %#v", items)
	}
	// A promise note carrying only its thread must still name the initiative.
	facts, err := store.ReportActivitySubjects(request.Context(), []string{"thread:" + anyString(card["thread_id"])}, planVisibility(request, reader.opts))
	if err != nil || len(facts) != 1 || facts[0].Title != "Launch search" || facts[0].Kind != "initiative" {
		t.Fatalf("thread subject: %#v %v", facts, err)
	}
	const answerExcerpt = "Approved: internal launch only"
	ask, err := store.AppendEvent(ctx, "requester", map[string]any{
		"type": "human_attention_requested", "thread_id": card["thread_id"], "refs": []string{anyString(card["ref"])},
		"payload": map[string]any{"kind": "ask", "title": "Approve the private launch?", "subject_ref": card["ref"]},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.AppendEvent(ctx, "planner", map[string]any{
		"type": "human_attention_responded", "thread_id": card["thread_id"], "refs": []string{anyString(ask["ref"])},
		"payload": map[string]any{"request_event_ref": ask["ref"], "response_text": answerExcerpt},
	}); err != nil {
		t.Fatal(err)
	}
	reader = reportReader{r: request, opts: handlerOptions{primitiveStore: store, actorRegistry: registry}, now: time.Now(), visibility: map[string]bool{}}
	data, _, err = reader.activity(reports.Query{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	visibleJSON, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{answerExcerpt, "Alex"} {
		if !strings.Contains(string(visibleJSON), want) {
			t.Fatalf("missing visible answer control %q: %s", want, visibleJSON)
		}
	}
	if _, err = store.PatchThread(ctx, "actor", anyString(card["thread_id"]), map[string]any{"pm_actor_id": "private-owner"}, nil); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest("GET", "/", nil).WithContext(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"}))
	reader = reportReader{r: request, opts: handlerOptions{primitiveStore: store, actorRegistry: registry}, now: time.Now(), visibility: map[string]bool{}}
	data, _, err = reader.activity(reports.Query{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(data)
	for _, secret := range []string{"Launch search", answerExcerpt, "Alex"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("private activity leaked %q: %s", secret, raw)
		}
	}
	facts, err = store.ReportActivitySubjects(request.Context(), []string{anyString(card["ref"])}, planVisibility(request, reader.opts))
	if err != nil || len(facts) != 1 || facts[0].Resolvable || facts[0].Title != "" {
		t.Fatalf("private subject leaked: %#v %v", facts, err)
	}
}

func TestReportActivityDoesNotMutateSharedDecisionCounts(t *testing.T) {
	now := time.Now().UTC()
	reader := reportReader{eventsRead: true, decisionsRead: true, decisions: []map[string]any{}}
	for i := range 2 {
		reader.decisions = append(reader.decisions, map[string]any{"ref": fmt.Sprintf("decision:%d", i), "type": "decision_proposed", "actor_id": "pm", "ts": now.Add(-time.Duration(i) * time.Minute).Format(time.RFC3339Nano), "count": 1, "impact": "normal", "verb": "requested a decision on", "detail": "", "subject": map[string]any{"ref": "card:launch", "kind": "initiative", "title": "Launch"}})
	}
	for range 2 {
		data, _, err := reader.activity(reports.Query{Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		items := data["items"].([]map[string]any)
		if len(items) != 1 || items[0]["count"] != 2 {
			t.Fatalf("cached decisions mutated: %#v", data)
		}
	}
}

func TestReportActivityStandaloneAskSubjects(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	reader := reportReader{eventsRead: true, events: []map[string]any{
		{"id": "q1", "ref": "event:q1", "type": "human_attention_requested", "actor_id": "requester", "ts": now, "payload": map[string]any{"title": "Ship on Friday?"}},
		{"ref": "event:a1", "type": "human_attention_responded", "actor_id": "human", "ts": now, "payload": map[string]any{"request_event_ref": "event:q1", "response_text": "Yes"}},
		{"ref": "event:missing", "type": "human_attention_responded", "actor_id": "other", "ts": now, "payload": map[string]any{"request_event_ref": "event:outside-window"}},
	}}
	data, _, err := reader.activity(reports.Query{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range data["items"].([]map[string]any) {
		subject := activityMap(item["subject"])
		if item["ref"] == "event:missing" {
			if subject != nil {
				t.Fatalf("invented missing ask: %#v", item)
			}
			continue
		}
		if subject["kind"] != "ask" || subject["title"] != "Ship on Friday?" || subject["ref"] != "inbox:q1" {
			t.Fatalf("ask subject: %#v", item)
		}
	}
}

func TestReportActivityEqualImpactKeepsNewestAction(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, kind, verb, detail string
		payload                  map[string]any
	}{
		{"newer reopening", "card_moved", "reopened", "Done → Ready", map[string]any{"from_column_key": "done", "column_key": "ready"}},
		{"newer empty detail", "human_attention_requested", "asked", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newest := now.Format(time.RFC3339Nano)
			reader := reportReader{eventsRead: true, visibility: map[string]bool{"card:launch": true}, labels: map[string]string{"card:launch": "Launch"}, events: []map[string]any{
				{"ref": "event:older", "type": "card_moved", "actor_id": "agent", "refs": []string{"card:launch"}, "ts": now.Add(-20 * time.Minute).Format(time.RFC3339Nano), "summary": "Older move", "payload": map[string]any{"from_column_key": "ready", "column_key": "in_progress"}},
				{"ref": "event:newer", "type": tc.kind, "actor_id": "agent", "refs": []string{"card:launch"}, "ts": newest, "summary": "Newest action", "payload": tc.payload},
			}}
			data, _, err := reader.activity(reports.Query{Limit: 10})
			if err != nil {
				t.Fatal(err)
			}
			items := data["items"].([]map[string]any)
			if len(items) != 1 {
				t.Fatalf("burst not coalesced: %#v", items)
			}
			item := items[0]
			for key, want := range map[string]any{"count": 2, "impact": "high", "verb": tc.verb, "detail": tc.detail, "summary": "Newest action", "ts": newest, "ref": "event:newer"} {
				if item[key] != want {
					t.Errorf("%s = %#v, want %#v in %#v", key, item[key], want, item)
				}
			}
		})
	}
}

func TestReportActivityHiddenAskResponseWithOnlyRequestRef(t *testing.T) {
	t.Parallel()
	h := newPrimitivesTestServer(t)
	store := h.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	card, err := store.CreateWork(ctx, "owner", "", map[string]any{"title": "Confidential launch initiative"})
	if err != nil {
		t.Fatal(err)
	}
	ask, err := store.AppendEvent(ctx, "requester", map[string]any{
		"type": "human_attention_requested", "thread_id": card["thread_id"], "refs": []string{},
		"summary": "Confidential launch approval", "payload": map[string]any{"kind": "ask", "title": "Confidential launch approval"},
	})
	if err != nil {
		t.Fatal(err)
	}
	const excerpt = "Approved: keep the launch embargoed"
	response, err := store.AppendEvent(ctx, "answerer", map[string]any{
		"type": "human_attention_responded", "refs": []string{},
		"payload": map[string]any{"request_event_ref": ask["ref"], "response_text": excerpt},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Only the request's thread becomes private. The response has no thread,
	// subject_ref or refs; its sole ownership input is request_event_ref.
	if _, err = store.PatchThread(ctx, "owner", anyString(card["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	registry := actors.NewStore(h.workspace.DB())
	if _, err = registry.Register(ctx, actors.Actor{ID: "answerer", DisplayName: "Alex", CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatal(err)
	}
	render := func(actor string) (map[string]any, *http.Request) {
		t.Helper()
		request := httptest.NewRequest("GET", "/", nil).WithContext(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: actor}))
		cacheAuthenticatedPrincipal(request, &auth.Principal{ActorID: actor})
		reader := reportReader{r: request, opts: handlerOptions{primitiveStore: store, actorRegistry: registry}, now: time.Now(), visibility: map[string]bool{}}
		data, _, err := reader.activity(reports.Query{Limit: 20})
		if err != nil {
			t.Fatal(err)
		}
		return data, request
	}
	visible, _ := render("owner")
	raw, err := json.Marshal(visible)
	if err != nil {
		t.Fatal(err)
	}
	secrets := []string{"Confidential launch approval", excerpt, "Alex", anyString(ask["ref"]), anyString(response["ref"])}
	for _, want := range secrets {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("missing authorized control %q: %s", want, raw)
		}
	}
	hidden, request := render("stranger")
	for _, event := range []map[string]any{ask, response} {
		if _, err := store.GetEvent(request.Context(), anyString(event["id"])); !errors.Is(err, primitives.ErrNotFound) {
			t.Fatalf("event %s must be access-denied, got %v", event["ref"], err)
		}
	}
	raw, err = json.Marshal(hidden)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range append(secrets, "Confidential launch initiative", "answerer") {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("hidden ask leaked %q: %s", secret, raw)
		}
	}
	if items := hidden["items"].([]map[string]any); len(items) != 0 {
		t.Fatalf("hidden request/response produced activity: %#v", items)
	}
}
