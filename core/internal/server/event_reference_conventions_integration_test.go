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

	workOrderResp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"work_order_created",
			"thread_id":"thread-1",
			"refs":["thread:thread-1"],
			"summary":"work order created",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusBadRequest)
	assertEventErrorMessageContains(t, workOrderResp, "event.refs must include")

	receiptResp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"receipt_added",
			"thread_id":"thread-1",
			"refs":["artifact:receipt-1"],
			"summary":"receipt added",
			"payload":{},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusBadRequest)
	assertEventErrorMessageContains(t, receiptResp, "at least 2 refs with prefix \"artifact\"")

	commitmentStatusResp := postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-1",
		"event":{
			"type":"commitment_status_changed",
			"thread_id":"thread-1",
			"refs":["snapshot:commitment-1"],
			"summary":"status changed",
			"payload":{"to_status":"done"},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusBadRequest)
	assertEventErrorMessageContains(t, commitmentStatusResp, "payload.to_status=\"done\"")
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
