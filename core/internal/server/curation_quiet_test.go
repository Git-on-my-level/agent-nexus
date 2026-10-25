package server

import (
	"agent-nexus-core/internal/storage"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
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

func TestCurationUpgradeClearsHumanInboxProjection(t *testing.T) {
	requireIntegrationTest(t)
	f := newCurationFixture(t, "upgrade-inbox")
	subject := f.openCard("Legacy curation subject")
	ordinary := f.fileAsk(f.thread, "Real human decision", subject, nil, []string{"Yes", "No"})
	legacyIDs := []string{}
	// All three legacy proposal kinds were already visible in the human inbox.
	for _, action := range []string{"batch", "merge", "withdraw"} {
		legacy := f.fileAsk(f.thread, "Legacy "+action+" proposal", subject, nil, []string{"Yes", "No"})
		if _, err := f.env.workspace.DB().Exec(`UPDATE events SET payload_json=json_set(payload_json,'$.payload.curation',json_object('action',?)) WHERE id=?`, action, legacy); err != nil {
			t.Fatal(err)
		}
		legacyIDs = append(legacyIDs, legacy)
	}
	if err := refreshDerivedTopicProjection(f.ctx, handlerOptions{primitiveStore: f.store}, f.thread, time.Now().UTC(), f.agent.ActorID); err != nil {
		t.Fatal(err)
	}
	before := f.call(http.MethodGet, "/inbox", f.human.AccessToken, nil, http.StatusOK)
	for _, legacy := range legacyIDs {
		if !strings.Contains(fmt.Sprint(before), legacy) {
			t.Fatalf("legacy ask was not materialized: %#v", before)
		}
	}
	if _, err := f.env.workspace.DB().Exec(`DELETE FROM schema_migrations WHERE version=119`); err != nil {
		t.Fatal(err)
	}
	upgraded, err := storage.InitializeWorkspace(f.ctx, f.env.workspace.Layout().RootDir)
	if err != nil {
		t.Fatal(err)
	}
	if err = upgraded.Close(); err != nil {
		t.Fatal(err)
	}
	var stale int
	encoded, _ := json.Marshal(legacyIDs)
	if err = f.env.workspace.DB().QueryRow(`SELECT count(*) FROM derived_inbox_items WHERE source_event_id IN (SELECT value FROM json_each(?))`, string(encoded)).Scan(&stale); err != nil {
		t.Fatal(err)
	}
	if stale != 0 {
		t.Fatalf("migration left %d stale human inbox rows", stale)
	}
	var desired, materialized int
	if err = f.env.workspace.DB().QueryRow(`SELECT desired_generation,materialized_generation FROM topic_projection_refresh_status WHERE thread_id=?`, f.thread).Scan(&desired, &materialized); err != nil {
		t.Fatal(err)
	}
	if desired <= materialized {
		t.Fatal("migration did not queue projection summary refresh")
	}
	after := f.call(http.MethodGet, "/inbox", f.human.AccessToken, nil, http.StatusOK)
	for _, legacy := range legacyIDs {
		if strings.Contains(fmt.Sprint(after), legacy) {
			t.Fatalf("legacy curation still listed: %#v", after)
		}
	}
	if !strings.Contains(fmt.Sprint(after), ordinary) {
		t.Fatalf("migration cleared an ordinary ask: %#v", after)
	}

}

func TestCurationCanResuggestAfterUndo(t *testing.T) {
	requireIntegrationTest(t)
	for _, action := range []string{"merge", "batch"} {
		t.Run(action, func(t *testing.T) {
			f := newCurationFixture(t, "resuggest-"+action)
			subject := f.openCard("Repair shared renderer")
			if action == "merge" {
				f.openCard("Repair the shared renderer")
			} else {
				f.fileAsk(f.thread, "First", subject, nil, []string{"Yes", "No"})
				f.fileAsk(f.thread, "Second", subject, nil, []string{"Yes", "No"})
			}
			c := candidatesByAction(f.candidates(f.agent.AccessToken), action)[0]
			body := map[string]any{"action": action, "targets": c["targets"], "rationale": "Agent reviewed cleanup."}
			first := f.call(http.MethodPost, "/curation/proposals", f.agent.AccessToken, body, http.StatusCreated)["proposal"].(map[string]any)
			f.respond(f.thread, anyString(first["event_id"]), "Apply", "answered")
			if action == "merge" {
				f.call(http.MethodPost, "/work/"+url.PathEscape(anyString(c["targets"].([]any)[0]))+"/unfold", f.agent.AccessToken, map[string]any{}, http.StatusOK)
			} else {
				f.call(http.MethodPost, "/curation/ungroup", f.agent.AccessToken, map[string]any{"key": first["key"]}, http.StatusOK)
			}
			next := f.call(http.MethodPost, "/curation/proposals", f.agent.AccessToken, body, http.StatusCreated)["proposal"].(map[string]any)
			if next["status"] != "created" || next["key"] != first["key"] || next["event_id"] == first["event_id"] {
				t.Fatalf("accepted suggestion did not get a fresh generation after undo: %#v", next)
			}
			repeat := f.call(http.MethodPost, "/curation/proposals", f.agent.AccessToken, body, http.StatusCreated)["proposal"].(map[string]any)
			if repeat["status"] != "existing" || repeat["event_id"] != next["event_id"] {
				t.Fatalf("open generation failed to dedupe: %#v", repeat)
			}
			f.respond(f.thread, anyString(next["event_id"]), "Apply", "answered")
			var receipts int
			if err := f.env.workspace.DB().QueryRow(`SELECT count(*) FROM events WHERE type='curation_receipt'`).Scan(&receipts); err != nil {
				t.Fatal(err)
			}
			if receipts != 2 {
				t.Fatalf("want one receipt per generation, got %d", receipts)
			}
		})
	}
}

func TestCurationProposeAdvertisesApplyCapability(t *testing.T) {
	requireIntegrationTest(t)
	f := newCurationFixture(t, "capability")
	subject := f.openCard("Capability delivery context")
	f.fileAsk(f.thread, "First", subject, nil, []string{"Yes", "No"})
	f.fileAsk(f.thread, "Second", subject, nil, []string{"Yes", "No"})
	c := candidatesByAction(f.candidates(f.agent.AccessToken), "batch")[0]
	for _, tc := range []struct {
		token    string
		canApply bool
	}{{f.agent.AccessToken, true}, {f.human.AccessToken, false}} {
		response := f.call(http.MethodPost, "/curation/proposals", tc.token, map[string]any{"action": "batch", "targets": c["targets"], "rationale": "Quiet capability check."}, http.StatusCreated)
		if response["can_apply"] != tc.canApply {
			t.Fatalf("wrong apply capability: %#v", response)
		}
	}
}
