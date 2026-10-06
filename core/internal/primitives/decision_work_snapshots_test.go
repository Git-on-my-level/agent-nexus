package primitives

import (
	"context"
	"errors"
	"testing"

	"agent-nexus-core/internal/storage"
)

func TestDecisionWorkSnapshotsRetainPlacementAndPrivacy(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Snapshot board"})
	if err != nil {
		t.Fatal(err)
	}
	work := map[string]map[string]any{}
	for _, handle := range []string{"public_snapshot", "private_snapshot", "missing_placement", "archived_snapshot"} {
		work[handle], err = s.CreateWork(ctx, "owner", anyStringValue(board["id"]), map[string]any{"title": handle, "handle": handle})
		if err != nil {
			t.Fatal(err)
		}
	}
	public := work["public_snapshot"]
	if _, err = s.PatchThread(ctx, "owner", anyStringValue(work["private_snapshot"]["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`DELETE FROM ref_edges WHERE source_type='board' AND edge_type='board_card' AND target_id=?`, work["missing_placement"]["id"]); err != nil {
		t.Fatal(err)
	}
	if _, err = s.ArchiveBoardCard(ctx, "owner", anyStringValue(board["id"]), anyStringValue(work["archived_snapshot"]["id"]), RemoveBoardCardInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`UPDATE ref_edges SET metadata_json=json_set(metadata_json,'$.column_key','blocked') WHERE source_type='board' AND edge_type='board_card' AND target_id=?`, public["id"]); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`INSERT INTO resource_handle_aliases(resource_type,alias_handle,resource_id,canonical_handle,created_at) VALUES('card','old-snapshot',?,'public-snapshot','now')`, public["id"]); err != nil {
		t.Fatal(err)
	}
	scope := WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"})
	refs := []string{anyStringValue(public["ref"]), "card:public_snapshot", "card:old_snapshot", "card:private_snapshot", "card:missing_placement", "card:archived_snapshot"}
	snapshots, err := s.LiveWorkSnapshots(scope, refs)
	if err != nil || len(snapshots) != 3 {
		t.Fatalf("bounded placement snapshots: %v %v", snapshots, err)
	}
	point, err := s.GetWork(scope, anyStringValue(public["ref"]))
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range refs[:3] {
		if snapshots[ref]["phase"] != "blocked" || snapshots[ref]["phase"] != point["phase"] || WorkDecisionRevision(snapshots[ref]) != WorkDecisionRevision(point) {
			t.Fatalf("placement/alias projection differs for %s: %v versus %v", ref, snapshots[ref], point)
		}
	}
	// Placement metadata remains scoped before selecting the first edge. The
	// already-prepared request must see this update's epoch and hide its refs.
	if _, err = ws.DB().Exec(`UPDATE ref_edges SET metadata_json=json_set(metadata_json,'$.private_ref',?) WHERE source_type='board' AND edge_type='board_card' AND target_id=?`, work["private_snapshot"]["ref"], public["id"]); err != nil {
		t.Fatal(err)
	}
	snapshots, err = s.LiveWorkSnapshots(scope, refs)
	if err != nil || len(snapshots) != 0 {
		t.Fatalf("private placement metadata visible: %v %v", snapshots, err)
	}
	if _, err = s.GetWork(scope, anyStringValue(public["ref"])); !errors.Is(err, ErrNotFound) {
		t.Fatalf("point visibility differs from snapshot: %v", err)
	}
}
