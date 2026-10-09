package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"testing"

	"agent-nexus-core/internal/primitives"
)

type accessTestAgent struct {
	notificationTestAgent
	AgentID string
}

func seedAccessTestAgent(t *testing.T, env authIntegrationEnv, username string) accessTestAgent {
	t.Helper()
	seeded := seedNotificationTestAgent(t, env, username)
	out := accessTestAgent{notificationTestAgent: seeded}
	if err := env.workspace.DB().QueryRow(`SELECT id FROM agents WHERE username=?`, seeded.Username).Scan(&out.AgentID); err != nil {
		t.Fatal(err)
	}
	return out
}

func accessTestEnv(t *testing.T) (authIntegrationEnv, string) {
	t.Helper()
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken, allowPasskeyDevBypass: true})
	status, p := hostHTTP(t, "POST", env.server.URL+"/auth/passkey/dev/register", "", map[string]any{"display_name": "Access reviewer", "bootstrap_token": testBootstrapToken})
	hostStatus(t, status, http.StatusCreated, p)
	return env, p["tokens"].(map[string]any)["access_token"].(string)
}

func TestAccessRequestHumanDecisionAndReplay(t *testing.T) {
	t.Parallel()
	env, human := accessTestEnv(t)
	agent := seedAccessTestAgent(t, env, "access.requester")
	base := env.server.URL
	status, p := hostHTTP(t, "POST", base+"/auth/access-requests", agent.AccessToken, map[string]any{"grant": "auth-admin", "reason": "Enroll a build host"})
	hostStatus(t, status, 200, p)
	request := p["request"].(map[string]any)
	id := request["id"].(string)
	inboxID := request["inbox_item_id"].(string)
	if request["principal_id"] != agent.AgentID || request["status"] != "pending" {
		t.Fatalf("wrong request: %#v", request)
	}
	status, p = hostHTTP(t, "POST", base+"/auth/access-requests", agent.AccessToken, map[string]any{"grant": "auth-admin", "reason": "Different retry"})
	hostStatus(t, status, 200, p)
	if p["request"].(map[string]any)["id"] != id || p["request"].(map[string]any)["reason"] != request["reason"] {
		t.Fatalf("retry changed request: %#v", p)
	}
	status, p = hostHTTP(t, "GET", base+"/auth/access-requests", human, nil)
	hostStatus(t, status, 200, p)
	if len(p["requests"].([]any)) != 1 {
		t.Fatalf("pending list: %#v", p)
	}
	status, p = hostHTTP(t, "GET", base+"/inbox", human, nil)
	hostStatus(t, status, 200, p)
	found := false
	for _, raw := range p["items"].([]any) {
		item := raw.(map[string]any)
		if item["id"] == inboxID {
			found = true
			if fmt.Sprint(item["allowed_response_outcomes"]) != "[approved rejected]" {
				t.Fatalf("unsafe outcomes: %#v", item)
			}
			if item["access_request_id"] != id || item["requested_grant"] != "auth-admin" {
				t.Fatalf("correlation: %#v", item)
			}
		}
	}
	if !found {
		t.Fatalf("access request missing from human Inbox: %#v", p)
	}
	for _, path := range []string{"/auth/access-requests", "/auth/access/summary"} {
		status, p = hostHTTP(t, "GET", base+path, agent.AccessToken, nil)
		hostStatus(t, status, 403, p)
	}
	for _, verb := range []string{"approve", "deny"} {
		status, p = hostHTTP(t, "POST", base+"/auth/access-requests/"+id+"/"+verb, agent.AccessToken, nil)
		hostStatus(t, status, 403, p)
	}
	status, p = hostHTTP(t, "POST", base+"/inbox/"+url.PathEscape(inboxID)+"/respond", human, map[string]any{"response_text": "Let me think", "outcome": "answered", "notify_mode": "none"})
	hostStatus(t, status, 400, p)
	status, p = hostHTTP(t, "POST", base+"/inbox/"+url.PathEscape(inboxID)+"/respond", human, map[string]any{"response_text": "Need details", "outcome": "needs_context", "notify_mode": "none"})
	hostStatus(t, status, 400, p)
	status, p = hostHTTP(t, "GET", base+"/auth/access/summary", human, nil)
	hostStatus(t, status, 200, p)
	if p["pending_count"] != float64(1) {
		t.Fatalf("unsupported reply changed pending request: %#v", p)
	}
	store := env.primitiveStore.(*primitives.Store)
	// The requester cannot withdraw a structured human grant decision.
	_, err := store.AppendHumanAttentionWithdrawal(context.Background(), agent.ActorID, request["request_event_ref"].(string)[6:], map[string]any{"payload": map[string]any{"reason": "Hide it"}})
	if err != primitives.ErrForbidden {
		t.Fatalf("access withdrawal err=%v", err)
	}
	beforeDecision, err := store.GetDerivedInboxItem(context.Background(), inboxID)
	if err != nil {
		t.Fatal(err)
	}
	status, p = hostHTTP(t, "POST", base+"/auth/access-requests/"+id+"/approve", human, nil)
	hostStatus(t, status, 200, p)
	if p["request"].(map[string]any)["status"] != "approved" {
		t.Fatalf("approval: %#v", p)
	}
	status, p = hostHTTP(t, "GET", base+"/auth/admins", agent.AccessToken, nil)
	hostStatus(t, status, 200, p)
	// An auth-admin grant still does not authorize agent approval of grants.
	for _, path := range []string{"/auth/access-requests", "/auth/access/summary"} {
		status, p = hostHTTP(t, "GET", base+path, agent.AccessToken, nil)
		hostStatus(t, status, 403, p)
	}
	for _, verb := range []string{"approve", "deny"} {
		status, p = hostHTTP(t, "POST", base+"/auth/access-requests/"+id+"/"+verb, agent.AccessToken, nil)
		hostStatus(t, status, 403, p)
	}
	status, p = hostHTTP(t, "POST", base+"/auth/admins/"+agent.AgentID+"/revoke", human, nil)
	hostStatus(t, status, 200, p)
	// Simulate a committed decision whose projection refresh failed. A retry
	// must repair the stale Inbox without reapplying the revoked grant.
	if err := store.ReplaceDerivedInboxItems(context.Background(), beforeDecision.ThreadID, []primitives.DerivedInboxItem{beforeDecision}); err != nil {
		t.Fatal(err)
	}
	status, p = hostHTTP(t, "POST", base+"/auth/access-requests/"+id+"/approve", human, nil)
	hostStatus(t, status, 200, p)
	status, p = hostHTTP(t, "GET", base+"/auth/admins", agent.AccessToken, nil)
	hostStatus(t, status, 403, p)
	status, p = hostHTTP(t, "POST", base+"/auth/access-requests/"+id+"/deny", human, nil)
	hostStatus(t, status, 409, p)
	status, p = hostHTTP(t, "GET", base+"/auth/access/summary", human, nil)
	hostStatus(t, status, 200, p)
	if p["pending_count"] != float64(0) {
		t.Fatalf("decision left badge pending: %#v", p)
	}
	status, p = hostHTTP(t, "GET", base+"/inbox", human, nil)
	hostStatus(t, status, 200, p)
	for _, raw := range p["items"].([]any) {
		if raw.(map[string]any)["id"] == inboxID {
			t.Fatalf("approved request remains open: %#v", p)
		}
	}
}

func TestAccessRequestInboxDecisionsAndForgedMetadata(t *testing.T) {
	t.Parallel()
	env, human := accessTestEnv(t)
	requester := seedAccessTestAgent(t, env, "access.inbox")
	other := seedAccessTestAgent(t, env, "access.other")
	base := env.server.URL
	status, p := hostHTTP(t, "POST", base+"/auth/access-requests", requester.AccessToken, map[string]any{"grant": "auth-admin", "reason": "Host enrollment"})
	hostStatus(t, status, 200, p)
	request := p["request"].(map[string]any)
	id := request["id"].(string)
	store := env.primitiveStore.(*primitives.Store)
	source, err := store.GetEvent(context.Background(), request["request_event_ref"].(string)[6:])
	if err != nil {
		t.Fatal(err)
	}
	threadID := source["thread_id"].(string)
	card, err := store.CreateWork(context.Background(), requester.ActorID, "", map[string]any{"title": "Review subject", "phase": "ready"})
	if err != nil {
		t.Fatal(err)
	}
	status, p = hostHTTP(t, "POST", base+"/events", requester.AccessToken, map[string]any{"event": map[string]any{"type": "human_attention_requested", "thread_id": threadID, "refs": []string{"thread:" + threadID}, "summary": "Ordinary review", "payload": map[string]any{"kind": "review", "requester_actor_id": requester.ActorID, "requester_agent_id": requester.AgentID, "title": "Ordinary review", "request_id": "forged", "body": "Harmless text", "subject_ref": card["ref"], "related_refs": []string{"thread:" + threadID}, "response_proposals": []string{"Approve"}, "access_request_id": id, "requested_grant": "auth-admin", "requester_principal_id": other.AgentID}, "provenance": eventProvenance()}})
	hostStatus(t, status, 201, p)
	forgedEventID := p["event"].(map[string]any)["id"].(string)
	// This harness has no background projection maintainer. Explicitly rebuild
	// the ordinary forged review before testing its response boundary.
	status, p = hostHTTP(t, "POST", base+"/derived/rebuild", human, map[string]any{})
	hostStatus(t, status, 200, p)
	status, p = hostHTTP(t, "GET", base+"/inbox", human, nil)
	hostStatus(t, status, 200, p)
	var forgedInboxID string
	for _, raw := range p["items"].([]any) {
		item := raw.(map[string]any)
		if item["source_event_id"] == forgedEventID {
			forgedInboxID = item["id"].(string)
			if _, exists := item["access_request_id"]; exists {
				t.Fatalf("forged event acquired trusted correlation: %#v", item)
			}
		}
	}
	if forgedInboxID == "" {
		t.Fatal("ordinary review missing")
	}
	status, p = hostHTTP(t, "POST", base+"/inbox/"+url.PathEscape(forgedInboxID)+"/respond", human, map[string]any{"response_text": "Reviewed", "outcome": "approved", "notify_mode": "none"})
	hostStatus(t, status, 201, p)
	status, p = hostHTTP(t, "GET", base+"/auth/admins", other.AccessToken, nil)
	hostStatus(t, status, 403, p)
	still, err := store.GetAccessRequest(context.Background(), id)
	if err != nil || still.Status != "pending" {
		t.Fatalf("forged correlation decided real request: %#v %v", still, err)
	}
	status, p = hostHTTP(t, "POST", base+"/inbox/"+url.PathEscape(request["inbox_item_id"].(string))+"/respond", human, map[string]any{"response_text": "Approved to enroll hosts", "outcome": "approved", "notify_mode": "none"})
	hostStatus(t, status, 201, p)
	status, p = hostHTTP(t, "POST", base+"/auth/access-requests/"+id+"/approve", human, nil)
	hostStatus(t, status, 200, p)
	status, p = hostHTTP(t, "GET", base+"/auth/admins", requester.AccessToken, nil)
	hostStatus(t, status, 200, p)
	denied := seedAccessTestAgent(t, env, "access.denied")
	status, p = hostHTTP(t, "POST", base+"/auth/access-requests", denied.AccessToken, map[string]any{"grant": "auth-admin", "reason": "Another grant"})
	hostStatus(t, status, 200, p)
	request = p["request"].(map[string]any)
	status, p = hostHTTP(t, "POST", base+"/inbox/"+url.PathEscape(request["inbox_item_id"].(string))+"/respond", human, map[string]any{"response_text": "No need for this grant", "outcome": "rejected", "notify_mode": "none"})
	hostStatus(t, status, 201, p)
	status, p = hostHTTP(t, "POST", base+"/auth/access-requests/"+request["id"].(string)+"/deny", human, nil)
	hostStatus(t, status, 200, p)
	status, p = hostHTTP(t, "GET", base+"/auth/admins", denied.AccessToken, nil)
	hostStatus(t, status, 403, p)
}

func TestAccessRequestCompetingDecisions(t *testing.T) {
	t.Parallel()
	env, human := accessTestEnv(t)
	agent := seedAccessTestAgent(t, env, "access.race")
	status, p := hostHTTP(t, "POST", env.server.URL+"/auth/access-requests", agent.AccessToken, map[string]any{"grant": "auth-admin", "reason": "Race request"})
	hostStatus(t, status, 200, p)
	id := p["request"].(map[string]any)["id"].(string)
	type result struct {
		status int
		verb   string
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, verb := range []string{"approve", "deny"} {
		wg.Add(1)
		go func(verb string) {
			defer wg.Done()
			<-start
			status, _ := hostHTTP(t, "POST", env.server.URL+"/auth/access-requests/"+id+"/"+verb, human, nil)
			results <- result{status, verb}
		}(verb)
	}
	close(start)
	wg.Wait()
	close(results)
	wins, conflicts := 0, 0
	for r := range results {
		if r.status == 200 {
			wins++
		} else if r.status == 409 {
			conflicts++
		} else {
			t.Fatalf("unexpected race %s: %d", r.verb, r.status)
		}
	}
	if wins != 1 || conflicts != 1 {
		t.Fatalf("wins=%d conflicts=%d", wins, conflicts)
	}
	var count int
	if err := env.workspace.DB().QueryRow(`SELECT COUNT(*) FROM human_attention_response_claims`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("claims=%d err=%v", count, err)
	}
	request, err := env.primitiveStore.(*primitives.Store).GetAccessRequest(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	status, p = hostHTTP(t, "GET", env.server.URL+"/auth/admins", agent.AccessToken, nil)
	if request.Status == "approved" {
		hostStatus(t, status, 200, p)
	} else {
		hostStatus(t, status, 403, p)
	}
}

func TestAccessRequestRevokedRequesterAndPendingEnrollments(t *testing.T) {
	t.Parallel()
	env, human := accessTestEnv(t)
	agent := seedAccessTestAgent(t, env, "access.revoked")
	status, p := hostHTTP(t, "POST", env.server.URL+"/auth/access-requests", agent.AccessToken, map[string]any{"grant": "auth-admin", "reason": "Needs permission"})
	hostStatus(t, status, 200, p)
	id := p["request"].(map[string]any)["id"].(string)
	if _, err := env.workspace.DB().Exec(`UPDATE agents SET revoked_at='2026-01-01T00:00:00Z' WHERE id=?`, agent.AgentID); err != nil {
		t.Fatal(err)
	}
	status, p = hostHTTP(t, "POST", env.server.URL+"/auth/access-requests/"+id+"/approve", human, nil)
	if status != 400 && status != 404 {
		t.Fatalf("revoked approval=%d %#v", status, p)
	}
	request, err := env.primitiveStore.(*primitives.Store).GetAccessRequest(context.Background(), id)
	if err != nil || request.Status != "pending" {
		t.Fatalf("failed grant wasn't atomic: %#v %v", request, err)
	}
	for i, spec := range []struct{ status, expiry string }{{"pending", "2999-01-01T00:00:00Z"}, {"approved", "2999-01-01T00:00:00Z"}, {"pending", "2000-01-01T00:00:00Z"}, {"completed", "2999-01-01T00:00:00Z"}, {"denied", "2999-01-01T00:00:00Z"}} {
		_, err := env.workspace.DB().Exec(`INSERT INTO host_enrollments(id,user_code,poll_token_hash,public_key,requested_slug,os_user,hostname,discovered_adapters_json,request_nonce,adoptions_json,requesting_ip,status,created_at,expires_at) VALUES(?,?,?,'key',?,'test','host','[]','nonce','[]','',?,'2026-01-01T00:00:00Z',?)`, fmt.Sprint(i), fmt.Sprint(i), fmt.Sprint(i), fmt.Sprint(i), spec.status, spec.expiry)
		if err != nil {
			t.Fatal(err)
		}
	}
	status, p = hostHTTP(t, "GET", env.server.URL+"/auth/access/summary", human, nil)
	hostStatus(t, status, 200, p)
	if p["pending_count"] != float64(3) || p["pending_host_enrollment_count"] != float64(2) || p["pending_access_request_count"] != float64(1) {
		t.Fatalf("wrong pending counts: %#v", p)
	}
}

func TestAccessRequestCreationIsSelfScopedAndConcurrentIdempotent(t *testing.T) {
	t.Parallel()
	env, human := accessTestEnv(t)
	agent := seedAccessTestAgent(t, env, "access.self")
	for _, body := range []map[string]any{{"grant": "auth-admin", "reason": " "}, {"grant": "unknown", "reason": "Grant this"}, {"grant": "auth-admin", "reason": "Grant this", "principal_id": "someone-else"}} {
		status, p := hostHTTP(t, "POST", env.server.URL+"/auth/access-requests", agent.AccessToken, body)
		hostStatus(t, status, 400, p)
	}
	status, p := hostHTTP(t, "POST", env.server.URL+"/auth/access-requests", human, map[string]any{"grant": "auth-admin", "reason": "Human request"})
	hostStatus(t, status, 403, p)
	ids := make(chan string, 4)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			status, p := hostHTTP(t, "POST", env.server.URL+"/auth/access-requests", agent.AccessToken, map[string]any{"grant": "auth-admin", "reason": "One request"})
			if status != 200 {
				t.Errorf("concurrent request %d %#v", status, p)
				return
			}
			ids <- p["request"].(map[string]any)["id"].(string)
		}()
	}
	close(start)
	wg.Wait()
	close(ids)
	first := ""
	n := 0
	for id := range ids {
		n++
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatalf("duplicate requests %s %s", id, first)
		}
	}
	if n != 4 {
		t.Fatalf("successful requests=%d", n)
	}
	var count int
	if err := env.workspace.DB().QueryRow(`SELECT COUNT(*) FROM events WHERE type='human_attention_requested'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("events=%d err=%v", count, err)
	}
}

func TestInboxSummaryRanksProjectedAskSeverity(t *testing.T) {
	t.Parallel()
	env, human := accessTestEnv(t)
	agent := seedAccessTestAgent(t, env, "summary.severity")
	status, p := hostHTTP(t, "POST", env.server.URL+"/auth/access-requests", agent.AccessToken, map[string]any{"grant": "auth-admin", "reason": "Create shared attention context"})
	hostStatus(t, status, 200, p)
	request := p["request"].(map[string]any)
	store := env.primitiveStore.(*primitives.Store)
	source, err := store.GetEvent(context.Background(), request["request_event_ref"].(string)[6:])
	if err != nil {
		t.Fatal(err)
	}
	threadID := source["thread_id"].(string)
	card, err := store.CreateWork(context.Background(), agent.ActorID, "", map[string]any{"title": "Ranking subject", "phase": "ready"})
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for i, severity := range []string{"low", "high", " HIGH ", ""} {
		event := map[string]any{
			"type": "human_attention_requested", "thread_id": threadID,
			"ts":   fmt.Sprintf("2026-01-01T00:00:0%dZ", i),
			"refs": []string{"thread:" + threadID}, "summary": "Summary ranking",
			"payload": map[string]any{"kind": "ask", "title": "Summary ranking", "body": "A decision is needed", "severity": severity,
				"requester_actor_id": agent.ActorID, "requester_agent_id": agent.AgentID,
				"subject_ref": card["ref"], "related_refs": []string{"thread:" + threadID}, "response_proposals": []string{"Proceed"}},
			"provenance": eventProvenance(),
		}
		status, p = hostHTTP(t, "POST", env.server.URL+"/events", agent.AccessToken, map[string]any{"event": event})
		hostStatus(t, status, 201, p)
		ids = append(ids, p["event"].(map[string]any)["id"].(string))
	}
	status, p = hostHTTP(t, "POST", env.server.URL+"/derived/rebuild", human, map[string]any{})
	hostStatus(t, status, 200, p)
	status, p = hostHTTP(t, "GET", env.server.URL+"/inbox/summary?limit=2", human, nil)
	hostStatus(t, status, 200, p)
	asks := p["asks"].([]any)
	if p["open_ask_count"] != float64(4) || len(asks) != 2 || asks[0].(map[string]any)["source_event_id"] != ids[1] || asks[1].(map[string]any)["source_event_id"] != ids[2] {
		t.Fatalf("severity rank, oldest tie, or unbounded count incorrect: %#v", p)
	}
}
