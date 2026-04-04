package server

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"organization-autorunner-core/internal/blob"
	"organization-autorunner-core/internal/primitives"
	"organization-autorunner-core/internal/schema"
)

func TestTopicWorkspaceResolvesBoardsCardsAndDocsViaRefEdges(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createTopicResp := postJSONExpectStatus(t, h.baseURL+"/topics", `{
		"actor_id":"actor-1",
		"topic":{
			"type":"initiative",
			"status":"active",
			"title":"Launch topic",
			"summary":"Coordinate launch work",
			"owner_refs":["actor:actor-1"],
			"document_refs":[],
			"board_refs":[],
			"related_refs":[],
			"provenance":{"sources":["seed:topic-workspace"]}
		}
	}`, http.StatusCreated)
	defer createTopicResp.Body.Close()

	var createdTopic struct {
		Topic map[string]any `json:"topic"`
	}
	if err := json.NewDecoder(createTopicResp.Body).Decode(&createdTopic); err != nil {
		t.Fatalf("decode create topic response: %v", err)
	}
	topicID := asString(createdTopic.Topic["id"])
	primaryThreadID := asString(createdTopic.Topic["thread_id"])
	if topicID == "" || primaryThreadID == "" {
		t.Fatalf("expected topic id and primary thread, got %#v", createdTopic.Topic)
	}

	memberThreadID := createBoardThreadViaHTTP(t, h, "Topic member thread")
	documentThreadID := createBoardThreadViaHTTP(t, h, "Topic document thread")
	topicDocumentID := createBoardDocumentViaHTTP(t, h, documentThreadID, "Topic context doc")
	memberDocumentID := createBoardDocumentViaHTTP(t, h, memberThreadID, "Topic member doc")

	createBoardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{
		"actor_id":"actor-1",
		"board":{
			"title":"Launch board",
			"refs":["thread:`+primaryThreadID+`","topic:`+topicID+`"],
			"document_refs":["document:`+topicDocumentID+`"]
		}
	}`, http.StatusCreated)
	defer createBoardResp.Body.Close()
	var createdBoard struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(createBoardResp.Body).Decode(&createdBoard); err != nil {
		t.Fatalf("decode create board response: %v", err)
	}
	boardID := asString(createdBoard.Board["id"])
	boardUpdatedAt := asString(createdBoard.Board["updated_at"])
	if boardID == "" || boardUpdatedAt == "" {
		t.Fatalf("expected board id and updated_at, got %#v", createdBoard.Board)
	}

	addCardResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards", `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+boardUpdatedAt+`",
		"parent_thread":"`+primaryThreadID+`",
		"column_key":"ready",
		"pinned_document_id":"`+memberDocumentID+`"
	}`, http.StatusCreated)
	defer addCardResp.Body.Close()
	var createdCard struct {
		Card map[string]any `json:"card"`
	}
	if err := json.NewDecoder(addCardResp.Body).Decode(&createdCard); err != nil {
		t.Fatalf("decode add card response: %v", err)
	}
	cardID := asString(createdCard.Card["id"])
	if cardID == "" {
		t.Fatal("expected card id")
	}

	contract, err := schema.Load(filepath.Join("..", "..", "..", "contracts", "oar-schema.yaml"))
	if err != nil {
		t.Fatalf("load schema contract: %v", err)
	}
	opts := handlerOptions{
		primitiveStore: primitives.NewStore(h.workspace.DB(), blob.NewFilesystemBackend(h.workspace.Layout().ArtifactContentDir), h.workspace.Layout().ArtifactContentDir),
		contract:       contract,
	}
	workspace, err := buildTopicWorkspacePayload(context.Background(), opts, topicID)
	if err != nil {
		t.Fatalf("buildTopicWorkspacePayload: %v", err)
	}

	boards, _ := workspace["boards"].([]map[string]any)
	cards, _ := workspace["cards"].([]map[string]any)
	documents, _ := workspace["documents"].([]map[string]any)
	threads, _ := workspace["threads"].([]map[string]any)
	if len(boards) != 1 || len(cards) != 1 {
		t.Fatalf("expected one linked board/card, got boards=%#v cards=%#v", boards, cards)
	}
	if asString(cards[0]["id"]) != cardID {
		t.Fatalf("expected workspace card %q, got %#v", cardID, cards)
	}
	if !containsResourceID(documents, topicDocumentID) || !containsResourceID(documents, memberDocumentID) {
		t.Fatalf("expected topic/member documents in workspace, got %#v", documents)
	}
	if !containsResourceID(threads, primaryThreadID) || !containsResourceID(threads, memberThreadID) {
		t.Fatalf("expected primary/member threads in workspace, got %#v", threads)
	}
}

func asStringFromTypedRef(ref string) string {
	if ref == "" {
		return ""
	}
	if _, id, err := schema.SplitTypedRef(ref); err == nil {
		return id
	}
	return ""
}

func containsResourceID(items []map[string]any, id string) bool {
	for _, item := range items {
		if asString(item["id"]) == id {
			return true
		}
	}
	return false
}
