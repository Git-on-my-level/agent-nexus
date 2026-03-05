package actors_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"organization-autorunner-core/internal/actors"
	"organization-autorunner-core/internal/storage"
)

func TestStoreRegisterListAndExists(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := actors.NewStore(workspace.DB())

	first, err := store.Register(context.Background(), actors.Actor{
		ID:          "actor-b",
		DisplayName: "Actor B",
		CreatedAt:   "2026-03-04T10:00:00Z",
	})
	if err != nil {
		t.Fatalf("register first actor: %v", err)
	}
	if len(first.Tags) != 0 {
		t.Fatalf("expected empty tags default, got %#v", first.Tags)
	}

	_, err = store.Register(context.Background(), actors.Actor{
		ID:          "actor-a",
		DisplayName: "Actor A",
		Tags:        []string{"human"},
		CreatedAt:   "2026-03-04T09:00:00Z",
	})
	if err != nil {
		t.Fatalf("register second actor: %v", err)
	}

	_, err = store.Register(context.Background(), actors.Actor{
		ID:          "actor-a",
		DisplayName: "Duplicate",
		CreatedAt:   "2026-03-04T11:00:00Z",
	})
	if !errors.Is(err, actors.ErrAlreadyExists) {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}

	exists, err := store.Exists(context.Background(), "actor-a")
	if err != nil {
		t.Fatalf("exists actor-a: %v", err)
	}
	if !exists {
		t.Fatal("expected actor-a to exist")
	}

	exists, err = store.Exists(context.Background(), "missing-actor")
	if err != nil {
		t.Fatalf("exists missing-actor: %v", err)
	}
	if exists {
		t.Fatal("expected missing-actor not to exist")
	}

	list, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list actors: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("unexpected actor count: got %d", len(list))
	}
	if list[0].ID != "actor-a" || list[1].ID != "actor-b" {
		t.Fatalf("expected stable ordering by created_at asc, got %#v", list)
	}
}

func TestStoreEnsureSystemActorIdempotent(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	store := actors.NewStore(workspace.DB())

	firstTime := time.Date(2026, time.March, 5, 10, 0, 0, 0, time.UTC)
	secondTime := firstTime.Add(2 * time.Hour)

	seededFirst, err := store.EnsureSystemActor(context.Background(), firstTime)
	if err != nil {
		t.Fatalf("ensure system actor first call: %v", err)
	}
	if seededFirst.ID != actors.SystemActorID {
		t.Fatalf("unexpected system actor id: %#v", seededFirst.ID)
	}
	if seededFirst.DisplayName != "OAR Core" {
		t.Fatalf("unexpected system actor display name: %#v", seededFirst.DisplayName)
	}
	if !reflect.DeepEqual(seededFirst.Tags, []string{"system"}) {
		t.Fatalf("unexpected system actor tags: %#v", seededFirst.Tags)
	}

	seededSecond, err := store.EnsureSystemActor(context.Background(), secondTime)
	if err != nil {
		t.Fatalf("ensure system actor second call: %v", err)
	}
	if seededSecond.ID != actors.SystemActorID {
		t.Fatalf("unexpected system actor id on second call: %#v", seededSecond.ID)
	}

	list, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("list actors: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected exactly one actor after idempotent seed, got %d", len(list))
	}

	systemActor := list[0]
	if systemActor.ID != actors.SystemActorID {
		t.Fatalf("unexpected listed system actor id: %#v", systemActor.ID)
	}
	if systemActor.DisplayName != "OAR Core" {
		t.Fatalf("unexpected listed system actor display name: %#v", systemActor.DisplayName)
	}
	if !reflect.DeepEqual(systemActor.Tags, []string{"system"}) {
		t.Fatalf("unexpected listed system actor tags: %#v", systemActor.Tags)
	}
	if systemActor.CreatedAt != firstTime.Format(time.RFC3339Nano) {
		t.Fatalf("expected created_at to remain first-seed value, got %#v", systemActor.CreatedAt)
	}
}
