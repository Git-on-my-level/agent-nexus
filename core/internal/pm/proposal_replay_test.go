package pm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestProposalOriginalHTTPReceiptCommitsSupersedingOriginAndReplay(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	origin := Origin{Transport: "telegram", TenantID: "bot", ChannelID: "channel", ExternalUserID: "reader"}
	bind(t, s, p, origin, true)
	old, err := s.ProposeDecision(ctx, p, DecisionInput{RequestKey: "old", WorkRef: "work:1", Scope: "work.phase", Instruction: "Move to ready", Payload: &ActionPayload{Phase: "ready"}, TargetRevision: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	// All dependency reads deliberately use the raw single-connection pool.
	// Calling any of these while holding the PM writer would fail the deadline.
	rawRead := func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		var one int
		return st.db.QueryRowContext(ctx, "SELECT 1").Scan(&one)
	}
	s.deps.Authorize = func(ctx context.Context, _ Principal, _, _ string) error { return rawRead(ctx) }
	s.deps.DecisionWork = func(ctx context.Context, _ Principal, _ string) (DecisionWork, error) {
		return DecisionWork{SourceAuthority: "nexus", Revision: "r1", Phase: "backlog"}, rawRead(ctx)
	}
	s.deps.DeliveryPath = func(ctx context.Context, _ Action) (string, error) { return "core", rawRead(ctx) }
	in := DecisionInput{ActorID: p.ActorID, ReplayKey: "original-http", RequestKey: "next", WorkRef: "work:1", Scope: "work.phase", Instruction: "Move to review", Payload: &ActionPayload{Phase: "review"}, TargetRevision: "r1", Origin: &origin}
	h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return p, nil }}
	send := func(in DecisionInput) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(in)
		out := httptest.NewRecorder()
		h.ServeHTTP(out, httptest.NewRequest("POST", "/pm/decisions", strings.NewReader(string(raw))))
		return out
	}
	baseline := func() string {
		var oldRaw string
		if err := st.db.QueryRow(`SELECT body FROM pm_records WHERE kind='decision' AND id=?`, old.ID).Scan(&oldRaw); err != nil {
			t.Fatal(err)
		}
		var decisions, deliveries, replays int
		for table, count := range map[string]*int{"decision": &decisions, "delivery": &deliveries} {
			if err := st.db.QueryRow(`SELECT count(*) FROM pm_records WHERE kind=?`, table).Scan(count); err != nil {
				t.Fatal(err)
			}
		}
		if err := st.db.QueryRow(`SELECT count(*) FROM idempotency_replays`).Scan(&replays); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal([]any{oldRaw, decisions, deliveries, replays})
		return string(raw)
	}
	before := baseline()
	for _, trigger := range []string{
		`CREATE TRIGGER reject_proposal BEFORE INSERT ON idempotency_replays WHEN NEW.scope LIKE 'pm.decisions.create:%' BEGIN SELECT RAISE(ABORT,'receipt failed'); END`,
		`CREATE TRIGGER reject_proposal BEFORE INSERT ON pm_records WHEN NEW.kind='delivery' BEGIN SELECT RAISE(ABORT,'origin delivery failed'); END`,
		`CREATE TRIGGER reject_proposal BEFORE INSERT ON pm_records WHEN NEW.kind='decision' BEGIN SELECT RAISE(ABORT,'decision failed'); END`,
	} {
		if _, err := st.db.Exec(trigger); err != nil {
			t.Fatal(err)
		}
		out := send(in)
		if out.Code < 400 {
			t.Fatal("partial proposal confirmed", out.Code, out.Body.String())
		}
		if after := baseline(); after != before {
			t.Fatal("proposal/superseding/delivery survived rollback", before, after)
		}
		if _, err := st.db.Exec("DROP TRIGGER reject_proposal"); err != nil {
			t.Fatal(err)
		}
	}
	start := make(chan struct{})
	results := make(chan *httptest.ResponseRecorder, 8)
	for i := 0; i < 8; i++ {
		go func() { <-start; results <- send(in) }()
	}
	close(start)
	first := <-results
	if first.Code != 201 {
		t.Fatal(first.Code, first.Body.String())
	}
	for i := 1; i < 8; i++ {
		if next := <-results; next.Code != first.Code || next.Body.String() != first.Body.String() {
			t.Fatal("receipt changed", next.Code, next.Body.String(), first.Body.String())
		}
	}
	var received map[string]any
	if err := json.Unmarshal(first.Body.Bytes(), &received); err != nil {
		t.Fatal(err)
	}
	if received["deliverable"] != true || received["delivery_path"] != "core" || received["supersedes"] != old.ID || received["target_current"] != true {
		t.Fatal("preflight projection lost", received)
	}
	var count int
	if err := st.db.QueryRow(`SELECT count(*) FROM pm_records WHERE kind='delivery'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate origin delivery", count, err)
	}
	if err := st.db.QueryRow(`SELECT count(*) FROM idempotency_replays WHERE scope='pm.decisions.create:ws'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate receipt", count, err)
	}
	var prior Decision
	if err := st.get(ctx, "decision", old.ID, &prior); err != nil || prior.Status != Superseded || prior.Revision != old.Revision+1 {
		t.Fatal("superseding was not atomic", prior, err)
	}
	id := received["id"].(string)
	if _, err := s.AnswerDecision(ctx, p, id, AnswerInput{Revision: 1, Approve: false, Text: "No"}); err != nil {
		t.Fatal(err)
	}
	s.deps.DecisionWork = func(context.Context, Principal, string) (DecisionWork, error) {
		return DecisionWork{SourceAuthority: "nexus", Revision: "newer", Phase: "review"}, nil
	}
	// Legacy callers still receive current terminal state and status 200.
	legacy := in
	legacy.ReplayKey = ""
	out := send(legacy)
	if out.Code != 200 {
		t.Fatal("legacy replay changed", out.Code, out.Body.String())
	}
	var current map[string]any
	json.Unmarshal(out.Body.Bytes(), &current)
	if current["replayed"] != true || current["status"] != "declined" {
		t.Fatal("legacy terminal state lost", current)
	}
	if _, err := s.Disconnect(ctx, Principal{WorkspaceID: p.WorkspaceID, ActorID: s.AgentActorID()}); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewService(st, s.cfg, s.deps)
	if err != nil {
		t.Fatal(err)
	}
	h.Service = restarted
	if next := send(in); next.Code != 201 || next.Body.String() != first.Body.String() {
		t.Fatal("restart/disconnect replay changed", next.Code, next.Body.String())
	}
	if out := send(legacy); out.Code != 409 || !strings.Contains(out.Body.String(), "pm_not_onboarded") {
		t.Fatal("legacy proposal bypassed setup", out.Code, out.Body.String())
	}
	for _, changed := range []DecisionInput{
		func() DecisionInput { v := in; v.TargetRevision = "newer"; return v }(),
		func() DecisionInput { v := in; v.WorkRef = "work:other"; return v }(),
		func() DecisionInput { v := in; v.RequestKey = "other-request"; return v }(),
		func() DecisionInput {
			v := in
			v.Payload = &ActionPayload{Phase: "done", ResolutionRefs: []string{"artifact:proof"}}
			return v
		}(),
	} {
		if out := send(changed); out.Code != 409 {
			t.Fatal("changed replay accepted", out.Code, out.Body.String())
		}
	}
	changedActor := in
	changedActor.ActorID = "other"
	if out := send(changedActor); out.Code != 403 {
		t.Fatal("changed actor accepted", out.Code, out.Body.String())
	}
	for _, key := range []string{strings.Repeat("x", 257), "bad\x00key"} {
		v := in
		v.ReplayKey = key
		if out := send(v); out.Code != 400 {
			t.Fatal("invalid key accepted", out.Code, out.Body.String())
		}
	}
	var binding Binding
	if err := st.get(ctx, "binding", bindingID(origin), &binding); err != nil {
		t.Fatal(err)
	}
	binding.Enabled = false
	binding.Revision++
	if err := st.cas(ctx, "binding", binding.ID, binding.Revision-1, binding); err != nil {
		t.Fatal(err)
	}
	if out := send(in); out.Code != 403 {
		t.Fatal("revoked origin replay accepted", out.Code, out.Body.String())
	}
	binding.Enabled = true
	binding.Revision++
	if err := st.cas(ctx, "binding", binding.ID, binding.Revision-1, binding); err != nil {
		t.Fatal(err)
	}
	restarted.deps.Authorize = func(context.Context, Principal, string, string) error { return ErrForbidden }
	if out := send(in); out.Code != 403 {
		t.Fatal("revoked replay accepted", out.Code, out.Body.String())
	}
}

func TestProposalReceiptFreezesLegacyReuseAndKeepsOnboardingGate(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	in := DecisionInput{RequestKey: "same", WorkRef: "work:1", Scope: "assignment", Instruction: "Assign", TargetRevision: "r1"}
	initial, err := s.ProposeDecision(ctx, p, in)
	if err != nil {
		t.Fatal(err)
	}
	in.ReplayKey = "reuse"
	receipt, err := s.ProposeDecision(ctx, p, in)
	if err != nil || receipt.ProposalReplayStatus != 200 || receipt.ID != initial.ID {
		t.Fatal(receipt, err)
	}
	if _, err := s.AnswerDecision(ctx, p, initial.ID, AnswerInput{Revision: 1, Approve: false, Text: "No"}); err != nil {
		t.Fatal(err)
	}
	next, err := s.ProposeDecision(ctx, p, in)
	if err != nil || !reflect.DeepEqual(next, receipt) {
		t.Fatal("reused receipt changed", next, receipt, err)
	}
	if _, err := st.db.Exec(`DELETE FROM pm_presence;DELETE FROM pm_onboarding_backfill;DELETE FROM pm_registration`); err != nil {
		t.Fatal(err)
	}
	if next, err := s.ProposeDecision(ctx, p, in); err != nil || !reflect.DeepEqual(next, receipt) {
		t.Fatal("existing receipt depends on onboarding", next, err)
	}
	in.RequestKey = "fresh"
	in.ReplayKey = "fresh"
	if _, err := s.ProposeDecision(ctx, p, in); !errors.Is(err, ErrNotOnboarded) {
		t.Fatal("new proposal bypassed setup", err)
	}
}
