package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
)

func assertLegacyCardShape(t *testing.T, value any) {
	t.Helper()
	switch v := value.(type) {
	case map[string]any:
		for _, key := range []string{"work_summary", "summary_text", "cards_truncated"} {
			if _, ok := v[key]; ok {
				t.Fatalf("default response gained %s", key)
			}
		}
		for _, child := range v {
			assertLegacyCardShape(t, child)
		}
	case []any:
		for _, child := range v {
			assertLegacyCardShape(t, child)
		}
	}
}

type countedSummaryStore struct {
	*primitives.Store
	candidates int
}

func (s *countedSummaryStore) EnrichCardPlans(ctx context.Context, cards []map[string]any, visible func(string, string) bool, now time.Time, threshold time.Duration) error {
	s.candidates += len(cards)
	return s.Store.EnrichCardPlans(ctx, cards, visible, now, threshold)
}

func TestSummaryBundlesBoundCandidatesAndLegacyArchiveIsComplete(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	s := h.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	b, err := s.CreateBoard(ctx, "actor-1", map[string]any{"title": "Large bundle"})
	if err != nil {
		t.Fatal(err)
	}
	cards := []map[string]any{}
	refs := []string{}
	for i := 0; i < 121; i++ {
		card, err := s.CreateWork(ctx, "actor-1", anyString(b["id"]), map[string]any{"title": fmt.Sprintf("Bundle %03d", i), "summary": "Legacy prose"})
		if err != nil {
			t.Fatal(err)
		}
		cards = append(cards, card)
		refs = append(refs, "card:"+anyString(card["id"]))
	}
	topic, err := s.CreateTopic(ctx, "actor-1", map[string]any{"title": "Large topic", "summary": "Legacy topic", "board_refs": []string{anyString(b["ref"])}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AppendEvent(ctx, "actor-1", map[string]any{"type": "card_updated", "thread_id": cards[0]["thread_id"], "refs": refs, "payload": map[string]any{"card_id": cards[0]["id"]}}); err != nil {
		t.Fatal(err)
	}
	spy := &countedSummaryStore{Store: s}
	opts := handlerOptions{primitiveStore: spy, summaryFormat: true}
	for _, build := range []func(context.Context, handlerOptions, string) (map[string]any, error){buildTopicWorkspacePayload, buildTopicTimelinePayload} {
		spy.candidates = 0
		body, err := build(ctx, opts, anyString(topic.Topic["id"]))
		if err != nil {
			t.Fatal(err)
		}
		if spy.candidates != summaryCandidateLimit || len(body["cards"].([]map[string]any)) != summaryCandidateLimit || body["cards_truncated"] != true {
			t.Fatalf("unbounded topic enrichment: %d, %v", spy.candidates, body["cards_truncated"])
		}
		spy.candidates = 0
		legacy, err := build(ctx, handlerOptions{primitiveStore: spy}, anyString(topic.Topic["id"]))
		if err != nil {
			t.Fatal(err)
		}
		if spy.candidates != 0 || len(legacy["cards"].([]map[string]any)) != len(cards) {
			t.Fatalf("legacy bundle changed: summaries=%d cards=%d", spy.candidates, len(legacy["cards"].([]map[string]any)))
		}
	}
	spy.candidates = 0
	recorder := httptest.NewRecorder()
	handleGetCardTimeline(recorder, httptest.NewRequest("GET", "/cards/"+anyString(cards[0]["id"])+"/timeline?summary=1", nil), opts, anyString(cards[0]["id"]))
	if recorder.Code != 200 {
		t.Fatal(recorder.Code, recorder.Body.String())
	}
	var timeline map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &timeline); err != nil {
		t.Fatal(err)
	}
	if spy.candidates < summaryCandidateLimit-1 || spy.candidates > summaryCandidateLimit || len(timeline["cards"].([]any)) > summaryCandidateLimit || timeline["cards_truncated"] != true {
		t.Fatal("unbounded timeline", spy.candidates, timeline["cards_truncated"])
	}
	if timeline["card"].(map[string]any)["work_summary"] == nil {
		t.Fatal("timeline lost its primary card summary")
	}
	for _, path := range []string{"/cards", "/boards/" + anyString(b["id"]) + "/cards", "/topics/" + anyString(topic.Topic["id"]) + "/workspace", "/topics/" + anyString(topic.Topic["id"]) + "/timeline", "/cards/" + anyString(cards[0]["id"]) + "/timeline", "/boards/" + anyString(b["id"]) + "/workspace"} {
		body := workGetJSON(t, h.baseURL+path, 200)
		assertLegacyCardShape(t, body)
		if strings.HasSuffix(path, "/cards") && len(body["cards"].([]any)) != len(cards) {
			t.Fatal("legacy collection lost cards", path)
		}
		if strings.HasSuffix(path, "/cards") {
			if _, ok := body["next_cursor"]; ok {
				t.Fatal("default collection gained cursor")
			}
		}
	}
	for _, card := range cards {
		if _, err = s.ArchiveBoardCard(ctx, "actor-1", anyString(b["id"]), anyString(card["id"]), primitives.RemoveBoardCardInput{}); err != nil {
			t.Fatal(err)
		}
	}
	archive := workGetJSON(t, h.baseURL+"/cards?state=archived&limit=1", 200)
	if len(archive["cards"].([]any)) != len(cards) {
		t.Fatalf("archive disappeared: %d of %d", len(archive["cards"].([]any)), len(cards))
	}
	assertLegacyCardShape(t, archive)
	archivedTimeline := workGetJSON(t, h.baseURL+"/cards/"+anyString(cards[0]["id"])+"/timeline?summary=1", 200)
	if _, ok := archivedTimeline["card"].(map[string]any)["summary"].(map[string]any); !ok {
		t.Fatal("archived primary card lost its summary")
	}
}

func TestInboxSummaryStreamIdleAcrossElapsedAgesAndHealthThreshold(t *testing.T) {
	// Serial: this file mutates process-wide environment, logging or counters.
	requireIntegrationTest(t)
	t.Setenv("ANX_PLAN_STALLED_AFTER", "1s")
	h := newMetaStreamTestHarness(t, WithStreamPollInterval(50*time.Millisecond))
	s := h.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	card, err := s.CreateWork(ctx, "idle-owner", "", map[string]any{"title": "Unchanged summary", "phase": "in_progress"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetCardPlan(ctx, "idle-owner", anyString(card["id"]), anyString(card["updated_at"]), plans.Plan{Steps: []plans.Step{{ID: "next", Title: "Next"}}}); err != nil {
		t.Fatal(err)
	}
	thread := anyString(card["thread_id"])
	item := streamPrivacyInboxItem(thread, "idle-summary", "Unchanged")
	item.Data["subject_ref"] = card["ref"]
	item.Data["related_refs"] = []any{card["ref"]}
	seedStreamPrivacyInbox(t, s, thread, item)
	resp := openSSEStream(t, h.baseURL+"/stream/inbox", "")
	events, stop := startSSEReader(resp.Body)
	defer stop()
	initial := awaitSSEEvent(t, events, 5*time.Second)
	if initial.Event != "inbox_item" || initial.Data["item"].(map[string]any)["related_cards"] == nil {
		t.Fatal("missing summary event", initial)
	}
	initialCards := initial.Data["item"].(map[string]any)["related_cards"].([]any)
	if initialCards[0].(map[string]any)["summary"].(map[string]any)["attention"] == nil {
		t.Fatal("fixture must exercise oldest_age as well as card age")
	}
	// The ages increment twice and computed health crosses its stale threshold.
	// Neither is a canonical write, so neither may reload the UI.
	select {
	case event := <-events:
		t.Fatalf("unchanged summary emitted event: %+v", event)
	case <-time.After(2200 * time.Millisecond):
	}
	current, err := s.GetWork(ctx, anyString(card["ref"]))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetCardPlan(ctx, "idle-owner", anyString(card["id"]), anyString(current["updated_at"]), plans.Plan{Steps: []plans.Step{{ID: "next", Title: "Next", Status: "blocked"}}}); err != nil {
		t.Fatal(err)
	}
	changed := awaitSSEEvent(t, events, 5*time.Second)
	if changed.Event != "inbox_item" || changed.ID == initial.ID {
		t.Fatal("meaningful plan edit did not emit", changed)
	}
	rows := changed.Data["item"].(map[string]any)["related_cards"].([]any)
	if rows[0].(map[string]any)["summary"].(map[string]any)["status"].(map[string]any)["state"] != "blocked" {
		t.Fatal("new event lost changed summary")
	}
}

func TestInboxChangeDigestRetainsPayloadAndAttentionAnchors(t *testing.T) {
	t.Parallel()
	age := int64(10)
	summary := &primitives.WorkSummary{Age: &age, CreatedAt: "2026-10-08T09:00:00Z", Attention: &primitives.SummaryAttention{Count: 1, OldestAge: 10, OldestAt: "2026-10-08T09:00:00Z"}}
	item := map[string]any{"id": "item", "age": uint64(9007199254740992), "related_cards": []primitives.RefPreview{{Ref: "card:visible", Summary: summary}}}
	first := buildInboxStreamRecords([]map[string]any{item})[0]
	age++
	summary.Attention.OldestAge++
	second := buildInboxStreamRecords([]map[string]any{item})[0]
	if first.eventID != second.eventID || *summary.Age != 11 || summary.Attention.OldestAge != 11 {
		t.Fatal("ages changed identity or payload was mutated")
	}
	summary.Attention.OldestAt = "2026-10-08T09:00:01Z"
	anchor := buildInboxStreamRecords([]map[string]any{item})[0]
	if anchor.eventID == second.eventID {
		t.Fatal("oldest-ask timestamp change was ignored")
	}
	item["age"] = uint64(9007199254740993)
	if buildInboxStreamRecords([]map[string]any{item})[0].eventID == anchor.eventID {
		t.Fatal("unrelated item data lost integer precision")
	}
}

func TestDefaultWorkAndOverviewRetainLegacyPlanlessHealth(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	s := h.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	b, err := s.CreateBoard(ctx, "actor-1", map[string]any{"title": "Health compatibility", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.CreateWork(ctx, "actor-1", anyString(b["id"]), map[string]any{"title": "Due soon", "summary": "Legacy prose", "phase": "in_progress", "due_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/work/" + anyString(card["ref"]), "/cards/" + anyString(card["ref"]), "/overview"} {
		legacy := workGetJSON(t, h.baseURL+path, 200)
		assertLegacyCardShape(t, legacy)
		var check func(any)
		check = func(value any) {
			switch v := value.(type) {
			case map[string]any:
				if v["title"] == "Due soon" {
					if health, ok := v["plan_health"].(map[string]any); ok && health["state"] != "no_plan" {
						t.Fatal("default health changed", path, health)
					}
				}
				for _, child := range v {
					check(child)
				}
			case []any:
				for _, child := range v {
					check(child)
				}
			}
		}
		check(legacy)
		computed := workGetJSON(t, h.baseURL+path+"?summary=1", 200)
		for _, row := range collectSummaryCards(computed) {
			if row["work_summary"].(map[string]any)["status"].(map[string]any)["state"] != "in_progress" {
				t.Fatal("near due date replaced planless workflow", path, row)
			}
		}
	}
}
