package primitives

import (
	"context"
	"testing"
	"time"
)

func TestWatchExplicitInitiativesSurviveDigestRouting(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "Outcomes", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	create := func(title string, fields map[string]any) map[string]any {
		t.Helper()
		if fields == nil {
			fields = map[string]any{}
		}
		fields["title"] = title
		card, err := s.CreateWork(ctx, "writer", workString(board["id"]), fields)
		if err != nil {
			t.Fatal(err)
		}
		return card
	}
	unowned := create("Unowned outcome", nil)
	orphaned := create("Awaiting reassignment", nil)
	reminded := create("Default reminder delivered", nil)
	released := create("Released worker", nil)
	expired := create("Expired worker", nil)
	for _, card := range []map[string]any{released, expired} {
		id := workString(card["id"])
		if _, err = s.AcquireCardLease(ctx, "writer", id, CardLeaseAcquireInput{Owner: "actor:writer", TTL: time.Minute}); err != nil {
			t.Fatal(err)
		}
		if card["id"] == released["id"] {
			_, err = ws.DB().Exec(`UPDATE card_leases SET released_at=? WHERE card_id=?`, cardLeaseStamp(time.Now()), id)
		} else {
			_, err = ws.DB().Exec(`UPDATE card_leases SET expires_at=? WHERE card_id=?`, cardLeaseStamp(time.Now().Add(-time.Hour)), id)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err = ws.DB().Exec(`UPDATE cards SET assignee=NULL WHERE id=?`, unowned["id"]); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`UPDATE work_tracking SET orphaned_at='2026-01-01',reminder_event_id='sent' WHERE card_id=?`, orphaned["id"]); err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`UPDATE work_tracking SET reminder_event_id='sent' WHERE card_id=?`, reminded["id"]); err != nil {
		t.Fatal(err)
	}
	// Explicit selection must still honor hierarchy, loop and lifecycle rules.
	create("Child", map[string]any{"under": unowned["ref"]})
	create("Recurring check", map[string]any{"cadence": map[string]any{"every_seconds": 3600}})
	archived := create("Archived outcome", nil)
	if _, err = s.ArchiveBoardCard(ctx, "writer", "", workString(archived["id"]), RemoveBoardCardInput{}); err != nil {
		t.Fatal(err)
	}
	trashed := create("Trashed outcome", nil)
	if _, err = s.TrashBoardCard(ctx, "writer", "", workString(trashed["id"]), "Obsolete", RemoveBoardCardInput{}); err != nil {
		t.Fatal(err)
	}
	private, err := s.CreateBoard(ctx, "writer", map[string]any{"title": "Private outcomes", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "writer", workString(private["thread_id"]), map[string]any{"pm_actor_id": "writer"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateWork(ctx, "writer", workString(private["id"]), map[string]any{"title": "Private outcome"}); err != nil {
		t.Fatal(err)
	}
	for drained := false; !drained; {
		drained, err = s.RefreshBriefInitiatives(ctx, 200)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []string{"human", "agent"} {
		readCtx := WithRequestAccessScope(ctx, AccessScope{ActorID: "viewer"})
		brief, err := s.Brief(readCtx, BriefReader{ActorID: "viewer", Kind: kind}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		items := brief["initiatives"].([]map[string]any)
		if len(items) != 5 {
			t.Fatalf("%s: want five explicit outcomes, got %d: %v", kind, len(items), items)
		}
		byRef := map[string]map[string]any{}
		for _, item := range items {
			byRef[workString(item["ref"])] = item
		}
		for _, card := range []map[string]any{unowned, orphaned, reminded, released, expired} {
			item := byRef[workString(card["ref"])]
			if item == nil {
				t.Fatalf("%s: missing %s", kind, card["title"])
			}
			wantChildren := 0
			if card["id"] == unowned["id"] {
				wantChildren = 1
			}
			if rollup := item["rollup"].(*WorkRollup); rollup.Total != wantChildren {
				t.Fatalf("%s: %s children=%d, want %d", kind, card["title"], rollup.Total, wantChildren)
			}
		}
	}
}

func TestWatchExplicitInitiativesExcludeInactiveBoards(t *testing.T) {
	for _, lifecycle := range []string{"archived", "trashed"} {
		t.Run(lifecycle, func(t *testing.T) {
			ctx := context.Background()
			ws, err := initializeTestWorkspace(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
			create := func(title string) (map[string]any, map[string]any) {
				t.Helper()
				board, err := s.CreateBoard(ctx, "writer", map[string]any{"title": title, "role": "initiatives"})
				if err != nil {
					t.Fatal(err)
				}
				card, err := s.CreateWork(ctx, "writer", workString(board["id"]), map[string]any{"title": title + " outcome"})
				if err != nil {
					t.Fatal(err)
				}
				if _, err = ws.DB().Exec(`UPDATE cards SET assignee=NULL WHERE id=?`, card["id"]); err != nil {
					t.Fatal(err)
				}
				return board, card
			}
			_, active := create("Active")
			inactiveBoard, inactive := create("Inactive")
			if lifecycle == "archived" {
				_, err = s.ArchiveBoard(ctx, "writer", workString(inactiveBoard["id"]))
			} else {
				_, err = s.TrashBoard(ctx, "writer", workString(inactiveBoard["id"]), "Obsolete")
			}
			if err != nil {
				t.Fatal(err)
			}
			// Only the board is inactive: card lifecycle filtering cannot hide it.
			var inactiveCards int
			if err = ws.DB().QueryRow(`SELECT count(*) FROM cards WHERE id=? AND (COALESCE(archived_at,'')<>'' OR COALESCE(trashed_at,'')<>'')`, inactive["id"]).Scan(&inactiveCards); err != nil {
				t.Fatal(err)
			}
			if inactiveCards != 0 {
				t.Fatal("expected the card itself to remain active")
			}
			for drained := false; !drained; {
				drained, err = s.RefreshBriefInitiatives(ctx, 200)
				if err != nil {
					t.Fatal(err)
				}
			}
			for _, kind := range []string{"human", "agent"} {
				readCtx := WithRequestAccessScope(ctx, AccessScope{ActorID: "viewer"})
				brief, err := s.Brief(readCtx, BriefReader{ActorID: "viewer", Kind: kind}, time.Now())
				if err != nil {
					t.Fatal(err)
				}
				items := brief["initiatives"].([]map[string]any)
				for _, item := range items {
					if item["ref"] == inactive["ref"] {
						t.Fatalf("%s: card on %s initiatives board is visible", kind, lifecycle)
					}
				}
				if len(items) != 1 || items[0]["ref"] != active["ref"] {
					t.Fatalf("%s: want only the active board's outcome, got %v", kind, items)
				}
			}
		})
	}
}
