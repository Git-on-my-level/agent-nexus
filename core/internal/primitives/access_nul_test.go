package primitives

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"agent-nexus-core/internal/storage"
)

func TestResourceAccessNULBinaryManifestAuthorization(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"id": "[]", "title": "private"}, "source", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", doc["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	content := []byte("\x89PNG\r\n\x1a\n\x00document:[]")
	owner := WithAccessScope(ctx, AccessScope{ActorID: "owner"})
	stranger := WithAccessScope(ctx, AccessScope{ActorID: "stranger"})
	metadata := map[string]any{"kind": "attachment", "refs": []string{}}
	if _, err = s.CreateArtifactAttachment(stranger, "stranger", metadata, "image/png", "secret.png", bytes.NewReader(content), 1024); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private binary write=%v", err)
	}
	artifact, err := s.CreateArtifactAttachment(owner, "owner", metadata, "image/png", "secret.png", bytes.NewReader(content), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.GetArtifactContent(stranger, artifact["id"].(string)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private binary read=%v", err)
	}
	got, _, err := s.GetArtifactContent(owner, artifact["id"].(string))
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("owner binary round trip=%q %v", got, err)
	}
}
