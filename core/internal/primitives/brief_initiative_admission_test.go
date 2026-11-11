package primitives

import (
	"fmt"
	"sort"
	"testing"
	"time"
)

// Watch counted "1 open" while five initiatives existed: the brief admitted
// its 200 candidates from every open card's promise order and only then kept
// the top-level ones, so a busy workspace's children crowded initiatives out
// without marking the section truncated. Every open initiative must be listed,
// and the section must agree with anx tree.
func TestBriefListsEveryOpenInitiativeWhenChildrenCrowdThePromiseOrder(t *testing.T) {
	s, ctx := newWorkTreeTestStore(t)
	now := time.Now().UTC()
	busy := mustWork(t, s, ctx, map[string]any{"title": "Busy initiative", "initiative": true})
	promise := func(work map[string]any, at time.Time, what string) {
		t.Helper()
		if _, err := s.AppendEvent(ctx, "writer", map[string]any{
			"type": "message_posted", "thread_id": work["thread_id"],
			"refs":    []string{"thread:" + workString(work["thread_id"])},
			"payload": map[string]any{"text": what, "next": map[string]any{"at": at.Format(time.RFC3339), "what": what}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 210; i++ {
		child := mustWork(t, s, ctx, map[string]any{"title": fmt.Sprintf("Child %03d", i), "under": busy["ref"]})
		promise(child, now.Add(time.Duration(i+1)*time.Minute), "child step")
	}
	promise(busy, now.Add(5*time.Hour), "busy rollup")
	// Promised later than every child, and one with no promise at all.
	late := mustWork(t, s, ctx, map[string]any{"title": "Late promise initiative", "initiative": true})
	promise(late, now.Add(24*time.Hour), "late step")
	quiet := mustWork(t, s, ctx, map[string]any{"title": "Not started initiative", "initiative": true})

	brief := refreshedBrief(t, s, ctx)
	listed := map[string]bool{}
	for _, item := range brief["initiatives"].([]map[string]any) {
		listed[workString(item["ref"])] = true
	}
	for _, w := range []map[string]any{busy, late, quiet} {
		if !listed[workString(w["ref"])] {
			t.Errorf("brief omitted open initiative %s; listed %v", w["ref"], listed)
		}
	}
	if truncated := brief["truncated"].(map[string]bool)["initiatives"]; truncated {
		t.Fatalf("three open initiatives must not truncate the section")
	}

	tree, err := s.WorkTree(ctx, "", 1, false)
	if err != nil {
		t.Fatal(err)
	}
	treeRefs := map[string]bool{}
	for _, node := range tree["initiatives"].([]map[string]any) {
		treeRefs[workString(node["ref"])] = true
	}
	if len(treeRefs) != len(listed) {
		t.Fatalf("tree lists %d initiatives, brief %d: tree=%v brief=%v", len(treeRefs), len(listed), treeRefs, listed)
	}
	for ref := range treeRefs {
		if !listed[ref] {
			t.Fatalf("tree initiative %s missing from the brief", ref)
		}
	}
}

// An initiative whose workstreams have started read backlog everywhere. The
// listings now show the effective phase (stored column untouched): started
// children or grandchildren make a not-started card read in_progress.
func TestUnstartedInitiativeWithActiveChildrenReadsInProgress(t *testing.T) {
	s, ctx := newWorkTreeTestStore(t)
	direct := mustWork(t, s, ctx, map[string]any{"title": "Margin goal", "initiative": true})
	mustWork(t, s, ctx, map[string]any{"title": "Meter", "under": direct["ref"], "phase": "in_progress"})
	mustWork(t, s, ctx, map[string]any{"title": "Legacy costs", "under": direct["ref"]})
	nested := mustWork(t, s, ctx, map[string]any{"title": "Churn goal", "initiative": true})
	stream := mustWork(t, s, ctx, map[string]any{"title": "Capture stream", "under": nested["ref"]})
	mustWork(t, s, ctx, map[string]any{"title": "Capture fix", "under": stream["ref"], "phase": "review"})
	idle := mustWork(t, s, ctx, map[string]any{"title": "Conversion goal", "initiative": true})
	mustWork(t, s, ctx, map[string]any{"title": "Purchase flow", "under": idle["ref"]})

	brief := refreshedBrief(t, s, ctx)
	byRef := map[string]map[string]any{}
	for _, item := range brief["initiatives"].([]map[string]any) {
		byRef[workString(item["ref"])] = item
	}
	for _, w := range []map[string]any{direct, nested} {
		item := byRef[workString(w["ref"])]
		if item == nil || item["placement"] != "in_progress" || item["proposed"] != false || item["stored_placement"] != "backlog" {
			t.Fatalf("%s should read in_progress from its children: %#v", w["ref"], item)
		}
	}
	if item := byRef[workString(idle["ref"])]; item == nil || item["placement"] != "backlog" || item["proposed"] != true || item["stored_placement"] != nil {
		t.Fatalf("an initiative with only unstarted children stays not started: %#v", item)
	}

	tree, err := s.WorkTree(ctx, "", 2, false)
	if err != nil {
		t.Fatal(err)
	}
	states := map[string]any{}
	for _, node := range tree["initiatives"].([]map[string]any) {
		states[workString(node["ref"])] = node["state"]
		for _, child := range node["children"].([]map[string]any) {
			states[workString(child["ref"])] = child["state"]
		}
	}
	for ref, want := range map[string]string{workString(direct["ref"]): "in_progress", workString(nested["ref"]): "in_progress", workString(stream["ref"]): "in_progress", workString(idle["ref"]): "backlog"} {
		if states[ref] != want {
			t.Fatalf("tree %s state %v, want %s (all %v)", ref, states[ref], want, states)
		}
	}
	if tree["open_initiatives"] != 3 {
		t.Fatalf("open_initiatives %v", tree["open_initiatives"])
	}
	where, err := s.WorkWhere(ctx, workString(nested["ref"]), "")
	if err != nil || where["state"] != "in_progress" || where["stored_state"] != "backlog" {
		t.Fatalf("where must show the effective state: %v %#v", err, where)
	}
	where, err = s.WorkWhere(ctx, workString(idle["ref"]), "")
	if err != nil || where["state"] != "backlog" || len(where["next_actions"].([]map[string]any)) == 0 {
		t.Fatalf("where on a not-started card must say so and offer work start: %v %#v", err, where)
	}
	// Reads never move the stored column.
	stored, err := s.GetWork(ctx, workString(direct["ref"]))
	if err != nil || stored["phase"] != "backlog" {
		t.Fatalf("stored phase changed: %v %v", err, stored["phase"])
	}
}

// A card whose stored parent the reader cannot see is that reader's
// initiative. Admitting only stored roots would drop it from the brief.
func TestBriefListsVisibleChildOfHiddenParentAsInitiative(t *testing.T) {
	s, ctx := newWorkTreeTestStore(t)
	publicBoard, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Public board"})
	if err != nil {
		t.Fatal(err)
	}
	private, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Private board"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", workString(private["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	secret, err := s.CreateWork(ctx, "owner", workString(private["id"]), map[string]any{"title": "Private root", "initiative": true})
	if err != nil {
		t.Fatal(err)
	}
	// Create the public board first: unboarded work joins the first board.
	public, err := s.CreateWork(ctx, "owner", workString(publicBoard["id"]), map[string]any{"title": "Public initiative", "initiative": true})
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Public child", "under": public["ref"]})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.GroupWork(ctx, "owner", workString(child["ref"]), workString(secret["ref"]), ""); err != nil {
		t.Fatal(err)
	}
	refreshedBrief(t, s, ctx)
	reader := WithRequestAccessScope(ctx, AccessScope{ActorID: "stranger"})
	brief, err := s.Brief(reader, BriefReader{ActorID: "stranger", Kind: "human", Visible: func(_, owner string) bool { return owner == "" || owner == "stranger" }}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, item := range brief["initiatives"].([]map[string]any) {
		listed[workString(item["ref"])] = true
	}
	if !listed[workString(child["ref"])] || !listed[workString(public["ref"])] || listed[workString(secret["ref"])] {
		t.Fatalf("stranger must see the public child as an initiative and never the private root: %v", listed)
	}
}

// More open top-level cards than the admission window reads (loops and
// hidden cards fill it too) must mark the section truncated, never shrink
// the open count silently.
func TestBriefMarksInitiativesTruncatedWhenOpenRootsOverflowAdmission(t *testing.T) {
	s, ctx := newWorkTreeTestStore(t)
	saved := briefOpenRootAdmission
	briefOpenRootAdmission = 3
	t.Cleanup(func() { briefOpenRootAdmission = saved })
	for i := 0; i < 3; i++ {
		mustWork(t, s, ctx, map[string]any{"title": fmt.Sprintf("Initiative %d", i), "initiative": true})
	}
	if brief := refreshedBrief(t, s, ctx); brief["truncated"].(map[string]bool)["initiatives"] {
		t.Fatalf("a full window that holds every open root is not truncated")
	}
	mustWork(t, s, ctx, map[string]any{"title": "One more", "initiative": true})
	brief := refreshedBrief(t, s, ctx)
	if !brief["truncated"].(map[string]bool)["initiatives"] {
		t.Fatalf("open roots past the admission window must mark the section truncated: %d listed", len(brief["initiatives"].([]map[string]any)))
	}
}

// Hidden cards never make a reader's section under-report: within the
// admission window every visible initiative is listed and the section is not
// truncated; past it (any reader, any visibility) the section reads truncated,
// a workspace-wide bit like initiative_budget.truncated.
func TestBriefAdmissionWithHiddenRootsListsOrTruncates(t *testing.T) {
	s, ctx := newWorkTreeTestStore(t)
	saved := briefOpenRootAdmission
	t.Cleanup(func() { briefOpenRootAdmission = saved })
	publicBoard, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Public board"})
	if err != nil {
		t.Fatal(err)
	}
	private, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Private board"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", workString(private["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if _, err = s.CreateWork(ctx, "owner", workString(private["id"]), map[string]any{"id": fmt.Sprintf("a-secret-%d", i), "title": "Secret", "initiative": true}); err != nil {
			t.Fatal(err)
		}
	}
	visible := map[string]bool{}
	for i := 0; i < 2; i++ {
		w, err := s.CreateWork(ctx, "owner", workString(publicBoard["id"]), map[string]any{"id": fmt.Sprintf("z-public-%d", i), "title": "Public", "initiative": true})
		if err != nil {
			t.Fatal(err)
		}
		visible[workString(w["ref"])] = true
	}
	refreshedBrief(t, s, ctx)
	read := func(actor string) (map[string]bool, bool) {
		t.Helper()
		brief, err := s.Brief(WithRequestAccessScope(ctx, AccessScope{ActorID: actor}), BriefReader{ActorID: actor, Kind: "human", Visible: func(_, owner string) bool { return owner == "" || owner == actor }}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		listed := map[string]bool{}
		for _, item := range brief["initiatives"].([]map[string]any) {
			listed[workString(item["ref"])] = true
		}
		return listed, brief["truncated"].(map[string]bool)["initiatives"]
	}
	briefOpenRootAdmission = 6
	if listed, truncated := read("stranger"); truncated || len(listed) != 2 || !listed[firstKey(visible, 0)] || !listed[firstKey(visible, 1)] {
		t.Fatalf("within the window the stranger sees exactly the public initiatives, untruncated: %v truncated=%v", listed, truncated)
	}
	briefOpenRootAdmission = 2
	for _, actor := range []string{"stranger", "owner"} {
		if listed, truncated := read(actor); !truncated {
			t.Fatalf("%s: six open roots past a window of two must read truncated, listed %v", actor, listed)
		}
	}
}

func firstKey(m map[string]bool, i int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys[i]
}
