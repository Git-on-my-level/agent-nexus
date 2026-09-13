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

func boolPtr(v bool) *bool { return &v }

func TestRound17QueueAndRunnerCapacityHTTP(t *testing.T) {
	s, st, human, _ := fixture(t)
	ctx := context.Background()
	s.deps.Dispatch = nil
	s.cfg.TurnTimeout = 10 * time.Minute
	if s.cfg.MaxQueued != 20 || s.cfg.MaxConcurrent != 2 {
		t.Fatalf("defaults: %+v", s.cfg)
	}
	caller := human
	h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return caller, nil }}
	ask := func(i, status int) map[string]any {
		t.Helper()
		caller = Principal{WorkspaceID: human.WorkspaceID, ActorID: fmt.Sprintf("human-%d", i), Human: true}
		c, err := s.CreateConversation(ctx, caller, CreateConversation{RequestKey: "conversation", Title: "Question"})
		if err != nil {
			t.Fatal(err)
		}
		return round11Request(t, h, "POST", "/pm/conversations/"+c.ID+"/messages", MessageInput{RequestKey: "question", Text: "Help?"}, status)
	}
	// Different humans can queue questions without a runner, including the third.
	for i := 0; i < 20; i++ {
		if out := ask(i, 202); out["claimed"] != false {
			t.Fatalf("unclaimed: %v", out)
		}
	}
	out := ask(20, 429)["error"].(map[string]any)
	if out["code"] != "busy" || out["message"] != "Workspace PM queue is full (20 waiting; limit 20)" || !reflect.DeepEqual(out["details"], map[string]any{"reason": "queue", "queued": float64(20), "limit": float64(20)}) {
		t.Fatal(out)
	}
	claim := func(runner string, status int) Turn {
		t.Helper()
		caller = Principal{WorkspaceID: human.WorkspaceID, ActorID: "pm-agent"}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest("POST", "/pm/turns/claim", strings.NewReader(fmt.Sprintf(`{"runner_id":%q}`, runner))))
		if rr.Code != status {
			t.Fatalf("claim %s: %d %s", runner, rr.Code, rr.Body)
		}
		var turn Turn
		if status == 204 {
			if rr.Body.Len() != 0 {
				t.Fatalf("204 body: %s", rr.Body)
			}
		} else if err := json.Unmarshal(rr.Body.Bytes(), &turn); err != nil {
			t.Fatal(err)
		}
		return turn
	}
	first := claim("runner-1", 200)
	second := claim("runner-2", 200)
	if first.ID == second.ID {
		t.Fatal("same turn allocated twice")
	}
	claim("runner-3", 204)
	if recovered := claim("runner-1", 200); !reflect.DeepEqual(recovered, first) {
		t.Fatalf("recovery changed: %+v", recovered)
	}
	// Claimed turns no longer occupy queue slots, even at full runner capacity.
	ask(20, 202)
	ask(21, 202)
	ask(22, 429)
	// Expire only the lease, leaving the turn deadline in the future.
	var saved Turn
	if err := st.get(ctx, "turn", first.ID, &saved); err != nil {
		t.Fatal(err)
	}
	saved.LeaseExpiresAt = time.Now().UTC().Add(-time.Second)
	saved.Revision++
	if err := st.cas(ctx, "turn", saved.ID, saved.Revision-1, saved); err != nil {
		t.Fatal(err)
	}
	reclaimed := claim("runner-3", 200)
	if reclaimed.ID != first.ID || reclaimed.LeaseToken == first.LeaseToken {
		t.Fatalf("expired lease not reclaimed: %+v", reclaimed)
	}
	claim("runner-4", 204)
}

func TestRound17QueueCountsOnlyWaitingTurns(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		status                           Status
		lease, expiredLease, expiredTurn bool
		waiting                          bool
	}{
		{name: "pending", status: Pending, waiting: true},
		{name: "sending", status: Sending, waiting: true},
		{name: "unknown", status: Unknown, waiting: true},
		{name: "claimed", status: Sending, lease: true},
		{name: "unknown_claimed", status: Unknown, lease: true},
		{name: "expired_lease", status: Sending, lease: true, expiredLease: true, waiting: true},
		{name: "expired_turn", status: Sending, expiredTurn: true},
		{name: "terminal", status: Failed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, st, p, _ := fixture(t)
			ctx := context.Background()
			s.cfg.MaxQueued = 1
			s.deps.Dispatch = nil
			now := time.Now().UTC()
			seed := Turn{ID: "seed", WorkspaceID: p.WorkspaceID, ConversationID: "seed", Status: tc.status, Deadline: now.Add(time.Minute), Revision: 1}
			if tc.lease {
				seed.LeaseToken, seed.LeaseExpiresAt = "token", now.Add(time.Minute)
			}
			if tc.expiredLease {
				seed.LeaseExpiresAt = now.Add(-time.Second)
			}
			if tc.expiredTurn {
				seed.Deadline = now.Add(-time.Second)
			}
			if _, err := st.insert(ctx, "turn", seed.ID, p.WorkspaceID, p.ActorID, seed.ConversationID, seed); err != nil {
				t.Fatal(err)
			}
			c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "new", Title: "New"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "new", Text: "Question"})
			if tc.waiting {
				var busy *BusyError
				if !errors.As(err, &busy) || busy.Reason != "queue" || busy.Queued != 1 || busy.Limit != 1 {
					t.Fatalf("waiting: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRound17MaxQueuedConfig(t *testing.T) {
	s, st, _, _ := fixture(t)
	for _, limit := range []int{-1, 0, 1, 40} {
		cfg := s.cfg
		cfg.MaxQueued = limit
		got, err := NewService(st, cfg, s.deps)
		if limit < 0 {
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("negative limit: %v", err)
			}
			continue
		}
		want := limit
		if want == 0 {
			want = 20
		}
		if err != nil || got.cfg.MaxQueued != want {
			t.Fatalf("limit %d: %+v %v", limit, got, err)
		}
	}
}

func TestRound17DecisionNullableTargetsHTTP(t *testing.T) {
	for _, state := range []string{"current", "mismatch", "missing", "transient", "unconfigured"} {
		t.Run(state, func(t *testing.T) {
			s, st, p, _ := fixture(t)
			ctx := context.Background()
			d, err := s.ProposeDecision(ctx, p, DecisionInput{RequestKey: "proposal", WorkRef: "work:1", Instruction: "Block", Scope: "work.phase", TargetRevision: "r1", Payload: &ActionPayload{Phase: "blocked"}})
			if err != nil {
				t.Fatal(err)
			}
			before := round13Body(t, st, "decision", d.ID)
			var current, atTarget any = true, true
			s.deps.DecisionWork = func(context.Context, Principal, string) (DecisionWork, error) {
				switch state {
				case "missing":
					return DecisionWork{}, ErrNotFound
				case "transient":
					return DecisionWork{}, context.DeadlineExceeded
				case "mismatch":
					return DecisionWork{Revision: "r2", Phase: "backlog"}, nil
				}
				return DecisionWork{Revision: "r1", Phase: "blocked"}, nil
			}
			if state == "unconfigured" {
				s.deps.DecisionWork = nil
			}
			if state == "missing" || state == "mismatch" {
				current, atTarget = false, false
			} else if state == "transient" || state == "unconfigured" {
				current, atTarget = nil, nil
			}
			h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return p, nil }}
			out := round11Request(t, h, "GET", "/pm/decisions/"+d.ID, nil, 200)
			for key, want := range map[string]any{"target_current": current, "already_at_target": atTarget, "work_missing": state == "missing", "can_answer": state == "current" || state == "mismatch"} {
				if got, present := out[key]; !present || got != want {
					t.Fatalf("%s: present=%v got=%v want=%v", key, present, got, want)
				}
			}
			if state == "transient" || state == "unconfigured" {
				out = round11Request(t, h, "POST", "/pm/decisions/"+d.ID+"/answer", AnswerInput{Revision: d.Revision, Approve: true, Text: "approve"}, 409)
				e := out["error"].(map[string]any)
				if e["code"] != "source_revision_changed" || e["details"].(map[string]any)["reason"] != "work_read_failed" {
					t.Fatal(e)
				}
			}
			if after := round13Body(t, st, "decision", d.ID); before != after {
				t.Fatal("projection or refused approval mutated decision")
			}
		})
	}
}
