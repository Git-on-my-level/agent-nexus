package server

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

func TestInboxResponseRequiresCurrentResourceAccess(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()
	for _, privacy := range []string{"thread", "card", "board", "board-thread-subject"} {
		t.Run(privacy, func(t *testing.T) {
			t.Parallel()
			env := newAuthIntegrationEnv(t, authIntegrationOptions{})
			ctx := context.Background()
			db := env.workspace.DB()
			owner := seedHumanPrincipalForLockoutTest(t, ctx, db, "response-owner", "response-owner-actor", "response-owner", "response-owner-token")
			stranger := seedHumanPrincipalForLockoutTest(t, ctx, db, "response-stranger", "response-stranger-actor", "response-stranger", "response-stranger-token")
			agent := seedMachinePrincipalForLockoutTest(t, ctx, db, "response-agent", "response-agent-actor", "response.agent", "response-agent-token")
			store := env.primitiveStore.(*primitives.Store)
			ask, privateThread := seedInboxResponsePrivacyAsk(t, store, owner.ActorID, privacy)
			if _, err := store.PatchThread(ctx, owner.ActorID, privateThread, map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
				t.Fatal(err)
			}
			endpoint := env.server.URL + "/inbox/" + url.PathEscape(ask.ID)
			request := map[string]any{"idempotency_key": "private-response", "response_text": "Approved", "outcome": "approved", "notify_mode": "none"}
			for _, token := range []string{stranger.AccessToken, agent.AccessToken} {
				getJSONExpectStatusWithAuth(t, endpoint, token, http.StatusNotFound).Body.Close()
				wantStatus := http.StatusNotFound
				if token == agent.AccessToken {
					wantStatus = http.StatusForbidden
				}
				status, body := hostHTTP(t, http.MethodPost, endpoint+"/respond", token, request)
				if status != wantStatus {
					t.Fatalf("unauthorized response: %d %#v", status, body)
				}
			}
			var count int
			if err := db.QueryRow(`SELECT COUNT(*) FROM human_attention_response_claims`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("unauthorized response committed a claim: count=%d err=%v", count, err)
			}
			if err := db.QueryRow(`SELECT COUNT(*) FROM events WHERE type='human_attention_responded'`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("unauthorized response committed an event: count=%d err=%v", count, err)
			}
			// The legitimate owner can still answer the archived request.
			status, first := hostHTTP(t, http.MethodPost, endpoint+"/respond", owner.AccessToken, request)
			if status != http.StatusCreated {
				t.Fatalf("owner response: %d %#v", status, first)
			}
			firstEvent := first["event"].(map[string]any)
			// The answered item has left the open projection. A retry must use
			// current resource policy rather than trusting its persisted result.
			if _, err := store.GetDerivedInboxItem(ctx, ask.ID); err != primitives.ErrNotFound {
				t.Fatalf("answered item still projected: %v", err)
			}
			status, replay := hostHTTP(t, http.MethodPost, endpoint+"/respond", owner.AccessToken, request)
			if status != http.StatusCreated || anyString(replay["event"].(map[string]any)["id"]) != anyString(firstEvent["id"]) {
				t.Fatalf("authorized replay changed response: %d %#v", status, replay)
			}
			if _, err := store.PatchThread(ctx, owner.ActorID, privateThread, map[string]any{"pm_actor_id": stranger.ActorID}, nil); err != nil {
				t.Fatal(err)
			}
			status, denied := hostHTTP(t, http.MethodPost, endpoint+"/respond", owner.AccessToken, request)
			if status != http.StatusNotFound || denied["event"] != nil {
				t.Fatalf("replay disclosed response after access loss: %d %#v", status, denied)
			}
			conflictingRequest := map[string]any{"idempotency_key": "private-response", "response_text": "Changed", "outcome": "approved", "notify_mode": "none"}
			status, denied = hostHTTP(t, http.MethodPost, endpoint+"/respond", owner.AccessToken, conflictingRequest)
			if status != http.StatusNotFound {
				t.Fatalf("conflict bypassed authorization: %d %#v", status, denied)
			}
			if _, err := store.PatchThread(ctx, stranger.ActorID, privateThread, map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
				t.Fatal(err)
			}
			status, restored := hostHTTP(t, http.MethodPost, endpoint+"/respond", owner.AccessToken, request)
			if status != http.StatusCreated || anyString(restored["event"].(map[string]any)["id"]) != anyString(firstEvent["id"]) {
				t.Fatalf("restored access lost replay: %d %#v", status, restored)
			}
			if err := db.QueryRow(`SELECT COUNT(*) FROM human_attention_response_claims`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("replay claim count=%d err=%v", count, err)
			}
			if err := db.QueryRow(`SELECT COUNT(*) FROM events WHERE type='human_attention_responded'`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("replay event count=%d err=%v", count, err)
			}
		})
	}
}

func seedInboxResponsePrivacyAsk(t *testing.T, store *primitives.Store, owner, privacy string) (primitives.DerivedInboxItem, string) {
	t.Helper()
	ctx := context.Background()
	board, err := store.CreateBoard(ctx, owner, map[string]any{"title": "Response privacy board"})
	if err != nil {
		t.Fatal(err)
	}
	boardID := anyString(board["id"])
	card, err := store.CreateBoardCard(ctx, owner, boardID, primitives.AddBoardCardInput{Title: "Confidential card"})
	if err != nil {
		t.Fatal(err)
	}
	askThread := seedStreamPrivacyThread(t, store, owner, false)
	ref := "card:" + anyString(card.Card["id"])
	privateThread := anyString(board["thread_id"])
	switch privacy {
	case "thread":
		privateThread = askThread
	case "card":
		privateThread = anyString(card.Card["thread_id"])
	case "board-thread-subject":
		askThread = anyString(card.Card["thread_id"])
		ref = "thread:" + askThread
	}
	_, err = store.AppendEvent(ctx, owner, map[string]any{
		"type": "human_attention_requested", "thread_id": askThread,
		"summary": "Confidential request title", "refs": []string{"thread:" + askThread, ref},
		"payload": map[string]any{"kind": "ask", "title": "Confidential request title", "body": "Confidential body", "subject_ref": ref, "related_refs": []string{ref}, "requester_actor_id": owner, "response_proposals": []any{"Approve", "Decline"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: store}, askThread, time.Now().UTC(), owner); err != nil {
		t.Fatal(err)
	}
	items, err := store.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: askThread})
	if err != nil || len(items) != 1 {
		t.Fatalf("ask projection: %#v err=%v", items, err)
	}
	if _, err := store.ArchiveBoard(ctx, owner, boardID); err != nil {
		t.Fatal(err)
	}
	return items[0], privateThread
}

func TestInboxResponseAndReplayAuthorizeAddedRefsConsistently(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, db, "evidence-owner", "evidence-owner-actor", "evidence-owner", "evidence-owner-token")
	stranger := seedHumanPrincipalForLockoutTest(t, ctx, db, "evidence-stranger", "evidence-stranger-actor", "evidence-stranger", "evidence-stranger-token")
	store := env.primitiveStore.(*primitives.Store)
	ask, _ := seedInboxResponsePrivacyAsk(t, store, owner.ActorID, "board")
	privateRefThread := seedStreamPrivacyThread(t, store, stranger.ActorID, true)
	request := map[string]any{"idempotency_key": "evidence-response", "response_text": "Approved", "outcome": "approved", "notify_mode": "none", "related_refs": []string{"thread:" + privateRefThread}}
	endpoint := env.server.URL + "/inbox/" + url.PathEscape(ask.ID) + "/respond"
	status, body := hostHTTP(t, http.MethodPost, endpoint, owner.AccessToken, request)
	if status != http.StatusNotFound {
		t.Fatalf("fresh response accepted inaccessible evidence: %d %#v", status, body)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM human_attention_response_claims`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("inaccessible evidence committed claim: count=%d err=%v", count, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM events WHERE type='human_attention_responded'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("inaccessible evidence committed event: count=%d err=%v", count, err)
	}
	if _, err := store.PatchThread(ctx, stranger.ActorID, privateRefThread, map[string]any{"pm_actor_id": ""}, nil); err != nil {
		t.Fatal(err)
	}
	status, first := hostHTTP(t, http.MethodPost, endpoint, owner.AccessToken, request)
	if status != http.StatusCreated {
		t.Fatalf("fresh response rejected accessible evidence: %d %#v", status, first)
	}
	status, replay := hostHTTP(t, http.MethodPost, endpoint, owner.AccessToken, request)
	if status != http.StatusCreated || anyString(first["event"].(map[string]any)["id"]) != anyString(replay["event"].(map[string]any)["id"]) {
		t.Fatalf("unchanged authority broke replay: %d %#v", status, replay)
	}
	if _, err := store.PatchThread(ctx, stranger.ActorID, privateRefThread, map[string]any{"pm_actor_id": stranger.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	status, body = hostHTTP(t, http.MethodPost, endpoint, owner.AccessToken, request)
	if status != http.StatusNotFound || body["event"] != nil {
		t.Fatalf("replay disclosed newly inaccessible evidence: %d %#v", status, body)
	}
}
