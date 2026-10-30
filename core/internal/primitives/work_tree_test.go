package primitives

import (
	"context"
	"encoding/json"
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

// Structural checks must see hidden ancestors/descendants without disclosing
// their identities, titles, paths or occupancy through ordinary projections.
func TestWorkTreeHiddenStructurePrivacy(t *testing.T) {
	s, ctx := newWorkTreeTestStore(t)
	mustWork(t, s, ctx, map[string]any{"title": "Public board anchor"})
	private, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "Secret board"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "writer", workString(private["thread_id"]), map[string]any{"pm_actor_id": "writer"}, nil); err != nil {
		t.Fatal(err)
	}
	secret, err := s.CreateWork(ctx, "writer", workString(private["id"]), map[string]any{"title": "Secret webhook outcome", "handle": "secret-webhooks", "initiative": true})
	if err != nil {
		t.Fatal(err)
	}
	visible := mustWork(t, s, ctx, map[string]any{"title": "Visible stream", "handle": "visible-stream"})
	if _, err = s.GroupWork(ctx, "writer", workString(visible["ref"]), workString(secret["ref"]), ""); err != nil {
		t.Fatal(err)
	}
	leaf := mustWork(t, s, ctx, map[string]any{"title": "Visible leaf", "under": visible["ref"]})
	loose := mustWork(t, s, ctx, map[string]any{"title": "Webhook repair"})
	reader := WithRequestAccessScope(ctx, AccessScope{ActorID: "stranger"})
	assertPrivate := func(value any) {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		for _, hidden := range []string{workString(secret["ref"]), workString(secret["id"]), "Secret webhook outcome", "secret-webhooks"} {
			if strings.Contains(string(raw), hidden) {
				t.Fatalf("hidden structure leaked: %s", raw)
			}
		}
	}
	if path, err := s.WorkPath(reader, workString(visible["id"])); err != nil || path != "" {
		t.Fatalf("hidden ancestor path=%q err=%v", path, err)
	}
	for _, ref := range []string{workString(visible["ref"]), workString(loose["ref"])} {
		where, err := s.WorkWhere(reader, ref, "")
		if err != nil {
			t.Fatal(err)
		}
		assertPrivate(where)
	}
	for _, root := range []string{"", workString(visible["ref"])} {
		tree, err := s.WorkTree(reader, root, 3, false)
		if err != nil {
			t.Fatal(err)
		}
		assertPrivate(tree)
	}
	if _, err := s.ResolveWorkPath(reader, "initiatives/secret-webhooks/visible-stream"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("hidden path resolved: %v", err)
	}
	// The nearest legal parent is hidden, so depth refusal must stay generic.
	if _, err := s.MoveWork(reader, "stranger", workString(loose["ref"]), MoveWorkInput{Under: workString(leaf["ref"])}); !errors.Is(err, ErrInvalidWorkRequest) {
		t.Fatalf("hidden ancestor ignored by depth check: %v", err)
	} else {
		assertPrivate(err.Error())
	}
	// A hidden child still contributes to subtree height, without becoming a
	// visible rollup child or an error suggestion.
	carrier := mustWork(t, s, ctx, map[string]any{"title": "Visible carrier"})
	hiddenLeaf, err := s.CreateWork(ctx, "writer", workString(private["id"]), map[string]any{"title": "Secret nested task"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.GroupWork(ctx, "writer", workString(hiddenLeaf["ref"]), workString(carrier["ref"]), ""); err != nil {
		t.Fatal(err)
	}
	if err = s.EnrichWorkHierarchy(reader, []map[string]any{carrier}, "stranger", false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if rollup := carrier["rollup"].(*WorkRollup); rollup.Total != 0 || rollup.HasMore {
		t.Fatalf("hidden subtree occupancy leaked: %+v", rollup)
	}
	if _, err = s.MoveWork(reader, "stranger", workString(carrier["ref"]), MoveWorkInput{Under: workString(visible["ref"])}); !errors.Is(err, ErrInvalidWorkRequest) {
		t.Fatalf("hidden descendant ignored by subtree height: %v", err)
	} else {
		assertPrivate(err.Error())
		if strings.Contains(err.Error(), workString(hiddenLeaf["ref"])) || strings.Contains(err.Error(), "Secret nested task") {
			t.Fatal(err)
		}
	}
	// Cycle detection traverses the hidden root too.
	if _, err = s.MoveWork(reader, "stranger", workString(visible["ref"]), MoveWorkInput{Under: workString(leaf["ref"])}); !errors.Is(err, ErrInvalidWorkRequest) {
		t.Fatalf("cycle accepted: %v", err)
	} else {
		assertPrivate(err.Error())
	}
}

func TestWorkTreeCreationDoesNotSuggestTrayAsHome(t *testing.T) {
	s, ctx := newWorkTreeTestStore(t)
	loose := mustWork(t, s, ctx, map[string]any{"title": "Webhook tray outcome"})
	child := mustWork(t, s, ctx, map[string]any{"title": "Webhook repair", "related_refs": []string{workString(loose["ref"])}})
	if homes := child["likely_home"].([]LikelyHome); len(homes) != 0 {
		t.Fatalf("tray work suggested as home: %v", homes)
	}
	if _, err := s.MoveWork(ctx, "writer", workString(loose["ref"]), MoveWorkInput{Root: true, Reason: "Independent outcome"}); err != nil {
		t.Fatal(err)
	}
	next := mustWork(t, s, ctx, map[string]any{"title": "Webhook delivery", "related_refs": []string{workString(loose["ref"])}})
	if homes := next["likely_home"].([]LikelyHome); len(homes) != 1 || homes[0].Ref != loose["ref"] {
		t.Fatalf("explicit initiative not suggested: %v", homes)
	}
}

func TestWorkTreeRollupOrdersStoredChildrenWithoutChecklistIDs(t *testing.T) {
	s, ctx := newWorkTreeTestStore(t)
	parent := mustWork(t, s, ctx, map[string]any{"title": "Outcome", "initiative": true})
	first := mustWork(t, s, ctx, map[string]any{"title": "Older", "under": parent["ref"]})
	second := mustWork(t, s, ctx, map[string]any{"title": "Newer", "under": parent["ref"]})
	if _, err := s.db.ExecContext(ctx, `UPDATE cards SET created_at=? WHERE id=?`, "2000-01-01", first["id"]); err != nil {
		t.Fatal(err)
	}
	current, err := s.GetWork(ctx, workString(parent["ref"]))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetCardPlan(ctx, "writer", workString(parent["id"]), workString(current["updated_at"]), plans.Plan{Steps: []plans.Step{{ID: "newer-first", Title: "Dependency", Ref: workString(second["ref"])}, {ID: "older-last", Title: "Dependency", Ref: workString(first["ref"])}}}); err != nil {
		t.Fatal(err)
	}
	if err = s.EnrichWorkHierarchy(ctx, []map[string]any{parent}, "writer", false, time.Now()); err != nil {
		t.Fatal(err)
	}
	children := parent["rollup"].(*WorkRollup).Children
	if len(children) != 2 || children[0].Ref != first["ref"] || children[1].Ref != second["ref"] {
		t.Fatalf("checklist changed sibling order: %v", children)
	}
	raw, _ := json.Marshal(children)
	if strings.Contains(string(raw), "step_id") {
		t.Fatalf("checklist identity in stored children: %s", raw)
	}
}

func TestWorkTreeUnfoldRestoresPreviousParent(t *testing.T) {
	s, ctx := newWorkTreeTestStore(t)
	before := mustWork(t, s, ctx, map[string]any{"title": "Original outcome", "initiative": true})
	into := mustWork(t, s, ctx, map[string]any{"title": "Merge destination", "initiative": true})
	child := mustWork(t, s, ctx, map[string]any{"title": "Merged task", "under": before["ref"]})
	if _, err := s.FoldWork(ctx, "writer", workString(child["ref"]), workString(into["ref"]), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UnfoldWork(ctx, "writer", workString(child["ref"])); err != nil {
		t.Fatal(err)
	}
	if parent, tray, initiative := workTreeFacts(t, s, ctx, workString(child["ref"])); parent != before["ref"] || tray || initiative {
		t.Fatalf("unfold lost original position: parent=%s tray=%v initiative=%v", parent, tray, initiative)
	}
}

func TestWorkTreeDigestDistinguishesFallbackFromMissingOwner(t *testing.T) {
	for _, tc := range []struct{ owner, orphaned, want string }{
		{"", "2000-01-01", "Awaiting an owner"},
		{"actor:writer", "2000-01-01", "Ownership fell back to actor:writer"},
	} {
		input := cardHealthInput{Owner: tc.owner, OrphanedAt: tc.orphaned}
		health := input.trackingDigest(plans.Health{State: plans.PromiseMoving}, time.Now())
		if health.State != plans.PromiseDigest || !strings.Contains(health.Reason, tc.want) || (tc.owner != "" && strings.Contains(health.Reason, "Awaiting an owner")) {
			t.Fatalf("owner=%q health=%+v", tc.owner, health)
		}
	}
}

func TestWorkTreeDoesNotRefillThroughHiddenRootCohort(t *testing.T) {
	s, ctx := newWorkTreeTestStore(t)
	mustWork(t, s, ctx, map[string]any{"title": "Public tray anchor"})
	board, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "Private root cohort"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "writer", workString(board["thread_id"]), map[string]any{"pm_actor_id": "writer"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.ExecContext(ctx, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<201)
 INSERT INTO cards(id,board_id,title,summary,column_key,created_at,created_by,updated_at,updated_by,needs_home)
 SELECT 'hidden-tree-'||i,?,'Secret root','','ready','2000-01-01','writer','2000-01-01','writer',0 FROM n`, board["id"]); err != nil {
		t.Fatal(err)
	}
	visible := mustWork(t, s, ctx, map[string]any{"title": "Visible later root", "initiative": true})
	reader := WithRequestAccessScope(ctx, AccessScope{ActorID: "stranger"})
	tree, err := s.WorkTree(reader, "", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(tree["initiatives"].([]map[string]any)) != 0 || tree["truncated"] != false {
		t.Fatalf("tree refilled or exposed hidden occupancy: %v", tree)
	}
	// A point/subtree read still reaches the visible root outside the cohort.
	point, err := s.WorkTree(reader, workString(visible["ref"]), 1, false)
	if err != nil || point["node"].(map[string]any)["ref"] != visible["ref"] {
		t.Fatalf("bounded admission hid a point read: %v %v", point, err)
	}
}

func TestWorkTreeListCardsMatchesStoredPosition(t *testing.T) {
	s, ctx := newWorkTreeTestStore(t)
	root := mustWork(t, s, ctx, map[string]any{"title": "Listed root", "initiative": true})
	child := mustWork(t, s, ctx, map[string]any{"title": "Listed child", "under": root["ref"]})
	loose := mustWork(t, s, ctx, map[string]any{"title": "Listed loose task"})
	cards, err := s.ListCards(ctx, CardListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cards) != 3 {
		t.Fatal(cards)
	}
	for _, card := range cards {
		initiative, tray := card["ref"] == root["ref"], card["ref"] == loose["ref"]
		if card["is_initiative"] != initiative || card["needs_home"] != tray {
			t.Fatalf("list lost tree position: child=%s row=%v", child["ref"], card)
		}
	}
}
