package primitives

import (
	"context"
	"errors"
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
	if _, err = s.MoveBoardCard(ctx, "writer", "", workString(c["id"]), MoveBoardCardInput{ColumnKey: "ready", ToBoard: workString(b["ref"])}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.MoveBoardCard(ctx, "writer", "", workString(c["id"]), MoveBoardCardInput{ColumnKey: "ready", ToBoard: workString(tasks["ref"]), Reason: "Move out"}); err != nil {
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
