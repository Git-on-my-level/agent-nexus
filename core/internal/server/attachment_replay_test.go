package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
)

func TestAttachmentAtomicConcurrentReplayAndRollback(t *testing.T) {
	h := newProjectionMaintenanceTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-upload","display_name":"Writer","created_at":"2026-10-10T00:00:00Z"}}`, 201).Body.Close()
	upload := func(content, name string) (int, string) {
		var body strings.Builder
		form := multipart.NewWriter(&body)
		_ = form.WriteField("actor_id", "actor-upload")
		_ = form.WriteField("request_key", "upload-key")
		_ = form.WriteField("refs", `["thread:thread-upload"]`)
		part, _ := form.CreateFormFile("file", name)
		_, _ = part.Write([]byte(content))
		_ = form.Close()
		response, err := http.Post(h.baseURL+"/artifacts/attachments", form.FormDataContentType(), strings.NewReader(body.String()))
		if err != nil {
			return 0, err.Error()
		}
		defer response.Body.Close()
		raw, _ := io.ReadAll(response.Body)
		return response.StatusCode, string(raw)
	}
	if _, err := h.workspace.DB().Exec(`CREATE TRIGGER fail_upload_replay BEFORE INSERT ON idempotency_replays WHEN NEW.scope='artifacts.attachments.create' BEGIN SELECT RAISE(ABORT,'injected replay failure'); END`); err != nil {
		t.Fatal(err)
	}
	if status, raw := upload("Original file bytes", "original.txt"); status != 500 {
		t.Fatalf("failure %d %s", status, raw)
	}
	var count int
	if err := h.workspace.DB().QueryRow(`SELECT count(*) FROM artifacts`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial artifact count=%d %v", count, err)
	}
	if err := h.workspace.DB().QueryRow(`SELECT count(*) FROM blob_usage_ledger`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("partial blob ledger count=%d %v", count, err)
	}
	if _, err := h.workspace.DB().Exec(`DROP TRIGGER fail_upload_replay`); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan string, 8)
	for i := 0; i < 8; i++ {
		go func() {
			<-start
			status, raw := upload("Original file bytes", "original.txt")
			if status != 201 {
				raw = fmt.Sprintf("status %d: %s", status, raw)
			}
			results <- raw
		}()
	}
	close(start)
	first := <-results
	var response map[string]any
	if err := json.Unmarshal([]byte(first), &response); err != nil || response["artifact"] == nil {
		t.Fatalf("initial response %s", first)
	}
	for i := 1; i < 8; i++ {
		if next := <-results; next != first {
			t.Fatalf("different replay %s vs %s", first, next)
		}
	}
	for _, change := range []struct{ content, name string }{{"Changed bytes", "original.txt"}, {"Original file bytes", "changed.txt"}} {
		if status, raw := upload(change.content, change.name); status != 409 {
			t.Fatalf("changed key %d %s", status, raw)
		}
	}
	if err := h.workspace.DB().QueryRow(`SELECT count(*) FROM artifacts`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("artifact count=%d %v", count, err)
	}
}

func TestAttachmentReplayRespectsRevokedOwnership(t *testing.T) {
	h := newProjectionMaintenanceTestServer(t)
	ctx := context.Background()
	store := h.store.(*primitives.Store)
	board, err := store.CreateBoard(ctx, "owner", map[string]any{"title": "Private upload"})
	if err != nil {
		t.Fatal(err)
	}
	thread := board["thread_id"].(string)
	if _, err = store.PatchThread(ctx, "owner", thread, map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	owner := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "owner"})
	metadata := map[string]any{"refs": []string{fmt.Sprint(board["ref"])}, "kind": "attachment"}
	if _, err = store.CreateArtifactAttachmentWithReplay(owner, "owner", metadata, "text/plain", "private.txt", strings.NewReader("Private content"), 1024, "private-upload"); err != nil {
		t.Fatal(err)
	}
	stranger := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "stranger"})
	if _, err = store.GetIdempotencyReplay(stranger, "artifacts.attachments.create", "owner", "private-upload"); !errors.Is(err, primitives.ErrNotFound) {
		t.Fatalf("stranger replay %v", err)
	}
	if _, err = store.PatchThread(ctx, "owner", thread, map[string]any{"pm_actor_id": "new-owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if receipt, err := store.CreateArtifactAttachmentWithReplay(owner, "owner", metadata, "text/plain", "private.txt", strings.NewReader("Private content"), 1024, "private-upload"); err == nil || receipt != nil {
		t.Fatalf("revoked replay %v %v", receipt, err)
	}
}
