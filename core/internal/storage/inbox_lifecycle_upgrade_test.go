package storage_test

import (
	"agent-nexus-core/internal/storage"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestInboxLifecycleUpgradeDoesNotBackfillAtStartup(t *testing.T) {
	if testing.Short() {
		t.Skip("real SQLite upgrade benchmark")
	}
	sizes := []int{1000, 10000, 100000}
	if value := os.Getenv("ANX_INBOX_UPGRADE_ROWS"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil {
			t.Fatal(err)
		}
		sizes = []int{n}
	}
	for _, size := range sizes {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			w, err := storage.InitializeWorkspace(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			// Remove only 70's objects/columns to model main's pre-inbox schema.
			rows, err := w.DB().Query(`SELECT name FROM sqlite_master WHERE type='trigger' AND name LIKE 'inbox_lifecycle_%'`)
			if err != nil {
				t.Fatal(err)
			}
			names := []string{}
			for rows.Next() {
				var name string
				if err = rows.Scan(&name); err != nil {
					rows.Close()
					t.Fatal(err)
				}
				names = append(names, name)
			}
			if err = rows.Err(); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			rows.Close()
			for _, name := range names {
				if _, err = w.DB().Exec(`DROP TRIGGER ` + name); err != nil {
					t.Fatal(err)
				}
			}
			for _, q := range []string{`DROP VIEW inbox_lifecycle_subjects`, `DROP TABLE inbox_lifecycle_refs`, `DROP TABLE inbox_hidden_subject_refs`, `DROP TABLE inbox_lifecycle_dirty`, `DROP TABLE inbox_lifecycle_job`, `ALTER TABLE derived_inbox_items DROP COLUMN lifecycle_ready`, `ALTER TABLE derived_inbox_items DROP COLUMN lifecycle_hidden`, `DELETE FROM schema_migrations WHERE version=70`} {
				if _, err = w.DB().Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = w.DB().Exec(`WITH RECURSIVE n(v) AS (VALUES(1) UNION ALL SELECT v+1 FROM n WHERE v<?) INSERT INTO derived_inbox_items(id,thread_id,category,trigger_at,generated_at,data_json) SELECT printf('legacy-%09d',v),'missing','agent_wake','now','now','{}' FROM n`, size); err != nil {
				t.Fatal(err)
			}
			if err = w.Close(); err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			w, err = storage.InitializeWorkspace(ctx, root)
			elapsed := time.Since(start)
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()
			var ready int
			if err = w.DB().QueryRow(`SELECT COUNT(*) FROM derived_inbox_items WHERE lifecycle_ready=1`).Scan(&ready); err != nil {
				t.Fatal(err)
			}
			if ready != 0 {
				t.Fatalf("startup touched %d existing rows", ready)
			}
			t.Logf("rows=%d startup=%s initial_ready=%d", size, elapsed, ready)
			if elapsed > 500*time.Millisecond {
				t.Fatalf("metadata-only startup took %s", elapsed)
			}
			// Reopening must keep an incomplete cursor; each batch admits at most200.
			var epochBefore int64
			if err = w.DB().QueryRow(`SELECT version FROM resource_access_epoch WHERE singleton=1`).Scan(&epochBefore); err != nil {
				t.Fatal(err)
			}
			for pass := 0; pass < 10; pass++ {
				if _, err = w.MaintainInboxLifecycleBatch(ctx, 200); err != nil {
					t.Fatal(err)
				}
			}
			var cursor string
			if err = w.DB().QueryRow(`SELECT cursor FROM inbox_lifecycle_job WHERE singleton=1`).Scan(&cursor); err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(cursor, "legacy-") {
				t.Fatalf("backfill cursor did not advance: %q", cursor)
			}
			var epochAfter int64
			if err = w.DB().QueryRow(`SELECT version FROM resource_access_epoch WHERE singleton=1`).Scan(&epochAfter); err != nil {
				t.Fatal(err)
			}
			if epochBefore != epochAfter {
				t.Fatalf("projection maintenance invalidated authorization: %d -> %d", epochBefore, epochAfter)
			}
			if err = w.Close(); err != nil {
				t.Fatal(err)
			}
			w, err = storage.InitializeWorkspace(ctx, root)
			if err != nil {
				t.Fatal(err)
			}
			defer w.Close()
			var resumed string
			if err = w.DB().QueryRow(`SELECT cursor FROM inbox_lifecycle_job WHERE singleton=1`).Scan(&resumed); err != nil {
				t.Fatal(err)
			}
			if resumed != cursor {
				t.Fatalf("restart lost cursor: %q -> %q", cursor, resumed)
			}
		})
	}
}
