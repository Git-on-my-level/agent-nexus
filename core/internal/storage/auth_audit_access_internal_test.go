package storage

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/resourceaccess"
)

// legacyScopeInboxTrigger is the exact preview template that installed
// scope_inbox_invalidate_auth_audit_events_* before the ledger dropped the
// source. The installer must remove these by name even though the ledger no
// longer generates them.
func legacyScopeInboxTrigger(table, op string) string {
	return `CREATE TRIGGER scope_inbox_invalidate_` + table + `_` + strings.ToLower(op) + `
BEFORE ` + op + ` ON ` + table + ` BEGIN
 SELECT CASE WHEN NOT EXISTS(SELECT 1 FROM resource_access_epoch WHERE singleton=1
  AND typeof(version)='integer' AND version>=0 AND version<9223372036854775807)
  THEN RAISE(ROLLBACK,'unavailable inbox source epoch') END;
 UPDATE resource_access_epoch SET version=version+1 WHERE singleton=1;
END`
}

func authAuditRows(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT quote(id),quote(event_type),quote(occurred_at),quote(occurred_at_sort_key),quote(actor_agent_id),quote(actor_actor_id),quote(subject_agent_id),quote(subject_actor_id),quote(invite_id),quote(metadata_json) FROM auth_audit_events ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id, eventType, occurredAt, sortKey, actorAgent, actorActor, subjectAgent, subjectActor, invite, metadata string
		if err := rows.Scan(&id, &eventType, &occurredAt, &sortKey, &actorAgent, &actorActor, &subjectAgent, &subjectActor, &invite, &metadata); err != nil {
			t.Fatal(err)
		}
		out = append(out, strings.Join([]string{id, eventType, occurredAt, sortKey, actorAgent, actorActor, subjectAgent, subjectActor, invite, metadata}, "|"))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestInstallAuthAuditAccessCleanupDropsLegacyTriggersAndKeepsEdges(t *testing.T) {
	ctx := context.Background()
	w, err := InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	// Seed a real audit row whose metadata used to publish a filter edge.
	if _, err = w.DB().ExecContext(ctx, `INSERT INTO auth_audit_events(id,event_type,occurred_at,occurred_at_sort_key,metadata_json) VALUES(?,?,?,?,?)`,
		"audit-1", "invite_created", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z", `{"label":"card:private","reason":"user supplied"}`); err != nil {
		t.Fatal(err)
	}
	if _, err = w.DB().ExecContext(ctx, `INSERT INTO resource_access_edges(source_kind,source_id,target_ref) VALUES(?,?,?)`,
		"card", "card-1", "card:private"); err != nil {
		t.Fatal(err)
	}

	// Recreate the real legacy edge triggers with the generator that installed
	// them for the old filter source, including its backfill.
	tx, err := w.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = installResourceAccessSourceEdges(ctx, tx, []resourceaccess.OwnershipSource{{
		Table: "auth_audit_events", Kind: authAuditAccessSourceKind, ID: "id", Columns: []string{"metadata_json"},
	}}); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// Recreate the real preview scope-inbox triggers.
	for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
		if _, err = w.DB().ExecContext(ctx, legacyScopeInboxTrigger("auth_audit_events", op)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = w.DB().ExecContext(ctx, `UPDATE `+authAuditAccessJobTable+` SET cursor=99,done=1 WHERE singleton=1`); err != nil {
		t.Fatal(err)
	}

	var authEdges int
	if err = w.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM resource_access_edges WHERE source_kind=?`, authAuditAccessSourceKind).Scan(&authEdges); err != nil {
		t.Fatal(err)
	}
	if authEdges == 0 {
		t.Fatal("legacy trigger did not publish any audit edges")
	}
	beforeAuthEdges := authEdges

	tx, err = w.DB().BeginTx(ctx, nil)
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

	var otherEdges, triggers int
	if err = w.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM resource_access_edges WHERE source_kind=?`, authAuditAccessSourceKind).Scan(&authEdges); err != nil {
		t.Fatal(err)
	}
	if err = w.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM resource_access_edges WHERE source_kind='card'`).Scan(&otherEdges); err != nil {
		t.Fatal(err)
	}
	if err = w.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_schema WHERE type='trigger' AND name IN ('access_auth_audit_events_insert','access_auth_audit_events_update','access_auth_audit_events_delete','scope_inbox_invalidate_auth_audit_events_insert','scope_inbox_invalidate_auth_audit_events_update','scope_inbox_invalidate_auth_audit_events_delete')`).Scan(&triggers); err != nil {
		t.Fatal(err)
	}
	if authEdges != beforeAuthEdges || otherEdges != 1 {
		t.Fatalf("migration deleted edges: auth=%d (was %d) other=%d", authEdges, beforeAuthEdges, otherEdges)
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

	// The third window holds the last audit edge and the first non-audit edge,
	// so the cursor must still advance and a fourth window publishes completion.
	done, err = w.maintainAuthAuditAccessChunk(ctx, 2)
	if err != nil || done {
		t.Fatalf("third chunk: done=%v err=%v", done, err)
	}
	if got := count(authAuditAccessSourceKind); got != 0 {
		t.Fatalf("third chunk auth edges=%d", got)
	}
	if got := count("card"); got != 2 {
		t.Fatalf("third chunk deleted non-audit edges: %d", got)
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

func TestMaintainAuthAuditAccessAdvancesPastSparseWindows(t *testing.T) {
	ctx := context.Background()
	w, err := InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	for i := 0; i < 3; i++ {
		if _, err = w.DB().ExecContext(ctx, `INSERT INTO resource_access_edges(source_kind,source_id,target_ref) VALUES(?,?,?)`,
			"card", "card-"+string(rune('a'+i)), "card:private-"+string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = w.DB().ExecContext(ctx, `INSERT INTO resource_access_edges(source_kind,source_id,target_ref) VALUES(?,?,?)`,
		authAuditAccessSourceKind, "audit-last", "card:private-last"); err != nil {
		t.Fatal(err)
	}

	// The first window contains no audit edges at all; it must still move the
	// cursor so the job cannot stall on a sparse legacy table.
	done, err := w.maintainAuthAuditAccessChunk(ctx, 2)
	if err != nil || done {
		t.Fatalf("sparse window: done=%v err=%v", done, err)
	}
	var cursor sql.NullInt64
	if err = w.DB().QueryRowContext(ctx, `SELECT cursor FROM `+authAuditAccessJobTable+` WHERE singleton=1`).Scan(&cursor); err != nil {
		t.Fatal(err)
	}
	if !cursor.Valid || cursor.Int64 != 2 {
		t.Fatalf("cursor did not advance past the sparse window: %+v", cursor)
	}

	done, err = w.maintainAuthAuditAccessChunk(ctx, 2)
	if err != nil || !done {
		t.Fatalf("final window: done=%v err=%v", done, err)
	}
	var authEdges, cardEdges int
	if err = w.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM resource_access_edges WHERE source_kind=?`, authAuditAccessSourceKind).Scan(&authEdges); err != nil {
		t.Fatal(err)
	}
	if err = w.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM resource_access_edges WHERE source_kind='card'`).Scan(&cardEdges); err != nil {
		t.Fatal(err)
	}
	if authEdges != 0 || cardEdges != 3 {
		t.Fatalf("cleanup removed the wrong edges: auth=%d card=%d", authEdges, cardEdges)
	}
}

func TestMaintainAuthAuditAccessStopsBetweenDeleteBatches(t *testing.T) {
	ctx := context.Background()
	w, err := InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	for i := 0; i < 100; i++ {
		if _, err = w.DB().ExecContext(ctx, `INSERT INTO resource_access_edges(source_kind,source_id,target_ref) VALUES(?,?,?)`,
			authAuditAccessSourceKind, "audit-"+string(rune('a'+i%26))+"-"+string(rune('a'+i/26)), "card:private-"+string(rune('a'+i%26))+"-"+string(rune('a'+i/26))); err != nil {
			t.Fatal(err)
		}
	}
	stopped := false
	done, err := w.maintainAuthAuditAccessChunkUntil(ctx, 100, time.Hour, func() bool {
		if stopped {
			return true
		}
		stopped = true
		return true
	})
	if err != nil || done {
		t.Fatalf("stopped chunk: done=%v err=%v", done, err)
	}
	var remaining int
	if err = w.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM resource_access_edges WHERE source_kind=?`, authAuditAccessSourceKind).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 100-authAuditAccessDeleteBatch {
		t.Fatalf("stopped chunk deleted %d edges, want %d", 100-remaining, authAuditAccessDeleteBatch)
	}

	done, err = w.maintainAuthAuditAccessChunk(ctx, 100)
	if err != nil || !done {
		t.Fatalf("resumed chunk: done=%v err=%v", done, err)
	}
	if err = w.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM resource_access_edges WHERE source_kind=?`, authAuditAccessSourceKind).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("resumed chunk left %d audit edges", remaining)
	}
}

func TestAuthAuditAccessScanUsesRowidRangeWithoutSort(t *testing.T) {
	ctx := context.Background()
	w, err := InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	rows, err := w.DB().QueryContext(ctx, `EXPLAIN QUERY PLAN `+authAuditAccessScanQuery, 0, authAuditAccessBatchSize)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(plan, "\n")
	if !strings.Contains(joined, "SEARCH resource_access_edges USING INTEGER PRIMARY KEY (rowid>?)") {
		t.Fatalf("cleanup scan is not a rowid range scan:\n%s", joined)
	}
	if strings.Contains(strings.ToUpper(joined), "TEMP B-TREE") {
		t.Fatalf("cleanup scan sorts remaining edges:\n%s", joined)
	}
}

func TestAuthAuditAccessCleanupPreservesAuditRows(t *testing.T) {
	ctx := context.Background()
	w, err := InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	for i, metadata := range []string{
		`{"label":"user supplied label","reason":"user supplied reason"}`,
		`{"nested":{"label":"card:private"},"tags":["one","two"]}`,
		`{"reason":"revoked by operator","note":"keep me"}`,
	} {
		if _, err = w.DB().ExecContext(ctx, `INSERT INTO auth_audit_events(id,event_type,occurred_at,occurred_at_sort_key,actor_agent_id,actor_actor_id,subject_agent_id,subject_actor_id,invite_id,metadata_json) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			"audit-"+string(rune('a'+i)), "invite_created", "2026-01-01T00:00:0"+string(rune('0'+i))+"Z", "2026-01-01T00:00:0"+string(rune('0'+i))+"Z",
			"agent-1", "actor-1", "agent-2", "actor-2", "invite-1", metadata); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = w.DB().ExecContext(ctx, `INSERT INTO resource_access_edges(source_kind,source_id,target_ref) VALUES(?,?,?),(?,?,?)`,
		authAuditAccessSourceKind, "audit-a", "card:private",
		"card", "card-1", "card:private"); err != nil {
		t.Fatal(err)
	}
	before := authAuditRows(t, w.DB())

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
	for {
		done, err := w.maintainAuthAuditAccessChunk(ctx, 2)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
	}
	after := authAuditRows(t, w.DB())
	if strings.Join(before, "\n") != strings.Join(after, "\n") {
		t.Fatalf("cleanup changed audit rows:\nbefore:\n%s\nafter:\n%s", strings.Join(before, "\n"), strings.Join(after, "\n"))
	}
	var authEdges int
	if err = w.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM resource_access_edges WHERE source_kind=?`, authAuditAccessSourceKind).Scan(&authEdges); err != nil {
		t.Fatal(err)
	}
	if authEdges != 0 {
		t.Fatalf("cleanup left %d audit edges", authEdges)
	}
}
