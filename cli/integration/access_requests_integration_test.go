//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestAccessRequestsAndFileAsksReachHumanInbox(t *testing.T) {
	h := newLiveCoreHarness(t)
	h.enrollHost(t, "requester")
	h.humanTokens["operator"] = h.adminToken
	request := h.runCLIExpectOK(t, "requester", nil, "auth", "access-requests", "request", "--grant", "auth-admin", "--reason", "Enroll a test host")
	id := mustStringPath(t, request.Payload, "result.request.id")
	retry := h.runCLIExpectOK(t, "requester", nil, "auth", "access-requests", "request", "--grant", "auth-admin", "--reason", "Retry")
	if mustStringPath(t, retry.Payload, "result.request.id") != id {
		t.Fatal("CLI retry duplicated the request")
	}
	h.runCLIExpectOK(t, "operator", nil, "auth", "access-requests", "approve", id)
	h.runCLIExpectOK(t, "requester", nil, "auth", "admins", "list")
	board := h.runCLIExpectOK(t, "requester", map[string]any{"board": map[string]any{"title": "Inbox grounding", "document_refs": []any{}, "pinned_refs": []any{}, "provenance": map[string]any{"sources": []any{"inferred"}}}}, "boards", "create")
	work := h.runCLIExpectOK(t, "requester", map[string]any{"board_ref": mustStringPath(t, board.Payload, "result.board.ref"), "title": "Ask context"}, "work", "create", "--from-file", "-")
	cardRef := mustStringPath(t, work.Payload, "result.work.ref")
	h.runCLIExpectOK(t, "requester", nil, "work", "start", cardRef)
	askIDs := []string{}
	inline := h.runCLIExpectOK(t, "requester", nil, "ask", "An inline ask", "--recommend", "Proceed")
	askIDs = append(askIDs, mustStringPath(t, inline.Payload, "result.ask_id"))
	for i, subject := range []string{"", cardRef} {
		path := filepath.Join(t.TempDir(), "ask.md")
		frontmatter := fmt.Sprintf("---\ntitle: File ask %d\nrecommended_response: Proceed\n", i)
		if subject != "" {
			frontmatter += "subject_ref: " + subject + "\n"
		}
		if err := os.WriteFile(path, []byte(frontmatter+"---\nA decision is needed.\n"), 0600); err != nil {
			t.Fatal(err)
		}
		ask := h.runCLIExpectOK(t, "requester", nil, "ask", "--from-file", path)
		askIDs = append(askIDs, mustStringPath(t, ask.Payload, "result.ask_id"))
	}
	for i, subject := range []string{"", cardRef} {
		path := filepath.Join(t.TempDir(), "ask.txt")
		if err := os.WriteFile(path, []byte("A plain-file decision is needed.\n"), 0600); err != nil {
			t.Fatal(err)
		}
		args := []string{"ask", fmt.Sprintf("Plain file ask %d", i), "--body-file", path, "--recommend", "Proceed"}
		if subject != "" {
			args = append(args, "--subject-ref", subject)
		}
		ask := h.runCLIExpectOK(t, "requester", nil, args...)
		askIDs = append(askIDs, mustStringPath(t, ask.Payload, "result.ask_id"))
	}
	verify := func() {
		t.Helper()
		inbox := h.runCLIExpectOK(t, "operator", nil, "debug", "inbox", "list")
		raw, _ := getPathValue(inbox.Payload, "result.items")
		items := raw.([]any)
		for _, id := range askIDs {
			found := false
			for _, raw := range items {
				if raw.(map[string]any)["request_event_ref"] == id {
					found = true
				}
			}
			if !found {
				t.Fatalf("agent ask %s missing from human Inbox: %s", id, inbox.Stdout)
			}
		}
		req, err := http.NewRequest("GET", h.baseURL+"/inbox/summary?limit=50", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+h.adminToken)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var summary map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 200 || summary["open_ask_count"] != float64(len(askIDs)) {
			t.Fatalf("wrong summary status=%d body=%#v", resp.StatusCode, summary)
		}
		for _, id := range askIDs {
			found := false
			for _, raw := range summary["asks"].([]any) {
				if raw.(map[string]any)["request_event_ref"] == id {
					found = true
				}
			}
			if !found {
				t.Fatalf("summary omitted %s: %#v", id, summary)
			}
		}
	}
	verify()
	h.runCLIExpectOK(t, "requester", nil, "cards", "archive", cardRef, "--reason", "Context archived")
	verify()
}
