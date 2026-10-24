package primitives

import (
	"context"
	"database/sql"
	"testing"
)

func TestCandidateProofBorrowedWriteSeesUnversionedLedgerChanges(t *testing.T) {
	for _, mode := range []string{"candidate", "inventory"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			ws, err := initializeTestWorkspace(ctx, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
			private, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "Private proof source"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.PatchThread(ctx, "owner", private["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
				t.Fatal(err)
			}
			doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": private["id"]}, "body", "text", nil)
			if err != nil {
				t.Fatal(err)
			}
			// The empty compact ledger omits this bare-ID exact reference. Installing
			// its rows inside a borrowed writer changes authority without an epoch bump.
			if _, err := ws.DB().Exec(`UPDATE resource_access_compact_control SET reader=1,force_legacy=0`); err != nil {
				t.Fatal(err)
			}
			err = s.db.WriteTransaction(ctx, func(write context.Context, tx *accessTx) error {
				request := WithRequestAccessScope(write, AccessScope{ActorID: "reader"})
				if mode == "inventory" {
					request = WithInventoryAdmission(request)
				}
				pin, finish, err := s.BeginResourceRead(request)
				if err != nil {
					return err
				}
				defer finish()
				read := func(want int) {
					t.Helper()
					var count int
					if err := s.db.QueryRowContext(pin, `SELECT count(*) FROM documents WHERE id=?`, doc["id"]).Scan(&count); err != nil || count != want {
						t.Fatalf("count=%d want=%d err=%v", count, want, err)
					}
				}
				read(1)
				read(1) // Populate and reuse the candidate proof.
				maintenance := CanonicalMaintenanceContext(write)
				var before, after int64
				if err := tx.QueryRowContext(maintenance, `SELECT version FROM resource_access_epoch`).Scan(&before); err != nil {
					return err
				}
				if _, err := tx.ExecContext(maintenance, `INSERT INTO resource_access_compact_edges SELECT edge_id,unhex(target_key) FROM resource_access_exact_edges WHERE true ON CONFLICT(edge_id) DO UPDATE SET target_key=excluded.target_key`); err != nil {
					return err
				}
				if err := tx.QueryRowContext(maintenance, `SELECT version FROM resource_access_epoch`).Scan(&after); err != nil {
					return err
				}
				if before != after {
					t.Fatalf("fixture unexpectedly advanced authorization epoch: %d -> %d", before, after)
				}
				read(0) // total_changes(), rather than epoch alone, invalidates the proof.
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}

}

func TestCandidateProofMalformedAndExpiredCacheFallsBack(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for _, rows := range []string{`[["card",null]]`, `[["card",42]]`, `[["card","bad","extra"]]`, `[["card","bad"]] []`, "[[\"card\",\"bad\xff\"]]"} {
		snapshot := &denialSnapshot{candidates: true, candidateIndex: make(map[denialTarget]bool)}
		token := pointSnapshotSequence.Add(1)
		pointSnapshots.Store(token, snapshot)
		var allowed int
		if err := ws.DB().QueryRow(`SELECT anx_candidate_remember(?,CAST('card' AS BLOB),CAST('self' AS BLOB),1,CAST(? AS BLOB))`, token, rows).Scan(&allowed); err != nil || allowed != 1 {
			t.Fatalf("canonical result=%d err=%v", allowed, err)
		}
		for _, id := range []string{"bad", ""} {
			var got sql.NullInt64
			if err := ws.DB().QueryRow(`SELECT anx_candidate_lookup(?,CAST('card' AS BLOB),CAST(? AS BLOB))`, token, id).Scan(&got); err != nil || got.Valid {
				t.Fatalf("malformed proof cached %q: %v %v", id, got, err)
			}
		}
		pointSnapshots.Delete(token)
		var got sql.NullInt64
		if err := ws.DB().QueryRow(`SELECT anx_candidate_lookup(?,CAST('card' AS BLOB),CAST('self' AS BLOB))`, token).Scan(&got); err != nil || got.Valid {
			t.Fatalf("expired cache=%v err=%v", got, err)
		}
	}
}
