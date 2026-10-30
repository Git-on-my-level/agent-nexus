package primitives

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/plans"
)

func TestGroupingTitleFamilies(t *testing.T) {
	for title, want := range map[string]string{"P76: monitor": "p76", "P76 reliability": "p76", "A1 F2": "a1", "fs-p3 cleanup": "fs-p3", "Cost 2/5: vendor spend": "cost", "ordinary prose": ""} {
		if got := TitleFamily(title); got != want {
			t.Errorf("%q: %q want %q", title, got, want)
		}
	}
}

func TestGroupingFoldRejectsRelationOverflowAtomically(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	p, err := s.CreateWork(ctx, "writer", "", map[string]any{"title": "Parent"})
	if err != nil {
		t.Fatal(err)
	}
	relations := []map[string]any{}
	for i := 1; i <= 200; i++ {
		relations = append(relations, map[string]any{"kind": "related", "ref": p["ref"]})
	}
	c, err := s.CreateWork(ctx, "writer", "", map[string]any{"title": "Child", "relations": relations})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.FoldWork(ctx, "writer", workString(c["ref"]), workString(p["ref"]), ""); !errors.Is(err, ErrInvalidWorkRequest) {
		t.Fatalf("fold exceeded relation contract: %v", err)
	}
	read, err := s.GetWork(ctx, workString(c["ref"]))
	if err != nil || read["archived_at"] != nil {
		t.Fatalf("failed fold archived child: %v %v", read, err)
	}
	var plansWithSteps int
	err = ws.DB().QueryRowContext(ctx, `SELECT count(*) FROM card_plans WHERE card_id=? AND json_array_length(body_json,'$.steps')>0`, p["id"]).Scan(&plansWithSteps)
	if err != nil || plansWithSteps != 0 {
		t.Fatalf("failed fold changed plan: %v %v", plansWithSteps, err)
	}
}

func TestGroupingLegacyCardReadsKeepWorkingAboveTwoHundred(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	p, err := s.CreateWork(ctx, "writer", "", map[string]any{"title": "Parent"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ws.DB().ExecContext(ctx, `WITH RECURSIVE n(value) AS (SELECT 1 UNION ALL SELECT value+1 FROM n WHERE value<200)
 INSERT INTO cards(id,board_id,title,summary,column_key,created_at,created_by,updated_at,updated_by)
 SELECT 'legacy-'||value,?,'Legacy card','','ready','now','writer','now','writer' FROM n`, p["board_id"])
	if err != nil {
		t.Fatal(err)
	}
	cards := []map[string]any{p}
	for i := 1; i <= 200; i++ {
		cards = append(cards, map[string]any{"id": fmt.Sprintf("legacy-%d", i)})
	}
	ctx = WithWorkHierarchyViewer(ctx, "viewer", true)
	if err = s.EnrichCardPlans(ctx, cards, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, card := range cards {
		if _, ok := card["rollup"]; !ok {
			t.Fatalf("missing hierarchy: %v", card["id"])
		}
	}
}

func TestGroupingDoesNotTreatBoardMembershipAsSharedEvidence(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	if _, err = s.CreateWork(ctx, "writer", "", map[string]any{"title": "Review onboarding"}); err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateWork(ctx, "writer", "", map[string]any{"title": "Measure storage"})
	if err != nil {
		t.Fatal(err)
	}
	if homes := c["likely_home"].([]LikelyHome); len(homes) != 0 {
		t.Fatalf("ordinary board membership suggested an unrelated home: %+v", homes)
	}
	rows, _, err := s.UngroupedWork(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if len(row["likely_home"].([]LikelyHome)) != 0 {
			t.Fatalf("tray suggested an unrelated home: %+v", row)
		}
	}
}

func TestGroupingRejectsCancelledParent(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	p, err := s.CreateWork(ctx, "writer", "", map[string]any{"title": "Cancelled parent"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateWork(ctx, "writer", "", map[string]any{"title": "Child"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().ExecContext(ctx, `UPDATE ref_edges SET metadata_json=json_set(metadata_json,'$.column_key','cancelled') WHERE source_type='board' AND target_id=?`, p["id"]); err != nil {
		t.Fatal(err)
	}
	if _, err = s.GroupWork(ctx, "writer", workString(c["ref"]), workString(p["ref"]), ""); !errors.Is(err, ErrInvalidWorkRequest) {
		t.Fatalf("grouped under cancelled parent: %v", err)
	}
}

func TestGroupingUngroupedIncludesCompletedCardsButHomesStayOpen(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	p, err := s.CreateWork(ctx, "writer", "", map[string]any{"title": "Cost 1/5 Budget", "initiative": true})
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.CreateWork(ctx, "writer", "", map[string]any{"title": "Cost 2/5 Measure"})
	if err != nil {
		t.Fatal(err)
	}
	proof, err := s.AppendEvent(ctx, "writer", map[string]any{"type": "message_posted", "thread_id": c["thread_id"], "refs": []string{workString(c["ref"])}, "payload": map[string]any{"text": "Complete"}})
	if err != nil {
		t.Fatal(err)
	}
	resolution := "done"
	refs := []string{"event:" + workString(proof["id"])}
	if _, err = s.MoveBoardCard(ctx, "writer", workString(c["board_id"]), workString(c["id"]), MoveBoardCardInput{ColumnKey: "done", Resolution: &resolution, ResolutionRefs: &refs}); err != nil {
		t.Fatal(err)
	}
	rows, more, err := s.UngroupedWork(ctx, 10)
	if err != nil || more || len(rows) != 1 {
		t.Fatalf("rows=%v more=%v err=%v", rows, more, err)
	}
	for _, row := range rows {
		homes := row["likely_home"].([]LikelyHome)
		if row["ref"] == c["ref"] {
			if row["phase"] != "done" || len(homes) != 1 || homes[0].Ref != p["ref"] {
				t.Fatal(row)
			}
		} else if len(homes) != 0 {
			t.Fatalf("completed card suggested as a home: %v", row)
		}
	}
}

func TestGroupingHierarchyRollupAndBudget(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	create := func(title string, extra map[string]any) map[string]any {
		t.Helper()
		input := map[string]any{"title": title}
		for k, v := range extra {
			input[k] = v
		}
		w, e := s.CreateWork(ctx, "writer", "", input)
		if e != nil {
			t.Fatal(e)
		}
		return w
	}
	p := create("P76 Parent", map[string]any{"initiative": true})
	c := create("P76 Workstream", map[string]any{"under": p["ref"]})
	g := create("P76 Leaf", map[string]any{"under": c["ref"]})
	_ = g
	create("Loop", map[string]any{"cadence": map[string]any{"every_seconds": 86400}})
	receiptLoop := create("Receipt-only loop", nil)
	if _, err = s.AppendEvent(ctx, "writer", map[string]any{"type": "run_recorded", "subject_ref": "card:" + workString(receiptLoop["id"]), "summary": "Measured once", "refs": []string{workString(receiptLoop["ref"])}, "payload": map[string]any{"run_id": "once", "outcome": "no_op"}}); err != nil {
		t.Fatal(err)
	}
	// Checklist links do not displace the stored parent.
	other := create("Other parent", map[string]any{"initiative": true})
	if err = s.SetCardPlan(ctx, "writer", workString(other["id"]), workString(other["updated_at"]), plans.Plan{Steps: []plans.Step{{ID: "dependency", Title: "Needs stream", Ref: workString(c["ref"])}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.MoveWork(ctx, "writer", workString(p["ref"]), MoveWorkInput{Under: workString(c["ref"]), Reason: "Invalid cycle"}); !errors.Is(err, ErrInvalidWorkRequest) {
		t.Fatalf("accepted cycle: %v", err)
	}
	if _, err = s.AppendEvent(ctx, "writer", map[string]any{"type": "message_posted", "thread_id": c["thread_id"], "refs": []string{workString(c["ref"])}, "payload": map[string]any{"next": map[string]any{"at": "2000-01-01", "what": "Ship"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AppendEvent(ctx, "asker", map[string]any{"type": "human_attention_requested", "thread_id": c["thread_id"], "refs": []string{workString(c["ref"])}, "payload": map[string]any{"subject_ref": c["ref"], "requester_actor_id": "asker", "recipient_actor_id": "viewer", "title": "Choose?", "body": "Choose an approach", "response_proposals": []string{"Proceed", "Wait"}}}); err != nil {
		t.Fatal(err)
	}
	for drained := false; !drained; {
		drained, err = s.RefreshBriefInitiatives(ctx, 200)
		if err != nil {
			t.Fatal(err)
		}
	}
	item, err := s.GetWork(ctx, workString(p["id"]))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.EnrichWorkHierarchy(ctx, []map[string]any{item}, "viewer", true, time.Now()); err != nil {
		t.Fatal(err)
	}
	r := item["rollup"].(*WorkRollup)
	if r.Total != 1 || r.PastPromise != 1 || len(r.Workstreams) != 1 || r.NeedingViewer != 1 {
		t.Fatalf("rollup=%+v", r)
	}
	read, err := s.GetWork(ctx, workString(c["id"]))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.EnrichWorkHierarchy(ctx, []map[string]any{read}, "viewer", true, time.Now()); err != nil {
		t.Fatal(err)
	}
	if read["parent_ref"] != p["ref"] {
		t.Fatalf("multiple parents: %#v", read["parent_ref"])
	}
	brief, err := s.Brief(ctx, BriefReader{ActorID: "viewer", Kind: "human"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := len(brief["initiatives"].([]map[string]any)); got != 2 {
		t.Fatalf("initiatives include children/loops: %d", got)
	}
	if got := len(brief["loops"].([]map[string]any)); got != 2 {
		t.Fatalf("loops=%d", got)
	}
	if budget := brief["initiative_budget"].(map[string]any); budget["count"] != 2 || budget["target"] != 15 {
		t.Fatalf("budget=%v", budget)
	}
	if _, err = s.ArchiveBoardCard(ctx, "writer", "", workString(p["id"]), RemoveBoardCardInput{}); err != nil {
		t.Fatal(err)
	}
	if err = s.EnrichWorkHierarchy(ctx, []map[string]any{read}, "viewer", true, time.Now()); err != nil {
		t.Fatal(err)
	}
	// Archived work is never a home: its children wait in the Unsorted tray.
	if read["parent_ref"] != nil || read["needs_home"] != true {
		t.Fatalf("archiving kept the child under its archived parent: %v %v", read["parent_ref"], read["needs_home"])
	}
}

func TestGroupingHiddenChildrenAndHomesDoNotLeak(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	public, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "P76 Public"})
	if err != nil {
		t.Fatal(err)
	}
	privateBoard, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Secret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", workString(privateBoard["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	secret, err := s.CreateWork(ctx, "owner", workString(privateBoard["id"]), map[string]any{"title": "P76 secret child"})
	if err != nil {
		t.Fatal(err)
	}
	inherited, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Private subtask", "under": secret["ref"]})
	if err != nil || inherited["board_id"] != privateBoard["id"] {
		t.Fatalf("under did not inherit the parent's board: %v %v", inherited, err)
	}
	if _, err = s.GroupWork(ctx, "owner", workString(secret["ref"]), workString(public["ref"]), ""); err != nil {
		t.Fatal(err)
	}
	// A hidden checklist remains authored data even though it no longer owns
	// parentage. Grouping another child must neither read nor replace it.
	if err = s.SetCardPlan(ctx, "owner", workString(public["id"]), workString(public["updated_at"]), plans.Plan{Steps: []plans.Step{{ID: "private-dependency", Title: "Secret dependency", Ref: workString(secret["ref"])}}}); err != nil {
		t.Fatal(err)
	}
	var planBefore string
	if err = ws.DB().QueryRowContext(ctx, `SELECT body_json FROM card_plans WHERE card_id=?`, public["id"]).Scan(&planBefore); err != nil {
		t.Fatal(err)
	}
	reader := WithAccessScope(ctx, AccessScope{ActorID: "stranger"})
	item, err := s.GetWork(reader, workString(public["id"]))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.EnrichWorkHierarchy(reader, []map[string]any{item}, "stranger", true, time.Now()); err != nil {
		t.Fatal(err)
	}
	r := item["rollup"].(*WorkRollup)
	if r.Total != 0 || r.HasMore || len(r.Children) != 0 {
		t.Fatalf("private child leaked: %+v", r)
	}
	newChild, err := s.CreateWork(ctx, "stranger", "", map[string]any{"title": "Visible new child"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.GroupWork(reader, "stranger", workString(newChild["ref"]), workString(public["ref"]), ""); err != nil {
		t.Fatalf("hidden sibling prevented grouping: %v", err)
	}
	var planAfter string
	if err = ws.DB().QueryRowContext(ctx, `SELECT body_json FROM card_plans WHERE card_id=?`, public["id"]).Scan(&planAfter); err != nil || planBefore != planAfter {
		t.Fatalf("group overwrote unreadable checklist: err=%v", err)
	}
	var hiddenParent string
	if err = ws.DB().QueryRowContext(ctx, `SELECT parent_card_id FROM cards WHERE id=?`, secret["id"]).Scan(&hiddenParent); err != nil || hiddenParent != public["id"] {
		t.Fatalf("hidden sibling displaced: %s err=%v", hiddenParent, err)
	}
	safeParent, err := s.CreateWork(ctx, "writer", "", map[string]any{"title": "Visible parent"})
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"First visible child", "Second visible child"} {
		if _, err = s.CreateWork(reader, "stranger", "", map[string]any{"title": title, "under": safeParent["ref"]}); err != nil {
			t.Fatalf("visible parent rejected scoped create: %v", err)
		}
	}
	items, more, err := s.UngroupedWork(reader, 10)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(items)
	if more || strings.Contains(string(raw), "secret") || strings.Contains(string(raw), workString(secret["id"])) {
		t.Fatalf("private home/feed leaked: %s more=%v", raw, more)
	}
	if _, err = s.GroupWork(reader, "stranger", workString(public["ref"]), workString(secret["ref"]), ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("can group into hidden parent: %v", err)
	}
}

func TestGroupingFoldTransfersAndReversesOwnedFields(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	p, err := s.CreateWork(ctx, "writer", "", map[string]any{"title": "Parent"})
	if err != nil {
		t.Fatal(err)
	}
	proof := map[string]any{"authority": "forge", "connection_id": "repo", "native_id": "7", "url": "https://example.test/repo/pull/7"}
	c, err := s.CreateWork(ctx, "writer", "", map[string]any{"title": "Child", "source_refs": []any{proof}, "related_refs": []string{workString(p["ref"])}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.AppendEvent(ctx, "writer", map[string]any{"type": "message_posted", "thread_id": c["thread_id"], "refs": []string{workString(c["ref"])}, "payload": map[string]any{"next": map[string]any{"at": "2099-01-01", "what": "Ship"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.FoldWork(ctx, "writer", workString(c["ref"]), workString(p["ref"]), ""); err != nil {
		t.Fatal(err)
	}
	pAfter, err := s.GetWork(ctx, workString(p["id"]))
	if err != nil {
		t.Fatal(err)
	}
	cAfter, err := s.GetWork(ctx, workString(c["id"]))
	if err == nil {
		err = s.EnrichWorkHierarchy(ctx, []map[string]any{cAfter}, "writer", false, time.Now())
	}
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(pAfter["source_refs"])
	if !strings.Contains(string(data), "pull/7") {
		t.Fatalf("source not transferred: %s", data)
	}
	if workString(cAfter["archived_at"]) == "" {
		t.Fatal("fold did not archive")
	}
	plan, _, _, err := s.loadPlans(ctx, []string{workString(p["id"])})
	if err != nil {
		t.Fatal(err)
	}
	steps := plan[workString(p["id"])].Steps
	if len(steps) != 0 || cAfter["parent_ref"] != p["ref"] {
		t.Fatalf("fold must store parent without writing checklist: child=%v steps=%+v", cAfter, steps)
	}
	if _, err = s.UnfoldWork(ctx, "writer", workString(c["ref"])); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetWork(ctx, workString(c["id"]))
	if err != nil {
		t.Fatal(err)
	}
	if workString(got["archived_at"]) != "" || got["parent_ref"] != nil {
		t.Fatalf("unfold failed: %#v", got)
	}
	for done := false; !done; {
		var refreshErr error
		done, refreshErr = s.RefreshBriefInitiatives(ctx, 200)
		if refreshErr != nil {
			t.Fatal(refreshErr)
		}
	}
	folds, err := s.PromiseFolds(ctx, []string{workString(c["id"])})
	if err != nil || folds[workString(c["id"])].Promise == nil || folds[workString(c["id"])].Promise.At.UTC().Format(time.RFC3339) != "2099-01-01T23:59:59Z" {
		t.Fatalf("unfold lost the child's authored promise: %v %v", folds, err)
	}
	data, _ = json.Marshal(got["source_refs"])
	if !strings.Contains(string(data), "pull/7") {
		t.Fatal("source lost on unfold")
	}
	if _, err = s.FoldWork(ctx, "writer", workString(c["ref"]), workString(p["ref"]), ""); err != nil {
		t.Fatal(err)
	}
	pAfter, err = s.GetWork(ctx, workString(p["id"]))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetCardPlan(ctx, "writer", workString(p["id"]), workString(pAfter["updated_at"]), plans.Plan{Steps: []plans.Step{{ID: "newer", Title: "Keep this newer work"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.UnfoldWork(ctx, "writer", workString(c["ref"])); !errors.Is(err, ErrConflict) {
		t.Fatalf("overwrote newer plan: %v", err)
	}
}
