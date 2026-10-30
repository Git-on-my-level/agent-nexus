package primitives

import (
	"context"
	"testing"
	"time"

	"agent-nexus-core/internal/plans"
)

func TestBriefResolvedProgressRetainsPrivatePlanProvenance(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	privateBoard, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", anyStringValue(privateBoard["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	child, err := s.CreateWork(ctx, "owner", anyStringValue(privateBoard["id"]), map[string]any{"title": "Secret milestone"})
	if err != nil {
		t.Fatal(err)
	}
	publicBoard, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Public"})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := s.CreateWork(ctx, "owner", anyStringValue(publicBoard["id"]), map[string]any{"title": "Public initiative", "initiative": true})
	if err != nil {
		t.Fatal(err)
	}
	id := anyStringValue(parent["id"])
	if err = s.SetCardPlan(ctx, "owner", id, anyStringValue(parent["updated_at"]), plans.Plan{Steps: []plans.Step{{ID: "secret", Title: "Private step", Ref: anyStringValue(child["ref"]), Due: "2099-01-01"}}}); err != nil {
		t.Fatal(err)
	}
	refresh := func() {
		t.Helper()
		for drained := false; !drained; {
			drained, err = s.RefreshBriefInitiatives(ctx, 200)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	refresh()
	check := func(actor string, want bool) {
		t.Helper()
		scoped := WithAccessScope(ctx, AccessScope{ActorID: actor})
		if _, err = s.GetWork(scoped, id); err != nil {
			t.Fatalf("public card disappeared: %v", err)
		}
		var count int
		if err = s.db.QueryRowContext(scoped, `SELECT count(*) FROM brief_initiatives WHERE card_id=?`, id).Scan(&count); err != nil || (count == 1) != want {
			t.Fatalf("%s projection=%d want=%v err=%v", actor, count, want, err)
		}
		brief, err := s.Brief(scoped, BriefReader{ActorID: actor, Kind: "human"}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range brief["initiatives"].([]map[string]any) {
			if item["ref"] == parent["ref"] {
				found = true
			}
		}
		if found != want {
			t.Fatalf("%s brief leaked private plan: %#v", actor, brief)
		}
	}
	check("owner", true)
	check("stranger", false)
	check("unauthorized-agent", false)
	scoped := WithAccessScope(ctx, AccessScope{ActorID: "stranger"})
	var count int
	if err = s.db.QueryRowContext(scoped, `SELECT count(*) FROM brief_plan_refs WHERE card_id=?`, id).Scan(&count); err != nil || count != 0 {
		t.Fatal("private reverse refs exposed", count, err)
	}
	parent, err = s.GetBoardCard(ctx, "", id)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetCardPlan(ctx, "owner", id, anyStringValue(parent["updated_at"]), plans.Plan{Steps: []plans.Step{}}); err != nil {
		t.Fatal(err)
	}
	// Current plan is now public, but the old fold still contains private facts.
	check("stranger", false)
	check("unauthorized-agent", false)
	refresh()
	check("stranger", true)

	// Replacing a private completed step with a public completed step changes
	// no effective status. Its old private clock must survive the refold too.
	publicChild, err := s.CreateWork(ctx, "owner", anyStringValue(publicBoard["id"]), map[string]any{"title": "Public milestone"})
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := s.AppendEvent(ctx, "owner", map[string]any{"type": "completion_evidence", "refs": []string{}, "summary": "Reviewed"})
	if err != nil {
		t.Fatal(err)
	}
	refs := []string{"event:" + anyStringValue(evidence["id"])}
	resolution := "done"
	if _, err = s.MoveBoardCard(ctx, "owner", anyStringValue(publicBoard["id"]), anyStringValue(publicChild["id"]), MoveBoardCardInput{ColumnKey: "done", Resolution: &resolution, ResolutionRefs: &refs}); err != nil {
		t.Fatal(err)
	}
	set := func(ref string) {
		t.Helper()
		card, err := s.GetBoardCard(ctx, "", id)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.SetCardPlan(ctx, "owner", id, anyStringValue(card["updated_at"]), plans.Plan{Steps: []plans.Step{{ID: "secret", Title: "Milestone", Ref: ref, Due: "2099-01-01"}}}); err != nil {
			t.Fatal(err)
		}
	}
	set(anyStringValue(child["ref"]))
	refresh()
	if _, err = s.MoveBoardCard(ctx, "owner", anyStringValue(privateBoard["id"]), anyStringValue(child["id"]), MoveBoardCardInput{ColumnKey: "done", Resolution: &resolution, ResolutionRefs: &refs}); err != nil {
		t.Fatal(err)
	}
	refresh()
	set(anyStringValue(publicChild["ref"]))
	refresh()
	check("stranger", false)
	check("unauthorized-agent", false)
	var currentRef, clockRef string
	if err = ws.DB().QueryRow(`SELECT json_extract(plan_provenance_json,'$.current.steps[0].ref'),json_extract(plan_provenance_json,'$.clock.steps[0].ref') FROM brief_initiatives WHERE card_id=?`, id).Scan(&currentRef, &clockRef); err != nil {
		t.Fatal(err)
	}
	if currentRef != publicChild["ref"] || clockRef != child["ref"] {
		t.Fatalf("current=%s clock=%s", currentRef, clockRef)
	}
	// A subsequent public state change replaces the private clock's provenance.
	if _, err = s.MoveBoardCard(ctx, "owner", anyStringValue(publicBoard["id"]), anyStringValue(publicChild["id"]), MoveBoardCardInput{ColumnKey: "in_progress"}); err != nil {
		t.Fatal(err)
	}
	refresh()
	check("stranger", true)
}
