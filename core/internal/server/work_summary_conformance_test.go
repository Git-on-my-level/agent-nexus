package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
)

func TestWorkSummaryCardReadConformanceAndLegacyProse(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	ctx := context.Background()
	s := h.primitiveStore.(*primitives.Store)
	b, err := s.CreateBoard(ctx, "actor-1", map[string]any{"title": "Portfolio", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateWork(ctx, "actor-1", anyString(b["id"]), map[string]any{"title": "Shared summary", "summary": "Preserve this prose", "phase": "in_progress"})
	if err != nil {
		t.Fatal(err)
	}
	p := plans.Plan{Steps: []plans.Step{{ID: "done", Title: "Finished", Status: "done"}, {ID: "blocked", Title: "Waiting", Status: "blocked"}, {ID: "next", Title: "Next"}, {ID: "other", Title: "Other"}}}
	if err = s.SetCardPlan(ctx, "actor-1", anyString(w["id"]), anyString(w["updated_at"]), p); err != nil {
		t.Fatal(err)
	}
	paths := []string{"/work", "/work?q=Shared", "/work/" + anyString(w["ref"]), "/cards", "/cards/" + anyString(w["ref"]), "/boards/" + anyString(b["id"]) + "/cards", "/boards/" + anyString(b["id"]) + "/cards/" + anyString(w["ref"]), "/overview", "/overview?work_view=summary"}
	for _, path := range paths {
		for _, optIn := range []bool{false, true} {
			target := path
			if optIn {
				separator := "?"
				if strings.Contains(target, "?") {
					separator = "&"
				}
				target += separator + "summary=1"
			}
			t.Run(target, func(t *testing.T) {
				body := workGetJSON(t, h.baseURL+target, 200)
				cards := collectSummaryCards(body)
				if !optIn {
					if len(cards) != 0 {
						t.Fatal("legacy response gained computed fields", body)
					}
					assertLegacyCardShape(t, body)
					return
				}
				if len(cards) == 0 {
					t.Fatalf("no card summary in %s: %v", target, body)
				}
				for _, card := range cards {
					summary, ok := card["work_summary"].(map[string]any)
					if !ok {
						t.Fatal("missing work_summary", card)
					}
					status, ok := summary["status"].(map[string]any)
					if !ok || status["state"] != "blocked" || status["label"] != "Blocked" || anyString(status["reason"]) == "" {
						t.Fatal("status disagrees", summary)
					}
					progress, ok := summary["progress"].(map[string]any)
					if !ok || progress["done"] != float64(1) || progress["total"] != float64(4) || progress["unit"] != "steps" {
						t.Fatal("progress disagrees", summary)
					}
					if _, ok := summary["steps"].(map[string]any); !ok {
						t.Fatal("missing folded digest", summary)
					}
					if optIn {
						if _, ok := card["summary"].(map[string]any); !ok {
							t.Fatal("opt-in summary is not computed", card)
						}
					} else if card["summary"] != "Preserve this prose" {
						t.Fatal("legacy prose changed", card)
					}
				}
			})
		}
	}
	refs := planRequest(t, "POST", h.baseURL+"/refs/resolve", map[string]any{"refs": []string{anyString(w["ref"]), "card:" + anyString(w["id"])}}, 200)
	for _, raw := range refs["items"].([]any) {
		row := raw.(map[string]any)
		if row["summary"].(map[string]any)["status"].(map[string]any)["state"] != "blocked" {
			t.Fatal(row)
		}
	}
}
func collectSummaryCards(value any) []map[string]any {
	out := []map[string]any{}
	switch v := value.(type) {
	case map[string]any:
		if _, ok := v["work_summary"]; ok {
			return []map[string]any{v}
		}
		for _, child := range v {
			out = append(out, collectSummaryCards(child)...)
		}
	case []any:
		for _, child := range v {
			out = append(out, collectSummaryCards(child)...)
		}
	}
	return out
}

func TestInboxRelatedCardsCarryComputedSummaryAndPrivacy(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	s := h.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	public, err := s.CreateWork(ctx, "actor-1", "", map[string]any{"title": "Related public", "phase": "blocked"})
	if err != nil {
		t.Fatal(err)
	}
	private, err := s.CreateWork(ctx, "actor-1", "", map[string]any{"title": "Related secret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "actor-1", anyString(private["thread_id"]), map[string]any{"pm_actor_id": "other-owner"}, nil); err != nil {
		t.Fatal(err)
	}
	// The inbox page has already been authorized. The related-card loader must
	// still use its own scoped resolver for every title and computed part.
	req := httptest.NewRequest("GET", "/inbox", nil).WithContext(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "reader"}))
	items := []map[string]any{{"subject_ref": public["ref"], "related_refs": []any{private["ref"]}}}
	if err = enrichInboxCardSummaries(req, handlerOptions{primitiveStore: s}, items); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(items)
	if strings.Contains(string(raw), "Related secret") {
		t.Fatal("private related title leaked", string(raw))
	}
	related := items[0]["related_cards"].([]primitives.RefPreview)
	if len(related) != 1 || related[0].Summary == nil || related[0].Summary.Status.State != "blocked" {
		t.Fatal(items)
	}
}

func TestCardSummaryPageLimitsAndCursors(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	s := h.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	b, err := s.CreateBoard(ctx, "actor-1", map[string]any{"title": "Pages"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err = s.CreateWork(ctx, "actor-1", anyString(b["id"]), map[string]any{"title": "Page card"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, base := range []string{"/cards", "/boards/" + anyString(b["id"]) + "/cards"} {
		first := workGetJSON(t, h.baseURL+base+"?summary=1&limit=2", 200)
		cards := first["cards"].([]any)
		cursor := anyString(first["next_cursor"])
		if len(cards) != 2 || cursor == "" {
			t.Fatal(first)
		}
		second := workGetJSON(t, h.baseURL+base+"?summary=1&limit=2&cursor="+cursor, 200)
		rest := second["cards"].([]any)
		if len(rest) != 1 || second["next_cursor"] != "" {
			t.Fatal(second)
		}
		for _, card := range cards {
			if card.(map[string]any)["ref"] == rest[0].(map[string]any)["ref"] {
				t.Fatal("cursor repeated card")
			}
		}
		workGetJSON(t, h.baseURL+base+"?summary=1&limit=51", 400)
	}
}
