package server

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

func TestGroupingCreateAcrossBothSurfaces(t *testing.T) {
	for _, surface := range []string{"work", "cards"} {
		t.Run(surface, func(t *testing.T) {
			h := newProjectionMaintenanceTestServer(t)
			workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"grouper","display_name":"Grouper","created_at":"2026-10-01T00:00:00Z"}}`, 201)
			parent := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"grouper","title":"P76 Reliability"}`, 201)["work"].(map[string]any)
			body := map[string]any{"actor_id": "grouper", "title": "P76 Child", "under": parent["ref"], "board_ref": parent["board_ref"], "request_key": "create-child"}
			raw, _ := json.Marshal(body)
			created := workPostJSON(t, h.baseURL+"/"+surface, string(raw), 201)
			key := "work"
			if surface == "cards" {
				key = "card"
			}
			child := created[key].(map[string]any)
			if child["parent_ref"] != parent["ref"] {
				t.Fatalf("%s create ignored under: %#v", surface, child)
			}
			replayed := workPostJSON(t, h.baseURL+"/"+surface, string(raw), 201)
			if replayed[key].(map[string]any)["ref"] != child["ref"] {
				t.Fatal("replay duplicated child")
			}
			plan := workGetJSON(t, h.baseURL+"/cards/"+asString(parent["ref"])+"/plan", 200)
			steps := plan["plan"].(map[string]any)["steps"].([]any)
			if len(steps) != 1 || steps[0].(map[string]any)["ref"] != child["ref"] {
				t.Fatalf("not one plan step: %#v", plan)
			}
		})
	}
}

func TestGroupingLikelyHomeAndUngrouped(t *testing.T) {
	for _, surface := range []string{"work", "cards"} {
		t.Run(surface, func(t *testing.T) { testGroupingLikelyHomeAndUngrouped(t, surface) })
	}
}

func testGroupingLikelyHomeAndUngrouped(t *testing.T, surface string) {
	t.Helper()
	h := newProjectionMaintenanceTestServer(t)
	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"grouper","display_name":"Grouper","created_at":"2026-10-01T00:00:00Z"}}`, 201)
	parent := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"grouper","title":"Cost 1/5: budget"}`, 201)["work"].(map[string]any)
	created := workPostJSON(t, h.baseURL+"/"+surface, fmt.Sprintf(`{"actor_id":"grouper","title":"Cost 2/5: measure","board_ref":%q,"related_refs":[%q]}`, parent["board_ref"], parent["ref"]), 201)
	homes, _ := created["likely_home"].([]any)
	if len(homes) == 0 || homes[0].(map[string]any)["ref"] != parent["ref"] {
		t.Fatalf("no likely home: %#v", created)
	}
	feed := workGetJSON(t, h.baseURL+"/work/ungrouped?limit=10", 200)
	if rows, _ := feed["work"].([]any); len(rows) != 2 {
		t.Fatalf("ungrouped feed: %#v", feed)
	}
}

func TestGroupingGroupFoldUnfold(t *testing.T) {
	h := newProjectionMaintenanceTestServer(t)
	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"grouper","display_name":"Grouper","created_at":"2026-10-01T00:00:00Z"}}`, 201)
	p := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"grouper","title":"Reliability"}`, 201)["work"].(map[string]any)
	c := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"grouper","title":"Cleanup"}`, 201)["work"].(map[string]any)
	url := h.baseURL + "/work/" + asString(c["ref"])
	group := fmt.Sprintf(`{"actor_id":"grouper","under":%q,"request_key":"group"}`, p["ref"])
	workPostJSON(t, url+"/group", group, 200)
	workPostJSON(t, url+"/group", group, 200)
	fold := fmt.Sprintf(`{"actor_id":"grouper","into":%q,"request_key":"fold"}`, p["ref"])
	workPostJSON(t, url+"/fold", fold, 200)
	workPostJSON(t, url+"/fold", fold, 200)
	workPostJSON(t, url+"/unfold", `{"actor_id":"grouper","request_key":"unfold"}`, 200)
	workPostJSON(t, url+"/unfold", `{"actor_id":"grouper","request_key":"unfold"}`, 200)
	got := workGetJSON(t, url, 200)["work"].(map[string]any)
	if got["archived_at"] != nil && got["archived_at"] != "" {
		t.Fatalf("still archived: %#v", got)
	}
}

func TestGroupingArchiveReasonIsDurable(t *testing.T) {
	h := newProjectionMaintenanceTestServer(t)
	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"grouper","display_name":"Grouper","created_at":"2026-10-01T00:00:00Z"}}`, 201)
	c := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"grouper","title":"Old card"}`, 201)["work"].(map[string]any)
	workPostJSON(t, h.baseURL+"/cards/"+asString(c["ref"])+"/archive", `{"actor_id":"grouper","reason":"superseded by initiative"}`, 200)
	var count int
	err := h.workspace.DB().QueryRow(`SELECT count(*) FROM events WHERE type='card_archived' AND json_extract(payload_json,'$.payload.reason')='superseded by initiative'`).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("archive reason not durable exactly once: count=%d err=%v", count, err)
	}
}

func TestGroupingCreateRejectsBadParentWithoutAnOrphan(t *testing.T) {
	for _, surface := range []string{"work", "cards"} {
		t.Run(surface, func(t *testing.T) {
			h := newProjectionMaintenanceTestServer(t)
			workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"grouper","display_name":"Grouper","created_at":"2026-10-01T00:00:00Z"}}`, 201)
			p := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"grouper","title":"Parent"}`, 201)["work"].(map[string]any)
			for i, tc := range []struct {
				under, after string
				status       int
			}{{"card:missing", "", 404}, {"topic:wrong-kind", "", 400}, {asString(p["ref"]), "missing-step", 400}} {
				body := map[string]any{"actor_id": "grouper", "board_ref": p["board_ref"], "title": "No orphan", "under": tc.under, "request_key": fmt.Sprintf("reject-%d", i)}
				if tc.after != "" {
					body["after"] = tc.after
				}
				raw, _ := json.Marshal(body)
				workPostJSON(t, h.baseURL+"/"+surface, string(raw), tc.status)
			}
			var cards int
			if err := h.workspace.DB().QueryRow(`SELECT count(*) FROM cards`).Scan(&cards); err != nil || cards != 1 {
				t.Fatalf("orphan card committed: %d err=%v", cards, err)
			}
		})
	}
}

// The board queues group/fold/unfold in a device outbox and retries an
// uncertain send with its original key. Concurrent exact retries must land
// once, and reusing a key for another body must be a definitive conflict,
// not a 500 the outbox would keep retrying.
func TestGroupingReplayIsExactAndChangedBodiesConflict(t *testing.T) {
	h := newProjectionMaintenanceTestServer(t)
	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"grouper","display_name":"Grouper","created_at":"2026-10-01T00:00:00Z"}}`, 201)
	p := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"grouper","title":"Reliability"}`, 201)["work"].(map[string]any)
	other := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"grouper","title":"Pricing"}`, 201)["work"].(map[string]any)
	c := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"grouper","title":"Cleanup"}`, 201)["work"].(map[string]any)
	url := h.baseURL + "/work/" + asString(c["ref"])
	group := fmt.Sprintf(`{"actor_id":"grouper","under":%q,"request_key":"tray-group"}`, p["ref"])
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := postJSONExpectStatus(t, url+"/group", group, 200)
			r.Body.Close()
		}()
	}
	wg.Wait()
	plan := workGetJSON(t, h.baseURL+"/cards/"+asString(p["ref"])+"/plan", 200)
	if steps := plan["plan"].(map[string]any)["steps"].([]any); len(steps) != 1 {
		t.Fatalf("concurrent replays added %d steps: %#v", len(steps), plan)
	}
	// Each verb's first send lands, then the same key with another body is
	// refused.
	workPostJSON(t, url+"/group", fmt.Sprintf(`{"actor_id":"grouper","under":%q,"request_key":"tray-group"}`, other["ref"]), 409)
	fold := fmt.Sprintf(`{"actor_id":"grouper","into":%q,"request_key":"tray-fold"}`, p["ref"])
	workPostJSON(t, url+"/fold", fold, 200)
	workPostJSON(t, url+"/fold", fmt.Sprintf(`{"actor_id":"grouper","into":%q,"request_key":"tray-fold"}`, other["ref"]), 409)
	workPostJSON(t, url+"/unfold", `{"actor_id":"grouper","request_key":"tray-unfold"}`, 200)
	workPostJSON(t, url+"/unfold", `{"actor_id":"grouper","request_key":"tray-unfold","after":"x"}`, 409)
}
