package server

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"

	"github.com/google/uuid"
)

func TestServerFaultLogsInternalErrorCauseWithoutClientLeak(t *testing.T) {
	// Serial: this file mutates process-wide logging.
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })

	ctx := context.Background()
	workspace, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	if err := workspace.Close(); err != nil {
		t.Fatalf("close workspace: %v", err)
	}

	body := `{"title":"secret-value","summary":"secret-value"}`
	req := httptest.NewRequest(http.MethodPost, "/boards/secret-value/cards?token=secret-value", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer secret-value")
	req.Header.Set("X-Request-ID", "secret-value")
	cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: "actor-1", PrincipalKind: "agent"})
	rec := httptest.NewRecorder()
	addBoardCardFromRaw(rec, req, handlerOptions{
		primitiveStore:             store,
		allowUnauthenticatedWrites: true,
	}, "secret-value", map[string]any{
		"title":   "secret-value",
		"summary": "secret-value",
	}, "boards.cards.add")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	response := rec.Body.String()
	if !strings.Contains(response, `"code":"internal_error"`) || !strings.Contains(response, "failed to add board card") {
		t.Fatalf("generic client error missing: %s", response)
	}
	if strings.Contains(response, "closed") || strings.Contains(response, "secret-value") || strings.Contains(response, "Bearer") {
		t.Fatalf("client response leaked cause or secret: %s", response)
	}
	requestID := rec.Header().Get("X-Request-ID")
	if _, err := uuid.Parse(requestID); err != nil || requestID == "secret-value" {
		t.Fatalf("request id %q", requestID)
	}
	logged := output.String()
	if strings.Count(logged, "anx-core: server error") != 1 {
		t.Fatalf("expected one server error line, got %q", logged)
	}
	for _, want := range []string{
		"status=500",
		"method=POST",
		"route=/boards/{id}/cards",
		"request_id=" + requestID,
		"actor_kind=agent",
		"message=failed to add board card",
		"closed",
	} {
		if !strings.Contains(logged, want) {
			t.Fatalf("log missing %q: %s", want, logged)
		}
	}
	if strings.Contains(logged, "secret-value") || strings.Contains(logged, "Bearer") || strings.Contains(logged, "omi-lanes") {
		t.Fatalf("log leaked request material: %s", logged)
	}
}

func TestBoardCardCreateClassifiesClientErrors(t *testing.T) {
	ctx := context.Background()
	workspace, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	thread, err := store.CreateThread(ctx, "actor-1", map[string]any{
		"title":      "Board thread",
		"type":       "incident",
		"provenance": map[string]any{"sources": []string{"inferred"}},
	})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	threadID, _ := thread.Thread["id"].(string)
	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{
		"title":     "Lanes",
		"thread_id": threadID,
	})
	if err != nil {
		t.Fatalf("create board: %v", err)
	}
	boardID, _ := board["id"].(string)
	opts := handlerOptions{primitiveStore: store}

	post := func(raw map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/boards/"+boardID+"/cards", strings.NewReader("secret-value"))
		req.Header.Set("Authorization", "Bearer secret-value")
		cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: "actor-1", PrincipalKind: "agent"})
		rec := httptest.NewRecorder()
		addBoardCardFromRaw(rec, req, opts, boardID, raw, "boards.cards.add")
		return rec
	}

	column := post(map[string]any{"title": "Nope", "column_key": "not-a-column"})
	if column.Code != http.StatusBadRequest || !strings.Contains(column.Body.String(), "column_key") {
		t.Fatalf("invalid column: %d %s", column.Code, column.Body.String())
	}

	nul := post(map[string]any{"title": "bad\x00title", "summary": "test"})
	if nul.Code != http.StatusBadRequest || strings.Contains(nul.Body.String(), "internal_error") || !strings.Contains(nul.Body.String(), "NUL") {
		t.Fatalf("nul title: %d %s", nul.Code, nul.Body.String())
	}

	ref := post(map[string]any{"title": "Bad ref", "related_refs": []string{"not a ref"}})
	if ref.Code != http.StatusBadRequest || strings.Contains(ref.Body.String(), "internal_error") || !strings.Contains(ref.Body.String(), "typed ref") {
		t.Fatalf("malformed ref: %d %s", ref.Code, ref.Body.String())
	}

	missingThread := post(map[string]any{"title": "Missing thread", "related_refs": []string{"thread:" + uuid.NewString()}})
	if missingThread.Code != http.StatusNotFound || !strings.Contains(missingThread.Body.String(), "not_found") {
		t.Fatalf("unknown thread: %d %s", missingThread.Code, missingThread.Body.String())
	}

	missingDoc := post(map[string]any{"title": "Missing doc", "document_ref": "document:" + uuid.NewString()})
	if missingDoc.Code != http.StatusNotFound || !strings.Contains(missingDoc.Body.String(), "not_found") {
		t.Fatalf("unknown document: %d %s", missingDoc.Code, missingDoc.Body.String())
	}

	created := post(map[string]any{"title": "Loop: storage-auto", "summary": "test"})
	if created.Code != http.StatusCreated || !strings.Contains(created.Body.String(), "Loop: storage-auto") {
		t.Fatalf("plain create: %d %s", created.Code, created.Body.String())
	}
	if strings.Contains(created.Body.String(), "secret-value") {
		t.Fatalf("success response leaked request material: %s", created.Body.String())
	}
}

func TestBoardCardInsertFailureLogsCauseAndPersistsNothing(t *testing.T) {
	// Serial: this file mutates process-wide logging.
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })

	ctx := context.Background()
	workspace, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	t.Cleanup(func() { _ = workspace.Close() })
	store := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	thread, err := store.CreateThread(ctx, "actor-1", map[string]any{
		"title":      "Board thread",
		"type":       "incident",
		"provenance": map[string]any{"sources": []string{"inferred"}},
	})
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	threadID, _ := thread.Thread["id"].(string)
	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{
		"title":     "Lanes",
		"thread_id": threadID,
	})
	if err != nil {
		t.Fatalf("create board: %v", err)
	}
	boardID, _ := board["id"].(string)
	if _, err := workspace.DB().ExecContext(ctx, `CREATE TRIGGER card_insert_probe BEFORE INSERT ON cards BEGIN SELECT RAISE(ABORT, 'probe failure'); END`); err != nil {
		t.Fatalf("install probe trigger: %v", err)
	}
	t.Cleanup(func() {
		_, _ = workspace.DB().ExecContext(context.Background(), `DROP TRIGGER IF EXISTS card_insert_probe`)
	})

	const title = "title-must-not-be-logged"
	const body = "body-must-not-be-logged"
	req := httptest.NewRequest(http.MethodPost, "/boards/"+boardID+"/cards", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer super-secret-token")
	cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: "actor-1", PrincipalKind: "agent"})
	rec := httptest.NewRecorder()
	addBoardCardFromRaw(rec, req, handlerOptions{primitiveStore: store}, boardID, map[string]any{
		"title":   title,
		"summary": body,
	}, "boards.cards.add")

	if rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), `"code":"internal_error"`) || !strings.Contains(rec.Body.String(), "failed to add board card") {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "probe failure") || strings.Contains(rec.Body.String(), title) || strings.Contains(rec.Body.String(), body) {
		t.Fatalf("client response leaked the insert failure: %s", rec.Body.String())
	}
	logged := output.String()
	if strings.Count(logged, "anx-core: server error") != 1 || !strings.Contains(logged, "probe failure") {
		t.Fatalf("log: %s", logged)
	}
	if strings.Contains(logged, title) || strings.Contains(logged, body) || strings.Contains(logged, "super-secret-token") {
		t.Fatalf("log leaked request material: %s", logged)
	}
	var cards int
	if err := workspace.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM cards`).Scan(&cards); err != nil {
		t.Fatalf("count cards: %v", err)
	}
	if cards != 0 {
		t.Fatalf("card persisted: %d", cards)
	}
}

func TestOutcomeUnknownLogsSanitizedChain(t *testing.T) {
	// Serial: this file mutates process-wide logging.
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	t.Cleanup(func() { log.SetOutput(previous) })

	for _, cause := range []string{"commit board card create transaction: probe commit failure", "finalize card content: probe promote failure"} {
		output.Reset()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/boards/board:omi-lanes/cards?token=super-secret-token", nil)
		req.Header.Set("Authorization", "Bearer super-secret-token")
		cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: "actor-1", PrincipalKind: "agent"})
		err := &primitives.MutationOutcomeUnknown{Cause: fmt.Errorf("%s", cause)}
		if !writeMutationOutcomeUnknownError(rec, req, err) {
			t.Fatal("outcome unknown was not classified")
		}
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), `"code":"outcome_unknown"`) || strings.Contains(rec.Body.String(), "probe") {
			t.Fatalf("client response: %d %s", rec.Code, rec.Body.String())
		}
		logged := output.String()
		if strings.Count(logged, "anx-core: server error") != 1 || !strings.Contains(logged, "status=409") || !strings.Contains(logged, cause) {
			t.Fatalf("log for %q: %s", cause, logged)
		}
		if strings.Contains(logged, "super-secret-token") || strings.Contains(logged, "Bearer") || strings.Contains(logged, "omi-lanes") {
			t.Fatalf("log leaked request material: %s", logged)
		}
	}
}
