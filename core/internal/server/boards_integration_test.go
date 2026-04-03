package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestBoardsWorkspaceAndThreadWorkspaceMemberships(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	primaryThreadID := createBoardThreadViaHTTP(t, h, "Board primary thread")
	memberThreadID := createBoardThreadViaHTTP(t, h, "Board member thread")
	secondPrimaryThreadID := createBoardThreadViaHTTP(t, h, "Second board primary thread")

	primaryDocumentID := createBoardDocumentViaHTTP(t, h, primaryThreadID, "Primary board doc")
	memberDocumentID := createBoardDocumentViaHTTP(t, h, memberThreadID, "Member board doc")

	postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"decision_needed",
			"thread_id":"`+memberThreadID+`",
			"refs":["topic:`+memberThreadID+`"],
			"summary":"Need member-thread decision",
			"payload":{"decision":"Approve board work"},
			"provenance":{"sources":["seed:board-workspace"]}
		}
	}`, http.StatusCreated).Body.Close()

	createBoardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{
		"actor_id":"actor-1",
		"board":{
			"title":"Ops Board",
			"labels":["ops","planning"],
			"owners":["actor-1"],
			"primary_thread_id":"`+primaryThreadID+`",
			"primary_document_id":"`+primaryDocumentID+`",
			"pinned_refs":["thread:`+primaryThreadID+`"]
		}
	}`, http.StatusCreated)
	defer createBoardResp.Body.Close()

	var createBoardPayload struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(createBoardResp.Body).Decode(&createBoardPayload); err != nil {
		t.Fatalf("decode create board response: %v", err)
	}
	boardID := asString(createBoardPayload.Board["id"])
	if boardID == "" {
		t.Fatal("expected created board id")
	}
	boardUpdatedAt := asString(createBoardPayload.Board["updated_at"])

	addCardResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards", `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+boardUpdatedAt+`",
		"thread_id":"`+memberThreadID+`",
		"column_key":"ready",
		"pinned_document_id":"`+memberDocumentID+`",
		"due_at":"`+time.Now().UTC().Add(24*time.Hour).Format(time.RFC3339)+`"
	}`, http.StatusCreated)
	defer addCardResp.Body.Close()

	var addCardPayload struct {
		Board map[string]any `json:"board"`
		Card  map[string]any `json:"card"`
	}
	if err := json.NewDecoder(addCardResp.Body).Decode(&addCardPayload); err != nil {
		t.Fatalf("decode add card response: %v", err)
	}
	if asString(addCardPayload.Card["parent_thread"]) != memberThreadID || asString(addCardPayload.Card["column_key"]) != "ready" {
		t.Fatalf("unexpected board card payload: %#v", addCardPayload.Card)
	}

	listResp, err := http.Get(h.baseURL + "/boards?status=active&label=ops&owner=actor-1")
	if err != nil {
		t.Fatalf("GET /boards: %v", err)
	}
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected boards list status: got %d", listResp.StatusCode)
	}

	var listPayload struct {
		Boards []struct {
			Board   map[string]any `json:"board"`
			Summary map[string]any `json:"summary"`
		} `json:"boards"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listPayload); err != nil {
		t.Fatalf("decode boards list response: %v", err)
	}
	if len(listPayload.Boards) != 1 {
		t.Fatalf("expected one board in filtered list, got %d", len(listPayload.Boards))
	}
	summary := listPayload.Boards[0].Summary
	if got := intFromAny(summary["card_count"]); got != 1 {
		t.Fatalf("expected card_count=1, got %#v", summary["card_count"])
	}
	if got := intFromAny(summary["at_risk_card_count"]); got != 1 {
		t.Fatalf("expected at_risk_card_count=1, got %#v", summary["at_risk_card_count"])
	}
	if got := intFromAny(summary["document_count"]); got != 1 {
		t.Fatalf("expected document_count=1, got %#v", summary["document_count"])
	}
	if got := summary["has_primary_document"]; got != true {
		t.Fatalf("expected has_primary_document=true, got %#v", got)
	}
	cardsByColumn, ok := summary["cards_by_column"].(map[string]any)
	if !ok || intFromAny(cardsByColumn["ready"]) != 1 {
		t.Fatalf("unexpected cards_by_column summary: %#v", summary["cards_by_column"])
	}

	workspaceResp, err := http.Get(h.baseURL + "/boards/" + boardID + "/workspace")
	if err != nil {
		t.Fatalf("GET /boards/{id}/workspace: %v", err)
	}
	defer workspaceResp.Body.Close()
	if workspaceResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected board workspace status: got %d", workspaceResp.StatusCode)
	}

	var workspacePayload struct {
		BoardID         string         `json:"board_id"`
		Board           map[string]any `json:"board"`
		PrimaryThread   map[string]any `json:"primary_thread"`
		PrimaryDocument map[string]any `json:"primary_document"`
		Cards           struct {
			Items []struct {
				Membership map[string]any `json:"membership"`
				Backing    struct {
					Thread         map[string]any `json:"thread"`
					PinnedDocument map[string]any `json:"pinned_document"`
				} `json:"backing"`
				Derived struct {
					Summary   map[string]any `json:"summary"`
					Freshness map[string]any `json:"freshness"`
				} `json:"derived"`
			} `json:"items"`
			Count int `json:"count"`
		} `json:"cards"`
		Documents struct {
			Items []map[string]any `json:"items"`
			Count int              `json:"count"`
		} `json:"documents"`
		Commitments struct {
			Items []map[string]any `json:"items"`
			Count int              `json:"count"`
		} `json:"commitments"`
		Inbox struct {
			Items []map[string]any `json:"items"`
			Count int              `json:"count"`
		} `json:"inbox"`
		BoardSummary          map[string]any `json:"board_summary"`
		ProjectionFreshness   map[string]any `json:"projection_freshness"`
		BoardSummaryFreshness map[string]any `json:"board_summary_freshness"`
		Warnings              struct {
			Count int `json:"count"`
		} `json:"warnings"`
		SectionKinds map[string]string `json:"section_kinds"`
	}
	if err := json.NewDecoder(workspaceResp.Body).Decode(&workspacePayload); err != nil {
		t.Fatalf("decode board workspace response: %v", err)
	}
	if workspacePayload.BoardID != boardID || asString(workspacePayload.Board["id"]) != boardID {
		t.Fatalf("unexpected board workspace id payload: %#v", workspacePayload)
	}
	if asString(workspacePayload.PrimaryThread["id"]) != boardID {
		t.Fatalf("unexpected primary_thread payload: %#v", workspacePayload.PrimaryThread)
	}
	if asString(workspacePayload.PrimaryDocument["id"]) != primaryDocumentID {
		t.Fatalf("unexpected primary_document payload: %#v", workspacePayload.PrimaryDocument)
	}
	if workspacePayload.Cards.Count != 1 || len(workspacePayload.Cards.Items) != 1 {
		t.Fatalf("expected one board workspace card, got %#v", workspacePayload.Cards)
	}
	cardItem := workspacePayload.Cards.Items[0]
	if asString(cardItem.Membership["parent_thread"]) != memberThreadID || asString(cardItem.Backing.Thread["id"]) == "" {
		t.Fatalf("unexpected board workspace card item: %#v", cardItem)
	}
	if asString(cardItem.Backing.PinnedDocument["id"]) != memberDocumentID {
		t.Fatalf("expected pinned document %q, got %#v", memberDocumentID, cardItem.Backing.PinnedDocument)
	}
	if asString(cardItem.Derived.Summary["risk_state"]) != "due_soon" || intFromAny(cardItem.Derived.Summary["document_count"]) != 0 || intFromAny(cardItem.Derived.Summary["inbox_count"]) != 0 {
		t.Fatalf("unexpected board card summary: %#v", cardItem.Derived.Summary)
	}
	if got := asString(cardItem.Derived.Freshness["status"]); got != "current" {
		t.Fatalf("expected card freshness current, got %#v", cardItem.Derived.Freshness)
	}
	if workspacePayload.Documents.Count != 1 || workspacePayload.Commitments.Count != 0 || workspacePayload.Inbox.Count != 0 {
		t.Fatalf("unexpected workspace aggregate sections: documents=%#v commitments=%#v inbox=%#v", workspacePayload.Documents, workspacePayload.Commitments, workspacePayload.Inbox)
	}
	if intFromAny(workspacePayload.BoardSummary["card_count"]) != 1 || intFromAny(workspacePayload.BoardSummary["at_risk_card_count"]) != 1 || intFromAny(workspacePayload.BoardSummary["document_count"]) != 1 {
		t.Fatalf("unexpected board summary: %#v", workspacePayload.BoardSummary)
	}
	if got := asString(workspacePayload.ProjectionFreshness["status"]); got != "current" {
		t.Fatalf("expected board projection freshness current, got %#v", workspacePayload.ProjectionFreshness)
	}
	if got := asString(workspacePayload.BoardSummaryFreshness["status"]); got != "current" {
		t.Fatalf("expected board summary freshness current, got %#v", workspacePayload.BoardSummaryFreshness)
	}
	if workspacePayload.Warnings.Count != 0 {
		t.Fatalf("expected warnings.count=0, got %#v", workspacePayload.Warnings)
	}
	if workspacePayload.SectionKinds["board"] != "canonical" ||
		workspacePayload.SectionKinds["cards"] != "convenience" ||
		workspacePayload.SectionKinds["documents"] != "derived" ||
		workspacePayload.SectionKinds["commitments"] != "derived" ||
		workspacePayload.SectionKinds["inbox"] != "derived" ||
		workspacePayload.SectionKinds["board_summary"] != "derived" {
		t.Fatalf("unexpected board workspace section kinds: %#v", workspacePayload.SectionKinds)
	}

	primaryThreadWorkspaceResp, err := http.Get(h.baseURL + "/threads/" + primaryThreadID + "/workspace")
	if err != nil {
		t.Fatalf("GET /threads/{id}/workspace for primary thread: %v", err)
	}
	defer primaryThreadWorkspaceResp.Body.Close()
	if primaryThreadWorkspaceResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected primary thread workspace status: got %d", primaryThreadWorkspaceResp.StatusCode)
	}
	var primaryThreadWorkspace struct {
		BoardMemberships struct {
			Items []map[string]any `json:"items"`
			Count int              `json:"count"`
		} `json:"board_memberships"`
		SectionKinds map[string]string `json:"section_kinds"`
	}
	if err := json.NewDecoder(primaryThreadWorkspaceResp.Body).Decode(&primaryThreadWorkspace); err != nil {
		t.Fatalf("decode primary thread workspace response: %v", err)
	}
	if primaryThreadWorkspace.BoardMemberships.Count != 0 {
		t.Fatalf("expected zero board memberships for primary thread, got %#v", primaryThreadWorkspace.BoardMemberships)
	}
	if primaryThreadWorkspace.SectionKinds["board_memberships"] != "canonical" {
		t.Fatalf("expected board_memberships section kind canonical, got %#v", primaryThreadWorkspace.SectionKinds)
	}

	secondBoardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{
		"actor_id":"actor-1",
		"board":{
			"title":"Shared Membership Board",
			"primary_thread_id":"`+secondPrimaryThreadID+`"
		}
	}`, http.StatusCreated)
	defer secondBoardResp.Body.Close()
	var secondBoardPayload struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(secondBoardResp.Body).Decode(&secondBoardPayload); err != nil {
		t.Fatalf("decode second board response: %v", err)
	}
	secondBoardID := asString(secondBoardPayload.Board["id"])
	secondBoardUpdatedAt := asString(secondBoardPayload.Board["updated_at"])

	postJSONExpectStatus(t, h.baseURL+"/boards/"+secondBoardID+"/cards", `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+secondBoardUpdatedAt+`",
		"thread_id":"`+memberThreadID+`",
		"column_key":"review"
	}`, http.StatusCreated).Body.Close()

	memberThreadWorkspaceResp, err := http.Get(h.baseURL + "/threads/" + memberThreadID + "/workspace")
	if err != nil {
		t.Fatalf("GET /threads/{id}/workspace for member thread: %v", err)
	}
	defer memberThreadWorkspaceResp.Body.Close()
	if memberThreadWorkspaceResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected member thread workspace status: got %d", memberThreadWorkspaceResp.StatusCode)
	}
	var memberThreadWorkspace struct {
		BoardMemberships struct {
			Items []struct {
				Board map[string]any `json:"board"`
				Card  map[string]any `json:"card"`
			} `json:"items"`
			Count int `json:"count"`
		} `json:"board_memberships"`
	}
	if err := json.NewDecoder(memberThreadWorkspaceResp.Body).Decode(&memberThreadWorkspace); err != nil {
		t.Fatalf("decode member thread workspace response: %v", err)
	}
	if memberThreadWorkspace.BoardMemberships.Count != 2 {
		t.Fatalf("expected two board memberships for shared thread, got %#v", memberThreadWorkspace.BoardMemberships)
	}
	boardIDs := []string{
		asString(memberThreadWorkspace.BoardMemberships.Items[0].Board["id"]),
		asString(memberThreadWorkspace.BoardMemberships.Items[1].Board["id"]),
	}
	if !containsAllStrings(boardIDs, []string{boardID, secondBoardID}) {
		t.Fatalf("unexpected board membership board ids: %#v", boardIDs)
	}
	for _, membership := range memberThreadWorkspace.BoardMemberships.Items {
		if asString(membership.Card["parent_thread"]) != memberThreadID {
			t.Fatalf("expected board membership parent_thread %q, got %#v", memberThreadID, membership.Card)
		}
	}
}

func TestBoardLifecycleEventsAndConflictValidation(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	primaryThreadID := createBoardThreadViaHTTP(t, h, "Lifecycle primary thread")
	memberThreadID := createBoardThreadViaHTTP(t, h, "Lifecycle member thread")
	primaryDocumentID := createBoardDocumentViaHTTP(t, h, primaryThreadID, "Lifecycle primary doc")
	pinnedDocumentID := createBoardDocumentViaHTTP(t, h, memberThreadID, "Lifecycle pinned doc")

	createBoardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{
		"actor_id":"actor-1",
		"board":{
			"title":"Lifecycle Board",
			"primary_thread_id":"`+primaryThreadID+`",
			"primary_document_id":"`+primaryDocumentID+`"
		}
	}`, http.StatusCreated)
	defer createBoardResp.Body.Close()
	var createBoardPayload struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(createBoardResp.Body).Decode(&createBoardPayload); err != nil {
		t.Fatalf("decode create board response: %v", err)
	}
	boardID := asString(createBoardPayload.Board["id"])
	initialBoardUpdatedAt := asString(createBoardPayload.Board["updated_at"])

	updateBoardResp := patchJSONExpectStatus(t, h.baseURL+"/boards/"+boardID, `{
		"actor_id":"actor-1",
		"if_updated_at":"`+initialBoardUpdatedAt+`",
		"patch":{"title":"Lifecycle Board Updated"}
	}`, http.StatusOK)
	defer updateBoardResp.Body.Close()
	var updateBoardPayload struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(updateBoardResp.Body).Decode(&updateBoardPayload); err != nil {
		t.Fatalf("decode update board response: %v", err)
	}
	afterBoardUpdate := asString(updateBoardPayload.Board["updated_at"])

	conflictBoardResp := patchJSONExpectStatus(t, h.baseURL+"/boards/"+boardID, `{
		"actor_id":"actor-1",
		"if_updated_at":"`+initialBoardUpdatedAt+`",
		"patch":{"status":"paused"}
	}`, http.StatusConflict)
	conflictBoardResp.Body.Close()

	addCardResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards", `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+afterBoardUpdate+`",
		"thread_id":"`+memberThreadID+`",
		"column_key":"backlog"
	}`, http.StatusCreated)
	defer addCardResp.Body.Close()
	var addCardPayload struct {
		Board map[string]any `json:"board"`
		Card  map[string]any `json:"card"`
	}
	if err := json.NewDecoder(addCardResp.Body).Decode(&addCardPayload); err != nil {
		t.Fatalf("decode add card response: %v", err)
	}
	afterCardAdd := asString(addCardPayload.Board["updated_at"])
	time.Sleep(2 * time.Millisecond)

	duplicateAddResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards", `{
		"actor_id":"actor-1",
		"thread_id":"`+memberThreadID+`"
	}`, http.StatusConflict)
	duplicateAddResp.Body.Close()

	invalidPrimaryCardResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards", `{
		"actor_id":"actor-1",
		"thread_id":"`+boardID+`"
	}`, http.StatusBadRequest)
	invalidPrimaryCardResp.Body.Close()

	updateCardResp := patchJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards/"+memberThreadID, `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+afterCardAdd+`",
		"patch":{"pinned_document_id":"`+pinnedDocumentID+`"}
	}`, http.StatusOK)
	defer updateCardResp.Body.Close()
	var updateCardPayload struct {
		Board map[string]any `json:"board"`
		Card  map[string]any `json:"card"`
	}
	if err := json.NewDecoder(updateCardResp.Body).Decode(&updateCardPayload); err != nil {
		t.Fatalf("decode update card response: %v", err)
	}
	afterCardUpdate := asString(updateCardPayload.Board["updated_at"])

	staleUpdateResp := patchJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards/"+memberThreadID, `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+afterCardAdd+`",
		"patch":{"status":"done"}
	}`, http.StatusConflict)
	defer staleUpdateResp.Body.Close()
	assertErrorCode(t, staleUpdateResp, "conflict")

	moveCardResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards/"+memberThreadID+"/move", `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+afterCardUpdate+`",
		"column_key":"blocked"
	}`, http.StatusOK)
	defer moveCardResp.Body.Close()
	var moveCardPayload struct {
		Board map[string]any `json:"board"`
		Card  map[string]any `json:"card"`
	}
	if err := json.NewDecoder(moveCardResp.Body).Decode(&moveCardPayload); err != nil {
		t.Fatalf("decode move card response: %v", err)
	}
	if asString(moveCardPayload.Card["column_key"]) != "blocked" {
		t.Fatalf("expected moved card in blocked column, got %#v", moveCardPayload.Card)
	}
	afterMove := asString(moveCardPayload.Board["updated_at"])

	staleMoveResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards/"+memberThreadID+"/move", `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+afterCardUpdate+`",
		"column_key":"review"
	}`, http.StatusConflict)
	staleMoveResp.Body.Close()

	deleteCardResp := requestJSONExpectStatus(t, http.MethodDelete, h.baseURL+"/boards/"+boardID+"/cards/"+memberThreadID, ``, http.StatusMethodNotAllowed)
	deleteCardResp.Body.Close()

	removeCardResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards/"+memberThreadID+"/remove", `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+afterMove+`"
	}`, http.StatusOK)
	defer removeCardResp.Body.Close()
	var removeCardPayload struct {
		Board           map[string]any `json:"board"`
		Card            map[string]any `json:"card"`
		RemovedThreadID string         `json:"removed_thread_id"`
	}
	if err := json.NewDecoder(removeCardResp.Body).Decode(&removeCardPayload); err != nil {
		t.Fatalf("decode remove card response: %v", err)
	}
	if removeCardPayload.RemovedThreadID != memberThreadID {
		t.Fatalf("unexpected removed_thread_id: %#v", removeCardPayload)
	}
	if asString(removeCardPayload.Card["parent_thread"]) != memberThreadID {
		t.Fatalf("expected removed card parent_thread %q, got %#v", memberThreadID, removeCardPayload.Card)
	}

	timelineResp, err := http.Get(h.baseURL + "/threads/" + boardID + "/timeline")
	if err != nil {
		t.Fatalf("GET /threads/{id}/timeline: %v", err)
	}
	defer timelineResp.Body.Close()
	if timelineResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected timeline status: got %d", timelineResp.StatusCode)
	}
	var timelinePayload struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(timelineResp.Body).Decode(&timelinePayload); err != nil {
		t.Fatalf("decode timeline response: %v", err)
	}
	assertTimelineStableOrder(t, timelinePayload.Events)

	boardEventsByType := map[string]map[string]any{}
	for _, event := range timelinePayload.Events {
		eventType := asString(event["type"])
		if strings.HasPrefix(eventType, "board_") {
			boardEventsByType[eventType] = event
		}
	}
	expectedBoardEvents := []string{"board_created", "board_updated", "board_card_added", "board_card_updated", "board_card_moved", "board_card_removed"}
	for _, eventType := range expectedBoardEvents {
		event, ok := boardEventsByType[eventType]
		if !ok {
			t.Fatalf("expected board lifecycle event %q in primary thread timeline, got %#v", eventType, mapKeysMapAny(boardEventsByType))
		}
		refs, ok := event["refs"].([]any)
		if !ok {
			t.Fatalf("expected refs on board lifecycle event %q, got %#v", eventType, event["refs"])
		}
		if !containsAny(refs, "board:"+boardID) {
			t.Fatalf("expected board ref on %q, got %#v", eventType, refs)
		}
	}
	if !containsAny(boardEventsByType["board_created"]["refs"].([]any), "thread:"+primaryThreadID) || !containsAny(boardEventsByType["board_created"]["refs"].([]any), "document:"+primaryDocumentID) {
		t.Fatalf("expected primary thread/document refs on board_created, got %#v", boardEventsByType["board_created"]["refs"])
	}
	if !containsAny(boardEventsByType["board_card_updated"]["refs"].([]any), "document:"+pinnedDocumentID) {
		t.Fatalf("expected pinned document ref on board_card_updated, got %#v", boardEventsByType["board_card_updated"]["refs"])
	}
	if !containsAny(boardEventsByType["board_card_moved"]["refs"].([]any), "card:"+asString(addCardPayload.Card["id"])) || !containsAny(boardEventsByType["board_card_removed"]["refs"].([]any), "card:"+asString(addCardPayload.Card["id"])) {
		t.Fatalf("expected card refs on move/remove events, got moved=%#v removed=%#v", boardEventsByType["board_card_moved"]["refs"], boardEventsByType["board_card_removed"]["refs"])
	}
}

func TestArchiveBoardCardGlobalRoute(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()

	primaryThreadID := createBoardThreadViaHTTP(t, h, "Archive primary thread")
	memberThreadID := createBoardThreadViaHTTP(t, h, "Archive member thread")

	createBoardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{
		"actor_id":"actor-1",
		"board":{
			"title":"Archive Board",
			"primary_thread_id":"`+primaryThreadID+`"
		}
	}`, http.StatusCreated)
	defer createBoardResp.Body.Close()

	var createBoardPayload struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(createBoardResp.Body).Decode(&createBoardPayload); err != nil {
		t.Fatalf("decode create board response: %v", err)
	}
	boardID := asString(createBoardPayload.Board["id"])
	boardUpdatedAt := asString(createBoardPayload.Board["updated_at"])

	addCardResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards", `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+boardUpdatedAt+`",
		"thread_id":"`+memberThreadID+`",
		"column_key":"review"
	}`, http.StatusCreated)
	defer addCardResp.Body.Close()

	var addCardPayload struct {
		Board map[string]any `json:"board"`
		Card  map[string]any `json:"card"`
	}
	if err := json.NewDecoder(addCardResp.Body).Decode(&addCardPayload); err != nil {
		t.Fatalf("decode add card response: %v", err)
	}
	cardID := asString(addCardPayload.Card["id"])
	if cardID == "" {
		t.Fatalf("expected card id in add response: %#v", addCardPayload.Card)
	}
	afterAdd := asString(addCardPayload.Board["updated_at"])

	archiveResp := postJSONExpectStatus(t, h.baseURL+"/cards/"+cardID+"/archive", `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+afterAdd+`"
	}`, http.StatusOK)
	defer archiveResp.Body.Close()

	var archivePayload struct {
		Board map[string]any `json:"board"`
		Card  map[string]any `json:"card"`
	}
	if err := json.NewDecoder(archiveResp.Body).Decode(&archivePayload); err != nil {
		t.Fatalf("decode archive response: %v", err)
	}
	if asString(archivePayload.Card["id"]) != cardID {
		t.Fatalf("expected archived card id %q, got %#v", cardID, archivePayload.Card["id"])
	}
	if asString(archivePayload.Card["board_id"]) != boardID {
		t.Fatalf("expected archived board id %q, got %#v", boardID, archivePayload.Card["board_id"])
	}
	if asString(archivePayload.Card["archived_at"]) == "" {
		t.Fatalf("expected archived_at on archive response, got %#v", archivePayload.Card)
	}

	cardsResp, err := http.Get(h.baseURL + "/boards/" + boardID + "/cards")
	if err != nil {
		t.Fatalf("GET /boards/{id}/cards: %v", err)
	}
	defer cardsResp.Body.Close()
	if cardsResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected board cards status: got %d", cardsResp.StatusCode)
	}

	var cardsPayload struct {
		Cards []map[string]any `json:"cards"`
	}
	if err := json.NewDecoder(cardsResp.Body).Decode(&cardsPayload); err != nil {
		t.Fatalf("decode cards list response: %v", err)
	}
	if len(cardsPayload.Cards) != 0 {
		t.Fatalf("expected archived card to be absent from active board cards, got %#v", cardsPayload.Cards)
	}

	patchArchivedResp := patchJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards/"+memberThreadID, `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+asString(archivePayload.Board["updated_at"])+`",
		"patch":{"status":"done"}
	}`, http.StatusBadRequest)
	defer patchArchivedResp.Body.Close()
	assertErrorCode(t, patchArchivedResp, "invalid_request")

	moveArchivedResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards/"+memberThreadID+"/move", `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+asString(archivePayload.Board["updated_at"])+`",
		"column_key":"done"
	}`, http.StatusBadRequest)
	defer moveArchivedResp.Body.Close()
	assertErrorCode(t, moveArchivedResp, "invalid_request")
}

func TestBoardCardPatchAllowsContractValidNoOpShapes(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	primaryThreadID := createBoardThreadViaHTTP(t, h, "Patch primary thread")
	memberThreadID := createBoardThreadViaHTTP(t, h, "Patch member thread")

	createBoardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{
		"actor_id":"actor-1",
		"board":{
			"title":"Patch Board",
			"primary_thread_id":"`+primaryThreadID+`"
		}
	}`, http.StatusCreated)
	defer createBoardResp.Body.Close()

	var createBoardPayload struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(createBoardResp.Body).Decode(&createBoardPayload); err != nil {
		t.Fatalf("decode create board response: %v", err)
	}
	boardID := asString(createBoardPayload.Board["id"])
	boardUpdatedAt := asString(createBoardPayload.Board["updated_at"])

	addCardResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards", `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+boardUpdatedAt+`",
		"thread_id":"`+memberThreadID+`",
		"column_key":"ready"
	}`, http.StatusCreated)
	defer addCardResp.Body.Close()

	var addCardPayload struct {
		Board map[string]any `json:"board"`
		Card  map[string]any `json:"card"`
	}
	if err := json.NewDecoder(addCardResp.Body).Decode(&addCardPayload); err != nil {
		t.Fatalf("decode add card response: %v", err)
	}
	cardUpdatedAt := asString(addCardPayload.Board["updated_at"])
	cardStatus := asString(addCardPayload.Card["status"])

	noopPatchResp := patchJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards/"+memberThreadID, `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+cardUpdatedAt+`",
		"patch":{}
	}`, http.StatusOK)
	defer noopPatchResp.Body.Close()

	var noopPatchPayload struct {
		Board map[string]any `json:"board"`
		Card  map[string]any `json:"card"`
	}
	if err := json.NewDecoder(noopPatchResp.Body).Decode(&noopPatchPayload); err != nil {
		t.Fatalf("decode noop patch response: %v", err)
	}
	if asString(noopPatchPayload.Board["updated_at"]) != cardUpdatedAt {
		t.Fatalf("expected noop patch to keep board updated_at, got %#v want %#v", noopPatchPayload.Board["updated_at"], cardUpdatedAt)
	}
	if got := noopPatchPayload.Card["pinned_document_id"]; got != nil {
		t.Fatalf("expected noop patch to keep pinned document nil, got %#v", got)
	}

	staleNoopPatchResp := patchJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards/"+memberThreadID, `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+boardUpdatedAt+`",
		"patch":{}
	}`, http.StatusConflict)
	defer staleNoopPatchResp.Body.Close()
	assertErrorCode(t, staleNoopPatchResp, "conflict")

	futurePatchResp := patchJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards/"+memberThreadID, `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+cardUpdatedAt+`",
		"patch":{"future_field":"ignored"}
	}`, http.StatusOK)
	defer futurePatchResp.Body.Close()

	var futurePatchPayload struct {
		Board map[string]any `json:"board"`
		Card  map[string]any `json:"card"`
	}
	if err := json.NewDecoder(futurePatchResp.Body).Decode(&futurePatchPayload); err != nil {
		t.Fatalf("decode future patch response: %v", err)
	}
	if asString(futurePatchPayload.Board["updated_at"]) != cardUpdatedAt {
		t.Fatalf("expected unknown-field patch to keep board updated_at, got %#v want %#v", futurePatchPayload.Board["updated_at"], cardUpdatedAt)
	}
	if got := futurePatchPayload.Card["pinned_document_id"]; got != nil {
		t.Fatalf("expected unknown-field patch to keep pinned document nil, got %#v", got)
	}

	sameValuePatchResp := patchJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards/"+memberThreadID, `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+cardUpdatedAt+`",
		"patch":{"status":"`+cardStatus+`"}
	}`, http.StatusOK)
	defer sameValuePatchResp.Body.Close()

	var sameValuePatchPayload struct {
		Board map[string]any `json:"board"`
		Card  map[string]any `json:"card"`
	}
	if err := json.NewDecoder(sameValuePatchResp.Body).Decode(&sameValuePatchPayload); err != nil {
		t.Fatalf("decode same-value patch response: %v", err)
	}
	if asString(sameValuePatchPayload.Board["updated_at"]) != cardUpdatedAt {
		t.Fatalf("expected same-value patch to keep board updated_at, got %#v want %#v", sameValuePatchPayload.Board["updated_at"], cardUpdatedAt)
	}
	if asString(sameValuePatchPayload.Card["status"]) != cardStatus {
		t.Fatalf("expected same-value patch to keep status %q, got %#v", cardStatus, sameValuePatchPayload.Card["status"])
	}

	mismatchedAliasResp := patchJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards/"+memberThreadID, `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+cardUpdatedAt+`",
		"patch":{"parent_thread":"thread-parent-a","thread_id":"thread-parent-b"}
	}`, http.StatusBadRequest)
	defer mismatchedAliasResp.Body.Close()
	assertErrorCode(t, mismatchedAliasResp, "invalid_request")

	timelineResp, err := http.Get(h.baseURL + "/threads/" + primaryThreadID + "/timeline")
	if err != nil {
		t.Fatalf("GET /threads/{id}/timeline: %v", err)
	}
	defer timelineResp.Body.Close()
	if timelineResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected timeline status: got %d", timelineResp.StatusCode)
	}
	var timelinePayload struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(timelineResp.Body).Decode(&timelinePayload); err != nil {
		t.Fatalf("decode timeline response: %v", err)
	}
	assertTimelineStableOrder(t, timelinePayload.Events)
	for _, event := range timelinePayload.Events {
		if asString(event["type"]) == "board_card_updated" {
			t.Fatalf("expected semantic no-op patches to avoid board_card_updated events, got %#v", timelinePayload.Events)
		}
	}
}

func TestBoardCardAddRequestKeyFallbackOnlyReplaysEquivalentState(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()

	primaryThreadID := createBoardThreadViaHTTP(t, h, "Board primary thread")
	memberThreadID := createBoardThreadViaHTTP(t, h, "Board member thread")
	memberDocumentID := createBoardDocumentViaHTTP(t, h, memberThreadID, "Member board doc")

	createBoardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{
		"actor_id":"actor-1",
		"board":{
			"title":"Ops Board",
			"primary_thread_id":"`+primaryThreadID+`"
		}
	}`, http.StatusCreated)
	defer createBoardResp.Body.Close()

	var createBoardPayload struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(createBoardResp.Body).Decode(&createBoardPayload); err != nil {
		t.Fatalf("decode create board response: %v", err)
	}
	boardID := asString(createBoardPayload.Board["id"])
	initialBoardUpdatedAt := asString(createBoardPayload.Board["updated_at"])

	firstAddResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards", `{
		"actor_id":"actor-1",
		"request_key":"retry-derived-id-add",
		"if_board_updated_at":"`+initialBoardUpdatedAt+`",
		"thread_id":"`+memberThreadID+`",
		"column_key":"ready",
		"pinned_document_id":"`+memberDocumentID+`"
	}`, http.StatusCreated)
	defer firstAddResp.Body.Close()

	retryFirstAddResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards", `{
		"actor_id":"actor-1",
		"request_key":"retry-derived-id-add",
		"if_board_updated_at":"`+initialBoardUpdatedAt+`",
		"thread_id":"`+memberThreadID+`",
		"column_key":"ready",
		"pinned_document_id":"`+memberDocumentID+`"
	}`, http.StatusCreated)
	defer retryFirstAddResp.Body.Close()

	var retryFirstAddPayload struct {
		Board map[string]any `json:"board"`
		Card  map[string]any `json:"card"`
	}
	if err := json.NewDecoder(retryFirstAddResp.Body).Decode(&retryFirstAddPayload); err != nil {
		t.Fatalf("decode retried first add response: %v", err)
	}
	if asString(retryFirstAddPayload.Card["parent_thread"]) != memberThreadID || asString(retryFirstAddPayload.Card["column_key"]) != "ready" || asString(retryFirstAddPayload.Card["pinned_document_id"]) != memberDocumentID {
		t.Fatalf("unexpected retried first add payload: %#v", retryFirstAddPayload)
	}

	replayableAddResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards", `{
		"actor_id":"actor-1",
		"request_key":"retry-equivalent-add",
		"thread_id":"`+memberThreadID+`",
		"column_key":"ready",
		"pinned_document_id":"`+memberDocumentID+`"
	}`, http.StatusCreated)
	defer replayableAddResp.Body.Close()
	var replayableAddPayload struct {
		Board map[string]any `json:"board"`
		Card  map[string]any `json:"card"`
	}
	if err := json.NewDecoder(replayableAddResp.Body).Decode(&replayableAddPayload); err != nil {
		t.Fatalf("decode replayable add response: %v", err)
	}
	if asString(replayableAddPayload.Card["parent_thread"]) != memberThreadID || asString(replayableAddPayload.Card["column_key"]) != "ready" || asString(replayableAddPayload.Card["pinned_document_id"]) != memberDocumentID {
		t.Fatalf("unexpected replayable add payload: %#v", replayableAddPayload)
	}

	conflictAddResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards", `{
		"actor_id":"actor-1",
		"request_key":"retry-mismatched-add",
		"thread_id":"`+memberThreadID+`",
		"column_key":"blocked"
	}`, http.StatusConflict)
	defer conflictAddResp.Body.Close()
	assertErrorCode(t, conflictAddResp, "conflict")

	staleEquivalentAddResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards", `{
		"actor_id":"actor-1",
		"request_key":"retry-stale-equivalent-add",
		"if_board_updated_at":"`+initialBoardUpdatedAt+`",
		"thread_id":"`+memberThreadID+`",
		"column_key":"ready",
		"pinned_document_id":"`+memberDocumentID+`"
	}`, http.StatusConflict)
	defer staleEquivalentAddResp.Body.Close()
	assertErrorCode(t, staleEquivalentAddResp, "conflict")
}

func TestBoardCardMoveRejectsInvalidPlacementAnchors(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	primaryThreadID := createBoardThreadViaHTTP(t, h, "Anchor primary thread")
	threadA := createBoardThreadViaHTTP(t, h, "Anchor card thread A")
	threadB := createBoardThreadViaHTTP(t, h, "Anchor card thread B")
	primaryDocumentID := createBoardDocumentViaHTTP(t, h, primaryThreadID, "Anchor primary doc")

	createBoardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{
		"actor_id":"actor-1",
		"board":{
			"title":"Anchor validation board",
			"primary_thread_id":"`+primaryThreadID+`",
			"primary_document_id":"`+primaryDocumentID+`"
		}
	}`, http.StatusCreated)
	defer createBoardResp.Body.Close()
	var createBoardPayload struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(createBoardResp.Body).Decode(&createBoardPayload); err != nil {
		t.Fatalf("decode create board response: %v", err)
	}
	boardID := asString(createBoardPayload.Board["id"])
	boardUpdatedAt := asString(createBoardPayload.Board["updated_at"])

	addAResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards", `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+boardUpdatedAt+`",
		"thread_id":"`+threadA+`",
		"column_key":"backlog"
	}`, http.StatusCreated)
	defer addAResp.Body.Close()
	var addAPayload struct {
		Board map[string]any `json:"board"`
		Card  map[string]any `json:"card"`
	}
	if err := json.NewDecoder(addAResp.Body).Decode(&addAPayload); err != nil {
		t.Fatalf("decode add card A response: %v", err)
	}
	cardAID := asString(addAPayload.Card["id"])
	afterAddA := asString(addAPayload.Board["updated_at"])

	addBResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards", `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+afterAddA+`",
		"thread_id":"`+threadB+`",
		"column_key":"backlog"
	}`, http.StatusCreated)
	defer addBResp.Body.Close()
	var addBPayload struct {
		Board map[string]any `json:"board"`
		Card  map[string]any `json:"card"`
	}
	if err := json.NewDecoder(addBResp.Body).Decode(&addBPayload); err != nil {
		t.Fatalf("decode add card B response: %v", err)
	}
	cardBID := asString(addBPayload.Card["id"])
	afterAddB := asString(addBPayload.Board["updated_at"])

	moveBase := h.baseURL + "/boards/" + boardID + "/cards/" + threadA + "/move"
	assertMoveInvalidRequest := func(t *testing.T, bodyJSON, wantMessage string) {
		t.Helper()
		resp := postJSONExpectStatus(t, moveBase, bodyJSON, http.StatusBadRequest)
		defer resp.Body.Close()
		var payload struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("decode error payload: %v", err)
		}
		if payload.Error.Code != "invalid_request" {
			t.Fatalf("unexpected error code: got %q want invalid_request", payload.Error.Code)
		}
		if payload.Error.Message != wantMessage {
			t.Fatalf("unexpected error message: got %q want %q", payload.Error.Message, wantMessage)
		}
	}

	assertMoveInvalidRequest(t, `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+afterAddB+`",
		"column_key":"ready",
		"before_card_id":"`+cardBID+`",
		"after_thread_id":"`+threadB+`"
	}`, "card-id and thread-id placement anchors cannot be combined")

	assertMoveInvalidRequest(t, `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+afterAddB+`",
		"column_key":"ready",
		"before_thread_id":"`+threadA+`",
		"after_card_id":"`+cardBID+`"
	}`, "card-id and thread-id placement anchors cannot be combined")

	assertMoveInvalidRequest(t, `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+afterAddB+`",
		"column_key":"ready",
		"before_card_id":"`+cardBID+`",
		"after_card_id":"`+cardAID+`"
	}`, "before and after anchors are mutually exclusive")
}

func TestCardMoveResolutionTransitionsAndEvents(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	primaryThreadID := createBoardThreadViaHTTP(t, h, "Move primary thread")
	memberThreadID := createBoardThreadViaHTTP(t, h, "Move member thread")

	createBoardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{
		"actor_id":"actor-1",
		"board":{
			"title":"Move resolution board",
			"primary_thread_id":"`+primaryThreadID+`"
		}
	}`, http.StatusCreated)
	defer createBoardResp.Body.Close()
	var createBoardPayload struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(createBoardResp.Body).Decode(&createBoardPayload); err != nil {
		t.Fatalf("decode create board response: %v", err)
	}
	boardID := asString(createBoardPayload.Board["id"])
	boardUpdatedAt := asString(createBoardPayload.Board["updated_at"])

	addResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards", `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+boardUpdatedAt+`",
		"thread_id":"`+memberThreadID+`",
		"column_key":"review"
	}`, http.StatusCreated)
	defer addResp.Body.Close()
	var addPayload struct {
		Board map[string]any `json:"board"`
		Card  map[string]any `json:"card"`
	}
	if err := json.NewDecoder(addResp.Body).Decode(&addPayload); err != nil {
		t.Fatalf("decode add card response: %v", err)
	}
	cardID := asString(addPayload.Card["id"])
	cardThreadID := asString(addPayload.Card["thread_id"])
	moveBase := h.baseURL + "/cards/" + cardID + "/move"

	doneMoveResp := postJSONExpectStatus(t, moveBase, `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+asString(addPayload.Board["updated_at"])+`",
		"column_key":"done",
		"resolution":"done",
		"resolution_refs":["event:card-completion-1"]
	}`, http.StatusOK)
	defer doneMoveResp.Body.Close()
	var doneMovePayload struct {
		Board map[string]any `json:"board"`
		Card  map[string]any `json:"card"`
	}
	if err := json.NewDecoder(doneMoveResp.Body).Decode(&doneMovePayload); err != nil {
		t.Fatalf("decode done move response: %v", err)
	}
	if asString(doneMovePayload.Card["column_key"]) != "done" {
		t.Fatalf("expected card to move into done, got %#v", doneMovePayload.Card["column_key"])
	}
	if asString(doneMovePayload.Card["resolution"]) != "done" {
		t.Fatalf("expected card resolution done, got %#v", doneMovePayload.Card["resolution"])
	}
	if !containsAny(doneMovePayload.Card["resolution_refs"].([]any), "event:card-completion-1") {
		t.Fatalf("expected terminal evidence ref on moved card, got %#v", doneMovePayload.Card["resolution_refs"])
	}

	staleMoveResp := postJSONExpectStatus(t, moveBase, `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+boardUpdatedAt+`",
		"column_key":"review"
	}`, http.StatusConflict)
	defer staleMoveResp.Body.Close()
	assertErrorCode(t, staleMoveResp, "conflict")

	cancelThreadID := createBoardThreadViaHTTP(t, h, "Cancel member thread")
	cancelAddResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+boardID+"/cards", `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+asString(doneMovePayload.Board["updated_at"])+`",
		"thread_id":"`+cancelThreadID+`",
		"column_key":"ready"
	}`, http.StatusCreated)
	defer cancelAddResp.Body.Close()
	var cancelAddPayload struct {
		Board map[string]any `json:"board"`
		Card  map[string]any `json:"card"`
	}
	if err := json.NewDecoder(cancelAddResp.Body).Decode(&cancelAddPayload); err != nil {
		t.Fatalf("decode cancel add response: %v", err)
	}
	cancelCardID := asString(cancelAddPayload.Card["id"])
	cancelMoveResp := postJSONExpectStatus(t, h.baseURL+"/cards/"+cancelCardID+"/move", `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+asString(cancelAddPayload.Board["updated_at"])+`",
		"column_key":"done",
		"resolution":"canceled",
		"resolution_refs":["event:card-canceled-1"]
	}`, http.StatusOK)
	defer cancelMoveResp.Body.Close()
	var cancelMovePayload struct {
		Board map[string]any `json:"board"`
		Card  map[string]any `json:"card"`
	}
	if err := json.NewDecoder(cancelMoveResp.Body).Decode(&cancelMovePayload); err != nil {
		t.Fatalf("decode canceled move response: %v", err)
	}
	if asString(cancelMovePayload.Card["resolution"]) != "canceled" {
		t.Fatalf("expected canceled resolution, got %#v", cancelMovePayload.Card["resolution"])
	}

	boardTimelineResp, err := http.Get(h.baseURL + "/threads/" + boardID + "/timeline")
	if err != nil {
		t.Fatalf("GET board timeline: %v", err)
	}
	defer boardTimelineResp.Body.Close()
	if boardTimelineResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected board timeline status: got %d", boardTimelineResp.StatusCode)
	}
	var boardTimelinePayload struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(boardTimelineResp.Body).Decode(&boardTimelinePayload); err != nil {
		t.Fatalf("decode board timeline response: %v", err)
	}
	var boardMovedEvent map[string]any
	for _, event := range boardTimelinePayload.Events {
		if asString(event["type"]) == "board_card_moved" {
			boardMovedEvent = event
			break
		}
	}
	if boardMovedEvent == nil {
		t.Fatalf("expected board_card_moved event in board timeline, got %#v", boardTimelinePayload.Events)
	}
	if !containsAny(boardMovedEvent["refs"].([]any), "board:"+boardID) || !containsAny(boardMovedEvent["refs"].([]any), "card:"+cardID) {
		t.Fatalf("expected board and card refs on board_card_moved, got %#v", boardMovedEvent["refs"])
	}

	cardTimelineResp, err := http.Get(h.baseURL + "/threads/" + cardThreadID + "/timeline")
	if err != nil {
		t.Fatalf("GET card timeline: %v", err)
	}
	defer cardTimelineResp.Body.Close()
	if cardTimelineResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected card timeline status: got %d", cardTimelineResp.StatusCode)
	}
	var cardTimelinePayload struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(cardTimelineResp.Body).Decode(&cardTimelinePayload); err != nil {
		t.Fatalf("decode card timeline response: %v", err)
	}
	var cardUpdatedEvent map[string]any
	for _, event := range cardTimelinePayload.Events {
		if asString(event["type"]) == "card_updated" {
			cardUpdatedEvent = event
			break
		}
	}
	if cardUpdatedEvent == nil {
		t.Fatalf("expected card_updated event in card timeline, got %#v", cardTimelinePayload.Events)
	}
	if !containsAny(cardUpdatedEvent["refs"].([]any), "board:"+boardID) || !containsAny(cardUpdatedEvent["refs"].([]any), "card:"+cardID) {
		t.Fatalf("expected board and card refs on card_updated, got %#v", cardUpdatedEvent["refs"])
	}
}

func createBoardThreadViaHTTP(t *testing.T, h primitivesTestHarness, title string) string {
	t.Helper()

	resp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"`+title+`",
			"type":"initiative",
			"status":"active",
			"priority":"p1",
			"tags":["boards"],
			"cadence":"daily",
			"next_check_in_at":"2099-03-05T00:00:00Z",
			"current_summary":"summary",
			"next_actions":["review"],
			"key_artifacts":[],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer resp.Body.Close()

	var payload struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode create thread response: %v", err)
	}
	threadID := asString(payload.Thread["id"])
	if threadID == "" {
		t.Fatalf("expected thread id for %q", title)
	}
	return threadID
}

func createBoardDocumentViaHTTP(t *testing.T, h primitivesTestHarness, threadID, title string) string {
	t.Helper()

	resp := postJSONExpectStatus(t, h.baseURL+"/docs", `{
		"actor_id":"actor-1",
		"document":{"thread_id":"`+threadID+`","title":"`+title+`","status":"active","labels":["boards"]},
		"refs":["thread:`+threadID+`"],
		"content":"# `+title+`",
		"content_type":"text"
	}`, http.StatusCreated)
	defer resp.Body.Close()

	var payload struct {
		Document map[string]any `json:"document"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode create document response: %v", err)
	}
	documentID := asString(payload.Document["id"])
	if documentID == "" {
		t.Fatalf("expected document id for %q", title)
	}
	return documentID
}

func createBoardCommitmentViaHTTP(t *testing.T, h primitivesTestHarness, threadID, title string) string {
	t.Helper()

	resp := postJSONExpectStatus(t, h.baseURL+"/commitments", `{
		"actor_id":"actor-1",
		"commitment":{
			"thread_id":"`+threadID+`",
			"title":"`+title+`",
			"owner":"actor-1",
			"due_at":"2099-03-08T00:00:00Z",
			"status":"open",
			"definition_of_done":["done"],
			"links":[],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer resp.Body.Close()

	var payload struct {
		Commitment map[string]any `json:"commitment"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode create commitment response: %v", err)
	}
	commitmentID := asString(payload.Commitment["id"])
	if commitmentID == "" {
		t.Fatalf("expected commitment id for %q", title)
	}
	return commitmentID
}

func intFromAny(raw any) int {
	switch typed := raw.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return 0
	}
}

func containsAllStrings(values []string, expected []string) bool {
	set := map[string]struct{}{}
	for _, value := range values {
		set[value] = struct{}{}
	}
	for _, value := range expected {
		if _, ok := set[value]; !ok {
			return false
		}
	}
	return true
}
