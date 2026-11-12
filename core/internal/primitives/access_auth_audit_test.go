package primitives

import (
	"context"
	"reflect"
	"testing"
)

// Removing legacy auth-audit filter edges must not change any non-audit
// resource's denial outcome. The filter source itself may disappear from the
// closure; ordinary resources must not.
func TestAuthAuditFilterCleanupPreservesNonAuditDenials(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)

	board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "private board"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", anyStringValue(board["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	card, err := s.CreateBoardCard(ctx, "owner", anyStringValue(board["id"]), AddBoardCardInput{Title: "private card", Body: "secret", ColumnKey: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	cardID := anyStringValue(card.Card["id"])
	if _, err = ws.DB().ExecContext(ctx, `INSERT INTO resource_access_edges(source_kind,source_id,target_ref) VALUES(?,?,?)`,
		"filter/auth_audit_events", "audit-1", "card:"+cardID); err != nil {
		t.Fatal(err)
	}

	scope := WithAccessScope(ctx, AccessScope{ActorID: "stranger"})
	denied := func() map[string]bool {
		t.Helper()
		rows, err := s.db.QueryContext(scope, `SELECT kind||':'||id FROM _anx_denied WHERE kind NOT LIKE 'filter/%'`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := map[string]bool{}
		for rows.Next() {
			var ref string
			if err := rows.Scan(&ref); err != nil {
				t.Fatal(err)
			}
			out[ref] = true
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	before := denied()
	if !before["card:"+cardID] {
		t.Fatalf("private card was not denied before cleanup: %#v", before)
	}

	for done := false; !done; {
		done, err = ws.MaintainAuthAuditAccessBatch(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	after := denied()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("non-audit denial set changed:\nbefore=%#v\nafter=%#v", before, after)
	}
	var remaining int
	if err = ws.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM resource_access_edges WHERE source_kind='filter/auth_audit_events'`).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("audit filter edges remain: %d", remaining)
	}
}
