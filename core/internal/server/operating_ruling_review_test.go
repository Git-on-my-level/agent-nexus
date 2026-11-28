package server

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"
)

func reviewRulingBody(id string) map[string]any {
	return map[string]any{"ruling_id": id, "idempotency_key": id, "title": "Routing", "scope": "workspace", "topic": "routing", "body": "Use the correct host.", "source_quote": "Use the correct host."}
}

func TestOperatingRulingBriefSurvivesFailedPendingRead(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	ctx := context.Background()
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "review-human", "review-human-actor", "review-human", "review-human-token")
	agent := seedNotificationTestAgent(t, env, "review.author")
	s := env.primitiveStore.(*primitives.Store)
	work, err := s.CreateWork(ctx, agent.ActorID, "", map[string]any{"title": "Keep visible ask", "phase": "in_progress"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.GetBoardCard(ctx, "", anyString(work["id"]))
	if err != nil {
		t.Fatal(err)
	}
	cardRef, thread := anyString(card["ref"]), anyString(card["thread_id"])
	status, result := hostHTTP(t, "POST", env.server.URL+"/events", agent.AccessToken, map[string]any{"actor_id": agent.ActorID, "event": map[string]any{
		"type": "human_attention_requested", "thread_id": thread, "refs": []string{cardRef, "thread:" + thread}, "summary": "Keep this visible?",
		"provenance": map[string]any{"sources": []string{"actor_statement:test"}},
		"payload":    map[string]any{"kind": "ask", "title": "Keep this visible?", "requester_actor_id": agent.ActorID, "subject_ref": cardRef, "response_proposals": []any{"Yes", "No"}, "situation": "A pending decision."},
	}})
	hostStatus(t, status, 201, result)
	id := "R-20261127-01"
	status, result = hostHTTP(t, "POST", env.server.URL+"/rulings/propose", agent.AccessToken, reviewRulingBody(id))
	hostStatus(t, status, 201, result)
	if _, err := env.workspace.DB().ExecContext(ctx, `UPDATE standing_rulings SET aliases_json='{}' WHERE ruling_id=?`, id); err != nil {
		t.Fatal(err)
	}
	status, brief := hostHTTP(t, "GET", env.server.URL+"/brief", human.AccessToken, nil)
	hostStatus(t, status, 200, brief)
	warnings, _ := brief["warnings"].([]any)
	decisions, _ := brief["decisions"].([]any)
	if len(warnings) != 1 || warnings[0].(map[string]any)["code"] != "ruling_proposals_unavailable" || len(decisions) != 1 || decisions[0].(map[string]any)["title"] != "Keep this visible?" {
		t.Fatalf("brief lost existing decisions or the warning: %v", brief)
	}
}

func TestOperatingRulingProposalRuneLimits(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	agent := seedNotificationTestAgent(t, env, "review.unicode")
	proposal := reviewRulingBody("R-20261127-01")
	limits := map[string]int{"title": 1024, "body": 32768, "source_quote": 8192, "source": 2048}
	for field, max := range limits {
		proposal[field] = strings.Repeat("😀", max)
	}
	status, result := hostHTTP(t, "POST", env.server.URL+"/rulings/propose", agent.AccessToken, proposal)
	hostStatus(t, status, 201, result)
	n := 1
	for field, max := range limits {
		n++
		proposal["ruling_id"] = fmt.Sprintf("R-20261127-%02d", n)
		proposal["idempotency_key"] = proposal["ruling_id"]
		proposal[field] = strings.Repeat("😀", max+1)
		status, result = hostHTTP(t, "POST", env.server.URL+"/rulings/propose", agent.AccessToken, proposal)
		hostStatus(t, status, 400, result)
		proposal[field] = strings.Repeat("😀", max)
	}
}

func TestOperatingRulingProposalChargesWorkspaceStorage(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	ctx := context.Background()
	agent := seedNotificationTestAgent(t, env, "review.quota")
	layout := env.workspace.Layout()
	newStore := func(limit int64) *primitives.Store {
		return primitives.NewStore(env.workspace.DB(), blob.NewFilesystemBackend(layout.ArtifactContentDir), layout.ArtifactContentDir, primitives.WithDatabasePath(layout.DatabasePath), primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{MaxBlobBytes: limit}))
	}
	usage, err := newStore(0).GetWorkspaceUsageSummary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	limited := newStore(usage.Usage.StorageBytes + 20*1024)
	before, err := limited.GetMaintenanceWorkspaceUsageSummary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewHandler("test", WithAuthStore(env.authStore), WithActorRegistry(env.registry), WithPrimitiveStore(limited)))
	defer server.Close()
	body := reviewRulingBody("R-20261127-01")
	body["body"] = strings.Repeat("é", 12*1024)
	status, result := hostHTTP(t, "POST", server.URL+"/rulings/propose", agent.AccessToken, body)
	hostStatus(t, status, 409, result)
	if !strings.Contains(fmt.Sprint(result), "quota_exceeded") {
		t.Fatalf("expected workspace storage quota error: %v", result)
	}
	page, _, _, err := newStore(0).OperatingRulingPage(ctx, "")
	if err != nil || len(page) != 0 {
		t.Fatalf("over-quota write committed: %v, %v", page, err)
	}

	text := strings.Repeat("é", 6*1024)
	body["body"] = text
	status, result = hostHTTP(t, "POST", server.URL+"/rulings/propose", agent.AccessToken, body)
	hostStatus(t, status, 201, result)
	after, err := limited.GetMaintenanceWorkspaceUsageSummary(ctx)
	if err != nil || after.Usage.StorageBytes-before.Usage.StorageBytes < int64(len(text)) {
		t.Fatalf("proposal did not count toward canonical storage: before=%v after=%v err=%v", before.Usage.StorageBytes, after.Usage.StorageBytes, err)
	}
	next := reviewRulingBody("R-20261127-02")
	next["body"] = text
	status, result = hostHTTP(t, "POST", server.URL+"/rulings/propose", agent.AccessToken, next)
	hostStatus(t, status, 409, result)
	if !strings.Contains(fmt.Sprint(result), "quota_exceeded") {
		t.Fatalf("second write should exceed cumulative storage quota: %v", result)
	}
	status, result = hostHTTP(t, "POST", server.URL+"/rulings/propose", agent.AccessToken, body)
	hostStatus(t, status, 201, result)
}

func TestOperatingRulingCannotClaimLegacyAnswerName(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	ctx := context.Background()
	agent := seedNotificationTestAgent(t, env, "review.alias")
	if _, err := env.workspace.DB().ExecContext(ctx, `INSERT INTO standing_rulings(id,title_key,kind,question,answer,source_ask_id,created_by,created_at)
	VALUES('ruling_old123','route','answer','Route?','Yes','older-ask','human','2026-11-26T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	body := reviewRulingBody("R-20261127-01")
	body["aliases"] = []string{"available-alias", "ruling_old123"}
	status, result := hostHTTP(t, "POST", env.server.URL+"/rulings/propose", agent.AccessToken, body)
	hostStatus(t, status, 409, result)
	body["aliases"] = []string{"available-alias"}
	status, result = hostHTTP(t, "POST", env.server.URL+"/rulings/propose", agent.AccessToken, body)
	hostStatus(t, status, 201, result)
}

func TestOperatingRulingHiddenPeerReturnsPrivateConflict(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	ctx := context.Background()
	s := env.primitiveStore.(*primitives.Store)
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "peer-owner", "peer-owner-actor", "peer-owner", "peer-owner-token")
	agent := seedNotificationTestAgent(t, env, "review.stranger")
	board, err := s.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Private board"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PatchThread(ctx, owner.ActorID, anyString(board["thread_id"]), map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	id := "R-20261127-01"
	body := reviewRulingBody(id)
	body["body"] = "Use " + anyString(board["ref"])
	status, result := hostHTTP(t, "POST", env.server.URL+"/rulings/propose", owner.AccessToken, body)
	hostStatus(t, status, 201, result)
	status, result = hostHTTP(t, "POST", env.server.URL+"/rulings/"+id+"/confirm", owner.AccessToken, map[string]any{"idempotency_key": "confirm"})
	hostStatus(t, status, 200, result)
	status, result = hostHTTP(t, "GET", env.server.URL+"/rulings/projection", agent.AccessToken, nil)
	hostStatus(t, status, 200, result)
	if strings.Contains(fmt.Sprint(result), id) {
		t.Fatalf("fixture leaked its private incumbent: %v", result)
	}
	status, result = hostHTTP(t, "POST", env.server.URL+"/rulings/propose", agent.AccessToken, reviewRulingBody("R-20261127-02"))
	hostStatus(t, status, 409, result)
	message := fmt.Sprint(result)
	if !strings.Contains(message, "needs_relation") || !strings.Contains(message, "ask a human with access") || strings.Contains(message, id) {
		t.Fatalf("incorrect hidden-peer resolution: %v", result)
	}
}
