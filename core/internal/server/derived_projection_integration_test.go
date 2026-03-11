package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/schema"
)

func TestRefreshDerivedThreadProjectionBasicFlow(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, 201)

	createResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"Projection thread",
			"type":"incident",
			"status":"active",
			"priority":"p1",
			"tags":["ops"],
			"cadence":"reactive",
			"current_summary":"summary",
			"next_actions":["do x"],
			"key_artifacts":[],
			"provenance":{"sources":["inferred"]}
		}
	}`, 201)
	defer createResp.Body.Close()

	var created struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode thread response: %v", err)
	}
	threadID := anyString(created.Thread["id"])
	if threadID == "" {
		t.Fatal("expected thread id")
	}

	contract, err := schema.Load(filepath.Join("..", "..", "..", "contracts", "oar-schema.yaml"))
	if err != nil {
		t.Fatalf("load schema contract: %v", err)
	}

	opts := handlerOptions{
		primitiveStore:   primitives.NewStore(h.workspace.DB(), h.workspace.Layout().ArtifactContentDir),
		contract:         contract,
		inboxRiskHorizon: defaultInboxRiskHorizon,
	}
	if err := refreshDerivedThreadProjection(context.Background(), opts, threadID, time.Now().UTC(), "actor-1"); err != nil {
		t.Fatalf("refreshDerivedThreadProjection: %v", err)
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

	postJSONExpectStatus(t, h.baseURL+"/commitments", `{
		"actor_id":"actor-1",
		"commitment":{
			"thread_id":"`+threadID+`",
			"title":"Projection commitment",
			"owner":"actor-1",
			"due_at":"`+time.Now().UTC().Add(24*time.Hour).Format(time.RFC3339)+`",
			"status":"open",
			"definition_of_done":["done"],
			"links":["url:https://example.com/task"],
			"provenance":{"sources":["inferred"]}
		}
	}`, 201).Body.Close()

	postJSONExpectStatus(t, h.baseURL+"/docs", `{
		"actor_id":"actor-1",
		"document":{"id":"proj-doc-1","thread_id":"`+threadID+`","title":"Projection doc","status":"active","labels":["ops"]},
		"refs":["thread:`+threadID+`"],
		"content":"initial text",
		"content_type":"text"
	}`, 201).Body.Close()

	items := getInboxItems(t, h.baseURL)
	if len(items) != 2 {
		t.Fatalf("expected decision + commitment inbox items, got %#v", items)
	}

	projection := mustLoadDerivedThreadProjection(t, h.workspace.DB(), threadID)
	if projection.InboxCount != 2 || projection.DecisionRequestCount != 1 || projection.OpenCommitmentCount != 1 || projection.DocumentCount != 1 {
		t.Fatalf("unexpected derived thread projection: %#v", projection)
	}
	if inboxRowCount := countDerivedInboxItemsForThread(t, h.workspace.DB(), threadID); inboxRowCount != 2 {
		t.Fatalf("expected two derived inbox rows, got %d", inboxRowCount)
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

func mustLoadDerivedThreadProjection(t *testing.T, db *sql.DB, threadID string) primitives.DerivedThreadProjection {
	t.Helper()

	store := primitives.NewStore(db, "")
	projection, err := store.GetDerivedThreadProjection(context.Background(), threadID)
	if err != nil {
		t.Fatalf("get derived thread projection: %v", err)
	}
	return projection
}
