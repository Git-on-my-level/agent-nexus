package actors_test

import (
	"context"
	"errors"
	"testing"

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
