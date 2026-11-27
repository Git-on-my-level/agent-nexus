package server

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
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
	status, reused := propose(second, "new-rule")
	hostStatus(t, status, 409, reused)
	if !strings.Contains(fmt.Sprint(reused), "idempotency_conflict") {
		t.Fatalf("different content reused a request key: %v", reused)
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
	dismissed := "R-20261127-03"
	status, pending = propose(dismissed, "dismissed-proposal")
	hostStatus(t, status, 201, pending)
	status, revoked = hostHTTP(t, "POST", env.server.URL+"/rulings/"+dismissed+"/revoke", human.AccessToken, map[string]any{})
	hostStatus(t, status, 200, revoked)
	status, denied = confirm(dismissed, "dismissed-confirmation", human.AccessToken)
	hostStatus(t, status, 409, denied)
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

	// Two agents can file before either sees an active rule. The second
	// confirmation must revalidate against the winner's committed state.
	submissions := []string{"R-20261129-01", "R-20261129-02"}
	for _, id := range submissions {
		if err := post(ctx, id, "competing"); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range submissions {
		go func() {
			_, err := store.ConfirmOperatingRuling(ctx, "human", id, now)
			errorsSeen <- err
		}()
	}
	first, second = <-errorsSeen, <-errorsSeen
	if first == nil {
		first, second = second, first
	}
	var missing *primitives.RulingRelationsRequired
	if second != nil || !errors.As(first, &missing) || len(missing.Missing) != 1 {
		t.Fatalf("one confirmation must seek a relation to the winner: %v, %v", first, second)
	}
}

func TestOperatingRulingImportedHistory(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	store := env.primitiveStore.(*primitives.Store)
	ctx, now := context.Background(), time.Now()
	propose := func(id, hint, successor string, relations ...primitives.OperatingRulingRelation) {
		t.Helper()
		_, err := store.ProposeOperatingRuling(ctx, "importer", primitives.OperatingRulingProposal{
			RulingID: id, Scope: "account", Topic: "import-" + id[2:], Title: id, Body: "Rule", SourceQuote: "Quote",
			StatusHint: hint, SupersededByHint: successor, Relations: relations,
		}, now)
		if err != nil {
			t.Fatal(err)
		}
	}
	confirm := func(id, status, successor string) {
		t.Helper()
		r, err := store.ConfirmOperatingRuling(ctx, "human", id, now)
		if err != nil || r.Status != status || r.SupersededBy != successor || r.DecidedAt == "" {
			t.Fatalf("confirm %s: status %q, successor %q, decided at %q, err %v", id, r.Status, r.SupersededBy, r.DecidedAt, err)
		}
	}
	retired, current := "R-20261126-01", "R-20261126-02"
	propose(retired, "superseded", current)
	propose(current, "active", "", primitives.OperatingRulingRelation{Kind: "supersedes", RulingID: retired})
	confirm(retired, "superseded", current)
	confirm(current, "active", "")
	confirm(retired, "superseded", current)
	retired, current = "R-20261126-03", "R-20261126-04"
	propose(retired, "superseded", current)
	propose(current, "active", "", primitives.OperatingRulingRelation{Kind: "supersedes", RulingID: retired})
	confirm(current, "active", "")
	confirm(retired, "superseded", current)
	for i, hint := range []string{"revoked", "spent", "historical"} {
		id := fmt.Sprintf("R-20261127-%02d", i+1)
		propose(id, hint, "")
		status := "revoked"
		if hint == "historical" {
			status = "active" // Explicit human approval of an unresolved historical entry.
		}
		confirm(id, status, "")
	}
}
