package server

import (
	"agent-nexus-core/internal/blob"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
	"agent-nexus-core/internal/storage"
)

func TestMetaHandshakeAndGeneratedMetaEndpoints(t *testing.T) {
	t.Parallel()

	h := newMetaStreamTestHarness(t,
		WithCoreVersion("1.2.3"),
		WithAPIVersion("v0"),
		WithMinCLIVersion("0.9.0"),
		WithRecommendedCLIVersion("1.1.0"),
		WithCLIDownloadURL("https://example.com/anx-cli"),
		WithCoreInstanceID("instance-test-1"),
		WithMetaCommandsPath(filepath.Join("..", "..", "..", "contracts", "gen", "meta", "commands.json")),
	)

	handshakeResp, err := http.Get(h.baseURL + "/meta/handshake")
	if err != nil {
		t.Fatalf("GET /meta/handshake: %v", err)
	}
	defer handshakeResp.Body.Close()
	if handshakeResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected /meta/handshake status: %d", handshakeResp.StatusCode)
	}

	var handshake map[string]any
	if err := json.NewDecoder(handshakeResp.Body).Decode(&handshake); err != nil {
		t.Fatalf("decode /meta/handshake response: %v", err)
	}
	if handshake["core_version"] != "1.2.3" {
		t.Fatalf("unexpected core_version: %#v", handshake)
	}
	if handshake["min_cli_version"] != "0.9.0" || handshake["recommended_cli_version"] != "1.1.0" {
		t.Fatalf("unexpected cli version metadata: %#v", handshake)
	}
	if handshake["cli_download_url"] != "https://example.com/anx-cli" || handshake["core_instance_id"] != "instance-test-1" {
		t.Fatalf("unexpected handshake values: %#v", handshake)
	}
	if handshake["dev_actor_mode"] != true {
		t.Fatalf("expected dev_actor_mode=true for test harness, got %v", handshake["dev_actor_mode"])
	}
	if handshake["human_auth_mode"] != "workspace_local" {
		t.Fatalf("expected default human_auth_mode=workspace_local, got %#v", handshake["human_auth_mode"])
	}

	commandsResp, err := http.Get(h.baseURL + "/meta/commands")
	if err != nil {
		t.Fatalf("GET /meta/commands: %v", err)
	}
	defer commandsResp.Body.Close()
	if commandsResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected /meta/commands status: %d", commandsResp.StatusCode)
	}
	var commandsPayload map[string]any
	if err := json.NewDecoder(commandsResp.Body).Decode(&commandsPayload); err != nil {
		t.Fatalf("decode /meta/commands response: %v", err)
	}
	commandCount, _ := commandsPayload["command_count"].(float64)
	if commandCount < 1 {
		t.Fatalf("expected non-empty command metadata, got %#v", commandsPayload)
	}

	commandResp, err := http.Get(h.baseURL + "/meta/commands/meta.version")
	if err != nil {
		t.Fatalf("GET /meta/commands/{id}: %v", err)
	}
	defer commandResp.Body.Close()
	if commandResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected /meta/commands/{id} status: %d", commandResp.StatusCode)
	}
	var commandPayload map[string]map[string]any
	if err := json.NewDecoder(commandResp.Body).Decode(&commandPayload); err != nil {
		t.Fatalf("decode /meta/commands/{id} response: %v", err)
	}
	if asString(commandPayload["command"]["command_id"]) != "meta.version" {
		t.Fatalf("unexpected command lookup payload: %#v", commandPayload)
	}

	conceptsResp, err := http.Get(h.baseURL + "/meta/concepts")
	if err != nil {
		t.Fatalf("GET /meta/concepts: %v", err)
	}
	defer conceptsResp.Body.Close()
	if conceptsResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected /meta/concepts status: %d", conceptsResp.StatusCode)
	}
	var conceptsPayload map[string][]map[string]any
	if err := json.NewDecoder(conceptsResp.Body).Decode(&conceptsPayload); err != nil {
		t.Fatalf("decode /meta/concepts response: %v", err)
	}
	if len(conceptsPayload["concepts"]) == 0 {
		t.Fatalf("expected non-empty concepts payload, got %#v", conceptsPayload)
	}

	conceptResp, err := http.Get(h.baseURL + "/meta/concepts/compatibility")
	if err != nil {
		t.Fatalf("GET /meta/concepts/{name}: %v", err)
	}
	defer conceptResp.Body.Close()
	if conceptResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected /meta/concepts/{name} status: %d", conceptResp.StatusCode)
	}
	var conceptPayload map[string]map[string]any
	if err := json.NewDecoder(conceptResp.Body).Decode(&conceptPayload); err != nil {
		t.Fatalf("decode /meta/concepts/{name} response: %v", err)
	}
	if asString(conceptPayload["concept"]["name"]) != "compatibility" {
		t.Fatalf("unexpected concept lookup payload: %#v", conceptPayload)
	}
}

func TestVersionHeadersAndCLIOutdatedResponse(t *testing.T) {
	t.Parallel()

	h := newMetaStreamTestHarness(t,
		WithCoreVersion("2.0.0"),
		WithAPIVersion("v0"),
		WithMinCLIVersion("1.5.0"),
		WithRecommendedCLIVersion("1.7.0"),
		WithCLIDownloadURL("https://example.com/anx-cli"),
	)

	healthResp, err := http.Get(h.baseURL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer healthResp.Body.Close()
	if healthResp.Header.Get("X-ANX-Core-Version") != "2.0.0" {
		t.Fatalf("missing core version header: %#v", healthResp.Header)
	}
	if healthResp.Header.Get("X-ANX-Min-CLI-Version") != "1.5.0" {
		t.Fatalf("missing min cli version header: %#v", healthResp.Header)
	}

	versionReq, err := http.NewRequest(http.MethodGet, h.baseURL+"/version", nil)
	if err != nil {
		t.Fatalf("build /version request: %v", err)
	}
	versionReq.Header.Set("X-ANX-CLI-Version", "0.1.0")
	versionResp, err := http.DefaultClient.Do(versionReq)
	if err != nil {
		t.Fatalf("GET /version with old cli header: %v", err)
	}
	defer versionResp.Body.Close()
	if versionResp.StatusCode != http.StatusOK {
		t.Fatalf("expected /version to remain compatible, got %d", versionResp.StatusCode)
	}

	listThreadsReq, err := http.NewRequest(http.MethodGet, h.baseURL+"/threads", nil)
	if err != nil {
		t.Fatalf("build /threads request: %v", err)
	}
	listThreadsReq.Header.Set("X-ANX-CLI-Version", "1.4.9")
	listThreadsResp, err := http.DefaultClient.Do(listThreadsReq)
	if err != nil {
		t.Fatalf("GET /threads with old cli header: %v", err)
	}
	defer listThreadsResp.Body.Close()
	if listThreadsResp.StatusCode != http.StatusUpgradeRequired {
		t.Fatalf("expected 426 for outdated cli, got %d", listThreadsResp.StatusCode)
	}
	var outdatedPayload map[string]any
	if err := json.NewDecoder(listThreadsResp.Body).Decode(&outdatedPayload); err != nil {
		t.Fatalf("decode outdated payload: %v", err)
	}
	errorPayload, _ := outdatedPayload["error"].(map[string]any)
	if asString(errorPayload["code"]) != "cli_outdated" {
		t.Fatalf("unexpected outdated error payload: %#v", outdatedPayload)
	}
	upgradePayload, _ := outdatedPayload["upgrade"].(map[string]any)
	if asString(upgradePayload["min_cli_version"]) != "1.5.0" || asString(upgradePayload["recommended_cli_version"]) != "1.7.0" {
		t.Fatalf("unexpected upgrade payload: %#v", outdatedPayload)
	}
}

func TestEventsStreamResumesFromLastEventID(t *testing.T) {
	t.Parallel()

	h := newMetaStreamTestHarness(t, WithStreamPollInterval(20*time.Millisecond))
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-05T10:00:00Z"}}`, http.StatusCreated).Body.Close()

	firstEventID := appendEventForTest(t, h.baseURL, "actor-1", "thread-stream-1", "event one")
	secondEventID := appendEventForTest(t, h.baseURL, "actor-1", "thread-stream-1", "event two")

	resp := openSSEStream(t, h.baseURL+"/stream/events?thread_id=thread-stream-1", "")
	reader, stop := startSSEReader(resp.Body)
	defer stop()

	first := awaitSSEEvent(t, reader, 2*time.Second)
	second := awaitSSEEvent(t, reader, 2*time.Second)
	if first.ID != firstEventID || second.ID != secondEventID {
		t.Fatalf("unexpected initial stream order: first=%q second=%q expected=(%q,%q)", first.ID, second.ID, firstEventID, secondEventID)
	}

	resumeResp := openSSEStream(t, h.baseURL+"/stream/events?thread_id=thread-stream-1", firstEventID)
	resumeReader, resumeStop := startSSEReader(resumeResp.Body)
	defer resumeStop()

	resumed := awaitSSEEvent(t, resumeReader, 2*time.Second)
	if resumed.ID != secondEventID {
		t.Fatalf("expected resumed stream to continue after last event id, got %q want %q", resumed.ID, secondEventID)
	}

	select {
	case duplicate := <-resumeReader:
		t.Fatalf("unexpected duplicate event after resume with no new writes: %#v", duplicate)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestEventsStreamSurvivesServerWriteTimeout(t *testing.T) {
	t.Parallel()

	server, _ := newMetaStreamTestServer(t, func(server *httptest.Server) {
		server.Config.WriteTimeout = 150 * time.Millisecond
		server.Config.IdleTimeout = time.Second
	}, WithStreamPollInterval(20*time.Millisecond))

	postJSONExpectStatus(t, server.URL+"/actors", `{"actor":{"id":"actor-timeout","display_name":"Actor Timeout","created_at":"2026-03-05T10:00:00Z"}}`, http.StatusCreated).Body.Close()

	resp := openSSEStream(t, server.URL+"/stream/events?thread_id=thread-timeout-1", "")
	reader, stop := startSSEReader(resp.Body)
	defer stop()

	time.Sleep(250 * time.Millisecond)

	eventID := appendEventForTest(t, server.URL, "actor-timeout", "thread-timeout-1", "event after timeout window")
	event := awaitSSEEvent(t, reader, 2*time.Second)
	if event.ID != eventID {
		t.Fatalf("expected stream to survive write timeout window and deliver %q, got %q", eventID, event.ID)
	}
}

func TestEventsStreamEmitsDocumentLifecycleEventsForThread(t *testing.T) {
	t.Parallel()

	h := newMetaStreamTestHarness(t, WithStreamPollInterval(20*time.Millisecond))
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-05T10:00:00Z"}}`, http.StatusCreated).Body.Close()

	threadID := integrationSeedThreadWithStore(t, h.primitiveStore, nil, "actor-1", map[string]any{
		"title":            "Document stream thread",
		"type":             "incident",
		"status":           "active",
		"priority":         "p1",
		"tags":             []any{"docs"},
		"cadence":          "daily",
		"next_check_in_at": "2030-01-01T00:00:00Z",
		"current_summary":  "summary",
		"next_actions":     []any{"action"},
		"key_artifacts":    []any{},
		"provenance":       map[string]any{"sources": []any{"inferred"}},
	})

	timelineResp, err := http.Get(h.baseURL + "/threads/" + threadID + "/timeline")
	if err != nil {
		t.Fatalf("GET timeline before stream: %v", err)
	}
	defer timelineResp.Body.Close()
	var timeline struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(timelineResp.Body).Decode(&timeline); err != nil {
		t.Fatalf("decode initial timeline: %v", err)
	}
	if len(timeline.Events) == 0 {
		t.Fatal("expected initial thread event in timeline")
	}
	lastExistingEventID := asString(timeline.Events[len(timeline.Events)-1]["id"])
	if lastExistingEventID == "" {
		t.Fatalf("expected initial event id in timeline, got %#v", timeline.Events)
	}

	resp := openSSEStream(t, h.baseURL+"/stream/events?thread_id="+threadID, lastExistingEventID)
	reader, stop := startSSEReader(resp.Body)
	defer stop()

	createDocResp := postJSONExpectStatus(t, h.baseURL+"/docs", fmt.Sprintf(`{
		"actor_id":"actor-1",
		"document":{"id":"stream-doc-1","thread_id":"%s","title":"Stream Document"},
		"content":"draft v1",
		"content_type":"text"
	}`, threadID), http.StatusCreated)
	defer createDocResp.Body.Close()

	var createdDoc struct {
		Document map[string]any `json:"document"`
		Revision map[string]any `json:"revision"`
	}
	if err := json.NewDecoder(createDocResp.Body).Decode(&createdDoc); err != nil {
		t.Fatalf("decode create doc response: %v", err)
	}
	documentID := asString(createdDoc.Document["id"])
	createRevisionID := asString(createdDoc.Revision["revision_id"])
	createArtifactID := asString(createdDoc.Revision["artifact_id"])

	updateDocResp := requestJSONExpectStatus(t, http.MethodPost, h.baseURL+"/docs/"+documentID+"/revisions", fmt.Sprintf(`{
		"actor_id":"actor-1",
		"document":{"title":"Stream Document v2"},
		"if_base_revision":"%s",
		"content":"draft v2",
		"content_type":"text"
	}`, createRevisionID), http.StatusCreated)
	defer updateDocResp.Body.Close()

	var updatedDoc struct {
		Revision map[string]any `json:"revision"`
	}
	if err := json.NewDecoder(updateDocResp.Body).Decode(&updatedDoc); err != nil {
		t.Fatalf("decode update doc response: %v", err)
	}
	updateRevisionID := asString(updatedDoc.Revision["revision_id"])
	updateArtifactID := asString(updatedDoc.Revision["artifact_id"])

	trashResp := postJSONExpectStatus(t, h.baseURL+"/docs/"+documentID+"/trash", `{
		"actor_id":"actor-1",
		"reason":"stream verification"
	}`, http.StatusOK)
	defer trashResp.Body.Close()

	first := awaitSSEEvent(t, reader, 2*time.Second)
	second := awaitSSEEvent(t, reader, 2*time.Second)
	third := awaitSSEEvent(t, reader, 2*time.Second)

	events := []sseEvent{first, second, third}
	expectedTypes := []string{"document_created", "document_updated", "document_trashed"}
	for index, expectedType := range expectedTypes {
		eventPayload, _ := events[index].Data["event"].(map[string]any)
		if asString(eventPayload["type"]) != expectedType {
			t.Fatalf("unexpected streamed event type at index %d: got %#v want %q", index, eventPayload, expectedType)
		}
	}

	assertStreamDocLifecycleEvent(t, first.Data, threadID, documentID, createRevisionID, createArtifactID)
	assertStreamDocLifecycleEvent(t, second.Data, threadID, documentID, updateRevisionID, updateArtifactID)
	assertStreamDocLifecycleEvent(t, third.Data, threadID, documentID, updateRevisionID, updateArtifactID)
}

func TestInboxStreamSuppressesDuplicateItems(t *testing.T) {
	t.Parallel()

	h := newMetaStreamTestHarness(t, WithStreamPollInterval(20*time.Millisecond))
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-05T10:00:00Z"}}`, http.StatusCreated).Body.Close()

	threadID := integrationSeedThreadWithStore(t, h.primitiveStore, nil, "actor-1", map[string]any{
		"title":            "Inbox stream thread",
		"type":             "incident",
		"status":           "active",
		"priority":         "p1",
		"tags":             []any{"ops"},
		"cadence":          "daily",
		"next_check_in_at": "2030-01-01T00:00:00Z",
		"current_summary":  "summary",
		"next_actions":     []any{"action"},
		"key_artifacts":    []any{},
		"provenance":       map[string]any{"sources": []any{"inferred"}},
	})

	createBoardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{
		"actor_id":"actor-1",
		"board":{
			"title":"Inbox stream board",
			"refs":["thread:`+threadID+`"]
		}
	}`, http.StatusCreated)
	defer createBoardResp.Body.Close()
	var boardPayload struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(createBoardResp.Body).Decode(&boardPayload); err != nil {
		t.Fatalf("decode board response: %v", err)
	}
	boardID := asString(boardPayload.Board["id"])
	boardUpdatedAt := asString(boardPayload.Board["updated_at"])

	dueSoon := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339)
	postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards", `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+boardUpdatedAt+`",
		"title":"At risk work item",
		"related_refs":["thread:`+threadID+`"],
		"column_key":"ready",
		"due_at":"`+dueSoon+`"
	}`, http.StatusCreated).Body.Close()

	resp := openSSEStream(t, h.baseURL+"/stream/inbox", "")
	reader, stop := startSSEReader(resp.Body)
	defer stop()

	first := awaitSSEEvent(t, reader, 2*time.Second)
	if first.Event != "inbox_item" {
		t.Fatalf("unexpected inbox event type: %#v", first)
	}

	select {
	case duplicate := <-reader:
		t.Fatalf("unexpected duplicate inbox item event without any state change: %#v", duplicate)
	case <-time.After(300 * time.Millisecond):
	}
}

type metaStreamTestHarness struct {
	baseURL        string
	primitiveStore PrimitiveStore
}

func newMetaStreamTestHarness(t *testing.T, options ...HandlerOption) metaStreamTestHarness {
	t.Helper()

	server, store := newMetaStreamTestServer(t, nil, options...)
	return metaStreamTestHarness{baseURL: server.URL, primitiveStore: store}
}

func newMetaStreamTestServer(t *testing.T, configure func(*httptest.Server), options ...HandlerOption) (*httptest.Server, PrimitiveStore) {
	t.Helper()

	workspace, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("initialize workspace: %v", err)
	}

	contractPath := filepath.Join("..", "..", "..", "contracts", "anx-schema.yaml")
	contract, err := schema.Load(contractPath)
	if err != nil {
		_ = workspace.Close()
		t.Fatalf("load schema contract: %v", err)
	}

	registry := actors.NewStore(workspace.DB())
	if _, err := registry.EnsureSystemActor(context.Background(), time.Now().UTC()); err != nil {
		_ = workspace.Close()
		t.Fatalf("ensure system actor: %v", err)
	}
	authStore := auth.NewStore(workspace.DB())
	primitiveStore := primitives.NewStore(workspace.DB(), blob.NewFilesystemBackend(workspace.Layout().ArtifactContentDir), workspace.Layout().ArtifactContentDir)

	baseOptions := []HandlerOption{
		WithHealthCheck(workspace.Ping),
		WithActorRegistry(registry),
		WithAuthStore(authStore),
		WithPrimitiveStore(primitiveStore),
		WithSchemaContract(contract),
		WithAllowUnauthenticatedWrites(true),
		WithEnableDevActorMode(true),
	}
	baseOptions = append(baseOptions, options...)

	server := httptest.NewUnstartedServer(NewHandler(contract.Version, baseOptions...))
	if configure != nil {
		configure(server)
	}
	server.Start()
	t.Cleanup(func() {
		server.Close()
		_ = workspace.Close()
	})

	return server, primitiveStore
}

func appendEventForTest(t *testing.T, baseURL string, actorID string, threadID string, summary string) string {
	t.Helper()

	response := postJSONExpectStatus(t, baseURL+"/events", fmt.Sprintf(`{
		"actor_id":"%s",
		"event":{
			"type":"my_custom_event",
			"thread_id":"%s",
			"refs":["topic:%s"],
			"summary":%q,
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, actorID, threadID, threadID, summary), http.StatusCreated)
	defer response.Body.Close()

	var payload struct {
		Event map[string]any `json:"event"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		t.Fatalf("decode append event response: %v", err)
	}
	eventID := asString(payload.Event["id"])
	if eventID == "" {
		t.Fatalf("missing event id in payload: %#v", payload)
	}
	return eventID
}

func openSSEStream(t *testing.T, url string, lastEventID string) *http.Response {
	t.Helper()

	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("build stream request: %v", err)
	}
	if strings.TrimSpace(lastEventID) != "" {
		request.Header.Set("Last-Event-ID", lastEventID)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("open stream request: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("unexpected stream status: got %d body=%s", response.StatusCode, string(body))
	}
	return response
}

type sseEvent struct {
	ID    string
	Event string
	Data  map[string]any
}

func startSSEReader(body io.ReadCloser) (<-chan sseEvent, func()) {
	events := make(chan sseEvent, 16)

	go func() {
		defer close(events)
		scanner := bufio.NewScanner(body)
		var (
			currentID    string
			currentEvent string
			dataLines    []string
		)
		emit := func() {
			if len(dataLines) == 0 {
				currentID = ""
				currentEvent = ""
				dataLines = nil
				return
			}
			var payload map[string]any
			if err := json.Unmarshal([]byte(strings.Join(dataLines, "\n")), &payload); err == nil {
				events <- sseEvent{
					ID:    currentID,
					Event: currentEvent,
					Data:  payload,
				}
			}
			currentID = ""
			currentEvent = ""
			dataLines = nil
		}

		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				emit()
				continue
			}
			if strings.HasPrefix(line, ":") {
				continue
			}
			switch {
			case strings.HasPrefix(line, "id:"):
				currentID = strings.TrimSpace(strings.TrimPrefix(line, "id:"))
			case strings.HasPrefix(line, "event:"):
				currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			}
		}
		emit()
	}()

	stop := func() {
		_ = body.Close()
	}
	return events, stop
}

func awaitSSEEvent(t *testing.T, events <-chan sseEvent, timeout time.Duration) sseEvent {
	t.Helper()
	select {
	case event, ok := <-events:
		if !ok {
			t.Fatal("sse stream closed before receiving event")
		}
		return event
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for sse event after %s", timeout)
	}
	return sseEvent{}
}

func assertStreamDocLifecycleEvent(t *testing.T, data map[string]any, threadID, documentID, revisionID, artifactID string) {
	t.Helper()

	eventPayload, _ := data["event"].(map[string]any)
	refs, _ := eventPayload["refs"].([]any)
	if !containsSSERef(refs, "thread:"+threadID) {
		t.Fatalf("expected thread ref in streamed event, got %#v", eventPayload)
	}
	if !containsSSERef(refs, "document:"+documentID) {
		t.Fatalf("expected document ref in streamed event, got %#v", eventPayload)
	}
	if !containsSSERef(refs, "document_revision:"+revisionID) {
		t.Fatalf("expected document_revision ref in streamed event, got %#v", eventPayload)
	}
	if !containsSSERef(refs, "artifact:"+artifactID) {
		t.Fatalf("expected artifact ref in streamed event, got %#v", eventPayload)
	}
}

func containsSSERef(values []any, expected string) bool {
	for _, value := range values {
		if asString(value) == expected {
			return true
		}
	}
	return false
}
