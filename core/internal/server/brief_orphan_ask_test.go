package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

// An ask whose thread row is missing is materialized into brief_asks by the
// event trigger, but the inbox projection never writes a derived row for it.
// Decide must not offer that ask, and every decision it does offer must be
// present in the same reader's inbox and answerable through respond.
func TestBriefDecisionsMatchAnswerableInbox(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()

	h := newPrimitivesTestServerWithHumanPrincipal(t)
	ctx := context.Background()
	store := h.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{"title": "Portfolio", "role": "initiatives"})
	if err != nil {
		t.Fatal(err)
	}
	work := func(title string) map[string]any {
		t.Helper()
		card, err := store.CreateWork(ctx, "actor-1", anyString(board["id"]), map[string]any{"title": title})
		if err != nil {
			t.Fatal(err)
		}
		return card
	}
	subject := work("Battery")
	blocked := work("Report")
	ask := func(threadID, title string) {
		t.Helper()
		if _, err := store.AppendEvent(ctx, "agent-1", map[string]any{
			"type": "human_attention_requested", "thread_id": threadID, "summary": title,
			"refs": []string{"thread:" + threadID},
			"payload": map[string]any{
				"kind": "ask", "title": title, "requester_actor_id": "agent-1",
				"subject_ref": anyString(subject["ref"]), "response_proposals": []any{"Proceed", "Wait"},
				"by": "2026-06-01T00:00:00Z", "blocks": []any{anyString(blocked["ref"])},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}
	ask(anyString(subject["thread_id"]), "Merge calibration?")
	ask("ac17545a-6113-47bc-b33c-4b0df6b0df6968893", "Orphan ask")
	for drained := false; !drained; {
		if drained, err = store.RefreshBriefInitiatives(ctx, 200); err != nil {
			t.Fatal(err)
		}
	}
	if err = h.maintainer.Step(ctx, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	body := getAuthedJSON(t, h.baseURL+"/brief", h.humanAccessToken)
	decisions, _ := body["decisions"].([]any)
	if len(decisions) != 1 {
		t.Fatalf("decisions = %#v", decisions)
	}
	decision := decisions[0].(map[string]any)
	if decision["title"] != "Merge calibration?" {
		t.Fatalf("decision = %#v", decision)
	}
	inboxID := anyString(decision["inbox_item_id"])
	if inboxID == "" {
		t.Fatalf("decision missing inbox_item_id: %#v", decision)
	}

	initiatives, _ := body["initiatives"].([]any)
	var battery, report map[string]any
	for _, raw := range initiatives {
		item := raw.(map[string]any)
		switch item["title"] {
		case "Battery":
			battery = item
		case "Report":
			report = item
		}
	}
	if battery["waiting_on_you_count"] != float64(1) || battery["blocked_count"] != float64(1) {
		t.Fatalf("battery counts = %#v", battery)
	}
	if report["blocked_count"] != float64(1) || report["waiting_on_you_count"] != float64(0) {
		t.Fatalf("report counts = %#v", report)
	}

	inbox := getAuthedJSON(t, h.baseURL+"/inbox", h.humanAccessToken)
	items, _ := inbox["items"].([]any)
	seen := map[string]bool{}
	for _, raw := range items {
		seen[anyString(raw.(map[string]any)["id"])] = true
	}
	for _, raw := range decisions {
		id := anyString(raw.(map[string]any)["inbox_item_id"])
		if !seen[id] {
			t.Fatalf("decision %s is not in the inbox: %#v", id, items)
		}
	}

	var orphanID string
	if err = h.workspace.DB().QueryRow(`SELECT inbox_item_id FROM brief_asks WHERE title=?`, "Orphan ask").Scan(&orphanID); err != nil {
		t.Fatal(err)
	}
	if seen[orphanID] {
		t.Fatalf("orphan inbox id %s is in the inbox", orphanID)
	}
	postJSONExpectStatusWithHeaders(t, h.baseURL+"/inbox/"+url.PathEscape(orphanID)+"/respond", map[string]any{
		"actor_id": "human-purge-principal-actor", "response_text": "Proceed", "outcome": "answered", "notify_mode": "none",
	}, map[string]string{"Authorization": "Bearer " + h.humanAccessToken}, http.StatusNotFound).Body.Close()

	postJSONExpectStatusWithHeaders(t, h.baseURL+"/inbox/"+url.PathEscape(inboxID)+"/respond", map[string]any{
		"actor_id": "human-purge-principal-actor", "response_text": "Proceed", "outcome": "answered", "notify_mode": "none",
	}, map[string]string{"Authorization": "Bearer " + h.humanAccessToken}, http.StatusCreated).Body.Close()
}

func getAuthedJSON(t *testing.T, rawURL, token string) map[string]any {
	t.Helper()
	resp := getJSONExpectStatusWithAuth(t, rawURL, token, http.StatusOK)
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}
