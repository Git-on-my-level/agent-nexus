package storage

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
)

// TestRetireLegacyAccessLedgerPreservesAdmissionDecisions builds a workspace on
// the pre-150 schema, certifies the compact ledger, records admission decisions
// for a corpus of principals and resources, then applies migration 150 and
// requires every decision to be identical.
func TestRetireLegacyAccessLedgerPreservesAdmissionDecisions(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 149)
	store := primitives.NewTestStore(db, t.TempDir())

	private, err := store.CreateWork(ctx, "owner", "", map[string]any{"title": "private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, "owner", private["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	public, err := store.CreateWork(ctx, "owner", "", map[string]any{"title": "public"})
	if err != nil {
		t.Fatal(err)
	}
	doc, _, err := store.CreateDocument(ctx, "owner", map[string]any{"title": "private document"}, "private body", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, "owner", doc["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	event, err := store.AppendEvent(ctx, "owner", map[string]any{"type": "message_posted", "refs": []string{}, "payload": map[string]any{"subject_ref": private["ref"]}})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.CreateArtifact(ctx, "owner", map[string]any{"kind": "note", "refs": []string{private["ref"].(string)}}, map[string]any{"ref": private["ref"], "title": "secret"}, "structured")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO resource_access_tombstones(kind,id,ref,owner) VALUES('card','purged-card','card:purged-card','owner')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at,metadata_json) VALUES('corpus-ref','card',?,'card',?,'ref','now','{}')`, public["id"], private["id"]); err != nil {
		t.Fatal(err)
	}

	// Certify the compact ledger on the old schema. Migration 150 refuses to
	// retire the legacy ledger until raw edge identities are stable.
	w := &Workspace{layout: NewLayout(root), db: db, processLock: &scopeProcessLease{refs: 1}}
	if err := w.initializeCompactAccessMetadata(ctx); err != nil {
		t.Fatal(err)
	}
	proof := compactCertify(t, w)
	if err := cutoverForTest(t, w, ctx, proof); err != nil {
		t.Fatal(err)
	}

	resources := map[string]string{
		"private-card":     private["id"].(string),
		"public-card":      public["id"].(string),
		"private-document": doc["id"].(string),
		"private-event":    event["id"].(string),
		"private-artifact": artifact["id"].(string),
		"private-thread":   private["thread_id"].(string),
		"public-thread":    public["thread_id"].(string),
	}
	kinds := map[string]string{
		"private-card":     "card",
		"public-card":      "card",
		"private-document": "document",
		"private-event":    "event",
		"private-artifact": "artifact",
		"private-thread":   "thread",
		"public-thread":    "thread",
	}
	principals := []string{"owner", "stranger", "pm"}
	before := map[string]bool{}
	for _, principal := range principals {
		scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: principal, PMActorID: "pm"})
		for name, id := range resources {
			before[principal+"/"+name] = store.CanAccessResource(scope, kinds[name], id)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	ws, err := InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	store = primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	for _, principal := range principals {
		scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: principal, PMActorID: "pm"})
		for name, id := range resources {
			got := store.CanAccessResource(scope, kinds[name], id)
			if want := before[principal+"/"+name]; got != want {
				t.Errorf("admission changed for %s/%s: before=%v after=%v", principal, name, want, got)
			}
		}
	}
	for _, table := range []string{"resource_access_exact_edges", "resource_access_admission_compact_atoms"} {
		var exists bool
		if err := ws.DB().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type='table' AND name=?)`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatalf("retired table still exists: %s", table)
		}
	}
}

// TestRetireLegacyAccessLedgerAbortsOnMismatch proves the migration refuses to
// drop the legacy ledger when the compact ledger diverges.
func TestRetireLegacyAccessLedgerAbortsOnMismatch(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 149)
	if _, err := db.ExecContext(ctx, `INSERT INTO resource_access_edges(source_kind,source_id,target_ref) VALUES('card','source','document:target')`); err != nil {
		t.Fatal(err)
	}
	w := &Workspace{layout: NewLayout(root), db: db, processLock: &scopeProcessLease{refs: 1}}
	if err := w.initializeCompactAccessMetadata(ctx); err != nil {
		t.Fatal(err)
	}
	proof := compactCertify(t, w)
	if err := cutoverForTest(t, w, ctx, proof); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE resource_access_compact_edges SET target_key=zeroblob(32) WHERE edge_id=(SELECT min(edge_id) FROM resource_access_compact_edges)`); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if opened, err := InitializeWorkspace(ctx, root); err == nil {
		opened.Close()
		t.Fatal("migration accepted a divergent compact ledger")
	}
}

// TestAdmissionLinkIndexesArePartialAndUsed asserts the narrowed indexes exist
// and that every statement which used the full indexes keeps an index-backed
// plan. The predicates mirror access_admission.go exactly.
func TestAdmissionLinkIndexesArePartialAndUsed(t *testing.T) {
	ctx := context.Background()
	ws, err := InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for _, index := range []string{"idx_access_admission_child", "idx_access_admission_parent", "idx_access_admission_via"} {
		var sql string
		if err := ws.DB().QueryRowContext(ctx, `SELECT sql FROM sqlite_schema WHERE type='index' AND name=?`, index).Scan(&sql); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(sql, " WHERE ") {
			t.Fatalf("%s is not partial: %s", index, sql)
		}
	}
	for _, tc := range []struct {
		query string
		index string
	}{
		{`DELETE FROM resource_access_admission_links WHERE origin='document_comment' AND parent_kind='event' AND parent_id='x'`, "idx_access_admission_parent"},
		{`DELETE FROM resource_access_admission_links WHERE origin='comment' AND child_kind='document' AND child_id='x'`, "idx_access_admission_child"},
		{`DELETE FROM resource_access_admission_links WHERE origin IN ('exact','compact','external','ref','project') AND via_id=1`, "idx_access_admission_via"},
	} {
		rows, err := ws.DB().QueryContext(ctx, "EXPLAIN QUERY PLAN "+tc.query)
		if err != nil {
			t.Fatal(err)
		}
		var details []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			details = append(details, detail)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			t.Fatal(err)
		}
		rows.Close()
		plan := strings.Join(details, "\n")
		if !strings.Contains(plan, "SEARCH resource_access_admission_links") || !strings.Contains(plan, tc.index) {
			t.Errorf("%s does not use %s:\n%s", tc.query, tc.index, plan)
		}
		if strings.Contains(plan, "SCAN resource_access_admission_links") {
			t.Errorf("%s scans the link ledger:\n%s", tc.query, plan)
		}
	}
}

// TestMeasureLegacyAccessLedgerReduction records the physical saving from
// retiring the legacy exact ledger and the admission compact mirror. It uses a
// bounded synthetic corpus so the measurement is reproducible on a laptop; the
// dense fixture remains the CI performance tier.
func TestMeasureLegacyAccessLedgerReduction(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	db := mainHistoryDatabase(t, root, 149)
	const edges = 20000
	if _, err := db.ExecContext(ctx, `WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<?)
		INSERT INTO resource_access_edges(source_kind,source_id,target_ref)
		SELECT 'card','source-'||i,'document:target-'||(i%97) FROM n`, edges); err != nil {
		t.Fatal(err)
	}
	w := &Workspace{layout: NewLayout(root), db: db, processLock: &scopeProcessLease{refs: 1}}
	if err := w.initializeCompactAccessMetadata(ctx); err != nil {
		t.Fatal(err)
	}
	proof := compactCertify(t, w)
	if err := cutoverForTest(t, w, ctx, proof); err != nil {
		t.Fatal(err)
	}
	size := func(handle interface {
		QueryRow(string, ...any) *sql.Row
	}) int64 {
		t.Helper()
		var busy, logPages, checkpointed int
		if err := handle.QueryRow(`PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logPages, &checkpointed); err != nil || busy != 0 {
			t.Fatalf("checkpoint: %d %v", busy, err)
		}
		info, err := os.Stat(w.Layout().DatabasePath)
		if err != nil {
			t.Fatal(err)
		}
		return info.Size()
	}
	before := size(db)
	var exactBytes, atomBytes int64
	if err := db.QueryRow(`SELECT COALESCE(sum(pgsize),0) FROM dbstat WHERE name IN ('resource_access_exact_edges','idx_access_exact_edges_target')`).Scan(&exactBytes); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COALESCE(sum(pgsize),0) FROM dbstat WHERE name IN ('resource_access_admission_compact_atoms','idx_admission_compact_atom')`).Scan(&atomBytes); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err := InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	after := size(ws.DB())
	destination := filepath.Join(t.TempDir(), "reclaimed.sqlite")
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ReclaimCompactAccessOffline(ctx, NewLayout(root).DatabasePath, destination); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("edges=%d exact_component_bytes=%d admission_mirror_bytes=%d db_before=%d db_after_migration=%d db_after_reclamation=%d saved_bytes=%d",
		edges, exactBytes, atomBytes, before, after, info.Size(), before-info.Size())
	if info.Size() >= before {
		t.Fatal("reclamation did not reduce the file")
	}
}

// TestRetireLegacyAccessLedgerFreshWorkspace proves a fresh workspace reaches
// the same final schema without a legacy ledger or compact mirror.
func TestRetireLegacyAccessLedgerFreshWorkspace(t *testing.T) {
	ctx := context.Background()
	ws, err := InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for _, table := range []string{"resource_access_exact_edges", "resource_access_admission_compact_atoms"} {
		var exists bool
		if err := ws.DB().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE type='table' AND name=?)`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatalf("fresh workspace retained %s", table)
		}
	}
	var reader, stable int
	if err := ws.DB().QueryRowContext(ctx, `SELECT reader,stable FROM resource_access_compact_control WHERE singleton=1`).Scan(&reader, &stable); err != nil {
		t.Fatal(err)
	}
	if reader != 1 || stable != 1 {
		t.Fatalf("fresh workspace reader=%d stable=%d", reader, stable)
	}
	// A canonical write must populate the compact ledger directly.
	if _, err := ws.DB().ExecContext(ctx, `INSERT INTO resource_access_edges(source_kind,source_id,target_ref) VALUES('card','fresh','document:fresh')`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := ws.DB().QueryRowContext(ctx, `SELECT count(*) FROM resource_access_compact_edges`).Scan(&n); err != nil || n == 0 {
		t.Fatalf("compact ledger not written: %d %v", n, err)
	}
}
