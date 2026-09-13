package pm

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestRound12TerminalReplayLeaseErrors(t *testing.T) {
	for _, endpoint := range []string{"complete", "fail"} {
		t.Run(endpoint, func(t *testing.T) {
			s, st, p, _ := fixture(t)
			ctx := context.Background()
			c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "r12", Title: "Replay"})
			if err != nil {
				t.Fatal(err)
			}
			turn, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "r12", Text: "go"})
			if err != nil {
				t.Fatal(err)
			}
			agent := Principal{WorkspaceID: p.WorkspaceID, ActorID: s.cfg.AgentActorID}
			claimed := claimTestTurn(t, s, ctx, agent, turn.ID)
			h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return agent, nil }}
			body := map[string]any{"text": "done", "lease_token": claimed.LeaseToken}
			status := "delivered"
			if endpoint == "fail" {
				body = map[string]any{"reason": "failed", "lease_token": claimed.LeaseToken}
				status = "failed"
			}
			path := "/pm/turns/" + turn.ID + "/" + endpoint
			round11Request(t, h, "POST", path, body, 200)
			var before Turn
			if err := st.get(ctx, "turn", turn.ID, &before); err != nil {
				t.Fatal(err)
			}
			for _, token := range []string{"", " ", "stale", claimed.LeaseToken} {
				body["lease_token"] = token
				code := 409
				if token == claimed.LeaseToken {
					code = 200
				}
				out := round11Request(t, h, "POST", path, body, code)
				if code == 409 {
					e := out["error"].(map[string]any)
					msg := e["message"].(string)
					if e["code"] != "lease_mismatch" || !strings.Contains(msg, "already "+status) || !strings.Contains(msg, "No retry is needed") || strings.Contains(msg, "claim the turn again") {
						t.Fatal(e)
					}
				}
			}
			var after Turn
			if err := st.get(ctx, "turn", turn.ID, &after); err != nil || !reflect.DeepEqual(before, after) {
				t.Fatalf("replay changed turn: %+v %v", after, err)
			}
			before.TerminalLeaseHash = "" // Legacy terminal rows cannot authenticate replay.
			if err := terminalLeaseGuard(before, claimed.LeaseToken); !errors.Is(err, ErrLeaseMismatch) {
				t.Fatal(err)
			}
		})
	}
}

func TestRound12ReleaseErrors(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "r12", Title: "Release"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "r12", Text: "go"})
	if err != nil {
		t.Fatal(err)
	}
	agent := Principal{WorkspaceID: p.WorkspaceID, ActorID: s.cfg.AgentActorID}
	claimed := claimTestTurn(t, s, ctx, agent, turn.ID)
	h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return agent, nil }}
	path := "/pm/turns/" + turn.ID + "/release"
	for _, in := range []ReleaseInput{{RunnerID: claimed.LeaseOwner, LeaseToken: "stale"}, {RunnerID: "other-runner", LeaseToken: claimed.LeaseToken}} {
		e := round11Request(t, h, "POST", path, in, 409)["error"].(map[string]any)
		if e["code"] != "lease_mismatch" {
			t.Fatal(e)
		}
	}
	var after Turn
	if err := st.get(ctx, "turn", turn.ID, &after); err != nil || !reflect.DeepEqual(claimed, after) {
		t.Fatalf("mismatch changed turn: %+v %v", after, err)
	}
	in := ReleaseInput{RunnerID: claimed.LeaseOwner, LeaseToken: claimed.LeaseToken}
	round11Request(t, h, "POST", path, in, 200)
	after = Turn{}
	if err := st.get(ctx, "turn", turn.ID, &after); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		e := round11Request(t, h, "POST", path, in, 409)["error"].(map[string]any)
		if e["code"] != "turn_not_claimed" || !strings.Contains(e["message"].(string), "already released") || !strings.Contains(e["message"].(string), "no release is needed") {
			t.Fatal(e)
		}
	}
	var replay Turn
	if err := st.get(ctx, "turn", turn.ID, &replay); err != nil || !reflect.DeepEqual(after, replay) {
		t.Fatalf("repeat release changed turn: %+v %v", replay, err)
	}
	claimTestTurn(t, s, ctx, agent, turn.ID)
	e := round11Request(t, h, "POST", path, in, 409)["error"].(map[string]any)
	if e["code"] != "lease_mismatch" {
		t.Fatal(e)
	}
}

func TestRound12DecisionProjectionReadFailures(t *testing.T) {
	s, _, p, _ := fixture(t)
	for _, readErr := range []error{ErrNotFound, context.DeadlineExceeded} {
		s.deps.DecisionWork = func(context.Context, Principal, string) (DecisionWork, error) { return DecisionWork{}, readErr }
		d := s.decisionForReader(context.Background(), p, Decision{ActorID: p.ActorID, Status: AwaitingAnswer, WorkMissing: true, TargetCurrent: true, AlreadyAtTarget: true})
		if d.CanAnswer || d.TargetCurrent || d.AlreadyAtTarget || d.WorkMissing != errors.Is(readErr, ErrNotFound) {
			t.Fatal(d)
		}
	}
}

func TestRound12DecisionTargetFlags(t *testing.T) {
	s, _, p, _ := fixture(t)
	s.deps.DecisionWork = func(context.Context, Principal, string) (DecisionWork, error) {
		return DecisionWork{Revision: "r2", Phase: "ready"}, nil
	}
	for _, tc := range []struct {
		revision, phase string
		current, moot   bool
	}{
		{"r2", "ready", true, true}, {"r2", "review", true, false}, {"r1", "ready", false, true}, {"r1", "review", false, false}, {"r2", "", true, false},
	} {
		t.Run(tc.revision+"/"+tc.phase, func(t *testing.T) {
			d := Decision{ActorID: p.ActorID, Status: AwaitingAnswer, TargetRevision: tc.revision}
			if tc.phase != "" {
				d.Payload = &ActionPayload{Phase: tc.phase}
			}
			d = s.decisionForReader(context.Background(), p, d)
			if d.WorkMissing || !d.CanAnswer || d.TargetCurrent != tc.current || d.AlreadyAtTarget != tc.moot {
				t.Fatal(d)
			}
		})
	}
}
