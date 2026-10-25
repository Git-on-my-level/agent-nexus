package storage

import (
	"context"
	"testing"
)

func TestDocumentFTSPositionsRetainLegacyDuplicatesAndReconcile(t *testing.T) {
	ctx := context.Background()
	w, err := InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if _, err = w.DB().Exec(`INSERT INTO document_fts(document_id,title,body) VALUES('legacy','first','needle'),('legacy','second','needle')`); err != nil {
		t.Fatal(err)
	}
	reconcile := func() {
		t.Helper()
		tx, err := w.DB().BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err = installDocumentFTSPositions(ctx, tx); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	reconcile()
	var n int
	if err = w.DB().QueryRow(`SELECT count(*) FROM document_search_fts_positions WHERE document_id='legacy'`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("legacy duplicates=%d err=%v", n, err)
	}
	if _, err = w.DB().Exec(`DELETE FROM document_fts WHERE rowid IN (SELECT fts_rowid FROM document_search_fts_positions WHERE document_id='legacy')`); err != nil {
		t.Fatal(err)
	}
	reconcile()
	if err = w.DB().QueryRow(`SELECT count(*) FROM document_search_fts_positions`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("stale FTS positions=%d err=%v", n, err)
	}
	if _, err = w.DB().Exec(`INSERT INTO document_fts(document_fts) VALUES('integrity-check')`); err != nil {
		t.Fatal(err)
	}
}
