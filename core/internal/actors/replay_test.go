package actors_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/storage"
)

func TestActorCreationAtomicOriginalReplay(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	ws.DB().SetMaxOpenConns(1)
	store := actors.NewStore(ws.DB())
	actor := actors.Actor{ID: "new-reader", DisplayName: "Reader", Tags: []string{"human"}, CreatedAt: "2026-10-14T10:00:00Z"}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := store.RegisterWithReplay(ctx, actor, "same-key")
			if err != nil || !reflect.DeepEqual(got, actor) {
				t.Errorf("retry: %v %v", got, err)
			}
		}()
	}
	wg.Wait()
	changed := actor
	changed.DisplayName = "Changed"
	if _, err = store.RegisterWithReplay(ctx, changed, "same-key"); !errors.Is(err, actors.ErrAlreadyExists) {
		t.Fatalf("changed key accepted: %v", err)
	}
	if _, err = store.RegisterWithReplay(ctx, actor, "new-key"); !errors.Is(err, actors.ErrAlreadyExists) {
		t.Fatalf("legacy identity adopted: %v", err)
	}
	if _, err = ws.DB().Exec(`UPDATE actors SET display_name='Later' WHERE id='new-reader'`); err != nil {
		t.Fatal(err)
	}
	got, err := store.RegisterWithReplay(ctx, actor, "same-key")
	if err != nil || !reflect.DeepEqual(got, actor) {
		t.Fatalf("original receipt replaced: %v %v", got, err)
	}
}

func TestActorReceiptFailureRollsBackRegistration(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	store := actors.NewStore(ws.DB())
	if _, err = ws.DB().Exec(`CREATE TRIGGER fail_actor_receipt BEFORE INSERT ON idempotency_replays WHEN NEW.scope='actors.register' BEGIN SELECT RAISE(ABORT,'injected'); END`); err != nil {
		t.Fatal(err)
	}
	_, err = store.RegisterWithReplay(ctx, actors.Actor{ID: "failed-reader", DisplayName: "Reader", CreatedAt: "2026-10-14T10:00:00Z"}, "key")
	if err == nil {
		t.Fatal("receipt failure ignored")
	}
	exists, err := store.Exists(ctx, "failed-reader")
	if err != nil || exists {
		t.Fatalf("registration survived rollback: %v %v", exists, err)
	}
}
