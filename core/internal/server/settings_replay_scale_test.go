package server

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

// Dev actor recovery has no authenticated owner yet. This measures actual
// receipt/identity point lookups; scoped visibility is covered separately.
func TestSettingsReplayIndexedScale(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db, capture, err := perfguard.Open("file:" + env.workspace.Layout().DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	registry := actors.NewStore(db)
	store := primitives.NewStore(db, nil, env.workspace.Layout().ArtifactContentDir)
	actor := actors.Actor{ID: "new-reader", DisplayName: "Reader", CreatedAt: "2026-10-14T10:00:00Z"}
	if _, err = registry.RegisterWithReplay(ctx, actor, "original"); err != nil {
		t.Fatal(err)
	}
	base := ""
	dashboard := func() error {
		_, err := store.ExecuteWithReplay(ctx, "workspace.dashboard.set", "reader", "original", "hash", 200, func(ctx context.Context) (map[string]any, error) {
			return store.SetWorkspaceDashboardIf(ctx, "reader", "", &base)
		})
		return err
	}
	if err = dashboard(); err != nil {
		t.Fatal(err)
	}
	for _, size := range []int{0, 4096} {
		if size > 0 {
			tx, err := env.workspace.DB().Begin()
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < size; i++ {
				if _, err = tx.Exec(`INSERT INTO idempotency_replays(scope,actor_id,request_key,request_hash,response_status,response_json) VALUES('background',?,?,?,200,'{}')`, fmt.Sprint(i), fmt.Sprint(i), "other"); err != nil {
					t.Fatal(err)
				}
				if _, err = tx.Exec(`INSERT INTO actors(id,display_name,tags_json,created_at,metadata_json) VALUES(?,?,'[]','now','{}')`, fmt.Sprintf("background-%d", i), "Other"); err != nil {
					t.Fatal(err)
				}
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
		}
		for name, run := range map[string]func() error{"actor": func() error { _, err := registry.RegisterWithReplay(ctx, actor, "original"); return err }, "dashboard": dashboard} {
			capture.Start()
			err := run()
			statements, queries, rows := capture.Stop()
			work := capture.Work()
			if err != nil || capture.WorkError() != nil || queries > 3 || rows > 2 || work.VMSteps > 256 || work.FullScanSteps != 0 || work.Sorts != 0 {
				t.Fatalf("%s size=%d err=%v queries=%d rows=%d work=%+v", name, size, err, queries, rows, work)
			}
			for _, stmt := range statements {
				plan, err := perfguard.Explain(ctx, env.workspace.DB(), stmt)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(strings.Join(plan, "\n"), "SCAN ") {
					t.Fatalf("unindexed receipt: %v", plan)
				}
			}
			t.Logf("%s size=%d queries=%d rows=%d work=%+v", name, size, queries, rows, work)
		}
	}
}
