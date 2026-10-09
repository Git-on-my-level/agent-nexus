package server

import (
	"agent-nexus-core/internal/primitives"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestHumanAttentionDerivationAndResponseSuppressesItem(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()

	h := newPrimitivesTestServerWithHumanPrincipal(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":           "Human attention thread",
		"type":            "incident",
		"status":          "active",
		"priority":        "p1",
		"tags":            []any{"ops"},
		"cadence":         "daily",
		"current_summary": "summary",
		"next_actions":    []any{"do x"},
		"key_artifacts":   []any{},
		"provenance":      map[string]any{"sources": []any{"inferred"}},
	})

	created := createHumanAttentionEvent(t, h.baseURL, threadID, "ask", "Should we ship Friday?", "topic:launch", []string{"artifact:analysis"}, map[string]any{
		"body":               "I found conflicting dates.",
		"coverage_hint":      "thin - 0 decisions",
		"requester_actor_id": "actor-agent",
		"requester_agent_id": "agent-a",
		"requester_label":    "agent-a",
	})
	requestEventID := asString(created["id"])
	if requestEventID == "" {
		t.Fatalf("expected request event id, got %#v", created)
	}

	items := getInboxItems(t, h.baseURL)
	item, ok := findInboxItem(items, func(candidate map[string]any) bool {
		return asString(candidate["kind"]) == "ask" && asString(candidate["source_event_id"]) == requestEventID
	})
	if !ok {
		t.Fatalf("expected human ask inbox item for source_event_id=%s, got %#v", requestEventID, items)
	}
	if got := asString(item["body"]); got != "I found conflicting dates." {
		t.Fatalf("expected item body, got %#v", item)
	}
	rp, ok := item["response_proposals"].([]any)
	if !ok || len(rp) < 1 {
		t.Fatalf("expected response_proposals on inbox item, got %#v", item["response_proposals"])
	}
	if got := asString(item["requester_agent_id"]); got != "agent-a" {
		t.Fatalf("expected requester_agent_id, got %#v", item)
	}
	status, _ := item["notification_target_status"].(map[string]any)
	if got := asString(status["state"]); got != "unresolved" {
		t.Fatalf("expected unresolved notification state without registered agent, got %#v", status)
	}

	itemID := asString(item["id"])
	for _, bad := range []string{
		`{"actor_id":"human-purge-principal-actor","response_text":"Approved.","notify_mode":"none"}`,
		`{"actor_id":"human-purge-principal-actor","response_text":"Approved.","outcome":"maybe","notify_mode":"none"}`,
	} {
		invalid := postJSONExpectStatusWithHeaders(t, h.baseURL+"/inbox/"+url.PathEscape(itemID)+"/respond", json.RawMessage(bad), map[string]string{"Authorization": "Bearer " + h.humanAccessToken}, http.StatusBadRequest)
		assertErrorCode(t, invalid, "invalid_request")
	}
	resp := postJSONExpectStatusWithHeaders(t, h.baseURL+"/inbox/"+url.PathEscape(itemID)+"/respond", json.RawMessage(`{
		"actor_id":"human-purge-principal-actor",
		"response_text":"Ship Friday with a rollback plan.",
		"outcome":"answered",
		"notify_mode":"none",
		"related_refs":["artifact:decision_note"]
	}`), map[string]string{"Authorization": "Bearer " + h.humanAccessToken}, http.StatusCreated)
	defer resp.Body.Close()

	var response struct {
		Event  map[string]any `json:"event"`
		Notify map[string]any `json:"notify"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := asString(response.Event["type"]); got != "human_attention_responded" {
		t.Fatalf("expected generic response event, got %#v", response.Event)
	}
	payload, _ := response.Event["payload"].(map[string]any)
	if got := asString(payload["response_text"]); got != "Ship Friday with a rollback plan." {
		t.Fatalf("expected response_text payload, got %#v", payload)
	}
	if got := asString(payload["outcome"]); got != "answered" {
		t.Fatalf("expected answered outcome payload, got %#v", payload)
	}
	completed := getInboxPayload(t, h.baseURL+"/inbox?status=completed")
	row, found := findInboxItem(completed.Items, func(candidate map[string]any) bool {
		return asString(candidate["response_event_ref"]) == "event:"+asString(response.Event["id"])
	})
	if !found || asString(row["outcome"]) != "answered" {
		t.Fatalf("completed inbox item lost outcome: %#v", completed.Items)
	}
	if got := asString(response.Notify["mode"]); got != "none" {
		t.Fatalf("expected no notification target metadata, got %#v", response.Notify)
	}

	itemsAfterResponse := getInboxItems(t, h.baseURL)
	if _, stillThere := findInboxItem(itemsAfterResponse, func(candidate map[string]any) bool {
		return asString(candidate["id"]) == itemID
	}); stillThere {
		t.Fatalf("expected response event to suppress original item, got %#v", itemsAfterResponse)
	}
}

func TestHumanAttentionSupportsReviewAndEscalateKinds(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()

	h := newPrimitivesTestServerWithHumanPrincipal(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":           "Human attention kinds",
		"type":            "incident",
		"status":          "active",
		"priority":        "p1",
		"tags":            []any{},
		"cadence":         "daily",
		"current_summary": "summary",
		"next_actions":    []any{},
		"key_artifacts":   []any{},
		"provenance":      map[string]any{"sources": []any{"inferred"}},
	})

	createHumanAttentionEvent(t, h.baseURL, threadID, "review", "Please review launch notes", "document:launch_notes", nil, nil)
	createHumanAttentionEvent(t, h.baseURL, threadID, "escalate", "Possible leaked secret", "artifact:scan_result", nil, map[string]any{"severity": "high"})

	items := getInboxItems(t, h.baseURL)
	if _, ok := findInboxItem(items, func(item map[string]any) bool {
		return asString(item["kind"]) == "review" && asString(item["title"]) == "Please review launch notes"
	}); !ok {
		t.Fatalf("expected review inbox item, got %#v", items)
	}
	escalation, ok := findInboxItem(items, func(item map[string]any) bool {
		return asString(item["kind"]) == "escalate" && asString(item["severity"]) == "high"
	})
	if !ok {
		t.Fatalf("expected escalation inbox item, got %#v", items)
	}

	postJSONExpectStatusWithHeaders(t, h.baseURL+"/inbox/"+url.PathEscape(asString(escalation["id"]))+"/respond", json.RawMessage(`{
		"actor_id":"human-purge-principal-actor",
		"response_text":"Investigating now.",
		"outcome":"answered",
		"notify_mode":"none"
	}`), map[string]string{"Authorization": "Bearer " + h.humanAccessToken}, http.StatusCreated).Body.Close()
}

func TestInboxReadsMaterializedProjectionWithFreshness(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()
	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":           "Materialized inbox projection",
		"type":            "incident",
		"status":          "active",
		"priority":        "p1",
		"tags":            []any{},
		"cadence":         "daily",
		"current_summary": "summary",
		"next_actions":    []any{},
		"key_artifacts":   []any{},
		"provenance":      map[string]any{"sources": []any{"inferred"}},
	})
	// Use an explicit board so this projection-read test does not also exercise
	// lazy default-board provisioning, whose backing thread starts unmaterialized.
	board := workPostJSON(t, h.baseURL+"/boards", `{"actor_id":"actor-1","board":{"title":"Materialized ask subjects"}}`, http.StatusCreated)["board"].(map[string]any)
	work := workPostJSON(t, h.baseURL+"/work", fmt.Sprintf(`{"actor_id":"actor-1","board_ref":%q,"title":"Need materialized answer","phase":"ready"}`, board["ref"]), http.StatusCreated)["work"].(map[string]any)
	created := createHumanAttentionEvent(t, h.baseURL, threadID, "ask", "Need materialized answer", asString(work["ref"]), []string{"thread:" + threadID}, nil)
	requestEventID := asString(created["id"])

	standard := getInboxPayload(t, h.baseURL+"/inbox")
	if asString(standard.ProjectionFreshness["status"]) != "current" {
		t.Fatalf("expected current freshness on inbox reads, got %#v", standard.ProjectionFreshness)
	}

	item, ok := findInboxItem(standard.Items, func(candidate map[string]any) bool {
		return asString(candidate["kind"]) == "ask" && asString(candidate["source_event_id"]) == requestEventID
	})
	if !ok {
		t.Fatalf("expected materialized inbox item for source_event_id=%s, got %#v", requestEventID, standard.Items)
	}

	itemResp, err := http.Get(h.baseURL + "/inbox/" + url.PathEscape(asString(item["id"])))
	if err != nil {
		t.Fatalf("GET /inbox/{id}: %v", err)
	}
	defer itemResp.Body.Close()
	if itemResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected GET /inbox/{id} status: %d", itemResp.StatusCode)
	}
	var itemPayload struct {
		Item                map[string]any `json:"item"`
		ProjectionFreshness map[string]any `json:"projection_freshness"`
	}
	if err := json.NewDecoder(itemResp.Body).Decode(&itemPayload); err != nil {
		t.Fatalf("decode /inbox/{id} response: %v", err)
	}
	if asString(itemPayload.Item["id"]) != asString(item["id"]) {
		t.Fatalf("expected GET /inbox/{id} to load same materialized item, got %#v want %#v", itemPayload.Item, item)
	}
	if got := asString(itemPayload.ProjectionFreshness["status"]); got != "current" {
		t.Fatalf("expected item projection freshness, got %#v", itemPayload.ProjectionFreshness)
	}
}

func TestInboxReadDoesNotRecomputePendingProjection(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()

	h := newManualProjectionTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()

	threadID := createBoardThreadViaHTTP(t, h.primitivesTestHarness, "Pending inbox projection")
	createHumanAttentionEvent(t, h.baseURL, threadID, "ask", "Do not recompute on read", "thread:"+threadID, nil, nil)

	inboxBefore := countTableRows(t, h.workspace.DB(), "derived_inbox_items")
	payload := getInboxPayload(t, h.baseURL+"/inbox")
	if got := asString(payload.ProjectionFreshness["status"]); got != "pending" {
		t.Fatalf("expected pending freshness before worker runs, got %#v", payload.ProjectionFreshness)
	}
	if len(payload.Items) != 0 {
		t.Fatalf("expected no read-time recomputed inbox items, got %#v", payload.Items)
	}
	if inboxAfter := countTableRows(t, h.workspace.DB(), "derived_inbox_items"); inboxAfter != inboxBefore {
		t.Fatalf("expected inbox read not to write derived inbox rows, got before=%d after=%d", inboxBefore, inboxAfter)
	}
}

func TestHumanAttentionResponseRequiresResolvableTargetOrExplicitNone(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()

	h := newPrimitivesTestServerWithHumanPrincipal(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":           "Notification target thread",
		"type":            "incident",
		"status":          "active",
		"priority":        "p1",
		"tags":            []any{},
		"cadence":         "daily",
		"current_summary": "summary",
		"next_actions":    []any{},
		"key_artifacts":   []any{},
		"provenance":      map[string]any{"sources": []any{"inferred"}},
	})
	createHumanAttentionEvent(t, h.baseURL, threadID, "ask", "Need unavailable requester", "thread:"+threadID, nil, map[string]any{
		"requester_actor_id": "actor-missing",
		"requester_agent_id": "agent-missing",
	})

	items := getInboxItems(t, h.baseURL)
	item, ok := findInboxItem(items, func(candidate map[string]any) bool {
		return asString(candidate["kind"]) == "ask"
	})
	if !ok {
		t.Fatalf("expected human ask item, got %#v", items)
	}

	resp := postJSONExpectStatusWithHeaders(t, h.baseURL+"/inbox/"+url.PathEscape(asString(item["id"]))+"/respond", json.RawMessage(`{
		"actor_id":"human-purge-principal-actor",
		"response_text":"Answer text"
		,"outcome":"answered"
	}`), map[string]string{"Authorization": "Bearer " + h.humanAccessToken}, http.StatusConflict)
	defer resp.Body.Close()
	assertErrorCode(t, resp, "notification_target_required")
}

func createHumanAttentionEvent(t *testing.T, baseURL, threadID, kind, title, subjectRef string, relatedRefs []string, extra map[string]any) map[string]any {
	t.Helper()
	if relatedRefs == nil {
		relatedRefs = []string{}
	}
	// Successful new-ask fixtures use real cards; prior non-card subjects remain
	// related evidence so the test still exercises their original relationships.
	if !strings.HasPrefix(subjectRef, "card:") {
		relatedRefs = append(relatedRefs, subjectRef)
		resp := postJSONExpectStatus(t, baseURL+"/work", string(mustJSON(t, map[string]any{"actor_id": "actor-1", "title": title, "phase": "ready"})), http.StatusCreated)
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		subjectRef = asString(body["work"].(map[string]any)["ref"])
	}

	payload := map[string]any{
		"kind":               kind,
		"title":              title,
		"subject_ref":        subjectRef,
		"related_refs":       relatedRefs,
		"requester_actor_id": "actor-1",
		"response_proposals": []any{"Recommended response.", "Alternative suggestion."},
	}
	for key, value := range extra {
		payload[key] = value
	}
	refs := []string{"thread:" + threadID, subjectRef}
	refs = append(refs, relatedRefs...)
	body := map[string]any{
		"actor_id": "actor-1",
		"event": map[string]any{
			"type":       "human_attention_requested",
			"thread_id":  threadID,
			"refs":       refs,
			"summary":    title,
			"payload":    payload,
			"provenance": map[string]any{"sources": []any{"inferred"}},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal human attention event: %v", err)
	}
	resp := postJSONExpectStatus(t, baseURL+"/events", string(raw), http.StatusCreated)
	defer resp.Body.Close()
	var created struct {
		Event map[string]any `json:"event"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode human attention event response: %v", err)
	}
	return created.Event
}

func getInboxItems(t *testing.T, baseURL string) []map[string]any {
	t.Helper()
	return getInboxPayload(t, baseURL+"/inbox").Items
}

type inboxPayloadForTest struct {
	Items               []map[string]any `json:"items"`
	ProjectionFreshness map[string]any   `json:"projection_freshness"`
}

func getInboxPayload(t *testing.T, rawURL string) inboxPayloadForTest {
	t.Helper()
	resp, err := http.Get(rawURL)
	if err != nil {
		t.Fatalf("GET %s: %v", rawURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected GET %s status: %d", rawURL, resp.StatusCode)
	}

	var payload inboxPayloadForTest
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode %s response: %v", rawURL, err)
	}
	return payload
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	return raw
}

func findInboxItem(items []map[string]any, predicate func(map[string]any) bool) (map[string]any, bool) {
	for _, item := range items {
		if predicate(item) {
			return item, true
		}
	}
	return nil, false
}

func TestHumanAttentionRequestedRejectsInvalidResponseProposals(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	threadID := integrationSeedThread(t, h, "actor-1", map[string]any{
		"title":           "Proposal validation thread",
		"type":            "incident",
		"status":          "active",
		"priority":        "p1",
		"tags":            []any{},
		"cadence":         "daily",
		"current_summary": "summary",
		"next_actions":    []any{},
		"key_artifacts":   []any{},
		"provenance":      map[string]any{"sources": []any{"inferred"}},
	})

	basePayload := map[string]any{
		"kind":               "ask",
		"title":              "Question",
		"subject_ref":        "thread:" + threadID,
		"related_refs":       []any{},
		"requester_actor_id": "actor-1",
	}

	postBad := func(payload map[string]any) {
		t.Helper()
		refs := []any{"thread:" + threadID, "thread:" + threadID}
		body := map[string]any{
			"actor_id": "actor-1",
			"event": map[string]any{
				"type":       "human_attention_requested",
				"thread_id":  threadID,
				"refs":       refs,
				"summary":    "Question",
				"payload":    payload,
				"provenance": map[string]any{"sources": []any{"inferred"}},
			},
		}
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		resp := postJSONExpectStatus(t, h.baseURL+"/events", string(raw), http.StatusBadRequest)
		defer resp.Body.Close()
		assertErrorCode(t, resp, "invalid_request")
	}

	t.Run("missing", func(t *testing.T) {
		postBad(basePayload)
	})
	t.Run("empty_after_trim", func(t *testing.T) {
		p := map[string]any{}
		for k, v := range basePayload {
			p[k] = v
		}
		p["response_proposals"] = []any{"", "  "}
		postBad(p)
	})
	t.Run("too_many", func(t *testing.T) {
		p := map[string]any{}
		for k, v := range basePayload {
			p[k] = v
		}
		p["response_proposals"] = []any{"a", "b", "c", "d", "e", "f", "g"}
		postBad(p)
	})
	t.Run("too_long", func(t *testing.T) {
		p := map[string]any{}
		for k, v := range basePayload {
			p[k] = v
		}
		p["response_proposals"] = []any{strings.Repeat("x", 241)}
		postBad(p)
	})
	t.Run("non_string", func(t *testing.T) {
		p := map[string]any{}
		for k, v := range basePayload {
			p[k] = v
		}
		p["response_proposals"] = []any{"ok", 99}
		postBad(p)
	})
}

func asString(value any) string {
	if value == nil {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return fmt.Sprint(value)
}

func TestAskCanonicalSubjectSurvivesIDHandleCollision(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	h := newPrimitivesTestServerWithHumanPrincipal(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()
	store := h.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	first, err := store.CreateWork(ctx, "actor-1", "", map[string]any{"title": "Original subject", "phase": "ready"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateWork(ctx, "actor-1", "", map[string]any{"title": "Colliding handle", "phase": "ready"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.workspace.DB().Exec(`UPDATE cards SET handle=anx_normalize_handle(?) WHERE id=?`, first["id"], second["id"]); err != nil {
		t.Fatal(err)
	}
	card, err := store.GetBoardCard(ctx, "", asString(first["id"]))
	if err != nil {
		t.Fatal(err)
	}
	ask := createHumanAttentionEvent(t, h.baseURL, asString(card["thread_id"]), "ask", "Resolve original subject", asString(card["ref"]), nil, nil)
	if ask["payload"].(map[string]any)["subject_ref"] != card["ref"] {
		t.Fatalf("wrong canonical subject: %#v", ask)
	}
	var mapped string
	if err = h.workspace.DB().QueryRow(`SELECT card_id FROM ask_subjects WHERE ask_id=?`, ask["id"]).Scan(&mapped); err != nil || mapped != first["id"] {
		t.Fatalf("mapped to %s: %v", mapped, err)
	}
	item, ok := findInboxItem(getInboxItems(t, h.baseURL), func(item map[string]any) bool { return item["source_event_id"] == ask["id"] })
	if !ok {
		t.Fatal("ask missing")
	}
	postJSONExpectStatusWithHeaders(t, h.baseURL+"/inbox/"+url.PathEscape(asString(item["id"]))+"/respond", json.RawMessage(`{"actor_id":"human-purge-principal-actor","response_text":"Resolved","outcome":"resolved","notify_mode":"none"}`), map[string]string{"Authorization": "Bearer " + h.humanAccessToken}, http.StatusCreated).Body.Close()
	for _, entry := range []struct {
		id    any
		phase string
	}{{first["id"], "done"}, {second["id"], "ready"}} {
		var phase string
		if err = h.workspace.DB().QueryRow(`SELECT column_key FROM cards WHERE id=?`, entry.id).Scan(&phase); err != nil || phase != entry.phase {
			t.Fatalf("card %s phase %s: %v", entry.id, phase, err)
		}
	}
}

func TestAskLegacyClientSubjectsBecomeTasks(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Legacy client","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()
	thread := integrationSeedThread(t, h, "actor-1", paginationTestThread("legacy-ask-thread", "Legacy ask thread"))
	store := h.primitiveStore.(*primitives.Store)
	// No active board is readable by this legacy client. In particular, the
	// reserved default identity and its backing thread belong to another actor.
	if _, err := store.CreateBoard(context.Background(), "private-owner", map[string]any{"id": "workspace-default", "title": "Private default"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PatchThread(context.Background(), "private-owner", "workspace-default", map[string]any{"pm_actor_id": "private-owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if store.CanAccessResource(primitives.WithAccessScope(context.Background(), primitives.AccessScope{ActorID: "actor-1"}), "board", "workspace-default") {
		t.Fatal("legacy caller can read private default")
	}
	topic, err := store.CreateTopic(context.Background(), "actor-1", map[string]any{"title": "Legacy subject topic", "summary": "Legacy context"})
	if err != nil {
		t.Fatal(err)
	}
	doc, _, err := store.CreateDocument(context.Background(), "actor-1", map[string]any{"title": "Legacy subject document"}, "Decision context", "text", []string{})
	if err != nil {
		t.Fatal(err)
	}
	for _, subject := range []string{"thread:" + thread, asString(topic.Topic["ref"]), asString(doc["ref"])} {
		t.Run(subject, func(t *testing.T) {
			request := map[string]any{"actor_id": "actor-1", "request_key": "legacy-" + subject, "event": map[string]any{"type": "human_attention_requested", "thread_id": thread, "refs": []string{"thread:" + thread}, "summary": "Legacy decision", "payload": map[string]any{"kind": "ask", "title": "Legacy decision", "subject_ref": subject, "requester_actor_id": "actor-1", "response_proposals": []string{"Proceed"}}, "provenance": eventProvenance()}}
			var firstID, firstCard string
			before := countTableRows(t, h.workspace.DB(), "cards")
			for i := 0; i < 2; i++ {
				response := postJSONExpectStatus(t, h.baseURL+"/events", string(mustJSON(t, request)), http.StatusCreated)
				var body map[string]any
				if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				response.Body.Close()
				event := body["event"].(map[string]any)
				payload := event["payload"].(map[string]any)
				ref := asString(payload["subject_ref"])
				if !strings.HasPrefix(ref, "card:") || !strings.Contains(fmt.Sprint(payload["related_refs"]), subject) {
					t.Fatalf("lost subject: %#v", event)
				}
				work, err := store.GetWork(context.Background(), ref)
				if err != nil || work["phase"] != "ready" {
					t.Fatalf("invalid subject: %#v %v", work, err)
				}
				if i == 0 {
					firstID = asString(event["id"])
					firstCard = ref
				} else if firstID != asString(event["id"]) || firstCard != ref {
					t.Fatalf("replay created another ask: %#v", event)
				}
			}
			if got := countTableRows(t, h.workspace.DB(), "cards"); got != before+1 {
				t.Fatalf("cards %d want %d", got, before+1)
			}
		})
	}
}
