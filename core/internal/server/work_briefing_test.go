package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestWorkBriefingDeterministicAndExplicitCoverage(t *testing.T) {
	work := map[string]any{"ref": "card:research", "title": "Compare papers", "freshness": map[string]any{"status": "error", "last_error": map[string]any{"message": "secret diagnostic"}}, "phase": "review", "owner": "actor:writer", "checkpoint": map[string]any{"version": 1, "content": map[string]any{"next_action": "Read reviewer objections"}}, "latest_observation": map[string]any{"status": "uncertain", "source_revision": "r7", "facts": map[string]any{"native_status": "vendor-new-state"}}, "refresh": map[string]any{"state": "failed", "lease_token": "secret", "error": "secret diagnostic"}}
	a, _ := json.Marshal(buildWorkBriefing(work))
	b, _ := json.Marshal(buildWorkBriefing(work))
	if string(a) != string(b) {
		t.Fatal("nondeterministic projection")
	}
	if strings.Contains(string(a), "secret") || !strings.Contains(string(a), "vendor-new-state") {
		t.Fatalf("bad projection: %s", a)
	}
	result := buildWorkBriefing(work)
	if result["coverage"].(map[string]any)["ownership"] != "unchanged" {
		t.Fatal("read must not take ownership")
	}
	work["summary"] = strings.Repeat("x", 8193)
	result = buildWorkBriefing(work)
	if len(result["omitted"].([]string)) != 1 || result["coverage"].(map[string]any)["complete"] != false {
		t.Fatal("oversize field must explicitly mark omission")
	}
}

func TestWorkBriefingBoundsAggregateWithoutDroppingCheckpoint(t *testing.T) {
	work := map[string]any{"checkpoint": map[string]any{"content": strings.Repeat("x", 16000)}}
	for _, key := range []string{"title", "summary", "definition_of_done", "source", "phase", "owner", "next_actor", "next_action", "blockers", "relations", "executions", "source_refs"} {
		work[key] = strings.Repeat("x", 8000)
	}
	result := buildWorkBriefing(work)
	encoded, err := json.Marshal(result)
	if err != nil || len(encoded) > 65536 || result["checkpoint"] == nil || len(result["omitted"].([]string)) == 0 {
		t.Fatalf("unbounded or silently incomplete result: bytes=%d err=%v", len(encoded), err)
	}
}

func TestWorkBriefingHTTPAndCheckpoint(t *testing.T) {
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"One","created_at":"2026-03-04T10:00:00Z"}}`, 201)
	created := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"actor-1","title":"Review research","source":{"authority":"nexus"}}`, 201)
	item := created["work"].(map[string]any)
	path := h.baseURL + "/work/" + item["ref"].(string)
	put := func(body string, status int) map[string]any {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPut, path+"/checkpoint", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var result map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&result)
		if resp.StatusCode != status {
			t.Fatalf("status %d want %d: %#v", resp.StatusCode, status, result)
		}
		return result
	}
	body := `{"actor_id":"actor-1","if_version":0,"checkpoint":{"summary":"Draft preserved","next_action":"Resolve objection","artifacts":[{"ref":"file:notes.txt","portability":"local_only","host":"laptop"}]}}`
	first := put(body, 200)
	duplicate := put(body, 200)
	if duplicate["unchanged"] != true || first["version"] != duplicate["version"] {
		t.Fatal("same checkpoint grew history")
	}
	put(strings.Replace(body, "Resolve objection", "Publish result", 1), 409)
	resp, err := http.Get(path + "/briefing")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var result map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&result)
	if resp.StatusCode != 200 {
		t.Fatalf("briefing %d: %#v", resp.StatusCode, result)
	}
	if result["checkpoint"].(map[string]any)["version"] != float64(1) {
		t.Fatal(result)
	}
	work := result["work"].(map[string]any)
	if work["version"] != item["version"] || work["phase"] != item["phase"] {
		t.Fatal("checkpoint changed work semantics", work, item)
	}
}
