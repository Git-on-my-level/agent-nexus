package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestThreadsCreatePatchListAndTimeline(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"Incident thread",
			"type":"incident",
			"status":"active",
			"priority":"p1",
			"tags":["ops","backend"],
			"cadence":"daily",
			"next_check_in_at":"2020-01-01T00:00:00Z",
			"current_summary":"Investigating issue",
			"next_actions":["triage"],
			"key_artifacts":["artifact:seed"],
			"provenance":{"sources":["inferred"]},
			"custom_unknown":"preserve_me"
		}
	}`, http.StatusCreated)
	defer createResp.Body.Close()

	var created struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create thread response: %v", err)
	}
	threadID, _ := created.Thread["id"].(string)
	if threadID == "" {
		t.Fatal("expected created thread id")
	}

	openCommitments, ok := created.Thread["open_commitments"].([]any)
	if !ok || len(openCommitments) != 0 {
		t.Fatalf("expected open_commitments=[], got %#v", created.Thread["open_commitments"])
	}

	patchResp := patchJSONExpectStatus(t, h.baseURL+"/threads/"+threadID, `{
		"actor_id":"actor-1",
		"patch":{
			"title":"Incident thread (updated)",
			"tags":["backend"]
		}
	}`, http.StatusOK)
	defer patchResp.Body.Close()

	var patched struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(patchResp.Body).Decode(&patched); err != nil {
		t.Fatalf("decode patch thread response: %v", err)
	}
	if patched.Thread["title"] != "Incident thread (updated)" {
		t.Fatalf("unexpected patched title: %#v", patched.Thread["title"])
	}
	tags, ok := patched.Thread["tags"].([]any)
	if !ok || len(tags) != 1 || tags[0] != "backend" {
		t.Fatalf("unexpected patched tags: %#v", patched.Thread["tags"])
	}

	getResp, err := http.Get(h.baseURL + "/threads/" + threadID)
	if err != nil {
		t.Fatalf("GET /threads/{id}: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected get thread status: got %d", getResp.StatusCode)
	}

	var loaded struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&loaded); err != nil {
		t.Fatalf("decode get thread response: %v", err)
	}
	if loaded.Thread["custom_unknown"] != "preserve_me" {
		t.Fatalf("expected custom unknown field preserved, got %#v", loaded.Thread["custom_unknown"])
	}

	listFilteredResp, err := http.Get(h.baseURL + "/threads?status=active&priority=p1&tag=backend")
	if err != nil {
		t.Fatalf("GET /threads filtered: %v", err)
	}
	defer listFilteredResp.Body.Close()
	if listFilteredResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected filtered list status: got %d", listFilteredResp.StatusCode)
	}

	var listedFiltered struct {
		Threads []map[string]any `json:"threads"`
	}
	if err := json.NewDecoder(listFilteredResp.Body).Decode(&listedFiltered); err != nil {
		t.Fatalf("decode filtered list response: %v", err)
	}
	if len(listedFiltered.Threads) != 1 {
		t.Fatalf("expected exactly one filtered thread, got %d", len(listedFiltered.Threads))
	}
	if stale, ok := listedFiltered.Threads[0]["stale"].(bool); !ok || !stale {
		t.Fatalf("expected filtered thread to include stale=true, got %#v", listedFiltered.Threads[0]["stale"])
	}

	staleResp, err := http.Get(h.baseURL + "/threads?stale=true")
	if err != nil {
		t.Fatalf("GET /threads?stale=true: %v", err)
	}
	defer staleResp.Body.Close()
	if staleResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected stale list status: got %d", staleResp.StatusCode)
	}

	var staleListed struct {
		Threads []map[string]any `json:"threads"`
	}
	if err := json.NewDecoder(staleResp.Body).Decode(&staleListed); err != nil {
		t.Fatalf("decode stale list response: %v", err)
	}
	if len(staleListed.Threads) < 1 {
		t.Fatalf("expected stale list to contain created thread")
	}
	if stale, ok := staleListed.Threads[0]["stale"].(bool); !ok || !stale {
		t.Fatalf("expected stale list thread to include stale=true, got %#v", staleListed.Threads[0]["stale"])
	}

	timelineResp, err := http.Get(h.baseURL + "/threads/" + threadID + "/timeline")
	if err != nil {
		t.Fatalf("GET /threads/{id}/timeline: %v", err)
	}
	defer timelineResp.Body.Close()
	if timelineResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected timeline status: got %d", timelineResp.StatusCode)
	}

	var timeline struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(timelineResp.Body).Decode(&timeline); err != nil {
		t.Fatalf("decode timeline response: %v", err)
	}
	if len(timeline.Events) < 2 {
		t.Fatalf("expected at least 2 timeline events, got %d", len(timeline.Events))
	}

	for _, event := range timeline.Events {
		refs, ok := event["refs"].([]any)
		if !ok || !containsAny(refs, "snapshot:"+threadID) {
			t.Fatalf("timeline event missing snapshot ref: %#v", event)
		}
		assertActorStatementProvenance(t, event)
	}

	assertTimelineStableOrder(t, timeline.Events)

	lastEvent := timeline.Events[len(timeline.Events)-1]
	payload, ok := lastEvent["payload"].(map[string]any)
	if !ok {
		t.Fatalf("missing event payload: %#v", lastEvent)
	}
	changedFields, ok := payload["changed_fields"].([]any)
	if !ok {
		t.Fatalf("changed_fields missing: %#v", payload)
	}
	gotFields := anyListToSortedStrings(changedFields)
	wantFields := []string{"tags", "title"}
	if len(gotFields) != len(wantFields) || gotFields[0] != wantFields[0] || gotFields[1] != wantFields[1] {
		t.Fatalf("unexpected changed_fields: got %#v want %#v", gotFields, wantFields)
	}

	rejectResp := patchJSONExpectStatus(t, h.baseURL+"/threads/"+threadID, `{
		"actor_id":"actor-1",
		"patch":{"open_commitments":["c-1"]}
	}`, http.StatusBadRequest)
	defer rejectResp.Body.Close()
}

func TestPatchThreadIfUpdatedAtOptimisticLocking(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"Locking thread",
			"type":"incident",
			"status":"active",
			"priority":"p1",
			"tags":["ops"],
			"cadence":"daily",
			"next_check_in_at":"2026-03-05T00:00:00Z",
			"current_summary":"initial",
			"next_actions":["step-1"],
			"key_artifacts":[],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer createResp.Body.Close()

	var created struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create thread response: %v", err)
	}
	threadID, _ := created.Thread["id"].(string)
	initialUpdatedAt, _ := created.Thread["updated_at"].(string)
	if threadID == "" || initialUpdatedAt == "" {
		t.Fatalf("expected thread id and updated_at, got id=%q updated_at=%q", threadID, initialUpdatedAt)
	}

	matchedResp := patchJSONExpectStatus(t, h.baseURL+"/threads/"+threadID, `{
		"actor_id":"actor-1",
		"if_updated_at":"`+initialUpdatedAt+`",
		"patch":{"title":"Locking thread matched"}
	}`, http.StatusOK)
	defer matchedResp.Body.Close()

	var matched struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(matchedResp.Body).Decode(&matched); err != nil {
		t.Fatalf("decode matched patch response: %v", err)
	}
	if matched.Thread["title"] != "Locking thread matched" {
		t.Fatalf("unexpected matched patch title: %#v", matched.Thread["title"])
	}

	timelineBeforeResp, err := http.Get(h.baseURL + "/threads/" + threadID + "/timeline")
	if err != nil {
		t.Fatalf("GET timeline before conflict: %v", err)
	}
	defer timelineBeforeResp.Body.Close()
	if timelineBeforeResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected timeline before conflict status: %d", timelineBeforeResp.StatusCode)
	}
	var timelineBefore struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(timelineBeforeResp.Body).Decode(&timelineBefore); err != nil {
		t.Fatalf("decode timeline before conflict: %v", err)
	}

	conflictResp := patchJSONExpectStatus(t, h.baseURL+"/threads/"+threadID, `{
		"actor_id":"actor-1",
		"if_updated_at":"`+initialUpdatedAt+`",
		"patch":{"title":"Locking thread stale"}
	}`, http.StatusConflict)
	defer conflictResp.Body.Close()

	var conflictBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(conflictResp.Body).Decode(&conflictBody); err != nil {
		t.Fatalf("decode conflict response: %v", err)
	}
	if conflictBody.Error.Code != "conflict" {
		t.Fatalf("unexpected conflict code: %#v", conflictBody.Error.Code)
	}

	timelineAfterResp, err := http.Get(h.baseURL + "/threads/" + threadID + "/timeline")
	if err != nil {
		t.Fatalf("GET timeline after conflict: %v", err)
	}
	defer timelineAfterResp.Body.Close()
	if timelineAfterResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected timeline after conflict status: %d", timelineAfterResp.StatusCode)
	}
	var timelineAfter struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(timelineAfterResp.Body).Decode(&timelineAfter); err != nil {
		t.Fatalf("decode timeline after conflict: %v", err)
	}
	if len(timelineAfter.Events) != len(timelineBefore.Events) {
		t.Fatalf("conflict patch emitted event: before=%d after=%d", len(timelineBefore.Events), len(timelineAfter.Events))
	}

	getResp, err := http.Get(h.baseURL + "/threads/" + threadID)
	if err != nil {
		t.Fatalf("GET thread after conflict: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected get thread status after conflict: %d", getResp.StatusCode)
	}
	var loaded struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&loaded); err != nil {
		t.Fatalf("decode thread after conflict: %v", err)
	}
	if loaded.Thread["title"] != "Locking thread matched" {
		t.Fatalf("thread changed despite conflict: %#v", loaded.Thread["title"])
	}

	noLockResp := patchJSONExpectStatus(t, h.baseURL+"/threads/"+threadID, `{
		"actor_id":"actor-1",
		"patch":{"current_summary":"no lock still works"}
	}`, http.StatusOK)
	defer noLockResp.Body.Close()
}

func TestPatchThreadProvenanceRoundTrip(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"Provenance roundtrip thread",
			"type":"incident",
			"status":"active",
			"priority":"p1",
			"tags":["ops"],
			"cadence":"daily",
			"next_check_in_at":"2026-03-05T00:00:00Z",
			"current_summary":"initial",
			"next_actions":["step-1"],
			"key_artifacts":[],
			"provenance":{"sources":["actor_statement:event-create"],"notes":"created"},
			"custom_unknown":"persist_me"
		}
	}`, http.StatusCreated)
	defer createResp.Body.Close()

	var created struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create thread response: %v", err)
	}
	threadID, _ := created.Thread["id"].(string)
	if threadID == "" {
		t.Fatal("expected thread id")
	}

	patchResp := patchJSONExpectStatus(t, h.baseURL+"/threads/"+threadID, `{
		"actor_id":"actor-1",
		"patch":{
			"title":"Provenance roundtrip thread updated",
			"provenance":{
				"sources":["actor_statement:event-patch"],
				"notes":"patched"
			}
		}
	}`, http.StatusOK)
	defer patchResp.Body.Close()

	var patched struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(patchResp.Body).Decode(&patched); err != nil {
		t.Fatalf("decode patch thread response: %v", err)
	}
	provenance, ok := patched.Thread["provenance"].(map[string]any)
	if !ok {
		t.Fatalf("expected patched thread provenance object, got %#v", patched.Thread["provenance"])
	}
	notes, _ := provenance["notes"].(string)
	if notes != "patched" {
		t.Fatalf("expected patched provenance notes, got %#v", provenance["notes"])
	}

	getResp, err := http.Get(h.baseURL + "/threads/" + threadID)
	if err != nil {
		t.Fatalf("GET /threads/{id}: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected get thread status: got %d", getResp.StatusCode)
	}

	var loaded struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&loaded); err != nil {
		t.Fatalf("decode get thread response: %v", err)
	}
	loadedProvenance, ok := loaded.Thread["provenance"].(map[string]any)
	if !ok {
		t.Fatalf("expected loaded thread provenance object, got %#v", loaded.Thread["provenance"])
	}
	loadedNotes, _ := loadedProvenance["notes"].(string)
	if loadedNotes != "patched" {
		t.Fatalf("expected loaded patched provenance notes, got %#v", loadedProvenance["notes"])
	}
	if loaded.Thread["custom_unknown"] != "persist_me" {
		t.Fatalf("expected custom unknown field preserved, got %#v", loaded.Thread["custom_unknown"])
	}

	patchWithoutProvenanceResp := patchJSONExpectStatus(t, h.baseURL+"/threads/"+threadID, `{
		"actor_id":"actor-1",
		"patch":{"current_summary":"no provenance update"}
	}`, http.StatusOK)
	defer patchWithoutProvenanceResp.Body.Close()

	var patchedWithoutProvenance struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(patchWithoutProvenanceResp.Body).Decode(&patchedWithoutProvenance); err != nil {
		t.Fatalf("decode patch thread without provenance response: %v", err)
	}
	latestProvenance, ok := patchedWithoutProvenance.Thread["provenance"].(map[string]any)
	if !ok {
		t.Fatalf("expected provenance object after patch without provenance, got %#v", patchedWithoutProvenance.Thread["provenance"])
	}
	latestNotes, _ := latestProvenance["notes"].(string)
	if latestNotes != "patched" {
		t.Fatalf("expected provenance unchanged when omitted, got %#v", latestProvenance)
	}
}

func TestListThreadsCadenceAndMultiTagFilters(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createThread := func(title string, cadence string, tags []string) string {
		t.Helper()

		payload := map[string]any{
			"actor_id": "actor-1",
			"thread": map[string]any{
				"title":            title,
				"type":             "incident",
				"status":           "active",
				"priority":         "p1",
				"tags":             tags,
				"cadence":          cadence,
				"next_check_in_at": "2026-03-05T00:00:00Z",
				"current_summary":  "summary",
				"next_actions":     []string{"step-1"},
				"key_artifacts":    []string{},
				"provenance":       map[string]any{"sources": []string{"inferred"}},
			},
		}
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal thread create payload: %v", err)
		}

		resp := postJSONExpectStatus(t, h.baseURL+"/threads", string(body), http.StatusCreated)
		defer resp.Body.Close()

		var created struct {
			Thread map[string]any `json:"thread"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
			t.Fatalf("decode thread create response: %v", err)
		}
		threadID, _ := created.Thread["id"].(string)
		if threadID == "" {
			t.Fatalf("expected thread id for %s", title)
		}
		return threadID
	}

	threadDailyOpsBackend := createThread("daily ops backend", "daily", []string{"ops", "backend"})
	threadCronDailyOps := createThread("cron daily ops", "0 9 * * *", []string{"ops", "backend", "cron"})
	threadWeeklyOps := createThread("weekly ops", "weekly", []string{"ops"})
	threadWeeklyBackend := createThread("weekly backend", "weekly", []string{"backend"})
	threadReactiveOpsBackend := createThread("reactive ops backend", "reactive", []string{"ops", "backend", "infra"})
	threadLegacyCustom := createThread("legacy custom cadence", "custom", []string{"ops", "legacy-custom"})
	threadCustomCron := createThread("custom cron cadence", "*/15 * * * *", []string{"ops", "custom-cron"})

	listIDs := func(rawURL string) []string {
		t.Helper()
		resp, err := http.Get(rawURL)
		if err != nil {
			t.Fatalf("GET %s: %v", rawURL, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("unexpected status for %s: %d", rawURL, resp.StatusCode)
		}

		var payload struct {
			Threads []map[string]any `json:"threads"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
			t.Fatalf("decode list threads response for %s: %v", rawURL, err)
		}

		ids := make([]string, 0, len(payload.Threads))
		for _, thread := range payload.Threads {
			threadID, _ := thread["id"].(string)
			if threadID != "" {
				ids = append(ids, threadID)
			}
		}
		sort.Strings(ids)
		return ids
	}

	assertIDs := func(got []string, want []string, context string) {
		t.Helper()
		sort.Strings(want)
		if len(got) != len(want) {
			t.Fatalf("%s: unexpected result count got=%d want=%d ids=%#v", context, len(got), len(want), got)
		}
		for idx := range want {
			if got[idx] != want[idx] {
				t.Fatalf("%s: unexpected IDs got=%#v want=%#v", context, got, want)
			}
		}
	}

	assertIDs(
		listIDs(h.baseURL+"/threads?tag=backend"),
		[]string{threadCronDailyOps, threadDailyOpsBackend, threadReactiveOpsBackend, threadWeeklyBackend},
		"single tag filter",
	)

	assertIDs(
		listIDs(h.baseURL+"/threads?tag=ops&tag=backend"),
		[]string{threadCronDailyOps, threadDailyOpsBackend, threadReactiveOpsBackend},
		"multi tag AND filter",
	)

	assertIDs(
		listIDs(h.baseURL+"/threads?cadence=weekly"),
		[]string{threadWeeklyBackend, threadWeeklyOps},
		"single cadence filter",
	)

	assertIDs(
		listIDs(h.baseURL+"/threads?cadence=daily&cadence=weekly"),
		[]string{threadCronDailyOps, threadDailyOpsBackend, threadWeeklyBackend, threadWeeklyOps},
		"multi cadence filter",
	)

	assertIDs(
		listIDs(h.baseURL+"/threads?cadence=weekly&tag=backend"),
		[]string{threadWeeklyBackend},
		"cadence plus tag filter",
	)

	assertIDs(
		listIDs(h.baseURL+"/threads?cadence="+url.QueryEscape("0 9 * * *")),
		[]string{threadCronDailyOps, threadDailyOpsBackend},
		"canonical daily cron cadence filter",
	)

	assertIDs(
		listIDs(h.baseURL+"/threads?cadence=custom"),
		[]string{threadCustomCron, threadLegacyCustom},
		"custom cadence preset filter",
	)

	assertIDs(
		listIDs(h.baseURL+"/threads?cadence="+url.QueryEscape("*/15 * * * *")),
		[]string{threadCustomCron},
		"exact custom cron cadence filter",
	)
}

func TestThreadCadenceValidationSupportsCronAndRejectsInvalidValue(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	validCronResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"cron-valid-thread",
			"type":"incident",
			"status":"active",
			"priority":"p1",
			"tags":["ops"],
			"cadence":"0 9 * * *",
			"next_check_in_at":"2026-03-05T00:00:00Z",
			"current_summary":"summary",
			"next_actions":["step-1"],
			"key_artifacts":[],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	validCronResp.Body.Close()

	invalidResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"invalid-cadence-thread",
			"type":"incident",
			"status":"active",
			"priority":"p1",
			"tags":["ops"],
			"cadence":"every-day",
			"next_check_in_at":"2026-03-05T00:00:00Z",
			"current_summary":"summary",
			"next_actions":["step-1"],
			"key_artifacts":[],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusBadRequest)
	defer invalidResp.Body.Close()

	var payload map[string]any
	if err := json.NewDecoder(invalidResp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode invalid thread response: %v", err)
	}
	errObj, _ := payload["error"].(map[string]any)
	message, _ := errObj["message"].(string)
	if message == "" || !strings.Contains(message, "thread.cadence") {
		t.Fatalf("expected cadence validation error message, got %#v", payload)
	}
}

func TestThreadTimelineIncludesReferencedObjectsAndOmitsMissingRefs(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createThreadResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"Timeline expansion thread",
			"type":"incident",
			"status":"active",
			"priority":"p1",
			"tags":["ops","timeline"],
			"cadence":"daily",
			"next_check_in_at":"2026-03-05T00:00:00Z",
			"current_summary":"summary",
			"next_actions":["triage"],
			"key_artifacts":[],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer createThreadResp.Body.Close()

	var createdThread struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(createThreadResp.Body).Decode(&createdThread); err != nil {
		t.Fatalf("decode create thread response: %v", err)
	}
	threadID, _ := createdThread.Thread["id"].(string)
	if threadID == "" {
		t.Fatal("expected created thread id")
	}

	createCommitmentResp := postJSONExpectStatus(t, h.baseURL+"/commitments", `{
		"actor_id":"actor-1",
		"commitment":{
			"thread_id":"`+threadID+`",
			"title":"Timeline commitment",
			"owner":"actor-1",
			"due_at":"2026-03-08T00:00:00Z",
			"status":"open",
			"definition_of_done":["done"],
			"links":["url:https://example.com/work"],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer createCommitmentResp.Body.Close()

	var createdCommitment struct {
		Commitment map[string]any `json:"commitment"`
	}
	if err := json.NewDecoder(createCommitmentResp.Body).Decode(&createdCommitment); err != nil {
		t.Fatalf("decode create commitment response: %v", err)
	}
	commitmentID, _ := createdCommitment.Commitment["id"].(string)
	if commitmentID == "" {
		t.Fatal("expected created commitment id")
	}

	const artifactID = "timeline-artifact-1"
	createArtifactResp := postJSONExpectStatus(t, h.baseURL+"/artifacts", `{
		"actor_id":"actor-1",
		"artifact":{
			"id":"`+artifactID+`",
			"kind":"doc",
			"refs":["thread:`+threadID+`"],
			"summary":"timeline artifact"
		},
		"content":"artifact body",
		"content_type":"text"
	}`, http.StatusCreated)
	createArtifactResp.Body.Close()

	appendEventResp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"timeline_ref_test",
			"thread_id":"`+threadID+`",
			"refs":[
				"thread:`+threadID+`",
				"snapshot:`+commitmentID+`",
				"artifact:`+artifactID+`",
				"snapshot:missing-snapshot-id",
				"artifact:missing-artifact-id"
			],
			"summary":"timeline ref expansion event",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	appendEventResp.Body.Close()

	timelineResp, err := http.Get(h.baseURL + "/threads/" + threadID + "/timeline")
	if err != nil {
		t.Fatalf("GET /threads/{id}/timeline: %v", err)
	}
	defer timelineResp.Body.Close()
	if timelineResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected timeline status: got %d", timelineResp.StatusCode)
	}

	var timeline struct {
		Events    []map[string]any          `json:"events"`
		Snapshots map[string]map[string]any `json:"snapshots"`
		Artifacts map[string]map[string]any `json:"artifacts"`
	}
	if err := json.NewDecoder(timelineResp.Body).Decode(&timeline); err != nil {
		t.Fatalf("decode timeline response: %v", err)
	}
	if len(timeline.Events) == 0 {
		t.Fatal("expected timeline events")
	}
	if len(timeline.Snapshots) == 0 {
		t.Fatal("expected referenced snapshots in timeline response")
	}
	if len(timeline.Artifacts) == 0 {
		t.Fatal("expected referenced artifacts in timeline response")
	}

	if snapshot, ok := timeline.Snapshots[commitmentID]; !ok {
		t.Fatalf("expected commitment snapshot %q in timeline response, got keys=%#v", commitmentID, mapKeysMapAny(timeline.Snapshots))
	} else {
		if snapshot["id"] != commitmentID {
			t.Fatalf("unexpected commitment snapshot payload: %#v", snapshot)
		}
	}

	if artifact, ok := timeline.Artifacts[artifactID]; !ok {
		t.Fatalf("expected artifact %q in timeline response, got keys=%#v", artifactID, mapKeysMapAny(timeline.Artifacts))
	} else {
		if artifact["id"] != artifactID {
			t.Fatalf("unexpected artifact payload: %#v", artifact)
		}
	}

	if _, exists := timeline.Snapshots["missing-snapshot-id"]; exists {
		t.Fatalf("did not expect missing snapshot to be expanded: %#v", timeline.Snapshots["missing-snapshot-id"])
	}
	if _, exists := timeline.Artifacts["missing-artifact-id"]; exists {
		t.Fatalf("did not expect missing artifact to be expanded: %#v", timeline.Artifacts["missing-artifact-id"])
	}
}

func TestThreadTimelineIncludesDocumentLifecycleEventsAndExpansions(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createThreadResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"Document lifecycle thread",
			"type":"incident",
			"status":"active",
			"priority":"p1",
			"tags":["docs"],
			"cadence":"daily",
			"next_check_in_at":"2030-01-01T00:00:00Z",
			"current_summary":"Track document lifecycle",
			"next_actions":["Verify timeline output"],
			"key_artifacts":[],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer createThreadResp.Body.Close()

	var threadPayload struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(createThreadResp.Body).Decode(&threadPayload); err != nil {
		t.Fatalf("decode create thread response: %v", err)
	}
	threadID := asString(threadPayload.Thread["id"])
	if threadID == "" {
		t.Fatal("expected created thread id")
	}

	createDocResp := postJSONExpectStatus(t, h.baseURL+"/docs", `{
		"actor_id":"actor-1",
		"document":{"id":"timeline-doc-1","thread_id":"`+threadID+`","title":"Timeline Document"},
		"content":"draft v1",
		"content_type":"text"
	}`, http.StatusCreated)
	defer createDocResp.Body.Close()

	var createdDoc struct {
		Document map[string]any `json:"document"`
		Revision map[string]any `json:"revision"`
	}
	if err := json.NewDecoder(createDocResp.Body).Decode(&createdDoc); err != nil {
		t.Fatalf("decode create doc response: %v", err)
	}
	documentID := asString(createdDoc.Document["id"])
	createRevisionID := asString(createdDoc.Revision["revision_id"])
	createArtifactID := asString(createdDoc.Revision["artifact_id"])
	if documentID == "" || createRevisionID == "" || createArtifactID == "" {
		t.Fatalf("expected document lifecycle ids, got document=%#v revision=%#v", createdDoc.Document, createdDoc.Revision)
	}

	updateDocResp := patchJSONExpectStatus(t, h.baseURL+"/docs/"+documentID, `{
		"actor_id":"actor-1",
		"document":{"title":"Timeline Document v2"},
		"if_base_revision":"`+createRevisionID+`",
		"content":"draft v2",
		"content_type":"text"
	}`, http.StatusOK)
	defer updateDocResp.Body.Close()

	var updatedDoc struct {
		Document map[string]any `json:"document"`
		Revision map[string]any `json:"revision"`
	}
	if err := json.NewDecoder(updateDocResp.Body).Decode(&updatedDoc); err != nil {
		t.Fatalf("decode update doc response: %v", err)
	}
	updateRevisionID := asString(updatedDoc.Revision["revision_id"])
	updateArtifactID := asString(updatedDoc.Revision["artifact_id"])
	if updateRevisionID == "" || updateArtifactID == "" {
		t.Fatalf("expected updated revision ids, got %#v", updatedDoc.Revision)
	}

	tombstoneResp := postJSONExpectStatus(t, h.baseURL+"/docs/"+documentID+"/tombstone", `{
		"actor_id":"actor-1",
		"reason":"superseded by final document"
	}`, http.StatusOK)
	defer tombstoneResp.Body.Close()

	timelineResp, err := http.Get(h.baseURL + "/threads/" + threadID + "/timeline")
	if err != nil {
		t.Fatalf("GET /threads/{id}/timeline: %v", err)
	}
	defer timelineResp.Body.Close()
	if timelineResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected timeline status: got %d", timelineResp.StatusCode)
	}

	var timeline struct {
		Events            []map[string]any          `json:"events"`
		Snapshots         map[string]map[string]any `json:"snapshots"`
		Artifacts         map[string]map[string]any `json:"artifacts"`
		Documents         map[string]map[string]any `json:"documents"`
		DocumentRevisions map[string]map[string]any `json:"document_revisions"`
	}
	if err := json.NewDecoder(timelineResp.Body).Decode(&timeline); err != nil {
		t.Fatalf("decode timeline response: %v", err)
	}

	var createdEvent, updatedEvent, tombstonedEvent map[string]any
	for _, event := range timeline.Events {
		switch asString(event["type"]) {
		case "document_created":
			createdEvent = event
		case "document_updated":
			updatedEvent = event
		case "document_tombstoned":
			tombstonedEvent = event
		}
	}
	if createdEvent == nil || updatedEvent == nil || tombstonedEvent == nil {
		t.Fatalf("expected document lifecycle events in timeline, got %#v", timeline.Events)
	}

	assertDocLifecycleEventRefs(t, createdEvent, threadID, documentID, createRevisionID, createArtifactID)
	assertDocLifecycleEventRefs(t, updatedEvent, threadID, documentID, updateRevisionID, updateArtifactID)
	assertDocLifecycleEventRefs(t, tombstonedEvent, threadID, documentID, updateRevisionID, updateArtifactID)

	if doc, ok := timeline.Documents[documentID]; !ok {
		t.Fatalf("expected document %q in timeline documents, got keys=%#v", documentID, mapKeysMapAny(timeline.Documents))
	} else if doc["tombstoned_at"] == nil {
		t.Fatalf("expected tombstoned document metadata in timeline documents, got %#v", doc)
	}

	if revision, ok := timeline.DocumentRevisions[createRevisionID]; !ok {
		t.Fatalf("expected created revision %q in timeline document revisions, got keys=%#v", createRevisionID, mapKeysMapAny(timeline.DocumentRevisions))
	} else if asString(revision["artifact_id"]) != createArtifactID {
		t.Fatalf("unexpected created revision payload: %#v", revision)
	}
	if revision, ok := timeline.DocumentRevisions[updateRevisionID]; !ok {
		t.Fatalf("expected updated revision %q in timeline document revisions, got keys=%#v", updateRevisionID, mapKeysMapAny(timeline.DocumentRevisions))
	} else if asString(revision["artifact_id"]) != updateArtifactID {
		t.Fatalf("unexpected updated revision payload: %#v", revision)
	}
}

func TestThreadContextBundlesRecentEventsArtifactsAndOpenCommitments(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createThreadResp := postJSONExpectStatus(t, h.baseURL+"/threads", `{
		"actor_id":"actor-1",
		"thread":{
			"title":"Context bundle thread",
			"type":"incident",
			"status":"active",
			"priority":"p1",
			"tags":["ops","context"],
			"cadence":"daily",
			"next_check_in_at":"2026-03-05T00:00:00Z",
			"current_summary":"summary",
			"next_actions":["triage"],
			"key_artifacts":["artifact:ctx-artifact-1"],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer createThreadResp.Body.Close()

	var createdThread struct {
		Thread map[string]any `json:"thread"`
	}
	if err := json.NewDecoder(createThreadResp.Body).Decode(&createdThread); err != nil {
		t.Fatalf("decode create thread response: %v", err)
	}
	threadID, _ := createdThread.Thread["id"].(string)
	if threadID == "" {
		t.Fatal("expected created thread id")
	}

	contentBody := strings.Repeat("A", 620)
	createArtifactResp := postJSONExpectStatus(t, h.baseURL+"/artifacts", `{
		"actor_id":"actor-1",
		"artifact":{
			"id":"ctx-artifact-1",
			"kind":"doc",
			"refs":["thread:`+threadID+`"],
			"summary":"context artifact"
		},
		"content":"`+contentBody+`",
		"content_type":"text"
	}`, http.StatusCreated)
	createArtifactResp.Body.Close()

	createCommitmentResp := postJSONExpectStatus(t, h.baseURL+"/commitments", `{
		"actor_id":"actor-1",
		"commitment":{
			"thread_id":"`+threadID+`",
			"title":"Context commitment",
			"owner":"actor-1",
			"due_at":"2026-03-08T00:00:00Z",
			"status":"open",
			"definition_of_done":["done"],
			"links":["url:https://example.com/work"],
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer createCommitmentResp.Body.Close()

	var createdCommitment struct {
		Commitment map[string]any `json:"commitment"`
	}
	if err := json.NewDecoder(createCommitmentResp.Body).Decode(&createdCommitment); err != nil {
		t.Fatalf("decode create commitment response: %v", err)
	}
	commitmentID, _ := createdCommitment.Commitment["id"].(string)
	if commitmentID == "" {
		t.Fatal("expected created commitment id")
	}

	postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"context_probe_1",
			"thread_id":"`+threadID+`",
			"refs":["thread:`+threadID+`"],
			"summary":"context probe 1",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated).Body.Close()

	postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"context_probe_2",
			"thread_id":"`+threadID+`",
			"refs":["thread:`+threadID+`"],
			"summary":"context probe 2",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated).Body.Close()

	contextResp, err := http.Get(h.baseURL + "/threads/" + threadID + "/context?max_events=2&include_artifact_content=true")
	if err != nil {
		t.Fatalf("GET /threads/{id}/context: %v", err)
	}
	defer contextResp.Body.Close()
	if contextResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected context status: got %d", contextResp.StatusCode)
	}

	var payload struct {
		Thread          map[string]any   `json:"thread"`
		RecentEvents    []map[string]any `json:"recent_events"`
		KeyArtifacts    []map[string]any `json:"key_artifacts"`
		OpenCommitments []map[string]any `json:"open_commitments"`
	}
	if err := json.NewDecoder(contextResp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode context response: %v", err)
	}

	if payload.Thread["id"] != threadID {
		t.Fatalf("unexpected thread payload: %#v", payload.Thread["id"])
	}
	if len(payload.RecentEvents) != 2 {
		t.Fatalf("expected 2 recent events, got %d", len(payload.RecentEvents))
	}
	if asString(payload.RecentEvents[0]["type"]) != "context_probe_1" || asString(payload.RecentEvents[1]["type"]) != "context_probe_2" {
		t.Fatalf("unexpected recent event types: %#v", payload.RecentEvents)
	}

	if len(payload.KeyArtifacts) != 1 {
		t.Fatalf("expected 1 key artifact, got %d", len(payload.KeyArtifacts))
	}
	if asString(payload.KeyArtifacts[0]["ref"]) != "artifact:ctx-artifact-1" {
		t.Fatalf("unexpected key artifact ref: %#v", payload.KeyArtifacts[0])
	}
	artifactObj, _ := payload.KeyArtifacts[0]["artifact"].(map[string]any)
	if asString(artifactObj["id"]) != "ctx-artifact-1" {
		t.Fatalf("unexpected key artifact payload: %#v", payload.KeyArtifacts[0]["artifact"])
	}
	preview := asString(payload.KeyArtifacts[0]["content_preview"])
	if len(preview) != 500 {
		t.Fatalf("expected preview length 500, got %d", len(preview))
	}

	if len(payload.OpenCommitments) != 1 || asString(payload.OpenCommitments[0]["id"]) != commitmentID {
		t.Fatalf("unexpected open commitments payload: %#v", payload.OpenCommitments)
	}

	contextNoContentResp, err := http.Get(h.baseURL + "/threads/" + threadID + "/context?max_events=1")
	if err != nil {
		t.Fatalf("GET /threads/{id}/context without include_artifact_content: %v", err)
	}
	defer contextNoContentResp.Body.Close()
	if contextNoContentResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected context status (no content): got %d", contextNoContentResp.StatusCode)
	}

	var payloadNoContent struct {
		KeyArtifacts []map[string]any `json:"key_artifacts"`
	}
	if err := json.NewDecoder(contextNoContentResp.Body).Decode(&payloadNoContent); err != nil {
		t.Fatalf("decode context no content response: %v", err)
	}
	if len(payloadNoContent.KeyArtifacts) != 1 {
		t.Fatalf("expected 1 key artifact in no-content context, got %d", len(payloadNoContent.KeyArtifacts))
	}
	if _, exists := payloadNoContent.KeyArtifacts[0]["content_preview"]; exists {
		t.Fatalf("did not expect content_preview when include_artifact_content=false: %#v", payloadNoContent.KeyArtifacts[0])
	}

	contextZeroEventsResp, err := http.Get(h.baseURL + "/threads/" + threadID + "/context?max_events=0")
	if err != nil {
		t.Fatalf("GET /threads/{id}/context with max_events=0: %v", err)
	}
	defer contextZeroEventsResp.Body.Close()
	if contextZeroEventsResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected context status (max_events=0): got %d", contextZeroEventsResp.StatusCode)
	}

	var payloadZeroEvents struct {
		RecentEvents []map[string]any `json:"recent_events"`
	}
	if err := json.NewDecoder(contextZeroEventsResp.Body).Decode(&payloadZeroEvents); err != nil {
		t.Fatalf("decode context max_events=0 response: %v", err)
	}
	if len(payloadZeroEvents.RecentEvents) != 0 {
		t.Fatalf("expected 0 recent events when max_events=0, got %d", len(payloadZeroEvents.RecentEvents))
	}
}

func TestThreadContextRejectsInvalidQueryParams(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	resp, err := http.Get(h.baseURL + "/threads/thread-1/context?max_events=abc")
	if err != nil {
		t.Fatalf("GET /threads/{id}/context with invalid max_events: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid max_events, got %d", resp.StatusCode)
	}

	resp, err = http.Get(h.baseURL + "/threads/thread-1/context?include_artifact_content=maybe")
	if err != nil {
		t.Fatalf("GET /threads/{id}/context with invalid include_artifact_content: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid include_artifact_content, got %d", resp.StatusCode)
	}
}

func mapKeysMapAny(values map[string]map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func assertDocLifecycleEventRefs(t *testing.T, event map[string]any, threadID, documentID, revisionID, artifactID string) {
	t.Helper()

	refs, ok := event["refs"].([]any)
	if !ok {
		t.Fatalf("expected refs array on lifecycle event, got %#v", event["refs"])
	}
	if !containsAny(refs, "thread:"+threadID) {
		t.Fatalf("expected thread ref on lifecycle event, got %#v", refs)
	}
	if !containsAny(refs, "document:"+documentID) {
		t.Fatalf("expected document ref on lifecycle event, got %#v", refs)
	}
	if !containsAny(refs, "document_revision:"+revisionID) {
		t.Fatalf("expected document revision ref on lifecycle event, got %#v", refs)
	}
	if !containsAny(refs, "artifact:"+artifactID) {
		t.Fatalf("expected artifact ref on lifecycle event, got %#v", refs)
	}
}

func containsAny(values []any, expected string) bool {
	for _, value := range values {
		if text, ok := value.(string); ok && text == expected {
			return true
		}
	}
	return false
}

func anyListToSortedStrings(values []any) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		text, ok := value.(string)
		if !ok {
			continue
		}
		out = append(out, text)
	}
	sort.Strings(out)
	return out
}

func assertTimelineStableOrder(t *testing.T, events []map[string]any) {
	t.Helper()

	last := time.Time{}
	for index, event := range events {
		ts, ok := event["ts"].(string)
		if !ok {
			t.Fatalf("timeline event missing ts string at index %d: %#v", index, event)
		}
		parsed, err := time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			t.Fatalf("timeline ts parse error at index %d: %v", index, err)
		}
		if index > 0 && parsed.Before(last) {
			t.Fatalf("timeline out of order at index %d: %s before %s", index, parsed, last)
		}
		last = parsed
	}
}

func patchJSONExpectStatus(t *testing.T, url string, body string, expectedStatus int) *http.Response {
	t.Helper()

	req, err := http.NewRequest(http.MethodPatch, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("build PATCH %s request: %v", url, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH %s failed: %v", url, err)
	}
	if resp.StatusCode != expectedStatus {
		defer resp.Body.Close()
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("PATCH %s unexpected status: got %d want %d body=%s", url, resp.StatusCode, expectedStatus, string(bodyBytes))
	}
	return resp
}
