package primitives_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
)

func TestScopeInboxInvalidationActualPMInitialization(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for _, path := range []string{"../scopedrepo/schema.sql", "../scopedrepo/feed_schema.sql"} {
		ddl, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = ws.DB().Exec(string(ddl)); err != nil {
			t.Fatal(err)
		}
	}
	install := func(wantComplete bool) {
		t.Helper()
		tx, err := ws.DB().BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		result, err := primitives.InstallScopeInboxInvalidation(ctx, tx)
		if err != nil || result.Complete != wantComplete {
			t.Fatal(result, err)
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	install(false)
	if _, err = pm.NewStore(ws.DB()); err != nil {
		t.Fatal(err)
	}
	// The current production PM initializer deliberately remains unchanged. A
	// future shared-file hook must reconcile inside that initializer's transaction.
	if _, err = primitives.ReadScopeInboxSourceSnapshot(ctx, ws.DB()); !errors.Is(err, primitives.ErrScopeInboxInvalidationIncomplete) {
		t.Fatal("unhooked real PM initialization admitted", err)
	}
	install(true)
	before, err := primitives.ReadScopeInboxSourceSnapshot(ctx, ws.DB())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ws.DB().Exec(`INSERT INTO pm_records(kind,id,workspace_id,actor_id,parent_id,revision,body) VALUES('decision','actual-pm','ws','owner','',1,CAST('{"status":"awaiting_answer"}' AS BLOB))`); err != nil {
		t.Fatal(err)
	}
	after, err := primitives.ReadScopeInboxSourceSnapshot(ctx, ws.DB())
	if err != nil || after.SourceRevision <= before.SourceRevision || after.AuthorityRevision <= before.AuthorityRevision || after.DirectoryRevision <= before.DirectoryRevision || after.DirectoryCovered() {
		t.Fatal("actual PM source mutation did not fence snapshot", before, after, err)
	}
}
