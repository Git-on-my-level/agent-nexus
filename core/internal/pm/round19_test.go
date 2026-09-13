package pm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

func TestRound19AnnotateProposalValidation(t *testing.T) {
	for _, throughTurn := range []bool{false, true} {
		t.Run(fmt.Sprint(throughTurn), func(t *testing.T) {
			s, st, p, _ := fixture(t)
			ctx := context.Background()
			caller, path, token := p, "/pm/decisions", ""
			if throughTurn {
				c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "c", Title: "Discuss"})
				if err != nil {
					t.Fatal(err)
				}
				turn, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "m", Text: "Propose"})
				if err != nil {
					t.Fatal(err)
				}
				caller = Principal{WorkspaceID: p.WorkspaceID, ActorID: "pm-agent"}
				claimed := claimTestTurn(t, s, ctx, caller, turn.ID)
				token, path = claimed.LeaseToken, "/pm/turns/"+turn.ID+"/decisions"
			}
			for i, tc := range []struct{ instruction, key string }{
				{`{"risk":"medium","title":"changed"}`, "risk, title"},
				{`{"priority":42}`, "priority"}, {`{"priority":"medium"}`, "priority"},
				{`{"due_at":"tomorrow"}`, "due_at"}, {`{"blockers":[1]}`, "blockers"},
				{`{"relations":[{"kind":"invalid","ref":"card:1"}]}`, "relation"},
				{`{"executions":[{}]}`, "execution"}, {`{}`, "patch"}, {`null`, "patch"}, {`[]`, "JSON object"},
			} {
				in := DecisionInput{RequestKey: fmt.Sprint(i), WorkRef: "work:1", Scope: "work.annotate", Instruction: tc.instruction, TargetRevision: "r1"}
				var body any = in
				if throughTurn {
					body = TurnProposeInput{DecisionInput: in, LeaseToken: token}
				}
				raw := round13Post(t, s, caller, path, body, 400)
				var out struct {
					Error struct{ Code, Message string }
				}
				if err := json.Unmarshal(raw, &out); err != nil {
					t.Fatal(err)
				}
				if out.Error.Code != "invalid_request" || !strings.Contains(out.Error.Message, tc.key) || !strings.Contains(out.Error.Message, strings.Join(primitives.WorkAnnotationKeys(), ", ")) {
					t.Fatalf("%s", raw)
				}
			}
			decisions, err := listRecords[Decision](ctx, st, "decision", p.WorkspaceID, "", "")
			if err != nil || len(decisions) != 0 {
				t.Fatalf("invalid proposals persisted: %+v %v", decisions, err)
			}
			in := DecisionInput{RequestKey: "valid", WorkRef: "work:1", Scope: "work.annotate", Instruction: `{"next_action":"review","priority":"p1"}`, TargetRevision: "r1"}
			var body any = in
			status := 201
			if throughTurn {
				body = TurnProposeInput{DecisionInput: in, LeaseToken: token}
				status = 200
			}
			round13Post(t, s, caller, path, body, status)
		})
	}
}

func TestRound19LeaseHeartbeatAndReclaim(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	s.cfg.TurnTimeout = 10 * time.Minute
	s.cfg.LeaseTTL = 60 * time.Second
	c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "c", Title: "Discuss"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "m", Text: "Help"})
	if err != nil {
		t.Fatal(err)
	}
	agent := Principal{WorkspaceID: p.WorkspaceID, ActorID: "pm-agent"}
	claimed, err := s.ClaimTurn(ctx, agent, ClaimInput{RunnerID: "crashed"})
	if err != nil {
		t.Fatal(err)
	}
	if ttl := time.Until(claimed.LeaseExpiresAt); ttl > time.Minute || ttl < 55*time.Second {
		t.Fatalf("TTL=%s", ttl)
	}
	oldToken := claimed.LeaseToken
	// Advance the persisted lease boundary without a sleep; turn deadline stays live.
	old := claimed.Revision
	claimed.LeaseExpiresAt = time.Now().Add(-time.Second)
	claimed.Revision++
	if err := st.cas(ctx, "turn", turn.ID, old, claimed); err != nil {
		t.Fatal(err)
	}
	h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return agent, nil }}
	heartbeatPath := "/pm/turns/" + turn.ID + "/heartbeat"
	out := round11Request(t, h, "POST", heartbeatPath, HeartbeatInput{LeaseToken: oldToken}, 409)
	if out["error"].(map[string]any)["code"] != "lease_mismatch" {
		t.Fatal(out)
	}
	queued, err := s.GetTurn(ctx, p, turn.ID)
	if err != nil || queued.Status != Sending || leaseHeld(queued, time.Now()) {
		t.Fatalf("queued: %+v %v", queued, err)
	}
	recovered, err := s.ClaimTurn(ctx, agent, ClaimInput{RunnerID: "replacement"})
	if err != nil {
		t.Fatal(err)
	}
	if recovered.ID != turn.ID || recovered.Status != Sending || recovered.LeaseToken == oldToken || recovered.WakeupID != claimed.WakeupID || recovered.CreatedAt != claimed.CreatedAt {
		t.Fatalf("reclaim lost history: %+v", recovered)
	}
	if _, err := s.CompleteTurnWithLease(ctx, agent, turn.ID, "done", nil, oldToken); !errors.Is(err, ErrLeaseMismatch) {
		t.Fatalf("old complete: %v", err)
	}
	for _, token := range []string{"foreign", oldToken, ""} {
		out := round11Request(t, h, "POST", heartbeatPath, HeartbeatInput{LeaseToken: token}, 409)
		want := "lease_mismatch"
		if token == "" {
			want = "lease_required"
		}
		if out["error"].(map[string]any)["code"] != want {
			t.Fatal(out)
		}
	}
	old = recovered.Revision
	recovered.LeaseExpiresAt = time.Now().Add(5 * time.Second)
	recovered.Revision++
	if err := st.cas(ctx, "turn", turn.ID, old, recovered); err != nil {
		t.Fatal(err)
	}
	out = round11Request(t, h, "POST", heartbeatPath, HeartbeatInput{LeaseToken: recovered.LeaseToken}, 200)
	if _, exists := out["lease_token"]; exists {
		t.Fatal("heartbeat exposed token")
	}
	expiry, err := time.Parse(time.RFC3339Nano, out["lease_expires_at"].(string))
	if err != nil || !expiry.After(recovered.LeaseExpiresAt) || expiry.After(recovered.Deadline) {
		t.Fatalf("renewal: %v %v", out, err)
	}
	var renewed Turn
	if err := st.get(ctx, "turn", turn.ID, &renewed); err != nil {
		t.Fatal(err)
	}
	if renewed.LeaseToken != recovered.LeaseToken || !renewed.ClaimedAt.Equal(*recovered.ClaimedAt) {
		t.Fatalf("changed ownership: %+v", renewed)
	}
	old = renewed.Revision
	renewed.Deadline = time.Now().Add(10 * time.Second)
	renewed.Revision++
	if err := st.cas(ctx, "turn", turn.ID, old, renewed); err != nil {
		t.Fatal(err)
	}
	bounded, err := s.HeartbeatTurn(ctx, agent, turn.ID, HeartbeatInput{LeaseToken: renewed.LeaseToken})
	if err != nil || !bounded.LeaseExpiresAt.Equal(renewed.Deadline) {
		t.Fatalf("deadline bound: %+v %v", bounded, err)
	}
	if _, err := s.CompleteTurnWithLease(ctx, agent, turn.ID, "done", nil, bounded.LeaseToken); err != nil {
		t.Fatal(err)
	}
}

func TestRound19HumanPendingIsActorScoped(t *testing.T) {
	s, _, a, _ := fixture(t)
	ctx := context.Background()
	in := DecisionInput{RequestKey: "human-a", WorkRef: "work:1", Scope: "work.phase", Instruction: "Block", TargetRevision: "r1", Payload: &ActionPayload{Phase: "blocked"}}
	human, err := s.ProposeDecision(ctx, a, in)
	if err != nil {
		t.Fatal(err)
	}
	for _, sameActor := range []bool{true, false} {
		b := a
		if !sameActor {
			b.ActorID = "human-b"
		}
		c, err := s.CreateConversation(ctx, b, CreateConversation{RequestKey: "c", Title: "Discuss"})
		if err != nil {
			t.Fatal(err)
		}
		turn, err := s.PostMessage(ctx, b, c.ID, MessageInput{RequestKey: "m", Text: "Propose"})
		if err != nil {
			t.Fatal(err)
		}
		agent := Principal{WorkspaceID: a.WorkspaceID, ActorID: "pm-agent"}
		claimed := claimTestTurn(t, s, ctx, agent, turn.ID)
		in.RequestKey = "pm"
		in.Instruction = "Move to ready"
		in.Payload = &ActionPayload{Phase: "ready"}
		d, err := s.ProposeForTurn(ctx, agent, turn.ID, in, claimed.LeaseToken)
		if sameActor {
			var pending *HumanProposalPendingError
			if !errors.As(err, &pending) || pending.PendingDecisionID != human.ID {
				t.Fatalf("same actor: %v", err)
			}
		} else if err != nil || d.ActorID != b.ActorID {
			t.Fatalf("different actor: %+v %v", d, err)
		}
	}
}
