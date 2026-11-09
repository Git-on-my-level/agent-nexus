package server

import (
	"context"
	"testing"
	"time"

	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
)

func TestWatcherClockAlarmsWithoutRunnerAndSurvivesRuntimeReopen(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	store := env.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	// Install the schema, then seed a registered PM with fixed presence. Do not
	// use the onboarding helper's wall clock or implicit configured actor.
	if _, err := pm.NewStore(env.workspace.DB()); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)
	last := now.Add(-2 * time.Hour).Format(time.RFC3339Nano)
	if _, err := env.workspace.DB().ExecContext(ctx, `INSERT INTO pm_registration(workspace_id,actor_id,runner,host) VALUES('ws_main','fixture-pm','runner','host')`); err != nil {
		t.Fatal(err)
	}
	if _, err := env.workspace.DB().ExecContext(ctx, `INSERT INTO pm_presence(workspace_id,actor_id,first_seen_at,last_seen_at,signal) VALUES('ws_main','fixture-pm',?,?,'connect')`, last, last); err != nil {
		t.Fatal(err)
	}
	cfg := PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}}
	runtime, err := NewPMRuntime(env.workspace.DB(), store, env.authStore, cfg)
	if err != nil {
		t.Fatal(err)
	}
	health := primitives.DefaultWatcherHealthConfig()
	if unhealthy, err := runtime.Service.WatcherUnhealthy(ctx, now, health.PMOfflineAfter); err != nil || !unhealthy {
		t.Fatalf("fixture must contain an offline PM: unhealthy=%v error=%v", unhealthy, err)
	}
	assertCount := func(query string, want int) {
		t.Helper()
		var got int
		if err := env.workspace.DB().QueryRowContext(ctx, query).Scan(&got); err != nil || got != want {
			t.Fatalf("%s: got=%d want=%d error=%v", query, got, want, err)
		}
	}
	const asks = `SELECT COUNT(*) FROM events WHERE type='human_attention_requested'`
	// The empty auth fixture has nobody to notify. This is a recipient
	// prerequisite, not a delayed/asynchronous alarm; no runner is started.
	assertCount(`SELECT COUNT(*) FROM agents WHERE json_extract(metadata_json,'$.principal_kind')='human' AND revoked_at IS NULL`, 0)
	if err = WatcherHealthStep(ctx, store, runtime.Service, now, health); err != nil {
		t.Fatal(err)
	}
	assertCount(asks, 0)
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "watcher-human", "watcher-human-actor", "watcher-human", "watcher-human-token")
	if err = WatcherHealthStep(ctx, store, runtime.Service, now, health); err != nil {
		t.Fatal(err)
	}
	assertCount(asks, 1)
	page, err := store.AttentionItems(primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: human.ActorID}), human.ActorID, "human", "decide", "", "builtin:initiatives", "", now)
	if err != nil || len(page.Items) != 1 || page.Items[0].Title != "Restore the workspace PM?" {
		t.Fatalf("offline PM must reach the human's Decide view: page=%+v error=%v", page, err)
	}
	restored, err := NewPMRuntime(env.workspace.DB(), store, env.authStore, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = WatcherHealthStep(ctx, store, restored.Service, now.Add(time.Minute), health); err != nil {
		t.Fatal(err)
	}
	assertCount(asks, 1)
	recovered := now.Add(2 * time.Minute)
	if _, err = env.workspace.DB().ExecContext(ctx, `UPDATE pm_presence SET last_seen_at=? WHERE workspace_id='ws_main' AND actor_id='fixture-pm'`, recovered.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	for _, at := range []time.Time{recovered, recovered.Add(time.Minute)} {
		if err = WatcherHealthStep(ctx, store, restored.Service, at, health); err != nil {
			t.Fatal(err)
		}
	}
	assertCount(`SELECT COUNT(*) FROM human_attention_request_resolutions WHERE resolution_type='withdrawn'`, 1)
}
