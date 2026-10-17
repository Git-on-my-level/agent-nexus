package server

import (
	"encoding/json"
	"fmt"
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

func TestCardCreateHandleAliasesNeverSelectStorageID(t *testing.T) {
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Writer","created_at":"2026-03-04T10:00:00Z"}}`, 201)
	initial := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"actor-1","title":"Alias fixtures"}`, 201)
	board := asString(initial["work"].(map[string]any)["board_ref"])
	for _, route := range []string{"global", "board", "batch"} {
		for _, nested := range []bool{false, true} {
			for _, field := range []string{"handle", "id", "card_id"} {
				slug := fmt.Sprintf("alias-%s-%t-%s", route, nested, strings.ReplaceAll(field, "_", "-"))
				card := map[string]any{field: slug, "title": "Different title from chosen slug"}
				body := map[string]any{"actor_id": "actor-1"}
				if nested {
					body["card"] = card
				} else {
					for k, v := range card {
						body[k] = v
					}
				}
				endpoint := h.baseURL + "/boards/" + board + "/cards"
				if route == "global" {
					endpoint = h.baseURL + "/cards"
					body["board_ref"] = board
				}
				if route == "batch" {
					endpoint += "/batch"
					item := map[string]any{}
					if nested {
						item["card"] = card
					} else {
						item = card
					}
					body = map[string]any{"actor_id": "actor-1", "items": []any{item}}
				}
				body["request_key"] = slug + "-request"
				raw, _ := json.Marshal(body)
				response := workPostJSON(t, endpoint, string(raw), 201)
				result := response["card"]
				if route == "batch" {
					result = response["cards"].([]any)[0]
				}
				replay := workPostJSON(t, endpoint, string(raw), 201)

				row := result.(map[string]any)
				if wrapped, ok := row["card"].(map[string]any); ok {
					row = wrapped
				}
				replayResult := replay["card"]
				if route == "batch" {
					replayResult = replay["cards"].([]any)[0]
				}
				replayRow := replayResult.(map[string]any)
				for _, key := range []string{"id", "ref", "handle"} {
					if replayRow[key] != row[key] {
						t.Fatalf("alias replay changed %s: %#v vs %#v", key, row, replayRow)
					}
				}
				if row["handle"] != slug || row["ref"] != "card:"+slug || row["id"] == slug {
					t.Fatalf("%s: %#v", slug, row)
				}
				fetched, err := http.Get(h.baseURL + "/cards/card:" + slug)
				if err != nil {
					t.Fatal(err)
				}
				var loaded map[string]any
				json.NewDecoder(fetched.Body).Decode(&loaded)
				fetched.Body.Close()
				if fetched.StatusCode != 200 || loaded["card"].(map[string]any)["id"] != row["id"] {
					t.Fatalf("ref doesn't resolve: %#v", loaded)
				}
				duplicate, _ := json.Marshal(map[string]any{"actor_id": "actor-1", "board_ref": board, "card": map[string]any{"handle": slug, "title": "Duplicate"}})
				workPostJSON(t, h.baseURL+"/cards", string(duplicate), 409)
			}
		}
	}
}

func TestCardHandleAliasesValidateAndAgree(t *testing.T) {
	for _, field := range []string{"handle", "id", "card_id"} {
		for _, bad := range []any{nil, 42, "", "UPPER", "cards", "two--dashes", "550e8400-e29b-41d4-a716-446655440000"} {
			if _, err := mergeAddBoardCardRaw(map[string]any{"title": "Ship", field: bad}); err == nil {
				t.Fatalf("accepted %s=%v", field, bad)
			}
		}
	}
	for _, raw := range []map[string]any{
		{"title": "Ship", "id": "same", "card_id": "same", "card": map[string]any{"handle": "same"}},
		{"title": "Ship", "handle": "same", "card": map[string]any{"handle": "same"}},
	} {
		got, err := mergeAddBoardCardRaw(raw)
		if err != nil || got.Handle != "same" || got.CardID != "" {
			t.Fatalf("%#v %v", got, err)
		}
	}
	for _, raw := range []map[string]any{
		{"title": "Ship", "id": "different", "handle": "chosen"},
		{"title": "Ship", "card_id": "different", "card": map[string]any{"handle": "chosen"}},
		{"title": "Ship", "handle": "different", "card": map[string]any{"handle": "chosen"}},
	} {
		if _, err := mergeAddBoardCardRaw(raw); err == nil {
			t.Fatalf("accepted conflicting aliases: %#v", raw)
		}
	}
}
