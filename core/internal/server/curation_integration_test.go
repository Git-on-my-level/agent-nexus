package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/curation"
	"agent-nexus-core/internal/primitives"
)

type curationFixture struct {
	t       *testing.T
	env     authIntegrationEnv
	store   *primitives.Store
	agent   notificationTestAgent
	human   lockoutPrincipalSeed
	thread  string
	boardID string
	ctx     context.Context
}

func newCurationFixture(t *testing.T, name string) *curationFixture {
	t.Helper()
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	ctx := context.Background()
	agent := seedNotificationTestAgent(t, env, name+".agent")
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), name+"-human", name+"-human-actor", name+"-human", name+"-human-token")
	store, ok := env.primitiveStore.(*primitives.Store)
	if !ok {
		t.Fatal("primitive store is not *primitives.Store")
	}
	f := &curationFixture{t: t, env: env, store: store, agent: agent, human: human, ctx: ctx}
	f.thread = f.newThread("Curation thread")
	board, err := store.CreateBoard(ctx, agent.ActorID, map[string]any{"title": "Curation"})
	if err != nil {
		t.Fatal(err)
	}
	f.boardID = anyString(board["id"])
	return f
}

func (f *curationFixture) newThread(title string) string {
	return integrationSeedThreadWithStore(f.t, f.env.primitiveStore, nil, f.agent.ActorID, map[string]any{
		"title": title, "type": "incident", "status": "active", "priority": "p2",
		"tags": []any{}, "cadence": "daily", "current_summary": "summary", "next_actions": []any{}, "key_artifacts": []any{},
		"provenance": map[string]any{"sources": []any{"inferred"}},
	})
}

func (f *curationFixture) call(method, path, token string, body any, want int) map[string]any {
	f.t.Helper()
	status, out := hostHTTP(f.t, method, f.env.server.URL+path, token, body)
	if status != want {
		f.t.Fatalf("%s %s: %d (want %d) %#v", method, path, status, want, out)
	}
	return out
}

// trackerCard creates an open card backed by a tracker source and returns its ref and id.
func (f *curationFixture) trackerCard(title, native string) (string, string) {
	f.t.Helper()
	card, err := f.store.CreateWork(f.ctx, f.agent.ActorID, f.boardID, map[string]any{"title": title, "source": map[string]any{"authority": "tracker", "connection_id": "c", "native_id": native}})
	if err != nil {
		f.t.Fatal(err)
	}
	return anyString(card["ref"]), anyString(card["id"])
}

// observeDone records a tracker observation reporting the card done.
func (f *curationFixture) observeDone(id, native string) {
	f.t.Helper()
	obs := map[string]any{"idempotency_key": "obs-" + native, "reader_id": "adapter", "reader_revision": "1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "status": "reported", "facts": map[string]any{"phase": "done"}, "evidence": []any{map[string]any{"kind": "issue", "url": "https://tracker.test/" + native, "knowledge": "reported"}}}
	if _, err := f.store.SubmitWorkObservation(f.ctx, f.agent.ActorID, id, obs); err != nil {
		f.t.Fatal(err)
	}
}

// mergedPR makes a card carrying a merged PR source and returns the PR URL.
func (f *curationFixture) mergedPR(n string) string {
	f.t.Helper()
	prURL := "https://github.com/example/repo/pull/" + n
	f.call(http.MethodPost, "/work", f.agent.AccessToken, map[string]any{"actor_id": f.agent.ActorID, "title": "PR carrier " + n, "source_refs": []any{map[string]any{"authority": "github", "connection_id": "c", "native_id": n, "url": prURL, "status": "merged", "phase": "done"}}}, http.StatusCreated)
	return prURL
}

func (f *curationFixture) openCard(title string) string {
	f.t.Helper()
	out := f.call(http.MethodPost, "/work", f.agent.AccessToken, map[string]any{"actor_id": f.agent.ActorID, "title": title}, http.StatusCreated)
	return anyString(out["work"].(map[string]any)["ref"])
}

func (f *curationFixture) fileAsk(thread, title, subject string, related []string, proposals []string) string {
	f.t.Helper()
	refs := []string{"thread:" + thread, subject}
	for _, r := range related {
		if strings.Contains(r, "://") {
			r = "url:" + r
		}
		refs = append(refs, r)
	}
	props := []any{}
	for _, p := range proposals {
		props = append(props, p)
	}
	out := f.call(http.MethodPost, "/events", f.agent.AccessToken, map[string]any{"actor_id": f.agent.ActorID, "event": map[string]any{
		"type": "human_attention_requested", "thread_id": thread, "refs": refs, "summary": title,
		"payload": map[string]any{
			"kind": "ask", "title": title, "subject_ref": subject, "related_refs": related,
			"requester_actor_id": f.agent.ActorID, "response_proposals": props,
		},
		"provenance": map[string]any{"sources": []any{"inferred"}},
	}}, http.StatusCreated)
	return anyString(out["event"].(map[string]any)["id"])
}

func (f *curationFixture) candidates(token string) []map[string]any {
	f.t.Helper()
	out := f.call(http.MethodGet, "/curation/candidates", token, nil, http.StatusOK)
	list := []map[string]any{}
	for _, raw := range out["candidates"].([]any) {
		list = append(list, raw.(map[string]any))
	}
	return list
}

func candidatesFor(list []map[string]any, action, target string) []map[string]any {
	out := []map[string]any{}
	for _, c := range list {
		if c["action"] != action {
			continue
		}
		for _, tg := range c["targets"].([]any) {
			if tg == target {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

func candidatesByAction(list []map[string]any, action string) []map[string]any {
	out := []map[string]any{}
	for _, c := range list {
		if c["action"] == action {
			out = append(out, c)
		}
	}
	return out
}

// respond answers the open inbox item whose source is eventID as the human.
func (f *curationFixture) respond(thread, eventID, text, outcome string) {
	if rec, err := f.store.CurationProposalByEvent(f.ctx, eventID); err == nil {
		resolution := "apply"
		if text == "Keep" {
			resolution = "dismiss"
		}
		f.call(http.MethodPost, "/curation/proposals", f.agent.AccessToken, map[string]any{"action": rec.Action, "targets": rec.Targets, "resolution": resolution, "rationale": "Agent reviewed the current evidence."}, http.StatusCreated)
		return
	}
	f.respondOnce(thread, eventID, text, outcome, "")
}

// respondOnce is respond with a caller-chosen idempotency key so a replay
// can return the same answer.
func (f *curationFixture) respondOnce(thread, eventID, text, outcome, key string) {
	f.t.Helper()
	if err := refreshDerivedTopicProjection(f.ctx, handlerOptions{primitiveStore: f.env.primitiveStore}, thread, time.Now().UTC(), f.agent.ActorID); err != nil {
		f.t.Fatal(err)
	}
	items, err := f.env.primitiveStore.ListDerivedInboxItems(f.ctx, primitives.DerivedInboxListFilter{ThreadID: thread})
	if err != nil {
		f.t.Fatal(err)
	}
	for _, item := range items {
		if item.SourceEventID == eventID {
			body := map[string]any{"response_text": text, "outcome": outcome, "notify_mode": "none"}
			if key != "" {
				body["idempotency_key"] = key
			}
			f.call(http.MethodPost, "/inbox/"+url.PathEscape(item.ID)+"/respond", f.human.AccessToken, body, http.StatusCreated)
			return
		}
	}
	f.t.Fatalf("no inbox item for event %s among %d items", eventID, len(items))
}

func (f *curationFixture) proposalStatus(key string) string {
	f.t.Helper()
	var status string
	err := f.env.workspace.DB().QueryRowContext(f.ctx, `SELECT status FROM curation_proposals WHERE key=?`, key).Scan(&status)
	if err == sql.ErrNoRows {
		return ""
	}
	if err != nil {
		f.t.Fatal(err)
	}
	return status
}

func (f *curationFixture) trail(thread string) []map[string]any {
	f.t.Helper()
	rows, err := f.env.workspace.DB().QueryContext(f.ctx, `SELECT json_extract(payload_json,'$.payload.curation_applied') FROM events WHERE type='curation_receipt' AND thread_id=? AND json_extract(payload_json,'$.payload.curation_applied') IS NOT NULL`, thread)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			f.t.Fatal(err)
		}
		var m map[string]any
		if err = jsonUnmarshalString(raw, &m); err != nil {
			f.t.Fatal(err)
		}
		out = append(out, m)
	}
	return out
}

func requireNoLeak(t *testing.T, c map[string]any, forbidden ...string) {
	t.Helper()
	text := fmt.Sprint(c["title"], "\n", c["body"], "\n", c["rationale"])
	for _, bad := range forbidden {
		if bad != "" && strings.Contains(text, bad) {
			t.Fatalf("candidate text leaks %q: %s", bad, text)
		}
	}
}

func TestCurationMootWithdrawEndToEnd(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	f := newCurationFixture(t, "moot")
	doneRef, doneID := f.trackerCard("Landed work", "done-1")
	prURL := f.mergedPR("123")
	askID := f.fileAsk(f.thread, "Authorize the merge?", f.openCard("Decision subject 1"), []string{doneRef, prURL}, []string{"Yes", "No"})
	f.observeDone(doneID, "done-1") // the subject card finishes after the ask was filed

	list := f.candidates(f.human.AccessToken)
	got := candidatesFor(list, "withdraw", "event:"+askID)
	if len(got) != 1 {
		t.Fatalf("want one withdraw candidate, got %#v", list)
	}
	requireNoLeak(t, got[0], prURL, doneRef, strings.TrimPrefix(doneRef, "card:"), "github.com", "tracker.test")

	run := f.call(http.MethodPost, "/curation/run", f.human.AccessToken, map[string]any{}, http.StatusOK)
	proposals := run["proposals"].([]any)
	var proposal map[string]any
	for _, raw := range proposals {
		p := raw.(map[string]any)
		if p["action"] == "withdraw" {
			proposal = p
		}
	}
	if proposal == nil || proposal["status"] != "created" || anyString(proposal["event_id"]) == "" {
		t.Fatalf("run did not create the withdraw proposal: %#v", run)
	}
	again := f.call(http.MethodPost, "/curation/run", f.human.AccessToken, map[string]any{}, http.StatusOK)
	if len(again["proposals"].([]any)) != 0 || again["skipped"].(float64) < 1 {
		t.Fatalf("second run must skip the existing proposal: %#v", again)
	}

	f.respond(f.thread, anyString(proposal["event_id"]), "Withdraw", "answered")
	var summary string
	if err := f.env.workspace.DB().QueryRow(`SELECT json_extract(e.payload_json,'$.summary') FROM events e JOIN human_attention_request_resolutions r ON r.resolution_event_id=e.id WHERE r.request_event_id=?`, askID).Scan(&summary); err != nil {
		t.Fatal(err)
	}
	if summary != "Ask withdrawn after agent curation review" {
		t.Fatalf("stale withdrawal summary: %q", summary)
	}

	outcome := f.call(http.MethodGet, "/asks/"+askID, f.agent.AccessToken, nil, http.StatusOK)
	text := fmt.Sprint(outcome)
	if !strings.Contains(text, "withdrawn") || !strings.Contains(text, "moot") || !strings.Contains(text, f.agent.ActorID) {
		t.Fatalf("target ask not withdrawn by the agent as moot: %#v", outcome)
	}
	trail := f.trail(f.thread)
	if len(trail) != 1 || trail[0]["result"] != "applied" || trail[0]["applied_by"] != f.agent.ActorID {
		t.Fatalf("missing applied trail: %#v", trail)
	}
	if f.proposalStatus(proposal["key"].(string)) != "accepted" {
		t.Fatal("proposal row not accepted")
	}
}

func TestCurationAgentResolutionReplays(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	f := newCurationFixture(t, "replay")
	subject := f.openCard("Shared delivery subject")
	f.fileAsk(f.thread, "First", subject, nil, []string{"Yes", "No"})
	f.fileAsk(f.thread, "Second", subject, nil, []string{"Yes", "No"})
	run := f.call(http.MethodPost, "/curation/run", f.agent.AccessToken, map[string]any{}, http.StatusOK)
	p := run["proposals"].([]any)[0].(map[string]any)
	f.respond(f.thread, anyString(p["event_id"]), "Batch", "answered")
	f.respond(f.thread, anyString(p["event_id"]), "Batch", "answered")
	if got := f.trail(f.thread); len(got) != 1 {
		t.Fatalf("replay wrote multiple receipts: %#v", got)
	}
}

func TestCurationUnknownSourceIsNotMoot(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	f := newCurationFixture(t, "unknown")
	doneRef, doneID := f.trackerCard("Landed work", "done-1")
	askID := f.fileAsk(f.thread, "Authorize?", f.openCard("Decision subject 2"), []string{doneRef, "https://github.com/example/repo/pull/999"}, []string{"Yes", "No"})
	f.observeDone(doneID, "done-1")
	if got := candidatesFor(f.candidates(f.human.AccessToken), "withdraw", "event:"+askID); len(got) != 0 {
		t.Fatalf("an unobserved source must withhold the candidate: %#v", got)
	}
}

// A thread with a pm_actor_id is a private PM thread. Its ask is moot by the
// same evidence as a public one, but it is never a candidate, even for the
// PM that owns the thread.
func TestCurationExcludesPrivatePMThread(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	f := newCurationFixture(t, "private")
	doneRef, doneID := f.trackerCard("Landed work", "done-1")
	prURL := f.mergedPR("123")
	private := f.newThread("Private PM thread")
	if _, err := f.store.PatchThread(f.ctx, f.agent.ActorID, private, map[string]any{"pm_actor_id": f.agent.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	privateAsk := f.fileAsk(private, "Private authorize?", f.openCard("Decision subject 3"), []string{doneRef, prURL}, []string{"Yes", "No"})
	publicAsk := f.fileAsk(f.thread, "Public authorize?", f.openCard("Decision subject 4"), []string{doneRef, prURL}, []string{"Yes", "No"})
	f.observeDone(doneID, "done-1")
	for _, token := range []string{f.human.AccessToken} {
		list := f.candidates(token)
		if got := candidatesFor(list, "withdraw", "event:"+privateAsk); len(got) != 0 {
			t.Fatalf("private PM thread ask leaked as candidate: %#v", got)
		}
		if got := candidatesFor(list, "withdraw", "event:"+publicAsk); len(got) != 1 {
			t.Fatalf("control: public ask should be a candidate: %#v", list)
		}
		if strings.Contains(fmt.Sprint(list), "Private authorize") {
			t.Fatalf("private ask title appears in candidates: %#v", list)
		}
	}
	run := f.call(http.MethodPost, "/curation/run", f.human.AccessToken, map[string]any{}, http.StatusOK)
	if strings.Contains(fmt.Sprint(run), privateAsk) {
		t.Fatalf("run touched the private ask: %#v", run)
	}
}

func TestCurationDismissKeepsAskAndNeverRefiles(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	f := newCurationFixture(t, "dismiss")
	doneRef, doneID := f.trackerCard("Landed work", "done-1")
	prURL := f.mergedPR("123")
	askID := f.fileAsk(f.thread, "Authorize the merge?", f.openCard("Decision subject 5"), []string{doneRef, prURL}, []string{"Yes", "No"})
	f.observeDone(doneID, "done-1")
	run := f.call(http.MethodPost, "/curation/run", f.human.AccessToken, map[string]any{}, http.StatusOK)
	var proposalID, key string
	for _, raw := range run["proposals"].([]any) {
		if p := raw.(map[string]any); p["action"] == "withdraw" {
			proposalID, key = anyString(p["event_id"]), anyString(p["key"])
		}
	}
	if proposalID == "" {
		t.Fatalf("no proposal: %#v", run)
	}
	f.respond(f.thread, proposalID, "Keep", "answered")
	if got := f.proposalStatus(key); got != "dismissed" {
		t.Fatalf("proposal status = %q", got)
	}
	outcome := f.call(http.MethodGet, "/asks/"+askID, f.agent.AccessToken, nil, http.StatusOK)
	if outcome["status"] != "open" {
		t.Fatalf("target ask must stay open: %#v", outcome)
	}
	again := f.call(http.MethodPost, "/curation/run", f.human.AccessToken, map[string]any{}, http.StatusOK)
	for _, raw := range again["proposals"].([]any) {
		if raw.(map[string]any)["key"] == key {
			t.Fatalf("dismissed key re-proposed: %#v", again)
		}
	}
	if got := f.trail(f.thread); len(got) != 1 || got[0]["result"] != "dismissed" {
		t.Fatalf("missing dismissal receipt: %#v", got)
	}
}

func TestCurationMergeProposalAndAccept(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	f := newCurationFixture(t, "merge")
	older := f.openCard("Fix flaky BLE test")
	time.Sleep(5 * time.Millisecond)
	newer := f.openCard("Fix the flaky BLE test")
	merges := candidatesByAction(f.candidates(f.human.AccessToken), "merge")
	if len(merges) != 1 {
		t.Fatalf("want one merge candidate, got %#v", merges)
	}
	targets := merges[0]["targets"].([]any)
	if len(targets) != 2 {
		t.Fatalf("targets: %#v", targets)
	}

	f.call(http.MethodPost, "/curation/proposals", f.human.AccessToken, map[string]any{"action": "merge", "targets": []string{"card:nope-1", "card:nope-2"}, "rationale": "same thing"}, http.StatusNotFound)
	f.call(http.MethodPost, "/curation/proposals", f.human.AccessToken, map[string]any{"action": "merge", "targets": targets, "rationale": ""}, http.StatusBadRequest)
	created := f.call(http.MethodPost, "/curation/proposals", f.human.AccessToken, map[string]any{"action": "merge", "targets": targets, "rationale": "Same BLE flake, two cards.", "title": "Merge the BLE cards?"}, http.StatusCreated)
	p := created["proposal"].(map[string]any)
	if p["status"] != "created" || p["source"] != "pm" || anyString(p["event_id"]) == "" {
		t.Fatalf("proposal: %#v", p)
	}
	replay := f.call(http.MethodPost, "/curation/proposals", f.human.AccessToken, map[string]any{"action": "merge", "targets": targets, "rationale": "Same BLE flake, two cards."}, http.StatusCreated)
	if replay["proposal"].(map[string]any)["status"] == "created" {
		t.Fatalf("second file must not create another ask: %#v", replay)
	}

	// The proposal lives in the thread of the kept (subject) card.
	thread, err := f.store.CurationCardThread(f.ctx, anyString(merges[0]["targets"].([]any)[1]))
	if err != nil || thread == "" {
		t.Fatalf("kept card thread: %q %v", thread, err)
	}
	f.respond(thread, anyString(p["event_id"]), "Merge", "answered")

	trail := f.trail(thread)
	if len(trail) != 1 || trail[0]["result"] != "applied" {
		t.Fatalf("merge trail: %#v", trail)
	}
	undo, _ := trail[0]["undo"].(map[string]any)
	dup := anyString(targets[0])
	if undo == nil || undo["unfold"] != dup {
		t.Fatalf("undo must restore the duplicate %s: %#v", dup, undo)
	}
	if dup != older && dup != newer {
		t.Fatalf("duplicate %s is neither created card", dup)
	}
	if dup != newer {
		t.Fatalf("the older card should be kept, duplicate=%s older=%s newer=%s", dup, older, newer)
	}
	var archived sql.NullString
	if err = f.env.workspace.DB().QueryRow(`SELECT archived_at FROM cards WHERE id=?`, undo["card_id"]).Scan(&archived); err != nil || !archived.Valid || archived.String == "" {
		t.Fatalf("duplicate not archived: %v %v", archived, err)
	}
}

func TestCurationBatchCandidate(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	f := newCurationFixture(t, "batch")
	subject, _ := f.trackerCard("Shared subject", "open-1")
	a := f.fileAsk(f.thread, "First question", subject, nil, []string{"Yes", "No"})
	b := f.fileAsk(f.thread, "Second question", subject, nil, []string{"Yes", "No"})
	batches := candidatesByAction(f.candidates(f.human.AccessToken), "batch")
	if len(batches) != 1 {
		t.Fatalf("want one batch candidate: %#v", batches)
	}
	if got := fmt.Sprint(batches[0]["targets"]); !strings.Contains(got, a) || !strings.Contains(got, b) {
		t.Fatalf("batch targets %s lack %s/%s", got, a, b)
	}
}

func TestCurationMetricsAndWindowValidation(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	f := newCurationFixture(t, "metrics")
	recommended := f.fileAsk(f.thread, "Ship it?", "thread:"+f.thread, nil, []string{"Ship", "Wait"})
	context := f.fileAsk(f.thread, "Which one?", "thread:"+f.thread, nil, []string{"Alpha", "Beta"})
	f.respond(f.thread, recommended, "Ship", "answered")
	f.respond(f.thread, context, "Need more detail", "needs_context")

	out := f.call(http.MethodGet, "/curation/metrics?window_days=7", f.human.AccessToken, nil, http.StatusOK)
	s18 := out["metrics"].(map[string]any)["s18"].(map[string]any)
	if s18["answered"].(float64) < 2 {
		t.Fatalf("s18: %#v", s18)
	}
	if _, ok := s18["answered_recommended_pct"].(float64); !ok {
		t.Fatalf("answered_recommended_pct not a number: %#v", s18)
	}
	if _, ok := s18["needs_context_pct"].(float64); !ok {
		t.Fatalf("needs_context_pct not a number: %#v", s18)
	}
	// S20 reads the materialized promise fold: one open miss and one promise
	// later cleared by evidence (a late miss). Both fell due inside the window.
	dueAt := time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339)
	note := func(card map[string]any, payload map[string]any) {
		payload["text"] = "progress"
		if _, err := f.store.AppendEvent(f.ctx, f.agent.ActorID, map[string]any{"type": "message_posted", "thread_id": card["thread_id"], "refs": []string{anyString(card["ref"]), "thread:" + anyString(card["thread_id"])}, "payload": payload}); err != nil {
			t.Fatal(err)
		}
	}
	work := func(title string) map[string]any {
		w, err := f.store.CreateWork(f.ctx, f.agent.ActorID, f.boardID, map[string]any{"title": title})
		if err != nil {
			t.Fatal(err)
		}
		return w
	}
	open, cleared := work("Open miss"), work("Late evidence")
	note(open, map[string]any{"next": map[string]any{"at": dueAt, "what": "ship"}})
	note(cleared, map[string]any{"next": map[string]any{"at": dueAt, "what": "ship"}})
	note(cleared, map[string]any{"evidence_refs": []string{"https://example.test/pr/1"}})
	for drained := false; !drained; {
		var err error
		if drained, err = f.store.RefreshBriefInitiatives(f.ctx, 200); err != nil {
			t.Fatal(err)
		}
	}
	out = f.call(http.MethodGet, "/curation/metrics?window_days=7", f.human.AccessToken, nil, http.StatusOK)
	s20 := out["metrics"].(map[string]any)["s20"].(map[string]any)
	if s20["promises"].(float64) != 2 || s20["due"].(float64) != 2 || s20["met_pct"].(float64) != 0 {
		t.Fatalf("s20: %#v", s20)
	}
	if slip, ok := s20["median_slip_hours"].(float64); !ok || slip < 1.9 {
		t.Fatalf("median_slip_hours: %#v", s20)
	}
	f.call(http.MethodGet, "/curation/metrics", f.human.AccessToken, nil, http.StatusOK)
	f.call(http.MethodGet, "/curation/metrics?window_days=0", f.human.AccessToken, nil, http.StatusBadRequest)
	f.call(http.MethodGet, "/curation/metrics?window_days=91", f.human.AccessToken, nil, http.StatusBadRequest)
}

func jsonUnmarshalString(raw string, v any) error { return json.Unmarshal([]byte(raw), v) }

func (f *curationFixture) briefDecisions(token string) []map[string]any {
	f.t.Helper()
	out := f.call(http.MethodGet, "/brief", token, nil, http.StatusOK)
	list := []map[string]any{}
	for _, raw := range out["decisions"].([]any) {
		list = append(list, raw.(map[string]any))
	}
	return list
}

func TestCurationBatchAcceptCollapsesBriefDecisions(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	f := newCurationFixture(t, "batchbrief")
	subject, _ := f.trackerCard("Shared subject", "open-1")
	a := f.fileAsk(f.thread, "First question", subject, nil, []string{"Yes", "No"})
	b := f.fileAsk(f.thread, "Second question", subject, nil, []string{"Yes", "No"})
	if got := len(f.briefDecisions(f.human.AccessToken)); got != 2 {
		t.Fatalf("before batching want 2 decisions, got %d", got)
	}
	batches := candidatesByAction(f.candidates(f.human.AccessToken), "batch")
	if len(batches) != 1 {
		t.Fatalf("want one batch candidate: %#v", batches)
	}
	created := f.call(http.MethodPost, "/curation/proposals", f.agent.AccessToken, map[string]any{"action": "batch", "targets": batches[0]["targets"], "rationale": "Same subject; decide together."}, http.StatusCreated)
	p := created["proposal"].(map[string]any)
	decisions := f.briefDecisions(f.human.AccessToken)
	if len(decisions) != 2 {
		t.Fatalf("suggestions must not become decisions: %#v", decisions)
	}

	f.respond(f.thread, anyString(p["event_id"]), "Batch", "answered")
	decisions = f.briefDecisions(f.human.AccessToken)
	if len(decisions) != 1 {
		t.Fatalf("accepted batch must collapse into one decision: %#v", decisions)
	}
	batch, _ := decisions[0]["batch"].(map[string]any)
	if batch == nil || batch["count"] != float64(2) || batch["key"] != p["key"] {
		t.Fatalf("batch field: %#v", decisions[0])
	}
	members := fmt.Sprint(batch["members"])
	if !strings.Contains(members, "First question") || !strings.Contains(members, "Second question") {
		t.Fatalf("members: %s (asks %s %s)", members, a, b)
	}
	trail := f.trail(f.thread)
	if len(trail) != 1 || trail[0]["result"] != "applied" || trail[0]["undo"].(map[string]any)["ungroup"] != p["key"] {
		t.Fatalf("batch trail: %#v", trail)
	}
	if out := f.call(http.MethodPost, "/curation/ungroup", f.human.AccessToken, map[string]any{"key": p["key"]}, http.StatusOK); out["ungrouped"] != true {
		t.Fatalf("ungroup: %#v", out)
	}
	if got := len(f.briefDecisions(f.human.AccessToken)); got != 2 {
		t.Fatalf("undo must restore separate decisions, got %d", got)
	}
}

func TestCurationProposalHiddenFromReaderOfUnreadableTarget(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	f := newCurationFixture(t, "proposalprivacy")
	owner := seedHumanPrincipalForLockoutTest(t, f.ctx, f.env.workspace.DB(), "pp-owner", "pp-owner-actor", "pp-owner", "pp-owner-token")
	secret := f.openCard("Secret BLE launch plan")
	public := f.openCard("Secret BLE launch plan, public copy")
	secretThread, err := f.store.CurationCardThread(f.ctx, secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.store.PatchThread(f.ctx, owner.ActorID, secretThread, map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	// File directly, as a PM that can read both cards would: the read side
	// must still withhold the proposal from a reader of only one of them.
	c := curation.Candidate{Action: curation.ActionMerge, Targets: []string{secret, public}, Title: "Merge the BLE cards?", Body: "Duplicate initiative.", Rationale: "Duplicate initiative.", SubjectRef: public, ResponseProposals: []string{"Merge", "Keep both"}}
	c.Key = curation.Key(c.Action, c.Targets)
	if _, created, err := fileCurationProposal(f.ctx, f.store, actors.SystemActorID, c, curationSourcePM); err != nil || !created {
		t.Fatalf("file: %v %v", created, err)
	}
	for _, d := range f.briefDecisions(f.human.AccessToken) {
		if d["kind"] == "pm_proposal" || strings.Contains(fmt.Sprint(d), secret) {
			t.Fatalf("proposal naming an unreadable card reached the reader: %#v", d)
		}
	}
	for _, token := range []string{f.human.AccessToken, f.agent.AccessToken} {
		quiet := f.call(http.MethodGet, "/curation/candidates", token, nil, http.StatusOK)
		if len(quiet["suggestions"].([]any)) != 0 {
			t.Fatalf("unreadable target leaked through quiet API: %#v", quiet)
		}
	}
	f.call(http.MethodPost, "/curation/proposals", f.agent.AccessToken, map[string]any{"action": "merge", "targets": c.Targets, "resolution": "apply", "rationale": "Cannot act on hidden targets."}, http.StatusNotFound)
	out := f.call(http.MethodGet, "/curation/candidates", owner.AccessToken, nil, http.StatusOK)
	if len(out["suggestions"].([]any)) != 1 {
		t.Fatalf("owner must see quiet suggestion: %#v", out)
	}
}
