package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

func TestAddressedAgentHandoffLifecycle(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{workspaceID: "handoff-workspace"})
	ctx := context.Background()
	sender := seedNotificationTestAgent(t, env, "sender.origin")
	recipient := seedNotificationTestAgent(t, env, "recipient.destination")
	other := seedNotificationTestAgent(t, env, "other.other-host")
	queueURL := env.server.URL + agentHandoffPath
	github := "https://github.com/example/repo/issues/10"
	multica := "multica:issue:00000000-0000-4000-8000-000000000001"
	deadline := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	send := map[string]any{"recipient_actor_id": recipient.ActorID, "refs": []string{multica, github}, "expires_at": deadline}
	read := func(resp *http.Response) map[string]any {
		t.Helper()
		defer resp.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	post := func(url string, body map[string]any, token string, status int) map[string]any {
		t.Helper()
		return read(postJSONExpectStatusWithAuth(t, url, body, token, status))
	}
	assertStatus := func(body map[string]any, transport, pickup string) {
		t.Helper()
		if body["delivery_status"] != transport || body["pickup_status"] != pickup {
			t.Fatalf("want %s/%s, got %#v", transport, pickup, body)
		}
	}
	queued := post(queueURL, send, sender.AccessToken, http.StatusOK)
	assertStatus(queued, "queued", "pending")
	id := anyString(queued["handoff_id"])
	if !primitives.IsAgentHandoffID(id) {
		t.Fatalf("bad handoff ID %q", id)
	}
	send["refs"] = []string{"https://github.com/EXAMPLE/Repo/issues/10", multica, github}
	replay := post(queueURL, send, sender.AccessToken, http.StatusOK)
	if replay["handoff_id"] != id || replay["queued_at"] != queued["queued_at"] {
		t.Fatalf("semantic replay changed record: %#v", replay)
	}
	lifecycleURL := queueURL + "/" + id
	ackURL := lifecycleURL + "/ack"
	positive := map[string]any{"outcome": "refs_verified"}
	getJSONExpectStatusWithAuth(t, lifecycleURL, other.AccessToken, http.StatusNotFound).Body.Close()
	post(ackURL, positive, sender.AccessToken, http.StatusNotFound)
	post(ackURL, positive, other.AccessToken, http.StatusNotFound)
	post(ackURL, positive, recipient.AccessToken, http.StatusConflict)

	otherWorkspace := httptest.NewServer(NewHandler("0.2.2", WithAuthStore(env.authStore),
		WithPrimitiveStore(env.primitiveStore), WithWorkspaceID("other-workspace")))
	defer otherWorkspace.Close()
	getJSONExpectStatusWithAuth(t, otherWorkspace.URL+agentHandoffPath+"/"+id, recipient.AccessToken, http.StatusNotFound).Body.Close()
	if _, err := env.primitiveStore.ClaimAgentWakeup(ctx, id, other.ActorID, "unrelated"); !errors.Is(err, primitives.ErrConflict) {
		t.Fatalf("other agent claimed handoff: %v", err)
	}

	claim := map[string]any{"wakeup_id": id, "bridge_instance_id": "recipient-runtime"}
	postNotificationWakeup(t, env.server.URL, "claim", claim, recipient).Body.Close()
	if _, err := env.primitiveStore.ClaimAgentWakeup(ctx, id, recipient.ActorID, "replacement-runtime"); !errors.Is(err, primitives.ErrConflict) {
		t.Fatalf("replacement runtime reclaimed a potentially executed wake: %v", err)
	}
	postNotificationWakeup(t, env.server.URL, "claim", claim, recipient).Body.Close()
	postNotificationWakeup(t, env.server.URL, "complete", claim, recipient).Body.Close()
	postNotificationWakeup(t, env.server.URL, "complete", claim, recipient).Body.Close()
	assertStatus(read(getJSONExpectStatusWithAuth(t, lifecycleURL, sender.AccessToken, http.StatusOK)), "bridge_delivered", "pending")

	post(ackURL, map[string]any{"outcome": "ref_blocked", "blocked_ref": "event:unrelated"}, recipient.AccessToken, http.StatusBadRequest)
	blocked := map[string]any{"outcome": "ref_blocked", "blocked_ref": github}
	ack := post(ackURL, blocked, recipient.AccessToken, http.StatusOK)
	assertStatus(ack, "bridge_delivered", "acknowledged")
	if ack["outcome"] != "ref_blocked" || ack["blocked_ref"] != github {
		t.Fatalf("missing ref blocker: %#v", ack)
	}
	if replay := post(ackURL, blocked, recipient.AccessToken, http.StatusOK); replay["acknowledged_at"] != ack["acknowledged_at"] {
		t.Fatalf("ack replay changed receipt: %#v", replay)
	}
	post(ackURL, positive, recipient.AccessToken, http.StatusConflict)
	if replay := post(queueURL, send, sender.AccessToken, http.StatusOK); replay["acknowledged_at"] != ack["acknowledged_at"] {
		t.Fatalf("send replay lost receipt: %#v", replay)
	}

	send["refs"] = []string{"event:unknown"}
	post(queueURL, send, sender.AccessToken, http.StatusUnprocessableEntity)
	send["refs"] = []string{"http://localhost/private"}
	post(queueURL, send, sender.AccessToken, http.StatusBadRequest)
	send["refs"] = []string{multica}
	send["recipient_actor_id"] = "unknown-actor"
	post(queueURL, send, sender.AccessToken, http.StatusNotFound)

	send["recipient_actor_id"] = recipient.ActorID
	queued = post(queueURL, send, sender.AccessToken, http.StatusOK)
	id = anyString(queued["handoff_id"])
	if _, err := env.workspace.DB().ExecContext(ctx, `UPDATE agent_wakeups SET handoff_expires_at=? WHERE wakeup_id=?`,
		deadline.Add(-2*time.Hour).Format(time.RFC3339), id); err != nil {
		t.Fatal(err)
	}
	assertStatus(read(getJSONExpectStatusWithAuth(t, queueURL+"/"+id, sender.AccessToken, http.StatusOK)), "expired", "expired")
	if _, err := env.primitiveStore.ClaimAgentWakeup(ctx, id, recipient.ActorID, "late-runtime"); !errors.Is(err, primitives.ErrConflict) {
		t.Fatalf("expired handoff could be claimed: %v", err)
	}
	post(queueURL+"/"+id+"/ack", positive, recipient.AccessToken, http.StatusConflict)
}

func seedHandoffFixture(t *testing.T, store *primitives.Store, sender, recipient string, refs []string) string {
	t.Helper()
	const workspaceID = "ws_main"
	const deadline = "2030-01-01T00:00:00Z"
	id := primitives.AgentHandoffID(workspaceID, sender, recipient, deadline, refs)
	if _, err := store.CreateAgentHandoff(context.Background(), primitives.AgentHandoff{
		ID: id, SenderActorID: sender, RecipientActorID: recipient,
		WorkspaceID: workspaceID, Refs: refs, ExpiresAt: deadline,
	}, "receiver.test"); err != nil {
		t.Fatal(err)
	}
	return id
}

func seedPerformanceWakeups(t *testing.T, store *primitives.Store, owner, agent lockoutPrincipalSeed, threadID, cardID string) string {
	t.Helper()
	seedReceiptStreamWakeup(t, store, primitives.AgentWakeup{
		WakeupID: "scale-target-wakeup", ThreadID: threadID, TargetActorID: owner.ActorID, TargetHandle: owner.Username,
		Status: primitives.AgentWakeupStatusRequested, TriggerText: "Scale target", Refs: []string{"card:" + cardID},
	})
	return seedHandoffFixture(t, store, agent.ActorID, "scale-handoff-recipient", []string{"https://github.com/example/repo/issues/10"})
}
