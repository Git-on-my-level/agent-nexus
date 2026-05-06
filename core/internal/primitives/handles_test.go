package primitives

import (
	"context"
	"testing"
	"time"

	"agent-nexus-core/internal/storage"
)

func TestResolveResourceRef(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer ws.Close()
	store := NewTestStore(ws.DB(), "")

	uuidID := "550e8400-e29b-41d4-a716-446655440000"
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := ws.DB().ExecContext(ctx,
		`INSERT INTO topics(id, handle, title, thread_id, summary, extensions_json, provenance_json, created_at, created_by, updated_at, updated_by)
		 VALUES (?, ?, ?, ?, '', '{}', '{}', ?, 'actor-1', ?, 'actor-1')`,
		uuidID, "caf-roadmap", "Café Roadmap", "thread-1", now, now,
	); err != nil {
		t.Fatalf("insert topic: %v", err)
	}
	if _, err := ws.DB().ExecContext(ctx,
		`INSERT INTO topics(id, handle, title, thread_id, summary, extensions_json, provenance_json, created_at, created_by, updated_at, updated_by)
		 VALUES ('legacy-id', 'legacy-source', 'Legacy Source', 'thread-legacy', '', '{}', '{}', ?, 'actor-1', ?, 'actor-1'),
		        ('handle-target', 'legacy-id', 'Handle Target', 'thread-target', '', '{}', '{}', ?, 'actor-1', ?, 'actor-1')`,
		now, now, now, now,
	); err != nil {
		t.Fatalf("insert legacy collision topics: %v", err)
	}
	if _, err := ws.DB().ExecContext(ctx,
		`INSERT INTO resource_handle_aliases(id, resource_type, alias_handle, resource_id, canonical_handle, created_at, created_by, reason)
		 VALUES ('alias-1', 'topic', 'old-roadmap', ?, 'caf-roadmap', ?, 'actor-1', 'rename')`,
		uuidID, now,
	); err != nil {
		t.Fatalf("insert alias: %v", err)
	}

	tests := []struct {
		name      string
		in        ResourceRefInput
		wantAlias bool
	}{
		{"typed handle", ResourceRefInput{Ref: "topic:caf-roadmap"}, false},
		{"endpoint implied handle", ResourceRefInput{Type: "topic", Ref: "caf-roadmap"}, false},
		{"internal uuid", ResourceRefInput{Type: "topic", Ref: uuidID}, false},
		{"typed internal uuid", ResourceRefInput{Ref: "topic:" + uuidID}, false},
		{"alias", ResourceRefInput{Ref: "topic:old-roadmap"}, true},
		{"non ascii normalized", ResourceRefInput{Type: "topic", Ref: "Café Roadmap"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := store.ResolveResourceRef(ctx, tt.in)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if got.ID != uuidID || got.Type != "topic" || got.Handle != "caf-roadmap" || got.CanonicalRef != "topic:caf-roadmap" {
				t.Fatalf("unexpected resolution: %#v", got)
			}
			if got.FromAlias != tt.wantAlias {
				t.Fatalf("FromAlias = %v, want %v", got.FromAlias, tt.wantAlias)
			}
		})
	}
}

func TestResolveResourceRefRejectsMismatchedTypedRefPrefix(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer ws.Close()
	store := NewTestStore(ws.DB(), "")

	if _, err := store.ResolveResourceRef(ctx, ResourceRefInput{Type: "card", Ref: "board:roadmap"}); err == nil {
		t.Fatal("expected mismatched typed ref prefix to fail")
	}
}

func TestResolveResourceRefPrefersHandleBeforeLegacyIDFallback(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer ws.Close()
	store := NewTestStore(ws.DB(), "")
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := ws.DB().ExecContext(ctx,
		`INSERT INTO topics(id, handle, title, thread_id, summary, extensions_json, provenance_json, created_at, created_by, updated_at, updated_by)
		 VALUES ('legacy-id', 'legacy-source', 'Legacy Source', 'thread-legacy', '', '{}', '{}', ?, 'actor-1', ?, 'actor-1'),
		        ('handle-target', 'legacy-id', 'Handle Target', 'thread-target', '', '{}', '{}', ?, 'actor-1', ?, 'actor-1')`,
		now, now, now, now,
	); err != nil {
		t.Fatalf("insert topics: %v", err)
	}

	got, err := store.ResolveResourceRef(ctx, ResourceRefInput{Type: "topic", Ref: "legacy-id"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.ID != "handle-target" || got.Handle != "legacy-id" || got.CanonicalRef != "topic:legacy-id" {
		t.Fatalf("expected public handle target, got %#v", got)
	}
}
