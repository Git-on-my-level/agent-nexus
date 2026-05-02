package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"agent-nexus-core/internal/primitives"
)

func TestNotificationsListReadAndDismissAreTargetScoped(t *testing.T) {
	t.Parallel()

	env := newAuthIntegrationEnv(t, authIntegrationOptions{
		bootstrapToken: testBootstrapToken,
	})

	sender := registerNotificationTestAgentWithBootstrap(t, env.server.URL, "sender.agent")
	targetInviteToken := createNotificationTestInvite(t, env.server.URL, sender.AccessToken)
	target := registerNotificationTestAgentWithInvite(t, env.server.URL, "target.agent", targetInviteToken)

	threadID := integrationSeedThreadWithStore(t, env.primitiveStore, nil, sender.ActorID, map[string]any{
		"title":            "Notification thread",
		"type":             "incident",
		"status":           "active",
		"priority":         "p2",
		"tags":             []any{"notifications"},
		"cadence":          "daily",
		"next_check_in_at": "2026-03-06T00:00:00Z",
		"current_summary":  "summary",
		"next_actions":     []any{"check"},
		"key_artifacts":    []any{},
		"provenance":       map[string]any{"sources": []any{"inferred"}},
	})

	sourceResp := postJSONExpectStatusWithAuth(t, env.server.URL+"/events", map[string]any{
		"event": map[string]any{
			"type":      "message_posted",
			"thread_id": threadID,
			"summary":   "@target.agent please check this",
			"refs":      []string{"thread:" + threadID},
			"payload": map[string]any{
				"text": "@target.agent please check this",
			},
			"provenance": map[string]any{"sources": []string{"inferred"}},
		},
	}, sender.AccessToken, http.StatusCreated)
	var sourcePayload struct {
		Event map[string]any `json:"event"`
	}
	if err := json.NewDecoder(sourceResp.Body).Decode(&sourcePayload); err != nil {
		t.Fatalf("decode source event response: %v", err)
	}
	sourceResp.Body.Close()
	triggerEventID := asString(sourcePayload.Event["id"])
	triggerCreatedAt := asString(sourcePayload.Event["ts"])
	if triggerEventID == "" {
		t.Fatal("expected trigger event id")
	}

	wakeupID := "wake-notification-1"
	if _, err := env.primitiveStore.UpsertAgentWakeup(context.Background(), primitives.AgentWakeup{
		WakeupID:         wakeupID,
		Status:           primitives.AgentWakeupStatusRequested,
		TargetHandle:     target.Username,
		TargetActorID:    target.ActorID,
		WorkspaceID:      "ws_main",
		WorkspaceName:    "Main",
		ThreadID:         threadID,
		TriggerEventID:   triggerEventID,
		TriggerCreatedAt: triggerCreatedAt,
		TriggerText:      "@target.agent please check this",
		Refs: []string{
			"thread:" + threadID,
			"event:" + triggerEventID,
			"artifact:" + wakeupID,
		},
	}); err != nil {
		t.Fatalf("seed wakeup: %v", err)
	}

	notificationsResp := getJSONExpectStatusWithAuth(t, env.server.URL+"/agent-notifications?status=unread", target.AccessToken, http.StatusOK)
	var notificationsPayload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(notificationsResp.Body).Decode(&notificationsPayload); err != nil {
		t.Fatalf("decode notifications response: %v", err)
	}
	notificationsResp.Body.Close()
	if len(notificationsPayload.Items) != 1 {
		t.Fatalf("expected one unread notification, got %#v", notificationsPayload.Items)
	}
	if asString(notificationsPayload.Items[0]["status"]) != notificationStatusUnread {
		t.Fatalf("expected unread notification, got %#v", notificationsPayload.Items[0])
	}

	timelineResp := getJSONExpectStatusWithAuth(t, env.server.URL+"/threads/"+threadID+"/timeline", sender.AccessToken, http.StatusOK)
	var timelinePayload struct {
		NotificationReceipts map[string][]map[string]any `json:"notification_receipts"`
	}
	if err := json.NewDecoder(timelineResp.Body).Decode(&timelinePayload); err != nil {
		t.Fatalf("decode timeline receipts response: %v", err)
	}
	timelineResp.Body.Close()
	receipts := timelinePayload.NotificationReceipts[triggerEventID]
	if len(receipts) != 1 {
		t.Fatalf("expected one notification receipt for trigger event, got %#v", timelinePayload.NotificationReceipts)
	}
	if got := asString(receipts[0]["delivery_status"]); got != primitives.AgentWakeupStatusRequested {
		t.Fatalf("expected requested delivery receipt, got %#v", receipts[0])
	}
	if got := asString(receipts[0]["notification_status"]); got != notificationStatusUnread {
		t.Fatalf("expected unread notification receipt, got %#v", receipts[0])
	}

	postJSONExpectStatusWithAuth(t, env.server.URL+"/agent-wakeups/claim", map[string]any{
		"wakeup_id":          wakeupID,
		"bridge_instance_id": "bridge-test-1",
	}, target.AccessToken, http.StatusOK).Body.Close()

	claimedTimelineResp := getJSONExpectStatusWithAuth(t, env.server.URL+"/threads/"+threadID+"/timeline", sender.AccessToken, http.StatusOK)
	if err := json.NewDecoder(claimedTimelineResp.Body).Decode(&timelinePayload); err != nil {
		t.Fatalf("decode claimed timeline receipts response: %v", err)
	}
	claimedTimelineResp.Body.Close()
	receipts = timelinePayload.NotificationReceipts[triggerEventID]
	if len(receipts) != 1 || asString(receipts[0]["delivery_status"]) != primitives.AgentWakeupStatusClaimed {
		t.Fatalf("expected claimed notification receipt, got %#v", receipts)
	}
	if asString(receipts[0]["claimed_at"]) == "" {
		t.Fatalf("expected claimed_at notification receipt timestamp, got %#v", receipts[0])
	}

	postJSONExpectStatusWithAuth(t, env.server.URL+"/events", map[string]any{
		"event": map[string]any{
			"type":      "agent_notification_read",
			"thread_id": threadID,
			"summary":   "forged read",
			"refs": []string{
				"thread:" + threadID,
				"artifact:" + wakeupID,
			},
			"payload": map[string]any{
				"wakeup_id":       wakeupID,
				"target_handle":   target.Username,
				"target_actor_id": target.ActorID,
			},
			"provenance": map[string]any{"sources": []string{"inferred"}},
		},
	}, sender.AccessToken, http.StatusCreated).Body.Close()

	forgedResp := getJSONExpectStatusWithAuth(t, env.server.URL+"/agent-notifications?status=unread", target.AccessToken, http.StatusOK)
	if err := json.NewDecoder(forgedResp.Body).Decode(&notificationsPayload); err != nil {
		t.Fatalf("decode forged notifications response: %v", err)
	}
	forgedResp.Body.Close()
	if len(notificationsPayload.Items) != 1 || asString(notificationsPayload.Items[0]["status"]) != notificationStatusUnread {
		t.Fatalf("expected forged read to be ignored, got %#v", notificationsPayload.Items)
	}

	notFoundResp := postJSONExpectStatusWithAuth(t, env.server.URL+"/agent-notifications/dismiss", map[string]any{
		"wakeup_id": wakeupID,
	}, sender.AccessToken, http.StatusNotFound)
	assertErrorCode(t, notFoundResp, "not_found")
	notFoundResp.Body.Close()

	readResp := postJSONExpectStatusWithAuth(t, env.server.URL+"/agent-notifications/read", map[string]any{
		"wakeup_id": wakeupID,
	}, target.AccessToken, http.StatusCreated)
	readResp.Body.Close()

	readListResp := getJSONExpectStatusWithAuth(t, env.server.URL+"/agent-notifications?status=read", target.AccessToken, http.StatusOK)
	if err := json.NewDecoder(readListResp.Body).Decode(&notificationsPayload); err != nil {
		t.Fatalf("decode read notifications response: %v", err)
	}
	readListResp.Body.Close()
	if len(notificationsPayload.Items) != 1 || asString(notificationsPayload.Items[0]["status"]) != notificationStatusRead {
		t.Fatalf("expected one read notification, got %#v", notificationsPayload.Items)
	}

	dismissResp := postJSONExpectStatusWithAuth(t, env.server.URL+"/agent-notifications/dismiss", map[string]any{
		"wakeup_id": wakeupID,
	}, target.AccessToken, http.StatusCreated)
	dismissResp.Body.Close()

	dismissedListResp := getJSONExpectStatusWithAuth(t, env.server.URL+"/agent-notifications?status=dismissed", target.AccessToken, http.StatusOK)
	if err := json.NewDecoder(dismissedListResp.Body).Decode(&notificationsPayload); err != nil {
		t.Fatalf("decode dismissed notifications response: %v", err)
	}
	dismissedListResp.Body.Close()
	if len(notificationsPayload.Items) != 1 || asString(notificationsPayload.Items[0]["status"]) != notificationStatusDismissed {
		t.Fatalf("expected one dismissed notification, got %#v", notificationsPayload.Items)
	}

	conflictResp := postJSONExpectStatusWithAuth(t, env.server.URL+"/agent-notifications/read", map[string]any{
		"wakeup_id": wakeupID,
	}, target.AccessToken, http.StatusConflict)
	assertErrorCode(t, conflictResp, "conflict")
	conflictResp.Body.Close()
}

func TestCardAssignmentEnqueuesAgentWakeupNotification(t *testing.T) {
	t.Parallel()

	env := newAuthIntegrationEnv(t, authIntegrationOptions{
		bootstrapToken:             testBootstrapToken,
		allowUnauthenticatedWrites: true,
	})

	assignor := registerNotificationTestAgentWithBootstrap(t, env.server.URL, "assignor.agent")
	targetInviteToken := createNotificationTestInvite(t, env.server.URL, assignor.AccessToken)
	assignee := registerNotificationTestAgentWithInvite(t, env.server.URL, "assignee.agent", targetInviteToken)

	threadID := integrationSeedThreadWithStore(t, env.primitiveStore, nil, assignor.ActorID, map[string]any{
		"title":            "Board thread",
		"type":             "incident",
		"status":           "active",
		"priority":         "p2",
		"tags":             []any{"board"},
		"cadence":          "daily",
		"next_check_in_at": "2026-03-06T00:00:00Z",
		"current_summary":  "summary",
		"next_actions":     []any{"check"},
		"key_artifacts":    []any{},
		"provenance":       map[string]any{"sources": []any{"inferred"}},
	})

	createBoardResp := postJSONExpectStatus(t, env.server.URL+"/boards", fmt.Sprintf(`{
		"actor_id":%q,
		"board":{
			"title":"Assignment board",
			"refs":["thread:%s"]
		}
	}`, assignor.ActorID, threadID), http.StatusCreated)
	defer createBoardResp.Body.Close()
	var boardPayload struct {
		Board map[string]any `json:"board"`
	}
	if err := json.NewDecoder(createBoardResp.Body).Decode(&boardPayload); err != nil {
		t.Fatalf("decode board: %v", err)
	}
	boardID := asString(boardPayload.Board["id"])
	boardUpdatedAt := asString(boardPayload.Board["updated_at"])

	addCardResp := postJSONExpectStatus(t, env.server.URL+"/boards/"+boardID+"/cards", fmt.Sprintf(`{
		"actor_id":%q,
		"if_board_updated_at":%q,
		"title":"Work item",
		"related_refs":["thread:%s"],
		"column_key":"ready"
	}`, assignor.ActorID, boardUpdatedAt, threadID), http.StatusCreated)
	defer addCardResp.Body.Close()
	var addPayload struct {
		Card map[string]any `json:"card"`
	}
	if err := json.NewDecoder(addCardResp.Body).Decode(&addPayload); err != nil {
		t.Fatalf("decode card create: %v", err)
	}
	cardID := asString(addPayload.Card["id"])
	cardUpdatedAt := asString(addPayload.Card["updated_at"])

	assigneeRef := fmt.Sprintf("actor:%s", assignee.ActorID)
	patchResp := patchJSONExpectStatus(t, env.server.URL+"/cards/"+cardID, fmt.Sprintf(`{
		"actor_id":%q,
		"if_updated_at":%q,
		"patch":{"assignee_refs":[%q]}
	}`, assignor.ActorID, cardUpdatedAt, assigneeRef), http.StatusOK)
	var patchPayload struct {
		Card map[string]any `json:"card"`
	}
	if err := json.NewDecoder(patchResp.Body).Decode(&patchPayload); err != nil {
		t.Fatalf("decode patch card: %v", err)
	}
	patchResp.Body.Close()
	cardThreadID := asString(patchPayload.Card["thread_id"])
	if cardThreadID == "" {
		t.Fatalf("expected card thread_id: %#v", patchPayload.Card)
	}

	notificationsResp := getJSONExpectStatusWithAuth(t, env.server.URL+"/agent-notifications?status=unread", assignee.AccessToken, http.StatusOK)
	var notificationsPayload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(notificationsResp.Body).Decode(&notificationsPayload); err != nil {
		t.Fatalf("decode notifications: %v", err)
	}
	notificationsResp.Body.Close()

	if len(notificationsPayload.Items) != 1 {
		t.Fatalf("expected one unread notification, got %#v", notificationsPayload.Items)
	}
	item := notificationsPayload.Items[0]
	if asString(item["status"]) != notificationStatusUnread {
		t.Fatalf("expected unread notification: %#v", item)
	}
	triggerEventID := asString(item["trigger_event_id"])
	if triggerEventID == "" {
		t.Fatalf("expected trigger_event_id: %#v", item)
	}

	timelineResp := getJSONExpectStatusWithAuth(t, env.server.URL+"/threads/"+cardThreadID+"/timeline", assignor.AccessToken, http.StatusOK)
	defer timelineResp.Body.Close()
	var timelinePayload struct {
		Events []map[string]any `json:"events"`
	}
	if err := json.NewDecoder(timelineResp.Body).Decode(&timelinePayload); err != nil {
		t.Fatalf("decode timeline: %v", err)
	}
	var matched bool
	for _, ev := range timelinePayload.Events {
		if asString(ev["id"]) != triggerEventID {
			continue
		}
		if asString(ev["type"]) != "card_updated" {
			t.Fatalf("expected card_updated for trigger id, got %#v", ev)
		}
		matched = true
		break
	}
	if !matched {
		t.Fatalf("timeline missing card_updated event id %s in %#v", triggerEventID, timelinePayload.Events)
	}
}

type notificationTestAgent struct {
	AccessToken string
	ActorID     string
	Username    string
}

func registerNotificationTestAgentWithBootstrap(t *testing.T, serverURL string, username string) notificationTestAgent {
	t.Helper()
	publicKey, _ := generateKeyPair(t)
	resp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/agents/register", map[string]any{
		"username":        username,
		"public_key":      publicKey,
		"bootstrap_token": testBootstrapToken,
	}, "", http.StatusCreated)
	defer resp.Body.Close()
	var payload struct {
		Agent struct {
			ActorID  string `json:"actor_id"`
			Username string `json:"username"`
		} `json:"agent"`
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	return notificationTestAgent{
		AccessToken: payload.Tokens.AccessToken,
		ActorID:     payload.Agent.ActorID,
		Username:    payload.Agent.Username,
	}
}

func registerNotificationTestAgentWithInvite(t *testing.T, serverURL string, username string, inviteToken string) notificationTestAgent {
	t.Helper()
	publicKey, _ := generateKeyPair(t)
	resp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/agents/register", map[string]any{
		"username":     username,
		"public_key":   publicKey,
		"invite_token": inviteToken,
	}, "", http.StatusCreated)
	defer resp.Body.Close()
	var payload struct {
		Agent struct {
			ActorID  string `json:"actor_id"`
			Username string `json:"username"`
		} `json:"agent"`
		Tokens struct {
			AccessToken string `json:"access_token"`
		} `json:"tokens"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	return notificationTestAgent{
		AccessToken: payload.Tokens.AccessToken,
		ActorID:     payload.Agent.ActorID,
		Username:    payload.Agent.Username,
	}
}

func createNotificationTestInvite(t *testing.T, serverURL string, accessToken string) string {
	t.Helper()
	resp := postJSONExpectStatusWithAuth(t, serverURL+"/auth/invites", map[string]any{
		"kind": "agent",
	}, accessToken, http.StatusCreated)
	defer resp.Body.Close()
	var payload struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode invite response: %v", err)
	}
	if payload.Token == "" {
		t.Fatal("expected invite token")
	}
	return payload.Token
}
