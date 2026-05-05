package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

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

func TestAgentWakeupRefsCorruptionFailsNotificationReads(t *testing.T) {
	t.Parallel()

	env := newAuthIntegrationEnv(t, authIntegrationOptions{
		bootstrapToken: testBootstrapToken,
	})

	sender := registerNotificationTestAgentWithBootstrap(t, env.server.URL, "corrupt.sender")
	targetInviteToken := createNotificationTestInvite(t, env.server.URL, sender.AccessToken)
	target := registerNotificationTestAgentWithInvite(t, env.server.URL, "corrupt.target", targetInviteToken)

	threadID := integrationSeedThreadWithStore(t, env.primitiveStore, nil, sender.ActorID, map[string]any{
		"title":            "Corrupt notification refs thread",
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
	triggerEventID := seedReceiptStreamMessageEvent(t, env.server.URL, sender.AccessToken, threadID, "@corrupt.target please check this")
	wakeupID := "wake-corrupt-notification-refs"
	seedReceiptStreamWakeup(t, env.primitiveStore, primitives.AgentWakeup{
		WakeupID:       wakeupID,
		Status:         primitives.AgentWakeupStatusRequested,
		TargetHandle:   target.Username,
		TargetActorID:  target.ActorID,
		WorkspaceID:    "ws_main",
		WorkspaceName:  "Main",
		ThreadID:       threadID,
		TriggerEventID: triggerEventID,
		TriggerText:    "@corrupt.target please check this",
		Refs:           []string{"thread:" + threadID, "event:" + triggerEventID, "artifact:" + wakeupID},
	})
	if _, err := env.workspace.DB().ExecContext(context.Background(), `UPDATE agent_wakeups SET refs_json = ? WHERE wakeup_id = ?`, `{not-json`, wakeupID); err != nil {
		t.Fatalf("corrupt wakeup refs_json: %v", err)
	}

	timelineResp := getJSONExpectStatusWithAuth(t, env.server.URL+"/threads/"+threadID+"/timeline", sender.AccessToken, http.StatusInternalServerError)
	assertErrorCode(t, timelineResp, "internal_error")
	timelineResp.Body.Close()

	notificationsResp := getJSONExpectStatusWithAuth(t, env.server.URL+"/agent-notifications?status=unread", target.AccessToken, http.StatusInternalServerError)
	assertErrorCode(t, notificationsResp, "internal_error")
	notificationsResp.Body.Close()
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

func TestAgentNotificationReceiptsStreamTracksWakeupStatus(t *testing.T) {
	t.Parallel()

	env := newAuthIntegrationEnv(t, authIntegrationOptions{
		bootstrapToken: testBootstrapToken,
	})

	sender := registerNotificationTestAgentWithBootstrap(t, env.server.URL, "receipt.sender")
	targetInviteToken := createNotificationTestInvite(t, env.server.URL, sender.AccessToken)
	target := registerNotificationTestAgentWithInvite(t, env.server.URL, "receipt.target", targetInviteToken)

	threadID := integrationSeedThreadWithStore(t, env.primitiveStore, nil, sender.ActorID, map[string]any{
		"title":            "Receipt stream thread",
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
	otherThreadID := integrationSeedThreadWithStore(t, env.primitiveStore, nil, sender.ActorID, map[string]any{
		"title":            "Other receipt stream thread",
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

	triggerEventID := seedReceiptStreamMessageEvent(t, env.server.URL, sender.AccessToken, threadID, "@receipt.target please check this")
	otherTriggerEventID := seedReceiptStreamMessageEvent(t, env.server.URL, sender.AccessToken, otherThreadID, "@receipt.target not this thread")

	wakeupID := "wake-receipt-stream-1"
	seedReceiptStreamWakeup(t, env.primitiveStore, primitives.AgentWakeup{
		WakeupID:       wakeupID,
		Status:         primitives.AgentWakeupStatusRequested,
		TargetHandle:   target.Username,
		TargetActorID:  target.ActorID,
		WorkspaceID:    "ws_main",
		WorkspaceName:  "Main",
		ThreadID:       threadID,
		TriggerEventID: triggerEventID,
		TriggerText:    "@receipt.target please check this",
		Refs:           []string{"thread:" + threadID, "event:" + triggerEventID, "artifact:" + wakeupID},
	})
	seedReceiptStreamWakeup(t, env.primitiveStore, primitives.AgentWakeup{
		WakeupID:       "wake-receipt-stream-other",
		Status:         primitives.AgentWakeupStatusRequested,
		TargetHandle:   target.Username,
		TargetActorID:  target.ActorID,
		WorkspaceID:    "ws_main",
		WorkspaceName:  "Main",
		ThreadID:       otherThreadID,
		TriggerEventID: otherTriggerEventID,
		TriggerText:    "@receipt.target not this thread",
		Refs:           []string{"thread:" + otherThreadID, "event:" + otherTriggerEventID, "artifact:wake-receipt-stream-other"},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, env.server.URL+"/stream/agent-notification-receipts?thread_id="+threadID, nil)
	if err != nil {
		t.Fatalf("build receipt stream request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+sender.AccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("open receipt stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("open receipt stream status=%d", resp.StatusCode)
	}
	reader := bufio.NewReader(resp.Body)

	initial := readNotificationReceiptSSE(t, reader)
	if initial.ID == "" || !strings.HasPrefix(initial.ID, "receipt:"+wakeupID+"@") {
		t.Fatalf("expected deterministic receipt event id, got %#v", initial)
	}
	if initial.Receipt["wakeup_id"] != wakeupID || initial.Receipt["trigger_event_id"] != triggerEventID {
		t.Fatalf("expected target-thread receipt, got %#v", initial.Receipt)
	}
	if got := asString(initial.Receipt["delivery_status"]); got != primitives.AgentWakeupStatusRequested {
		t.Fatalf("expected requested receipt, got %#v", initial.Receipt)
	}

	postJSONExpectStatusWithAuth(t, env.server.URL+"/agent-wakeups/claim", map[string]any{
		"wakeup_id":          wakeupID,
		"bridge_instance_id": "bridge-receipt-stream",
	}, target.AccessToken, http.StatusOK).Body.Close()
	claimed := readNotificationReceiptSSE(t, reader)
	if got := asString(claimed.Receipt["delivery_status"]); got != primitives.AgentWakeupStatusClaimed {
		t.Fatalf("expected claimed receipt, got %#v", claimed.Receipt)
	}
	if claimed.ID == initial.ID {
		t.Fatalf("expected digest event id to change after claim, got %q", claimed.ID)
	}
	if asString(claimed.Receipt["claimed_at"]) == "" {
		t.Fatalf("expected claimed_at receipt timestamp, got %#v", claimed.Receipt)
	}

	postJSONExpectStatusWithAuth(t, env.server.URL+"/agent-wakeups/complete", map[string]any{
		"wakeup_id":          wakeupID,
		"bridge_instance_id": "bridge-receipt-stream",
	}, target.AccessToken, http.StatusOK).Body.Close()
	completed := readNotificationReceiptSSE(t, reader)
	if got := asString(completed.Receipt["delivery_status"]); got != primitives.AgentWakeupStatusCompleted {
		t.Fatalf("expected completed receipt, got %#v", completed.Receipt)
	}

	failedWakeupID := "wake-receipt-stream-failed"
	seedReceiptStreamWakeup(t, env.primitiveStore, primitives.AgentWakeup{
		WakeupID:       failedWakeupID,
		Status:         primitives.AgentWakeupStatusRequested,
		TargetHandle:   target.Username,
		TargetActorID:  target.ActorID,
		WorkspaceID:    "ws_main",
		WorkspaceName:  "Main",
		ThreadID:       threadID,
		TriggerEventID: triggerEventID,
		TriggerText:    "@receipt.target please check this",
		Refs:           []string{"thread:" + threadID, "event:" + triggerEventID, "artifact:" + failedWakeupID},
	})
	added := readNotificationReceiptSSE(t, reader)
	if added.Receipt["wakeup_id"] != failedWakeupID {
		t.Fatalf("expected newly added failed wakeup receipt, got %#v", added.Receipt)
	}
	postJSONExpectStatusWithAuth(t, env.server.URL+"/agent-wakeups/claim", map[string]any{
		"wakeup_id":          failedWakeupID,
		"bridge_instance_id": "bridge-receipt-stream",
	}, target.AccessToken, http.StatusOK).Body.Close()
	readNotificationReceiptSSE(t, reader)
	postJSONExpectStatusWithAuth(t, env.server.URL+"/agent-wakeups/fail", map[string]any{
		"wakeup_id":          failedWakeupID,
		"bridge_instance_id": "bridge-receipt-stream",
		"error":              "bridge failed",
	}, target.AccessToken, http.StatusOK).Body.Close()
	failed := readNotificationReceiptSSE(t, reader)
	if got := asString(failed.Receipt["delivery_status"]); got != primitives.AgentWakeupStatusFailed {
		t.Fatalf("expected failed receipt, got %#v", failed.Receipt)
	}
	if got := asString(failed.Receipt["failure_reason"]); got != "bridge failed" {
		t.Fatalf("expected failure reason, got %#v", failed.Receipt)
	}
}

func TestNotificationReceiptRecordsAfterIDResumesAfterCursor(t *testing.T) {
	records := []notificationReceiptStreamRecord{
		{eventID: "receipt:wake-1@aaa", wakeupID: "wake-1", digest: "aaa"},
		{eventID: "receipt:wake-2@bbb", wakeupID: "wake-2", digest: "bbb"},
	}
	got := notificationReceiptRecordsAfterID(records, "receipt:wake-2@bbb")
	if len(got) != 0 {
		t.Fatalf("expected no replay when cursor is latest record, got %#v", got)
	}
	got = notificationReceiptRecordsAfterID(records, "receipt:wake-1@aaa")
	if len(got) != 1 || got[0].eventID != "receipt:wake-2@bbb" {
		t.Fatalf("expected records after first id, got %#v", got)
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

func seedReceiptStreamMessageEvent(t *testing.T, serverURL string, accessToken string, threadID string, text string) string {
	t.Helper()
	resp := postJSONExpectStatusWithAuth(t, serverURL+"/events", map[string]any{
		"event": map[string]any{
			"type":      "message_posted",
			"thread_id": threadID,
			"summary":   text,
			"refs":      []string{"thread:" + threadID},
			"payload": map[string]any{
				"text": text,
			},
			"provenance": map[string]any{"sources": []string{"inferred"}},
		},
	}, accessToken, http.StatusCreated)
	defer resp.Body.Close()
	var payload struct {
		Event map[string]any `json:"event"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode source event response: %v", err)
	}
	eventID := asString(payload.Event["id"])
	if eventID == "" {
		t.Fatal("expected seeded message event id")
	}
	return eventID
}

func seedReceiptStreamWakeup(t *testing.T, store PrimitiveStore, wakeup primitives.AgentWakeup) {
	t.Helper()
	if _, err := store.UpsertAgentWakeup(context.Background(), wakeup); err != nil {
		t.Fatalf("seed receipt stream wakeup: %v", err)
	}
}

type notificationReceiptSSE struct {
	ID      string
	Receipt map[string]any
}

func readNotificationReceiptSSE(t *testing.T, reader *bufio.Reader) notificationReceiptSSE {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		type result struct {
			record notificationReceiptSSE
			err    error
		}
		ch := make(chan result, 1)
		go func() {
			record, err := readNotificationReceiptSSEOnce(reader)
			ch <- result{record: record, err: err}
		}()
		select {
		case <-deadline:
			t.Fatal("timed out waiting for notification receipt SSE")
		case got := <-ch:
			if got.err != nil {
				t.Fatalf("read notification receipt SSE: %v", got.err)
			}
			if got.record.Receipt != nil {
				return got.record
			}
		}
	}
}

func readNotificationReceiptSSEOnce(reader *bufio.Reader) (notificationReceiptSSE, error) {
	var id string
	var eventName string
	var dataLines []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return notificationReceiptSSE{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "id":
			id = value
		case "event":
			eventName = value
		case "data":
			dataLines = append(dataLines, value)
		}
	}
	if eventName != "notification_receipt" || len(dataLines) == 0 {
		return notificationReceiptSSE{}, nil
	}
	var payload struct {
		Receipt map[string]any `json:"receipt"`
	}
	if err := json.Unmarshal([]byte(strings.Join(dataLines, "\n")), &payload); err != nil {
		return notificationReceiptSSE{}, err
	}
	return notificationReceiptSSE{ID: id, Receipt: payload.Receipt}, nil
}
