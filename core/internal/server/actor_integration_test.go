package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"organization-autorunner-core/internal/blob"
	"path/filepath"
	"testing"

	"organization-autorunner-core/internal/actors"
	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/schema"
	"organization-autorunner-core/internal/storage"
)

func TestActorEndpointsRegisterAndListStableOrder(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	registry := actors.NewStore(workspace.DB())
	contractPath := filepath.Join("..", "..", "..", "contracts", "oar-schema.yaml")
	contract, err := schema.Load(contractPath)
	if err != nil {
		t.Fatalf("load schema contract: %v", err)
	}
	primitiveStore := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	handler := NewHandler(
		"0.2.2",
		WithActorRegistry(registry),
		WithHealthCheck(workspace.Ping),
		WithPrimitiveStore(primitiveStore),
		WithSchemaContract(contract),
		WithAllowUnauthenticatedWrites(true),
		WithEnableDevActorMode(true),
	)
	server := httptest.NewServer(handler)
	defer server.Close()

	postJSON(t, server.URL+"/actors", `{"actor":{"id":"actor-b","display_name":"Actor B","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	postJSON(t, server.URL+"/actors", `{"actor":{"id":"actor-a","display_name":"Actor A","created_at":"2026-03-04T09:00:00Z","tags":["human"]}}`, http.StatusCreated)

	resp, err := http.Get(server.URL + "/actors")
	if err != nil {
		t.Fatalf("GET /actors: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: got %d", resp.StatusCode)
	}

	var payload struct {
		Actors []actors.Actor `json:"actors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode actors response: %v", err)
	}

	if len(payload.Actors) != 2 {
		t.Fatalf("unexpected actor count: got %d", len(payload.Actors))
	}
	if payload.Actors[0].ID != "actor-a" || payload.Actors[1].ID != "actor-b" {
		t.Fatalf("expected created_at asc ordering, got %#v", payload.Actors)
	}
}

func TestPostThreadsRejectsUnknownActorID(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()

	registry := actors.NewStore(workspace.DB())
	contractPath := filepath.Join("..", "..", "..", "contracts", "oar-schema.yaml")
	contract, err := schema.Load(contractPath)
	if err != nil {
		t.Fatalf("load schema contract: %v", err)
	}
	primitiveStore := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	handler := NewHandler(
		"0.2.2",
		WithActorRegistry(registry),
		WithHealthCheck(workspace.Ping),
		WithPrimitiveStore(primitiveStore),
		WithSchemaContract(contract),
		WithAllowUnauthenticatedWrites(true),
	)
	server := httptest.NewServer(handler)
	defer server.Close()

	resp := postJSON(t, server.URL+"/threads", `{"actor_id":"missing-actor","thread":{"title":"thread"}}`, http.StatusBadRequest)
	defer resp.Body.Close()

	var payload map[string]map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode error payload: %v", err)
	}
	if payload["error"]["code"] != "unknown_actor_id" {
		t.Fatalf("unexpected error code: got %q", payload["error"]["code"])
	}

	assertTableCount(t, workspace.DB(), "threads", 0)
}

func postJSON(t *testing.T, url string, body string, expectedStatus int) *http.Response {
	t.Helper()

	resp, err := http.Post(url, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("POST %s failed: %v", url, err)
	}
	if resp.StatusCode != expectedStatus {
		defer resp.Body.Close()
		t.Fatalf("POST %s unexpected status: got %d want %d", url, resp.StatusCode, expectedStatus)
	}
	return resp
}

func assertTableCount(t *testing.T, db *sql.DB, table string, expected int) {
	t.Helper()

	var count int
	query := "SELECT COUNT(*) FROM " + table
	if err := db.QueryRow(query).Scan(&count); err != nil {
		t.Fatalf("count %s rows: %v", table, err)
	}
	if count != expected {
		t.Fatalf("unexpected %s row count: got %d want %d", table, count, expected)
	}
}
