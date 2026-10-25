package server

import (
	"net/http"
	"testing"
	"time"
)

func TestCurationNeverFilesHumanAttention(t *testing.T) {
	requireIntegrationTest(t)
	f := newCurationFixture(t, "quiet")
	subject := f.openCard("Repair shared renderer")
	f.openCard("Repair the shared renderer")
	f.fileAsk(f.thread, "First question", subject, nil, []string{"Yes", "No"})
	f.fileAsk(f.thread, "Second question", subject, nil, []string{"Yes", "No"})
	var before, after int
	if err := f.env.workspace.DB().QueryRow(`SELECT count(*) FROM events WHERE type='human_attention_requested'`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	run := f.call(http.MethodPost, "/curation/run", f.agent.AccessToken, map[string]any{}, http.StatusOK)
	if len(run["proposals"].([]any)) < 2 {
		t.Fatalf("missing fallback suggestions: %#v", run)
	}
	if err := f.env.workspace.DB().QueryRow(`SELECT count(*) FROM events WHERE type='human_attention_requested'`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("curation added %d human attention requests; want 0", after-before)
	}
}

func TestCurationSuggestionsAreAgentWork(t *testing.T) {
	requireIntegrationTest(t)
	f := newCurationFixture(t, "agentwork")
	subject := f.openCard("Delivery context")
	f.fileAsk(f.thread, "First", subject, nil, []string{"Yes", "No"})
	f.fileAsk(f.thread, "Second", subject, nil, []string{"Yes", "No"})
	candidate := candidatesByAction(f.candidates(f.agent.AccessToken), "batch")[0]
	p := f.call(http.MethodPost, "/curation/proposals", f.agent.AccessToken, map[string]any{"action": "batch", "targets": candidate["targets"], "rationale": "Group related delivery."}, http.StatusCreated)["proposal"].(map[string]any)
	f.call(http.MethodPost, "/curation/proposals", f.human.AccessToken, map[string]any{"action": "batch", "targets": candidate["targets"], "resolution": "apply", "rationale": "Human should never be asked."}, http.StatusForbidden)
	agent := f.call(http.MethodGet, "/brief", f.agent.AccessToken, nil, http.StatusOK)
	suggestions := agent["curation_suggestions"].([]any)
	if len(suggestions) != 1 {
		t.Fatalf("agent suggestions: %#v", agent)
	}
	suggestion := suggestions[0].(map[string]any)
	if len(suggestion["apply_argv"].([]any)) != 5 || len(suggestion["dismiss_argv"].([]any)) != 5 {
		t.Fatalf("missing runnable actions: %#v", suggestion)
	}
	human := f.call(http.MethodGet, "/brief", f.human.AccessToken, nil, http.StatusOK)
	if len(human["curation_suggestions"].([]any)) != 0 || len(human["decisions"].([]any)) != 2 {
		t.Fatalf("suggestion interrupted human: %#v", human)
	}
	quiet := f.call(http.MethodGet, "/curation/candidates", f.human.AccessToken, nil, http.StatusOK)
	if len(quiet["suggestions"].([]any)) != 1 {
		t.Fatalf("quiet human affordance missing: %#v", quiet)
	}
	if err := refreshDerivedTopicProjection(f.ctx, handlerOptions{primitiveStore: f.env.primitiveStore}, f.thread, time.Now().UTC(), f.agent.ActorID); err != nil {
		t.Fatal(err)
	}
	var inbox, notifications int
	if err := f.env.workspace.DB().QueryRow(`SELECT count(*) FROM derived_inbox_items WHERE source_event_id=?`, p["event_id"]).Scan(&inbox); err != nil {
		t.Fatal(err)
	}
	if err := f.env.workspace.DB().QueryRow(`SELECT count(*) FROM deliveries WHERE source_ref=?`, "event:"+anyString(p["event_id"])).Scan(&notifications); err != nil {
		t.Fatal(err)
	}
	if inbox != 0 || notifications != 0 {
		t.Fatal(inbox, notifications)
	}
	f.respond(f.thread, anyString(p["event_id"]), "Batch", "answered")
	if len(f.briefDecisions(f.human.AccessToken)) != 1 {
		t.Fatal("agent batch did not group delivery")
	}
	agent = f.call(http.MethodGet, "/brief", f.agent.AccessToken, nil, http.StatusOK)
	if len(agent["curation_suggestions"].([]any)) != 0 {
		t.Fatal("resolved suggestion remained open")
	}
}

func TestCurationActionAndReceiptRollbackTogether(t *testing.T) {
	requireIntegrationTest(t)
	f := newCurationFixture(t, "atomic")
	subject := f.openCard("Atomic delivery context")
	f.fileAsk(f.thread, "First", subject, nil, []string{"Yes", "No"})
	f.fileAsk(f.thread, "Second", subject, nil, []string{"Yes", "No"})
	c := candidatesByAction(f.candidates(f.agent.AccessToken), "batch")[0]
	p := f.call(http.MethodPost, "/curation/proposals", f.agent.AccessToken, map[string]any{"action": "batch", "targets": c["targets"], "rationale": "Group related delivery."}, http.StatusCreated)["proposal"].(map[string]any)
	if _, err := f.env.workspace.DB().Exec(`CREATE TRIGGER fail_curation_receipt BEFORE INSERT ON events WHEN NEW.type='curation_receipt' BEGIN SELECT RAISE(ABORT,'injected receipt failure'); END`); err != nil {
		t.Fatal(err)
	}
	f.call(http.MethodPost, "/curation/proposals", f.agent.AccessToken, map[string]any{"action": "batch", "targets": c["targets"], "resolution": "apply", "rationale": "Apply atomically."}, http.StatusInternalServerError)
	var n int
	if err := f.env.workspace.DB().QueryRow(`SELECT count(*) FROM curation_batch_members`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 || f.proposalStatus(anyString(p["key"])) != "open" {
		t.Fatal("failed receipt left a partial action")
	}
	if _, err := f.env.workspace.DB().Exec(`DROP TRIGGER fail_curation_receipt`); err != nil {
		t.Fatal(err)
	}
	f.respond(f.thread, anyString(p["event_id"]), "Batch", "answered")
	if len(f.trail(f.thread)) != 1 {
		t.Fatal("retry lost receipt")
	}
}

func TestCurationCannotBeFiledThroughGenericAsk(t *testing.T) {
	requireIntegrationTest(t)
	f := newCurationFixture(t, "generic")
	subject := f.openCard("Generic guard")
	f.call(http.MethodPost, "/events", f.agent.AccessToken, map[string]any{"actor_id": f.agent.ActorID, "event": map[string]any{"type": "human_attention_requested", "thread_id": f.thread, "refs": []string{subject}, "summary": "Batch these asks?", "payload": map[string]any{"title": "Batch these asks?", "subject_ref": subject, "requester_actor_id": f.agent.ActorID, "curation": map[string]any{"action": "batch"}}, "provenance": eventProvenance()}}, http.StatusBadRequest)
}
