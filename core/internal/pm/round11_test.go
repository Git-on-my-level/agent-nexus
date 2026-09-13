package pm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func round11Request(t *testing.T, h Handler, method, path string, in any, status int) map[string]any {
	t.Helper()
	raw, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(method, path, strings.NewReader(string(raw))))
	if rr.Code != status {
		t.Fatalf("%s %s: %d %s", method, path, rr.Code, rr.Body)
	}
	var out map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRound11DecisionReplayHTTP(t *testing.T) {
	for _, status := range []Status{AwaitingAnswer, Answered, Declined, Acknowledged, Superseded} {
		t.Run(string(status), func(t *testing.T) {
			s, st, p, _ := fixture(t)
			ctx := context.Background()
			h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return p, nil }}
			in := DecisionInput{RequestKey: "r11", WorkRef: "work:1", Scope: "work.phase", Payload: &ActionPayload{Phase: "ready"}, Instruction: "Ready", TargetRevision: "r1"}
			first := round11Request(t, h, "POST", "/pm/decisions", in, 201)
			id := first["id"].(string)
			if first["replayed"] != nil || first["replayed_terminal_status"] != nil {
				t.Fatal(first)
			}
			if status == Answered || status == Declined {
				if _, err := s.AnswerDecision(ctx, p, id, AnswerInput{Revision: 1, Approve: status == Answered, Text: "answer"}); err != nil {
					t.Fatal(err)
				}
			} else if status != AwaitingAnswer {
				// Historical acknowledged decisions and superseded records must also replay truthfully.
				var d Decision
				if err := st.get(ctx, "decision", id, &d); err != nil {
					t.Fatal(err)
				}
				d.Status = status
				if status == Superseded {
					d.SupersededBy = "replacement"
				}
				d.Revision++
				if err := st.cas(ctx, "decision", id, d.Revision-1, d); err != nil {
					t.Fatal(err)
				}
			}
			snapshot := func() string {
				t.Helper()
				var raw string
				if err := st.db.QueryRow("SELECT body FROM pm_records WHERE kind='decision' AND id=?", id).Scan(&raw); err != nil {
					t.Fatal(err)
				}
				return raw
			}
			before := snapshot()
			restarted, err := NewService(st, s.cfg, s.deps)
			if err != nil {
				t.Fatal(err)
			}
			h.Service = restarted
			for i := 0; i < 2; i++ {
				replay := round11Request(t, h, "POST", "/pm/decisions", in, 200)
				if replay["id"] != id || replay["replayed"] != true || replay["status"] != string(status) || replay["can_answer"] != (status == AwaitingAnswer) {
					t.Fatal(replay)
				}
				want := any(nil)
				if status != AwaitingAnswer {
					want = string(status)
				}
				if replay["replayed_terminal_status"] != want {
					t.Fatal(replay)
				}
			}
			for _, path := range []string{"/pm/decisions/" + id, "/pm/decisions"} {
				read := round11Request(t, h, "GET", path, nil, 200)
				raw, _ := json.Marshal(read)
				if strings.Contains(string(raw), "replayed") {
					t.Fatalf("replay flags leaked to ordinary read: %s", raw)
				}
			}
			if snapshot() != before {
				t.Fatal("replay/read mutated decision")
			}
			for _, field := range []string{"replayed", "deliverable", "delivery_path"} {
				if strings.Contains(before, field) {
					t.Fatalf("derived field persisted: %s", before)
				}
			}
			decisions, err := s.ListDecisions(ctx, p)
			if err != nil || len(decisions) != 1 {
				t.Fatalf("%+v %v", decisions, err)
			}
			in.Instruction = "changed"
			conflict := round11Request(t, h, "POST", "/pm/decisions", in, 409)
			if conflict["error"].(map[string]any)["details"].(map[string]any)["existing_decision_id"] != id {
				t.Fatal(conflict)
			}
		})
	}
}

func TestRound11LeaseErrorsAndProposalValidation(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "c11", Title: "Lease"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "m11", Text: "propose"})
	if err != nil {
		t.Fatal(err)
	}
	agent := Principal{WorkspaceID: p.WorkspaceID, ActorID: s.cfg.AgentActorID}
	claimed := claimTestTurn(t, s, ctx, agent, turn.ID)
	caller := agent
	h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return caller, nil }}
	for _, endpoint := range []string{"complete", "fail", "context", "decisions"} {
		for _, token := range []string{"", "  ", "wrong"} {
			body := map[string]any{"lease_token": token}
			switch endpoint {
			case "complete":
				body["text"] = "done"
			case "fail":
				body["reason"] = "failed"
			case "decisions":
				body["request_key"] = "r11"
				body["work_ref"] = "work:1"
				body["scope"] = "assignment"
				body["instruction"] = "assign"
				body["target_revision"] = "r1"
			}
			out := round11Request(t, h, "POST", "/pm/turns/"+turn.ID+"/"+endpoint, body, 409)["error"].(map[string]any)
			code, message := "lease_required", "this turn's current lease token is required"
			if token == "wrong" {
				code, message = "lease_mismatch", "the lease was released or re-claimed; claim the turn again"
			}
			if out["code"] != code || !strings.Contains(out["message"].(string), message) {
				t.Fatal(out)
			}
		}
	}
	var after Turn
	if err := st.get(ctx, "turn", turn.ID, &after); err != nil || !reflect.DeepEqual(after, claimed) {
		t.Fatalf("invalid leases mutated turn: %+v %v", after, err)
	}
	for _, token := range []string{claimed.LeaseToken, "wrong"} {
		expired := claimed
		expired.LeaseExpiresAt = time.Now().Add(-time.Second)
		if !errors.Is(leaseGuard(expired, token), ErrLeaseMismatch) {
			t.Fatal("expired lease accepted")
		}
	}
	in := DecisionInput{RequestKey: "valid", WorkRef: "work:1", Scope: "work.phase", Instruction: "ready", TargetRevision: "r1", Payload: &ActionPayload{Phase: "ready"}}
	for _, viaTurn := range []bool{false, true} {
		path := "/pm/decisions"
		caller = p
		for _, payload := range []*ActionPayload{nil, {Phase: "bogus"}, {Phase: "done"}, {Phase: "ready", ResolutionRefs: []string{"card:1"}}} {
			in.Payload = payload
			var body any = in
			if viaTurn {
				caller = agent
				path = "/pm/turns/" + turn.ID + "/decisions"
				body = TurnProposeInput{DecisionInput: in, LeaseToken: claimed.LeaseToken}
			}
			out := round11Request(t, h, "POST", path, body, 400)["error"].(map[string]any)
			if out["code"] != "invalid_request" || !strings.Contains(out["message"].(string), invalidActionPayloadMessage) {
				t.Fatal(out)
			}
		}
	}
	ds, err := s.ListDecisions(ctx, p)
	if err != nil || len(ds) != 0 {
		t.Fatalf("invalid proposals persisted: %+v %v", ds, err)
	}
	in.Payload = &ActionPayload{Phase: "ready"}
	caller = agent
	path := "/pm/turns/" + turn.ID + "/decisions"
	body := TurnProposeInput{DecisionInput: in, LeaseToken: claimed.LeaseToken}
	first := round11Request(t, h, "POST", path, body, 200)
	replay := round11Request(t, h, "POST", path, body, 200)
	if first["replayed"] != nil || replay["replayed"] != true || first["id"] != replay["id"] {
		t.Fatalf("%v %v", first, replay)
	}
}

func TestRound11DecisionAvailabilityIsLive(t *testing.T) {
	s, _, p, _ := fixture(t)
	route := "github"
	s.deps.DeliveryPath = func(_ context.Context, a Action) (string, error) {
		if a.WorkRef != "work:1" || a.Scope != "assignment" || a.ActorID != p.ActorID {
			t.Fatalf("bad preflight: %+v", a)
		}
		if route == "none" {
			return "none", NoDeliveryPath("GitHub")
		}
		return route, nil
	}
	s.deps.CheckDelivery = func(context.Context, Action) error { t.Fatal("legacy preflight used instead of registry"); return nil }
	h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return p, nil }}
	in := DecisionInput{RequestKey: "availability", WorkRef: "work:1", Scope: "assignment", Instruction: "assign", TargetRevision: "r1"}
	d := round11Request(t, h, "POST", "/pm/decisions", in, 201)
	id := d["id"].(string)
	if d["deliverable"] != true || d["delivery_path"] != "github" {
		t.Fatal(d)
	}
	for _, r := range []string{"none", "nexus", "github"} {
		route = r
		for _, path := range []string{"/pm/decisions/" + id, "/pm/decisions"} {
			out := round11Request(t, h, "GET", path, nil, 200)
			if path == "/pm/decisions" {
				out = out["items"].([]any)[0].(map[string]any)
			}
			if out["deliverable"] != (route != "none") || out["delivery_path"] != route {
				t.Fatal(out)
			}
		}
	}
	route = "none"
	answered := round11Request(t, h, "POST", "/pm/decisions/"+id+"/answer", AnswerInput{Revision: 1, Approve: true, Text: "yes"}, 200)
	if answered["deliverable"] != false || answered["delivery_path"] != "none" {
		t.Fatal(answered)
	}
	actionID := answered["action_id"].(string)
	round11Request(t, h, "POST", "/pm/decisions/"+id+"/dispatch", struct{}{}, 503)
	closed := round11Request(t, h, "POST", "/pm/actions/"+actionID+"/acknowledge", struct{}{}, 200)
	want := "Closed without delivery: no delivery path is configured for GitHub; nothing was sent."
	if closed["closed_without_delivery"] != true || closed["receipt"].(map[string]any)["detail"] != want {
		t.Fatal(closed)
	}
	route = "github"
	closed = round11Request(t, h, "GET", "/pm/actions/"+actionID, nil, 200)
	if closed["closed_without_delivery"] != true || closed["deliverable"] != false {
		t.Fatal(closed)
	}
}

func TestRound11PendingClosureDoesNotInventNonDelivery(t *testing.T) {
	for _, sent := range []bool{false, true} {
		s, st, p, _ := fixture(t)
		ctx := context.Background()
		d := round4Approved(t, s, p)
		var a Action
		if err := st.get(ctx, "action", d.ActionID, &a); err != nil {
			t.Fatal(err)
		}
		if sent {
			now := time.Now().UTC()
			a.Attempts = []Attempt{{SentAt: &now, Status: Unknown}}
			a.Revision++
			if err := st.cas(ctx, "action", a.ID, a.Revision-1, a); err != nil {
				t.Fatal(err)
			}
			s.deps.CheckDelivery = func(context.Context, Action) error { return NoDeliveryPath("GitHub") }
		} else {
			s.deps.CheckDelivery = func(context.Context, Action) error { return context.DeadlineExceeded }
		}
		if _, err := s.AcknowledgeAction(ctx, p, a.ID); err == nil {
			t.Fatal("invented closure")
		}
		var after Action
		if err := st.get(ctx, "action", a.ID, &after); err != nil || !reflect.DeepEqual(a, after) {
			t.Fatalf("mutated: %+v %v", after, err)
		}
	}
}

func TestRound11GeneratedLeaseErrors(t *testing.T) {
	for _, file := range []string{"contracts/gen/meta/commands.json", "contracts/gen/meta/help.json", "cli/internal/registry/commands.json", "cli/internal/registry/help.json"} {
		raw, err := os.ReadFile(filepath.Join("../../..", file))
		if err != nil {
			t.Fatal(err)
		}
		var registry struct {
			Commands []struct {
				ID     string   `json:"command_id"`
				Errors []string `json:"error_codes"`
			}
		}
		if err := json.Unmarshal(raw, &registry); err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, command := range registry.Commands {
			switch command.ID {
			case "pm.turns.complete", "pm.turns.fail", "pm.turns.context", "pm.turns.decisions.create", "pm.turns.release":
				found++
				codes := map[string]bool{}
				for _, code := range command.Errors {
					codes[code] = true
				}
				required := "lease_required"
				if command.ID == "pm.turns.release" {
					required = "turn_not_claimed"
				}
				if !codes[required] || !codes["lease_mismatch"] || !codes["turn_closed"] {
					t.Fatalf("%s %+v", file, command)
				}
			}
		}
		if found != 5 {
			t.Fatalf("%s: matched %d commands", file, found)
		}
	}
}

func TestRound11LegacyInvalidApprovalExplainsPayload(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	d, err := s.ProposeDecision(ctx, p, DecisionInput{RequestKey: "legacy", WorkRef: "work:1", Scope: "work.phase", Payload: &ActionPayload{Phase: "ready"}, Instruction: "ready", TargetRevision: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	d.Payload.ResolutionRefs = []string{"card:1"}
	d.Revision++
	if err := st.cas(ctx, "decision", d.ID, d.Revision-1, d); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AnswerDecision(ctx, p, d.ID, AnswerInput{Revision: d.Revision, Approve: true, Text: "yes"}); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), invalidActionPayloadMessage) {
		t.Fatal(err)
	}
	var after Decision
	if err := st.get(ctx, "decision", d.ID, &after); err != nil || after.ActionID != "" || after.Status != AwaitingAnswer || after.Revision != d.Revision {
		t.Fatalf("%+v %v", after, err)
	}
}
