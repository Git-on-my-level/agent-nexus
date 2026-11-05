package server

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"testing"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
)

// A single human whose own host's agent owns a private thread keeps Watch
// Trends; another human's (or their agent's) private thread still blocks it,
// and no agent inherits anything from its sponsor.
func TestWatchTrendsHostSponsorDelegationHTTP(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	store := env.primitiveStore.(*primitives.Store)
	david := seedHumanPrincipalForLockoutTest(t, ctx, db, "david", "david", "david", "david-token")
	davidP := auth.Principal{AgentID: david.AgentID, ActorID: david.ActorID}

	enroll := func(slug string, approver auth.Principal, headless bool) (string, string, ed25519.PrivateKey) {
		t.Helper()
		public, private, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		in := auth.HostEnrollmentInput{PublicKey: base64.StdEncoding.EncodeToString(public), RequestedSlug: slug, OSUser: "test", Hostname: slug, DiscoveredAdapters: []string{}, RequestNonce: base64.RawURLEncoding.EncodeToString(public[:16]), Adoptions: []auth.AdoptionProof{}}
		var host auth.Host
		if headless {
			_, secret, err := env.authStore.CreateHostEnrollmentTokenWithTTL(ctx, slug, time.Hour, approver)
			if err != nil {
				t.Fatal(err)
			}
			in.EnrollmentToken = secret
			in.Signature = hostSign(private, "anx-host-headless-enroll|"+in.RequestNonce+"|"+in.RequestedSlug+"|"+in.PublicKey)
			host, err = env.authStore.CompleteHeadlessHostEnrollment(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
		} else {
			request, err := env.authStore.StartHostEnrollment(ctx, in, "127.0.0.1")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := env.authStore.DecideHostEnrollment(ctx, request.EnrollmentID, true, approver); err != nil {
				t.Fatal(err)
			}
			host, err = env.authStore.CompleteHostEnrollment(ctx, request.EnrollmentID, request.PollToken, hostSign(private, "anx-host-enroll-complete|"+request.EnrollmentID+"|"+request.PollToken))
			if err != nil {
				t.Fatal(err)
			}
		}
		return host.ID, host.KeyID, private
	}
	derive := func(hostID, keyID string, key ed25519.PrivateKey, name string) (string, string) {
		t.Helper()
		signed := time.Now().UTC().Format(time.RFC3339)
		agent, tokens, err := env.authStore.IssueHostAgentToken(ctx, hostID, keyID, name, signed, hostSign(key, "anx-host-agent-token|"+hostID+"|"+keyID+"|"+name+"|"+signed))
		if err != nil {
			t.Fatal(err)
		}
		return agent.ActorID, tokens.AccessToken
	}
	sponsor := func(hostID string) string {
		t.Helper()
		var actor string
		_ = db.QueryRow(`SELECT sponsor_actor_id FROM host_sponsors WHERE host_id=?`, hostID).Scan(&actor)
		return actor
	}
	privateBoard := func(owner, title string) string {
		t.Helper()
		board, err := store.CreateBoard(ctx, owner, map[string]any{"title": title})
		if err != nil {
			t.Fatal(err)
		}
		thread := anyString(board["thread_id"])
		if _, err = store.PatchThread(ctx, owner, thread, map[string]any{"pm_actor_id": owner}, nil); err != nil {
			t.Fatal(err)
		}
		return thread
	}
	read := func(token string, status int) {
		t.Helper()
		for _, group := range primitives.WatchTrendGroups {
			trendsResponseBody(t, env.server.URL+"/watch/trends?group="+group, token, status)
		}
	}

	// While David is the only human, his device approval sponsors the host;
	// a token he issues sponsors its host in any workspace.
	deviceHost, deviceKey, devicePriv := enroll("david-laptop", davidP, false)
	erin := seedHumanPrincipalForLockoutTest(t, ctx, db, "erin", "erin", "erin", "erin-token")
	erinP := auth.Principal{AgentID: erin.AgentID, ActorID: erin.ActorID}
	tokenHost, tokenKey, tokenPriv := enroll("david-server", davidP, true)
	erinHost, erinKey, erinPriv := enroll("erin-laptop", erinP, true)
	if sponsor(deviceHost) != "david" || sponsor(tokenHost) != "david" || sponsor(erinHost) != "erin" {
		t.Fatalf("sponsors: device=%q token=%q erin=%q", sponsor(deviceHost), sponsor(tokenHost), sponsor(erinHost))
	}
	pm, pmToken := derive(deviceHost, deviceKey, devicePriv, "pm")
	persona, _ := derive(tokenHost, tokenKey, tokenPriv, "reviewer")
	erinAgent, _ := derive(erinHost, erinKey, erinPriv, "claude")

	read("david-token", http.StatusOK)
	privateBoard(pm, "PM's private board")
	privateBoard(persona, "Persona's private board")
	read("david-token", http.StatusOK) // own agents' private roots no longer lock him out
	read(pmToken, http.StatusForbidden)

	// The human still cannot read his agent's private board item by item.
	status, body := hostHTTP(t, http.MethodGet, env.server.URL+"/boards", "david-token", nil)
	if status != http.StatusOK {
		t.Fatalf("boards: %d %v", status, body)
	}
	for _, board := range body["boards"].([]any) {
		if title := anyString(board.(map[string]any)["title"]); title == "PM's private board" || title == "Persona's private board" {
			t.Fatalf("delegation widened ordinary reads: %v", board)
		}
	}
	// The agent does not gain the human's private items.
	privateBoard("david", "David's private board")
	status, body = hostHTTP(t, http.MethodGet, env.server.URL+"/boards", pmToken, nil)
	if status != http.StatusOK {
		t.Fatalf("agent boards: %d %v", status, body)
	}
	for _, board := range body["boards"].([]any) {
		if anyString(board.(map[string]any)["title"]) == "David's private board" {
			t.Fatal("agent inherited the human's private board")
		}
	}

	// Another human's agent, and another human, still block David.
	for _, foreign := range []string{erinAgent, "erin"} {
		thread := privateBoard(foreign, "Foreign private board")
		read("david-token", http.StatusForbidden)
		if _, err := store.PatchThread(ctx, foreign, thread, map[string]any{"pm_actor_id": ""}, nil); err != nil {
			t.Fatal(err)
		}
		read("david-token", http.StatusOK)
	}
	read("erin-token", http.StatusForbidden) // David's agents and David block Erin

	// With two humans, approving by ID proves nothing about who runs the host:
	// Erin approving David's machine admits it but sponsors nobody, so Erin
	// gains nothing over its agents and David's own Trends do not change.
	strayHost, strayKey, strayPriv := enroll("david-desktop", erinP, false)
	if got := sponsor(strayHost); got != "" {
		t.Fatalf("cross-human device approval recorded sponsor %q", got)
	}
	stray, _ := derive(strayHost, strayKey, strayPriv, "claude")
	strayThread := privateBoard(stray, "Unsponsored agent's private board")
	read("erin-token", http.StatusForbidden)
	read("david-token", http.StatusForbidden) // unsponsored roots fail closed
	if _, err := store.PatchThread(ctx, stray, strayThread, map[string]any{"pm_actor_id": ""}, nil); err != nil {
		t.Fatal(err)
	}
	read("david-token", http.StatusOK)
	// A device approval by David himself is no longer sole-human either.
	secondHost, _, _ := enroll("david-tablet", davidP, false)
	if got := sponsor(secondHost); got != "" {
		t.Fatalf("multi-human device approval recorded sponsor %q", got)
	}
}

// An auth-admin agent can approve hosts, but sponsors nothing.
// A second human who joins between approval and completion cancels the sole
// approver's sponsorship: completion re-checks the condition.
func TestHostSponsorRecheckedAtCompletion(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	david := seedHumanPrincipalForLockoutTest(t, ctx, db, "david", "david", "david", "david-token")
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	in := auth.HostEnrollmentInput{PublicKey: base64.StdEncoding.EncodeToString(public), RequestedSlug: "window", OSUser: "test", Hostname: "window", DiscoveredAdapters: []string{}, RequestNonce: base64.RawURLEncoding.EncodeToString(public[:16]), Adoptions: []auth.AdoptionProof{}}
	request, err := env.authStore.StartHostEnrollment(ctx, in, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.authStore.DecideHostEnrollment(ctx, request.EnrollmentID, true, auth.Principal{AgentID: david.AgentID, ActorID: david.ActorID}); err != nil {
		t.Fatal(err)
	}
	seedHumanPrincipalForLockoutTest(t, ctx, db, "erin", "erin", "erin", "erin-token")
	host, err := env.authStore.CompleteHostEnrollment(ctx, request.EnrollmentID, request.PollToken, hostSign(private, "anx-host-enroll-complete|"+request.EnrollmentID+"|"+request.PollToken))
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM host_sponsors WHERE host_id=?`, host.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("sponsor survived a second human joining before completion: %d %v", n, err)
	}
}

func TestHostSponsorRequiresHumanApprover(t *testing.T) {
	requireIntegrationTest(t)
	env, _, agent, _ := securityEnv(t)
	ctx := context.Background()
	public, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	in := auth.HostEnrollmentInput{PublicKey: base64.StdEncoding.EncodeToString(public), RequestedSlug: "agent-approved", OSUser: "test", Hostname: "agent-approved", DiscoveredAdapters: []string{}, RequestNonce: base64.RawURLEncoding.EncodeToString(public[:16]), Adoptions: []auth.AdoptionProof{}}
	request, err := env.authStore.StartHostEnrollment(ctx, in, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := env.authStore.DecideHostEnrollment(ctx, request.EnrollmentID, true, agent); err != nil {
		t.Fatal(err)
	}
	host, err := env.authStore.CompleteHostEnrollment(ctx, request.EnrollmentID, request.PollToken, hostSign(private, "anx-host-enroll-complete|"+request.EnrollmentID+"|"+request.PollToken))
	if err != nil {
		t.Fatal(err)
	}
	_, secret, err := env.authStore.CreateHostEnrollmentTokenWithTTL(ctx, "agent-token", time.Hour, agent)
	if err != nil {
		t.Fatal(err)
	}
	public2, private2, _ := ed25519.GenerateKey(nil)
	in2 := auth.HostEnrollmentInput{PublicKey: base64.StdEncoding.EncodeToString(public2), RequestedSlug: "agent-token", OSUser: "test", Hostname: "agent-token", DiscoveredAdapters: []string{}, RequestNonce: base64.RawURLEncoding.EncodeToString(public2[:16]), Adoptions: []auth.AdoptionProof{}, EnrollmentToken: secret}
	in2.Signature = hostSign(private2, "anx-host-headless-enroll|"+in2.RequestNonce+"|"+in2.RequestedSlug+"|"+in2.PublicKey)
	host2, err := env.authStore.CompleteHeadlessHostEnrollment(ctx, in2)
	if err != nil {
		t.Fatal(err)
	}
	var n int
	if err := env.workspace.DB().QueryRow(`SELECT count(*) FROM host_sponsors WHERE host_id IN (?,?)`, host.ID, host2.ID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("agent approval sponsored %d hosts: %v", n, err)
	}
}
