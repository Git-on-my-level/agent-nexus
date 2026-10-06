package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"agent-nexus-core/internal/primitives"
)

func TestInboxSummaryCountsVisibleOpenAsksBeforeLimit(t *testing.T) {
	t.Parallel()
	h := newPrimitivesTestServerWithHumanPrincipal(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-1","display_name":"One","created_at":"2026-03-04T10:00:00Z"}}`, http.StatusCreated)
	thread := integrationSeedThread(t, h, "actor-1", map[string]any{"title": "Public asks"})
	for _, title := range []string{"First", "Second", "Third"} {
		createHumanAttentionEvent(t, h.baseURL, thread, "ask", title, "topic:launch", nil, nil)
	}
	createHumanAttentionEvent(t, h.baseURL, thread, "review", "Review is not an ask", "topic:launch", nil, nil)
	private := integrationSeedThread(t, h, "actor-1", map[string]any{"title": "Private"})
	createHumanAttentionEvent(t, h.baseURL, private, "ask", "Private ask", "topic:launch", nil, nil)
	store := h.primitiveStore.(*primitives.Store)
	if _, err := store.PatchThread(context.Background(), "actor-1", private, map[string]any{"pm_actor_id": "another-human"}, nil); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		query      string
		count, top int
	}{{"", 3, 3}, {"?limit=1", 3, 1}, {"?limit=0", 3, 0}} {
		resp := getJSONExpectStatusWithAuth(t, h.baseURL+"/inbox/summary"+tc.query, h.humanAccessToken, http.StatusOK)
		var body struct {
			Count int              `json:"open_ask_count"`
			Asks  []map[string]any `json:"asks"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if body.Count != tc.count || len(body.Asks) != tc.top {
			t.Fatalf("%s: %+v", tc.query, body)
		}
		if len(body.Asks) > 0 && body.Asks[0]["title"] != "First" {
			t.Fatal("oldest ask should be first", body)
		}
	}
	for _, query := range []string{"?limit=-1", "?limit=51", "?limit=bad", "?limit=", "?limit=1&limit=2"} {
		resp := getJSONExpectStatusWithAuth(t, h.baseURL+"/inbox/summary"+query, h.humanAccessToken, http.StatusBadRequest)
		resp.Body.Close()
	}
}
