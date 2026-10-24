package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

// Exercise the poller's list-then-point-read pattern as an authenticated human.
func TestInboxItemReadLifecycle(t *testing.T) {
	requireIntegrationTest(t)
	for _, scenario := range []string{"active", "archived", "moved", "folded", "withdrawn", "superseded", "answered", "hidden", "rebuilding", "missing-projection", "missing-item"} {
		t.Run(scenario, func(t *testing.T) {
			env := newAuthIntegrationEnv(t, authIntegrationOptions{})
			ctx := context.Background()
			s := env.primitiveStore.(*primitives.Store)
			owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "read-owner", "read-owner-actor", "read-owner", "read-owner-token")
			stranger := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "read-stranger", "read-stranger-actor", "read-stranger", "read-stranger-token")
			work, err := s.CreateWork(ctx, owner.ActorID, "", map[string]any{"title": "Decision subject", "handle": "decision-subject"})
			if err != nil {
				t.Fatal(err)
			}
			id, thread := anyString(work["id"]), anyString(work["thread_id"])
			event, err := s.AppendEvent(ctx, owner.ActorID, map[string]any{
				"type": "human_attention_requested", "thread_id": thread, "refs": []string{"card:" + id},
				"payload": map[string]any{"subject_ref": "card:" + id, "requester_actor_id": owner.ActorID, "kind": "ask", "title": "Proceed?", "response_proposals": []string{"Proceed"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err = refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: s}, thread, time.Now().UTC(), owner.ActorID); err != nil {
				t.Fatal(err)
			}
			items, err := s.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: thread})
			if err != nil || len(items) != 1 {
				t.Fatalf("projection: %#v %v", items, err)
			}
			item := items[0]
			// A denied root forces the private-by-default authorization path even for
			// the owner reading a visible subject.
			seedStreamPrivacyThread(t, s, stranger.ActorID, true)
			wantInbox, wantAsk, outcome := http.StatusOK, http.StatusOK, "open"
			token := owner.AccessToken
			switch scenario {
			case "archived":
				_, err = s.ArchiveBoardCard(ctx, owner.ActorID, "", id, primitives.RemoveBoardCardInput{})
			case "moved":
				_, err = s.PatchWork(ctx, owner.ActorID, id, 1, map[string]any{"workspace_move": map[string]any{"move_id": "local-move", "source_ref": "card:" + id, "destination_workspace_id": "other"}})
				if err == nil {
					_, err = s.ArchiveBoardCard(ctx, owner.ActorID, "", id, primitives.RemoveBoardCardInput{})
				}
			case "folded":
				var parent map[string]any
				parent, err = s.CreateWork(ctx, owner.ActorID, "", map[string]any{"title": "Parent", "handle": "parent"})
				if err == nil {
					_, err = s.FoldWork(ctx, owner.ActorID, id, "card:"+anyString(parent["id"]), "")
				}
			case "withdrawn", "superseded":
				_, err = s.AppendHumanAttentionWithdrawal(ctx, owner.ActorID, anyString(event["id"]), map[string]any{"type": "human_attention_withdrawn", "payload": map[string]any{"reason": scenario}})
				wantInbox, outcome = http.StatusNotFound, "withdrawn"
			case "answered":
				var status int
				status, _ = hostHTTP(t, http.MethodPost, env.server.URL+"/inbox/"+url.PathEscape(item.ID)+"/respond", token, map[string]any{"response_text": "Proceed", "outcome": "answered", "notify_mode": "none"})
				if status != http.StatusCreated {
					t.Fatalf("answer status %d", status)
				}
				wantInbox, outcome = http.StatusNotFound, "answered"
			case "hidden":
				_, err = s.PatchThread(ctx, owner.ActorID, thread, map[string]any{"pm_actor_id": owner.ActorID}, nil)
				token, wantInbox, wantAsk = stranger.AccessToken, http.StatusNotFound, http.StatusNotFound
			case "rebuilding":
				_, err = s.MarkTopicProjectionRefreshStarted(ctx, thread, time.Now().UTC())
			case "missing-item":
				_, err = env.workspace.DB().Exec(`DELETE FROM derived_inbox_items WHERE id=?`, item.ID)
				wantInbox = http.StatusNotFound
			case "missing-projection":
				_, err = env.workspace.DB().Exec(`DELETE FROM derived_topic_views WHERE thread_id=?`, thread)
			}
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "hidden" {
				for _, path := range []string{"/inbox/" + url.PathEscape(item.ID), "/asks/" + anyString(event["id"])} {
					status, body := hostHTTP(t, http.MethodGet, env.server.URL+path, owner.AccessToken, nil)
					if status != http.StatusOK {
						t.Fatalf("owner read %s: %d %#v", path, status, body)
					}
				}
			}
			for _, read := range []struct {
				path   string
				status int
			}{
				{"/inbox/" + url.PathEscape(item.ID), wantInbox},
				{"/asks/" + url.PathEscape(anyString(event["id"])), wantAsk},
			} {
				status, body := hostHTTP(t, http.MethodGet, env.server.URL+read.path, token, nil)
				if status != read.status {
					t.Errorf("%s: got %d want %d: %#v", read.path, status, read.status, body)
				}
				if status == http.StatusOK && strings.HasPrefix(read.path, "/inbox/") {
					freshness := body["projection_freshness"].(map[string]any)
					if scenario == "rebuilding" && (freshness["status"] != "pending" || freshness["in_progress"] != true) {
						t.Errorf("rebuilding freshness: %#v", freshness)
					}
					if scenario == "missing-projection" && freshness["materialized"] != false {
						t.Errorf("missing freshness: %#v", freshness)
					}
				}
				if read.status == http.StatusOK && strings.HasPrefix(read.path, "/asks/") && body["status"] != outcome {
					t.Errorf("outcome %#v want %s", body, outcome)
				}
			}
			if scenario == "archived" || scenario == "moved" || scenario == "folded" {
				// The generic lifecycle worker closes asks on retired subjects. A stale
				// derived row must not make its cached ID readable as an open request.
				if err = s.MaintainAskLifecycleBatch(ctx); err != nil {
					t.Fatal(err)
				}
				status, body := hostHTTP(t, http.MethodGet, env.server.URL+"/inbox/"+url.PathEscape(item.ID), token, nil)
				if status != http.StatusNotFound {
					t.Errorf("closed subject inbox: %d %#v", status, body)
				}
				status, body = hostHTTP(t, http.MethodGet, env.server.URL+"/asks/"+anyString(event["id"]), token, nil)
				if status != http.StatusOK || body["status"] != "withdrawn" {
					t.Errorf("closed subject ask: %d %#v", status, body)
				}
			}
		})
	}
}
