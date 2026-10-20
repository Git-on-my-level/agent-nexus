package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

// unavailableBlobStore fails every pre-replay preload and records whether a
// replay transaction was opened anyway.
type unavailableBlobStore struct {
	*primitives.Store
	replays int
}

func (s *unavailableBlobStore) PreloadDocumentContentForReplay(ctx context.Context, _ any, _ string) (context.Context, func(), error) {
	return ctx, func() {}, primitives.ErrBlobStorageUnavailable
}

func (s *unavailableBlobStore) ExecuteWithReplay(ctx context.Context, scope, actor, key, hash string, status int, run func(context.Context) (map[string]any, error)) (primitives.IdempotencyReplay, error) {
	s.replays++
	return s.Store.ExecuteWithReplay(ctx, scope, actor, key, hash, status, run)
}

func TestReplayDocumentPreloadFailureIsRetryableWithoutTransaction(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	realStore := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	document, revision, err := realStore.CreateDocument(ctx, "actor-1", map[string]any{"title": "Existing"}, "first", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	contract, err := schema.Load(filepath.Join("..", "..", "..", "contracts", "anx-schema.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	store := &unavailableBlobStore{Store: realStore}
	handler := NewHandler(contract.Version, WithPrimitiveStore(store), WithSchemaContract(contract), WithActorRegistry(actors.NewStore(ws.DB())), WithAllowUnauthenticatedWrites(true), WithEnableDevActorMode(true))
	server := httptest.NewServer(handler)
	defer server.Close()
	postJSONExpectStatus(t, server.URL+"/actors", `{"actor":{"id":"actor-1","display_name":"Writer","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()

	for name, path := range map[string]string{
		"create": "/docs",
		"revise": "/docs/" + anyString(document["id"]) + "/revisions",
	} {
		t.Run(name, func(t *testing.T) {
			before := store.replays
			body := `{"actor_id":"actor-1","request_key":"preload-` + name + `","if_base_revision":"` + anyString(revision["revision_id"]) + `","document":{"title":"Doc"},"content":"new body","content_type":"text"}`
			req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			var payload map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
				t.Fatal(err)
			}
			errBody, _ := payload["error"].(map[string]any)
			if errBody["code"] != "storage_unavailable" || errBody["recoverable"] != true {
				t.Fatalf("failed preload is not a retryable storage error: %v", payload)
			}
			if rec.Header().Get("Retry-After") != "1" {
				t.Fatalf("missing Retry-After: %v", rec.Header())
			}
			if store.replays != before {
				t.Fatal("replay transaction opened after the preload failed")
			}
		})
	}
	if _, err := realStore.GetIdempotencyReplay(ctx, "docs.create", "actor-1", "preload-create"); !errors.Is(err, primitives.ErrNotFound) {
		t.Fatalf("receipt recorded for a request that failed before its transaction: %v", err)
	}
}
