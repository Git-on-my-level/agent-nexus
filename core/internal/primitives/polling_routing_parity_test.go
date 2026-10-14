package primitives

import (
	"context"
	"errors"
	"testing"
)

// Legacy PM delivery needs routing even after lifecycle changes, while alias
// resolution and private-work denial must match the point GetWork reader.
func TestPollingRoutingSnapshotsMatchPointLifecycleAndPrivacy(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Routing parity"})
	if err != nil {
		t.Fatal(err)
	}
	work := map[string]map[string]any{}
	for _, name := range []string{"active-routing", "archived-routing", "trashed-routing", "private-routing", "external-routing"} {
		input := map[string]any{"title": name, "handle": name}
		if name == "external-routing" {
			input["source"] = map[string]any{"authority": "github", "connection_id": "parity", "native_id": "item-1"}
		}
		work[name], err = s.CreateWork(ctx, "owner", anyStringValue(board["id"]), input)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.ArchiveBoardCard(ctx, "owner", anyStringValue(board["id"]), anyStringValue(work["archived-routing"]["id"]), RemoveBoardCardInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.TrashBoardCard(ctx, "owner", anyStringValue(board["id"]), anyStringValue(work["trashed-routing"]["id"]), "parity fixture", RemoveBoardCardInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", anyStringValue(work["private-routing"]["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`INSERT INTO resource_handle_aliases(resource_type,alias_handle,resource_id,canonical_handle,created_at) VALUES('card','old-routing',?,'active-routing','now')`, work["active-routing"]["id"]); err != nil {
		t.Fatal(err)
	}
	refs := []string{"card:old_routing", "card:active-routing", "card:archived-routing", "card:trashed-routing", "card:external-routing", "card:private-routing", "card:missing-routing"}
	for _, actor := range []string{"reader", "owner"} {
		scope := WithRequestAccessScope(ctx, AccessScope{ActorID: actor})
		batch, err := s.WorkRoutingSnapshots(scope, refs)
		if err != nil {
			t.Fatal(err)
		}
		for _, ref := range refs {
			point, err := s.GetWork(scope, ref)
			if errors.Is(err, ErrNotFound) {
				if batch[ref] != nil {
					t.Fatalf("%s: batch disclosed point-hidden work %s", actor, ref)
				}
				continue
			}
			if err != nil {
				t.Fatalf("%s: point read %s: %v", actor, ref, err)
			}
			if batch[ref] == nil || anyStringValue(workMap(batch[ref]["source"])["authority"]) != anyStringValue(workMap(point["source"])["authority"]) {
				t.Fatalf("%s: routing differs for %s: batch=%v point=%v", actor, ref, batch[ref], point)
			}
		}
	}
}
