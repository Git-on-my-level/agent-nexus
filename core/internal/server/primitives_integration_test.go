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
	"os"
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

	contentHash, _ := createdArtifact["artifact"]["content_hash"].(string)
	if contentHash == "" {
		t.Fatal("expected content_hash in created artifact")
	}
	expectedHash := sha256Hex([]byte("hello artifact"))
	if contentHash != expectedHash {
		t.Fatalf("content_hash mismatch: got %q want %q", contentHash, expectedHash)
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

	path1, _ := art1["artifact"]["content_path"].(string)
	path2, _ := art2["artifact"]["content_path"].(string)
	if path1 != path2 {
		t.Fatalf("identical content should share content_path: %q vs %q", path1, path2)
	}

	entries, err := os.ReadDir(h.workspace.Layout().ArtifactContentDir)
	if err != nil {
		t.Fatalf("read artifact content dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 content file (dedup), got %d", len(entries))
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
	contractPath := filepath.Join("..", "..", "..", "contracts", "oar-schema.yaml")
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
		WithAllowUnauthenticatedWrites(true),
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
