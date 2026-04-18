package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"agent-nexus-core/internal/blob"
	"path/filepath"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

func TestRefreshDerivedTopicProjectionBasicFlow(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, 201)

	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":           "Projection thread",
		"type":            "incident",
		"status":          "active",
		"priority":        "p1",
		"tags":            []any{"ops"},
		"cadence":         "reactive",
		"current_summary": "summary",
		"next_actions":    []any{"do x"},
		"key_artifacts":   []any{},
		"provenance":      map[string]any{"sources": []any{"inferred"}},
	})

	contract, err := schema.Load(filepath.Join("..", "..", "..", "contracts", "anx-schema.yaml"))
	if err != nil {
		t.Fatalf("load schema contract: %v", err)
	}

	opts := handlerOptions{
		primitiveStore:   primitives.NewStore(h.workspace.DB(), blob.NewFilesystemBackend(h.workspace.Layout().ArtifactContentDir), h.workspace.Layout().ArtifactContentDir),
		contract:         contract,
		inboxRiskHorizon: defaultInboxRiskHorizon,
	}
	if err := refreshDerivedTopicProjection(context.Background(), opts, threadID, time.Now().UTC(), "actor-1"); err != nil {
		t.Fatalf("refreshDerivedTopicProjection: %v", err)
	}

	postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"decision_needed",
			"thread_id":"`+threadID+`",
			"refs":["thread:`+threadID+`"],
			"summary":"Need a decision",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, 201).Body.Close()

	boardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{
		"actor_id":"actor-1",
		"board":{
			"title":"Projection board",
			"refs":["thread:`+threadID+`"]
		}
	}`, 201)
	defer boardResp.Body.Close()
	var createdBoard struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(boardResp.Body).Decode(&createdBoard); err != nil {
		t.Fatalf("decode board response: %v", err)
	}
	boardID := anyString(createdBoard.Board["id"])
	boardUpdatedAt := anyString(createdBoard.Board["updated_at"])
	postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards", `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+boardUpdatedAt+`",
		"title":"Projection work item",
		"related_refs":["thread:`+threadID+`"],
		"column_key":"ready",
		"due_at":"`+time.Now().UTC().Add(24*time.Hour).Format(time.RFC3339)+`"
	}`, 201).Body.Close()

	postJSONExpectStatus(t, h.baseURL+"/docs", `{
		"actor_id":"actor-1",
		"document":{"id":"proj-doc-1","thread_id":"`+threadID+`","title":"Projection doc","labels":["ops"]},
		"refs":["thread:`+threadID+`"],
		"content":"initial text",
		"content_type":"text"
	}`, 201).Body.Close()

	items := getInboxItems(t, h.baseURL)
	if len(items) != 2 {
		t.Fatalf("expected decision + work item inbox items, got %#v", items)
	}

	projection := mustLoadDerivedTopicProjection(t, h.workspace.DB(), threadID)
	if projection.InboxCount != 2 || projection.DecisionRequestCount != 1 || workspaceIntValue(projection.Data["open_work_item_count"]) != 1 || projection.DocumentCount != 1 {
		t.Fatalf("unexpected derived thread projection: %#v", projection)
	}
	if inboxRowCount := countDerivedInboxItemsForThread(t, h.workspace.DB(), threadID); inboxRowCount != 2 {
		t.Fatalf("expected two derived inbox rows, got %d", inboxRowCount)
	}
}

func TestEnsureDerivedTopicProjectionRefreshesExpiredTimeSensitiveState(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, 201)

	baseNow := time.Now().UTC()
	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":            "Expiring projection thread",
		"type":             "incident",
		"status":           "active",
		"priority":         "p1",
		"tags":             []any{"ops"},
		"cadence":          "daily",
		"next_check_in_at": baseNow.Add(30 * time.Second).Format(time.RFC3339),
		"current_summary":  "summary",
		"next_actions":     []any{"check"},
		"key_artifacts":    []any{},
		"provenance":       map[string]any{"sources": []any{"inferred"}},
	})

	contract, err := schema.Load(filepath.Join("..", "..", "..", "contracts", "anx-schema.yaml"))
	if err != nil {
		t.Fatalf("load schema contract: %v", err)
	}

	opts := handlerOptions{
		primitiveStore:   primitives.NewStore(h.workspace.DB(), blob.NewFilesystemBackend(h.workspace.Layout().ArtifactContentDir), h.workspace.Layout().ArtifactContentDir),
		contract:         contract,
		inboxRiskHorizon: defaultInboxRiskHorizon,
	}
	if err := refreshDerivedTopicProjection(context.Background(), opts, threadID, baseNow, "actor-1"); err != nil {
		t.Fatalf("refreshDerivedTopicProjection: %v", err)
	}

	initialProjection := mustLoadDerivedTopicProjection(t, h.workspace.DB(), threadID)
	if initialProjection.Stale {
		t.Fatalf("expected fresh projection to start non-stale, got %#v", initialProjection)
	}

	maintainer := NewProjectionMaintainer(ProjectionMaintainerConfig{
		PrimitiveStore:   opts.primitiveStore,
		Contract:         opts.contract,
		InboxRiskHorizon: opts.inboxRiskHorizon,
		DirtyBatchSize:   20,
		SystemActorID:    "actor-1",
	})
	if err := maintainer.RunFullRebuild(context.Background(), baseNow.Add(2*time.Minute), "actor-1"); err != nil {
		t.Fatalf("RunFullRebuild: %v", err)
	}
	refreshedState, err := loadTopicProjectionState(context.Background(), opts, threadID)
	if err != nil {
		t.Fatalf("loadTopicProjectionState: %v", err)
	}
	if !refreshedState.Projection.Stale {
		t.Fatalf("expected expired projection to refresh stale=true after next_check_in_at, got %#v", refreshedState.Projection)
	}
}

func TestDocumentThreadRetargetRefreshesBothDerivedProjections(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, 201)

	createThread := func(title string) string {
		return integrationSeedThread(t, h, "actor-1", map[string]any{
			"title":           title,
			"type":            "incident",
			"status":          "active",
			"priority":        "p1",
			"tags":            []any{"ops"},
			"cadence":         "reactive",
			"current_summary": "summary",
			"next_actions":    []any{"check"},
			"key_artifacts":   []any{},
			"provenance":      map[string]any{"sources": []any{"inferred"}},
		})
	}

	fromThreadID := createThread("Projection source")
	toThreadID := createThread("Projection target")

	createDocResp := postJSONExpectStatus(t, h.baseURL+"/docs", `{
		"actor_id":"actor-1",
		"document":{"id":"projection-retarget-doc","thread_id":"`+fromThreadID+`","title":"Projection move doc"},
		"refs":["thread:`+fromThreadID+`"],
		"content":"initial text",
		"content_type":"text"
	}`, 201)
	defer createDocResp.Body.Close()

	var createdDoc struct {
		Document map[string]any `json:"document"`
		Revision map[string]any `json:"revision"`
	}
	if err := json.NewDecoder(createDocResp.Body).Decode(&createdDoc); err != nil {
		t.Fatalf("decode create doc response: %v", err)
	}
	baseRevisionID := anyString(createdDoc.Revision["revision_id"])
	if baseRevisionID == "" {
		t.Fatal("expected base revision id")
	}

	if projection := mustLoadDerivedTopicProjection(t, h.workspace.DB(), fromThreadID); projection.DocumentCount != 1 {
		t.Fatalf("expected source projection document_count=1 after create, got %#v", projection)
	}

	updateResp := requestJSONExpectStatus(t, http.MethodPost, h.baseURL+"/docs/projection-retarget-doc/revisions", `{
		"actor_id":"actor-1",
		"document":{"thread_id":"`+toThreadID+`","title":"Projection move doc"},
		"if_base_revision":"`+baseRevisionID+`",
		"content":"moved text",
		"content_type":"text",
		"refs":["thread:`+toThreadID+`"]
	}`, 201)
	defer updateResp.Body.Close()

	if projection := mustLoadDerivedTopicProjection(t, h.workspace.DB(), fromThreadID); projection.DocumentCount != 0 {
		t.Fatalf("expected source projection document_count=0 after move, got %#v", projection)
	}
	if projection := mustLoadDerivedTopicProjection(t, h.workspace.DB(), toThreadID); projection.DocumentCount != 1 {
		t.Fatalf("expected target projection document_count=1 after move, got %#v", projection)
	}
}

func countDerivedInboxItemsForThread(t *testing.T, db *sql.DB, threadID string) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM derived_inbox_items WHERE thread_id = ?`, threadID).Scan(&count); err != nil {
		t.Fatalf("count derived inbox items: %v", err)
	}
	return count
}

func mustLoadDerivedTopicProjection(t *testing.T, db *sql.DB, threadID string) primitives.DerivedTopicProjection {
	t.Helper()

	store := primitives.NewStore(db, nil, "")
	projection, err := store.GetDerivedTopicProjection(context.Background(), threadID)
	if err != nil {
		t.Fatalf("get derived thread projection: %v", err)
	}
	return projection
}
