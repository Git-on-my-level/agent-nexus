package server

import (
	"encoding/json"
	"fmt"
	"testing"
)

// Mutations that echo the mutated card must report the same grouping fields a
// fresh GET returns. Automations read the mutation response instead of
// re-reading, so a dropped parent_ref reads as a lost grouping parent.

func seedGroupedCardForMutation(t *testing.T, h projectionMaintenanceTestHarness, label string) (parent, child map[string]any) {
	t.Helper()
	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"grouper","display_name":"Grouper","created_at":"2026-10-01T00:00:00Z"}}`, 201)
	parent = workPostJSON(t, h.baseURL+"/work", fmt.Sprintf(`{"actor_id":"grouper","title":%q}`, label+" parent"), 201)["work"].(map[string]any)
	child = workPostJSON(t, h.baseURL+"/work", fmt.Sprintf(`{"actor_id":"grouper","title":%q,"under":%q}`, label+" child", parent["ref"]), 201)["work"].(map[string]any)
	return parent, child
}

func freshCardParentRef(t *testing.T, h projectionMaintenanceTestHarness, ref string) any {
	t.Helper()
	card := workGetJSON(t, h.baseURL+"/cards/"+ref, 200)["card"].(map[string]any)
	return card["parent_ref"]
}

func assertMutationParentRefMatchesFreshGet(t *testing.T, h projectionMaintenanceTestHarness, mutated map[string]any, cardRef string, wantParent any, mutation string) {
	t.Helper()
	if got := mutated["card"].(map[string]any)["parent_ref"]; got != wantParent {
		t.Fatalf("%s response parent_ref %#v does not match fresh get %#v", mutation, got, wantParent)
	}
}

func TestCardsMoveAcrossBoardsResponseParentRefMatchesFreshGet(t *testing.T) {
	h := newProjectionMaintenanceTestServer(t)
	parent, child := seedGroupedCardForMutation(t, h, "Cross-board")
	source := workGetJSON(t, h.baseURL+"/boards/"+asString(child["board_ref"]), 200)["board"].(map[string]any)
	dest := workPostJSON(t, h.baseURL+"/boards", `{"actor_id":"grouper","board":{"title":"Destination"}}`, 201)["board"].(map[string]any)
	url := h.baseURL + "/cards/" + asString(child["ref"]) + "/move"
	body := map[string]any{"actor_id": "grouper", "board_ref": dest["ref"], "column_key": "ready", "if_board_updated_at": source["updated_at"], "if_version": child["version"], "request_key": "cross-board-parent"}
	raw, _ := json.Marshal(body)
	moved := workPostJSON(t, url, string(raw), 200)
	if card := moved["card"].(map[string]any); card["board_ref"] != dest["ref"] {
		t.Fatalf("card did not move to destination board: %#v", card)
	}
	fresh := freshCardParentRef(t, h, asString(child["ref"]))
	if fresh != parent["ref"] {
		t.Fatalf("grouping parent lost across boards: %#v", fresh)
	}
	assertMutationParentRefMatchesFreshGet(t, h, moved, asString(child["ref"]), fresh, "cross-board move")
}

func TestCardsColumnMoveResponseParentRefMatchesFreshGet(t *testing.T) {
	h := newProjectionMaintenanceTestServer(t)
	parent, child := seedGroupedCardForMutation(t, h, "Column")
	board := workGetJSON(t, h.baseURL+"/boards/"+asString(child["board_ref"]), 200)["board"].(map[string]any)
	url := h.baseURL + "/cards/" + asString(child["ref"]) + "/move"
	body := map[string]any{"actor_id": "grouper", "column_key": "review", "if_board_updated_at": board["updated_at"], "request_key": "column-move-parent"}
	raw, _ := json.Marshal(body)
	moved := workPostJSON(t, url, string(raw), 200)
	if card := moved["card"].(map[string]any); card["column_key"] != "review" {
		t.Fatalf("card did not change column: %#v", card)
	}
	fresh := freshCardParentRef(t, h, asString(child["ref"]))
	if fresh != parent["ref"] {
		t.Fatalf("column move lost grouping parent: %#v", fresh)
	}
	assertMutationParentRefMatchesFreshGet(t, h, moved, asString(child["ref"]), fresh, "column move")
}

func TestCardPatchArchiveRestoreResponseParentRefMatchesFreshGet(t *testing.T) {
	h := newProjectionMaintenanceTestServer(t)
	parent, child := seedGroupedCardForMutation(t, h, "Lifecycle")
	cardURL := h.baseURL + "/cards/" + asString(child["ref"])

	patchBody, _ := json.Marshal(map[string]any{"actor_id": "grouper", "if_updated_at": child["updated_at"], "patch": map[string]any{"due_at": "2026-11-15T00:00:00Z"}, "request_key": "patch-parent"})
	patchResp := patchJSONExpectStatus(t, cardURL, string(patchBody), 200)
	var patched map[string]any
	if err := json.NewDecoder(patchResp.Body).Decode(&patched); err != nil {
		t.Fatal(err)
	}
	patchResp.Body.Close()
	assertMutationParentRefMatchesFreshGet(t, h, patched, asString(child["ref"]), parent["ref"], "patch")

	archived := workPostJSON(t, cardURL+"/archive", `{"actor_id":"grouper","reason":"curation"}`, 200)
	assertMutationParentRefMatchesFreshGet(t, h, archived, asString(child["ref"]), parent["ref"], "archive")

	restored := workPostJSON(t, cardURL+"/restore", `{"actor_id":"grouper"}`, 200)
	assertMutationParentRefMatchesFreshGet(t, h, restored, asString(child["ref"]), parent["ref"], "restore")
}
