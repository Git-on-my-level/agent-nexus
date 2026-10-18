package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/delivery"
)

// A trusted issuer's managed-agent grant may declare one principal-addressed
// channel; that agent claims deliveries for every human with the human's
// external identity, while a grant without the claim gets nothing.
func TestGrantDeclaredPrincipalChannel(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()

	const workspaceID = "ws_grant_channel"
	fixture := newWorkspaceHumanGrantTestFixture(t, workspaceID, "anx-core")
	env := newAuthIntegrationEnv(t, authIntegrationOptions{
		workspaceID:                   workspaceID,
		workspaceHumanGrantVerifier:   fixture.verifier,
		workspaceManagedGrantVerifier: fixture.managedVerifier,
	})
	base := env.server.URL

	exchange := func(grantType, assertion string, want int) map[string]any {
		t.Helper()
		resp := postJSONExpectStatusWithAuth(t, base+"/auth/token", map[string]any{"grant_type": grantType, "assertion": assertion}, "", want)
		defer resp.Body.Close()
		out := map[string]any{}
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return out
	}
	accessToken := func(out map[string]any) string {
		return out["tokens"].(map[string]any)["access_token"].(string)
	}
	declaration := &delivery.GrantDeclaration{Channel: "hosted", Addressing: delivery.AddressingPrincipal, Kinds: []string{delivery.KindPush}, Label: "Hosted notifications"}
	notifierGrant := func(jti string, d *delivery.GrantDeclaration) string {
		return fixture.signManagedAgentGrantAssertion(t, workspaceManagedGrantTokenOptions{
			Subject: "notifier", JTI: jti, OrganizationID: "org_1", SlotID: "notifier",
			Provider: "notifications", ProviderConnectionID: "notifier", OwnerAccountID: "acct_owner", Delivers: d,
		})
	}

	// Only a trusted grant may declare a principal-addressed channel; a host
	// adapter declaration cannot reach people who did not opt in.
	seedSeriesIdentities(t, env)
	status, out := hostHTTP(t, "POST", base+"/adapters", "admin-token", map[string]any{"name": "sneaky", "description": "x", "agent_id": "series-owner", "expected_interval": "1m", "series": []any{}, "delivers": []string{"x"}, "addressing": "principal"})
	hostStatus(t, status, 400, out)

	notifier := exchange(auth.TokenGrantTypeWorkspaceManagedAgent, notifierGrant("jti-notifier-1", declaration), http.StatusOK)
	notifierToken := accessToken(notifier)
	plain := exchange(auth.TokenGrantTypeWorkspaceManagedAgent, fixture.signManagedAgentGrantAssertion(t, workspaceManagedGrantTokenOptions{
		Subject: "researcher", JTI: "jti-plain-1", OrganizationID: "org_1", SlotID: "slot_researcher", OwnerAccountID: "acct_1",
	}), http.StatusOK)
	human := exchange(auth.TokenGrantTypeWorkspaceHuman, fixture.signGrantAssertion(t, workspaceHumanGrantTokenOptions{
		Subject: "acct_1", JTI: "jti-human-1", Email: "person@example.com",
	}), http.StatusOK)
	humanToken := accessToken(human)
	humanActor := human["agent"].(map[string]any)["actor_id"].(string)

	// The person sees the channel, on by default, with no address to enter.
	status, out = hostHTTP(t, "GET", base+"/notification-settings", humanToken, nil)
	hostStatus(t, status, 200, out)
	options := out["channel_options"].(map[string]any)["hosted"].(map[string]any)
	if options["addressing"] != "principal" || options["label"] != "Hosted notifications" {
		t.Fatalf("channel options %#v", out)
	}
	// Saving an address on a principal channel stores only the choice.
	status, out = hostHTTP(t, "PUT", base+"/notification-settings", humanToken, map[string]any{"channels": []map[string]any{{"channel": "hosted", "address": "ignored@example.com", "push_enabled": true, "digest_enabled": true}}})
	hostStatus(t, status, 200, out)
	if row := out["channels"].([]any)[0].(map[string]any); row["address"] != "" {
		t.Fatalf("principal channel kept an address: %#v", row)
	}

	store := delivery.Store{DB: env.workspace.DB()}
	if _, _, err := store.Enqueue(context.Background(), delivery.Request{IdempotencyKey: "push:grant", PrincipalID: humanActor, Channel: "hosted", Kind: delivery.KindPush, Payload: map[string]any{"title": "Merge?"}}); err != nil {
		t.Fatal(err)
	}

	// A managed agent without the claim cannot claim, even on the same channel.
	status, out = hostHTTP(t, "POST", base+"/deliveries/claim", accessToken(plain), map[string]any{"channel": "hosted"})
	hostStatus(t, status, 403, out)
	// People cannot claim.
	status, out = hostHTTP(t, "POST", base+"/deliveries/claim", humanToken, map[string]any{"channel": "hosted"})
	hostStatus(t, status, 403, out)

	status, out = hostHTTP(t, "POST", base+"/deliveries/claim", notifierToken, map[string]any{"channel": "hosted"})
	hostStatus(t, status, 200, out)
	items := out["deliveries"].([]any)
	if len(items) != 1 {
		t.Fatalf("claim %#v", out)
	}
	claimed := items[0].(map[string]any)
	recipient, _ := claimed["recipient"].(map[string]any)
	if recipient["external_subject"] != "acct_1" || recipient["external_issuer"] == "" || claimed["address"] != nil {
		t.Fatalf("claimed %#v", claimed)
	}
	status, out = hostHTTP(t, "POST", base+"/deliveries/"+claimed["delivery_id"].(string)+"/receipt", notifierToken, map[string]any{"claim_token": claimed["claim_token"], "outcome": "skipped", "error": "suppressed_by_account"})
	hostStatus(t, status, 200, out)
	if got := out["delivery"].(map[string]any); got["status"] != "canceled" {
		t.Fatalf("skipped receipt %#v", got)
	}

	// A conflicting declaration from another grant is refused.
	exchange(auth.TokenGrantTypeWorkspaceManagedAgent, fixture.signManagedAgentGrantAssertion(t, workspaceManagedGrantTokenOptions{
		Subject: "other", JTI: "jti-other-1", OrganizationID: "org_1", SlotID: "other", OwnerAccountID: "acct_owner",
		Delivers: &delivery.GrantDeclaration{Channel: "hosted"},
	}), http.StatusBadRequest)

	// A malformed declaration fails verification.
	exchange(auth.TokenGrantTypeWorkspaceManagedAgent, notifierGrant("jti-notifier-bad", &delivery.GrantDeclaration{Channel: "bridge"}), http.StatusUnauthorized)

	// An empty channel revokes the declaration.
	exchange(auth.TokenGrantTypeWorkspaceManagedAgent, notifierGrant("jti-notifier-2", &delivery.GrantDeclaration{}), http.StatusOK)
	status, out = hostHTTP(t, "POST", base+"/deliveries/claim", notifierToken, map[string]any{"channel": "hosted"})
	hostStatus(t, status, 403, out)
	exchange(auth.TokenGrantTypeWorkspaceManagedAgent, notifierGrant("jti-notifier-3", declaration), http.StatusOK)

	// Revoking the agent is final: a new grant cannot bring it back.
	if _, err := env.workspace.DB().Exec(`UPDATE agents SET revoked_at='2026-01-01T00:00:00Z' WHERE id=?`, notifier["agent"].(map[string]any)["agent_id"]); err != nil {
		t.Fatal(err)
	}
	exchange(auth.TokenGrantTypeWorkspaceManagedAgent, notifierGrant("jti-notifier-4", declaration), http.StatusForbidden)
	status, out = hostHTTP(t, "GET", base+"/notification-settings", humanToken, nil)
	hostStatus(t, status, 200, out)
	if available := out["available_channels"].([]any); len(available) != 0 {
		t.Fatalf("revoked agent's channel still available: %#v", out)
	}
}
