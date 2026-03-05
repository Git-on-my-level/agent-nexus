package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"organization-autorunner-core/internal/actors"
	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/schema"
	"organization-autorunner-core/internal/storage"
)

func TestPrimitivesCRUDRoundTrip(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)

	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	eventResp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"my_custom_event",
			"thread_id":"thread-1",
			"refs":["customprefix:abc"],
			"summary":"custom event",
			"payload":{"x":1},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer eventResp.Body.Close()

	var createdEvent map[string]map[string]any
	if err := json.NewDecoder(eventResp.Body).Decode(&createdEvent); err != nil {
		t.Fatalf("decode create event response: %v", err)
	}
	eventID, _ := createdEvent["event"]["id"].(string)
	if eventID == "" {
		t.Fatal("expected created event id")
	}

	getEventResp, err := http.Get(h.baseURL + "/events/" + eventID)
	if err != nil {
		t.Fatalf("GET /events/{id}: %v", err)
	}
	defer getEventResp.Body.Close()
	if getEventResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected GET /events/{id} status: got %d", getEventResp.StatusCode)
	}
	var loadedEvent map[string]map[string]any
	if err := json.NewDecoder(getEventResp.Body).Decode(&loadedEvent); err != nil {
		t.Fatalf("decode get event response: %v", err)
	}
	if loadedEvent["event"]["type"] != "my_custom_event" {
		t.Fatalf("unexpected event type: %#v", loadedEvent["event"]["type"])
	}

	refs, ok := loadedEvent["event"]["refs"].([]any)
	if !ok || len(refs) != 1 || refs[0] != "customprefix:abc" {
		t.Fatalf("unexpected event refs: %#v", loadedEvent["event"]["refs"])
	}

	artifactResp := postJSONExpectStatus(t, h.baseURL+"/artifacts", `{
		"actor_id":"actor-1",
		"artifact":{
			"kind":"my_custom_artifact",
			"refs":["thread:thread-1","customprefix:abc"],
			"summary":"artifact summary"
		},
		"content":"hello artifact",
		"content_type":"text"
	}`, http.StatusCreated)
	defer artifactResp.Body.Close()

	var createdArtifact map[string]map[string]any
	if err := json.NewDecoder(artifactResp.Body).Decode(&createdArtifact); err != nil {
		t.Fatalf("decode create artifact response: %v", err)
	}
	artifactID, _ := createdArtifact["artifact"]["id"].(string)
	if artifactID == "" {
		t.Fatal("expected created artifact id")
	}

	getArtifactResp, err := http.Get(h.baseURL + "/artifacts/" + artifactID)
	if err != nil {
		t.Fatalf("GET /artifacts/{id}: %v", err)
	}
	defer getArtifactResp.Body.Close()
	if getArtifactResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected GET /artifacts/{id} status: got %d", getArtifactResp.StatusCode)
	}

	var loadedArtifact map[string]map[string]any
	if err := json.NewDecoder(getArtifactResp.Body).Decode(&loadedArtifact); err != nil {
		t.Fatalf("decode get artifact response: %v", err)
	}
	if loadedArtifact["artifact"]["kind"] != "my_custom_artifact" {
		t.Fatalf("unexpected artifact kind: %#v", loadedArtifact["artifact"]["kind"])
	}

	contentResp, err := http.Get(h.baseURL + "/artifacts/" + artifactID + "/content")
	if err != nil {
		t.Fatalf("GET /artifacts/{id}/content: %v", err)
	}
	defer contentResp.Body.Close()
	if contentResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected content status: got %d", contentResp.StatusCode)
	}
	bodyBytes := make([]byte, 0)
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(contentResp.Body); err != nil {
		t.Fatalf("read content response: %v", err)
	}
	bodyBytes = append(bodyBytes, buf.Bytes()...)
	if string(bodyBytes) != "hello artifact" {
		t.Fatalf("unexpected artifact content: got %q", string(bodyBytes))
	}

	listResp, err := http.Get(h.baseURL + "/artifacts?thread_id=thread-1")
	if err != nil {
		t.Fatalf("GET /artifacts?thread_id=...: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected list status: got %d", listResp.StatusCode)
	}
	var listed map[string][]map[string]any
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed["artifacts"]) != 1 {
		t.Fatalf("expected one filtered artifact, got %d", len(listed["artifacts"]))
	}
}

func TestInvalidTypedRefsRejectedForEventsAndArtifacts(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	eventResp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"custom",
			"refs":["invalidref"],
			"summary":"invalid",
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusBadRequest)
	defer eventResp.Body.Close()

	artifactResp := postJSONExpectStatus(t, h.baseURL+"/artifacts", `{
		"actor_id":"actor-1",
		"artifact":{"kind":"custom","refs":["invalidref"]},
		"content":"x",
		"content_type":"text"
	}`, http.StatusBadRequest)
	defer artifactResp.Body.Close()
}

func TestCreateArtifactRejectsUnsafeArtifactIDs(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	respWithSeparator := postJSONExpectStatus(t, h.baseURL+"/artifacts", `{
		"actor_id":"actor-1",
		"artifact":{
			"id":"../../etc/passwd",
			"kind":"doc",
			"refs":["thread:thread-1"],
			"summary":"bad artifact id"
		},
		"content":"x",
		"content_type":"text"
	}`, http.StatusBadRequest)
	assertErrorMessageContains(t, respWithSeparator, "artifact.id")

	respWithAbsolute := postJSONExpectStatus(t, h.baseURL+"/artifacts", `{
		"actor_id":"actor-1",
		"artifact":{
			"id":"/tmp/absolute",
			"kind":"doc",
			"refs":["thread:thread-1"],
			"summary":"bad artifact id"
		},
		"content":"x",
		"content_type":"text"
	}`, http.StatusBadRequest)
	assertErrorMessageContains(t, respWithAbsolute, "artifact.id")
}

func TestGetSnapshotByID(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)

	_, err := h.workspace.DB().Exec(
		`INSERT INTO snapshots(id, kind, thread_id, updated_at, updated_by, body_json, provenance_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"snapshot-1",
		"thread",
		"thread-1",
		"2026-03-04T11:00:00Z",
		"actor-1",
		`{"title":"Thread title"}`,
		`{"sources":["inferred"]}`,
	)
	if err != nil {
		t.Fatalf("insert snapshot row: %v", err)
	}

	resp, err := http.Get(h.baseURL + "/snapshots/snapshot-1")
	if err != nil {
		t.Fatalf("GET /snapshots/{id}: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected snapshot status: got %d", resp.StatusCode)
	}

	var payload map[string]map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode snapshot response: %v", err)
	}
	if payload["snapshot"]["id"] != "snapshot-1" {
		t.Fatalf("unexpected snapshot id: %#v", payload["snapshot"]["id"])
	}
	if payload["snapshot"]["title"] != "Thread title" {
		t.Fatalf("unexpected snapshot body: %#v", payload["snapshot"])
	}
}

type primitivesTestHarness struct {
	workspace *storage.Workspace
	baseURL   string
}

func newPrimitivesTestServer(t *testing.T) primitivesTestHarness {
	t.Helper()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	contractPath := filepath.Join("..", "..", "contracts", "oar-schema.yaml")
	contract, err := schema.Load(contractPath)
	if err != nil {
		_ = workspace.Close()
		t.Fatalf("load schema contract: %v", err)
	}

	registry := actors.NewStore(workspace.DB())
	primitiveStore := primitives.NewStore(workspace.DB(), workspace.Layout().ArtifactContentDir)
	handler := NewHandler(
		contract.Version,
		WithHealthCheck(workspace.Ping),
		WithActorRegistry(registry),
		WithPrimitiveStore(primitiveStore),
		WithSchemaContract(contract),
	)
	server := httptest.NewServer(handler)
	t.Cleanup(func() {
		server.Close()
		_ = workspace.Close()
	})

	return primitivesTestHarness{workspace: workspace, baseURL: server.URL}
}

func postJSONExpectStatus(t *testing.T, url string, body string, expectedStatus int) *http.Response {
	t.Helper()

	resp, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s failed: %v", url, err)
	}
	if resp.StatusCode != expectedStatus {
		defer resp.Body.Close()
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("POST %s unexpected status: got %d want %d body=%s", url, resp.StatusCode, expectedStatus, string(bodyBytes))
	}
	return resp
}
