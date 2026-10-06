package readmodel

import (
	"context"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/storage"
)

func TestProposedDispatcherBudgetIncludesLegacyDenialPreparation(t *testing.T) {
	if testing.Short() {
		t.Skip("real canonical workspace + disabled dispatcher budget probe")
	}
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	store := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	for i := 0; i < 8; i++ {
		board, err := store.CreateBoard(ctx, "owner", map[string]any{"title": "private"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.PatchThread(ctx, "owner", board["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
			t.Fatal(err)
		}
		if _, err = store.CreateBoardCard(ctx, "owner", board["id"].(string), primitives.AddBoardCardInput{Title: "private sentinel", ColumnKey: "ready"}); err != nil {
			t.Fatal(err)
		}
	}
	db, c := countedDSN(t, ws.Layout().DatabasePath)
	_, repo, request, streams := adapterFixtureOnDB(t, db, 64, 4)
	// The old authorized relation loader triggers the real request's denial graph
	// preparation. This probe grants no parity receipt and installs no HTTP reader.
	scoped := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"})
	c.start()
	start := time.Now()
	rows, err := resourceaccess.NewDB(db).QueryContext(scoped, `SELECT id FROM cards ORDER BY id LIMIT 1`)
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		t.Fatal("private canonical sentinel leaked")
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	err = repo.ReadFeed(scoped, request, streams, func(r scopedrepo.FeedReader) error {
		a := feedAdapter{reader: r}
		if _, err := Read(scoped, a, codec(t), 100, ""); err != nil {
			return err
		}
		_, err := Count(scoped, a, []string{"total"})
		return err
	})
	elapsed := time.Since(start)
	statements, returned := c.stop()
	if err != nil {
		t.Fatal(err)
	}
	if statements <= 643 || returned < 1509 {
		t.Fatal("legacy preparation excluded", statements, returned)
	}
	t.Logf("complete disabled dispatcher probe incl. legacy denial preparation: SQL=%d returned_rows=%d elapsed=%s; serving gate FAIL", statements, returned, elapsed)
}
