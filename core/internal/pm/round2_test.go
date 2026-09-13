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

func TestUnavailableDeliveryDoesNotChangeAction(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	d, err := s.ProposeDecision(ctx, p, DecisionInput{RequestKey: "no-path", WorkRef: "work:1", Scope: "github", Instruction: "close", TargetRevision: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	d, err = s.AnswerDecision(ctx, p, d.ID, AnswerInput{Revision: 1, Approve: true, Text: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	s.deps.CheckDelivery = func(context.Context, Action) error { return NoDeliveryPath("GitHub") }
	s.deps.Execute = func(context.Context, Action) (Receipt, error) { t.Fatal("executor called"); return Receipt{}, nil }
	var before Action
	if err = st.get(ctx, "action", d.ActionID, &before); err != nil {
		t.Fatal(err)
	}
	h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return p, nil }}
	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/pm/decisions/"+d.ID+"/dispatch", strings.NewReader(`{}`)))
		if w.Code != 503 || !strings.Contains(w.Body.String(), `"code":"unavailable"`) || !strings.Contains(w.Body.String(), "No delivery path is configured for GitHub yet") {
			t.Fatalf("%d %s", w.Code, w.Body)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/pm/actions/"+d.ActionID+"/reconcile", strings.NewReader(`{}`)))
	if w.Code != 400 || !strings.Contains(w.Body.String(), "Nothing has been delivered yet, so there is nothing to read back") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var after Action
	if err = st.get(ctx, "action", d.ActionID, &after); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("action changed: %+v => %+v", before, after)
	}
	for _, available := range []bool{false, true} {
		if available {
			s.deps.CheckDelivery = func(context.Context, Action) error { return nil }
		}
		got, err := s.action(ctx, p, d.ActionID, "pm.read")
		if err != nil || got.Deliverable != available {
			t.Fatalf("get %+v %v", got, err)
		}
		list, err := s.ListActions(ctx, p)
		if err != nil || len(list) != 1 || list[0].Deliverable != available {
			t.Fatalf("list %+v %v", list, err)
		}
		page, err := s.ActionPage(ctx, p, 20, "")
		if err != nil || len(page.Items) != 1 || page.Items[0].Deliverable != available {
			t.Fatalf("page %+v %v", page, err)
		}
	}
}

func TestDeadlineExpiryOnReadsAndMaintenance(t *testing.T) {
	for _, read := range []string{"history", "turn", "tick"} {
		t.Run(read, func(t *testing.T) {
			s, st, p, _ := fixture(t)
			s.deps.Dispatch = nil
			ctx := context.Background()
			c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "expiry", Title: "Expiry"})
			if err != nil {
				t.Fatal(err)
			}
			now := time.Now().UTC()
			for _, status := range []Status{Pending, Sending, Unknown, Delivered, Failed} {
				turn := Turn{ID: string(status), ConversationID: c.ID, WorkspaceID: p.WorkspaceID, ActorID: p.ActorID, AgentActorID: "previous-pm", Status: status, Revision: 1, Deadline: now.Add(-time.Second), LeaseOwner: "runner", LeaseToken: "lease", LeaseExpiresAt: now.Add(time.Minute)}
				if _, err = st.insert(ctx, "turn", turn.ID, p.WorkspaceID, p.ActorID, c.ID, turn); err != nil {
					t.Fatal(err)
				}
			}
			future := Turn{ID: "future", ConversationID: c.ID, WorkspaceID: p.WorkspaceID, ActorID: p.ActorID, Status: Sending, Revision: 1, Deadline: now.Add(time.Hour)}
			if _, err = st.insert(ctx, "turn", future.ID, p.WorkspaceID, p.ActorID, c.ID, future); err != nil {
				t.Fatal(err)
			}
			other := p
			other.ActorID = "another-human"
			if _, err := s.GetTurn(ctx, other, string(Sending)); !errors.Is(err, ErrForbidden) {
				t.Fatalf("cross-actor turn read: %v", err)
			}
			var unchanged Turn
			if err := st.get(ctx, "turn", string(Sending), &unchanged); err != nil || unchanged.Revision != 1 {
				t.Fatalf("unauthorized read changed turn %+v %v", unchanged, err)
			}
			if read == "tick" {
				err = s.ExpireTurns(ctx, now)
			} else {
				h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return p, nil }}
				path := "/pm/turns/sending"
				if read == "history" {
					path = "/pm/conversations/" + c.ID
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
				if w.Code != 200 || !strings.Contains(w.Body.String(), `"deadline":`) || !strings.Contains(w.Body.String(), "The PM did not answer before the deadline. Retry, or check that a runner is attached.") {
					t.Fatalf("%d %s", w.Code, w.Body)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, status := range []Status{Pending, Sending, Unknown} {
				var got Turn
				if err = st.get(ctx, "turn", string(status), &got); err != nil {
					t.Fatal(err)
				}
				if got.Status != Failed || got.Revision != 2 || got.LeaseToken != "" || got.LeaseOwner != "" || !got.LeaseExpiresAt.IsZero() {
					t.Fatalf("not expired %+v", got)
				}
			}
			for _, id := range []string{"future", string(Delivered), string(Failed)} {
				var got Turn
				if err = st.get(ctx, "turn", id, &got); err != nil || got.Revision != 1 {
					t.Fatalf("changed terminal/future %+v %v", got, err)
				}
			}
		})
	}
}

func TestChangedTurnProposalSupersedesIntentAtomically(t *testing.T) {
	for _, change := range []string{"payload", "instruction", "revision"} {
		t.Run(change, func(t *testing.T) {
			s, st, p, _ := fixture(t)
			ctx := context.Background()
			c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "c", Title: "Proposal"})
			if err != nil {
				t.Fatal(err)
			}
			turn, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "m", Text: "Propose"})
			if err != nil {
				t.Fatal(err)
			}
			agent := Principal{WorkspaceID: p.WorkspaceID, ActorID: "pm-agent"}
			in := DecisionInput{RequestKey: "first", WorkRef: "work:1", Instruction: "Move", Scope: "work.phase", TargetRevision: "r1", Payload: &ActionPayload{Phase: "ready"}}
			first, err := s.ProposeForTurn(ctx, agent, turn.ID, in)
			if err != nil {
				t.Fatal(err)
			}
			in.RequestKey = "second"
			switch change {
			case "payload":
				in.Payload = &ActionPayload{Phase: "review"}
			case "instruction":
				in.Instruction = "Review then move"
			case "revision":
				in.TargetRevision = "r2"
			}
			next, err := s.ProposeForTurn(ctx, agent, turn.ID, in)
			if err != nil || next.ID == first.ID || next.Instruction != in.Instruction || !reflect.DeepEqual(next.Payload, in.Payload) || next.TargetRevision != in.TargetRevision {
				t.Fatalf("intent lost %+v %v", next, err)
			}
			old, err := s.decision(ctx, p, first.ID, "pm.read")
			if err != nil || old.Status != Superseded || old.SupersededBy != next.ID || old.SupersededReason == "" || old.CanAnswer {
				t.Fatalf("old %+v %v", old, err)
			}
			if _, err = s.AnswerDecision(ctx, p, old.ID, AnswerInput{Revision: 1, Approve: true, Text: "yes"}); !errors.Is(err, ErrConflict) {
				t.Fatalf("stale approval %v", err)
			}
			var saved Turn
			if err = st.get(ctx, "turn", turn.ID, &saved); err != nil {
				t.Fatal(err)
			}
			if saved.DecisionIDs[len(saved.DecisionIDs)-1] != next.ID {
				t.Fatalf("missing new link %+v", saved)
			}
			replay, err := s.ProposeForTurn(ctx, agent, turn.ID, in)
			if err != nil || replay.ID != next.ID {
				t.Fatalf("replay %+v %v", replay, err)
			}
			// A failed turn link must roll back both supersession and insertion.
			candidate := Decision{ID: "rollback", WorkspaceID: p.WorkspaceID, ActorID: p.ActorID, WorkRef: in.WorkRef, Scope: in.Scope, Instruction: "third", Status: AwaitingAnswer, Revision: 1}
			if _, _, err = st.proposeDecision(ctx, candidate, "missing-turn"); err == nil {
				t.Fatal("missing turn accepted")
			}
			current, err := s.decision(ctx, p, next.ID, "pm.read")
			if err != nil || current.Status != AwaitingAnswer {
				t.Fatalf("partial transaction %+v %v", current, err)
			}
			var absent Decision
			if err = st.get(ctx, "decision", candidate.ID, &absent); !errors.Is(err, ErrNotFound) {
				t.Fatalf("leaked insert %v", err)
			}
		})
	}
}

func TestDirectAgentProposalsForbiddenButTurnProposalsAddressHuman(t *testing.T) {
	s, _, human, _ := fixture(t)
	ctx := context.Background()
	agent := Principal{WorkspaceID: human.WorkspaceID, ActorID: "pm-agent"}
	h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return agent, nil }}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/pm/decisions", strings.NewReader(`{"request_key":"agent","work_ref":"work:1","scope":"work.phase","instruction":"Move","target_revision":"r1","payload":{"phase":"ready"}}`)))
	if w.Code != 403 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	c, err := s.CreateConversation(ctx, human, CreateConversation{RequestKey: "c", Title: "Human question"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := s.PostMessage(ctx, human, c.ID, MessageInput{RequestKey: "m", Text: "What next?"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.ProposeForTurn(ctx, agent, turn.ID, DecisionInput{RequestKey: "through-turn", WorkRef: "work:1", Scope: "work.phase", Instruction: "Move", TargetRevision: "r1", Payload: &ActionPayload{Phase: "ready"}})
	if err != nil || d.ActorID != human.ActorID {
		t.Fatalf("proposal %+v %v", d, err)
	}
	if _, err = s.AnswerDecision(ctx, human, d.ID, AnswerInput{Revision: 1, Approve: true, Text: "yes"}); err != nil {
		t.Fatal(err)
	}
}

func TestRequestKeyRevisionConflictIdentifiesExistingDecision(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	in := DecisionInput{RequestKey: "drag-work-ready", WorkRef: "work:1", Scope: "work.phase", Instruction: "Move", TargetRevision: "r1", Payload: &ActionPayload{Phase: "ready"}}
	d, err := s.ProposeDecision(ctx, p, in)
	if err != nil {
		t.Fatal(err)
	}
	h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return p, nil }}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/pm/decisions", strings.NewReader(`{"request_key":"drag-work-ready","work_ref":"work:1","scope":"work.phase","instruction":"Move","target_revision":"r2","payload":{"phase":"ready"}}`)))
	var response struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				ID string `json:"existing_decision_id"`
			} `json:"details"`
		} `json:"error"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != 409 || response.Error.Code != "conflict" || response.Error.Details.ID != d.ID {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var saved Decision
	if err = st.get(ctx, "decision", d.ID, &saved); err != nil || saved.Status != AwaitingAnswer || saved.TargetRevision != "r1" || saved.Revision != 1 {
		t.Fatalf("changed intent %+v %v", saved, err)
	}
	replay, err := s.ProposeDecision(ctx, p, in)
	if err != nil || replay.ID != d.ID {
		t.Fatalf("replay %+v %v", replay, err)
	}
}

func TestConcurrentChangedProposalsPreserveBothIntents(t *testing.T) {
	s, st, p, _ := fixture(t)
	second, err := NewService(st, s.cfg, s.deps)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	type result struct {
		decision Decision
		err      error
	}
	results := make(chan result, 2)
	for i, svc := range []*Service{s, second} {
		go func(i int, svc *Service) {
			phase := []string{"ready", "review"}[i]
			d, err := svc.ProposeDecision(ctx, p, DecisionInput{RequestKey: phase, WorkRef: "work:1", Scope: "work.phase", Instruction: phase, TargetRevision: "r1", Payload: &ActionPayload{Phase: phase}})
			if err == nil && (d.Instruction != phase || d.Payload.Phase != phase) {
				err = errors.New("returned different intent")
			}
			results <- result{d, err}
		}(i, svc)
	}
	a, b := <-results, <-results
	if a.err != nil || b.err != nil || a.decision.ID == b.decision.ID {
		t.Fatalf("lost proposal %+v %+v", a, b)
	}
	ds, err := s.ListDecisions(ctx, p)
	if err != nil || len(ds) != 2 {
		t.Fatalf("decisions %+v %v", ds, err)
	}
	var awaiting, superseded *Decision
	for i := range ds {
		switch ds[i].Status {
		case AwaitingAnswer:
			awaiting = &ds[i]
		case Superseded:
			superseded = &ds[i]
		}
	}
	if awaiting == nil || superseded == nil || superseded.SupersededBy != awaiting.ID {
		t.Fatalf("broken replacement chain %+v", ds)
	}
}

func TestUnavailableExecutorConfigurationFailsClosed(t *testing.T) {
	s, _, p, _ := fixture(t)
	ctx := context.Background()
	d, err := s.ProposeDecision(ctx, p, DecisionInput{RequestKey: "no-executor", WorkRef: "work:1", Scope: "github", Instruction: "close", TargetRevision: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	d, err = s.AnswerDecision(ctx, p, d.ID, AnswerInput{Revision: 1, Approve: true, Text: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	original := s.deps
	for _, missing := range []string{"preflight", "execute", "revision"} {
		s.deps = original
		switch missing {
		case "preflight":
			s.deps.CheckDelivery = nil
		case "execute":
			s.deps.Execute = nil
		case "revision":
			s.deps.CurrentRevision = nil
		}
		if _, err = s.DispatchDecision(ctx, p, d.ID); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("%s: %v", missing, err)
		}
		actions, err := s.ListActions(ctx, p)
		if err != nil || len(actions) != 1 || actions[0].Deliverable || actions[0].Revision != 1 || len(actions[0].Attempts) != 0 {
			t.Fatalf("%s: %+v %v", missing, actions, err)
		}
	}
}
