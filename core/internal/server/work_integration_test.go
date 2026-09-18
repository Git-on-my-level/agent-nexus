package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestWorkHTTPRegistrationObservationAndAuthority(t *testing.T) {
	h := newPrimitivesTestServer(t)
	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	b := workPostJSON(t, h.baseURL+"/boards", `{"actor_id":"actor-1","board":{"title":"Work portfolio"}}`, http.StatusCreated)
	board := b["board"].(map[string]any)
	boardRef := asString(board["ref"])
	body := fmt.Sprintf(`{"actor_id":"actor-1","board_ref":%q,"title":"External work","source":{"authority":"github","connection_id":"fixture","native_id":"org/repo/issues/7"}}`, boardRef)
	created := workPostJSON(t, h.baseURL+"/work", body, http.StatusCreated)
	work := created["work"].(map[string]any)
	ref := asString(work["ref"])
	if _, ok := work["id"]; ok {
		t.Fatal("public work leaks storage id")
	}
	if asString(work["rank"]) == "" {
		t.Fatal("public work omitted board rank")
	}
	again := workPostJSON(t, h.baseURL+"/work", body, http.StatusCreated)
	if again["work"].(map[string]any)["ref"] != ref {
		t.Fatal("source dedupe failed")
	}
	observation := `{"actor_id":"actor-1","observation":{"idempotency_key":"one","reader_id":"test","reader_revision":"v1","observed_at":"2026-01-01T00:00:00Z","status":"verified","facts":{"phase":"review","native_status":"unfamiliar"},"evidence":[{"url":"https://example.test/1"}]}}`
	result := workPostJSON(t, h.baseURL+"/work/"+ref+"/observations", observation, http.StatusOK)
	if result["observation"].(map[string]any)["verification"] != "reported" {
		t.Fatal("remote self verification")
	}
	replay := workPostJSON(t, h.baseURL+"/work/"+ref+"/observations", observation, http.StatusOK)
	if replay["duplicate"] != true {
		t.Fatal("no replay dedupe")
	}
	workPostJSON(t, h.baseURL+"/work/"+ref+"/refresh", `{"actor_id":"actor-1"}`, http.StatusAccepted)
	workPostJSON(t, h.baseURL+"/work/"+ref+"/observations", `{"actor_id":"actor-1","observation":{"idempotency_key":"missing-proof","reader_id":"test","reader_revision":"v1","observed_at":"2026-01-02T00:00:00Z","status":"reported","facts":{"phase":"done"}}}`, http.StatusBadRequest)
}

func TestWorkCreateOmittingBoardRefProvisionsDefaultBoard(t *testing.T) {
	h := newPrimitivesTestServer(t)
	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	listed := workGetJSON(t, h.baseURL+"/boards", http.StatusOK)
	if boards, _ := listed["boards"].([]any); len(boards) != 0 {
		t.Fatalf("expected no boards, got %#v", listed["boards"])
	}
	created := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"actor-1","title":"First task"}`, http.StatusCreated)
	work := created["work"].(map[string]any)
	if asString(work["board_ref"]) == "" {
		t.Fatal("created work omitted board_ref")
	}
	if asString(work["title"]) != "First task" {
		t.Fatalf("unexpected title %#v", work["title"])
	}
	after := workGetJSON(t, h.baseURL+"/boards", http.StatusOK)
	boards, _ := after["boards"].([]any)
	if len(boards) != 1 {
		t.Fatalf("expected one default board, got %#v", after["boards"])
	}
	explicit := workPostJSON(t, h.baseURL+"/boards", `{"actor_id":"actor-1","board":{"title":"Other"}}`, http.StatusCreated)
	otherRef := asString(explicit["board"].(map[string]any)["ref"])
	if otherRef == "" {
		t.Fatal("other board missing ref")
	}
	placed := workPostJSON(t, h.baseURL+"/work", fmt.Sprintf(`{"actor_id":"actor-1","board_ref":%q,"title":"On other board"}`, otherRef), http.StatusCreated)
	if asString(placed["work"].(map[string]any)["board_ref"]) != otherRef {
		t.Fatalf("explicit board_ref ignored: %#v", placed["work"])
	}
}

func workPostJSON(t *testing.T, url, body string, status int) map[string]any {
	t.Helper()
	r := postJSONExpectStatus(t, url, body, status)
	defer r.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(r.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func workGetJSON(t *testing.T, url string, status int) map[string]any {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != status {
		t.Fatalf("GET %s: status %d, want %d", url, resp.StatusCode, status)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}
