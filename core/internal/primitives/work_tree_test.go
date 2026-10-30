package primitives

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/storage"
)

func newWorkTreeTestStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	return NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir), ctx
}

func mustWork(t *testing.T, s *Store, ctx context.Context, input map[string]any) map[string]any {
	t.Helper()
	work, err := s.CreateWork(ctx, "writer", "", input)
	if err != nil {
		t.Fatal(err)
	}
	return work
}

func workTreeFacts(t *testing.T, s *Store, ctx context.Context, ref string) (parent string, needsHome, initiative bool) {
	t.Helper()
	work, err := s.GetWork(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.EnrichWorkHierarchy(ctx, []map[string]any{work}, "writer", false, time.Now()); err != nil {
		t.Fatal(err)
	}
	return workString(work["parent_ref"]), work["needs_home"] == true, work["is_initiative"] == true
}

// New work never becomes an initiative by accident: without a parent it waits
// in the Unsorted tray; initiative:true (or legacy top_level) files it on top.
func TestWorkTreeCreateDefaultsToUnsortedTray(t *testing.T) {
	s, ctx := newWorkTreeTestStore(t)
	loose := mustWork(t, s, ctx, map[string]any{"title": "Loose task"})
	if _, tray, initiative := workTreeFacts(t, s, ctx, workString(loose["ref"])); !tray || initiative {
		t.Fatalf("loose work should wait in the tray: tray=%v initiative=%v", tray, initiative)
	}
	root := mustWork(t, s, ctx, map[string]any{"title": "Payments reliability", "initiative": true})
	legacy := mustWork(t, s, ctx, map[string]any{"title": "Legacy top level", "top_level": true})
	for _, w := range []map[string]any{root, legacy} {
		if parent, tray, initiative := workTreeFacts(t, s, ctx, workString(w["ref"])); parent != "" || tray || !initiative {
			t.Fatalf("%s should be an initiative: parent=%q tray=%v initiative=%v", w["ref"], parent, tray, initiative)
		}
	}
	child := mustWork(t, s, ctx, map[string]any{"title": "Retry webhooks", "under": root["ref"]})
	if parent, tray, initiative := workTreeFacts(t, s, ctx, workString(child["ref"])); parent != workString(root["ref"]) || tray || initiative {
		t.Fatalf("child: parent=%q tray=%v initiative=%v", parent, tray, initiative)
	}
	brief := refreshedBrief(t, s, ctx)
	refs := map[string]bool{}
	for _, item := range brief["initiatives"].([]map[string]any) {
		refs[workString(item["ref"])] = true
	}
	if !refs[workString(root["ref"])] || !refs[workString(legacy["ref"])] || refs[workString(loose["ref"])] || refs[workString(child["ref"])] {
		t.Fatalf("brief initiatives must be exactly the top-level work: %v", refs)
	}
	tray, _, err := s.UngroupedWork(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(tray) != 1 || tray[0]["ref"] != loose["ref"] {
		t.Fatalf("tray must hold only unsorted work: %v", tray)
	}
}

func refreshedBrief(t *testing.T, s *Store, ctx context.Context) map[string]any {
	t.Helper()
	for done := false; !done; {
		var err error
		if done, err = s.RefreshBriefInitiatives(ctx, 200); err != nil {
			t.Fatal(err)
		}
	}
	brief, err := s.Brief(ctx, BriefReader{ActorID: "writer", Kind: "agent"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return brief
}

// Plan links are checklists, not parentage: linking a card in a plan never
// moves it, and the stored parent alone drives rollups.
func TestWorkTreePlanLinksDoNotCreateParentage(t *testing.T) {
	s, ctx := newWorkTreeTestStore(t)
	a := mustWork(t, s, ctx, map[string]any{"title": "Initiative A", "initiative": true})
	b := mustWork(t, s, ctx, map[string]any{"title": "Initiative B", "initiative": true})
	child := mustWork(t, s, ctx, map[string]any{"title": "Task", "under": a["ref"]})
	if err := s.SetCardPlan(ctx, "writer", workString(b["id"]), workString(b["updated_at"]), plans.Plan{Steps: []plans.Step{{ID: "x", Title: "Linked", Ref: workString(child["ref"])}}}); err != nil {
		t.Fatal(err)
	}
	items := []map[string]any{a, b}
	if err := s.EnrichWorkHierarchy(ctx, items, "writer", false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if a["rollup"].(*WorkRollup).Total != 1 || b["rollup"].(*WorkRollup).Total != 0 {
		t.Fatalf("rollups follow the stored parent only: a=%v b=%v", a["rollup"], b["rollup"])
	}
	if parent, _, _ := workTreeFacts(t, s, ctx, workString(child["ref"])); parent != workString(a["ref"]) {
		t.Fatalf("plan link moved the card to %q", parent)
	}
}

func TestWorkTreeMoveRules(t *testing.T) {
	s, ctx := newWorkTreeTestStore(t)
	root := mustWork(t, s, ctx, map[string]any{"title": "Initiative", "initiative": true})
	stream := mustWork(t, s, ctx, map[string]any{"title": "Workstream", "under": root["ref"]})
	task := mustWork(t, s, ctx, map[string]any{"title": "Task", "under": stream["ref"]})
	loose := mustWork(t, s, ctx, map[string]any{"title": "Loose"})

	// Depth is capped at initiative -> workstream -> task.
	_, err := s.MoveWork(ctx, "writer", workString(loose["ref"]), MoveWorkInput{Under: workString(task["ref"])})
	if !errors.Is(err, ErrInvalidWorkRequest) || !strings.Contains(err.Error(), "limited to 3 levels") || !strings.Contains(err.Error(), workString(stream["ref"])) {
		t.Fatalf("depth limit should name the nearest legal parent: %v", err)
	}
	// A card cannot move under its own descendant.
	if _, err = s.MoveWork(ctx, "writer", workString(stream["ref"]), MoveWorkInput{Under: workString(task["ref"])}); !errors.Is(err, ErrInvalidWorkRequest) {
		t.Fatalf("cycle accepted: %v", err)
	}
	// Promoting needs a reason; the move then makes it an initiative.
	if _, err = s.MoveWork(ctx, "writer", workString(loose["ref"]), MoveWorkInput{Root: true}); !errors.Is(err, ErrInvalidWorkRequest) {
		t.Fatalf("promotion without a reason accepted: %v", err)
	}
	out, err := s.MoveWork(ctx, "writer", workString(loose["ref"]), MoveWorkInput{Root: true, Reason: "Separate outcome"})
	if err != nil || out["event"] == nil {
		t.Fatalf("promote: %v %v", out, err)
	}
	if _, _, initiative := workTreeFacts(t, s, ctx, workString(loose["ref"])); !initiative {
		t.Fatal("promoted card is not an initiative")
	}
	// Implicit grouping never demotes an initiative; an explicit move does.
	if _, err = s.GroupWork(ctx, "writer", workString(loose["ref"]), workString(root["ref"]), ""); !errors.Is(err, ErrInvalidWorkRequest) {
		t.Fatalf("group demoted an initiative: %v", err)
	}
	if _, err = s.MoveWork(ctx, "writer", workString(loose["ref"]), MoveWorkInput{Under: workString(root["ref"]), Reason: "Part of the initiative"}); err != nil {
		t.Fatal(err)
	}
	// Ungroup: back to the tray.
	if _, err = s.MoveWork(ctx, "writer", workString(task["ref"]), MoveWorkInput{Unsorted: true}); err != nil {
		t.Fatal(err)
	}
	if parent, tray, _ := workTreeFacts(t, s, ctx, workString(task["ref"])); parent != "" || !tray {
		t.Fatalf("ungroup: parent=%q tray=%v", parent, tray)
	}
	// Promote/retire routes are spellings of move, and need no initiatives board.
	if _, err = s.ChangeInitiative(ctx, "writer", workString(task["ref"]), "promote", "Own outcome", "", ""); err != nil {
		t.Fatalf("promote without an initiatives board: %v", err)
	}
	if _, err = s.ChangeInitiative(ctx, "writer", workString(task["ref"]), "retire", "Folded back", "", workString(root["ref"])); err != nil {
		t.Fatal(err)
	}
	if parent, _, _ := workTreeFacts(t, s, ctx, workString(task["ref"])); parent != workString(root["ref"]) {
		t.Fatalf("retire --under: parent=%q", parent)
	}
}

func TestWorkTreePathsTreeAndWhere(t *testing.T) {
	s, ctx := newWorkTreeTestStore(t)
	root := mustWork(t, s, ctx, map[string]any{"title": "Webhook delivery", "handle": "webhooks", "initiative": true})
	child := mustWork(t, s, ctx, map[string]any{"title": "Retry policy", "handle": "retry-policy", "under": root["ref"]})
	loose := mustWork(t, s, ctx, map[string]any{"title": "Webhook retries flaky", "handle": "flaky-retries"})

	if path, err := s.WorkPath(ctx, workString(child["id"])); err != nil || path != "initiatives/webhooks/retry-policy" {
		t.Fatalf("path=%q err=%v", path, err)
	}
	if path, err := s.WorkPath(ctx, workString(loose["id"])); err != nil || path != "unsorted/flaky-retries" {
		t.Fatalf("tray path=%q err=%v", path, err)
	}
	resolved, err := s.ResolveWorkPath(ctx, "initiatives/webhooks/retry-policy")
	if err != nil || resolved["ref"] != "card:retry-policy" {
		t.Fatalf("resolve: %v %v", resolved, err)
	}
	if _, err = s.ResolveWorkPath(ctx, "initiatives/retry-policy"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a child is not reachable as a top-level path: %v", err)
	}
	tree, err := s.WorkTree(ctx, "", 2, false)
	if err != nil {
		t.Fatal(err)
	}
	initiatives := tree["initiatives"].([]map[string]any)
	if len(initiatives) != 1 || len(initiatives[0]["children"].([]map[string]any)) != 1 || len(tree["unsorted"].([]map[string]any)) != 1 {
		t.Fatalf("tree: %v", tree)
	}
	where, err := s.WorkWhere(ctx, workString(loose["ref"]), "")
	if err != nil {
		t.Fatal(err)
	}
	suggestions := where["suggestions"].([]map[string]any)
	if where["needs_home"] != true || len(suggestions) == 0 || suggestions[0]["ref"] != root["ref"] {
		t.Fatalf("where should suggest the matching initiative: %v", where)
	}
	argv := where["next_actions"].([]map[string]any)[0]["argv"].([]string)
	if strings.Join(argv, " ") != "anx work move card:flaky-retries --under card:webhooks" {
		t.Fatalf("confirm command: %v", argv)
	}
	if filed, err := s.WorkWhere(ctx, workString(child["ref"]), ""); err != nil || filed["parent_ref"] != root["ref"] || len(filed["suggestions"].([]map[string]any)) != 0 {
		t.Fatalf("filed work needs no suggestion: %v %v", filed, err)
	}
}

// Trashing a parent sends its children to the tray instead of orphaning them
// under an invisible card.
func TestWorkTreeTrashSendsChildrenToTray(t *testing.T) {
	s, ctx := newWorkTreeTestStore(t)
	root := mustWork(t, s, ctx, map[string]any{"title": "Initiative", "initiative": true})
	child := mustWork(t, s, ctx, map[string]any{"title": "Task", "under": root["ref"]})
	if _, err := s.TrashBoardCard(ctx, "writer", workString(root["board_id"]), workString(root["id"]), "obsolete", RemoveBoardCardInput{}); err != nil {
		t.Fatal(err)
	}
	if parent, tray, _ := workTreeFacts(t, s, ctx, workString(child["ref"])); parent != "" || !tray {
		t.Fatalf("child of trashed parent: parent=%q tray=%v", parent, tray)
	}
}

// The upgrade freezes what reads showed before the work tree existed.
func TestWorkTreeBackfillFreezesLegacyHierarchy(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	initiatives, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "Initiatives", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "Tasks"})
	if err != nil {
		t.Fatal(err)
	}
	create := func(board map[string]any, title string) map[string]any {
		w, err := s.CreateWork(ctx, "writer", workString(board["id"]), map[string]any{"title": title})
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	link := func(parent map[string]any, children ...map[string]any) {
		steps := []plans.Step{}
		for _, c := range children {
			steps = append(steps, plans.Step{ID: workString(c["id"]), Title: workString(c["title"]), Ref: workString(c["ref"])})
		}
		current, err := s.GetWork(ctx, workString(parent["id"]))
		if err != nil {
			t.Fatal(err)
		}
		if err = s.SetCardPlan(ctx, "writer", workString(parent["id"]), workString(current["updated_at"]), plans.Plan{Steps: steps}); err != nil {
			t.Fatal(err)
		}
	}
	r1 := create(initiatives, "Role-board initiative")
	r2 := create(initiatives, "Declared root")
	p := create(tasks, "Planned parent")
	c1 := create(tasks, "Child of planned parent")
	c2 := create(tasks, "Linked from two parents")
	loose := create(tasks, "Loose task")
	done := create(tasks, "Finished task")
	x := create(tasks, "Cycle X")
	y := create(tasks, "Cycle Y")
	link(r1, c2)
	link(p, c1, c2, r2)
	link(x, y)
	link(y, x)
	for _, q := range []string{
		`UPDATE cards SET initiative_root=1 WHERE id='` + workString(r2["id"]) + `'`,
		`UPDATE cards SET column_key='done' WHERE id='` + workString(done["id"]) + `'`,
	} {
		if _, err = ws.DB().ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := ws.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.BackfillWorkTree(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct {
		card       map[string]any
		parent     map[string]any
		tray       bool
		initiative bool
	}{
		{r1, nil, false, true},
		{r2, nil, false, true}, // a declared root ignores incoming plan links
		{p, nil, false, true},  // parentless with children
		{c1, p, false, false},
		{c2, r1, false, false}, // the oldest open parent wins
		{loose, nil, true, false},
		{done, nil, true, false}, // finished tasks never read as finished initiatives
		{x, nil, false, true},    // a cycle breaks at its oldest member
		{y, x, false, false},
	} {
		parent, tray, initiative := workTreeFacts(t, s, ctx, workString(want.card["ref"]))
		if parent != workString(want.parent["ref"]) || tray != want.tray || initiative != want.initiative {
			t.Errorf("%s: parent=%q tray=%v initiative=%v; want parent=%q tray=%v initiative=%v", want.card["title"], parent, tray, initiative, workString(want.parent["ref"]), want.tray, want.initiative)
		}
	}
}
