package scopedrepo

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"agent-nexus-core/internal/readmodel"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/scopes"
	_ "modernc.org/sqlite"
)

func TestRegisteredProxyRejectsSQLBeforeExecution(t *testing.T) {
	// TempDir includes subtest names in its directory component. Keep SQL out
	// of those names so the rejection matrix is portable to Linux NAME_MAX.
	for _, test := range []struct{ name, query string }{
		{"commit", "COMMIT"}, {"rollback", "ROLLBACK"}, {"begin", "BEGIN"},
		{"savepoint", "SAVEPOINT escape"}, {"pragma", "PRAGMA foreign_keys=OFF"},
		{"ddl", "CREATE TABLE escaped(v)"}, {"delete", "DELETE FROM documents"},
		{"arbitrary-read", "SELECT title FROM documents"},
		{"compound-write", readmodel.InsertFeed + "; COMMIT"},
		{"compound-read", readModelHookIdentity + "; SELECT title FROM documents"},
		{"altered-read", " " + readModelHookIdentity},
		{"altered-write", readmodel.InsertFeed + " -- bypass"},
		{"cte", "WITH x AS (SELECT 1) SELECT * FROM x"},
	} {
		for _, method := range []string{"exec", "query"} {
			t.Run(test.name+"/"+method, func(t *testing.T) {
				query := test.query
				ctx := context.Background()
				db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "hooks.db"))
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				if _, err = db.Exec(`CREATE TABLE documents(title TEXT)`); err != nil {
					t.Fatal(err)
				}
				tx, err := resourceaccess.NewDB(db).BeginTx(ctx, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				if _, err = tx.ExecContext(ctx, `INSERT INTO documents VALUES('source')`); err != nil {
					t.Fatal(err)
				}
				remaining := scopes.MaxComputationOps
				cap := &mutationTx{tx: tx, alive: true, ctx: ctx, remaining: &remaining, registered: true}
				if method == "query" {
					_, err = cap.QueryContext(ctx, query)
				} else {
					_, err = cap.ExecContext(ctx, query)
				}
				if !errors.Is(err, scopes.ErrDenied) {
					t.Fatal(query, err)
				}
				// Ignoring the first rejection cannot make even an approved statement run.
				_, err = cap.ExecContext(ctx, readmodel.InsertFeed)
				if !errors.Is(err, scopes.ErrDenied) {
					t.Fatal("nonsticky SQL rejection", err)
				}
				if !errors.Is(cap.close(), scopes.ErrDenied) {
					t.Fatal("close lost SQL rejection")
				}
				// SQL transaction control was never dispatched: source remains uncommitted.
				if err = tx.Rollback(); err != nil {
					t.Fatal("rejected SQL altered source transaction", err)
				}
				var count int
				if err = db.QueryRow(`SELECT count(*) FROM documents`).Scan(&count); err != nil || count != 0 {
					t.Fatal(count, err)
				}
			})
		}
	}
}
