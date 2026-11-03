package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
)

func getCardJSON(t *testing.T, url string, wantStatus int) map[string]any {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("GET %s: status %d, want %d: %v", url, resp.StatusCode, wantStatus, payload)
	}
	return payload
}

// TestCardRevisionHistoryRoutes covers the history contract from issue #510
// over HTTP: cards get carries no history, history pages are metadata only,
// content comes from one revision or a diff, and the summary limit is a 400
// that names the limit.
func TestCardRevisionHistoryRoutes(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	boardResp := postJSONExpectStatus(t, h.baseURL+"/boards", `{"actor_id":"actor-1","board":{"title":"History Board","owners":["actor-1"]}}`, http.StatusCreated)
	var board struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(boardResp.Body).Decode(&board); err != nil {
		t.Fatal(err)
	}
	boardResp.Body.Close()
	cardResp := postJSONExpectStatus(t, h.baseURL+"/boards/"+asString(board.Board["id"])+"/cards", `{
		"actor_id":"actor-1",
		"if_board_updated_at":"`+asString(board.Board["updated_at"])+`",
		"title":"Plan",
		"summary":"one\ntwo\nthree",
		"column_key":"ready"
	}`, http.StatusCreated)
	var created struct {
		Card map[string]any `json:"card"`
	}
	if err := json.NewDecoder(cardResp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	cardResp.Body.Close()
	cardID := asString(created.Card["id"])
	revise := func(summary string, wantStatus int) *http.Response {
		body, _ := json.Marshal(map[string]any{"actor_id": "actor-1", "revision": map[string]any{"summary": summary}})
		return postJSONExpectStatus(t, h.baseURL+"/cards/"+cardID+"/revisions", string(body), wantStatus)
	}
	for i := 2; i <= 5; i++ {
		revise(fmt.Sprintf("one\ntwo %d\nthree", i), http.StatusCreated).Body.Close()
	}

	card := getCardJSON(t, h.baseURL+"/cards/"+cardID, http.StatusOK)
	if c, _ := card["card"].(map[string]any); c == nil || c["history"] != nil || card["history"] != nil {
		t.Fatalf("cards get must return the current card without history: %v", card)
	}

	page := getCardJSON(t, h.baseURL+"/cards/"+cardID+"/revisions?limit=2", http.StatusOK)
	revisions, _ := page["revisions"].([]any)
	cursor := asString(page["next_cursor"])
	if len(revisions) != 2 || cursor == "" {
		t.Fatalf("first page = %v", page)
	}
	newest, _ := revisions[0].(map[string]any)
	if newest["revision_number"] != float64(5) || newest["summary_bytes"] == nil || newest["content_bytes"] == nil {
		t.Fatalf("history entries are newest first with sizes: %v", newest)
	}
	for _, key := range []string{"summary", "title", "definition_of_done"} {
		if _, ok := newest[key]; ok {
			t.Fatalf("history entries must be metadata only, found %q: %v", key, newest)
		}
	}
	seen := 2
	for cursor != "" {
		next := getCardJSON(t, h.baseURL+"/cards/"+cardID+"/revisions?limit=2&cursor="+cursor, http.StatusOK)
		more, _ := next["revisions"].([]any)
		seen += len(more)
		cursor = asString(next["next_cursor"])
	}
	if seen != 5 {
		t.Fatalf("paging visited %d revisions, want 5", seen)
	}
	for _, bad := range []string{"limit=0", "limit=201", "limit=x", "cursor=bogus"} {
		resp, err := http.Get(h.baseURL + "/cards/" + cardID + "/revisions?" + bad)
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: status %d, want 400", bad, resp.StatusCode)
		}
		assertErrorCode(t, resp, "invalid_request")
		resp.Body.Close()
	}

	revisionID := asString(newest["revision_id"])
	full := getCardJSON(t, h.baseURL+"/cards/"+cardID+"/revisions/"+revisionID, http.StatusOK)
	if rev, _ := full["revision"].(map[string]any); asString(rev["summary"]) != "one\ntwo 5\nthree" || asString(rev["title"]) != "Plan" {
		t.Fatalf("revision get must return that revision's content: %v", full)
	}
	diff := getCardJSON(t, h.baseURL+"/cards/"+cardID+"/revisions/"+revisionID+"/diff", http.StatusOK)
	summary, _ := diff["summary"].(map[string]any)
	if !strings.Contains(asString(summary["unified"]), "-two 4\n+two 5\n") || diff["identical"] != false {
		t.Fatalf("diff against the previous revision = %v", diff)
	}
	if base, _ := diff["base_revision"].(map[string]any); base["revision_number"] != float64(4) {
		t.Fatalf("default diff base = %v", diff["base_revision"])
	}
	first := asString(created.Card["head_revision_ref"])
	explicit := getCardJSON(t, h.baseURL+"/cards/"+cardID+"/revisions/"+revisionID+"/diff?base="+strings.TrimPrefix(first, "card_revision:"), http.StatusOK)
	if base, _ := explicit["base_revision"].(map[string]any); base["revision_number"] != float64(1) {
		t.Fatalf("explicit diff base = %v", explicit["base_revision"])
	}
	getCardJSON(t, h.baseURL+"/cards/"+cardID+"/revisions/missing-revision/diff", http.StatusNotFound)

	// Short lines keep the markdown line-length rule out of the way.
	line := strings.Repeat("x", 99) + "\n"
	atLimit := strings.Repeat(line, primitives.MaxCardSummaryBytes/len(line)) + strings.Repeat("x", primitives.MaxCardSummaryBytes%len(line))
	tooLarge := revise(atLimit+"\nx", http.StatusBadRequest)
	defer tooLarge.Body.Close()
	assertErrorMessageContains(t, tooLarge, fmt.Sprintf("%d-byte (64 KiB) summary limit", primitives.MaxCardSummaryBytes))
	revise(atLimit, http.StatusCreated).Body.Close()
}
