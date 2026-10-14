package primitives_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"agent-nexus-core/internal/primitives"
)

func TestDashboardFrozenBaseReplayAndABA(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ws.DB().SetMaxOpenConns(1)
	store := primitives.NewStore(ws.DB(), nil, ws.Layout().ArtifactContentDir)
	base := ""
	send := func(key, hash string) (primitives.IdempotencyReplay, error) {
		return store.ExecuteWithReplay(ctx, "workspace.dashboard.set", "reader", key, hash, 200, func(ctx context.Context) (map[string]any, error) {
			return store.SetWorkspaceDashboardIf(ctx, "reader", "", &base)
		})
	}
	first, err := send("original", "same-body")
	if err != nil {
		t.Fatal(err)
	}
	laterBase := first.Response["updated_at"].(string)
	later, err := store.SetWorkspaceDashboardIf(ctx, "other", "", &laterBase)
	if err != nil {
		t.Fatal(err)
	}
	if later["updated_at"] == laterBase {
		t.Fatal("setting token did not advance")
	}
	replay, err := send("original", "same-body")
	if err != nil || !reflect.DeepEqual(first, replay) {
		t.Fatalf("original receipt changed: %v %v", replay, err)
	}
	if _, err = send("original", "changed-body"); !errors.Is(err, primitives.ErrConflict) {
		t.Fatalf("changed key: %v", err)
	}
	if _, err = send("offline", "same-body"); !errors.Is(err, primitives.ErrConflict) {
		t.Fatalf("clear/clear ABA was accepted: %v", err)
	}
	got, err := store.DashboardReports(ctx)
	if err != nil || got["pin_updated_at"] != later["updated_at"] {
		t.Fatalf("replay reapplied: %v %v", got, err)
	}
}

func TestDashboardReceiptFailureRollsBackSetting(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	store := primitives.NewStore(ws.DB(), nil, ws.Layout().ArtifactContentDir)
	_, err = ws.DB().Exec(`CREATE TRIGGER fail_dashboard_receipt BEFORE INSERT ON idempotency_replays WHEN NEW.scope='workspace.dashboard.set' BEGIN SELECT RAISE(ABORT,'injected'); END`)
	if err != nil {
		t.Fatal(err)
	}
	base := ""
	_, err = store.ExecuteWithReplay(ctx, "workspace.dashboard.set", "reader", "failed", "hash", 200, func(ctx context.Context) (map[string]any, error) {
		return store.SetWorkspaceDashboardIf(ctx, "reader", "", &base)
	})
	if err == nil {
		t.Fatal("receipt failure ignored")
	}
	got, err := store.DashboardReports(ctx)
	if err != nil || got["pin_updated_at"] != nil {
		t.Fatalf("setting survived rollback: %v %v", got, err)
	}
}
