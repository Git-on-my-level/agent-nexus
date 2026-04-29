package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestEventReferenceConventionsRejectMissingRequiredRefs(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	cardCreatedMissingRefsResp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"card_created",
			"thread_id":"thread-1",
			"refs":["card:card-1","board:board-1"],
			"summary":"card created",
			"payload":{"subject_ref":"card:card-1"},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusBadRequest)
	assertEventErrorMessageContains(t, cardCreatedMissingRefsResp, "event.refs must include")

	cardMovedMissingRefsResp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"card_moved",
			"thread_id":"thread-1",
			"refs":["card:card-1"],
			"summary":"card moved",
			"payload":{"subject_ref":"card:card-1"},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusBadRequest)
	assertEventErrorMessageContains(t, cardMovedMissingRefsResp, "event.refs must include")

	humanAttentionRequestedResp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"human_attention_requested",
			"thread_id":"thread-1",
			"refs":[],
			"summary":"status changed",
			"payload":{"kind":"ask","title":"Need a decision","subject_ref":"card:card-1","requester_actor_id":"actor-1","response_proposals":["Approve"]},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusBadRequest)
	assertEventErrorMessageContains(t, humanAttentionRequestedResp, "event.refs must include")

	humanAttentionRequestedTopicOnlyResp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"human_attention_requested",
			"thread_id":"thread-1",
			"refs":["topic:thread-1"],
			"summary":"status changed",
			"payload":{"kind":"ask","title":"Need a decision","subject_ref":"card:card-1","requester_actor_id":"actor-1","response_proposals":["Approve"]},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusBadRequest)
	assertEventErrorMessageContains(t, humanAttentionRequestedTopicOnlyResp, "thread:<id>")

	humanAttentionRespondedTopicOnlyResp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"human_attention_responded",
			"thread_id":"thread-1",
			"refs":["topic:topic-1"],
			"summary":"responded",
			"payload":{"inbox_item_id":"inbox-1","kind":"ask","response_text":"Approved.","responding_actor_id":"actor-1"},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusBadRequest)
	assertEventErrorMessageContains(t, humanAttentionRespondedTopicOnlyResp, "inbox:<id>")

	humanAttentionRespondedThreadOnlyResp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"human_attention_responded",
			"thread_id":"thread-1",
			"refs":["thread:thread-1"],
			"summary":"responded",
			"payload":{"inbox_item_id":"inbox-1","kind":"ask","response_text":"Approved.","responding_actor_id":"actor-1"},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusBadRequest)
	assertEventErrorMessageContains(t, humanAttentionRespondedThreadOnlyResp, "inbox:<id>")
}

func TestEventReferenceConventionsRejectMissingRequiredPayloadFields(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	missingSubtypeResp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"exception_raised",
			"thread_id":"thread-1",
			"refs":["thread:thread-1"],
			"summary":"thread became stale",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusBadRequest)
	assertEventErrorMessageContains(t, missingSubtypeResp, "event.payload.subtype is required")

	withSubtypeResp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"exception_raised",
			"thread_id":"thread-1",
			"refs":["thread:thread-1"],
			"summary":"thread became stale",
			"payload":{"subtype":"stale_topic"},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer withSubtypeResp.Body.Close()
}

func TestEventReferenceConventionsAllowUnknownEventType(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"Actor One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)

	createResp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"my_unknown_event_type",
			"thread_id":"thread-1",
			"refs":["customprefix:abc"],
			"summary":"unknown event",
			"payload":{"x":1},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated)
	defer createResp.Body.Close()

	var created struct {
		Event map[string]any `json:"event"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatalf("decode create event response: %v", err)
	}

	eventID, _ := created.Event["id"].(string)
	if eventID == "" {
		t.Fatal("expected event id")
	}

	getResp, err := http.Get(h.baseURL + "/events/" + eventID)
	if err != nil {
		t.Fatalf("GET /events/{id}: %v", err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected get status: got %d", getResp.StatusCode)
	}

	var loaded struct {
		Event map[string]any `json:"event"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&loaded); err != nil {
		t.Fatalf("decode get event response: %v", err)
	}
	if loaded.Event["type"] != "my_unknown_event_type" {
		t.Fatalf("unexpected event type: %#v", loaded.Event["type"])
	}
}

func assertEventErrorMessageContains(t *testing.T, resp *http.Response, want string) {
	t.Helper()
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}

	var payload map[string]map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode response body: %v body=%s", err, string(body))
	}

	message, _ := payload["error"]["message"].(string)
	if !strings.Contains(message, want) {
		t.Fatalf("expected error message to contain %q, got %q", want, message)
	}
}
