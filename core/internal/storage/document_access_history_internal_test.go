package storage

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// The backfill moves a bounded number of edges per chunk, resumes a partially
// moved revision, and converges to one deduplicated set per document.
func TestDocumentAccessHistoryBackfillIsBoundedAndResumable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w, err := InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	db := w.DB()
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type='trigger' AND name LIKE 'access_document_history_%'`)
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
	const revisions, refs = 6, 5
	if _, err = db.ExecContext(ctx, `INSERT INTO documents(id,handle,title,head_revision_id,head_revision_number,created_at,created_by,updated_at,updated_by) VALUES('doc-a','doc-a','legacy','rev-6',6,'t','owner','t','owner')`); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= revisions; i++ {
		var list []string
		for n := 0; n < refs; n++ {
			list = append(list, fmt.Sprintf(`"card:stable-%d"`, n))
		}
		list = append(list, fmt.Sprintf(`"event:rolling-%d"`, i))
		content := "[" + strings.Join(list, ",") + "]"
		if _, err = db.ExecContext(ctx, `INSERT INTO artifacts(id,handle,kind,created_at,created_by,content_type,content_hash,refs_json,metadata_json,content_refs_json) VALUES(?,?,'doc','t','owner','application/json','h','[]','{}',?)`, fmt.Sprintf("art-%d", i), fmt.Sprintf("art-%d", i), content); err != nil {
			t.Fatal(err)
		}
		if _, err = db.ExecContext(ctx, `INSERT INTO document_revisions(revision_id,document_id,revision_number,artifact_id,created_at,created_by) VALUES(?,'doc-a',?,?,'t','owner')`, fmt.Sprintf("rev-%d", i), i, fmt.Sprintf("art-%d", i)); err != nil {
			t.Fatal(err)
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
	legacy := count(`SELECT count(*) FROM resource_access_edges WHERE source_kind IN ('artifact','document_revision') AND (source_id LIKE 'art-%' OR source_id LIKE 'rev-%')`)
	if legacy < revisions*(refs+1) {
		t.Fatalf("legacy fixture has %d edges", legacy)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = installDocumentAccessHistory(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	chunks := 0
	for done := false; !done; chunks++ {
		if chunks > 100 {
			t.Fatal("backfill did not converge")
		}
		before := count(`SELECT count(*) FROM resource_access_edges WHERE source_kind IN ('artifact','document_revision') AND (source_id LIKE 'art-%' OR source_id LIKE 'rev-%')`)
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if done, err = moveDocumentAccessHistory(ctx, tx, 4); err != nil {
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
		after := count(`SELECT count(*) FROM resource_access_edges WHERE source_kind IN ('artifact','document_revision') AND (source_id LIKE 'art-%' OR source_id LIKE 'rev-%')`)
		if before-after > 4 {
			t.Fatalf("chunk moved %d edges, budget 4", before-after)
		}
	}
	if chunks < legacy/4 {
		t.Fatalf("%d edges moved in only %d chunks", legacy, chunks)
	}
	if n := count(`SELECT count(*) FROM resource_access_edges WHERE source_kind IN ('artifact','document_revision') AND (source_id LIKE 'art-%' OR source_id LIKE 'rev-%')`); n != 0 {
		t.Fatalf("%d per-revision edges left", n)
	}
	merged := count(`SELECT count(*) FROM resource_access_edges WHERE source_kind='document_history' AND source_id='doc-a'`)
	distinct := count(`SELECT count(DISTINCT lower(target_ref)) FROM resource_access_edges WHERE source_kind='document_history' AND source_id='doc-a'`)
	if merged != distinct || merged >= legacy || merged < refs+revisions {
		t.Fatalf("merged %d distinct %d legacy %d", merged, distinct, legacy)
	}
	// New revisions write straight into the document set.
	if _, err = db.ExecContext(ctx, `INSERT INTO artifacts(id,handle,kind,created_at,created_by,content_type,content_hash,refs_json,metadata_json,content_refs_json) VALUES('art-7','art-7','doc','t','owner','application/json','h','[]','{}','["card:stable-0","event:rolling-7"]')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO document_revisions(revision_id,document_id,revision_number,artifact_id,prev_revision_id,created_at,created_by) VALUES('rev-7','doc-a',7,'art-7','rev-6','t','owner')`); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT count(*) FROM resource_access_edges WHERE source_id IN ('art-7','rev-7')`); n != 0 {
		t.Fatalf("new revision kept %d own edges", n)
	}
	if got := count(`SELECT count(*) FROM resource_access_edges WHERE source_kind='document_history' AND source_id='doc-a'`); got > merged+4 {
		t.Fatalf("new revision added %d edges", got-merged)
	}
	// Detaching an artifact restores its own edges.
	if _, err = db.ExecContext(ctx, `DELETE FROM document_revisions WHERE revision_id='rev-7'`); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT count(*) FROM resource_access_edges WHERE source_kind='artifact' AND source_id='art-7'`); n == 0 {
		t.Fatal("detached artifact lost its edges")
	}
	// An artifact that starts backing a card revision keeps its own edges,
	// so the card's denial still follows its targets.
	if _, err = db.ExecContext(ctx, `INSERT INTO card_revisions(revision_id,card_id,revision_number,artifact_id,created_at,created_by) VALUES('card-rev-1','card-a',1,'art-1','t','owner')`); err != nil {
		t.Fatal(err)
	}
	if n := count(`SELECT count(*) FROM resource_access_edges WHERE source_kind='artifact' AND source_id='art-1'`); n < refs+1 {
		t.Fatalf("card-backed artifact has %d own edges", n)
	}
}
