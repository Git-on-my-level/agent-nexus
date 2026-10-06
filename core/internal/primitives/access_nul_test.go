package primitives

import (
	"bytes"
	"context"
	"encoding/base64"
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
	// Binary document creation, revision and FTS rebuild must preserve bytes
	// without treating them as text; the blob manifest still controls access.
	if _, _, err = s.CreateDocument(stranger, "stranger", map[string]any{"title": "forbidden binary"}, content, "binary", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("private binary document write=%v", err)
	}
	binaryDoc, revision, err := s.CreateDocument(owner, "owner", map[string]any{"title": "binary copy"}, content, "binary", nil)
	if err != nil {
		t.Fatal(err)
	}
	id := binaryDoc["id"].(string)
	for _, principal := range []string{"stranger", "unauthorized-agent"} {
		if _, _, err = s.GetDocument(WithAccessScope(ctx, AccessScope{ActorID: principal}), id); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s private binary document read=%v", principal, err)
		}
	}
	if revision["content_base64"] != base64.StdEncoding.EncodeToString(content) {
		t.Fatalf("binary document content changed: %#v", revision)
	}
	_, revision, err = s.UpdateDocument(owner, "owner", id, nil, revision["revision_id"].(string), content, "binary", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if revision["content_base64"] != base64.StdEncoding.EncodeToString(content) {
		t.Fatalf("binary revision content changed: %#v", revision)
	}
	if _, _, err = s.PatchDocument(owner, "owner", id, map[string]any{"title": "binary rebuilt"}, nil); err != nil {
		t.Fatal(err)
	}
	var title, body string
	if err = ws.DB().QueryRow(`SELECT title, body FROM document_fts WHERE document_id=?`, id).Scan(&title, &body); err != nil || title != "binary rebuilt" || body != "" {
		t.Fatalf("binary FTS title=%q body=%q err=%v", title, body, err)
	}
}
