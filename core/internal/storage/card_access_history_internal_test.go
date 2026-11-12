package storage

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// legacyCardHistoryWorkspace holds per-revision edges written before the card
// history triggers existed, with the backfill job pending.
func legacyCardHistoryWorkspace(t *testing.T, cards, revisions, refs int) (*Workspace, func(string) int) {
	t.Helper()
	ctx := context.Background()
	w, err := InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	db := w.DB()
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type='trigger' AND name LIKE 'access_card_history_%'`)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	rows.Close()
	for _, name := range names {
		if _, err = db.ExecContext(ctx, `DROP TRIGGER `+name); err != nil {
			t.Fatal(err)
		}
	}
	for c := 0; c < cards; c++ {
		cardID := fmt.Sprintf("card-%c", 'a'+c)
		if _, err = db.ExecContext(ctx, `INSERT INTO cards(id,title,created_at,created_by,updated_at,updated_by) VALUES(?,?,'t','owner','t','owner')`, cardID, cardID); err != nil {
			t.Fatal(err)
		}
		for i := 1; i <= revisions; i++ {
			var list []string
			for n := 0; n < refs; n++ {
				list = append(list, fmt.Sprintf(`"event:%s-stable-%d"`, cardID, n))
			}
			list = append(list, fmt.Sprintf(`"event:%s-rolling-%d"`, cardID, i))
			revisionID, artifactID := fmt.Sprintf("%s-rev-%d", cardID, i), fmt.Sprintf("%s-art-%d", cardID, i)
			if _, err = db.ExecContext(ctx, `INSERT INTO artifacts(id,handle,kind,created_at,created_by,content_type,content_hash,refs_json,metadata_json,content_refs_json) VALUES(?,?,'card','t','owner','application/json','h','[]','{}',?)`, artifactID, artifactID, "["+strings.Join(list, ",")+"]"); err != nil {
				t.Fatal(err)
			}
			if _, err = db.ExecContext(ctx, `INSERT INTO card_revisions(revision_id,card_id,revision_number,artifact_id,created_at,created_by) VALUES(?,?,?,?,'t','owner')`, revisionID, cardID, i, artifactID); err != nil {
				t.Fatal(err)
			}
		}
	}
	count := func(query string) int {
		t.Helper()
		var n int
		if err := db.QueryRowContext(ctx, query).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	return w, count
}

// The backfill moves a bounded number of edges per chunk, resumes a partially
// moved revision, and converges to one deduplicated set per card.
func TestCardAccessHistoryBackfillIsBoundedAndResumable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w, count := legacyCardHistoryWorkspace(t, 2, 6, 5)
	db := w.DB()
	perRevision := `SELECT count(*) FROM resource_access_edges WHERE source_kind IN ('artifact','card_revision') AND (source_id LIKE 'card-%-art-%' OR source_id LIKE 'card-%-rev-%')`
	legacy := count(perRevision)
	if legacy < 2*6*6 {
		t.Fatalf("legacy fixture has %d edges", legacy)
	}
	// Simulate an upgraded workspace: admission was installed before card
	// history existed, so its relationship triggers and rows are absent.
	for _, q := range []string{
		`DROP TRIGGER IF EXISTS admission_card_history_insert`,
		`DROP TRIGGER IF EXISTS admission_card_history_update`,
		`DROP TRIGGER IF EXISTS admission_card_history_delete`,
		`DELETE FROM resource_access_admission_links WHERE origin='card_history'`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = installCardAccessHistory(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT count(*) FROM resource_access_admission_links WHERE origin='card_history' AND parent_kind='card_history' AND child_kind='card'`); n != 2 {
		t.Fatalf("card history admission links = %d, want 2", n)
	}
	chunks := 0
	for done := false; !done; chunks++ {
		if chunks > 200 {
			t.Fatal("backfill did not converge")
		}
		before := count(perRevision)
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if done, err = moveCardAccessHistory(ctx, tx, 4); err != nil {
			t.Fatal(err)
		}
		if chunks == 2 {
			// A crash before commit leaves the committed cursor untouched.
			if err = tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			done = false
			continue
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
		after := count(perRevision)
		if before-after > 4 {
			t.Fatalf("chunk moved %d edges, budget 4", before-after)
		}
	}
	if n := count(perRevision); n != 0 {
		t.Fatalf("%d per-revision edges left", n)
	}
	merged := count(`SELECT count(*) FROM resource_access_edges WHERE source_kind='card_history'`)
	distinct := count(`SELECT count(*) FROM (SELECT DISTINCT source_id,lower(target_ref) FROM resource_access_edges WHERE source_kind='card_history')`)
	if merged != distinct || merged >= legacy {
		t.Fatalf("merged %d distinct %d legacy %d", merged, distinct, legacy)
	}
	// New revisions write straight into the card set.
	if _, err = db.ExecContext(ctx, `INSERT INTO artifacts(id,handle,kind,created_at,created_by,content_type,content_hash,refs_json,metadata_json,content_refs_json) VALUES('card-a-art-7','card-a-art-7','card','t','owner','application/json','h','[]','{}','["event:card-a-stable-0","event:card-a-rolling-7"]')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO card_revisions(revision_id,card_id,revision_number,prev_revision_id,artifact_id,created_at,created_by) VALUES('card-a-rev-7','card-a',7,'card-a-rev-6','card-a-art-7','t','owner')`); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT count(*) FROM resource_access_edges WHERE source_id IN ('card-a-art-7','card-a-rev-7')`); n != 0 {
		t.Fatalf("new revision kept %d own edges", n)
	}
	// Detaching an artifact restores its own edges.
	if _, err = db.ExecContext(ctx, `DELETE FROM card_revisions WHERE revision_id='card-a-rev-7'`); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT count(*) FROM resource_access_edges WHERE source_kind='artifact' AND source_id='card-a-art-7'`); n == 0 {
		t.Fatal("detached artifact lost its edges")
	}
	// A card artifact shared with a document keeps its own edges so the
	// document's denial still follows the artifact's targets.
	if _, err = db.ExecContext(ctx, `INSERT INTO document_revisions(revision_id,document_id,revision_number,artifact_id,created_at,created_by) VALUES('doc-rev-1','doc-a',1,'card-a-art-1','t','owner')`); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT count(*) FROM resource_access_edges WHERE source_kind='artifact' AND source_id='card-a-art-1'`); n == 0 {
		t.Fatal("shared artifact lost its own edges")
	}
}
