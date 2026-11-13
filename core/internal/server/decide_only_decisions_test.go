package server

import (
	"context"
	"net/url"
	"testing"

	"agent-nexus-core/internal/primitives"
)

// One question is one Decide row; a standing ruling answers a covered ask
// without a row; "not a decision" turns the next copy into an fyi; and any
// agent may retire a stale ask, which the asker then reads in its brief.
func TestDecideHoldsOnlyDecisions(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	ctx := context.Background()
	store := env.primitiveStore.(*primitives.Store)
	asker := seedNotificationTestAgent(t, env, "decide.asker")
	tender := seedNotificationTestAgent(t, env, "decide.tender")
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "decide-human", "decide-human-actor", "decide-human", "decide-human-token")
	work, err := store.CreateWork(ctx, asker.ActorID, "", map[string]any{"title": "Decide subject", "phase": "in_progress"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.GetBoardCard(ctx, "", anyString(work["id"]))
	if err != nil {
		t.Fatal(err)
	}
	file := func(title string, extra map[string]any) (int, map[string]any) {
		t.Helper()
		payload := map[string]any{"kind": "ask", "title": title, "requester_actor_id": asker.ActorID, "subject_ref": card["ref"], "response_proposals": []any{"Yes", "No"}, "situation": "The nightly report ran."}
		for k, v := range extra {
			payload[k] = v
		}
		return hostHTTP(t, "POST", env.server.URL+"/events", asker.AccessToken, map[string]any{"actor_id": asker.ActorID, "event": map[string]any{
			"type": "human_attention_requested", "thread_id": card["thread_id"], "refs": []string{anyString(card["ref"]), "thread:" + anyString(card["thread_id"])}, "summary": title,
			"provenance": map[string]any{"sources": []string{"actor_statement:test"}}, "payload": payload}})
	}
	brief := func(token string) map[string]any {
		t.Helper()
		status, p := hostHTTP(t, "GET", env.server.URL+"/brief", token, nil)
		hostStatus(t, status, 200, p)
		return p
	}

	status, first := file("Rerun the counts-only diagnostic?", nil)
	hostStatus(t, status, 201, first)
	firstID := anyString(first["event"].(map[string]any)["id"])
	status, repeat := file("rerun the counts only diagnostic", nil)
	hostStatus(t, status, 200, repeat)
	if repeat["deduplicated"] != true || anyString(repeat["event"].(map[string]any)["id"]) != firstID {
		t.Fatalf("a repeat must return the open ask: %v", repeat)
	}
	if got := len(brief(human.AccessToken)["decisions"].([]any)); got != 1 {
		t.Fatalf("decisions=%d, want 1", got)
	}

	// Superseding closes the old ask instead of leaving two rows.
	status, replacement := file("Rerun the counts-only diagnostic twice?", map[string]any{"supersedes": "event:" + firstID})
	hostStatus(t, status, 201, replacement)
	replacementID := anyString(replacement["event"].(map[string]any)["id"])
	decisions := brief(human.AccessToken)["decisions"].([]any)
	if len(decisions) != 1 || anyString(decisions[0].(map[string]any)["event_ref"]) != "event:"+replacementID {
		t.Fatalf("supersede must leave only the new ask: %v", decisions)
	}

	// Any agent may retire a stale ask; the asker reads why in its brief.
	status, retired := hostHTTP(t, "POST", env.server.URL+"/asks/"+url.PathEscape("event:"+replacementID)+"/retire", tender.AccessToken, map[string]any{"request_key": "tend-1", "reason": "The diagnostic already ran.", "evidence": []string{anyString(card["ref"])}})
	hostStatus(t, status, 200, retired)
	returned := brief(asker.AccessToken)["returned_to_you"].([]any)
	if len(returned) != 1 || anyString(returned[0].(map[string]any)["reason"]) != "The diagnostic already ran." {
		t.Fatalf("returned_to_you=%v", returned)
	}
	if by := returned[0].(map[string]any)["returned_by"].(map[string]any); anyString(by["id"]) != tender.ActorID {
		t.Fatalf("returned_by=%v", by)
	}

	// An agent cannot make a ruling; a human can, and it answers the next ask.
	status, p := hostHTTP(t, "POST", env.server.URL+"/rulings", asker.AccessToken, map[string]any{"ask_ref": "event:" + firstID, "kind": "answer", "answer": "Yes, counts-only reruns never need me."})
	hostStatus(t, status, 403, p)
	status, p = hostHTTP(t, "POST", env.server.URL+"/rulings", human.AccessToken, map[string]any{"ask_ref": "event:" + firstID, "kind": "answer", "answer": "Yes, counts-only reruns never need me."})
	hostStatus(t, status, 201, p)
	rulingID := anyString(p["ruling"].(map[string]any)["id"])
	status, covered := file("Rerun the counts-only diagnostic?", nil)
	hostStatus(t, status, 200, covered)
	if covered["answered_by_ruling"] != true || anyString(covered["ruling"].(map[string]any)["answer"]) != "Yes, counts-only reruns never need me." {
		t.Fatalf("a covered ask must return the ruling: %v", covered)
	}
	status, list := hostHTTP(t, "GET", env.server.URL+"/rulings", asker.AccessToken, nil)
	hostStatus(t, status, 200, list)
	if rulings := list["rulings"].([]any); len(rulings) != 1 || rulings[0].(map[string]any)["uses"].(float64) != 1 {
		t.Fatalf("rulings=%v", list)
	}
	// A retry with the same request key replays the first response and counts
	// the use once.
	retry := map[string]any{"actor_id": asker.ActorID, "request_key": "covered-retry", "event": map[string]any{
		"type": "human_attention_requested", "thread_id": card["thread_id"], "refs": []string{anyString(card["ref"]), "thread:" + anyString(card["thread_id"])}, "summary": "Rerun the counts-only diagnostic?",
		"provenance": map[string]any{"sources": []string{"actor_statement:test"}},
		"payload":    map[string]any{"kind": "ask", "title": "Rerun the counts-only diagnostic?", "requester_actor_id": asker.ActorID, "subject_ref": card["ref"], "response_proposals": []any{"Yes", "No"}}}}
	for i := 0; i < 2; i++ {
		status, p = hostHTTP(t, "POST", env.server.URL+"/events", asker.AccessToken, retry)
		hostStatus(t, status, 200, p)
		if p["answered_by_ruling"] != true {
			t.Fatalf("retry %d: %v", i, p)
		}
	}
	status, list = hostHTTP(t, "GET", env.server.URL+"/rulings", asker.AccessToken, nil)
	if list["rulings"].([]any)[0].(map[string]any)["uses"].(float64) != 2 {
		t.Fatalf("a retried covered ask must count once: %v", list)
	}
	// A client that applied the ruling before filing reports the use.
	status, p = hostHTTP(t, "POST", env.server.URL+"/rulings/"+rulingID+"/applied", asker.AccessToken, map[string]any{})
	hostStatus(t, status, 200, p)
	status, list = hostHTTP(t, "GET", env.server.URL+"/rulings", asker.AccessToken, nil)
	if list["rulings"].([]any)[0].(map[string]any)["uses"].(float64) != 3 {
		t.Fatalf("an applied use must be counted: %v", list)
	}
	// --again reaches the human even when a ruling exists: the facts changed.
	status, again := file("Rerun the counts-only diagnostic?", map[string]any{"again": true})
	hostStatus(t, status, 201, again)
	status, p = hostHTTP(t, "POST", env.server.URL+"/rulings/"+rulingID+"/revoke", human.AccessToken, map[string]any{})
	hostStatus(t, status, 200, p)

	// "Not a decision" binds the requester and the dated pattern: the next
	// day's digest is told, not asked.
	status, digest := file("Daily digest 2026-11-12: change any priorities?", nil)
	hostStatus(t, status, 201, digest)
	digestID := anyString(digest["event"].(map[string]any)["id"])
	// A ruling is made from an ask the human has dealt with, never an open one.
	status, p = hostHTTP(t, "POST", env.server.URL+"/rulings", human.AccessToken, map[string]any{"ask_ref": "event:" + digestID, "kind": "not_a_decision"})
	hostStatus(t, status, 409, p)
	status, p = hostHTTP(t, "POST", env.server.URL+"/asks/"+url.PathEscape("event:"+digestID)+"/retire", human.AccessToken, map[string]any{"request_key": "send-back", "reason": "Not a decision: this is information, not a choice.", "evidence": []string{"event:" + digestID}})
	hostStatus(t, status, 200, p)
	status, p = hostHTTP(t, "POST", env.server.URL+"/rulings", human.AccessToken, map[string]any{"ask_ref": "event:" + digestID, "kind": "not_a_decision"})
	hostStatus(t, status, 201, p)
	before := len(brief(human.AccessToken)["decisions"].([]any))
	status, told := file("Daily digest 2026-11-13: change any priorities?", nil)
	hostStatus(t, status, 200, told)
	if told["redirected"] != "tell" {
		t.Fatalf("the next digest must be redirected: %v", told)
	}
	after := brief(human.AccessToken)
	if got := len(after["decisions"].([]any)); got != before {
		t.Fatalf("decisions grew from %d to %d", before, got)
	}
	fyi := after["fyi"].([]any)
	if len(fyi) != 1 || anyString(fyi[0].(map[string]any)["title"]) != "Daily digest 2026-11-13: change any priorities?" || anyString(fyi[0].(map[string]any)["text"]) != "The nightly report ran." {
		t.Fatalf("fyi=%v", fyi)
	}
}
