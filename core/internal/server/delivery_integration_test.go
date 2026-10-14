package server

import (
	"context"
	"testing"

	"agent-nexus-core/internal/delivery"
)

func TestDeliveryChannelAdapterClaimReceiptAndPrivacy(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	seedSeriesIdentities(t, env)
	ctx := context.Background()
	seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "other-human", "other-human-actor", "other-human", "other-token")
	base := env.server.URL

	declaration := map[string]any{"name": "mailer", "description": "Email channel", "agent_id": "series-owner", "expected_interval": "1m", "series": []any{}, "delivers": []string{"email"}}
	status, out := hostHTTP(t, "POST", base+"/adapters", "admin-token", declaration)
	hostStatus(t, status, 200, out)
	status, out = hostHTTP(t, "POST", base+"/adapters", "admin-token", map[string]any{"name": "bad", "description": "x", "agent_id": "series-owner", "expected_interval": "1m", "series": []any{}, "delivers": []string{"bridge"}})
	hostStatus(t, status, 400, out)

	// Settings are human-only, own-principal, and limited to declared channels.
	status, out = hostHTTP(t, "PUT", base+"/notification-settings", "owner-token", map[string]any{"channels": []any{}})
	hostStatus(t, status, 403, out)
	status, out = hostHTTP(t, "PUT", base+"/notification-settings", "admin-token", map[string]any{"channels": []map[string]any{{"channel": "sms", "address": "1", "push_enabled": true}}})
	hostStatus(t, status, 400, out)
	status, out = hostHTTP(t, "PUT", base+"/notification-settings", "admin-token", map[string]any{"channels": []map[string]any{{"channel": "email", "address": "admin@example.com", "push_enabled": true, "digest_enabled": true}}})
	hostStatus(t, status, 200, out)
	if channels := out["channels"].([]any); len(channels) != 1 || out["available_channels"].([]any)[0] != "email" {
		t.Fatalf("settings not stored: %#v", out)
	}
	status, out = hostHTTP(t, "GET", base+"/notification-settings", "other-token", nil)
	hostStatus(t, status, 200, out)
	if channels := out["channels"].([]any); len(channels) != 0 {
		t.Fatalf("another human's settings leaked: %#v", out)
	}

	store := delivery.Store{DB: env.workspace.DB()}
	queued, created, err := store.Enqueue(ctx, delivery.Request{IdempotencyKey: "push:test", PrincipalID: "series-human", Channel: "email", Kind: delivery.KindPush, Payload: map[string]any{"title": "Merge?"}})
	if err != nil || !created {
		t.Fatalf("enqueue: %v %v", created, err)
	}

	// Only the owning agent of an adapter delivering the channel may claim.
	status, out = hostHTTP(t, "POST", base+"/deliveries/claim", "admin-token", map[string]any{"channel": "email"})
	hostStatus(t, status, 403, out)
	status, out = hostHTTP(t, "POST", base+"/deliveries/claim", "owner-token", map[string]any{"channel": "telegram"})
	hostStatus(t, status, 403, out)
	status, out = hostHTTP(t, "POST", base+"/deliveries/claim", "owner-token", map[string]any{"channel": "email", "limit": 5, "lease_seconds": 60})
	hostStatus(t, status, 200, out)
	items := out["deliveries"].([]any)
	if len(items) != 1 {
		t.Fatalf("claim: %#v", out)
	}
	claimed := items[0].(map[string]any)
	if claimed["delivery_id"] != queued.DeliveryID || claimed["address"] != "admin@example.com" || claimed["claim_token"] == "" {
		t.Fatalf("claimed delivery: %#v", claimed)
	}
	status, out = hostHTTP(t, "POST", base+"/deliveries/"+queued.DeliveryID+"/receipt", "owner-token", map[string]any{"claim_token": "stale", "outcome": "delivered"})
	hostStatus(t, status, 409, out)
	status, out = hostHTTP(t, "POST", base+"/deliveries/"+queued.DeliveryID+"/receipt", "other-token", map[string]any{"claim_token": claimed["claim_token"], "outcome": "delivered"})
	hostStatus(t, status, 404, out)
	status, out = hostHTTP(t, "POST", base+"/deliveries/"+queued.DeliveryID+"/receipt", "owner-token", map[string]any{"claim_token": claimed["claim_token"], "outcome": "delivered"})
	hostStatus(t, status, 200, out)
	if out["delivery"].(map[string]any)["status"] != "delivered" {
		t.Fatalf("receipt: %#v", out)
	}

	// Each principal reads only its own deliveries; claim tokens never leak.
	status, out = hostHTTP(t, "GET", base+"/deliveries", "admin-token", nil)
	hostStatus(t, status, 200, out)
	list := out["deliveries"].([]any)
	if len(list) != 1 {
		t.Fatalf("own deliveries: %#v", out)
	}
	entry := list[0].(map[string]any)
	if _, ok := entry["delivery"].(map[string]any)["claim_token"]; ok {
		t.Fatalf("claim token leaked: %#v", entry)
	}
	if receipts := entry["receipts"].([]any); len(receipts) != 1 || receipts[0].(map[string]any)["outcome"] != delivery.OutcomeDelivered {
		t.Fatalf("receipts: %#v", entry)
	}
	status, out = hostHTTP(t, "GET", base+"/deliveries", "other-token", nil)
	hostStatus(t, status, 200, out)
	if list := out["deliveries"].([]any); len(list) != 0 {
		t.Fatalf("another principal's deliveries leaked: %#v", out)
	}
}
