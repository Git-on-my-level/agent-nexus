package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
	} else {
		for _, row := range rows {
			item := row.(map[string]any)
			if item["created_by"].(map[string]any)["display"] != "Grouper" {
				t.Fatalf("creator missing on wire: %v", item)
			}
			actions := item["next_actions"].([]any)
			if len(actions) < 2 || actions[len(actions)-1].(map[string]any)["requires_human"] != true {
				t.Fatalf("tray actions missing: %v", actions)
			}
		}
	}
}

func TestGroupingWatchWorkstreamsOnWire(t *testing.T) {
	h := newProjectionMaintenanceTestServer(t)
	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"grouper","display_name":"Grouper","created_at":"2026-10-01T00:00:00Z"}}`, 201)
	root := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"grouper","title":"Root","summary":"Ship the board\nDetails","top_level":true}`, 201)["work"].(map[string]any)
	if root["top_level"] != true {
		t.Fatalf("intent lost on response: %v", root)
	}
	stream := workPostJSON(t, h.baseURL+"/work", fmt.Sprintf(`{"actor_id":"grouper","title":"P76: stream","under":%q}`, root["ref"]), 201)["work"].(map[string]any)
	workPostJSON(t, h.baseURL+"/work", fmt.Sprintf(`{"actor_id":"grouper","title":"fs-p3: leaf","under":%q}`, stream["ref"]), 201)
	read := workGetJSON(t, h.baseURL+"/work/"+asString(root["ref"]), 200)["work"].(map[string]any)
	assertRollup := func(item map[string]any) {
		t.Helper()
		rollup := item["rollup"].(map[string]any)
		streams := rollup["workstreams"].([]any)
		if len(streams) != 1 {
			t.Fatalf("no stream: %v", rollup)
		}
		w := streams[0].(map[string]any)
		if w["short_code"] != "p76" || w["rollup"].(map[string]any)["total"] != float64(1) {
			t.Fatalf("no nested rollup: %v", w)
		}
		leaf := w["rollup"].(map[string]any)["children"].([]any)[0].(map[string]any)
		if leaf["short_code"] != "fs-p3" || leaf["done_at"] != nil {
			t.Fatalf("leaf fields: %v", leaf)
		}
	}
	assertRollup(read)
	list := workGetJSON(t, h.baseURL+"/work?limit=20", 200)["work"].([]any)
	found := false
	for _, row := range list {
		item := row.(map[string]any)
		if item["ref"] == root["ref"] {
			assertRollup(item)
			found = true
		}
	}
	if !found {
		t.Fatal("root missing from work list")
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

func TestGroupingBatchCreateAtomicAndReplay(t *testing.T) {
	if testing.Short() {
		t.Skip("full HTTP/storage stack")
	}
	h := newProjectionMaintenanceTestServer(t)
	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"grouper","display_name":"Grouper","created_at":"2026-10-01T00:00:00Z"}}`, 201)
	parent := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"grouper","title":"Batch parent"}`, 201)["work"].(map[string]any)
	path := h.baseURL + "/boards/" + asString(parent["board_ref"]) + "/cards/batch"
	payload := map[string]any{"actor_id": "grouper", "request_key": "grouped-batch", "items": []any{
		map[string]any{"under": parent["ref"], "card": map[string]any{"handle": "batch-child-a", "title": "Child A"}},
		map[string]any{"card": map[string]any{"under": parent["ref"], "handle": "batch-child-b", "title": "Child B"}},
	}}
	raw, _ := json.Marshal(payload)
	first := workPostJSON(t, path, string(raw), 201)
	again := workPostJSON(t, path, string(raw), 201)
	if len(first["cards"].([]any)) != 2 || first["cards"].([]any)[0].(map[string]any)["ref"] != again["cards"].([]any)[0].(map[string]any)["ref"] {
		t.Fatal("batch replay duplicated cards")
	}
	for _, ref := range []string{"card:batch-child-a", "card:batch-child-b"} {
		child := workGetJSON(t, h.baseURL+"/work/"+ref, 200)["work"].(map[string]any)
		if child["parent_ref"] != parent["ref"] {
			t.Fatal("batch ignored grouping", child)
		}
	}
	planPath := h.baseURL + "/cards/" + asString(parent["ref"]) + "/plan"
	before := workGetJSON(t, planPath, 200)
	steps := before["plan"].(map[string]any)["steps"].([]any)
	if len(steps) != 2 {
		t.Fatalf("duplicate plan steps: %v", steps)
	}
	payload["request_key"] = "rejected-batch"
	payload["items"] = []any{
		map[string]any{"under": parent["ref"], "card": map[string]any{"handle": "batch-rollback-a", "title": "Rollback A"}},
		map[string]any{"under": "card:missing-parent", "card": map[string]any{"handle": "batch-rollback-b", "title": "Rollback B"}},
	}
	raw, _ = json.Marshal(payload)
	postJSONExpectStatus(t, path, string(raw), 404).Body.Close()
	after := workGetJSON(t, planPath, 200)
	if len(after["plan"].(map[string]any)["steps"].([]any)) != 2 {
		t.Fatal("failed batch changed parent plan")
	}
	workGetJSON(t, h.baseURL+"/work/card:batch-rollback-a", 404)
}

func TestInitiativeLifecycleWireReplayAndRefusal(t *testing.T) {
	h := newProjectionMaintenanceTestServer(t)
	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"grouper","display_name":"Grouper","created_at":"2026-10-01T00:00:00Z"}}`, 201)
	c := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"grouper","title":"Launch"}`, 201)["work"].(map[string]any)
	b := workPostJSON(t, h.baseURL+"/boards", `{"actor_id":"grouper","board":{"title":"Initiatives","role":"initiatives","initiative_target":0}}`, 201)["board"].(map[string]any)
	body := `{"actor_id":"grouper","request_key":"promote-once","reason":"New independent outcome"}`
	first := workPostJSON(t, h.baseURL+"/work/"+asString(c["ref"])+"/promote", body, 200)
	again := workPostJSON(t, h.baseURL+"/work/"+asString(c["ref"])+"/promote", body, 200)
	if first["event"].(map[string]any)["id"] != again["event"].(map[string]any)["id"] || first["over_target"] == nil || first["work"].(map[string]any)["board_ref"] != b["ref"] {
		t.Fatalf("first=%v replay=%v", first, again)
	}
	for _, verb := range []string{"group", "fold"} {
		key := "under"
		if verb == "fold" {
			key = "into"
		}
		errBody := workPostJSON(t, h.baseURL+"/work/"+asString(c["ref"])+"/"+verb, fmt.Sprintf(`{"actor_id":"grouper",%q:%q}`, key, c["ref"]), 400)
		if !strings.Contains(fmt.Sprint(errBody), "anx initiative retire") {
			t.Fatal(errBody)
		}
	}
	workPostJSON(t, h.baseURL+"/work/"+asString(c["ref"])+"/retire", `{"actor_id":"grouper","request_key":"retire-once","reason":"Fold scope"}`, 200)
}

func TestCardsMoveBoardRefIsAtomicAndReplaySafe(t *testing.T) {
	h := newProjectionMaintenanceTestServer(t)
	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"grouper","display_name":"Grouper","created_at":"2026-10-01T00:00:00Z"}}`, 201)
	c := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"grouper","title":"Curate"}`, 201)["work"].(map[string]any)
	anchor := workPostJSON(t, h.baseURL+"/work", fmt.Sprintf(`{"actor_id":"grouper","title":"Source anchor","board_ref":%q}`, c["board_ref"]), 201)["work"].(map[string]any)
	beforeCard := workGetJSON(t, h.baseURL+"/cards/"+asString(c["ref"]), 200)["card"].(map[string]any)
	source := workGetJSON(t, h.baseURL+"/boards/"+asString(c["board_ref"]), 200)["board"].(map[string]any)
	dest := workPostJSON(t, h.baseURL+"/boards", `{"actor_id":"grouper","board":{"title":"Destination"}}`, 201)["board"].(map[string]any)
	url := h.baseURL + "/cards/" + asString(c["ref"]) + "/move"
	body := map[string]any{"actor_id": "grouper", "board_ref": dest["ref"], "column_key": "ready", "if_board_updated_at": source["updated_at"], "if_version": c["version"], "reason": "Board curation", "request_key": "cross-board"}
	send := func(status int) map[string]any {
		raw, _ := json.Marshal(body)
		return workPostJSON(t, url, string(raw), status)
	}
	body["if_board_updated_at"] = "2000-01-01T00:00:00Z"
	send(409)
	body["if_board_updated_at"] = source["updated_at"]
	body["if_version"] = 999
	send(409)
	body["if_version"] = c["version"]
	body["before_card_id"] = anchor["ref"]
	send(400) // source-board anchor is not in destination
	delete(body, "before_card_id")
	moved := send(200)
	replay := send(200)
	for _, result := range []map[string]any{moved, replay} {
		card := result["card"].(map[string]any)
		if card["board_ref"] != dest["ref"] || card["ref"] != c["ref"] || card["thread_id"] != beforeCard["thread_id"] {
			t.Fatal(result)
		}
	}
	var count int
	if err := h.workspace.DB().QueryRow(`SELECT count(*) FROM events WHERE type='card_moved' AND json_extract(payload_json,'$.payload.before.board_ref')=? AND json_extract(payload_json,'$.payload.after.board_ref')=?`, "board:"+asString(source["id"]), "board:"+asString(dest["id"])).Scan(&count); err != nil || count != 1 {
		t.Fatalf("audit count=%d err=%v", count, err)
	}
	// Explicitly specifying the current board remains a normal column move.
	body["request_key"] = "same-board"
	body["if_board_updated_at"] = moved["board"].(map[string]any)["updated_at"]
	delete(body, "if_version")
	body["column_key"] = "review"
	send(200)
}

func TestBoardInitiativeTargetGetAndPatch(t *testing.T) {
	h := newProjectionMaintenanceTestServer(t)
	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"grouper","display_name":"Grouper","created_at":"2026-10-01T00:00:00Z"}}`, 201)
	b := workPostJSON(t, h.baseURL+"/boards", `{"actor_id":"grouper","board":{"title":"Outcomes","role":"initiatives","initiative_target":5}}`, 201)["board"].(map[string]any)
	url := h.baseURL + "/boards/" + asString(b["ref"])
	patch := func(value any, status int) {
		t.Helper()
		current := workGetJSON(t, url, 200)["board"].(map[string]any)
		raw, _ := json.Marshal(map[string]any{"actor_id": "grouper", "if_updated_at": current["updated_at"], "patch": map[string]any{"initiative_target": value}})
		req, _ := http.NewRequest("PATCH", url, strings.NewReader(string(raw)))
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != status {
			t.Fatalf("patch=%d %s", res.StatusCode, data)
		}
	}
	if got := workGetJSON(t, url, 200)["board"].(map[string]any)["initiative_target"]; got != float64(5) {
		t.Fatal(got)
	}
	patch(3, 200)
	if got := workGetJSON(t, url, 200)["board"].(map[string]any)["initiative_target"]; got != float64(3) {
		t.Fatal(got)
	}
	patch(-1, 400)
	patch(1.5, 400)
	patch(nil, 200)
	if got := workGetJSON(t, url, 200)["board"].(map[string]any)["initiative_target"]; got != nil {
		t.Fatal(got)
	}
}
