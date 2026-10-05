package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"agent-nexus-core/internal/primitives"
)

func TestAgentInboxProjectionPaginatesPastInterveningEventsAndTracksAnswersIndividually(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	target := seedNotificationTestAgent(t, env, "inbox.target")
	other := seedNotificationTestAgent(t, env, "inbox.other")
	ctx := context.Background()
	store := env.primitiveStore.(*primitives.Store)

	olderAsk := appendAnswerWakeTestAsk(t, ctx, store, target.ActorID, "older-paged")
	for i := 0; i < 101; i++ {
		appendAnswerWakeTestAsk(t, ctx, store, "other-requester", fmt.Sprintf("intervening-%03d", i))
	}
	olderAnswer := appendAnswerWakeTestResponse(t, ctx, store, target.ActorID, target.Username, olderAsk)
	siblingAsk := appendAnswerWakeTestAsk(t, ctx, store, target.ActorID, "sibling-answered")
	siblingAnswer := appendAnswerWakeTestResponse(t, ctx, store, target.ActorID, target.Username, siblingAsk)
	firstNewerAsk := appendAnswerWakeTestAsk(t, ctx, store, target.ActorID, "newer-one")
	secondNewerAsk := appendAnswerWakeTestAsk(t, ctx, store, target.ActorID, "newer-two")

	pageOneResp := getJSONExpectStatusWithAuth(t, env.server.URL+"/agent-inbox/asks?limit=2", target.AccessToken, http.StatusOK)
	var pageOne struct {
		Items []map[string]any `json:"items"`
		Info  struct {
			NextCursor string `json:"next_cursor"`
			HasMore    bool   `json:"has_more"`
		} `json:"page_info"`
	}
	if err := json.NewDecoder(pageOneResp.Body).Decode(&pageOne); err != nil {
		t.Fatalf("decode inbox first page: %v", err)
	}
	pageOneResp.Body.Close()
	if len(pageOne.Items) != 2 || !pageOne.Info.HasMore || pageOne.Info.NextCursor == "" {
		t.Fatalf("expected a continuation page for newer asks: %#v", pageOne)
	}
	if pageOne.Items[0]["ask_id"] != "event:"+fmt.Sprint(secondNewerAsk["id"]) || pageOne.Items[1]["ask_id"] != "event:"+fmt.Sprint(firstNewerAsk["id"]) {
		t.Fatalf("first page ordering/scoping was wrong: %#v", pageOne.Items)
	}

	pageTwoURL := env.server.URL + "/agent-inbox/asks?limit=2&cursor=" + url.QueryEscape(pageOne.Info.NextCursor)
	pageTwoResp := getJSONExpectStatusWithAuth(t, pageTwoURL, target.AccessToken, http.StatusOK)
	var pageTwo struct {
		Items []map[string]any `json:"items"`
		Info  struct {
			NextCursor string `json:"next_cursor"`
			HasMore    bool   `json:"has_more"`
		} `json:"page_info"`
	}
	if err := json.NewDecoder(pageTwoResp.Body).Decode(&pageTwo); err != nil {
		t.Fatalf("decode inbox second page: %v", err)
	}
	pageTwoResp.Body.Close()
	if len(pageTwo.Items) != 2 || pageTwo.Info.HasMore || pageTwo.Items[1]["ask_id"] != "event:"+fmt.Sprint(olderAsk["id"]) {
		t.Fatalf("older answered ask disappeared behind unrelated events: %#v", pageTwo)
	}
	answerStates := map[string]bool{}
	for _, item := range pageTwo.Items {
		answer := item["answer"].(map[string]any)
		if item["answer_unread"] != true {
			t.Fatalf("new answer must be individually unread before a wake exists: %#v", item)
		}
		answerStates[fmt.Sprint(answer["response_event_id"])] = true
	}
	if !answerStates[fmt.Sprint(olderAnswer["id"])] || !answerStates[fmt.Sprint(siblingAnswer["id"])] {
		t.Fatalf("both answer events should be independently unread: %#v", answerStates)
	}
	if wakeups, err := store.ListAgentWakeups(ctx, primitives.AgentWakeupListFilter{TargetActorID: target.ActorID}); err != nil || len(wakeups) != 0 {
		t.Fatalf("fixture should exercise answer read state before wake delivery: wakeups=%#v err=%v", wakeups, err)
	}

	readResp := postJSONExpectStatusWithAuth(t, env.server.URL+"/agent-inbox/answers/read", map[string]any{
		"answer_event_id": olderAnswer["id"],
	}, target.AccessToken, http.StatusCreated)
	readResp.Body.Close()
	otherReadResp := postJSONExpectStatusWithAuth(t, env.server.URL+"/agent-inbox/answers/read", map[string]any{
		"answer_event_id": olderAnswer["id"],
	}, other.AccessToken, http.StatusNotFound)
	otherReadResp.Body.Close()

	pageTwoResp = getJSONExpectStatusWithAuth(t, pageTwoURL, target.AccessToken, http.StatusOK)
	if err := json.NewDecoder(pageTwoResp.Body).Decode(&pageTwo); err != nil {
		t.Fatalf("decode inbox page after answer read: %v", err)
	}
	pageTwoResp.Body.Close()
	if len(pageTwo.Items) != 2 || pageTwo.Items[0]["answer_unread"] != true || pageTwo.Items[1]["answer_unread"] != false {
		t.Fatalf("reading one response changed another response's state: %#v", pageTwo.Items)
	}
}
