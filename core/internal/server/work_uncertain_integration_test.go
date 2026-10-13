package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

func TestWorkPatchPostCommitFailureIsNonRetryable(t *testing.T) {
	requireIntegrationTest(t)
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	store := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	work, err := store.CreateWork(ctx, "actor-1", "", map[string]any{"title": "Post-commit annotation failure"})
	if err != nil {
		t.Fatal(err)
	}
	contract, err := schema.Load(filepath.Join("..", "..", "..", "contracts", "anx-schema.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHandler(contract.Version, WithPrimitiveStore(store), WithSchemaContract(contract), WithActorRegistry(actors.NewStore(ws.DB())), WithAllowUnauthenticatedWrites(true), WithEnableDevActorMode(true)))
	defer server.Close()
	postJSONExpectStatus(t, server.URL+"/actors", `{"actor":{"id":"actor-1","display_name":"Writer","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()
	// The write commits; only its subsequent response projection is broken.
	_, err = ws.DB().ExecContext(ctx, `CREATE TRIGGER review_bad_response AFTER UPDATE OF version ON work_metadata BEGIN UPDATE cards SET definition_of_done_json='broken' WHERE id=NEW.card_id; END`)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]any{"actor_id": "actor-1", "if_version": work["version"], "patch": map[string]any{"next_action": "review"}})
	if err != nil {
		t.Fatal(err)
	}
	resp := patchJSONExpectStatus(t, server.URL+"/work/"+url.PathEscape(anyString(work["ref"])), string(body), http.StatusConflict)
	var payload map[string]any
	if err = json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	apiError := payload["error"].(map[string]any)
	if apiError["code"] != "outcome_unknown" || apiError["recoverable"] != false {
		t.Fatalf("unsafe post-commit error: %v", payload)
	}
	if _, err = ws.DB().ExecContext(ctx, `UPDATE cards SET definition_of_done_json='[]' WHERE id=?`, work["id"]); err != nil {
		t.Fatal(err)
	}
	after, err := store.GetWork(ctx, anyString(work["ref"]))
	if err != nil || after["next_action"] != "review" || after["version"].(int64) != work["version"].(int64)+1 {
		t.Fatalf("annotation did not commit exactly once: %v %v", after, err)
	}
	t.Logf("HTTP 409 outcome_unknown, recoverable=false; committed annotation version=%v", after["version"])
}
