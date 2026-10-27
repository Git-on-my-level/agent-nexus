package primitives

import (
	"agent-nexus-core/internal/storage"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/plans"
)

func TestInitiativeBoardAuthorityRegression(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "Initiatives", "role": "initiatives", "initiative_target": 1})
	if err != nil {
		t.Fatal(err)
	}
	root, err := s.CreateWork(ctx, "writer", workString(board["id"]), map[string]any{"title": "Declared initiative"})
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "Tasks"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateWork(ctx, "writer", workString(tasks["id"]), map[string]any{"title": "New unrelated card"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.CreateWork(ctx, "writer", workString(tasks["id"]), map[string]any{"title": "Real child", "under": root["ref"]})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetCardPlan(ctx, "writer", workString(other["id"]), workString(other["updated_at"]), plans.Plan{Steps: []plans.Step{{ID: "dependency", Title: "Needs initiative", Ref: workString(root["ref"])}}}); err != nil {
		t.Fatal(err)
	}
	if err = s.EnrichWorkHierarchy(ctx, []map[string]any{root, other, child}, "writer", false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if root["parent_ref"] != nil || root["rollup"].(*WorkRollup).Total != 1 || other["rollup"].(*WorkRollup).Total != 0 || child["parent_ref"] != root["ref"] {
		t.Fatalf("root=%v other=%v child=%v", root, other, child)
	}
	tray, _, e := s.UngroupedWork(ctx, 50)
	if e != nil {
		t.Fatal(e)
	}
	for _, item := range tray {
		if item["ref"] == root["ref"] {
			t.Fatal("declared initiative entered ungrouped tray")
		}
		if item["ref"] == other["ref"] {
			found := false
			for _, a := range item["next_actions"].([]map[string]any) {
				if a["command_id"] == "initiative.promote" {
					found = true
				}
			}
			if !found {
				t.Fatal("tray still patches top_level instead of promoting")
			}
		}
	}
	for _, verb := range []string{"group", "fold"} {
		var e error
		if verb == "group" {
			_, e = s.GroupWork(ctx, "writer", workString(root["ref"]), workString(other["ref"]), "")
		} else {
			_, e = s.FoldWork(ctx, "writer", workString(root["ref"]), workString(other["ref"]), "")
		}
		if !errors.Is(e, ErrInvalidWorkRequest) || !strings.Contains(e.Error(), "anx initiative retire "+workString(root["ref"])) {
			t.Fatalf("%s accepted declared root: %v", verb, e)
		}
	}
	for done := false; !done; {
		done, err = s.RefreshBriefInitiatives(ctx, 200)
		if err != nil {
			t.Fatal(err)
		}
	}
	brief, err := s.Brief(ctx, BriefReader{ActorID: "writer", Kind: "agent"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	roots := brief["initiatives"].([]map[string]any)
	if len(roots) != 1 || roots[0]["ref"] != root["ref"] || brief["initiative_budget"].(map[string]any)["count"] != 1 {
		t.Fatalf("lost declared initiative: %v", brief)
	}
	// Clearing the role restores ordinary oldest-open-parent semantics.
	if _, err = s.UpdateBoard(ctx, "writer", workString(board["id"]), map[string]any{"role": ""}, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.EnrichWorkHierarchy(ctx, []map[string]any{root, other}, "writer", false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if root["parent_ref"] != other["ref"] || other["rollup"].(*WorkRollup).Total != 1 {
		t.Fatalf("fallback changed: root=%v other=%v", root, other)
	}
}

func TestInitiativeLifecycleFeedTargetAndPrivacy(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	b, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "Initiatives", "role": "initiatives", "initiative_target": 0})
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "Tasks"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateWork(ctx, "writer", workString(tasks["id"]), map[string]any{"title": "Launch"})
	if err != nil {
		t.Fatal(err)
	}
	promote, err := s.ChangeInitiative(ctx, "writer", workString(c["ref"]), "promote", "Independent outcome", "", "")
	if err != nil {
		t.Fatal(err)
	}
	fact := promote["over_target"].(map[string]any)
	if fact["count"] != 1 || fact["target"] != 0 || len(fact["next_actions"].([]map[string]any)) != 1 {
		t.Fatal(promote)
	}
	event := promote["event"].(map[string]any)
	payload := event["payload"].(map[string]any)
	if event["actor_id"] != "writer" || event["type"] != "initiative_promoted" || payload["reason"] != "Independent outcome" || payload["before"].(map[string]any)["initiative"] != false || payload["after"].(map[string]any)["initiative"] != true {
		t.Fatal(event)
	}
	for done := false; !done; {
		done, err = s.RefreshBriefInitiatives(ctx, 200)
		if err != nil {
			t.Fatal(err)
		}
	}
	brief, err := s.Brief(ctx, BriefReader{ActorID: "writer", Kind: "agent"}, time.Now())
	if err != nil || brief["over_target"] == nil {
		t.Fatalf("brief=%v err=%v", brief, err)
	}
	retired, err := s.ChangeInitiative(ctx, "writer", workString(c["ref"]), "retire", "Finished independent scope", workString(tasks["ref"]), "")
	if err != nil {
		t.Fatal(err)
	}
	if retired["event"].(map[string]any)["type"] != "initiative_retired" || retired["work"].(map[string]any)["board_ref"] != tasks["ref"] {
		t.Fatal(retired)
	}
	// Ordinary board moves emit membership changes without requiring a reason.
	if _, err = s.MoveBoardCard(ctx, "writer", "", workString(c["id"]), MoveBoardCardInput{ColumnKey: "ready", BoardRef: workString(b["ref"])}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.MoveBoardCard(ctx, "writer", "", workString(c["id"]), MoveBoardCardInput{ColumnKey: "ready", BoardRef: workString(tasks["ref"]), Reason: "Move out"}); err != nil {
		t.Fatal(err)
	}
	feed, err := s.InitiativeChanges(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed["items"].([]map[string]any)) != 4 {
		t.Fatal(feed)
	}
	private, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "Secret", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "writer", workString(private["thread_id"]), map[string]any{"pm_actor_id": "writer"}, nil); err != nil {
		t.Fatal(err)
	}
	hidden, err := s.CreateWork(ctx, "writer", workString(private["id"]), map[string]any{"title": "Secret initiative"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ChangeInitiative(ctx, "writer", workString(hidden["ref"]), "retire", "Confidential rationale", workString(tasks["ref"]), ""); err != nil {
		t.Fatal(err)
	}
	scoped := WithRequestAccessScope(ctx, AccessScope{ActorID: "stranger"})
	feed, err = s.InitiativeChanges(scoped, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(feed["items"].([]map[string]any)) != 4 {
		t.Fatalf("restricted feed leaked or suppressed public changes: %v", feed)
	}
	// Historical privacy persists through the before-board ref even after moving public.
	if feed["truncated"] != true {
		t.Fatal(feed)
	}
	if _, err = s.UpdateBoard(ctx, "writer", workString(b["id"]), map[string]any{"initiative_target": -1}, nil); err == nil {
		t.Fatal("negative target accepted")
	}
	updated, err := s.UpdateBoard(ctx, "writer", workString(b["id"]), map[string]any{"initiative_target": 3}, nil)
	if err != nil || updated["initiative_target"] != int64(3) {
		t.Fatalf("target patch=%v err=%v", updated, err)
	}
}

func TestInitiativeRetireUnderAtomic(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	b, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "Initiatives", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := s.CreateWork(ctx, "writer", workString(b["id"]), map[string]any{"title": "Parent"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateWork(ctx, "writer", workString(b["id"]), map[string]any{"title": "Child initiative"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ChangeInitiative(ctx, "writer", workString(c["ref"]), "retire", "Consolidate", "", workString(parent["ref"])); err != nil {
		t.Fatal(err)
	}
	if err = s.EnrichWorkHierarchy(ctx, []map[string]any{c, parent}, "writer", false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if c["parent_ref"] != parent["ref"] || parent["rollup"].(*WorkRollup).Total != 1 {
		t.Fatalf("retire under lost child: %v %v", c, parent)
	}
}

func TestInitiativeImplicitChildrenKeepHierarchyAndPrivacy(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	b, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "Private initiatives", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "writer", workString(b["thread_id"]), map[string]any{"pm_actor_id": "writer"}, nil); err != nil {
		t.Fatal(err)
	}
	parent, err := s.CreateWork(ctx, "writer", workString(b["id"]), map[string]any{"title": "Private outcome"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.CreateWork(ctx, "writer", "", map[string]any{"title": "Implicit child", "under": parent["ref"]})
	if err != nil {
		t.Fatal(err)
	}
	if child["board_ref"] == b["ref"] {
		t.Fatal("child inherited declaration board")
	}
	if err = s.EnrichWorkHierarchy(ctx, []map[string]any{parent, child}, "writer", false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if child["parent_ref"] != parent["ref"] || parent["rollup"].(*WorkRollup).Total != 1 {
		t.Fatalf("parent=%v child=%v", parent, child)
	}
	if _, err = s.GetWork(WithRequestAccessScope(ctx, AccessScope{ActorID: "stranger"}), workString(child["id"])); !errors.Is(err, ErrNotFound) {
		t.Fatalf("implicit child lost inherited privacy: %v", err)
	}
}

// Shape a released workspace like the curated board: five real initiatives,
// eighty grouped work cards and twenty-one loops sharing one primary board.
func TestInitiativeAuthorityUpgradePreservesMixedBoard(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	ws, err := initializeTestWorkspace(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "Outcomes"})
	if err != nil {
		t.Fatal(err)
	}
	roots := []map[string]any{}
	children := []map[string]any{}
	for i := 0; i < 5; i++ {
		root, e := s.CreateWork(ctx, "writer", workString(board["id"]), map[string]any{"title": fmt.Sprintf("Outcome %d", i)})
		if e != nil {
			t.Fatal(e)
		}
		roots = append(roots, root)
		for j := 0; j < 16; j++ {
			child, e := s.CreateWork(ctx, "writer", workString(board["id"]), map[string]any{"title": fmt.Sprintf("Work %d/%d", i, j), "under": root["ref"]})
			if e != nil {
				t.Fatal(e)
			}
			children = append(children, child)
		}
	}
	for i := 0; i < 21; i++ {
		if _, err = s.CreateWork(ctx, "writer", workString(board["id"]), map[string]any{"title": fmt.Sprintf("Loop %d", i), "cadence": map[string]any{"every_seconds": 3600}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.UpdateBoard(ctx, "writer", workString(board["id"]), map[string]any{"role": "initiatives"}, nil); err != nil {
		t.Fatal(err)
	}
	assertWatch := func() {
		t.Helper()
		for done := false; !done; {
			done, err = s.RefreshBriefInitiatives(ctx, 200)
			if err != nil {
				t.Fatal(err)
			}
		}
		brief, e := s.Brief(ctx, BriefReader{ActorID: "writer", Kind: "human"}, time.Now())
		if e != nil {
			t.Fatal(e)
		}
		items := brief["initiatives"].([]map[string]any)
		if len(items) != 5 || brief["initiative_budget"].(map[string]any)["count"] != 5 {
			t.Fatalf("mixed board expanded: roots=%d budget=%v", len(items), brief["initiative_budget"])
		}
		want := map[string]bool{}
		for _, root := range roots {
			want[workString(root["ref"])] = true
		}
		for _, item := range items {
			if !want[workString(item["ref"])] || item["rollup"].(*WorkRollup).Total != 16 {
				t.Fatalf("hierarchy changed: %v", item)
			}
		}
		if len(brief["loops"].([]map[string]any)) == 0 {
			t.Fatal("loops lost their separate section")
		}
	}
	assertWatch()
	// Reopen through the real migration, with the new protection column absent.
	for _, q := range []string{`DROP VIEW initiative_card_positions`, `DROP VIEW initiative_legacy_parent_positions`, `DROP VIEW initiative_change_positions`, `DROP INDEX idx_events_initiative_changes`, `ALTER TABLE cards DROP COLUMN initiative_root`, `ALTER TABLE boards DROP COLUMN initiative_target`, `DELETE FROM schema_migrations WHERE version=126`} {
		if _, err = ws.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err = storage.InitializeWorkspace(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s = NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	assertWatch()
	// Editing a retained legacy grouping reference must not declare its child.
	root, err := s.GetWork(ctx, workString(roots[0]["ref"]))
	if err != nil {
		t.Fatal(err)
	}
	p := plans.Plan{Steps: []plans.Step{}}
	for i, child := range children[:16] {
		p.Steps = append(p.Steps, plans.Step{ID: fmt.Sprintf("retained-%d", i), Title: "Retained work", Ref: workString(child["ref"])})
	}
	if err = s.SetCardPlan(ctx, "writer", workString(root["id"]), workString(root["updated_at"]), p); err != nil {
		t.Fatal(err)
	}
	assertWatch()
	tasks, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "Tasks"})
	if err != nil {
		t.Fatal(err)
	}
	// The incident's new-card inline plan is a dependency, even against an
	// unmarked pre-upgrade root. Plan creation and editing share the guard.
	if _, err = s.CreateWork(ctx, "writer", workString(tasks["id"]), map[string]any{"title": "New unrelated dependency", "plan": map[string]any{"steps": []any{map[string]any{"id": "needs", "title": "Needs outcome", "ref": root["ref"]}}}}); err != nil {
		t.Fatal(err)
	}
	assertWatch()
	for _, child := range children {
		work, e := s.GetWork(ctx, workString(child["ref"]))
		if e == nil {
			e = s.EnrichWorkHierarchy(ctx, []map[string]any{work}, "writer", false, time.Now())
		}
		if e != nil || work["parent_ref"] == nil {
			t.Fatalf("legacy child lost parent: %v %v", work, e)
		}
	}
}

func TestInitiativeNewLinkPreservesHiddenLegacyParent(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	b, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "Public outcomes"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateWork(ctx, "writer", workString(b["id"]), map[string]any{"title": "Legacy child"})
	if err != nil {
		t.Fatal(err)
	}
	private, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "Private parent"})
	if err != nil {
		t.Fatal(err)
	}
	parent, err := s.CreateWork(ctx, "writer", workString(private["id"]), map[string]any{"title": "Hidden parent", "plan": map[string]any{"steps": []any{map[string]any{"id": "child", "title": "Child", "ref": c["ref"]}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "writer", workString(private["thread_id"]), map[string]any{"pm_actor_id": "writer"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.UpdateBoard(ctx, "writer", workString(b["id"]), map[string]any{"role": "initiatives"}, nil); err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateWork(ctx, "stranger", "", map[string]any{"title": "Unrelated"})
	if err != nil {
		t.Fatal(err)
	}
	scoped := WithRequestAccessScope(ctx, AccessScope{ActorID: "stranger"})
	if err = s.SetCardPlan(scoped, "stranger", workString(other["id"]), workString(other["updated_at"]), plans.Plan{Steps: []plans.Step{{ID: "needs", Title: "Needs", Ref: workString(c["ref"])}}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetWork(ctx, workString(c["ref"]))
	if err == nil {
		err = s.EnrichWorkHierarchy(ctx, []map[string]any{got}, "writer", false, time.Now())
	}
	if err != nil || got["parent_ref"] != parent["ref"] {
		t.Fatalf("hidden parent displaced: %v %v", got, err)
	}
}

func TestCrossBoardMoveChecksBothBoardScopes(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	public, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "Public"})
	if err != nil {
		t.Fatal(err)
	}
	private, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "Private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "writer", workString(private["thread_id"]), map[string]any{"pm_actor_id": "writer"}, nil); err != nil {
		t.Fatal(err)
	}
	pub, err := s.CreateWork(ctx, "writer", workString(public["id"]), map[string]any{"title": "Public work"})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := s.CreateWork(ctx, "writer", workString(private["id"]), map[string]any{"title": "Secret work"})
	if err != nil {
		t.Fatal(err)
	}
	scoped := WithRequestAccessScope(ctx, AccessScope{ActorID: "stranger"})
	for _, tc := range []struct{ card, dest map[string]any }{{pub, private}, {secret, public}} {
		_, err = s.MoveBoardCard(scoped, "stranger", "", workString(tc.card["ref"]), MoveBoardCardInput{BoardRef: workString(tc.dest["ref"]), ColumnKey: "ready"})
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("cross-board privacy bypass: %v", err)
		}
		after, e := s.GetWork(ctx, workString(tc.card["ref"]))
		if e != nil || after["board_ref"] != tc.card["board_ref"] {
			t.Fatalf("rejected move changed membership: %v %v", after, e)
		}
	}
}

func TestInitiativeProtectedRootRemainsInTrends(t *testing.T) {
	f := newTrendsFixture(t)
	root := f.card("Outcome", "", nil)
	other := f.card("New dependency", "", nil)
	if err := f.store.SetCardPlan(f.ctx, "owner", workString(other["id"]), workString(other["updated_at"]), plans.Plan{Steps: []plans.Step{{ID: "needs", Title: "Needs outcome", Ref: workString(root["ref"])}}}); err != nil {
		t.Fatal(err)
	}
	for done := false; !done; {
		var err error
		done, err = f.store.RefreshBriefInitiatives(f.ctx, 200)
		if err != nil {
			t.Fatal(err)
		}
	}
	f.exec(`UPDATE brief_initiatives SET promise_at=NULL`)
	f.exec(`UPDATE brief_initiatives SET promise_at=? WHERE card_id=?`, loopTS(f.now.Add(-time.Hour)), root["id"])
	b := &trendsBuild{briefBuild: &briefBuild{s: f.store, ctx: f.ctx, now: f.now}, w: newTrendWindow(30, f.now)}
	promises, _, _, err := b.trendPromises()
	if err != nil || len(promises) != 1 {
		t.Fatalf("protected root excluded from trends: %v %v", promises, err)
	}
}

func TestInitiativeRetireUnderRejectsCompetingEarlierParentAtomically(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	b, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "Outcomes", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "Tasks"})
	if err != nil {
		t.Fatal(err)
	}
	earlier, err := s.CreateWork(ctx, "writer", workString(tasks["id"]), map[string]any{"title": "Earlier dependency"})
	if err != nil {
		t.Fatal(err)
	}
	under, err := s.CreateWork(ctx, "writer", workString(b["id"]), map[string]any{"title": "Intended parent"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.CreateWork(ctx, "writer", workString(b["id"]), map[string]any{"title": "Outcome"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetCardPlan(ctx, "writer", workString(earlier["id"]), workString(earlier["updated_at"]), plans.Plan{Steps: []plans.Step{{ID: "dependency", Title: "Needs outcome", Ref: workString(child["ref"])}}}); err != nil {
		t.Fatal(err)
	}
	_, err = s.ChangeInitiative(ctx, "writer", workString(child["ref"]), "retire", "Consolidate", workString(tasks["ref"]), workString(under["ref"]))
	if !errors.Is(err, ErrInvalidWorkRequest) || !strings.Contains(err.Error(), "earlier plan links") {
		t.Fatalf("silently selected the wrong parent: %v", err)
	}
	after, err := s.GetWork(ctx, workString(child["ref"]))
	if err != nil || after["board_ref"] != b["ref"] {
		t.Fatalf("rejected retirement committed: %v %v", after, err)
	}
	feed, err := s.InitiativeChanges(ctx, 50)
	if err != nil || len(feed["items"].([]map[string]any)) != 0 {
		t.Fatalf("rejected retirement emitted event: %v %v", feed, err)
	}
}
