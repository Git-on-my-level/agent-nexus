package storage

import (
	"context"
	"testing"
)

func TestInstallAuthAuditAccessCleanupDropsTriggersAndKeepsEdges(t *testing.T) {
	ctx := context.Background()
	w, err := InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	if _, err = w.DB().ExecContext(ctx, `INSERT INTO resource_access_edges(source_kind,source_id,target_ref) VALUES(?,?,?),(?,?,?)`,
		authAuditAccessSourceKind, "audit-1", "card:private",
		"card", "card-1", "card:private"); err != nil {
		t.Fatal(err)
	}
	// Recreate the legacy triggers so this test proves the installer drops them.
	for _, name := range []string{"access_auth_audit_events_insert", "access_auth_audit_events_update", "access_auth_audit_events_delete"} {
		if _, err = w.DB().ExecContext(ctx, `CREATE TRIGGER `+name+` AFTER INSERT ON auth_audit_events BEGIN SELECT 1; END`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = w.DB().ExecContext(ctx, `UPDATE `+authAuditAccessJobTable+` SET cursor=99,done=1 WHERE singleton=1`); err != nil {
		t.Fatal(err)
	}

	tx, err := w.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = installAuthAuditAccessCleanup(ctx, tx); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}

	var authEdges, otherEdges, triggers int
	if err = w.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM resource_access_edges WHERE source_kind=?`, authAuditAccessSourceKind).Scan(&authEdges); err != nil {
		t.Fatal(err)
	}
	if err = w.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM resource_access_edges WHERE source_kind='card'`).Scan(&otherEdges); err != nil {
		t.Fatal(err)
	}
	if err = w.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema WHERE type='trigger' AND name IN ('access_auth_audit_events_insert','access_auth_audit_events_update','access_auth_audit_events_delete')`).Scan(&triggers); err != nil {
		t.Fatal(err)
	}
	if authEdges != 1 || otherEdges != 1 {
		t.Fatalf("migration deleted edges: auth=%d other=%d", authEdges, otherEdges)
	}
	if triggers != 0 {
		t.Fatalf("legacy audit triggers remain: %d", triggers)
	}
	var cursor *int64
	var done bool
	if err = w.DB().QueryRowContext(ctx, `SELECT cursor,done FROM `+authAuditAccessJobTable+` WHERE singleton=1`).Scan(&cursor, &done); err != nil {
		t.Fatal(err)
	}
	if cursor != nil || done {
		t.Fatalf("job was not reset: cursor=%v done=%v", cursor, done)
	}
}

func TestMaintainAuthAuditAccessBatchDeletesOnlyAuditEdgesAndResumes(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	w, err := InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err = w.DB().ExecContext(ctx, `INSERT INTO resource_access_edges(source_kind,source_id,target_ref) VALUES(?,?,?)`,
			authAuditAccessSourceKind, "audit-"+string(rune('a'+i)), "card:private-"+string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if _, err = w.DB().ExecContext(ctx, `INSERT INTO resource_access_edges(source_kind,source_id,target_ref) VALUES(?,?,?)`,
			"card", "card-"+string(rune('a'+i)), "card:private-"+string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}
	count := func(kind string) int {
		t.Helper()
		var n int
		if err := w.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM resource_access_edges WHERE source_kind=?`, kind).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	done, err := w.maintainAuthAuditAccessChunk(ctx, 2)
	if err != nil || done {
		t.Fatalf("first chunk: done=%v err=%v", done, err)
	}
	if got := count(authAuditAccessSourceKind); got != 3 {
		t.Fatalf("first chunk auth edges=%d", got)
	}
	if got := count("card"); got != 2 {
		t.Fatalf("first chunk deleted non-audit edges: %d", got)
	}

	// Reopen the workspace to prove the cursor is durable across interruption.
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	w, err = InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	done, err = w.maintainAuthAuditAccessChunk(ctx, 2)
	if err != nil || done {
		t.Fatalf("resumed chunk: done=%v err=%v", done, err)
	}
	if got := count(authAuditAccessSourceKind); got != 1 {
		t.Fatalf("resumed chunk auth edges=%d", got)
	}
	if got := count("card"); got != 2 {
		t.Fatalf("resumed chunk deleted non-audit edges: %d", got)
	}

	done, err = w.maintainAuthAuditAccessChunk(ctx, 2)
	if err != nil || !done {
		t.Fatalf("final chunk: done=%v err=%v", done, err)
	}
	if got := count(authAuditAccessSourceKind); got != 0 {
		t.Fatalf("final chunk auth edges=%d", got)
	}
	if got := count("card"); got != 2 {
		t.Fatalf("final chunk deleted non-audit edges: %d", got)
	}
	var jobDone bool
	if err = w.DB().QueryRowContext(ctx, `SELECT done FROM `+authAuditAccessJobTable+` WHERE singleton=1`).Scan(&jobDone); err != nil {
		t.Fatal(err)
	}
	if !jobDone {
		t.Fatal("job did not finish")
	}
}
