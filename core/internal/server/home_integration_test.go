package server

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestHomeUnreadGroupsEligibleEventsAndMarksRead(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-home","display_name":"Home Actor","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()

	createTopicResp := postJSONExpectStatus(t, h.baseURL+"/topics", `{
		"actor_id":"actor-home",
		"topic":{
			"title":"Home topic",
			"summary":"Unread coordination",
			"priority":"P0",
			"owner_refs":["actor:actor-home"],
			"document_refs":[],
			"board_refs":[],
			"related_refs":[],
			"provenance":{"sources":["event:home-topic"]}
		}
	}`, http.StatusCreated)
	defer createTopicResp.Body.Close()
	var createdTopic struct {
		Topic map[string]any `json:"topic"`
	}
	if err := json.NewDecoder(createTopicResp.Body).Decode(&createdTopic); err != nil {
		t.Fatalf("decode topic: %v", err)
	}
	topicID := asString(createdTopic.Topic["id"])
	threadID := asString(createdTopic.Topic["thread_id"])
	if topicID == "" || threadID == "" {
		t.Fatalf("expected topic and thread ids, got %#v", createdTopic.Topic)
	}

	postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-home",
		"event":{
			"type":"message_posted",
			"thread_id":"`+threadID+`",
			"refs":["thread:`+threadID+`","topic:`+topicID+`"],
			"summary":"Important message",
			"payload":{"body":"Read this"},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated).Body.Close()

	postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-home",
		"event":{
			"type":"agent_bridge_checked_in",
			"thread_id":"`+threadID+`",
			"refs":["thread:`+threadID+`","topic:`+topicID+`"],
			"summary":"Heartbeat",
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated).Body.Close()

	unreadResp, err := http.Get(h.baseURL + "/home/unread?reader_id=reader-home")
	if err != nil {
		t.Fatalf("GET /home/unread: %v", err)
	}
	defer unreadResp.Body.Close()
	if unreadResp.StatusCode != http.StatusOK {
		var payload map[string]any
		_ = json.NewDecoder(unreadResp.Body).Decode(&payload)
		t.Fatalf("unexpected home unread status: %d %#v", unreadResp.StatusCode, payload)
	}
	var unread struct {
		Groups      []map[string]any `json:"groups"`
		UnreadCount int              `json:"unread_count"`
		TopicCount  int              `json:"topic_count"`
	}
	if err := json.NewDecoder(unreadResp.Body).Decode(&unread); err != nil {
		t.Fatalf("decode unread: %v", err)
	}
	if unread.UnreadCount != 1 || unread.TopicCount != 1 || len(unread.Groups) != 1 {
		t.Fatalf("expected one unread Home event, got %#v", unread)
	}
	newest, _ := unread.Groups[0]["newest_event"].(map[string]any)

	postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-home",
		"event":{
			"type":"message_posted",
			"thread_id":"`+threadID+`",
			"refs":["thread:`+threadID+`","topic:`+topicID+`"],
			"summary":"Arrived after render",
			"payload":{"body":"Keep this unread"},
			"provenance":{"sources":["inferred"]}
		}
	}`, http.StatusCreated).Body.Close()

	postJSONExpectStatus(t, h.baseURL+"/home/read", `{
		"reader_id":"reader-home",
		"topic_id":"`+topicID+`",
		"expected_newest_event_cursor":{"ts":"`+asString(newest["ts"])+`","id":"`+asString(newest["id"])+`"}
	}`, http.StatusOK).Body.Close()

	emptyResp, err := http.Get(h.baseURL + "/home/unread?reader_id=reader-home")
	if err != nil {
		t.Fatalf("GET /home/unread after read: %v", err)
	}
	defer emptyResp.Body.Close()
	var afterRace struct {
		UnreadCount int `json:"unread_count"`
		TopicCount  int `json:"topic_count"`
	}
	if err := json.NewDecoder(emptyResp.Body).Decode(&afterRace); err != nil {
		t.Fatalf("decode empty unread: %v", err)
	}
	if afterRace.UnreadCount != 1 || afterRace.TopicCount != 1 {
		t.Fatalf("expected post-render event to remain unread after cursor mark read, got %#v", afterRace)
	}

	postJSONExpectStatus(t, h.baseURL+"/home/read", `{"reader_id":"reader-home","topic_id":"`+topicID+`"}`, http.StatusOK).Body.Close()
}

func TestEventsHomeFeedPresetMatchesHomeEligibility(t *testing.T) {
	t.Parallel()

	h := newPrimitivesTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-events-preset","display_name":"Events Actor","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated).Body.Close()
	postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-events-preset",
		"event":{"type":"message_posted","thread_id":"t1","refs":["thread:t1"],"summary":"kept","provenance":{"sources":["inferred"]}}
	}`, http.StatusCreated).Body.Close()
	postJSONExpectStatus(t, h.baseURL+"/events", `{
		"actor_id":"actor-events-preset",
		"event":{"type":"agent_wakeup_failed","refs":["thread:t1"],"summary":"visible only in Events","provenance":{"sources":["inferred"]}}
	}`, http.StatusCreated).Body.Close()

	presetResp, err := http.Get(h.baseURL + "/events?preset=home_feed")
	if err != nil {
		t.Fatalf("GET /events preset: %v", err)
	}
	defer presetResp.Body.Close()
	var preset struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(presetResp.Body).Decode(&preset); err != nil {
		t.Fatalf("decode preset: %v", err)
	}
	if len(preset.Events) != 1 || asString(preset.Events[0]["type"]) != "message_posted" {
		t.Fatalf("expected only home-feed event, got %#v", preset.Events)
	}

	allResp, err := http.Get(h.baseURL + "/events")
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer allResp.Body.Close()
	var all struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(allResp.Body).Decode(&all); err != nil {
		t.Fatalf("decode all: %v", err)
	}
	if len(all.Events) < 2 {
		t.Fatalf("expected unknown event to remain visible on Events, got %#v", all.Events)
	}
}
