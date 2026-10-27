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

// Exercise the unchanged storage-ID bodies on every create entry point, including
// replay after parsing. Storage IDs must never acquire handle slug constraints.
func TestCardCreateStorageIDAndHandleReplay(t *testing.T) {
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Writer","created_at":"2026-03-04T10:00:00Z"}}`, 201)
	initial := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"actor-1","title":"Storage ID fixtures"}`, 201)
	board := asString(initial["work"].(map[string]any)["board_ref"])
	sequence := 0
	for _, route := range []string{"global", "board", "batch"} {
		for _, nested := range []bool{false, true} {
			for _, mode := range []string{"uuid-card-id", "id-and-handle", "handle-only", "legacy-storage-id"} {
				t.Run(fmt.Sprintf("%s/nested-%t/%s", route, nested, mode), func(t *testing.T) {
					sequence++
					slug := fmt.Sprintf("storage-case-%d", sequence)
					card := map[string]any{"title": slug}
					storageID := ""
					switch mode {
					case "uuid-card-id":
						storageID = fmt.Sprintf("550e8400-e29b-41d4-a716-%012d", sequence)
						card["card_id"] = storageID
					case "id-and-handle":
						storageID = fmt.Sprintf("stable-row-%d", sequence)
						card["id"] = storageID
						card["handle"] = slug
					case "handle-only":
						card["handle"] = slug
					case "legacy-storage-id":
						storageID = fmt.Sprintf("UPPER_row.%d", sequence)
						card["card_id"] = storageID
					}
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
						item := card
						if nested {
							item = map[string]any{"card": card}
						}
						body = map[string]any{"actor_id": "actor-1", "items": []any{item}}
					}
					body["request_key"] = slug + "-request"
					raw, err := json.Marshal(body)
					if err != nil {
						t.Fatal(err)
					}
					cardFrom := func(response map[string]any) map[string]any {
						if route == "batch" {
							return response["cards"].([]any)[0].(map[string]any)
						}
						return response["card"].(map[string]any)
					}
					row := cardFrom(workPostJSON(t, endpoint, string(raw), 201))
					replay := cardFrom(workPostJSON(t, endpoint, string(raw), 201))
					for _, key := range []string{"id", "ref", "handle"} {
						if replay[key] != row[key] {
							t.Fatalf("same-body replay changed %s: %#v vs %#v", key, row, replay)
						}
					}
					if route == "batch" {
						// Batch receipts are scoped references; verify full handle/storage
						// ergonomics through the GET representation the reference resolves to.
						if len(row) != 3 || row["version"] == nil {
							t.Fatalf("unexpected reference receipt: %#v", row)
						}
						row = workGetJSON(t, h.baseURL+"/cards/"+asString(row["ref"]), 200)["card"].(map[string]any)
					}
					if storageID != "" && row["id"] != storageID {
						t.Fatalf("storage ID changed: want %q, got %#v", storageID, row)
					}
					if asString(row["id"]) == "" || row["id"] == slug || row["handle"] != slug || row["ref"] != "card:"+slug {
						t.Fatalf("storage ID and public handle were conflated: %#v", row)
					}
					fetched, err := http.Get(h.baseURL + "/cards/card:" + slug)
					if err != nil {
						t.Fatal(err)
					}
					defer fetched.Body.Close()
					var loaded map[string]any
					if err := json.NewDecoder(fetched.Body).Decode(&loaded); err != nil {
						t.Fatal(err)
					}
					if fetched.StatusCode != 200 || loaded["card"].(map[string]any)["id"] != row["id"] {
						t.Fatalf("ref does not resolve to storage ID: %#v", loaded)
					}
				})
			}
		}
	}
}

func TestCardCreateStorageIDParsingCompatibility(t *testing.T) {
	const uuid = "550e8400-e29b-41d4-a716-446655440000"
	for _, tc := range []struct {
		name   string
		raw    map[string]any
		id     string
		handle string
	}{
		{"uuid card_id", map[string]any{"card_id": uuid}, uuid, ""},
		{"uuid id", map[string]any{"id": uuid}, uuid, ""},
		{"independent handle", map[string]any{"id": "stable-row", "handle": "ship-fix"}, "stable-row", "ship-fix"},
		{"handle only", map[string]any{"handle": "ship-fix"}, "", "ship-fix"},
		{"non slug", map[string]any{"card_id": "UPPER_row.123"}, "UPPER_row.123", ""},
		{"card_id precedence", map[string]any{"card_id": "primary", "id": "fallback", "handle": "chosen"}, "primary", "chosen"},
		{"empty card_id fallback", map[string]any{"card_id": " ", "id": "fallback"}, "fallback", ""},
		{"nested id precedence", map[string]any{"id": "flat", "card": map[string]any{"id": uuid, "handle": "chosen"}}, uuid, "chosen"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.raw["title"] = "Ship"
			got, err := mergeAddBoardCardRaw(tc.raw)
			if err != nil || got.CardID != tc.id || got.Handle != tc.handle || addBoardCardStoreInput(got).CardID != tc.id {
				t.Fatalf("want id=%q handle=%q, got %#v: %v", tc.id, tc.handle, got, err)
			}
		})
	}
}
