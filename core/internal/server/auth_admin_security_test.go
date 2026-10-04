package server

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-nexus-core/internal/auth"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

// Uses the same verified software WebAuthn fixture as the production registration
// integration test. No development bypass endpoint is enabled or called.
func securityHuman(t *testing.T, env authIntegrationEnv) auth.Principal {
	t.Helper()
	claim, err := env.authStore.ResolveOnboardingClaim(context.Background(), testBootstrapToken, "", auth.PrincipalKindHuman)
	if err != nil {
		t.Fatal(err)
	}
	session := securityRegistrationSession(t, env, claim)
	response := postJSONExpectStatusWithHeaders(t, env.server.URL+"/auth/passkey/register/verify", map[string]any{
		"session_id": session, "bootstrap_token": testBootstrapToken, "credential": mustDecodeJSONObject(t, samplePasskeyCredentialJSON),
	}, map[string]string{"Origin": "https://webauthn.io"}, http.StatusCreated)
	payload := sessionJSON(t, response)
	principal, err := env.authStore.AuthenticateAccessToken(context.Background(), payload["tokens"].(map[string]any)["access_token"].(string))
	if err != nil {
		t.Fatal(err)
	}
	return principal
}

func securityRegistrationSession(t *testing.T, env authIntegrationEnv, claim auth.OnboardingClaim) string {
	t.Helper()
	user := mustDecodeStdBase64(t, "1zMAAAAAAAAAAA==")
	return env.passkeySessionStore.Save(auth.PasskeySession{
		Kind: auth.PasskeySessionKindRegistration, DisplayName: "testuser1", UserHandle: user, OnboardingClaim: claim,
		SessionData: webauthn.SessionData{Challenge: "sVt4ScceMzqFSnfAq8hgLzblvo3fa4_aFVEcIESHIJ0", RelyingPartyID: "webauthn.io", UserID: user, Expires: time.Now().Add(time.Minute), UserVerification: protocol.VerificationPreferred, CredParams: []protocol.CredentialParameter{{Type: protocol.PublicKeyCredentialType, Algorithm: -7}}},
	})
}

func securityEnv(t *testing.T) (authIntegrationEnv, auth.Principal, auth.Principal, string) {
	t.Helper()
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken, webAuthnConfig: WebAuthnConfig{RPID: "webauthn.io", RPOrigin: "https://webauthn.io"}})
	human := securityHuman(t, env)
	machine := seedNotificationTestAgent(t, env, "fleet.security-host")
	agent, err := env.authStore.AuthenticateAccessToken(context.Background(), machine.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.authStore.SetAuthAdmin(context.Background(), agent.AgentID, true, human); err != nil {
		t.Fatal(err)
	}
	agent, err = env.authStore.AuthenticateAccessToken(context.Background(), machine.AccessToken)
	if err != nil || !agent.AuthAdmin {
		t.Fatalf("granted principal snapshot: %v", err)
	}
	return env, human, agent, machine.AccessToken
}

func TestAuthAdminCannotMintHumanCredentials(t *testing.T) {
	env, human, agent, bearer := securityEnv(t)
	ctx := context.Background()
	before, err := env.authStore.CountActiveHumanPrincipals(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"human", "any", "agent"} {
		status, p := hostHTTP(t, "POST", env.server.URL+"/auth/invites", bearer, map[string]any{"kind": kind})
		hostStatus(t, status, 403, p)
	}
	forged := agent
	forged.PrincipalKind = "human"
	if _, _, err := env.authStore.CreateInvite(ctx, forged, auth.CreateInviteInput{Kind: "human"}); !errors.Is(err, auth.ErrHumanRequired) {
		t.Fatalf("forged caller: %v", err)
	}

	invite, secret, err := env.authStore.CreateInvite(ctx, human, auth.CreateInviteInput{Kind: "human"})
	if err != nil {
		t.Fatal(err)
	}
	// A valid human-issued invitation still cannot be redeemed with agent authority.
	for _, path := range []string{"register/options", "register/verify", "login/options", "login/verify"} {
		status, p := hostHTTP(t, "POST", env.server.URL+"/auth/passkey/"+path, bearer, map[string]any{"invite_token": secret, "bootstrap_token": testBootstrapToken, "display_name": "Escalation", "credential": mustDecodeJSONObject(t, samplePasskeyCredentialJSON)})
		hostStatus(t, status, 403, p)
		if p["error"].(map[string]any)["code"] != "human_required" {
			t.Fatal(p)
		}
	}
	for _, path := range []string{"dev/register", "dev/login"} {
		status, p := hostHTTP(t, "POST", env.server.URL+"/auth/passkey/"+path, bearer, map[string]any{"invite_token": secret})
		hostStatus(t, status, 403, p)
		if p["error"].(map[string]any)["code"] != "dev_passkey_bypass_disabled" {
			t.Fatal(p)
		}
	}

	// Historical agent-issued invites are not human identity authority. Both fresh
	// options and already-issued registration sessions/claims must be rejected.
	claim, err := env.authStore.ResolveOnboardingClaim(ctx, "", secret, auth.PrincipalKindHuman)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.workspace.DB().Exec(`UPDATE auth_invites SET created_by_agent_id=?,created_by_actor_id=? WHERE id=?`, agent.AgentID, agent.ActorID, invite.ID); err != nil {
		t.Fatal(err)
	}
	status, p := hostHTTP(t, "POST", env.server.URL+"/auth/passkey/register/options", "", map[string]any{"invite_token": secret, "display_name": "Escalation"})
	hostStatus(t, status, 401, p)
	response := postJSONExpectStatusWithHeaders(t, env.server.URL+"/auth/passkey/register/verify", map[string]any{"session_id": securityRegistrationSession(t, env, claim), "invite_token": secret, "credential": mustDecodeJSONObject(t, samplePasskeyCredentialJSON)}, map[string]string{"Origin": "https://webauthn.io"}, 401)
	response.Body.Close()
	credential, err := auth.DevSyntheticPasskeyCredential()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := env.authStore.RegisterPasskeyAgent(ctx, auth.RegisterPasskeyAgentInput{DisplayName: "Escalation", UserHandle: []byte("new-human"), Credential: &credential}, claim); !errors.Is(err, auth.ErrInvalidToken) {
		t.Fatalf("cached invite claim bypass: %v", err)
	}
	// Omitting bearer authority does not help: bootstrap is already consumed and
	// a new human invite cannot be obtained. A session ID alone is no credential.
	for _, body := range []map[string]any{{"bootstrap_token": testBootstrapToken, "display_name": "Escalation"}, {"display_name": "Escalation"}} {
		status, p := hostHTTP(t, "POST", env.server.URL+"/auth/passkey/register/options", "", body)
		if status == 200 {
			t.Fatalf("onboarding without valid human authority: %v", p)
		}
	}
	for _, grant := range []string{"workspace_human_grant", "workspace_managed_agent_grant", "assertion", "refresh_token"} {
		status, p := hostHTTP(t, "POST", env.server.URL+"/auth/token", bearer, map[string]any{"grant_type": grant, "agent_id": human.AgentID, "assertion": bearer, "refresh_token": bearer, "signature": bearer})
		if status == 200 {
			t.Fatalf("agent bearer exchanged for human: %v", p)
		}
	}
	after, err := env.authStore.CountActiveHumanPrincipals(ctx)
	if err != nil || after != before {
		t.Fatalf("human principal growth: %d -> %d (%v)", before, after, err)
	}
}

func TestAuthAdminCannotRemoveHumanRecovery(t *testing.T) {
	env, human, agent, bearer := securityEnv(t)
	ctx := context.Background()
	// Provision a second human independently of the agent's grant.
	_, secret, err := env.authStore.CreateInvite(ctx, human, auth.CreateInviteInput{Kind: "human"})
	if err != nil {
		t.Fatal(err)
	}
	claim, err := env.authStore.ResolveOnboardingClaim(ctx, "", secret, auth.PrincipalKindHuman)
	if err != nil {
		t.Fatal(err)
	}
	credential, err := auth.DevSyntheticPasskeyCredential()
	if err != nil {
		t.Fatal(err)
	}
	_, tokens, err := env.authStore.RegisterPasskeyAgent(ctx, auth.RegisterPasskeyAgentInput{DisplayName: "Recovery human", UserHandle: []byte("recovery-human"), Credential: &credential}, claim)
	if err != nil {
		t.Fatal(err)
	}
	otherHuman, err := env.authStore.AuthenticateAccessToken(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	invite, recoverySecret, err := env.authStore.CreateInvite(ctx, human, auth.CreateInviteInput{Kind: "human"})
	if err != nil {
		t.Fatal(err)
	}
	// An agent grant adds fleet writes and administrative reads only.
	for _, path := range []string{"/auth/principals", "/auth/invites", "/auth/audit"} {
		status, p := hostHTTP(t, "GET", env.server.URL+path, bearer, nil)
		hostStatus(t, status, 200, p)
	}
	for _, target := range []string{human.AgentID, otherHuman.AgentID, agent.AgentID} {
		for _, body := range []map[string]any{{}, {"allow_human_lockout": true, "human_lockout_reason": "fleet takeover"}} {
			status, p := hostHTTP(t, "POST", env.server.URL+"/auth/principals/"+target+"/revoke", bearer, body)
			hostStatus(t, status, 403, p)
			if p["error"].(map[string]any)["code"] != "human_required" {
				t.Fatal(p)
			}
		}
	}
	status, p := hostHTTP(t, "POST", env.server.URL+"/auth/invites/"+invite.ID+"/revoke", bearer, nil)
	hostStatus(t, status, 403, p)
	// Transactional checks use actual database identity, not caller-supplied kind.
	forged := agent
	forged.PrincipalKind = "human"
	for _, target := range []string{human.AgentID, otherHuman.AgentID, agent.AgentID} {
		for _, override := range []bool{false, true} {
			_, err := env.authStore.RevokeAgent(ctx, target, auth.RevokeAgentInput{Actor: forged, Mode: auth.RevocationModeAdmin, AllowHumanLockout: override, HumanLockoutReason: map[bool]string{true: "takeover"}[override]})
			if !errors.Is(err, auth.ErrHumanRequired) {
				t.Fatalf("store revoke %s: %v", target, err)
			}
		}
	}
	if _, err := env.authStore.RevokeInvite(ctx, invite.ID, forged); !errors.Is(err, auth.ErrHumanRequired) {
		t.Fatal(err)
	}
	if _, err := env.authStore.RevokeAgent(ctx, agent.AgentID, auth.RevokeAgentInput{Actor: forged, Mode: auth.RevocationModeSelf, AllowHumanLockout: true, HumanLockoutReason: "takeover"}); !errors.Is(err, auth.ErrHumanRequired) {
		t.Fatalf("agent used self lockout override: %v", err)
	}
	// Self mode cannot be used to target a different human.
	if _, err := env.authStore.RevokeAgent(ctx, human.AgentID, auth.RevokeAgentInput{Actor: forged, Mode: auth.RevocationModeSelf}); !errors.Is(err, auth.ErrAuthRequired) {
		t.Fatal(err)
	}
	if _, err := env.authStore.ResolveOnboardingClaim(ctx, "", recoverySecret, auth.PrincipalKindHuman); err != nil {
		t.Fatalf("agent invalidated recovery invite: %v", err)
	}
	count, err := env.authStore.CountActiveHumanPrincipals(ctx)
	if err != nil || count != 3 {
		t.Fatalf("humans removed: %d %v", count, err)
	}
	if _, err := env.authStore.RevokeAgent(ctx, otherHuman.AgentID, auth.RevokeAgentInput{Actor: human, Mode: auth.RevocationModeAdmin}); err != nil {
		t.Fatal(err)
	}
	if _, err := env.authStore.RevokeAgent(ctx, "test-admin", auth.RevokeAgentInput{Actor: human, Mode: auth.RevocationModeAdmin}); err != nil {
		t.Fatal(err)
	}
	if count, err := env.authStore.CountActiveHumanPrincipals(ctx); err != nil || count != 1 {
		t.Fatalf("last-human fixture: %d %v", count, err)
	}
	status, p = hostHTTP(t, "POST", env.server.URL+"/auth/principals/"+human.AgentID+"/revoke", bearer, map[string]any{"allow_human_lockout": true, "human_lockout_reason": "last human takeover"})
	hostStatus(t, status, 403, p)
	// Stale human identity is denied in the transaction too, for both routes.
	if _, err := env.authStore.RevokeInvite(ctx, invite.ID, otherHuman); !errors.Is(err, auth.ErrHumanRequired) {
		t.Fatal(err)
	}
	if _, err := env.authStore.RevokeAgent(ctx, human.AgentID, auth.RevokeAgentInput{Actor: otherHuman, Mode: auth.RevocationModeAdmin}); !errors.Is(err, auth.ErrHumanRequired) {
		t.Fatal(err)
	}
	if _, err := env.authStore.RevokeInvite(ctx, invite.ID, human); err != nil {
		t.Fatal(err)
	}
	if _, err := env.authStore.SetAuthAdmin(ctx, agent.AgentID, false, human); err != nil {
		t.Fatal(err)
	}
}

func TestAuthAdminRemovalPreservesIssuedEnrollmentAuthority(t *testing.T) {
	env, human, agent, _ := securityEnv(t)
	ctx := context.Background()
	newInput := func(slug string) (auth.HostEnrollmentInput, ed25519.PrivateKey) {
		t.Helper()
		public, private, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		return auth.HostEnrollmentInput{PublicKey: base64.StdEncoding.EncodeToString(public), RequestedSlug: slug, OSUser: "test", Hostname: slug, DiscoveredAdapters: []string{}, RequestNonce: base64.RawURLEncoding.EncodeToString(public[:16]), Adoptions: []auth.AdoptionProof{}}, private
	}
	_, usableSecret, err := env.authStore.CreateHostEnrollmentTokenWithTTL(ctx, "survives", time.Hour, agent)
	if err != nil {
		t.Fatal(err)
	}
	revocable, revokedSecret, err := env.authStore.CreateHostEnrollmentTokenWithTTL(ctx, "cancel", time.Hour, agent)
	if err != nil {
		t.Fatal(err)
	}
	start := func(slug string) (auth.EnrollmentStart, ed25519.PrivateKey) {
		in, key := newInput(slug)
		request, err := env.authStore.StartHostEnrollment(ctx, in, "127.0.0.1")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := env.authStore.DecideHostEnrollment(ctx, request.EnrollmentID, true, agent); err != nil {
			t.Fatal(err)
		}
		return request, key
	}
	usable, usableKey := start("approved-survives")
	cancelable, cancelableKey := start("approved-cancel")
	if _, err := env.authStore.SetAuthAdmin(ctx, agent.AgentID, false, human); err != nil {
		t.Fatal(err)
	}
	list, err := env.authStore.PendingHostEnrollments(ctx)
	if err != nil || len(list) != 2 || list[0].Status != "approved" || list[1].Status != "approved" {
		t.Fatalf("approvals not discoverable for cancellation: %#v %v", list, err)
	}
	if _, err := env.authStore.DecideHostEnrollment(ctx, cancelable.EnrollmentID, false, agent); !errors.Is(err, auth.ErrAuthAdminRequired) {
		t.Fatal(err)
	}
	if _, err := env.authStore.DecideHostEnrollment(ctx, cancelable.EnrollmentID, false, human); err != nil {
		t.Fatal(err)
	}
	if _, err := env.authStore.RevokeHostEnrollmentToken(ctx, revocable.ID, human); err != nil {
		t.Fatal(err)
	}
	complete := func(request auth.EnrollmentStart, key ed25519.PrivateKey) error {
		_, err := env.authStore.CompleteHostEnrollment(ctx, request.EnrollmentID, request.PollToken, hostSign(key, "anx-host-enroll-complete|"+request.EnrollmentID+"|"+request.PollToken))
		return err
	}
	if err := complete(usable, usableKey); err != nil {
		t.Fatalf("approved ceremony lost independent authority: %v", err)
	}
	if err := complete(cancelable, cancelableKey); !errors.Is(err, auth.ErrEnrollmentDenied) {
		t.Fatalf("canceled approval completed: %v", err)
	}
	for _, test := range []struct {
		slug, secret string
		denied       bool
	}{{"token-survives", usableSecret, false}, {"token-cancel", revokedSecret, true}} {
		in, key := newInput(test.slug)
		in.EnrollmentToken = test.secret
		in.Signature = hostSign(key, "anx-host-headless-enroll|"+in.RequestNonce+"|"+in.RequestedSlug+"|"+in.PublicKey)
		_, err := env.authStore.CompleteHeadlessHostEnrollment(ctx, in)
		if test.denied && !errors.Is(err, auth.ErrInvalidToken) {
			t.Fatalf("revoked token consumed: %v", err)
		}
		if !test.denied && err != nil {
			t.Fatalf("issued token lost independent authority: %v", err)
		}
	}
	if _, err := env.authStore.DecideHostEnrollment(ctx, usable.EnrollmentID, false, human); !errors.Is(err, auth.ErrEnrollmentConsumed) {
		t.Fatalf("completed enrollment cancelable: %v", err)
	}
}

type blockedAdminBody struct {
	io.Reader
	entered, release chan struct{}
	once             sync.Once
}

func (b *blockedAdminBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.entered); <-b.release })
	return b.Reader.Read(p)
}
func (b *blockedAdminBody) Close() error { return nil }

func TestAuthAdminRevokedDuringBodyUpload(t *testing.T) {
	env, human, agent, bearer := securityEnv(t)
	other := seedNotificationTestAgent(t, env, "fleet.other-security-host")
	body := &blockedAdminBody{Reader: strings.NewReader(`{"label":"must-not-exist","expires_in_seconds":600}`), entered: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	unblock := func() { release.Do(func() { close(body.release) }) }
	defer unblock()
	request := httptest.NewRequest("POST", "/auth/hosts/enrollment-tokens", body)
	request.Header.Set("Authorization", "Bearer "+bearer)
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); env.server.Config.Handler.ServeHTTP(recorder, request) }()
	select {
	case <-body.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not reach body read")
	}
	// Admission has cached the granted principal, but the write has not begun.
	if _, err := env.authStore.SetAuthAdmin(context.Background(), agent.AgentID, false, human); err != nil {
		t.Fatal(err)
	}
	unblock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("request did not complete")
	}
	if recorder.Code != 403 || !strings.Contains(recorder.Body.String(), "auth_admin_required") {
		t.Fatalf("stale request succeeded: %d %s", recorder.Code, recorder.Body.String())
	}
	tokens, err := env.authStore.ListHostEnrollmentTokens(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range tokens {
		if token.Label == "must-not-exist" {
			t.Fatal("revoked caller created token")
		}
	}
	// Every privileged mutation rejects the original, stale principal snapshot.
	ctx := context.Background()
	checks := []func() error{
		func() error { _, err := env.authStore.RevokeHost(ctx, other.Host.ID, agent); return err },
		func() error {
			_, _, err := env.authStore.CreateHostEnrollmentTokenWithTTL(ctx, "stale", time.Hour, agent)
			return err
		},
		func() error { _, err := env.authStore.DecideHostEnrollment(ctx, "missing", true, agent); return err },
		func() error { _, err := env.authStore.RevokeHostEnrollmentToken(ctx, "missing", agent); return err },
	}
	for i, check := range checks {
		if err := check(); !errors.Is(err, auth.ErrAuthAdminRequired) {
			t.Errorf("stale mutation %d: %v", i, err)
		}
	}
}
