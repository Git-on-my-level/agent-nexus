package storage

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
)

func TestCommentSearchMigration82FromReleaseAnd81(t *testing.T) {
	for _, version := range []int{80, 81} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			ctx := context.Background()
			layout := NewLayout(t.TempDir())
			if err := ensureLayout(layout); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", sqliteDSN(layout.DatabasePath))
			if err != nil {
				t.Fatal(err)
			}
			commentSearchExec(t, db, createMigrationsTableSQL)
			last := 0
			for _, m := range migrations {
				if m.Version <= last {
					t.Fatalf("migration ordering: %d after %d", m.Version, last)
				}
				last = m.Version
				if m.Version <= version {
					if err = applyMigration(ctx, db, m); err != nil {
						t.Fatal(err)
					}
				}
			}
			commentSearchEvent(t, db, "legacy", "historical needle")
			db.Close()
			ws, err := InitializeWorkspace(ctx, layout.RootDir)
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			if n := commentSearchCount(t, ws.DB(), `SELECT count(*) FROM schema_migrations WHERE version IN(81,82)`); n != 2 {
				t.Fatal("collision or missing migration", n)
			}
			if n := commentSearchCount(t, ws.DB(), `SELECT count(*) FROM pragma_table_info('topic_projection_refresh_status') WHERE name IN('retry_after','failure_count')`); n != 2 {
				t.Fatal("lost projection retry schema", n)
			}
			if n := commentSearchCount(t, ws.DB(), `SELECT count(*) FROM comment_search_heads`); n != 0 {
				t.Fatal("startup copied history", n)
			}
			for i := 0; ; i++ {
				if i > 4 {
					t.Fatal("backfill did not converge")
				}
				done, e := ws.MaintainCommentSearchBatch(ctx)
				if e != nil {
					t.Fatal(e)
				}
				if done {
					break
				}
			}
			if n := commentSearchCount(t, ws.DB(), `SELECT count(*) FROM comment_search_postings WHERE term='needle'`); n != 1 {
				t.Fatal("upgrade lost source", n)
			}
		})
	}
}
