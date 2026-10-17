package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestCardAndWorkCreateHonorHandles(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	work := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"actor-1","title":"Ship","handle":"chosen-work","request_key":"handle-replay"}`, 201)
	if asString(work["work"].(map[string]any)["ref"]) != "card:chosen-work" {
		t.Fatal(work)
	}
	replay := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"actor-1","title":"Ship","handle":"chosen-work","request_key":"handle-replay"}`, 201)
	if asString(replay["work"].(map[string]any)["ref"]) != "card:chosen-work" {
		t.Fatal(replay)
	}
	conflict := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"actor-1","title":"Other","handle":"chosen-work"}`, 409)
	if !strings.Contains(asString(conflict["error"].(map[string]any)["message"]), "handle") {
		t.Fatal(conflict)
	}
	board := work["work"].(map[string]any)["board_ref"]
	created := workPostJSON(t, h.baseURL+"/cards", `{"actor_id":"actor-1","board_ref":"`+asString(board)+`","card":{"handle":"chosen-card","title":"Card","summary":"Details","column_key":"ready"}}`, 201)
	if asString(created["card"].(map[string]any)["ref"]) != "card:chosen-card" {
		t.Fatal(created)
	}
	workPostJSON(t, h.baseURL+"/cards", `{"actor_id":"actor-1","board_ref":"`+asString(board)+`","card":{"handle":"chosen-card","title":"Duplicate"}}`, 409)
	for _, handle := range []string{"UPPER", "two--dashes", "cards", ""} {
		workPostJSON(t, h.baseURL+"/cards", `{"actor_id":"actor-1","board_ref":"`+asString(board)+`","card":{"handle":"`+handle+`","title":"Invalid"}}`, 400)
	}
	workPostJSON(t, h.baseURL+"/work", `{"actor_id":"actor-1","title":"Invalid","handle":42}`, 400)
	batchURL := h.baseURL + "/boards/" + asString(board) + "/cards/batch"
	workPostJSON(t, batchURL, `{"actor_id":"actor-1","items":[{"card":{"handle":"batch-chosen","title":"Batch"}}]}`, 201)
	batchConflict := workPostJSON(t, batchURL, `{"actor_id":"actor-1","items":[{"card":{"handle":"batch-chosen","title":"Duplicate"}}]}`, 409)
	if !strings.Contains(asString(batchConflict["error"].(map[string]any)["message"]), "handle") {
		t.Fatal(batchConflict)
	}

}

func TestCardCreateRequestParsesHandle(t *testing.T) {
	for _, raw := range []map[string]any{{"card": map[string]any{"handle": "chosen", "title": "Ship"}}, {"handle": "chosen", "title": "Ship"}} {
		got, err := mergeAddBoardCardRaw(raw)
		if err != nil || got.Handle != "chosen" || addBoardCardStoreInput(got).Handle != "chosen" {
			t.Fatalf("%#v %v", got, err)
		}
	}
	for _, bad := range []any{42, "", "UPPER", "two--dashes", "cards"} {
		if _, err := mergeAddBoardCardRaw(map[string]any{"card": map[string]any{"handle": bad, "title": "Ship"}}); err == nil {
			t.Fatalf("accepted %#v", bad)
		}
	}
}
