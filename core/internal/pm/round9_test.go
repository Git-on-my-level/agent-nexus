package pm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func claimTestTurn(t *testing.T, s *Service, ctx context.Context, p Principal, id string) Turn {
	t.Helper()
	claimed, err := s.ClaimTurn(ctx, p, ClaimInput{RunnerID: id})
	if err != nil || claimed.ID != id {
		t.Fatalf("claim %s: %+v %v", id, claimed, err)
	}
	return claimed
}

func claimAndComplete(t *testing.T, s *Service, ctx context.Context, p Principal, id, text string, refs []string) (Turn, error) {
	t.Helper()
	claimed := claimTestTurn(t, s, ctx, p, id)
	return s.CompleteTurnWithLease(ctx, p, id, text, refs, claimed.LeaseToken)
}

func TestRound9UnclaimedTurnOperations(t *testing.T) {
	for _, state := range []string{"new", "pending", "released", "expired_lease"} {
		for _, endpoint := range []string{"complete", "fail", "decisions", "context"} {
			t.Run(state+"/"+endpoint, func(t *testing.T) {
				s, st, p, _ := fixture(t)
				ctx := context.Background()
				c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "c", Title: "Lease"})
				if err != nil {
					t.Fatal(err)
				}
				turn, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "m", Text: "work"})
				if err != nil {
					t.Fatal(err)
				}
				agent := Principal{WorkspaceID: p.WorkspaceID, ActorID: s.cfg.AgentActorID}
				token := "stale"
				if state == "pending" {
					turn.Status = Pending
					turn.Revision++
					if err := st.cas(ctx, "turn", turn.ID, turn.Revision-1, turn); err != nil {
						t.Fatal(err)
					}
				}
				if state != "new" && state != "pending" {
					claimed := claimTestTurn(t, s, ctx, agent, turn.ID)
					token = claimed.LeaseToken
					if state == "released" {
						if _, err := s.ReleaseTurn(ctx, agent, turn.ID, ReleaseInput{RunnerID: turn.ID, LeaseToken: token}); err != nil {
							t.Fatal(err)
						}
					} else {
						claimed.LeaseExpiresAt = time.Now().Add(-time.Second)
						claimed.Revision++
						if err := st.cas(ctx, "turn", turn.ID, claimed.Revision-1, claimed); err != nil {
							t.Fatal(err)
						}
					}
				}
				var before Turn
				if err := st.get(ctx, "turn", turn.ID, &before); err != nil {
					t.Fatal(err)
				}
				h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return agent, nil }}
				call := func(token string) *httptest.ResponseRecorder {
					t.Helper()
					method, path := "POST", "/pm/turns/"+turn.ID+"/"+endpoint
					body := fmt.Sprintf(`{"text":"done","lease_token":%q}`, token)
					switch endpoint {
					case "fail":
						body = fmt.Sprintf(`{"reason":"failed","lease_token":%q}`, token)
					case "decisions":
						body = fmt.Sprintf(`{"request_key":"proposal","work_ref":"work:1","scope":"assignment","instruction":"assign","target_revision":"r1","lease_token":%q}`, token)
					case "context":
						body = fmt.Sprintf(`{"lease_token":%q}`, token)
					}
					rr := httptest.NewRecorder()
					h.ServeHTTP(rr, httptest.NewRequest(method, path, strings.NewReader(body)))
					return rr
				}
				for _, supplied := range []string{"", token} {
					rr := call(supplied)
					if rr.Code != 409 || !strings.Contains(rr.Body.String(), "this turn is not claimed; claim it first") {
						t.Fatalf("unclaimed %d %s", rr.Code, rr.Body)
					}
				}
				var after Turn
				if err := st.get(ctx, "turn", turn.ID, &after); err != nil || !reflect.DeepEqual(before, after) {
					t.Fatalf("mutated unclaimed turn: %+v %v", after, err)
				}
				ds, err := s.ListDecisions(ctx, p)
				if err != nil || len(ds) != 0 {
					t.Fatalf("proposal leaked: %+v %v", ds, err)
				}
				if state == "pending" {
					// Dispatch admission finishes before this turn becomes claimable.
					after.Status = Sending
					after.Revision++
					if err := st.cas(ctx, "turn", turn.ID, after.Revision-1, after); err != nil {
						t.Fatal(err)
					}
				}
				claimed := claimTestTurn(t, s, ctx, agent, turn.ID)
				if claimed.LeaseToken == token {
					t.Fatal("stale token reused")
				}
				{
					if rr := call(token); rr.Code != 409 {
						t.Fatalf("stale token accepted %d %s", rr.Code, rr.Body)
					}
				}
				if rr := call(claimed.LeaseToken); rr.Code != 200 {
					t.Fatalf("claimed operation %d %s", rr.Code, rr.Body)
				}
			})
		}
	}
}

func TestRound9BusyAdmissionDetails(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	s.cfg.MaxConcurrent = 1
	s.deps.Dispatch = nil
	c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "one", Title: "One"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "two", Title: "Two"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "first", Text: "first"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewService(st, s.cfg, s.deps)
	if err != nil {
		t.Fatal(err)
	}
	h := Handler{Service: second, Authenticate: func(*http.Request) (Principal, error) { return p, nil }}
	for _, tc := range []struct {
		conversation, reason string
		details              map[string]any
	}{
		{c.ID, "conversation", map[string]any{"reason": "conversation", "turn_id": turn.ID}},
		{other.ID, "capacity", map[string]any{"reason": "capacity", "in_flight": float64(1), "limit": float64(1)}},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequest("POST", "/pm/conversations/"+tc.conversation+"/messages", strings.NewReader(`{"request_key":"next","text":"next"}`)))
			var got struct {
				Error struct {
					Code    string
					Details map[string]any
				}
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil || rr.Code != 429 || got.Error.Code != "busy" || !reflect.DeepEqual(got.Error.Details, tc.details) {
				t.Fatalf("%d %s %v", rr.Code, rr.Body, err)
			}
		})
	}
	// Request replay wins over admission and does not change the original turn.
	replay, err := second.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "first", Text: "first"})
	if err != nil || !reflect.DeepEqual(replay, turn) {
		t.Fatalf("replay %+v %v", replay, err)
	}
	// Unknown serializes its conversation but frees workspace admission capacity.
	turn.Status = Unknown
	turn.Revision++
	if err := st.cas(ctx, "turn", turn.ID, turn.Revision-1, turn); err != nil {
		t.Fatal(err)
	}
	_, err = second.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "unknown", Text: "next"})
	var busy *BusyError
	if !errors.As(err, &busy) || busy.Reason != "conversation" || busy.TurnID != turn.ID {
		t.Fatalf("unknown: %v", err)
	}
	if _, err := second.PostMessage(ctx, p, other.ID, MessageInput{RequestKey: "allowed", Text: "next"}); err != nil {
		t.Fatal(err)
	}
}

func TestRound9AcknowledgeNoDeliveryPath(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	d := round4Approved(t, s, p)
	s.deps.CheckDelivery = func(context.Context, Action) error { return NoDeliveryPath("GitHub") }
	caller := p
	h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return caller, nil }}
	ack := func(status int) Action {
		t.Helper()
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest("POST", "/pm/actions/"+d.ActionID+"/acknowledge", strings.NewReader(`{}`)))
		if rr.Code != status {
			t.Fatalf("ack %d %s", rr.Code, rr.Body)
		}
		var a Action
		if status == 200 {
			if err := json.Unmarshal(rr.Body.Bytes(), &a); err != nil {
				t.Fatal(err)
			}
		}
		return a
	}
	caller = Principal{WorkspaceID: p.WorkspaceID, ActorID: "another-human", Human: true}
	ack(403)
	caller = p
	caller.WorkspaceID = "another-workspace"
	ack(403)
	caller = p
	caller.Human = false
	ack(403)
	caller = p
	a := ack(200)
	if a.Status != Acknowledged || a.Deliverable || a.AcknowledgedBy != p.ActorID || a.AcknowledgedAt == nil || !strings.Contains(a.Receipt.Detail, "No delivery path is configured for GitHub") || len(a.Attempts) != 0 || strings.Contains(a.Receipt.Detail, "action stays pending") {
		t.Fatalf("%+v", a)
	}
	if replay := ack(200); !reflect.DeepEqual(replay, a) {
		t.Fatalf("ack replay %+v", replay)
	}
	savedDecision, err := s.decision(ctx, p, d.ID, "pm.read")
	if err != nil || savedDecision.Status != Answered || savedDecision.Answer != d.Answer || savedDecision.AnsweredBy != d.AnsweredBy {
		t.Fatalf("approval changed: %+v %v", savedDecision, err)
	}
	s.deps.CheckDelivery = func(context.Context, Action) error { return nil }
	calls := 0
	s.deps.Execute = func(context.Context, Action) (Receipt, error) {
		calls++
		return Receipt{Status: Delivered, ExternalID: "remote"}, nil
	}
	s.deps.Reconcile = func(context.Context, Action) (Receipt, error) {
		t.Fatal("unsent action reconciled")
		return Receipt{}, nil
	}
	s, err = NewService(st, s.cfg, s.deps)
	if err != nil {
		t.Fatal(err)
	}
	h.Service = s
	got, err := s.action(ctx, p, a.ID, "pm.read")
	if err != nil || got.Deliverable || got.Receipt.Detail != a.Receipt.Detail {
		t.Fatalf("route revived action: %+v %v", got, err)
	}
	got, err = s.DispatchDecision(ctx, p, d.ID)
	if err != nil || got.Status != Acknowledged || calls != 0 {
		t.Fatalf("resent: %+v %v", got, err)
	}
	if _, err = s.ReconcileAction(ctx, p, a.ID); !errors.Is(err, ErrNothingDelivered) {
		t.Fatalf("reconcile: %v", err)
	}
	in := DecisionInput{RequestKey: "fresh", WorkRef: d.WorkRef, Scope: d.Scope, Instruction: d.Instruction, TargetRevision: d.TargetRevision, Payload: d.Payload}
	fresh, err := s.ProposeDecision(ctx, p, in)
	if err != nil || fresh.ID == d.ID {
		t.Fatalf("fresh %+v %v", fresh, err)
	}
	fresh, err = s.AnswerDecision(ctx, p, fresh.ID, AnswerInput{Revision: 1, Approve: true, Text: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err = s.DispatchDecision(ctx, p, fresh.ID); err != nil || got.Status != Delivered || calls != 1 {
		t.Fatalf("fresh delivery %+v %v", got, err)
	}
}

func TestRound9TurnReadAuthorizationAndRunner(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "c", Title: "Read"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "m", Text: "read"})
	if err != nil {
		t.Fatal(err)
	}
	agent := Principal{WorkspaceID: p.WorkspaceID, ActorID: s.cfg.AgentActorID}
	// PM can inspect any workspace turn, even before claiming it.
	if _, err := s.GetTurn(ctx, agent, turn.ID); err != nil {
		t.Fatal(err)
	}
	claimed := claimTestTurn(t, s, ctx, agent, turn.ID)
	for _, caller := range []Principal{p, agent, {WorkspaceID: p.WorkspaceID, ActorID: "another-human", Human: true}, {WorkspaceID: p.WorkspaceID, ActorID: "another-agent"}, {WorkspaceID: "elsewhere", ActorID: agent.ActorID}} {
		h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return caller, nil }}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest("GET", "/pm/turns/"+turn.ID, nil))
		if caller != p && caller != agent {
			if rr.Code != 403 {
				t.Fatalf("unauthorized %d %s", rr.Code, rr.Body)
			}
			continue
		}
		var got map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil || rr.Code != 200 || got["lease_owner"] != turn.ID || got["claimed"] != true {
			t.Fatalf("read %d %s %v", rr.Code, rr.Body, err)
		}
		if got["lease_token"] != nil || strings.Contains(rr.Body.String(), claimed.LeaseToken) {
			t.Fatal("token leaked")
		}
	}
	s.deps.Authorize = func(context.Context, Principal, string, string) error { return ErrForbidden }
	if _, err := s.GetTurn(ctx, agent, turn.ID); !errors.Is(err, ErrForbidden) {
		t.Fatal(err)
	}
	s.deps.Authorize = func(context.Context, Principal, string, string) error { return nil }
	claimed.LeaseExpiresAt = time.Now().Add(-time.Second)
	claimed.Revision++
	if err := st.cas(ctx, "turn", turn.ID, claimed.Revision-1, claimed); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(turnResponse(claimed, false))
	if err != nil || strings.Contains(string(raw), "lease_owner") {
		t.Fatalf("expired owner: %s %v", raw, err)
	}
}

func TestRound9NativeReceiptAttestationCannotComeFromJSON(t *testing.T) {
	for _, native := range []bool{false, true} {
		t.Run(fmt.Sprint(native), func(t *testing.T) {
			s, _, p, _ := fixture(t)
			d := round4Approved(t, s, p)
			s.deps.Execute = func(context.Context, Action) (Receipt, error) {
				var r Receipt
				err := json.Unmarshal([]byte(`{"status":"verified","external_id":"remote","evidence_refs":["work:1"],"independently_verified":true,"NativeReadBack":true,"native_read_back":true}`), &r)
				if err != nil || r.NativeReadBack {
					t.Fatalf("wire forged attestation: %+v %v", r, err)
				}
				r.NativeReadBack = native
				return r, nil
			}
			a, err := s.DispatchDecision(context.Background(), p, d.ID)
			want := Unknown
			if native {
				want = Verified
			}
			if err != nil || a.Status != want || a.Receipt.NativeReadBack {
				t.Fatalf("dispatch %+v %v", a, err)
			}
		})
	}
}

func TestRound9ConcurrentAdmissionReason(t *testing.T) {
	for _, sameConversation := range []bool{false, true} {
		t.Run(fmt.Sprint(sameConversation), func(t *testing.T) {
			s, st, p, _ := fixture(t)
			ctx := context.Background()
			s.cfg.MaxConcurrent = 1
			s.deps.Dispatch = nil
			second, err := NewService(st, s.cfg, s.deps)
			if err != nil {
				t.Fatal(err)
			}
			c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "c", Title: "Concurrent"})
			if err != nil {
				t.Fatal(err)
			}
			other := c
			if !sameConversation {
				other, err = s.CreateConversation(ctx, p, CreateConversation{RequestKey: "other", Title: "Other"})
				if err != nil {
					t.Fatal(err)
				}
			}
			type result struct {
				turn Turn
				err  error
			}
			results := make(chan result, 2)
			start := make(chan struct{})
			for i, svc := range []*Service{s, second} {
				go func(i int, svc *Service) {
					<-start
					id := c.ID
					if i == 1 {
						id = other.ID
					}
					turn, err := svc.PostMessage(ctx, p, id, MessageInput{RequestKey: fmt.Sprint(i), Text: "concurrent"})
					results <- result{turn, err}
				}(i, svc)
			}
			close(start)
			a, b := <-results, <-results
			if a.err != nil {
				a, b = b, a
			}
			var busy *BusyError
			if a.err != nil || !errors.As(b.err, &busy) {
				t.Fatalf("admission: %+v %+v", a, b)
			}
			if sameConversation {
				if busy.Reason != "conversation" || busy.TurnID != a.turn.ID {
					t.Fatal(busy)
				}
			} else {
				if busy.Reason != "capacity" || busy.InFlight != 1 || busy.Limit != 1 {
					t.Fatal(busy)
				}
			}
		})
	}
}

func TestRound9ExternalDispatchRemainsSourceReported(t *testing.T) {
	s, _, p, _ := fixture(t)
	d := round4Approved(t, s, p)
	s.deps.Execute = func(context.Context, Action) (Receipt, error) {
		return Receipt{Status: Reported, ExternalID: "external", Detail: "source says applied"}, nil
	}
	a, err := s.DispatchDecision(context.Background(), p, d.ID)
	if err != nil || a.Status != Reported || a.Receipt.IndependentlyVerified {
		t.Fatalf("external verification: %+v %v", a, err)
	}
}

func TestRound9TerminalReplaysPreserveState(t *testing.T) {
	for _, endpoint := range []string{"complete", "fail"} {
		t.Run(endpoint, func(t *testing.T) {
			s, st, p, _ := fixture(t)
			ctx := context.Background()
			c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "c", Title: "Replay"})
			if err != nil {
				t.Fatal(err)
			}
			turn, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "m", Text: "Work"})
			if err != nil {
				t.Fatal(err)
			}
			agent := Principal{WorkspaceID: p.WorkspaceID, ActorID: s.cfg.AgentActorID}
			claimed := claimTestTurn(t, s, ctx, agent, turn.ID)
			field := "text"
			if endpoint == "fail" {
				field = "reason"
			}
			body := fmt.Sprintf(`{%q:"finished","lease_token":%q}`, field, claimed.LeaseToken)
			h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return agent, nil }}
			call := func() *httptest.ResponseRecorder {
				rr := httptest.NewRecorder()
				h.ServeHTTP(rr, httptest.NewRequest("POST", "/pm/turns/"+turn.ID+"/"+endpoint, strings.NewReader(body)))
				return rr
			}
			if rr := call(); rr.Code != 200 {
				t.Fatalf("first: %d %s", rr.Code, rr.Body)
			}
			var before, after Turn
			if err := st.get(ctx, "turn", turn.ID, &before); err != nil {
				t.Fatal(err)
			}
			if rr := call(); rr.Code != 200 {
				t.Fatalf("replay: %d %s", rr.Code, rr.Body)
			}
			if err := st.get(ctx, "turn", turn.ID, &after); err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("replay mutation: %+v %v", after, err)
			}
		})
	}
}
