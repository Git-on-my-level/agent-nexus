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

// Every active human keeps Watch Trends, even with another human's private
// board, an agent's private board and a legacy artifact whose content manifest
// is unknown. Agents still need full visibility and item reads keep their rules.
func TestWatchTrendsEveryHumanAdmittedHTTP(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	store := env.primitiveStore.(*primitives.Store)
	david := seedHumanPrincipalForLockoutTest(t, ctx, db, "david", "david", "david", "david-token")
	operator := seedHumanPrincipalForLockoutTest(t, ctx, db, "operator", "operator", "operator", "operator-token")
	agent := seedAgentReaderForTrendsTest(t, ctx, db, "pm-agent", "pm-agent-token")
	read := func(token string, status int) {
		t.Helper()
		for _, group := range primitives.WatchTrendGroups {
			trendsResponseBody(t, env.server.URL+"/watch/trends?group="+group, token, status)
		}
	}
	privateBoard := func(owner, title string) string {
		t.Helper()
		board, err := store.CreateBoard(ctx, owner, map[string]any{"title": title})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.PatchThread(ctx, owner, anyString(board["thread_id"]), map[string]any{"pm_actor_id": owner}, nil); err != nil {
			t.Fatal(err)
		}
		return anyString(board["thread_id"])
	}

	read(agent.AccessToken, http.StatusOK) // nothing hidden yet
	artifact, err := store.CreateArtifact(ctx, david.ActorID, map[string]any{"kind": "doc", "summary": "legacy", "refs": []string{}}, "legacy body", "text")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE artifacts SET content_refs_json=NULL WHERE id=?`, anyString(artifact["id"])); err != nil {
		t.Fatal(err)
	}
	privateBoard(operator.ActorID, "Operator's private board")
	privateBoard("unsponsored-agent", "Agent's private board")
	read("david-token", http.StatusOK)
	read("operator-token", http.StatusOK)
	read(agent.AccessToken, http.StatusForbidden)

	// Item-level reads keep their rules.
	if status, _ := hostHTTP(t, http.MethodGet, env.server.URL+"/artifacts/"+anyString(artifact["id"]), "david-token", nil); status == http.StatusOK {
		t.Fatal("Trends admission widened item reads")
	}
	status, body := hostHTTP(t, http.MethodGet, env.server.URL+"/boards", "david-token", nil)
	if status != http.StatusOK {
		t.Fatalf("boards: %d %v", status, body)
	}
	for _, board := range body["boards"].([]any) {
		if anyString(board.(map[string]any)["title"]) == "Operator's private board" {
			t.Fatal("Trends admission widened board reads")
		}
	}
}
