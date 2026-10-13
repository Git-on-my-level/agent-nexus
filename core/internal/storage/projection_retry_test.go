package storage

import (
	"context"
	"testing"
)

func TestProjectionRetryMigrationPreservesGenerations(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	ws, err := InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ws.Close() }()
	for _, query := range []string{
		`INSERT INTO topic_projection_refresh_status(thread_id,desired_generation,materialized_generation,in_progress_generation,queued_at,last_error,updated_at) VALUES('stranded',5,3,5,'2026-10-01T00:00:00Z','legacy failure','2026-10-01T00:00:00Z')`,
		`ALTER TABLE topic_projection_refresh_status DROP COLUMN retry_after`,
		`ALTER TABLE topic_projection_refresh_status DROP COLUMN failure_count`,
		`DELETE FROM schema_migrations WHERE version=81`,
	} {
		if _, err := ws.DB().ExecContext(ctx, query); err != nil {
			t.Fatal(err)
		}
	}
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err = InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	var desired, materialized, inProgress, retry, failures int64
	var queued, message string
	if err := ws.DB().QueryRowContext(ctx, `SELECT desired_generation,materialized_generation,in_progress_generation,queued_at,last_error,retry_after,failure_count FROM topic_projection_refresh_status WHERE thread_id='stranded'`).Scan(&desired, &materialized, &inProgress, &queued, &message, &retry, &failures); err != nil {
		t.Fatal(err)
	}
	if desired != 5 || materialized != 3 || inProgress != 5 || queued != "2026-10-01T00:00:00Z" || message != "legacy failure" || retry != 0 || failures != 0 {
		t.Fatalf("upgrade changed recoverable work: %d %d %d %s %s %d %d", desired, materialized, inProgress, queued, message, retry, failures)
	}
	if _, err := ws.DB().ExecContext(ctx, `UPDATE topic_projection_refresh_status SET retry_after=123456789,failure_count=3 WHERE thread_id='stranded'`); err != nil {
		t.Fatal(err)
	}
	if err := ws.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err = InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.DB().QueryRowContext(ctx, `SELECT retry_after,failure_count FROM topic_projection_refresh_status WHERE thread_id='stranded'`).Scan(&retry, &failures); err != nil {
		t.Fatal(err)
	}
	if retry != 123456789 || failures != 3 {
		t.Fatalf("restart lost backoff: %d %d", retry, failures)
	}
}
