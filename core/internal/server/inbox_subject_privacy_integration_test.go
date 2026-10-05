package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

func TestArchivedInboxAsksEnforceSubjectCardAndBoardPrivacy(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()
	for _, role := range []string{"owner", "stranger", "agent"} {
		t.Run(role, func(t *testing.T) {
			t.Parallel()
			env := newAuthIntegrationEnv(t, authIntegrationOptions{})
			ctx := context.Background()
			db := env.workspace.DB()
			owner := seedHumanPrincipalForLockoutTest(t, ctx, db, "subject-owner", "subject-owner-actor", "subject-owner", "subject-owner-token")
			stranger := seedHumanPrincipalForLockoutTest(t, ctx, db, "subject-stranger", "subject-stranger-actor", "subject-stranger", "subject-stranger-token")
			agent := seedMachinePrincipalForLockoutTest(t, ctx, db, "subject-agent", "subject-agent-actor", "subject.agent", "subject-agent-token")
			token := map[string]string{"owner": owner.AccessToken, "stranger": stranger.AccessToken, "agent": agent.AccessToken}[role]
			store := env.primitiveStore.(*primitives.Store)
			items := make([]primitives.DerivedInboxItem, 0, 8)
			want := map[string]string{}
			titles := map[string]string{}
			for _, privacy := range []string{"board", "card", "public", "board-thread", "card-thread", "public-thread", "public-source", "board-legacy-thread"} {
				board, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": privacy + " board"})
				if err != nil {
					t.Fatal(err)
				}
				boardID := anyString(board["id"])
				cardInput := primitives.AddBoardCardInput{Title: privacy + " card"}
				if privacy == "public-source" {
					cardInput.ParentThreadID = seedStreamPrivacyThread(t, store, owner.ActorID, false)
				}
				card, err := store.CreateBoardCard(ctx, owner.ActorID, boardID, cardInput)
				if err != nil {
					t.Fatal(err)
				}
				cardThread := anyString(card.Card["thread_id"])
				privateThread := ""
				if strings.HasPrefix(privacy, "board") || privacy == "public-source" {
					privateThread = anyString(board["thread_id"])
				} else if strings.HasPrefix(privacy, "card") {
					privateThread = cardThread
				}
				if privateThread != "" {
					if _, err := store.PatchThread(ctx, owner.ActorID, privateThread, map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
						t.Fatal(err)
					}
				}
				// Reproduce a public card thread under a private board. Keep the
				// request's own thread public too, so its privacy alone cannot help.
				askThread := seedStreamPrivacyThread(t, store, owner.ActorID, false)
				ref := "card:" + anyString(card.Card["id"])
				if privacy == "public-source" {
					// A modern parent is provenance, not the card's backing thread.
					askThread = cardInput.ParentThreadID
					ref = "thread:" + askThread
				}
				if privacy == "board-legacy-thread" {
					if _, err := db.Exec(`UPDATE cards SET thread_id='',parent_thread_id=? WHERE id=?`, cardThread, card.Card["id"]); err != nil {
						t.Fatal(err)
					}
				}
				if strings.HasSuffix(privacy, "-thread") {
					askThread = cardThread
					ref = "thread:" + cardThread
				}
				event, err := store.AppendEvent(ctx, owner.ActorID, map[string]any{
					"ts":   time.Now().UTC().Format(time.RFC3339Nano),
					"type": "human_attention_requested", "thread_id": askThread,
					"summary": privacy + " ask", "refs": []string{"thread:" + askThread, ref},
					"payload": map[string]any{"response_proposals": []any{"Approve", "Decline"}, "kind": "ask", "title": privacy + " ask", "body": privacy + " confidential body", "subject_ref": ref, "related_refs": []string{ref}, "requester_actor_id": owner.ActorID},
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: store}, askThread, time.Now().UTC(), owner.ActorID); err != nil {
					t.Fatal(err)
				}
				projected, err := store.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: askThread})
				if err != nil {
					t.Fatal(err)
				}
				var ask primitives.DerivedInboxItem
				for _, item := range projected {
					if anyString(item.Data["source_event_id"]) == anyString(event["id"]) {
						ask = item
					}
				}
				if ask.ID == "" {
					t.Fatalf("request not projected: %#v", projected)
				}
				items = append(items, ask)
				titles[ask.ID] = anyString(ask.Data["title"])
				if _, err := store.ArchiveBoard(ctx, owner.ActorID, boardID); err != nil {
					t.Fatal(err)
				}
				if strings.HasPrefix(privacy, "public") || role == "owner" {
					want[ask.ID] = privacy + " confidential body"
				}
			}
			assertReads := func() {
				t.Helper()
				assertPrivacyInboxList(t, env.server.URL, token, want)
				for _, limit := range []string{"", "?limit=0"} {
					resp := getJSONExpectStatusWithAuth(t, env.server.URL+"/inbox/summary"+limit, token, http.StatusOK)
					var summary struct {
						Count int              `json:"open_ask_count"`
						Asks  []map[string]any `json:"asks"`
					}
					err := json.NewDecoder(resp.Body).Decode(&summary)
					resp.Body.Close()
					if err != nil {
						t.Fatal(err)
					}
					if summary.Count != len(want) || (limit == "" && len(summary.Asks) != min(len(want), 5)) || (limit != "" && len(summary.Asks) != 0) {
						t.Fatalf("summary leaked or lost archived asks: %#v; want %#v", summary, want)
					}
					for _, ask := range summary.Asks {
						if body, ok := want[anyString(ask["id"])]; !ok || body != anyString(ask["body"]) {
							t.Fatalf("summary exposed unauthorized ask: %#v", ask)
						}
					}
				}
				for _, ask := range items {
					status := http.StatusNotFound
					if _, ok := want[ask.ID]; ok {
						status = http.StatusOK
					}
					resp := getJSONExpectStatusWithAuth(t, env.server.URL+"/inbox/"+url.PathEscape(ask.ID), token, status)
					resp.Body.Close()
				}
			}
			assertReads()
			resp := openAuthenticatedPrivacyStream(t, env.server.URL+"/stream/inbox", token, "unknown-subject-cursor")
			reader, stop := startSSEReader(resp.Body)
			defer stop()
			assertPrivacyInboxEvents(t, reader, want, titles)
			for _, ask := range items {
				ask.Data["body"] = anyString(ask.Data["body"]) + " updated"
				seedStreamPrivacyInbox(t, store, ask.ThreadID, ask)
				if _, ok := want[ask.ID]; ok {
					want[ask.ID] = anyString(ask.Data["body"])
				}
			}
			// A public archived ask update proves the stream is actively polling;
			// neither the initial snapshot nor updates may expose private asks.
			assertPrivacyInboxEvents(t, reader, want, titles)
			assertReads()
		})
	}
}
