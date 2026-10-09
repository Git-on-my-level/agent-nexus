package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestWorkHTTPRegistrationObservationAndAuthority(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
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
	t.Parallel()
	requireIntegrationTest(t)
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

func TestWorkHTTPMigrationRelationRoundTrip(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	created := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"actor-1","title":"Legacy detail","source":{"authority":"github","connection_id":"fixture","native_id":"org/repo/issues/7"}}`, http.StatusCreated)
	legacy := created["work"].(map[string]any)
	board := workPostJSON(t, h.baseURL+"/boards", `{"actor_id":"actor-1","board":{"title":"Initiatives"}}`, http.StatusCreated)["board"].(map[string]any)
	initiative := workPostJSON(t, h.baseURL+"/work", fmt.Sprintf(`{"actor_id":"actor-1","board_ref":%q,"title":"Reliable execution initiative"}`, board["ref"]), http.StatusCreated)["work"].(map[string]any)
	workPostJSON(t, h.baseURL+"/work/"+asString(legacy["ref"])+"/observations", `{"actor_id":"actor-1","observation":{"idempotency_key":"adapter-read","reader_id":"example-adapter/github","reader_revision":"0.1.0","observed_at":"2026-10-01T00:00:00Z","status":"reported","source_revision":"source-fence","facts":{"phase":"blocked"}}}`, http.StatusOK)
	legacy = workGetJSON(t, h.baseURL+"/work/"+asString(legacy["ref"]), http.StatusOK)["work"].(map[string]any)
	_, err := h.workspace.DB().ExecContext(context.Background(), `WITH RECURSIVE numbers(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM numbers WHERE n<1000)
		INSERT OR IGNORE INTO events(id,handle,type,ts,actor_id,thread_id,refs_json,payload_json)
		SELECT 'fixture-' || n, CASE WHEN n=1 THEN 'card-updated' ELSE 'card-updated-' || n END,
		'card_updated','2026-10-01T00:00:00Z','actor-1','','[]','{}' FROM numbers`)
	if err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"actor_id":"actor-1","if_version":1,"patch":{"relations":[{"kind":"related","ref":%q,"adapter_migration":"db92c565fbc1c2fa88fe2538306f9292c8523aab5eb8210f7af5b245aa036703","note":"Folded into initiative; archived detail retained, source remains authoritative."}]}}`, initiative["ref"])
	resp := patchJSONExpectStatus(t, h.baseURL+"/work/"+asString(legacy["ref"]), body, http.StatusOK)
	resp.Body.Close()
	readback := workGetJSON(t, h.baseURL+"/work/"+asString(legacy["ref"]), http.StatusOK)["work"].(map[string]any)
	relation := readback["relations"].([]any)[0].(map[string]any)
	if relation["ref"] != initiative["ref"] || relation["adapter_migration"] != "db92c565fbc1c2fa88fe2538306f9292c8523aab5eb8210f7af5b245aa036703" || relation["note"] != "Folded into initiative; archived detail retained, source remains authoritative." {
		t.Fatalf("migration relation did not round trip: %#v", relation)
	}
	if readback["decision_revision"] != "source-fence" || readback["phase"] != legacy["phase"] || readback["updated_at"] != legacy["updated_at"] || readback["version"] != float64(2) {
		t.Fatalf("migration annotation changed the source fence: %#v", readback)
	}
	old := workGetJSON(t, h.baseURL+"/events/event:card-updated-1000", http.StatusOK)
	if old["event"].(map[string]any)["ref"] != "event:card-updated-1000" {
		t.Fatal("existing event ref changed")
	}
	for _, invalid := range []string{
		`null`, `{}`, `[null]`, `["card:missing"]`,
		`[{"kind":"related"}]`, `[{"kind":"bogus","ref":"card:missing"}]`,
		`[{"kind":"related","ref":"card:missing"}]`,
		`[{"kind":"parent","ref":"topic:missing"}]`,
		`[{"kind":"related","ref":123}]`,
	} {
		t.Run(invalid, func(t *testing.T) {
			resp := patchJSONExpectStatus(t, h.baseURL+"/work/"+asString(legacy["ref"]), `{"actor_id":"actor-1","if_version":2,"patch":{"relations":`+invalid+`}}`, http.StatusBadRequest)
			defer resp.Body.Close()
			var payload map[string]any
			if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			e := payload["error"].(map[string]any)
			if e["code"] != "invalid_request" || !strings.Contains(asString(e["message"]), "relations") {
				t.Fatalf("unclear relation error: %#v", e)
			}
		})
	}
	after := workGetJSON(t, h.baseURL+"/work/"+asString(legacy["ref"]), http.StatusOK)["work"].(map[string]any)
	if after["version"] != float64(2) || len(after["relations"].([]any)) != 1 {
		t.Fatal("invalid relation mutated work")
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

func TestArchiveCardLatestObservationFence(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	workPostJSON(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	created := workPostJSON(t, h.baseURL+"/work", `{"actor_id":"actor-1","title":"Legacy detail","source":{"authority":"github","connection_id":"fixture","native_id":"org/repo/issues/observation-fence"}}`, http.StatusCreated)
	legacy := created["work"].(map[string]any)
	ref := asString(legacy["ref"])
	archiveURL := h.baseURL + "/cards/" + ref + "/archive"
	for _, body := range []string{
		`{"actor_id":"actor-1","if_latest_observation_id":""}`,
		`{"actor_id":"actor-1","if_latest_observation_id":"   "}`,
	} {
		workPostJSON(t, archiveURL, body, http.StatusBadRequest)
	}
	workPostJSON(t, archiveURL, `{"actor_id":"actor-1","if_latest_observation_id":"missing"}`, http.StatusConflict)
	observe := func(key, at string) {
		t.Helper()
		body := fmt.Sprintf(`{"actor_id":"actor-1","observation":{"idempotency_key":%q,"reader_id":"example-adapter/github","reader_revision":"1","observed_at":%q,"status":"reported","facts":{"phase":"blocked"}}}`, key, at)
		workPostJSON(t, h.baseURL+"/work/"+ref+"/observations", body, http.StatusOK)
	}
	observe("first", "2026-10-01T00:00:00Z")
	before := workGetJSON(t, h.baseURL+"/work/"+ref, http.StatusOK)["work"].(map[string]any)
	oldID := before["latest_observation"].(map[string]any)["id"]
	boardURL := h.baseURL + "/boards/" + asString(before["board_ref"])
	board := workGetJSON(t, boardURL, http.StatusOK)["board"].(map[string]any)
	observe("same-phase-new-poll", "2026-10-01T00:01:00Z")
	after := workGetJSON(t, h.baseURL+"/work/"+ref, http.StatusOK)["work"].(map[string]any)
	latestID := after["latest_observation"].(map[string]any)["id"]
	currentBoard := workGetJSON(t, boardURL, http.StatusOK)["board"].(map[string]any)
	if oldID == latestID || before["version"] != after["version"] || before["phase"] != after["phase"] || board["updated_at"] != currentBoard["updated_at"] {
		t.Fatalf("fixture must change observation only: before=%#v after=%#v boards=%#v / %#v", before, after, board, currentBoard)
	}
	body := fmt.Sprintf(`{"actor_id":"actor-1","if_board_updated_at":%q,"if_version":%v,"if_latest_observation_id":%q}`, board["updated_at"], after["version"], oldID)
	workPostJSON(t, archiveURL, body, http.StatusConflict)
	card := workGetJSON(t, h.baseURL+"/cards/"+ref, http.StatusOK)["card"].(map[string]any)
	if asString(card["archived_at"]) != "" {
		t.Fatal("stale observation fence archived the card")
	}
	boardAfterConflict := workGetJSON(t, boardURL, http.StatusOK)["board"].(map[string]any)
	if boardAfterConflict["updated_at"] != board["updated_at"] {
		t.Fatal("failed archive mutated board")
	}
	body = fmt.Sprintf(`{"actor_id":"actor-1","if_board_updated_at":%q,"if_version":%v,"if_latest_observation_id":%q}`, board["updated_at"], after["version"], latestID)
	workPostJSON(t, archiveURL, body, http.StatusOK)
	card = workGetJSON(t, h.baseURL+"/cards/"+ref, http.StatusOK)["card"].(map[string]any)
	if asString(card["archived_at"]) == "" {
		t.Fatal("matching fences did not archive")
	}
}
