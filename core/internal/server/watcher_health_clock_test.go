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
	runtime, err := newOnboardedPMRuntime(t, env.workspace.DB(), store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err = env.workspace.DB().ExecContext(ctx, `INSERT INTO pm_registration(workspace_id,actor_id,runner,host) VALUES('ws_main','fixture-pm','runner','host')`); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err = env.workspace.DB().ExecContext(ctx, `UPDATE pm_presence SET last_seen_at=?`, now.Add(-2*time.Hour).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	cfg := primitives.DefaultWatcherHealthConfig()
	if err = WatcherHealthStep(ctx, store, runtime.Service, now, cfg); err != nil {
		t.Fatal(err)
	}
	restored, err := NewPMRuntime(env.workspace.DB(), store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = WatcherHealthStep(ctx, store, restored.Service, now.Add(time.Minute), cfg); err != nil {
		t.Fatal(err)
	}
	var asks int
	if err = env.workspace.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE type='human_attention_requested'`).Scan(&asks); err != nil || asks != 1 {
		t.Fatal(asks, err)
	}
	if _, err = env.workspace.DB().ExecContext(ctx, `UPDATE pm_presence SET last_seen_at=?`, now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err = WatcherHealthStep(ctx, store, restored.Service, now, cfg); err != nil {
		t.Fatal(err)
	}
	var closed int
	if err = env.workspace.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM human_attention_request_resolutions WHERE resolution_type='withdrawn'`).Scan(&closed); err != nil || closed != 1 {
		t.Fatal(closed, err)
	}
}
