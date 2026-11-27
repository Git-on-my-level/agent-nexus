package server

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

func TestOperatingRulingDecideAndProjection(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	ctx := context.Background()
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "r45-human", "r45-human-actor", "r45-human", "r45-human-token")
	agent := seedNotificationTestAgent(t, env, "ruling.author")
	first, second := "R-20261126-08", "R-20261127-01"
	propose := func(id, key string, relations ...map[string]string) (int, map[string]any) {
		t.Helper()
		return hostHTTP(t, "POST", env.server.URL+"/rulings/propose", agent.AccessToken, map[string]any{
			"ruling_id": id, "title": "Fleet routing", "topic": "routing", "scope": "account",
			"body": "Use the enrolled host.", "source_quote": "Use the enrolled host.", "idempotency_key": key,
			"aliases": []string{"legacy-" + id}, "relations": relations,
		})
	}
	read := func(path, token string) map[string]any {
		t.Helper()
		status, p := hostHTTP(t, "GET", env.server.URL+path, token, nil)
		hostStatus(t, status, 200, p)
		return p
	}
	confirm := func(id, key, token string) (int, map[string]any) {
		t.Helper()
		return hostHTTP(t, "POST", env.server.URL+"/rulings/"+id+"/confirm", token, map[string]any{"idempotency_key": key})
	}
	status, made := propose(first, "new-rule")
	hostStatus(t, status, 201, made)
	status, replay := propose(first, "new-rule")
	hostStatus(t, status, 201, replay)
	if !reflect.DeepEqual(made, replay) {
		t.Fatalf("proposal replay changed: %v %v", made, replay)
	}
	brief := read("/brief", human.AccessToken)
	decisions := brief["decisions"].([]any)
	if len(decisions) != 1 || decisions[0].(map[string]any)["kind"] != "ruling_proposal" {
		t.Fatalf("expected one ruling confirmation: %v", decisions)
	}
	if decisions := read("/brief", agent.AccessToken)["decisions"].([]any); len(decisions) != 0 {
		t.Fatalf("agent sees human decisions: %v", decisions)
	}
	status, denied := confirm(first, "denied", agent.AccessToken)
	hostStatus(t, status, 403, denied)
	status, active := confirm(first, "accept-first", human.AccessToken)
	hostStatus(t, status, 200, active)
	status, redirect := propose(second, "new-second")
	hostStatus(t, status, 200, redirect)
	if redirect["redirected"] != "needs_relation" || !reflect.DeepEqual(redirect["missing_relations"], []any{first}) {
		t.Fatalf("missing relation was not redirected: %v", redirect)
	}
	status, pending := propose(second, "new-second", map[string]string{"kind": "supersedes", "ruling_id": first})
	hostStatus(t, status, 201, pending)
	before := read("/rulings/projection", agent.AccessToken)
	status, accepted := confirm(second, "accept-second", human.AccessToken)
	hostStatus(t, status, 200, accepted)
	after := read("/rulings/projection", agent.AccessToken)
	if before["source_revision"] == after["source_revision"] {
		t.Fatal("confirmation did not invalidate the revision")
	}
	rows := after["rulings"].([]any)
	old, next := rows[0].(map[string]any), rows[1].(map[string]any)
	if old["status"] != "superseded" || old["superseded_by"] != second || next["status"] != "active" || next["decided_by"] != human.ActorID || next["decided_at"] == "" {
		t.Fatalf("incorrect atomic supersession: %v", rows)
	}
	if left := read("/brief", human.AccessToken)["decisions"].([]any); len(left) != 0 {
		t.Fatalf("closed proposal still in Decide: %v", left)
	}
	status, denied = hostHTTP(t, "POST", env.server.URL+"/rulings/"+second+"/revoke", agent.AccessToken, map[string]any{})
	hostStatus(t, status, 403, denied)
	status, revoked := hostHTTP(t, "POST", env.server.URL+"/rulings/"+second+"/revoke", human.AccessToken, map[string]any{})
	hostStatus(t, status, 200, revoked)
	status, replay = confirm(second, "accept-second", human.AccessToken)
	hostStatus(t, status, 200, replay)
	if !reflect.DeepEqual(accepted, replay) {
		t.Fatalf("confirm must replay the saved receipt even after revoke: %v", replay)
	}
}

func TestOperatingRulingPageAndConcurrentAliases(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	ctx := context.Background()
	store := env.primitiveStore.(*primitives.Store)
	now := time.Now()
	post := func(ctx context.Context, id, topic string, aliases ...string) error {
		_, err := store.ProposeOperatingRuling(ctx, "author", primitives.OperatingRulingProposal{RulingID: id, Scope: "workspace", Topic: topic, Title: id, Body: "Rule", SourceQuote: "Quote", Aliases: aliases}, now)
		return err
	}
	for i := 0; i < 101; i++ {
		if err := post(ctx, fmt.Sprintf("R-202611%02d-%02d", 1+i/99, i%99+1), "many"); err != nil {
			t.Fatal(err)
		}
	}
	page, revision, cursor, err := store.OperatingRulingPage(ctx, "")
	if err != nil || len(page) != 100 || (cursor == "" || cursor == page[99].RulingID) {
		t.Fatalf("first page length=%d cursor=%q err=%v", len(page), cursor, err)
	}
	tail, revisionAgain, cursor, err := store.OperatingRulingPage(ctx, cursor)
	if err != nil || len(tail) != 1 || cursor != "" || revisionAgain != revision {
		t.Fatalf("last page length=%d cursor=%q err=%v", len(tail), cursor, err)
	}
	errorsSeen := make(chan error, 2)
	for _, id := range []string{"R-20261128-01", "R-20261128-02"} {
		go func() { errorsSeen <- post(ctx, id, "many", "shared-alias") }()
	}
	first, second := <-errorsSeen, <-errorsSeen
	if !((first == nil && second == primitives.ErrConflict) || (second == nil && first == primitives.ErrConflict)) {
		t.Fatalf("one write must own the alias: %v, %v", first, second)
	}
}
