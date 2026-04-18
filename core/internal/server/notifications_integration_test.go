package server

import (
	"encoding/json"
	"net/http"
	"testing"
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
	postJSONExpectStatusWithAuth(t, env.server.URL+"/events", map[string]any{
		"event": map[string]any{
			"type":      agentWakeRequestEvent,
			"thread_id": threadID,
			"summary":   "Wake requested for @target.agent",
			"refs": []string{
				"thread:" + threadID,
				"event:" + triggerEventID,
				"artifact:" + wakeupID,
			},
			"payload": map[string]any{
				"wakeup_id":          wakeupID,
				"wake_artifact_id":   wakeupID,
				"target_handle":      target.Username,
				"target_actor_id":    target.ActorID,
				"workspace_id":       "ws_main",
				"workspace_name":     "Main",
				"thread_id":          threadID,
				"trigger_event_id":   triggerEventID,
				"trigger_created_at": triggerCreatedAt,
				"trigger_text":       "@target.agent please check this",
				"session_key":        "anx:ws_main:" + threadID + ":" + target.Username,
			},
			"provenance": map[string]any{"sources": []string{"actor_statement:" + triggerEventID}},
		},
	}, sender.AccessToken, http.StatusCreated).Body.Close()

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

	postJSONExpectStatusWithAuth(t, env.server.URL+"/events", map[string]any{
		"event": map[string]any{
			"type":      agentNotificationReadEvent,
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
