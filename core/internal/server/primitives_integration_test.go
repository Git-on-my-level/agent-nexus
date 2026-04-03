package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"organization-autorunner-core/internal/blob"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"organization-autorunner-core/internal/actors"
	"organization-autorunner-core/internal/auth"
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

	contentHash, _ := createdArtifact["artifact"]["content_hash"].(string)
	if contentHash == "" {
		t.Fatal("expected content_hash in created artifact")
	}
	expectedHash := sha256Hex([]byte("hello artifact"))
	if contentHash != expectedHash {
		t.Fatalf("content_hash mismatch: got %q want %q", contentHash, expectedHash)
	}
	if _, ok := createdArtifact["artifact"]["content_path"]; ok {
		t.Fatalf("expected create artifact response to omit content_path: %#v", createdArtifact["artifact"])
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
	if loadedArtifact["artifact"]["content_hash"] != expectedHash {
		t.Fatalf("content_hash mismatch on GET: got %#v", loadedArtifact["artifact"]["content_hash"])
	}
	if _, ok := loadedArtifact["artifact"]["content_path"]; ok {
		t.Fatalf("expected get artifact response to omit content_path: %#v", loadedArtifact["artifact"])
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

func TestPrimitivesCRUDRoundTripWithObjectBackend(t *testing.T) {
	t.Parallel()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	defer workspace.Close()
	primitiveStore := primitives.NewStore(
		workspace.DB(),
		blob.NewObjectStoreBackend(workspace.Layout().ArtifactContentDir),
		workspace.Layout().ArtifactContentDir,
	)
	h := newPrimitivesTestServerWithStore(t, workspace, primitiveStore)

	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	artifactResp := postJSONExpectStatus(t, h.baseURL+"/artifacts", `{
		"actor_id":"actor-1",
		"artifact":{
			"kind":"my_custom_artifact",
			"refs":["thread:thread-1"],
			"summary":"object store artifact"
		},
		"content":"object-store content",
		"content_type":"text"
	}`, http.StatusCreated)
	defer artifactResp.Body.Close()

	var created map[string]map[string]any
	if err := json.NewDecoder(artifactResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode object-backend create response: %v", err)
	}
	artifactID := asString(created["artifact"]["id"])
	if artifactID == "" {
		t.Fatal("expected object-backend artifact id")
	}

	contentResp, err := http.Get(h.baseURL + "/artifacts/" + artifactID + "/content")
	if err != nil {
		t.Fatalf("GET object-backend artifact content: %v", err)
	}
	defer contentResp.Body.Close()
	if contentResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected object-backend content status: %d", contentResp.StatusCode)
	}
	body, err := io.ReadAll(contentResp.Body)
	if err != nil {
		t.Fatalf("read object-backend content: %v", err)
	}
	if string(body) != "object-store content" {
		t.Fatalf("unexpected object-backend content: %q", string(body))
	}
}

func TestArtifactsListBySecondaryThreadRef(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/artifacts", `{
		"actor_id":"actor-1",
		"artifact":{
			"kind":"my_custom_artifact",
			"refs":["thread:thread-primary","thread:thread-secondary","customprefix:abc"],
			"summary":"cross-thread artifact"
		},
		"content":"hello artifact",
		"content_type":"text"
	}`, http.StatusCreated)
	defer createResp.Body.Close()

	var created map[string]map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create artifact response: %v", err)
	}
	artifactID, _ := created["artifact"]["id"].(string)
	if artifactID == "" {
		t.Fatal("expected created artifact id")
	}

	listResp, err := http.Get(h.baseURL + "/artifacts?thread_id=thread-secondary")
	if err != nil {
		t.Fatalf("GET /artifacts?thread_id=thread-secondary: %v", err)
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
		t.Fatalf("expected one filtered artifact for secondary thread ref, got %d", len(listed["artifacts"]))
	}
	if got := asString(listed["artifacts"][0]["id"]); got != artifactID {
		t.Fatalf("expected artifact %q in secondary-thread filter, got %#v", artifactID, listed["artifacts"])
	}
}

func TestCreateArtifactReturnsConflictForDuplicateID(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	firstResp := postJSONExpectStatus(t, h.baseURL+"/artifacts", `{
		"actor_id":"actor-1",
		"artifact":{
			"id":"artifact-duplicate",
			"kind":"my_custom_artifact",
			"refs":["thread:thread-1"],
			"summary":"first create"
		},
		"content":"hello artifact",
		"content_type":"text"
	}`, http.StatusCreated)
	firstResp.Body.Close()

	secondResp := postJSONExpectStatus(t, h.baseURL+"/artifacts", `{
		"actor_id":"actor-1",
		"artifact":{
			"id":"artifact-duplicate",
			"kind":"my_custom_artifact",
			"refs":["thread:thread-1"],
			"summary":"duplicate create"
		},
		"content":"hello artifact again",
		"content_type":"text"
	}`, http.StatusConflict)
	defer secondResp.Body.Close()

	var payload map[string]map[string]any
	if err := json.NewDecoder(secondResp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode conflict response: %v", err)
	}
	if payload["error"]["code"] != "conflict" {
		t.Fatalf("expected conflict error code, got %#v", payload["error"])
	}
	if payload["error"]["message"] != "artifact already exists" {
		t.Fatalf("expected duplicate artifact message, got %#v", payload["error"])
	}
}

func TestDocumentsLifecycleRoundTrip(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"id":"thread-1",
			"title":"Replay event thread",
			"type":"incident",
			"status":"active",
			"priority":"p2",
			"tags":["events"],
			"cadence":"daily",
			"next_check_in_at":"2026-03-05T00:00:00Z",
			"current_summary":"summary",
			"next_actions":["review"],
			"key_artifacts":[],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated).Body.Close()

	createResp := postJSONExpectStatus(t, h.baseURL+"/docs", `{
		"actor_id":"actor-1",
		"document":{"id":"doc-1","title":"Constitution","labels":["governance"]},
		"refs":["thread:thread-docs"],
		"content":"initial text",
		"content_type":"text"
	}`, http.StatusCreated)
	defer createResp.Body.Close()

	var created map[string]map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create doc response: %v", err)
	}
	if created["document"]["id"] != "doc-1" {
		t.Fatalf("unexpected document id: %#v", created["document"]["id"])
	}
	headRevisionID, _ := created["revision"]["revision_id"].(string)
	if headRevisionID == "" {
		t.Fatal("expected created revision id")
	}

	createContentHash, _ := created["revision"]["content_hash"].(string)
	if createContentHash == "" {
		t.Fatal("expected content_hash in created revision")
	}
	if createContentHash != sha256Hex([]byte("initial text")) {
		t.Fatalf("content_hash mismatch on create: got %q", createContentHash)
	}
	createdArtifactMeta, ok := created["revision"]["artifact"].(map[string]any)
	if !ok {
		t.Fatalf("expected created revision artifact metadata map, got %#v", created["revision"]["artifact"])
	}
	if _, ok := createdArtifactMeta["content_path"]; ok {
		t.Fatalf("expected created revision artifact metadata to omit content_path: %#v", createdArtifactMeta)
	}
	createRevisionHash, _ := created["revision"]["revision_hash"].(string)
	if createRevisionHash == "" {
		t.Fatal("expected revision_hash in created revision")
	}

	getResp, err := http.Get(h.baseURL + "/docs/doc-1")
	if err != nil {
		t.Fatalf("GET /docs/{document_id}: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected GET /docs/{document_id} status: got %d", getResp.StatusCode)
	}

	listResp, err := http.Get(h.baseURL + "/docs")
	if err != nil {
		t.Fatalf("GET /docs: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected GET /docs status: got %d", listResp.StatusCode)
	}
	var listed struct {
		Documents []map[string]any `json:"documents"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode documents list response: %v", err)
	}
	if len(listed.Documents) != 1 {
		t.Fatalf("expected one document in list, got %d", len(listed.Documents))
	}
	if listed.Documents[0]["id"] != "doc-1" {
		t.Fatalf("unexpected listed document id: %#v", listed.Documents[0]["id"])
	}
	if _, ok := listed.Documents[0]["title"].(string); !ok {
		t.Fatalf("expected listed document title, got %#v", listed.Documents[0]["title"])
	}
	headRevisionSummary, ok := listed.Documents[0]["head_revision"].(map[string]any)
	if !ok {
		t.Fatalf("expected listed document head_revision summary, got %#v", listed.Documents[0]["head_revision"])
	}
	if headRevisionSummary["revision_id"] != headRevisionID {
		t.Fatalf("unexpected listed document head revision id: %#v", headRevisionSummary["revision_id"])
	}
	if headRevisionSummary["revision_number"] != float64(1) {
		t.Fatalf("unexpected listed document head revision number: %#v", headRevisionSummary["revision_number"])
	}
	if _, ok := headRevisionSummary["artifact_id"].(string); !ok {
		t.Fatalf("expected listed document head revision artifact id, got %#v", headRevisionSummary["artifact_id"])
	}
	if headRevisionSummary["content_type"] != "text" {
		t.Fatalf("unexpected listed document head revision content type: %#v", headRevisionSummary["content_type"])
	}

	filteredListResp, err := http.Get(h.baseURL + "/docs?thread_id=thread-1")
	if err != nil {
		t.Fatalf("GET /docs?thread_id=thread-1: %v", err)
	}
	defer filteredListResp.Body.Close()
	if filteredListResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected GET /docs?thread_id=thread-1 status: got %d", filteredListResp.StatusCode)
	}
	var unrelatedThreadDocs struct {
		Documents []map[string]any `json:"documents"`
	}
	if err := json.NewDecoder(filteredListResp.Body).Decode(&unrelatedThreadDocs); err != nil {
		t.Fatalf("decode unrelated thread-filtered documents response: %v", err)
	}
	if len(unrelatedThreadDocs.Documents) != 0 {
		t.Fatalf("expected no documents for unrelated thread filter, got %#v", unrelatedThreadDocs.Documents)
	}

	threadDocsResp, err := http.Get(h.baseURL + "/docs?thread_id=thread-docs")
	if err != nil {
		t.Fatalf("GET /docs?thread_id=thread-docs: %v", err)
	}
	defer threadDocsResp.Body.Close()
	if threadDocsResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected GET /docs?thread_id=thread-docs status: got %d", threadDocsResp.StatusCode)
	}
	var threadDocs struct {
		Documents []map[string]any `json:"documents"`
	}
	if err := json.NewDecoder(threadDocsResp.Body).Decode(&threadDocs); err != nil {
		t.Fatalf("decode thread-filtered documents response: %v", err)
	}
	if len(threadDocs.Documents) != 1 {
		t.Fatalf("expected one thread-filtered document, got %#v", threadDocs.Documents)
	}
	if got := threadDocs.Documents[0]["thread_id"]; got != "thread-docs" {
		t.Fatalf("expected thread-filtered document thread_id=thread-docs, got %#v", got)
	}

	updateResp := requestJSONExpectStatus(t, http.MethodPatch, h.baseURL+"/docs/doc-1", `{
		"actor_id":"actor-1",
		"document":{"title":"Constitution v2"},
		"if_base_revision":"`+headRevisionID+`",
		"content":"second text",
		"content_type":"text"
	}`, http.StatusOK)
	defer updateResp.Body.Close()

	var updated map[string]map[string]any
	if err := json.NewDecoder(updateResp.Body).Decode(&updated); err != nil {
		t.Fatalf("decode update doc response: %v", err)
	}
	if updated["document"]["head_revision_number"] != float64(2) {
		t.Fatalf("unexpected head revision number: %#v", updated["document"]["head_revision_number"])
	}
	newHeadRevisionID, _ := updated["revision"]["revision_id"].(string)
	if newHeadRevisionID == "" || newHeadRevisionID == headRevisionID {
		t.Fatalf("unexpected new revision id: old=%q new=%q", headRevisionID, newHeadRevisionID)
	}

	updateContentHash, _ := updated["revision"]["content_hash"].(string)
	if updateContentHash == "" {
		t.Fatal("expected content_hash in updated revision")
	}
	if updateContentHash != sha256Hex([]byte("second text")) {
		t.Fatalf("content_hash mismatch on update: got %q", updateContentHash)
	}
	updateRevisionHash, _ := updated["revision"]["revision_hash"].(string)
	if updateRevisionHash == "" {
		t.Fatal("expected revision_hash in updated revision")
	}
	if updateRevisionHash == createRevisionHash {
		t.Fatal("revision_hash should differ between revisions")
	}

	staleResp := requestJSONExpectStatus(t, http.MethodPatch, h.baseURL+"/docs/doc-1", `{
		"actor_id":"actor-1",
		"if_base_revision":"`+headRevisionID+`",
		"content":"stale write",
		"content_type":"text"
	}`, http.StatusConflict)
	defer staleResp.Body.Close()

	historyResp, err := http.Get(h.baseURL + "/docs/doc-1/history")
	if err != nil {
		t.Fatalf("GET /docs/{document_id}/history: %v", err)
	}
	defer historyResp.Body.Close()
	if historyResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected history status: got %d", historyResp.StatusCode)
	}
	var historyPayload map[string]any
	if err := json.NewDecoder(historyResp.Body).Decode(&historyPayload); err != nil {
		t.Fatalf("decode history response: %v", err)
	}
	revisions, _ := historyPayload["revisions"].([]any)
	if len(revisions) != 2 {
		t.Fatalf("expected two revisions in history, got %d payload=%#v", len(revisions), historyPayload)
	}

	revisionResp, err := http.Get(h.baseURL + "/docs/doc-1/revisions/" + headRevisionID)
	if err != nil {
		t.Fatalf("GET /docs/{document_id}/revisions/{revision_id}: %v", err)
	}
	defer revisionResp.Body.Close()
	if revisionResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected revision status: got %d", revisionResp.StatusCode)
	}
	var revisionPayload map[string]map[string]any
	if err := json.NewDecoder(revisionResp.Body).Decode(&revisionPayload); err != nil {
		t.Fatalf("decode revision response: %v", err)
	}
	if revisionPayload["revision"]["content"] != "initial text" {
		t.Fatalf("unexpected revision content: %#v", revisionPayload["revision"]["content"])
	}
	loadedRevisionHash, _ := revisionPayload["revision"]["revision_hash"].(string)
	if loadedRevisionHash != createRevisionHash {
		t.Fatalf("revision_hash mismatch on GET revision: got %q want %q", loadedRevisionHash, createRevisionHash)
	}
}

func TestDocumentCreateRequestKeyReplaysSingleWrite(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"id":"thread-docs",
			"title":"Replay docs thread",
			"type":"incident",
			"status":"active",
			"priority":"p2",
			"tags":["docs"],
			"cadence":"daily",
			"next_check_in_at":"2026-03-05T00:00:00Z",
			"current_summary":"summary",
			"next_actions":["review"],
			"key_artifacts":[],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated).Body.Close()

	body := `{
		"actor_id":"actor-1",
		"request_key":"replay-doc",
		"document":{"thread_id":"thread-docs","title":"Replay-safe doc","labels":["governance"]},
		"refs":["thread:thread-docs"],
		"content":"initial text",
		"content_type":"text"
	}`

	firstResp := postJSONExpectStatus(t, h.baseURL+"/docs", body, http.StatusCreated)
	defer firstResp.Body.Close()
	secondResp := postJSONExpectStatus(t, h.baseURL+"/docs", body, http.StatusCreated)
	defer secondResp.Body.Close()

	var firstPayload struct {
		Document map[string]any `json:"document"`
		Revision map[string]any `json:"revision"`
	}
	if err := json.NewDecoder(firstResp.Body).Decode(&firstPayload); err != nil {
		t.Fatalf("decode first doc create response: %v", err)
	}
	var secondPayload struct {
		Document map[string]any `json:"document"`
		Revision map[string]any `json:"revision"`
	}
	if err := json.NewDecoder(secondResp.Body).Decode(&secondPayload); err != nil {
		t.Fatalf("decode second doc create response: %v", err)
	}

	documentID, _ := firstPayload.Document["id"].(string)
	if documentID == "" {
		t.Fatal("expected server-issued document id")
	}
	if secondPayload.Document["id"] != documentID {
		t.Fatalf("expected replayed document id %q, got %#v", documentID, secondPayload.Document["id"])
	}
	if secondPayload.Revision["revision_id"] != firstPayload.Revision["revision_id"] {
		t.Fatalf("expected replayed revision id %#v, got %#v", firstPayload.Revision["revision_id"], secondPayload.Revision["revision_id"])
	}

	listResp, err := http.Get(h.baseURL + "/docs?thread_id=thread-docs")
	if err != nil {
		t.Fatalf("GET /docs: %v", err)
	}
	defer listResp.Body.Close()
	var listed struct {
		Documents []map[string]any `json:"documents"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode docs list: %v", err)
	}
	if len(listed.Documents) != 1 {
		t.Fatalf("expected one document after replay, got %d", len(listed.Documents))
	}

	timelineResp, err := http.Get(h.baseURL + "/threads/thread-docs/timeline")
	if err != nil {
		t.Fatalf("GET /threads/{id}/timeline: %v", err)
	}
	defer timelineResp.Body.Close()
	if timelineResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected timeline status: %d", timelineResp.StatusCode)
	}
	var timeline struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(timelineResp.Body).Decode(&timeline); err != nil {
		t.Fatalf("decode timeline: %v", err)
	}
	if countEventsOfType(timeline.Events, "document_created") != 1 {
		t.Fatalf("expected one document_created event, got %d", countEventsOfType(timeline.Events, "document_created"))
	}
}

func TestEventCreateRequestKeyReplaysSingleWrite(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	body := `{
		"actor_id":"actor-1",
		"request_key":"replay-event",
		"event":{
			"type":"my_custom_event",
			"thread_id":"thread-1",
			"refs":["customprefix:abc"],
			"summary":"custom event",
			"payload":{"x":1},
			"provenance":{"sources":["inferred"]}
		}
	}`

	firstResp := postJSONExpectStatus(t, h.baseURL+"/events", body, http.StatusCreated)
	defer firstResp.Body.Close()
	secondResp := postJSONExpectStatus(t, h.baseURL+"/events", body, http.StatusCreated)
	defer secondResp.Body.Close()

	var firstPayload map[string]map[string]any
	if err := json.NewDecoder(firstResp.Body).Decode(&firstPayload); err != nil {
		t.Fatalf("decode first event create response: %v", err)
	}
	var secondPayload map[string]map[string]any
	if err := json.NewDecoder(secondResp.Body).Decode(&secondPayload); err != nil {
		t.Fatalf("decode second event create response: %v", err)
	}
	if secondPayload["event"]["id"] != firstPayload["event"]["id"] {
		t.Fatalf("expected replayed event id %#v, got %#v", firstPayload["event"]["id"], secondPayload["event"]["id"])
	}

	eventID, _ := firstPayload["event"]["id"].(string)
	getEventResp, err := http.Get(h.baseURL + "/events/" + eventID)
	if err != nil {
		t.Fatalf("GET /events/{id}: %v", err)
	}
	defer getEventResp.Body.Close()
	if getEventResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected GET /events/{id} status: %d", getEventResp.StatusCode)
	}
}

func countEventsOfType(events []map[string]any, eventType string) int {
	count := 0
	for _, event := range events {
		if asString(event["type"]) == eventType {
			count++
		}
	}
	return count
}

func TestArtifactContentDeduplication(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	resp1 := postJSONExpectStatus(t, h.baseURL+"/artifacts", `{
		"actor_id":"actor-1",
		"artifact":{"kind":"evidence","refs":["thread:t1"]},
		"content":"identical content",
		"content_type":"text"
	}`, http.StatusCreated)
	defer resp1.Body.Close()
	var art1 map[string]map[string]any
	if err := json.NewDecoder(resp1.Body).Decode(&art1); err != nil {
		t.Fatalf("decode artifact 1: %v", err)
	}

	resp2 := postJSONExpectStatus(t, h.baseURL+"/artifacts", `{
		"actor_id":"actor-1",
		"artifact":{"kind":"evidence","refs":["thread:t2"]},
		"content":"identical content",
		"content_type":"text"
	}`, http.StatusCreated)
	defer resp2.Body.Close()
	var art2 map[string]map[string]any
	if err := json.NewDecoder(resp2.Body).Decode(&art2); err != nil {
		t.Fatalf("decode artifact 2: %v", err)
	}

	id1, _ := art1["artifact"]["id"].(string)
	id2, _ := art2["artifact"]["id"].(string)
	if id1 == id2 {
		t.Fatal("two artifacts with identical content should have different UUIDs")
	}

	hash1, _ := art1["artifact"]["content_hash"].(string)
	hash2, _ := art2["artifact"]["content_hash"].(string)
	if hash1 == "" || hash2 == "" {
		t.Fatal("expected content_hash on both artifacts")
	}
	if hash1 != hash2 {
		t.Fatalf("identical content should produce identical content_hash: %q vs %q", hash1, hash2)
	}
	if _, ok := art1["artifact"]["content_path"]; ok {
		t.Fatalf("expected first artifact response to omit content_path: %#v", art1["artifact"])
	}
	if _, ok := art2["artifact"]["content_path"]; ok {
		t.Fatalf("expected second artifact response to omit content_path: %#v", art2["artifact"])
	}

	entries, err := os.ReadDir(h.workspace.Layout().ArtifactContentDir)
	if err != nil {
		t.Fatalf("read artifact content dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 content file (dedup), got %d", len(entries))
	}
}

func TestLegacyContentPathIsStrippedFromArtifactAndRevisionResponses(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)

	artifactContent := []byte("legacy artifact body")
	artifactHash := sha256Hex(artifactContent)
	if err := os.WriteFile(filepath.Join(h.workspace.Layout().ArtifactContentDir, artifactHash), artifactContent, 0o644); err != nil {
		t.Fatalf("write legacy artifact body: %v", err)
	}

	revisionContent := []byte("legacy revision body")
	revisionHash := sha256Hex(revisionContent)
	if err := os.WriteFile(filepath.Join(h.workspace.Layout().ArtifactContentDir, revisionHash), revisionContent, 0o644); err != nil {
		t.Fatalf("write legacy revision body: %v", err)
	}

	legacyArtifactMetadata, err := json.Marshal(map[string]any{
		"id":           "artifact-legacy-http",
		"kind":         "evidence",
		"created_at":   "2026-03-04T10:00:00Z",
		"created_by":   "actor-1",
		"content_type": "text",
		"content_hash": artifactHash,
		"content_path": filepath.Join(h.workspace.Layout().ArtifactContentDir, artifactHash),
		"refs":         []string{"thread:thread-legacy-http"},
	})
	if err != nil {
		t.Fatalf("marshal legacy artifact metadata: %v", err)
	}
	legacyRevisionMetadata, err := json.Marshal(map[string]any{
		"id":               "artifact-doc-legacy-http",
		"kind":             "doc",
		"created_at":       "2026-03-04T11:00:00Z",
		"created_by":       "actor-1",
		"content_type":     "text",
		"content_hash":     revisionHash,
		"content_path":     filepath.Join(h.workspace.Layout().ArtifactContentDir, revisionHash),
		"refs":             []string{"thread:thread-legacy-http"},
		"document_id":      "legacy-doc-http",
		"revision_id":      "rev-legacy-http",
		"revision_number":  1,
		"prev_revision_id": nil,
	})
	if err != nil {
		t.Fatalf("marshal legacy revision metadata: %v", err)
	}

	if _, err := h.workspace.DB().ExecContext(
		context.Background(),
		`INSERT INTO artifacts(id, kind, thread_id, created_at, created_by, content_type, content_hash, refs_json, metadata_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"artifact-legacy-http",
		"evidence",
		"thread-legacy-http",
		"2026-03-04T10:00:00Z",
		"actor-1",
		"text",
		artifactHash,
		`["thread:thread-legacy-http"]`,
		string(legacyArtifactMetadata),
	); err != nil {
		t.Fatalf("insert legacy artifact row: %v", err)
	}
	if _, err := h.workspace.DB().ExecContext(
		context.Background(),
		`INSERT INTO artifacts(id, kind, thread_id, created_at, created_by, content_type, content_hash, refs_json, metadata_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"artifact-doc-legacy-http",
		"doc",
		"thread-legacy-http",
		"2026-03-04T11:00:00Z",
		"actor-1",
		"text",
		revisionHash,
		`["thread:thread-legacy-http"]`,
		string(legacyRevisionMetadata),
	); err != nil {
		t.Fatalf("insert legacy doc artifact row: %v", err)
	}
	if _, err := h.workspace.DB().ExecContext(
		context.Background(),
		`INSERT INTO document_revisions(
			revision_id, document_id, revision_number, prev_revision_id, artifact_id, thread_id, refs_json, revision_hash, created_at, created_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"rev-legacy-http",
		"legacy-doc-http",
		1,
		nil,
		"artifact-doc-legacy-http",
		"thread-legacy-http",
		`["thread:thread-legacy-http"]`,
		"legacy-chain-hash",
		"2026-03-04T11:00:00Z",
		"actor-1",
	); err != nil {
		t.Fatalf("insert legacy document revision row: %v", err)
	}

	artifactResp, err := http.Get(h.baseURL + "/artifacts/artifact-legacy-http")
	if err != nil {
		t.Fatalf("GET legacy artifact: %v", err)
	}
	defer artifactResp.Body.Close()
	if artifactResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected legacy artifact status: %d", artifactResp.StatusCode)
	}
	var artifactPayload map[string]map[string]any
	if err := json.NewDecoder(artifactResp.Body).Decode(&artifactPayload); err != nil {
		t.Fatalf("decode legacy artifact payload: %v", err)
	}
	if _, ok := artifactPayload["artifact"]["content_path"]; ok {
		t.Fatalf("expected legacy artifact response to omit content_path: %#v", artifactPayload["artifact"])
	}

	revisionResp, err := http.Get(h.baseURL + "/docs/legacy-doc-http/revisions/rev-legacy-http")
	if err != nil {
		t.Fatalf("GET legacy revision: %v", err)
	}
	defer revisionResp.Body.Close()
	if revisionResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected legacy revision status: %d", revisionResp.StatusCode)
	}
	var revisionPayload map[string]map[string]any
	if err := json.NewDecoder(revisionResp.Body).Decode(&revisionPayload); err != nil {
		t.Fatalf("decode legacy revision payload: %v", err)
	}
	revisionArtifact, ok := revisionPayload["revision"]["artifact"].(map[string]any)
	if !ok {
		t.Fatalf("expected legacy revision artifact metadata map, got %#v", revisionPayload["revision"]["artifact"])
	}
	if _, ok := revisionArtifact["content_path"]; ok {
		t.Fatalf("expected legacy revision response to omit content_path: %#v", revisionArtifact)
	}
}

func TestDocumentRevisionMerkleChainIntegrity(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/docs", `{
		"actor_id":"actor-1",
		"document":{"id":"merkle-doc","title":"Merkle Test"},
		"content":"revision one",
		"content_type":"text"
	}`, http.StatusCreated)
	defer createResp.Body.Close()
	var created map[string]map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	rev1ID, _ := created["revision"]["revision_id"].(string)

	updateResp1 := requestJSONExpectStatus(t, http.MethodPatch, h.baseURL+"/docs/merkle-doc", `{
		"actor_id":"actor-1",
		"if_base_revision":"`+rev1ID+`",
		"content":"revision two",
		"content_type":"text"
	}`, http.StatusOK)
	defer updateResp1.Body.Close()
	var updated1 map[string]map[string]any
	if err := json.NewDecoder(updateResp1.Body).Decode(&updated1); err != nil {
		t.Fatalf("decode update 1: %v", err)
	}
	rev2ID, _ := updated1["revision"]["revision_id"].(string)

	updateResp2 := requestJSONExpectStatus(t, http.MethodPatch, h.baseURL+"/docs/merkle-doc", `{
		"actor_id":"actor-1",
		"if_base_revision":"`+rev2ID+`",
		"content":"revision three",
		"content_type":"text"
	}`, http.StatusOK)
	defer updateResp2.Body.Close()

	historyResp, err := http.Get(h.baseURL + "/docs/merkle-doc/history")
	if err != nil {
		t.Fatalf("GET history: %v", err)
	}
	defer historyResp.Body.Close()
	var historyPayload map[string]any
	if err := json.NewDecoder(historyResp.Body).Decode(&historyPayload); err != nil {
		t.Fatalf("decode history: %v", err)
	}
	revisions, _ := historyPayload["revisions"].([]any)
	if len(revisions) != 3 {
		t.Fatalf("expected 3 revisions, got %d", len(revisions))
	}

	contents := []string{"revision one", "revision two", "revision three"}
	prevHash := ""
	for i, rawRev := range revisions {
		rev, ok := rawRev.(map[string]any)
		if !ok {
			t.Fatalf("revision %d is not a map", i)
		}
		revID, _ := rev["revision_id"].(string)

		revResp, err := http.Get(h.baseURL + "/docs/merkle-doc/revisions/" + revID)
		if err != nil {
			t.Fatalf("GET revision %d: %v", i, err)
		}
		defer revResp.Body.Close()
		var revPayload map[string]map[string]any
		if err := json.NewDecoder(revResp.Body).Decode(&revPayload); err != nil {
			t.Fatalf("decode revision %d: %v", i, err)
		}

		revisionHash, _ := revPayload["revision"]["revision_hash"].(string)
		if revisionHash == "" {
			t.Fatalf("revision %d missing revision_hash", i)
		}

		contentHash := sha256Hex([]byte(contents[i]))
		revNum := i + 1
		createdAt, _ := revPayload["revision"]["created_at"].(string)
		createdBy, _ := revPayload["revision"]["created_by"].(string)
		expectedHash := testComputeRevisionHash(contentHash, prevHash, "merkle-doc", revNum, createdAt, createdBy)

		if revisionHash != expectedHash {
			t.Fatalf("revision %d hash mismatch: got %q want %q (contentHash=%q prevHash=%q)", i, revisionHash, expectedHash, contentHash, prevHash)
		}

		prevHash = revisionHash
	}
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func testComputeRevisionHash(contentHash, prevRevisionHash, documentID string, revisionNumber int, createdAt, createdBy string) string {
	h := sha256.New()
	fmt.Fprintf(h, "content_hash:%s\n", contentHash)
	fmt.Fprintf(h, "prev_revision_hash:%s\n", prevRevisionHash)
	fmt.Fprintf(h, "document_id:%s\n", documentID)
	fmt.Fprintf(h, "revision_number:%d\n", revisionNumber)
	fmt.Fprintf(h, "created_at:%s\n", createdAt)
	fmt.Fprintf(h, "created_by:%s\n", createdBy)
	return hex.EncodeToString(h.Sum(nil))
}

func TestDocumentsInvalidInputReturnsInvalidRequest(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createInvalidResp := postJSONExpectStatus(t, h.baseURL+"/docs", `{
		"actor_id":"actor-1",
		"document":{"id":"doc-invalid-create","labels":["ok",1]},
		"content":"invalid",
		"content_type":"text"
	}`, http.StatusBadRequest)
	defer createInvalidResp.Body.Close()
	assertErrorCode(t, createInvalidResp, "invalid_request")

	createResp := postJSONExpectStatus(t, h.baseURL+"/docs", `{
		"actor_id":"actor-1",
		"document":{"id":"doc-invalid-update"},
		"content":"initial",
		"content_type":"text"
	}`, http.StatusCreated)
	defer createResp.Body.Close()

	var created map[string]map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	baseRevision, _ := created["revision"]["revision_id"].(string)
	if strings.TrimSpace(baseRevision) == "" {
		t.Fatalf("expected revision_id in create response: %#v", created)
	}

	updateInvalidResp := requestJSONExpectStatus(t, http.MethodPatch, h.baseURL+"/docs/doc-invalid-update", `{
		"actor_id":"actor-1",
		"if_base_revision":"`+baseRevision+`",
		"document":{"id":"should-not-be-allowed"},
		"content":"next",
		"content_type":"text"
	}`, http.StatusBadRequest)
	defer updateInvalidResp.Body.Close()
	assertErrorCode(t, updateInvalidResp, "invalid_request")
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
		`INSERT INTO threads(id, kind, thread_id, updated_at, updated_by, body_json, provenance_json)
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
	workspace        *storage.Workspace
	baseURL          string
	maintainer       *ProjectionMaintainer
	humanAccessToken string
}

func newPrimitivesTestServer(t *testing.T) primitivesTestHarness {
	t.Helper()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	contractPath := filepath.Join("..", "..", "..", "contracts", "oar-schema.yaml")
	contract, err := schema.Load(contractPath)
	if err != nil {
		_ = workspace.Close()
		t.Fatalf("load schema contract: %v", err)
	}

	registry := actors.NewStore(workspace.DB())
	primitiveStore := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	maintainer := NewProjectionMaintainer(ProjectionMaintainerConfig{
		PrimitiveStore:   primitiveStore,
		Contract:         contract,
		InboxRiskHorizon: defaultInboxRiskHorizon,
		DirtyBatchSize:   100,
		SystemActorID:    "oar-core",
	})
	handler := NewHandler(
		contract.Version,
		WithHealthCheck(workspace.Ping),
		WithActorRegistry(registry),
		WithPrimitiveStore(primitiveStore),
		WithSchemaContract(contract),
		WithProjectionMaintainer(maintainer),
		WithAllowUnauthenticatedWrites(true),
		WithEnableDevActorMode(true),
	)
	server := httptest.NewServer(newProjectionMaintainerAutoStepHandler(handler, maintainer))
	t.Cleanup(func() {
		server.Close()
		_ = workspace.Close()
	})

	return primitivesTestHarness{workspace: workspace, baseURL: server.URL, maintainer: maintainer, humanAccessToken: ""}
}

func newPrimitivesTestServerWithHumanPrincipal(t *testing.T) primitivesTestHarness {
	t.Helper()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}
	contractPath := filepath.Join("..", "..", "..", "contracts", "oar-schema.yaml")
	contract, err := schema.Load(contractPath)
	if err != nil {
		_ = workspace.Close()
		t.Fatalf("load schema contract: %v", err)
	}

	registry := actors.NewStore(workspace.DB())
	primitiveStore := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)
	authStore := auth.NewStore(workspace.DB())
	passkeySessionStore := auth.NewPasskeySessionStore(auth.DefaultPasskeySessionTTL)
	maintainer := NewProjectionMaintainer(ProjectionMaintainerConfig{
		PrimitiveStore:   primitiveStore,
		Contract:         contract,
		InboxRiskHorizon: defaultInboxRiskHorizon,
		DirtyBatchSize:   100,
		SystemActorID:    "oar-core",
	})
	humanToken := "human-primitives-purge-token-test-ok-32"
	seedHumanPrincipalForLockoutTest(t, context.Background(), workspace.DB(), "human-purge-principal-agent", "human-purge-principal-actor", "human.purge.primitives.test", humanToken)

	handler := NewHandler(
		contract.Version,
		WithHealthCheck(workspace.Ping),
		WithActorRegistry(registry),
		WithPrimitiveStore(primitiveStore),
		WithSchemaContract(contract),
		WithProjectionMaintainer(maintainer),
		WithAuthStore(authStore),
		WithPasskeySessionStore(passkeySessionStore),
		WithAllowUnauthenticatedWrites(true),
		WithEnableDevActorMode(true),
	)
	server := httptest.NewServer(newProjectionMaintainerAutoStepHandler(handler, maintainer))
	t.Cleanup(func() {
		server.Close()
		passkeySessionStore.Close()
		_ = workspace.Close()
	})

	return primitivesTestHarness{
		workspace:        workspace,
		baseURL:          server.URL,
		maintainer:       maintainer,
		humanAccessToken: humanToken,
	}
}

func newProjectionMaintainerAutoStepHandler(inner http.Handler, maintainer *ProjectionMaintainer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := httptest.NewRecorder()
		inner.ServeHTTP(recorder, r)

		if shouldAutoStepProjectionMaintainer(r, recorder.Code) && maintainer != nil {
			_ = maintainer.Step(context.Background(), time.Now().UTC())
		}

		for key, values := range recorder.Header() {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(recorder.Code)
		_, _ = w.Write(recorder.Body.Bytes())
	})
}

func shouldAutoStepProjectionMaintainer(r *http.Request, statusCode int) bool {
	if statusCode >= http.StatusBadRequest {
		return false
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
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

func postJSONExpectStatusBearer(t *testing.T, url string, body string, bearerToken string, expectedStatus int) *http.Response {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s create request: %v", url, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(bearerToken) != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}

	resp, err := http.DefaultClient.Do(req)
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

func requestJSONExpectStatus(t *testing.T, method string, url string, body string, expectedStatus int) *http.Response {
	t.Helper()

	req, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("%s %s create request: %v", method, url, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, url, err)
	}
	if resp.StatusCode != expectedStatus {
		defer resp.Body.Close()
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s %s unexpected status: got %d want %d body=%s", method, url, resp.StatusCode, expectedStatus, string(bodyBytes))
	}
	return resp
}

func TestArtifactTombstoneLifecycle(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/artifacts", `{
		"actor_id":"actor-1",
		"artifact":{
			"kind":"blob",
			"refs":["thread:thread-1"],
			"summary":"tombstone test"
		},
		"content":"tombstone test content",
		"content_type":"text"
	}`, http.StatusCreated)
	defer createResp.Body.Close()

	var created map[string]map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create artifact response: %v", err)
	}
	artifactID, _ := created["artifact"]["id"].(string)
	if artifactID == "" {
		t.Fatal("expected created artifact id")
	}

	listResp, err := http.Get(h.baseURL + "/artifacts")
	if err != nil {
		t.Fatalf("GET /artifacts: %v", err)
	}
	defer listResp.Body.Close()
	var listed map[string][]map[string]any
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	found := false
	for _, a := range listed["artifacts"] {
		if a["id"] == artifactID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected artifact in list before tombstone")
	}

	tombstoneResp := postJSONExpectStatus(t, h.baseURL+"/artifacts/"+artifactID+"/tombstone", `{
		"actor_id":"actor-1",
		"reason":"no longer needed"
	}`, http.StatusOK)
	defer tombstoneResp.Body.Close()

	var tombstoned map[string]map[string]any
	if err := json.NewDecoder(tombstoneResp.Body).Decode(&tombstoned); err != nil {
		t.Fatalf("decode tombstone response: %v", err)
	}
	if tombstoned["artifact"]["tombstoned_at"] == nil {
		t.Fatal("expected tombstoned_at to be set")
	}
	if tombstoned["artifact"]["tombstoned_by"] != "actor-1" {
		t.Fatalf("expected tombstoned_by=actor-1, got %v", tombstoned["artifact"]["tombstoned_by"])
	}
	if tombstoned["artifact"]["tombstone_reason"] != "no longer needed" {
		t.Fatalf("expected tombstone_reason='no longer needed', got %v", tombstoned["artifact"]["tombstone_reason"])
	}

	filteredResp, err := http.Get(h.baseURL + "/artifacts")
	if err != nil {
		t.Fatalf("GET /artifacts after tombstone: %v", err)
	}
	defer filteredResp.Body.Close()
	var filtered map[string][]map[string]any
	if err := json.NewDecoder(filteredResp.Body).Decode(&filtered); err != nil {
		t.Fatalf("decode filtered list response: %v", err)
	}
	for _, a := range filtered["artifacts"] {
		if a["id"] == artifactID {
			t.Fatal("tombstoned artifact should not appear in default list")
		}
	}

	withTombstonedResp, err := http.Get(h.baseURL + "/artifacts?include_tombstoned=true")
	if err != nil {
		t.Fatalf("GET /artifacts?include_tombstoned=true: %v", err)
	}
	defer withTombstonedResp.Body.Close()
	var withTombstoned map[string][]map[string]any
	if err := json.NewDecoder(withTombstonedResp.Body).Decode(&withTombstoned); err != nil {
		t.Fatalf("decode include_tombstoned list response: %v", err)
	}
	found = false
	for _, a := range withTombstoned["artifacts"] {
		if a["id"] == artifactID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected tombstoned artifact in list with include_tombstoned=true")
	}

	getResp, err := http.Get(h.baseURL + "/artifacts/" + artifactID)
	if err != nil {
		t.Fatalf("GET /artifacts/{id} after tombstone: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for direct get of tombstoned artifact, got %d", getResp.StatusCode)
	}
	var got map[string]map[string]any
	if err := json.NewDecoder(getResp.Body).Decode(&got); err != nil {
		t.Fatalf("decode get response: %v", err)
	}
	if got["artifact"]["tombstoned_at"] == nil {
		t.Fatal("expected tombstoned_at in direct get")
	}

	reTombstoneResp := postJSONExpectStatus(t, h.baseURL+"/artifacts/"+artifactID+"/tombstone", `{
		"actor_id":"actor-1",
		"reason":"still not needed"
	}`, http.StatusOK)
	defer reTombstoneResp.Body.Close()
	var reTombstoned map[string]map[string]any
	if err := json.NewDecoder(reTombstoneResp.Body).Decode(&reTombstoned); err != nil {
		t.Fatalf("decode repeat tombstone response: %v", err)
	}
	if reTombstoned["artifact"]["tombstoned_at"] != tombstoned["artifact"]["tombstoned_at"] {
		t.Fatalf("expected repeated tombstone to preserve tombstoned_at, first=%v second=%v", tombstoned["artifact"]["tombstoned_at"], reTombstoned["artifact"]["tombstoned_at"])
	}
	if reTombstoned["artifact"]["tombstone_reason"] != tombstoned["artifact"]["tombstone_reason"] {
		t.Fatalf("expected repeated tombstone to preserve tombstone_reason, first=%v second=%v", tombstoned["artifact"]["tombstone_reason"], reTombstoned["artifact"]["tombstone_reason"])
	}
}

func TestArtifactRestoreLifecycle(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/artifacts", `{
		"actor_id":"actor-1",
		"artifact":{
			"kind":"blob",
			"refs":["thread:thread-1"],
			"summary":"restore test"
		},
		"content":"restore test content",
		"content_type":"text"
	}`, http.StatusCreated)
	defer createResp.Body.Close()

	var created map[string]map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create artifact response: %v", err)
	}
	artifactID, _ := created["artifact"]["id"].(string)
	if artifactID == "" {
		t.Fatal("expected created artifact id")
	}

	postJSONExpectStatus(t, h.baseURL+"/artifacts/"+artifactID+"/tombstone", `{
		"actor_id":"actor-1",
		"reason":"tmp"
	}`, http.StatusOK).Body.Close()

	restoreResp := postJSONExpectStatus(t, h.baseURL+"/artifacts/"+artifactID+"/restore", `{"actor_id":"actor-1"}`, http.StatusOK)
	defer restoreResp.Body.Close()

	var restored map[string]map[string]any
	if err := json.NewDecoder(restoreResp.Body).Decode(&restored); err != nil {
		t.Fatalf("decode restore response: %v", err)
	}
	if restored["artifact"]["tombstoned_at"] != nil {
		t.Fatalf("expected tombstoned_at cleared, got %#v", restored["artifact"]["tombstoned_at"])
	}
	if restored["artifact"]["tombstoned_by"] != nil {
		t.Fatalf("expected tombstoned_by cleared, got %#v", restored["artifact"]["tombstoned_by"])
	}
	if restored["artifact"]["tombstone_reason"] != nil {
		t.Fatalf("expected tombstone_reason cleared, got %#v", restored["artifact"]["tombstone_reason"])
	}

	reRestoreResp, err := http.Post(h.baseURL+"/artifacts/"+artifactID+"/restore", "application/json", strings.NewReader(`{"actor_id":"actor-1"}`))
	if err != nil {
		t.Fatalf("POST restore again: %v", err)
	}
	defer reRestoreResp.Body.Close()
	if reRestoreResp.StatusCode != http.StatusConflict {
		bodyBytes, _ := io.ReadAll(reRestoreResp.Body)
		t.Fatalf("expected 409 on second restore, got %d body=%s", reRestoreResp.StatusCode, string(bodyBytes))
	}

	listResp, err := http.Get(h.baseURL + "/artifacts")
	if err != nil {
		t.Fatalf("GET /artifacts: %v", err)
	}
	defer listResp.Body.Close()
	var listed map[string][]map[string]any
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	found := false
	for _, a := range listed["artifacts"] {
		if a["id"] == artifactID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected restored artifact in default list")
	}
}

func TestArtifactPurgeLifecycle(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServerWithHumanPrincipal(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/artifacts", `{
		"actor_id":"actor-1",
		"artifact":{
			"kind":"blob",
			"refs":["thread:thread-1"],
			"summary":"purge test"
		},
		"content":"purge test content bytes",
		"content_type":"text"
	}`, http.StatusCreated)
	defer createResp.Body.Close()

	var created map[string]map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create artifact response: %v", err)
	}
	artifactID, _ := created["artifact"]["id"].(string)
	if artifactID == "" {
		t.Fatal("expected created artifact id")
	}

	postJSONExpectStatus(t, h.baseURL+"/artifacts/"+artifactID+"/tombstone", `{
		"actor_id":"actor-1",
		"reason":"gone"
	}`, http.StatusOK).Body.Close()

	purgeResp := postJSONExpectStatusBearer(t, h.baseURL+"/artifacts/"+artifactID+"/purge", `{}`, h.humanAccessToken, http.StatusOK)
	defer purgeResp.Body.Close()

	var purged map[string]any
	if err := json.NewDecoder(purgeResp.Body).Decode(&purged); err != nil {
		t.Fatalf("decode purge response: %v", err)
	}
	if purged["purged"] != true {
		t.Fatalf("expected purged true, got %#v", purged["purged"])
	}
	if purged["artifact_id"] != artifactID {
		t.Fatalf("expected artifact_id %q, got %#v", artifactID, purged["artifact_id"])
	}

	getResp, err := http.Get(h.baseURL + "/artifacts/" + artifactID)
	if err != nil {
		t.Fatalf("GET artifact after purge: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 after purge, got %d", getResp.StatusCode)
	}

	listResp, err := http.Get(h.baseURL + "/artifacts?include_tombstoned=true")
	if err != nil {
		t.Fatalf("GET /artifacts: %v", err)
	}
	defer listResp.Body.Close()
	var listed map[string][]map[string]any
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	for _, a := range listed["artifacts"] {
		if a["id"] == artifactID {
			t.Fatal("purged artifact must not appear in list")
		}
	}
}

func TestArtifactPurgeUnauthenticatedDevHumanTaggedActor(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-human-dev","display_name":"Human Dev","created_at":"2026-03-04T10:00:00Z","tags":["human"]}}`, http.StatusCreated)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-bot-dev","display_name":"Bot Dev","created_at":"2026-03-04T10:01:00Z","tags":["agent"]}}`, http.StatusCreated)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:02:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/artifacts", `{
		"actor_id":"actor-1",
		"artifact":{"kind":"blob","refs":["thread:thread-1"],"summary":"dev purge"},
		"content":"x",
		"content_type":"text"
	}`, http.StatusCreated)
	defer createResp.Body.Close()

	var created map[string]map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create artifact response: %v", err)
	}
	artifactID, _ := created["artifact"]["id"].(string)
	if artifactID == "" {
		t.Fatal("expected created artifact id")
	}

	postJSONExpectStatus(t, h.baseURL+"/artifacts/"+artifactID+"/tombstone", `{
		"actor_id":"actor-1",
		"reason":"dev purge prep"
	}`, http.StatusOK).Body.Close()

	purgeResp := postJSONExpectStatus(t, h.baseURL+"/artifacts/"+artifactID+"/purge", `{"actor_id":"actor-human-dev"}`, http.StatusOK)
	defer purgeResp.Body.Close()

	var purged map[string]any
	if err := json.NewDecoder(purgeResp.Body).Decode(&purged); err != nil {
		t.Fatalf("decode purge response: %v", err)
	}
	if purged["purged"] != true {
		t.Fatalf("expected purged true, got %#v", purged["purged"])
	}
}

func TestArtifactPurgeUnauthenticatedDevRejectsNonHumanTag(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-bot-dev","display_name":"Bot Dev","created_at":"2026-03-04T10:00:00Z","tags":["agent"]}}`, http.StatusCreated)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:01:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/artifacts", `{
		"actor_id":"actor-1",
		"artifact":{"kind":"blob","refs":["thread:thread-1"],"summary":"reject purge"},
		"content":"y",
		"content_type":"text"
	}`, http.StatusCreated)
	defer createResp.Body.Close()

	var created map[string]map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create artifact response: %v", err)
	}
	artifactID, _ := created["artifact"]["id"].(string)
	if artifactID == "" {
		t.Fatal("expected created artifact id")
	}

	postJSONExpectStatus(t, h.baseURL+"/artifacts/"+artifactID+"/tombstone", `{
		"actor_id":"actor-1",
		"reason":"dev purge prep"
	}`, http.StatusOK).Body.Close()

	rejectResp := postJSONExpectStatus(t, h.baseURL+"/artifacts/"+artifactID+"/purge", `{"actor_id":"actor-bot-dev"}`, http.StatusForbidden)
	defer rejectResp.Body.Close()
	assertErrorCode(t, rejectResp, "human_only")
}

func TestArtifactPurgeNotTombstoned(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServerWithHumanPrincipal(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/artifacts", `{
		"actor_id":"actor-1",
		"artifact":{
			"kind":"blob",
			"refs":["thread:thread-1"],
			"summary":"live artifact"
		},
		"content":"x",
		"content_type":"text"
	}`, http.StatusCreated)
	defer createResp.Body.Close()

	var created map[string]map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create artifact response: %v", err)
	}
	artifactID, _ := created["artifact"]["id"].(string)
	if artifactID == "" {
		t.Fatal("expected created artifact id")
	}

	purgeResp := postJSONExpectStatusBearer(t, h.baseURL+"/artifacts/"+artifactID+"/purge", `{}`, h.humanAccessToken, http.StatusConflict)
	defer purgeResp.Body.Close()
	assertErrorCode(t, purgeResp, "not_tombstoned")
}

func TestArtifactTombstonedOnlyListFilter(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createA := postJSONExpectStatus(t, h.baseURL+"/artifacts", `{
		"actor_id":"actor-1",
		"artifact":{"kind":"blob","refs":["thread:thread-1"],"summary":"a"},
		"content":"a",
		"content_type":"text"
	}`, http.StatusCreated)
	defer createA.Body.Close()
	var payloadA map[string]map[string]any
	if err := json.NewDecoder(createA.Body).Decode(&payloadA); err != nil {
		t.Fatalf("decode artifact a: %v", err)
	}
	idA, _ := payloadA["artifact"]["id"].(string)

	createB := postJSONExpectStatus(t, h.baseURL+"/artifacts", `{
		"actor_id":"actor-1",
		"artifact":{"kind":"blob","refs":["thread:thread-1"],"summary":"b"},
		"content":"b",
		"content_type":"text"
	}`, http.StatusCreated)
	defer createB.Body.Close()
	var payloadB map[string]map[string]any
	if err := json.NewDecoder(createB.Body).Decode(&payloadB); err != nil {
		t.Fatalf("decode artifact b: %v", err)
	}
	idB, _ := payloadB["artifact"]["id"].(string)

	postJSONExpectStatus(t, h.baseURL+"/artifacts/"+idA+"/tombstone", `{"actor_id":"actor-1","reason":"x"}`, http.StatusOK).Body.Close()

	onlyResp, err := http.Get(h.baseURL + "/artifacts?tombstoned_only=true")
	if err != nil {
		t.Fatalf("GET tombstoned_only: %v", err)
	}
	defer onlyResp.Body.Close()
	var onlyListed map[string][]map[string]any
	if err := json.NewDecoder(onlyResp.Body).Decode(&onlyListed); err != nil {
		t.Fatalf("decode tombstoned_only list: %v", err)
	}
	if len(onlyListed["artifacts"]) != 1 {
		t.Fatalf("expected exactly one tombstoned artifact, got %d", len(onlyListed["artifacts"]))
	}
	if onlyListed["artifacts"][0]["id"] != idA {
		t.Fatalf("expected tombstoned id %s, got %#v", idA, onlyListed["artifacts"][0]["id"])
	}
	for _, a := range onlyListed["artifacts"] {
		if a["id"] == idB {
			t.Fatal("non-tombstoned artifact must not appear in tombstoned_only list")
		}
	}
}

func TestArtifactPurgeReferencedByDocRevision(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServerWithHumanPrincipal(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	docResp := postJSONExpectStatus(t, h.baseURL+"/docs", `{
		"actor_id":"actor-1",
		"document":{"title":"Doc With Revision Artifact","thread_id":"thread-1"},
		"content":"body",
		"content_type":"text"
	}`, http.StatusCreated)
	defer docResp.Body.Close()

	var docPayload struct {
		Revision map[string]any `json:"revision"`
	}
	if err := json.NewDecoder(docResp.Body).Decode(&docPayload); err != nil {
		t.Fatalf("decode create doc: %v", err)
	}
	artifactID, _ := docPayload.Revision["artifact_id"].(string)
	if artifactID == "" {
		t.Fatal("expected revision artifact_id")
	}

	postJSONExpectStatus(t, h.baseURL+"/artifacts/"+artifactID+"/tombstone", `{"actor_id":"actor-1","reason":"try purge"}`, http.StatusOK).Body.Close()

	conflictResp := postJSONExpectStatusBearer(t, h.baseURL+"/artifacts/"+artifactID+"/purge", `{}`, h.humanAccessToken, http.StatusConflict)
	defer conflictResp.Body.Close()
	assertErrorCode(t, conflictResp, "artifact_in_use")
}

func TestDocumentTombstoneLifecycle(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/docs", `{
		"actor_id":"actor-1",
		"document":{"title":"Tombstone Test Doc"},
		"content":"initial content",
		"content_type":"text"
	}`, http.StatusCreated)
	defer createResp.Body.Close()

	var created map[string]map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create doc response: %v", err)
	}
	documentID, _ := created["document"]["id"].(string)
	if documentID == "" {
		t.Fatal("expected created document id")
	}

	tombstoneResp := postJSONExpectStatus(t, h.baseURL+"/docs/"+documentID+"/tombstone", `{
		"actor_id":"actor-1",
		"reason":"replaced by v2"
	}`, http.StatusOK)
	defer tombstoneResp.Body.Close()

	var tombstonePayload struct {
		Document map[string]any `json:"document"`
		Revision map[string]any `json:"revision"`
	}
	if err := json.NewDecoder(tombstoneResp.Body).Decode(&tombstonePayload); err != nil {
		t.Fatalf("decode tombstone doc response: %v", err)
	}
	if tombstonePayload.Document["tombstoned_at"] == nil {
		t.Fatal("expected tombstoned_at to be set on document")
	}
	if tombstonePayload.Document["tombstoned_by"] != "actor-1" {
		t.Fatalf("expected tombstoned_by=actor-1, got %v", tombstonePayload.Document["tombstoned_by"])
	}
	if tombstonePayload.Document["tombstone_reason"] != "replaced by v2" {
		t.Fatalf("expected tombstone_reason='replaced by v2', got %v", tombstonePayload.Document["tombstone_reason"])
	}
	if tombstonePayload.Revision == nil {
		t.Fatal("expected revision to be returned")
	}

	getResp, err := http.Get(h.baseURL + "/docs/" + documentID)
	if err != nil {
		t.Fatalf("GET /docs/{id} after tombstone: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for direct get of tombstoned document, got %d", getResp.StatusCode)
	}
	var gotDoc struct {
		Document map[string]any `json:"document"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&gotDoc); err != nil {
		t.Fatalf("decode get doc response: %v", err)
	}
	if gotDoc.Document["tombstoned_at"] == nil {
		t.Fatal("expected tombstoned_at in direct get")
	}

	listResp, err := http.Get(h.baseURL + "/docs")
	if err != nil {
		t.Fatalf("GET /docs after tombstone: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected docs list status after tombstone: got %d", listResp.StatusCode)
	}
	var withoutTombstones struct {
		Documents []map[string]any `json:"documents"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&withoutTombstones); err != nil {
		t.Fatalf("decode docs list after tombstone: %v", err)
	}
	if len(withoutTombstones.Documents) != 0 {
		t.Fatalf("expected tombstoned document to be hidden by default, got %#v", withoutTombstones.Documents)
	}

	withTombstonesResp, err := http.Get(h.baseURL + "/docs?include_tombstoned=true")
	if err != nil {
		t.Fatalf("GET /docs?include_tombstoned=true: %v", err)
	}
	defer withTombstonesResp.Body.Close()
	if withTombstonesResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected docs list include_tombstoned status: got %d", withTombstonesResp.StatusCode)
	}
	var withTombstones struct {
		Documents []map[string]any `json:"documents"`
	}
	if err := json.NewDecoder(withTombstonesResp.Body).Decode(&withTombstones); err != nil {
		t.Fatalf("decode docs list include_tombstoned response: %v", err)
	}
	if len(withTombstones.Documents) != 1 {
		t.Fatalf("expected one tombstoned document in include_tombstoned list, got %d", len(withTombstones.Documents))
	}
	if withTombstones.Documents[0]["id"] != documentID {
		t.Fatalf("unexpected include_tombstoned document id: %#v", withTombstones.Documents[0]["id"])
	}
	if withTombstones.Documents[0]["tombstoned_at"] == nil {
		t.Fatalf("expected tombstoned document metadata in include_tombstoned list, got %#v", withTombstones.Documents[0])
	}

	reTombstoneResp := postJSONExpectStatus(t, h.baseURL+"/docs/"+documentID+"/tombstone", `{
		"actor_id":"actor-1",
		"reason":"still replaced"
	}`, http.StatusOK)
	defer reTombstoneResp.Body.Close()
	var repeatPayload struct {
		Document map[string]any `json:"document"`
	}
	if err := json.NewDecoder(reTombstoneResp.Body).Decode(&repeatPayload); err != nil {
		t.Fatalf("decode repeat tombstone doc response: %v", err)
	}
	if repeatPayload.Document["tombstoned_at"] != tombstonePayload.Document["tombstoned_at"] {
		t.Fatalf("expected repeated doc tombstone to preserve tombstoned_at, first=%v second=%v", tombstonePayload.Document["tombstoned_at"], repeatPayload.Document["tombstoned_at"])
	}
	if repeatPayload.Document["tombstone_reason"] != tombstonePayload.Document["tombstone_reason"] {
		t.Fatalf("expected repeated doc tombstone to preserve tombstone_reason, first=%v second=%v", tombstonePayload.Document["tombstone_reason"], repeatPayload.Document["tombstone_reason"])
	}
}

func TestArtifactArchiveLifecycle(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/artifacts", `{
		"actor_id":"actor-1",
		"artifact":{"kind":"blob","refs":["thread:thread-1"],"summary":"archive lifecycle"},
		"content":"body",
		"content_type":"text"
	}`, http.StatusCreated)
	defer createResp.Body.Close()

	var created map[string]map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create artifact: %v", err)
	}
	artifactID := asString(created["artifact"]["id"])
	if artifactID == "" {
		t.Fatal("expected artifact id")
	}

	archiveResp := postJSONExpectStatus(t, h.baseURL+"/artifacts/"+artifactID+"/archive", `{"actor_id":"actor-1"}`, http.StatusOK)
	defer archiveResp.Body.Close()
	var archived map[string]map[string]any
	if err := json.NewDecoder(archiveResp.Body).Decode(&archived); err != nil {
		t.Fatalf("decode archive response: %v", err)
	}
	if archived["artifact"]["archived_at"] == nil {
		t.Fatal("expected archived_at after archive")
	}
	firstArchivedAt := archived["artifact"]["archived_at"]

	archiveAgain := postJSONExpectStatus(t, h.baseURL+"/artifacts/"+artifactID+"/archive", `{"actor_id":"actor-1"}`, http.StatusOK)
	defer archiveAgain.Body.Close()
	var archivedAgain map[string]map[string]any
	if err := json.NewDecoder(archiveAgain.Body).Decode(&archivedAgain); err != nil {
		t.Fatalf("decode idempotent archive: %v", err)
	}
	if archivedAgain["artifact"]["archived_at"] != firstArchivedAt {
		t.Fatalf("idempotent archive should preserve archived_at: first=%v second=%v", firstArchivedAt, archivedAgain["artifact"]["archived_at"])
	}

	listDefault, err := http.Get(h.baseURL + "/artifacts")
	if err != nil {
		t.Fatalf("GET /artifacts: %v", err)
	}
	defer listDefault.Body.Close()
	var defaultListed map[string][]map[string]any
	if err := json.NewDecoder(listDefault.Body).Decode(&defaultListed); err != nil {
		t.Fatalf("decode default list: %v", err)
	}
	for _, a := range defaultListed["artifacts"] {
		if a["id"] == artifactID {
			t.Fatal("archived artifact must not appear in default list")
		}
	}

	withArchived, err := http.Get(h.baseURL + "/artifacts?include_archived=true")
	if err != nil {
		t.Fatalf("GET include_archived: %v", err)
	}
	defer withArchived.Body.Close()
	var incListed map[string][]map[string]any
	if err := json.NewDecoder(withArchived.Body).Decode(&incListed); err != nil {
		t.Fatalf("decode include_archived list: %v", err)
	}
	found := false
	for _, a := range incListed["artifacts"] {
		if a["id"] == artifactID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected archived artifact with include_archived=true")
	}

	onlyArchived, err := http.Get(h.baseURL + "/artifacts?archived_only=true")
	if err != nil {
		t.Fatalf("GET archived_only: %v", err)
	}
	defer onlyArchived.Body.Close()
	var onlyListed map[string][]map[string]any
	if err := json.NewDecoder(onlyArchived.Body).Decode(&onlyListed); err != nil {
		t.Fatalf("decode archived_only list: %v", err)
	}
	if len(onlyListed["artifacts"]) != 1 || onlyListed["artifacts"][0]["id"] != artifactID {
		t.Fatalf("expected exactly archived artifact, got %#v", onlyListed["artifacts"])
	}

	unarchiveResp := postJSONExpectStatus(t, h.baseURL+"/artifacts/"+artifactID+"/unarchive", `{"actor_id":"actor-1"}`, http.StatusOK)
	defer unarchiveResp.Body.Close()

	reUnarchive, err := http.Post(h.baseURL+"/artifacts/"+artifactID+"/unarchive", "application/json", strings.NewReader(`{"actor_id":"actor-1"}`))
	if err != nil {
		t.Fatalf("POST second unarchive: %v", err)
	}
	defer reUnarchive.Body.Close()
	if reUnarchive.StatusCode != http.StatusConflict {
		b, _ := io.ReadAll(reUnarchive.Body)
		t.Fatalf("expected 409 on second unarchive, got %d body=%s", reUnarchive.StatusCode, string(b))
	}

	listFinal, err := http.Get(h.baseURL + "/artifacts")
	if err != nil {
		t.Fatalf("GET /artifacts final: %v", err)
	}
	defer listFinal.Body.Close()
	var finalListed map[string][]map[string]any
	if err := json.NewDecoder(listFinal.Body).Decode(&finalListed); err != nil {
		t.Fatalf("decode final list: %v", err)
	}
	found = false
	for _, a := range finalListed["artifacts"] {
		if a["id"] == artifactID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected artifact back in default list after unarchive")
	}
}

func TestArtifactArchiveThenTrash(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/artifacts", `{
		"actor_id":"actor-1",
		"artifact":{"kind":"blob","refs":["thread:thread-1"],"summary":"archive then trash"},
		"content":"x",
		"content_type":"text"
	}`, http.StatusCreated)
	defer createResp.Body.Close()
	var created map[string]map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	artifactID := asString(created["artifact"]["id"])
	if artifactID == "" {
		t.Fatal("expected artifact id")
	}

	postJSONExpectStatus(t, h.baseURL+"/artifacts/"+artifactID+"/archive", `{"actor_id":"actor-1"}`, http.StatusOK).Body.Close()

	tombResp := postJSONExpectStatus(t, h.baseURL+"/artifacts/"+artifactID+"/tombstone", `{"actor_id":"actor-1","reason":"cleanup"}`, http.StatusOK)
	defer tombResp.Body.Close()
	var tomb map[string]map[string]any
	if err := json.NewDecoder(tombResp.Body).Decode(&tomb); err != nil {
		t.Fatalf("decode tombstone: %v", err)
	}
	if tomb["artifact"]["archived_at"] != nil {
		t.Fatalf("expected archived_at cleared after tombstone, got %#v", tomb["artifact"]["archived_at"])
	}
	if tomb["artifact"]["tombstoned_at"] == nil {
		t.Fatal("expected tombstoned_at set")
	}

	restoreResp := postJSONExpectStatus(t, h.baseURL+"/artifacts/"+artifactID+"/restore", `{"actor_id":"actor-1"}`, http.StatusOK)
	defer restoreResp.Body.Close()
	var restored map[string]map[string]any
	if err := json.NewDecoder(restoreResp.Body).Decode(&restored); err != nil {
		t.Fatalf("decode restore: %v", err)
	}
	if restored["artifact"]["archived_at"] != nil {
		t.Fatalf("expected no archived_at after restore, got %#v", restored["artifact"]["archived_at"])
	}
	if restored["artifact"]["tombstoned_at"] != nil {
		t.Fatalf("expected no tombstoned_at after restore, got %#v", restored["artifact"]["tombstoned_at"])
	}
}

func TestArtifactCannotArchiveTrashed(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/artifacts", `{
		"actor_id":"actor-1",
		"artifact":{"kind":"blob","refs":["thread:thread-1"],"summary":"trashed"},
		"content":"y",
		"content_type":"text"
	}`, http.StatusCreated)
	defer createResp.Body.Close()
	var created map[string]map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	artifactID := asString(created["artifact"]["id"])

	postJSONExpectStatus(t, h.baseURL+"/artifacts/"+artifactID+"/tombstone", `{"actor_id":"actor-1","reason":"x"}`, http.StatusOK).Body.Close()

	conflict := postJSONExpectStatus(t, h.baseURL+"/artifacts/"+artifactID+"/archive", `{"actor_id":"actor-1"}`, http.StatusConflict)
	defer conflict.Body.Close()
	assertErrorCode(t, conflict, "already_tombstoned")
}

func TestDocumentArchiveLifecycle(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/docs", `{
		"actor_id":"actor-1",
		"document":{"title":"Archive doc"},
		"content":"c",
		"content_type":"text"
	}`, http.StatusCreated)
	defer createResp.Body.Close()
	var created map[string]map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create doc: %v", err)
	}
	docID := asString(created["document"]["id"])
	if docID == "" {
		t.Fatal("expected document id")
	}

	archiveResp := postJSONExpectStatus(t, h.baseURL+"/docs/"+docID+"/archive", `{"actor_id":"actor-1"}`, http.StatusOK)
	defer archiveResp.Body.Close()
	var archived struct {
		Document map[string]any `json:"document"`
	}
	if err := json.NewDecoder(archiveResp.Body).Decode(&archived); err != nil {
		t.Fatalf("decode archive doc: %v", err)
	}
	if archived.Document["archived_at"] == nil {
		t.Fatal("expected archived_at on document")
	}
	firstAt := archived.Document["archived_at"]

	postJSONExpectStatus(t, h.baseURL+"/docs/"+docID+"/archive", `{"actor_id":"actor-1"}`, http.StatusOK).Body.Close()

	listDefault, err := http.Get(h.baseURL + "/docs")
	if err != nil {
		t.Fatalf("GET /docs: %v", err)
	}
	defer listDefault.Body.Close()
	var defaultListed struct {
		Documents []map[string]any `json:"documents"`
	}
	if err := json.NewDecoder(listDefault.Body).Decode(&defaultListed); err != nil {
		t.Fatalf("decode default docs list: %v", err)
	}
	for _, d := range defaultListed.Documents {
		if d["id"] == docID {
			t.Fatal("archived document must not appear in default list")
		}
	}

	withArchived, err := http.Get(h.baseURL + "/docs?include_archived=true")
	if err != nil {
		t.Fatalf("GET include_archived docs: %v", err)
	}
	defer withArchived.Body.Close()
	var incListed struct {
		Documents []map[string]any `json:"documents"`
	}
	if err := json.NewDecoder(withArchived.Body).Decode(&incListed); err != nil {
		t.Fatalf("decode include_archived: %v", err)
	}
	found := false
	for _, d := range incListed.Documents {
		if d["id"] == docID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected doc in include_archived list")
	}

	onlyArchived, err := http.Get(h.baseURL + "/docs?archived_only=true")
	if err != nil {
		t.Fatalf("GET archived_only docs: %v", err)
	}
	defer onlyArchived.Body.Close()
	var onlyListed struct {
		Documents []map[string]any `json:"documents"`
	}
	if err := json.NewDecoder(onlyArchived.Body).Decode(&onlyListed); err != nil {
		t.Fatalf("decode archived_only: %v", err)
	}
	if len(onlyListed.Documents) != 1 || onlyListed.Documents[0]["id"] != docID {
		t.Fatalf("expected single archived doc, got %#v", onlyListed.Documents)
	}
	if onlyListed.Documents[0]["archived_at"] == nil {
		t.Fatal("archived_only row should carry archived_at")
	}
	if onlyListed.Documents[0]["archived_at"] != firstAt {
		t.Fatalf("archived_at should match archive response: list=%v archive=%v", onlyListed.Documents[0]["archived_at"], firstAt)
	}

	postJSONExpectStatus(t, h.baseURL+"/docs/"+docID+"/unarchive", `{"actor_id":"actor-1"}`, http.StatusOK).Body.Close()

	reUnarchive, err := http.Post(h.baseURL+"/docs/"+docID+"/unarchive", "application/json", strings.NewReader(`{"actor_id":"actor-1"}`))
	if err != nil {
		t.Fatalf("second unarchive: %v", err)
	}
	defer reUnarchive.Body.Close()
	if reUnarchive.StatusCode != http.StatusConflict {
		b, _ := io.ReadAll(reUnarchive.Body)
		t.Fatalf("expected 409 second unarchive, got %d body=%s", reUnarchive.StatusCode, string(b))
	}

	listFinal, err := http.Get(h.baseURL + "/docs")
	if err != nil {
		t.Fatalf("GET /docs final: %v", err)
	}
	defer listFinal.Body.Close()
	var finalListed struct {
		Documents []map[string]any `json:"documents"`
	}
	if err := json.NewDecoder(listFinal.Body).Decode(&finalListed); err != nil {
		t.Fatalf("decode final list: %v", err)
	}
	found = false
	for _, d := range finalListed.Documents {
		if d["id"] == docID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected document back in default list")
	}
}

func TestDocumentRestoreLifecycle(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/docs", `{
		"actor_id":"actor-1",
		"document":{"title":"Restore doc"},
		"content":"z",
		"content_type":"text"
	}`, http.StatusCreated)
	defer createResp.Body.Close()
	var created map[string]map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	docID := asString(created["document"]["id"])

	postJSONExpectStatus(t, h.baseURL+"/docs/"+docID+"/tombstone", `{"actor_id":"actor-1","reason":"r"}`, http.StatusOK).Body.Close()

	restoreResp := postJSONExpectStatus(t, h.baseURL+"/docs/"+docID+"/restore", `{"actor_id":"actor-1"}`, http.StatusOK)
	defer restoreResp.Body.Close()
	var restored struct {
		Document map[string]any `json:"document"`
	}
	if err := json.NewDecoder(restoreResp.Body).Decode(&restored); err != nil {
		t.Fatalf("decode restore: %v", err)
	}
	if restored.Document["tombstoned_at"] != nil {
		t.Fatalf("expected tombstoned_at cleared, got %#v", restored.Document["tombstoned_at"])
	}
	if restored.Document["tombstoned_by"] != nil {
		t.Fatalf("expected tombstoned_by cleared, got %#v", restored.Document["tombstoned_by"])
	}
	if restored.Document["tombstone_reason"] != nil {
		t.Fatalf("expected tombstone_reason cleared, got %#v", restored.Document["tombstone_reason"])
	}

	reRestore, err := http.Post(h.baseURL+"/docs/"+docID+"/restore", "application/json", strings.NewReader(`{"actor_id":"actor-1"}`))
	if err != nil {
		t.Fatalf("second restore: %v", err)
	}
	defer reRestore.Body.Close()
	if reRestore.StatusCode != http.StatusConflict {
		b, _ := io.ReadAll(reRestore.Body)
		t.Fatalf("expected 409 second restore, got %d body=%s", reRestore.StatusCode, string(b))
	}
	assertErrorCode(t, reRestore, "not_tombstoned")
}

func TestDocumentPurgeLifecycle(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServerWithHumanPrincipal(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/docs", `{
		"actor_id":"actor-1",
		"document":{"title":"Purge doc"},
		"content":"p",
		"content_type":"text"
	}`, http.StatusCreated)
	defer createResp.Body.Close()
	var created map[string]map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	docID := asString(created["document"]["id"])

	postJSONExpectStatus(t, h.baseURL+"/docs/"+docID+"/tombstone", `{"actor_id":"actor-1","reason":"gone"}`, http.StatusOK).Body.Close()

	purgeResp := postJSONExpectStatusBearer(t, h.baseURL+"/docs/"+docID+"/purge", `{}`, h.humanAccessToken, http.StatusOK)
	defer purgeResp.Body.Close()
	var purged map[string]any
	if err := json.NewDecoder(purgeResp.Body).Decode(&purged); err != nil {
		t.Fatalf("decode purge: %v", err)
	}
	if purged["purged"] != true || purged["document_id"] != docID {
		t.Fatalf("unexpected purge payload: %#v", purged)
	}

	getResp, err := http.Get(h.baseURL + "/docs/" + docID)
	if err != nil {
		t.Fatalf("GET doc after purge: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 after purge, got %d", getResp.StatusCode)
	}
}

func TestDocumentTombstonedOnlyFilter(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createA := postJSONExpectStatus(t, h.baseURL+"/docs", `{
		"actor_id":"actor-1",
		"document":{"title":"Doc A"},
		"content":"a",
		"content_type":"text"
	}`, http.StatusCreated)
	defer createA.Body.Close()
	var payloadA map[string]map[string]any
	if err := json.NewDecoder(createA.Body).Decode(&payloadA); err != nil {
		t.Fatalf("decode doc A: %v", err)
	}
	idA := asString(payloadA["document"]["id"])

	createB := postJSONExpectStatus(t, h.baseURL+"/docs", `{
		"actor_id":"actor-1",
		"document":{"title":"Doc B"},
		"content":"b",
		"content_type":"text"
	}`, http.StatusCreated)
	defer createB.Body.Close()
	var payloadB map[string]map[string]any
	if err := json.NewDecoder(createB.Body).Decode(&payloadB); err != nil {
		t.Fatalf("decode doc B: %v", err)
	}
	idB := asString(payloadB["document"]["id"])

	postJSONExpectStatus(t, h.baseURL+"/docs/"+idA+"/tombstone", `{"actor_id":"actor-1","reason":"x"}`, http.StatusOK).Body.Close()

	onlyResp, err := http.Get(h.baseURL + "/docs?tombstoned_only=true")
	if err != nil {
		t.Fatalf("GET tombstoned_only: %v", err)
	}
	defer onlyResp.Body.Close()
	var only struct {
		Documents []map[string]any `json:"documents"`
	}
	if err := json.NewDecoder(onlyResp.Body).Decode(&only); err != nil {
		t.Fatalf("decode tombstoned_only: %v", err)
	}
	if len(only.Documents) != 1 || only.Documents[0]["id"] != idA {
		t.Fatalf("expected one tombstoned doc %q, got %#v", idA, only.Documents)
	}
	for _, d := range only.Documents {
		if d["id"] == idB {
			t.Fatal("live doc must not appear in tombstoned_only list")
		}
	}
}

func TestThreadArchiveLifecycle(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"Archive thread",
			"type":"incident",
			"status":"active",
			"priority":"p2",
			"tags":["archive-test"],
			"cadence":"daily",
			"next_check_in_at":"2026-03-05T00:00:00Z",
			"current_summary":"s",
			"next_actions":["a"],
			"key_artifacts":[],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer createResp.Body.Close()
	var created struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create thread: %v", err)
	}
	threadID := asString(created.Thread["id"])
	if threadID == "" {
		t.Fatal("expected thread id")
	}

	archiveResp := postJSONExpectStatus(t, h.baseURL+"/threads/"+threadID+"/archive", `{"actor_id":"actor-1"}`, http.StatusOK)
	defer archiveResp.Body.Close()
	var archived struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(archiveResp.Body).Decode(&archived); err != nil {
		t.Fatalf("decode archive thread: %v", err)
	}
	if archived.Thread["archived_at"] == nil {
		t.Fatal("expected archived_at on thread")
	}
	firstAt := archived.Thread["archived_at"]

	postJSONExpectStatus(t, h.baseURL+"/threads/"+threadID+"/archive", `{"actor_id":"actor-1"}`, http.StatusOK).Body.Close()

	listDefault, err := http.Get(h.baseURL + "/threads")
	if err != nil {
		t.Fatalf("GET /threads: %v", err)
	}
	defer listDefault.Body.Close()
	var defaultListed struct {
		Threads []map[string]any `json:"threads"`
	}
	if err := json.NewDecoder(listDefault.Body).Decode(&defaultListed); err != nil {
		t.Fatalf("decode threads list: %v", err)
	}
	for _, th := range defaultListed.Threads {
		if th["id"] == threadID {
			t.Fatal("archived thread must not appear in default list")
		}
	}

	withArchived, err := http.Get(h.baseURL + "/threads?include_archived=true")
	if err != nil {
		t.Fatalf("GET include_archived threads: %v", err)
	}
	defer withArchived.Body.Close()
	var incListed struct {
		Threads []map[string]any `json:"threads"`
	}
	if err := json.NewDecoder(withArchived.Body).Decode(&incListed); err != nil {
		t.Fatalf("decode include_archived threads: %v", err)
	}
	found := false
	for _, th := range incListed.Threads {
		if th["id"] == threadID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected thread in include_archived list")
	}

	onlyArchived, err := http.Get(h.baseURL + "/threads?archived_only=true")
	if err != nil {
		t.Fatalf("GET archived_only threads: %v", err)
	}
	defer onlyArchived.Body.Close()
	var onlyListed struct {
		Threads []map[string]any `json:"threads"`
	}
	if err := json.NewDecoder(onlyArchived.Body).Decode(&onlyListed); err != nil {
		t.Fatalf("decode archived_only threads: %v", err)
	}
	if len(onlyListed.Threads) != 1 || onlyListed.Threads[0]["id"] != threadID {
		t.Fatalf("expected single archived thread, got %#v", onlyListed.Threads)
	}
	if onlyListed.Threads[0]["archived_at"] == nil {
		t.Fatal("archived_only thread should have archived_at")
	}
	if onlyListed.Threads[0]["archived_at"] != firstAt {
		t.Fatalf("archived_at mismatch: list=%v archive=%v", onlyListed.Threads[0]["archived_at"], firstAt)
	}

	postJSONExpectStatus(t, h.baseURL+"/threads/"+threadID+"/unarchive", `{"actor_id":"actor-1"}`, http.StatusOK).Body.Close()

	reUnarchive, err := http.Post(h.baseURL+"/threads/"+threadID+"/unarchive", "application/json", strings.NewReader(`{"actor_id":"actor-1"}`))
	if err != nil {
		t.Fatalf("second unarchive: %v", err)
	}
	defer reUnarchive.Body.Close()
	if reUnarchive.StatusCode != http.StatusConflict {
		b, _ := io.ReadAll(reUnarchive.Body)
		t.Fatalf("expected 409 second unarchive, got %d body=%s", reUnarchive.StatusCode, string(b))
	}

	listFinal, err := http.Get(h.baseURL + "/threads")
	if err != nil {
		t.Fatalf("GET /threads final: %v", err)
	}
	defer listFinal.Body.Close()
	var finalListed struct {
		Threads []map[string]any `json:"threads"`
	}
	if err := json.NewDecoder(listFinal.Body).Decode(&finalListed); err != nil {
		t.Fatalf("decode final threads: %v", err)
	}
	found = false
	for _, th := range finalListed.Threads {
		if th["id"] == threadID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected thread back in default list after unarchive")
	}
}

func TestThreadTombstoneLifecycle(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"Tomb thread",
			"type":"incident",
			"status":"active",
			"priority":"p2",
			"tags":["tomb"],
			"cadence":"daily",
			"next_check_in_at":"2026-03-05T00:00:00Z",
			"current_summary":"s",
			"next_actions":["a"],
			"key_artifacts":[],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer createResp.Body.Close()
	var created struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create thread: %v", err)
	}
	threadID := asString(created.Thread["id"])

	tomb1 := postJSONExpectStatus(t, h.baseURL+"/threads/"+threadID+"/tombstone", `{"actor_id":"actor-1","reason":"one"}`, http.StatusOK)
	defer tomb1.Body.Close()
	var firstTomb struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(tomb1.Body).Decode(&firstTomb); err != nil {
		t.Fatalf("decode first tombstone: %v", err)
	}
	if firstTomb.Thread["tombstoned_at"] == nil {
		t.Fatal("expected tombstoned_at")
	}

	tomb2 := postJSONExpectStatus(t, h.baseURL+"/threads/"+threadID+"/tombstone", `{"actor_id":"actor-1","reason":"two"}`, http.StatusOK)
	defer tomb2.Body.Close()
	var secondTomb struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(tomb2.Body).Decode(&secondTomb); err != nil {
		t.Fatalf("decode second tombstone: %v", err)
	}
	if secondTomb.Thread["tombstoned_at"] != firstTomb.Thread["tombstoned_at"] {
		t.Fatalf("idempotent tombstone should preserve tombstoned_at: %v vs %v", firstTomb.Thread["tombstoned_at"], secondTomb.Thread["tombstoned_at"])
	}

	listDefault, err := http.Get(h.baseURL + "/threads")
	if err != nil {
		t.Fatalf("GET /threads: %v", err)
	}
	defer listDefault.Body.Close()
	var defaultListed struct {
		Threads []map[string]any `json:"threads"`
	}
	if err := json.NewDecoder(listDefault.Body).Decode(&defaultListed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	for _, th := range defaultListed.Threads {
		if th["id"] == threadID {
			t.Fatal("tombstoned thread must not appear in default list")
		}
	}

	withTomb, err := http.Get(h.baseURL + "/threads?include_tombstoned=true")
	if err != nil {
		t.Fatalf("GET include_tombstoned: %v", err)
	}
	defer withTomb.Body.Close()
	var incListed struct {
		Threads []map[string]any `json:"threads"`
	}
	if err := json.NewDecoder(withTomb.Body).Decode(&incListed); err != nil {
		t.Fatalf("decode include_tombstoned: %v", err)
	}
	found := false
	for _, th := range incListed.Threads {
		if th["id"] == threadID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected thread in include_tombstoned list")
	}

	onlyTomb, err := http.Get(h.baseURL + "/threads?tombstoned_only=true")
	if err != nil {
		t.Fatalf("GET tombstoned_only: %v", err)
	}
	defer onlyTomb.Body.Close()
	var onlyListed struct {
		Threads []map[string]any `json:"threads"`
	}
	if err := json.NewDecoder(onlyTomb.Body).Decode(&onlyListed); err != nil {
		t.Fatalf("decode tombstoned_only: %v", err)
	}
	if len(onlyListed.Threads) != 1 || onlyListed.Threads[0]["id"] != threadID {
		t.Fatalf("expected exactly one tombstoned thread, got %#v", onlyListed.Threads)
	}
}

func TestThreadRestoreLifecycle(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"Restore thread",
			"type":"incident",
			"status":"active",
			"priority":"p2",
			"tags":["restore"],
			"cadence":"daily",
			"next_check_in_at":"2026-03-05T00:00:00Z",
			"current_summary":"s",
			"next_actions":["a"],
			"key_artifacts":[],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer createResp.Body.Close()
	var created struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	threadID := asString(created.Thread["id"])

	postJSONExpectStatus(t, h.baseURL+"/threads/"+threadID+"/tombstone", `{"actor_id":"actor-1","reason":"t"}`, http.StatusOK).Body.Close()

	restoreResp := postJSONExpectStatus(t, h.baseURL+"/threads/"+threadID+"/restore", `{"actor_id":"actor-1"}`, http.StatusOK)
	defer restoreResp.Body.Close()
	var restored struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(restoreResp.Body).Decode(&restored); err != nil {
		t.Fatalf("decode restore: %v", err)
	}
	if restored.Thread["tombstoned_at"] != nil {
		t.Fatalf("expected tombstoned_at cleared, got %#v", restored.Thread["tombstoned_at"])
	}

	reRestore, err := http.Post(h.baseURL+"/threads/"+threadID+"/restore", "application/json", strings.NewReader(`{"actor_id":"actor-1"}`))
	if err != nil {
		t.Fatalf("second restore: %v", err)
	}
	defer reRestore.Body.Close()
	if reRestore.StatusCode != http.StatusConflict {
		b, _ := io.ReadAll(reRestore.Body)
		t.Fatalf("expected 409 second restore, got %d body=%s", reRestore.StatusCode, string(b))
	}
	assertErrorCode(t, reRestore, "not_tombstoned")
}

func TestThreadArchiveThenTrash(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"Archive trash thread",
			"type":"incident",
			"status":"active",
			"priority":"p2",
			"tags":["at"],
			"cadence":"daily",
			"next_check_in_at":"2026-03-05T00:00:00Z",
			"current_summary":"s",
			"next_actions":["a"],
			"key_artifacts":[],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer createResp.Body.Close()
	var created struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	threadID := asString(created.Thread["id"])

	postJSONExpectStatus(t, h.baseURL+"/threads/"+threadID+"/archive", `{"actor_id":"actor-1"}`, http.StatusOK).Body.Close()

	tombResp := postJSONExpectStatus(t, h.baseURL+"/threads/"+threadID+"/tombstone", `{"actor_id":"actor-1","reason":"cleanup"}`, http.StatusOK)
	defer tombResp.Body.Close()
	var tomb struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(tombResp.Body).Decode(&tomb); err != nil {
		t.Fatalf("decode tombstone: %v", err)
	}
	if tomb.Thread["archived_at"] != nil {
		t.Fatalf("expected archived_at cleared, got %#v", tomb.Thread["archived_at"])
	}
	if tomb.Thread["tombstoned_at"] == nil {
		t.Fatal("expected tombstoned_at")
	}

	restoreResp := postJSONExpectStatus(t, h.baseURL+"/threads/"+threadID+"/restore", `{"actor_id":"actor-1"}`, http.StatusOK)
	defer restoreResp.Body.Close()
	var restored struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(restoreResp.Body).Decode(&restored); err != nil {
		t.Fatalf("decode restore: %v", err)
	}
	if restored.Thread["archived_at"] != nil {
		t.Fatalf("expected no archived_at after restore, got %#v", restored.Thread["archived_at"])
	}
	if restored.Thread["tombstoned_at"] != nil {
		t.Fatalf("expected no tombstoned_at after restore, got %#v", restored.Thread["tombstoned_at"])
	}
}

func TestBoardArchiveLifecycle(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	threadID := createBoardThreadViaHTTP(t, h, "Board archive primary")

	createBoardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{
		"actor_id":"actor-1",
		"board":{"title":"Archive board test","primary_thread_id":"`+threadID+`"}
	}`, http.StatusCreated)
	defer createBoardResp.Body.Close()
	var createPayload struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(createBoardResp.Body).Decode(&createPayload); err != nil {
		t.Fatalf("decode create board: %v", err)
	}
	boardID := asString(createPayload.Board["id"])
	if boardID == "" {
		t.Fatal("expected board id")
	}

	archiveResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/archive", `{"actor_id":"actor-1"}`, http.StatusOK)
	defer archiveResp.Body.Close()
	var archived struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(archiveResp.Body).Decode(&archived); err != nil {
		t.Fatalf("decode archive board: %v", err)
	}
	if archived.Board["archived_at"] == nil {
		t.Fatal("expected archived_at on board")
	}
	firstAt := archived.Board["archived_at"]

	postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/archive", `{"actor_id":"actor-1"}`, http.StatusOK).Body.Close()

	listDefault, err := http.Get(h.baseURL + "/boards")
	if err != nil {
		t.Fatalf("GET /boards: %v", err)
	}
	defer listDefault.Body.Close()
	var defaultListed struct {
		Boards []struct {
			Board map[string]any `json:"board"`
		} `json:"boards"`
	}
	if err := json.NewDecoder(listDefault.Body).Decode(&defaultListed); err != nil {
		t.Fatalf("decode boards list: %v", err)
	}
	for _, item := range defaultListed.Boards {
		if asString(item.Board["id"]) == boardID {
			t.Fatal("archived board must not appear in default list")
		}
	}

	withArchived, err := http.Get(h.baseURL + "/boards?include_archived=true")
	if err != nil {
		t.Fatalf("GET include_archived boards: %v", err)
	}
	defer withArchived.Body.Close()
	var incListed struct {
		Boards []struct {
			Board map[string]any `json:"board"`
		} `json:"boards"`
	}
	if err := json.NewDecoder(withArchived.Body).Decode(&incListed); err != nil {
		t.Fatalf("decode include_archived boards: %v", err)
	}
	found := false
	for _, item := range incListed.Boards {
		if asString(item.Board["id"]) == boardID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected board in include_archived list")
	}

	onlyArchived, err := http.Get(h.baseURL + "/boards?archived_only=true")
	if err != nil {
		t.Fatalf("GET archived_only boards: %v", err)
	}
	defer onlyArchived.Body.Close()
	var onlyListed struct {
		Boards []struct {
			Board map[string]any `json:"board"`
		} `json:"boards"`
	}
	if err := json.NewDecoder(onlyArchived.Body).Decode(&onlyListed); err != nil {
		t.Fatalf("decode archived_only boards: %v", err)
	}
	if len(onlyListed.Boards) != 1 || asString(onlyListed.Boards[0].Board["id"]) != boardID {
		t.Fatalf("expected single archived board, got %#v", onlyListed.Boards)
	}
	if onlyListed.Boards[0].Board["archived_at"] == nil {
		t.Fatal("archived_only board should have archived_at")
	}
	if onlyListed.Boards[0].Board["archived_at"] != firstAt {
		t.Fatalf("archived_at mismatch: list=%v archive=%v", onlyListed.Boards[0].Board["archived_at"], firstAt)
	}

	postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/unarchive", `{"actor_id":"actor-1"}`, http.StatusOK).Body.Close()

	reUnarchive, err := http.Post(h.baseURL+"/boards/"+boardID+"/unarchive", "application/json", strings.NewReader(`{"actor_id":"actor-1"}`))
	if err != nil {
		t.Fatalf("second unarchive: %v", err)
	}
	defer reUnarchive.Body.Close()
	if reUnarchive.StatusCode != http.StatusConflict {
		b, _ := io.ReadAll(reUnarchive.Body)
		t.Fatalf("expected 409 second unarchive, got %d body=%s", reUnarchive.StatusCode, string(b))
	}

	listFinal, err := http.Get(h.baseURL + "/boards")
	if err != nil {
		t.Fatalf("GET /boards final: %v", err)
	}
	defer listFinal.Body.Close()
	var finalListed struct {
		Boards []struct {
			Board map[string]any `json:"board"`
		} `json:"boards"`
	}
	if err := json.NewDecoder(listFinal.Body).Decode(&finalListed); err != nil {
		t.Fatalf("decode final boards: %v", err)
	}
	found = false
	for _, item := range finalListed.Boards {
		if asString(item.Board["id"]) == boardID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected board back in default list after unarchive")
	}
}

func TestBoardTombstoneLifecycle(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	threadID := createBoardThreadViaHTTP(t, h, "Board tomb primary")

	createBoardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{
		"actor_id":"actor-1",
		"board":{"title":"Tomb board","primary_thread_id":"`+threadID+`"}
	}`, http.StatusCreated)
	defer createBoardResp.Body.Close()
	var createPayload struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(createBoardResp.Body).Decode(&createPayload); err != nil {
		t.Fatalf("decode create board: %v", err)
	}
	boardID := asString(createPayload.Board["id"])

	postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/tombstone", `{"actor_id":"actor-1","reason":"done"}`, http.StatusOK).Body.Close()

	listDefault, err := http.Get(h.baseURL + "/boards")
	if err != nil {
		t.Fatalf("GET /boards: %v", err)
	}
	defer listDefault.Body.Close()
	var defaultListed struct {
		Boards []struct {
			Board map[string]any `json:"board"`
		} `json:"boards"`
	}
	if err := json.NewDecoder(listDefault.Body).Decode(&defaultListed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	for _, item := range defaultListed.Boards {
		if asString(item.Board["id"]) == boardID {
			t.Fatal("tombstoned board must not appear in default list")
		}
	}

	onlyTomb, err := http.Get(h.baseURL + "/boards?tombstoned_only=true")
	if err != nil {
		t.Fatalf("GET tombstoned_only boards: %v", err)
	}
	defer onlyTomb.Body.Close()
	var onlyListed struct {
		Boards []struct {
			Board map[string]any `json:"board"`
		} `json:"boards"`
	}
	if err := json.NewDecoder(onlyTomb.Body).Decode(&onlyListed); err != nil {
		t.Fatalf("decode tombstoned_only: %v", err)
	}
	if len(onlyListed.Boards) != 1 || asString(onlyListed.Boards[0].Board["id"]) != boardID {
		t.Fatalf("expected one tombstoned board, got %#v", onlyListed.Boards)
	}
}

func TestBoardRestoreLifecycle(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	threadID := createBoardThreadViaHTTP(t, h, "Board restore primary")

	createBoardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{
		"actor_id":"actor-1",
		"board":{"title":"Restore board","primary_thread_id":"`+threadID+`"}
	}`, http.StatusCreated)
	defer createBoardResp.Body.Close()
	var createPayload struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(createBoardResp.Body).Decode(&createPayload); err != nil {
		t.Fatalf("decode create board: %v", err)
	}
	boardID := asString(createPayload.Board["id"])

	postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/tombstone", `{"actor_id":"actor-1","reason":"x"}`, http.StatusOK).Body.Close()

	restoreResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/restore", `{"actor_id":"actor-1"}`, http.StatusOK)
	defer restoreResp.Body.Close()
	var restored struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(restoreResp.Body).Decode(&restored); err != nil {
		t.Fatalf("decode restore: %v", err)
	}
	if restored.Board["tombstoned_at"] != nil {
		t.Fatalf("expected tombstoned_at cleared, got %#v", restored.Board["tombstoned_at"])
	}

	reRestore, err := http.Post(h.baseURL+"/boards/"+boardID+"/restore", "application/json", strings.NewReader(`{"actor_id":"actor-1"}`))
	if err != nil {
		t.Fatalf("second restore: %v", err)
	}
	defer reRestore.Body.Close()
	if reRestore.StatusCode != http.StatusConflict {
		b, _ := io.ReadAll(reRestore.Body)
		t.Fatalf("expected 409 second restore, got %d body=%s", reRestore.StatusCode, string(b))
	}
	assertErrorCode(t, reRestore, "not_tombstoned")
}

func TestDocumentArchiveThenTrash(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/docs", `{
		"actor_id":"actor-1",
		"document":{"title":"Archive then trash doc"},
		"content":"c",
		"content_type":"text"
	}`, http.StatusCreated)
	defer createResp.Body.Close()
	var created map[string]map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	docID := asString(created["document"]["id"])
	if docID == "" {
		t.Fatal("expected document id")
	}

	archiveResp := postJSONExpectStatus(t, h.baseURL+"/docs/"+docID+"/archive", `{"actor_id":"actor-1"}`, http.StatusOK)
	defer archiveResp.Body.Close()
	var archived struct {
		Document map[string]any `json:"document"`
	}
	if err := json.NewDecoder(archiveResp.Body).Decode(&archived); err != nil {
		t.Fatalf("decode archive: %v", err)
	}
	if archived.Document["archived_at"] == nil {
		t.Fatal("expected archived_at after archive")
	}

	tombResp := postJSONExpectStatus(t, h.baseURL+"/docs/"+docID+"/tombstone", `{"actor_id":"actor-1","reason":"cleanup"}`, http.StatusOK)
	defer tombResp.Body.Close()
	var tomb struct {
		Document map[string]any `json:"document"`
	}
	if err := json.NewDecoder(tombResp.Body).Decode(&tomb); err != nil {
		t.Fatalf("decode tombstone: %v", err)
	}
	if tomb.Document["archived_at"] != nil {
		t.Fatalf("expected archived_at cleared after tombstone, got %#v", tomb.Document["archived_at"])
	}
	if tomb.Document["tombstoned_at"] == nil {
		t.Fatal("expected tombstoned_at set")
	}

	restoreResp := postJSONExpectStatus(t, h.baseURL+"/docs/"+docID+"/restore", `{"actor_id":"actor-1"}`, http.StatusOK)
	defer restoreResp.Body.Close()
	var restored struct {
		Document map[string]any `json:"document"`
	}
	if err := json.NewDecoder(restoreResp.Body).Decode(&restored); err != nil {
		t.Fatalf("decode restore: %v", err)
	}
	if restored.Document["archived_at"] != nil {
		t.Fatalf("expected no archived_at after restore, got %#v", restored.Document["archived_at"])
	}
	if restored.Document["tombstoned_at"] != nil {
		t.Fatalf("expected no tombstoned_at after restore, got %#v", restored.Document["tombstoned_at"])
	}
}

func TestDocumentCannotArchiveTrashed(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/docs", `{
		"actor_id":"actor-1",
		"document":{"title":"Trashed doc"},
		"content":"y",
		"content_type":"text"
	}`, http.StatusCreated)
	defer createResp.Body.Close()
	var created map[string]map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	docID := asString(created["document"]["id"])

	postJSONExpectStatus(t, h.baseURL+"/docs/"+docID+"/tombstone", `{"actor_id":"actor-1","reason":"x"}`, http.StatusOK).Body.Close()

	conflict := postJSONExpectStatus(t, h.baseURL+"/docs/"+docID+"/archive", `{"actor_id":"actor-1"}`, http.StatusConflict)
	defer conflict.Body.Close()
	assertErrorCode(t, conflict, "already_tombstoned")
}

func TestThreadCannotArchiveTrashed(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"Cannot archive trashed",
			"type":"incident",
			"status":"active",
			"priority":"p2",
			"tags":["cat"],
			"cadence":"daily",
			"next_check_in_at":"2026-03-05T00:00:00Z",
			"current_summary":"s",
			"next_actions":["a"],
			"key_artifacts":[],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer createResp.Body.Close()
	var created struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	threadID := asString(created.Thread["id"])

	postJSONExpectStatus(t, h.baseURL+"/threads/"+threadID+"/tombstone", `{"actor_id":"actor-1","reason":"x"}`, http.StatusOK).Body.Close()

	conflict := postJSONExpectStatus(t, h.baseURL+"/threads/"+threadID+"/archive", `{"actor_id":"actor-1"}`, http.StatusConflict)
	defer conflict.Body.Close()
	assertErrorCode(t, conflict, "already_tombstoned")
}

func TestBoardArchiveThenTrash(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	threadID := createBoardThreadViaHTTP(t, h, "Board archive then trash primary")

	createBoardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{
		"actor_id":"actor-1",
		"board":{"title":"Archive then trash board","primary_thread_id":"`+threadID+`"}
	}`, http.StatusCreated)
	defer createBoardResp.Body.Close()
	var createPayload struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(createBoardResp.Body).Decode(&createPayload); err != nil {
		t.Fatalf("decode create board: %v", err)
	}
	boardID := asString(createPayload.Board["id"])
	if boardID == "" {
		t.Fatal("expected board id")
	}

	archiveResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/archive", `{"actor_id":"actor-1"}`, http.StatusOK)
	defer archiveResp.Body.Close()
	var archived struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(archiveResp.Body).Decode(&archived); err != nil {
		t.Fatalf("decode archive: %v", err)
	}
	if archived.Board["archived_at"] == nil {
		t.Fatal("expected archived_at after archive")
	}

	tombResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/tombstone", `{"actor_id":"actor-1","reason":"cleanup"}`, http.StatusOK)
	defer tombResp.Body.Close()
	var tomb struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(tombResp.Body).Decode(&tomb); err != nil {
		t.Fatalf("decode tombstone: %v", err)
	}
	if tomb.Board["archived_at"] != nil {
		t.Fatalf("expected archived_at cleared after tombstone, got %#v", tomb.Board["archived_at"])
	}
	if tomb.Board["tombstoned_at"] == nil {
		t.Fatal("expected tombstoned_at set")
	}

	restoreResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/restore", `{"actor_id":"actor-1"}`, http.StatusOK)
	defer restoreResp.Body.Close()
	var restored struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(restoreResp.Body).Decode(&restored); err != nil {
		t.Fatalf("decode restore: %v", err)
	}
	if restored.Board["archived_at"] != nil {
		t.Fatalf("expected no archived_at after restore, got %#v", restored.Board["archived_at"])
	}
	if restored.Board["tombstoned_at"] != nil {
		t.Fatalf("expected no tombstoned_at after restore, got %#v", restored.Board["tombstoned_at"])
	}
}

func TestBoardCannotArchiveTrashed(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	threadID := createBoardThreadViaHTTP(t, h, "Board cannot archive trashed primary")

	createBoardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{
		"actor_id":"actor-1",
		"board":{"title":"Trashed board","primary_thread_id":"`+threadID+`"}
	}`, http.StatusCreated)
	defer createBoardResp.Body.Close()
	var createPayload struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(createBoardResp.Body).Decode(&createPayload); err != nil {
		t.Fatalf("decode create board: %v", err)
	}
	boardID := asString(createPayload.Board["id"])

	postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/tombstone", `{"actor_id":"actor-1","reason":"x"}`, http.StatusOK).Body.Close()

	conflict := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/archive", `{"actor_id":"actor-1"}`, http.StatusConflict)
	defer conflict.Body.Close()
	assertErrorCode(t, conflict, "already_tombstoned")
}

func TestThreadPurgeLifecycle(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServerWithHumanPrincipal(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"Purge thread",
			"type":"incident",
			"status":"active",
			"priority":"p2",
			"tags":["purge"],
			"cadence":"daily",
			"next_check_in_at":"2026-03-05T00:00:00Z",
			"current_summary":"s",
			"next_actions":["a"],
			"key_artifacts":[],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer createResp.Body.Close()
	var created struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	threadID := asString(created.Thread["id"])

	purgeEarly := postJSONExpectStatusBearer(t, h.baseURL+"/threads/"+threadID+"/purge", `{}`, h.humanAccessToken, http.StatusConflict)
	defer purgeEarly.Body.Close()
	assertErrorCode(t, purgeEarly, "not_tombstoned")

	postJSONExpectStatus(t, h.baseURL+"/threads/"+threadID+"/tombstone", `{"actor_id":"actor-1","reason":"gone"}`, http.StatusOK).Body.Close()

	purgeResp := postJSONExpectStatusBearer(t, h.baseURL+"/threads/"+threadID+"/purge", `{}`, h.humanAccessToken, http.StatusOK)
	defer purgeResp.Body.Close()
	var purged map[string]any
	if err := json.NewDecoder(purgeResp.Body).Decode(&purged); err != nil {
		t.Fatalf("decode purge: %v", err)
	}
	if purged["purged"] != true || purged["thread_id"] != threadID {
		t.Fatalf("unexpected purge payload: %#v", purged)
	}

	getResp, err := http.Get(h.baseURL + "/threads/" + threadID)
	if err != nil {
		t.Fatalf("GET thread after purge: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 after purge, got %d", getResp.StatusCode)
	}
}

func TestBoardPurgeLifecycle(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServerWithHumanPrincipal(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	threadID := createBoardThreadViaHTTP(t, h, "Board purge primary")

	createBoardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{
		"actor_id":"actor-1",
		"board":{"title":"Purge board","primary_thread_id":"`+threadID+`"}
	}`, http.StatusCreated)
	defer createBoardResp.Body.Close()
	var createPayload struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(createBoardResp.Body).Decode(&createPayload); err != nil {
		t.Fatalf("decode create board: %v", err)
	}
	boardID := asString(createPayload.Board["id"])

	purgeEarly := postJSONExpectStatusBearer(t, h.baseURL+"/boards/"+boardID+"/purge", `{}`, h.humanAccessToken, http.StatusConflict)
	defer purgeEarly.Body.Close()
	assertErrorCode(t, purgeEarly, "not_tombstoned")

	postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/tombstone", `{"actor_id":"actor-1","reason":"gone"}`, http.StatusOK).Body.Close()

	purgeResp := postJSONExpectStatusBearer(t, h.baseURL+"/boards/"+boardID+"/purge", `{}`, h.humanAccessToken, http.StatusOK)
	defer purgeResp.Body.Close()
	var purged map[string]any
	if err := json.NewDecoder(purgeResp.Body).Decode(&purged); err != nil {
		t.Fatalf("decode purge: %v", err)
	}
	if purged["purged"] != true || purged["board_id"] != boardID {
		t.Fatalf("unexpected purge payload: %#v", purged)
	}

	getResp, err := http.Get(h.baseURL + "/boards/" + boardID)
	if err != nil {
		t.Fatalf("GET board after purge: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 after purge, got %d", getResp.StatusCode)
	}
}
