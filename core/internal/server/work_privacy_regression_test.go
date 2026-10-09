package server

import (
	"agent-nexus-core/internal/primitives"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestWorkEvidencePrivacyAcrossReadPaths(t *testing.T) {
	t.Parallel()
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "evidence-owner", "evidence-owner-actor", "evidence-owner", "evidence-owner-token")
	stranger := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "evidence-reader", "evidence-reader-actor", "evidence-reader", "evidence-reader-token")
	store := env.primitiveStore.(*primitives.Store)
	public, err := store.CreateWork(ctx, owner.ActorID, "", map[string]any{"title": "Visible work"})
	if err != nil {
		t.Fatal(err)
	}
	for _, scope := range []string{"card", "board"} {
		t.Run(scope, func(t *testing.T) {
			board, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Board " + scope})
			if err != nil {
				t.Fatal(err)
			}
			card, err := store.CreateWork(ctx, owner.ActorID, anyString(board["id"]), map[string]any{"title": "Secret " + scope, "source_refs": []any{map[string]any{"authority": "tracker", "connection_id": "confidential-connection-" + scope, "native_id": "private-identity-" + scope, "identifier_aliases": []string{"SECRET-" + scope}, "title": "Confidential evidence " + scope, "url": "https://source.test/private"}}})
			if err != nil {
				t.Fatal(err)
			}
			thread := anyString(card["thread_id"])
			if scope == "board" {
				thread = anyString(board["thread_id"])
			}
			if _, err = store.PatchThread(ctx, owner.ActorID, thread, map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
				t.Fatal(err)
			}
			topic, err := store.CreateTopic(ctx, owner.ActorID, map[string]any{"title": "Shared topic " + scope, "summary": "Privacy fixture", "board_refs": []string{anyString(board["ref"])}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = env.workspace.DB().Exec(`UPDATE cards SET parent_thread_id=? WHERE id=?`, topic.Topic["thread_id"], card["id"]); err != nil {
				t.Fatal(err)
			}
			for _, path := range []string{"/work", "/cards", "/topics/" + anyString(topic.Topic["ref"]) + "/workspace", "/topics/" + anyString(topic.Topic["ref"]) + "/timeline", "/threads/" + anyString(topic.Topic["thread_id"]) + "/workspace", "/boards/" + anyString(board["ref"]) + "/workspace", "/boards/" + anyString(board["ref"]) + "/cards"} {
				status := http.StatusOK
				if scope == "board" && (strings.HasPrefix(path, "/boards/") || strings.HasPrefix(path, "/topics/") || strings.HasPrefix(path, "/threads/")) {
					status = http.StatusNotFound
				}
				resp := getJSONExpectStatusWithAuth(t, env.server.URL+path, stranger.AccessToken, status)
				data, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				for _, secret := range []string{anyString(card["ref"]), "Secret " + scope, "confidential-connection-" + scope, "private-identity-" + scope, "Confidential evidence " + scope} {
					if strings.Contains(string(data), secret) {
						t.Fatalf("%s leaked %s: %s", path, secret, data)
					}
				}
			}
			for _, path := range []string{"/work/" + anyString(card["ref"]), "/work/" + anyString(card["ref"]) + "/observations", "/work/" + anyString(card["ref"]) + "/refresh", "/cards/" + anyString(card["ref"]), "/cards/" + anyString(card["ref"]) + "/plan", "/cards/" + anyString(card["ref"]) + "/timeline", "/cards/" + anyString(card["ref"]) + "/revisions"} {
				resp := getJSONExpectStatusWithAuth(t, env.server.URL+path, stranger.AccessToken, http.StatusNotFound)
				resp.Body.Close()
			}
			resp := getJSONExpectStatusWithAuth(t, env.server.URL+"/work/"+anyString(card["ref"]), owner.AccessToken, http.StatusOK)
			defer resp.Body.Close()
			var body map[string]any
			if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if len(body["work"].(map[string]any)["source_refs"].([]any)) != 1 {
				t.Fatal(body)
			}
		})
	}
	resp := getJSONExpectStatusWithAuth(t, env.server.URL+"/work?limit=1", stranger.AccessToken, http.StatusOK)
	defer resp.Body.Close()
	var page struct {
		Work       []map[string]any `json:"work"`
		NextCursor string           `json:"next_cursor"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	if len(page.Work) != 1 || page.Work[0]["ref"] != public["ref"] || page.NextCursor != "" {
		t.Fatalf("hidden cards influenced the visible page or cursor: %+v", page)
	}
}

func TestPrivateChildrenExcludedFromBoardSummariesAndArchivedRefs(t *testing.T) {
	t.Parallel()
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "hidden-owner", "hidden-owner-actor", "hidden-owner", "hidden-owner-token")
	reader := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "hidden-reader", "hidden-reader-actor", "hidden-reader", "hidden-reader-token")
	store := env.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Shared board"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateWork(ctx, owner.ActorID, anyString(board["id"]), map[string]any{"title": "Hidden child"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, owner.ActorID, anyString(card["thread_id"]), map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = env.workspace.DB().Exec(`UPDATE cards SET column_key='blocked',due_at='2020-01-01T00:00:00Z' WHERE id=?`, card["id"]); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/boards", "/boards/" + anyString(board["ref"])} {
		resp := getJSONExpectStatusWithAuth(t, env.server.URL+path, reader.AccessToken, 200)
		var body map[string]any
		if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		summary, _ := body["summary"].(map[string]any)
		if path == "/boards" {
			entry := body["boards"].([]any)[0].(map[string]any)
			summary = entry["summary"].(map[string]any)
		}
		if summary["card_count"] != float64(0) || summary["overdue_card_count"] != float64(0) {
			t.Fatalf("private state leaked via %s: %v", path, summary)
		}
	}
	if _, err = env.workspace.DB().Exec(`UPDATE cards SET trashed_at=? WHERE id=?`, "2026-10-01T00:00:00Z", card["id"]); err != nil {
		t.Fatal(err)
	}
	resp := getJSONExpectStatusWithAuth(t, env.server.URL+"/work", reader.AccessToken, 200)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(raw), anyString(card["ref"])) {
		t.Fatalf("private archived ref leaked %s", raw)
	}
	resp = getJSONExpectStatusWithAuth(t, env.server.URL+"/cards?state=trashed", owner.AccessToken, 200)
	raw, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(raw), anyString(card["ref"])) {
		t.Fatalf("authorized trashed card omitted %s", raw)
	}
}
