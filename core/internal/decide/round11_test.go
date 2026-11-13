package decide

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
