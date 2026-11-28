package server

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

// Withdrawing proposals releases their names so a merged ruling can carry
// them, and an agent confirms the merged ruling only on a recorded human
// decision that names it.
func TestOperatingRulingWithdrawMergeAndRecordedConfirm(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	ctx := context.Background()
	store := env.primitiveStore.(*primitives.Store)
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "merge-human", "merge-human-actor", "merge-human", "merge-human-token")
	author := seedNotificationTestAgent(t, env, "ruling.author")
	other := seedNotificationTestAgent(t, env, "ruling.other")
	post := func(path, token string, body map[string]any) (int, map[string]any) {
		t.Helper()
		return hostHTTP(t, "POST", env.server.URL+path, token, body)
	}
	propose := func(token, id, key string, aliases []string, relations ...map[string]string) (int, map[string]any) {
		t.Helper()
		return post("/rulings/propose", token, map[string]any{
			"ruling_id": id, "title": "Rule " + id, "topic": "deploy", "scope": "workspace",
			"body": "Body " + id, "source_quote": "Quote", "idempotency_key": key, "aliases": aliases, "relations": relations,
		})
	}
	withdraw := func(token, key string, ids ...string) (int, map[string]any) {
		t.Helper()
		return post("/rulings/withdraw", token, map[string]any{"ruling_ids": ids, "idempotency_key": key})
	}
	confirm := func(token, id, key, ref string) (int, map[string]any) {
		t.Helper()
		body := map[string]any{"idempotency_key": key}
		if ref != "" {
			body["decision_ref"] = ref
		}
		return post("/rulings/"+id+"/confirm", token, body)
	}
	a, b, c := "R-20261112-01", "R-20261114-01", "R-20261120-02"
	status, payload := propose(author.AccessToken, a, "a", []string{"R1"})
	hostStatus(t, status, 201, payload)
	status, payload = propose(author.AccessToken, b, "b", []string{"R17"}, map[string]string{"kind": "amends", "ruling_id": a})
	hostStatus(t, status, 201, payload)
	status, payload = propose(other.AccessToken, c, "c", []string{"R28"})
	hostStatus(t, status, 201, payload)

	// Another agent's proposal is not yours to withdraw; nothing changes.
	status, payload = withdraw(author.AccessToken, "w-foreign", a, b, c)
	hostStatus(t, status, 403, payload)
	// A remaining proposal that relates to one in the set blocks it.
	status, payload = withdraw(author.AccessToken, "w-dangling", a)
	hostStatus(t, status, 409, payload)
	if !strings.Contains(fmt.Sprint(payload), b) {
		t.Fatalf("dangling relation was not named: %v", payload)
	}
	status, payload = withdraw(author.AccessToken, "w-own", a, b)
	hostStatus(t, status, 200, payload)
	status, replay := withdraw(author.AccessToken, "w-own", a, b)
	hostStatus(t, status, 200, replay)
	// A human may withdraw any proposal.
	status, payload = withdraw(human.AccessToken, "w-human", c)
	hostStatus(t, status, 200, payload)
	if pending, _, err := store.OperatingRulingDecisions(ctx, time.Now()); err != nil || len(pending) != 0 {
		t.Fatalf("withdrawn proposals still reach Decide: %v %v", pending, err)
	}

	// The merged ruling carries the withdrawn IDs and their legacy names.
	merged := "R-20261128-02"
	status, payload = propose(author.AccessToken, merged, "merged", []string{a, b, c, "R1", "R17", "R28"})
	hostStatus(t, status, 201, payload)
	// A live ruling's ID cannot become another ruling's alias.
	status, payload = propose(author.AccessToken, "R-20261128-03", "steal", []string{merged})
	hostStatus(t, status, 409, payload)
	// Nor can a withdrawn ID be proposed again once a ruling holds it.
	status, payload = propose(author.AccessToken, a, "again", nil)
	hostStatus(t, status, 409, payload)

	note := func(text, decidedBy string) string {
		t.Helper()
		p := map[string]any{"kind": "card_message", "text": text}
		if decidedBy != "" {
			p["decided_by"] = decidedBy
		}
		event, err := store.AppendEvent(ctx, author.ActorID, map[string]any{"type": "message_posted", "thread_id": "rulings", "refs": []string{"thread:rulings"}, "payload": p})
		if err != nil {
			t.Fatal(err)
		}
		return "event:" + fmt.Sprint(event["id"])
	}
	status, payload = confirm(author.AccessToken, merged, "no-ref", "")
	hostStatus(t, status, 403, payload)
	status, payload = confirm(author.AccessToken, merged, "undecided", note("Confirm "+merged, ""))
	hostStatus(t, status, 400, payload)
	status, payload = confirm(author.AccessToken, merged, "unrelated", note("Confirm R-20261128-09", "David"))
	hostStatus(t, status, 400, payload)
	status, payload = confirm(author.AccessToken, merged, "missing", "event:missing")
	hostStatus(t, status, 400, payload)
	decision := note("Decided: confirm "+merged+" as the deploy ruling.", "David")
	status, payload = confirm(author.AccessToken, merged, "decided", decision)
	hostStatus(t, status, 200, payload)
	ruling := payload["ruling"].(map[string]any)
	if ruling["status"] != "active" || ruling["decision_ref"] != decision || ruling["decided_for"] != "David" || fmt.Sprint(ruling["decided_by"]) == "" {
		t.Fatalf("recorded confirmation lost its provenance: %v", ruling)
	}
	page, _, _, err := store.OperatingRulingPage(ctx, "")
	if err != nil || len(page) != 1 || page[0].RulingID != merged || len(page[0].Aliases) != 6 {
		t.Fatalf("projection after merge: %+v %v", page, err)
	}
	// Active rulings are not withdrawable.
	status, payload = withdraw(human.AccessToken, "w-active", merged)
	hostStatus(t, status, 409, payload)
}
