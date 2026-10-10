package storage_test

import (
	"agent-nexus-core/internal/primitives"
	"context"
	"testing"
	"time"
)

func TestIdentityRefreshIgnoresTimestampWrites(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	store := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := store.CreateBoard(ctx, "owner", map[string]any{"title": "Identity"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateWork(ctx, "owner", board["id"].(string), map[string]any{"title": "Target"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		table string
		row   map[string]any
	}{{"boards", board}, {"cards", card}} {
		id := tc.row["id"].(string)
		identities := func() string {
			var result string
			if err := ws.DB().QueryRow(`SELECT group_concat(identity_id) FROM resource_access_identities WHERE origin=? AND origin_id=?`, tc.table, id).Scan(&result); err != nil {
				t.Fatal(err)
			}
			return result
		}
		before := identities()
		if _, err := ws.DB().Exec(`UPDATE `+tc.table+` SET updated_at='later',handle=handle WHERE id=?`, id); err != nil {
			t.Fatal(err)
		}
		if after := identities(); after != before {
			t.Fatalf("%s timestamp rebuilt identity rows %s -> %s", tc.table, before, after)
		}
		newHandle := "renamed-" + tc.table
		mention, _, err := store.CreateDocument(ctx, "owner", map[string]any{"title": "Future reference"}, "See "+tc.table[:len(tc.table)-1]+":"+newHandle, "text", nil)
		if err != nil {
			t.Fatal(err)
		}

		if _, err := ws.DB().Exec(`UPDATE `+tc.table+` SET handle=? WHERE id=?`, newHandle, id); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := ws.DB().QueryRow(`SELECT count(*) FROM resource_access_mentions m JOIN resource_access_identities i ON i.identity_id=m.identity_id WHERE i.origin=? AND i.origin_id=? AND i.ref=? AND m.source_id=?`, tc.table, id, newHandle, mention["id"]).Scan(&count); err != nil || count != 1 {
			t.Fatalf("rename did not resolve existing prose: count=%d err=%v", count, err)
		}
	}
}

func TestLegacyMaintenanceYieldsToWriter(t *testing.T) {
	ws, err := initializeTestWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	writer, err := ws.DB().BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback()
	for _, run := range []func(context.Context) (bool, error){ws.MaintainAskSubjectsBatch, func(ctx context.Context) (bool, error) { return ws.MaintainInboxLifecycleBatch(ctx, 64) }} {
		start := time.Now()
		_, err := run(context.Background())
		if err == nil || time.Since(start) > time.Second {
			t.Fatalf("maintenance waited on serving writer: %s %v", time.Since(start), err)
		}
	}
	if err := writer.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.MaintainAskSubjectsBatch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.MaintainInboxLifecycleBatch(context.Background(), 64); err != nil {
		t.Fatal(err)
	}
}
