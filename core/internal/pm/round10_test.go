package pm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func testTurnLease(t *testing.T, s *Service, ctx context.Context, id string) string {
	t.Helper()
	var turn Turn
	if err := s.store.get(ctx, "turn", id, &turn); err != nil {
		t.Fatal(err)
	}
	return turn.LeaseToken
}

func TestRound10TerminalReplay(t *testing.T) {
	for _, endpoint := range []string{"complete", "fail"} {
		t.Run(endpoint, func(t *testing.T) {
			s, st, p, _ := fixture(t)
			ctx := context.Background()
			c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "c", Title: "Replay"})
			if err != nil {
				t.Fatal(err)
			}
			turn, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "m", Text: "work"})
			if err != nil {
				t.Fatal(err)
			}
			agent := Principal{WorkspaceID: p.WorkspaceID, ActorID: s.cfg.AgentActorID}
			claimed := claimTestTurn(t, s, ctx, agent, turn.ID)
			h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return agent, nil }}
			call := func(text, token, refs string) *httptest.ResponseRecorder {
				t.Helper()
				field := "text"
				if endpoint == "fail" {
					field = "reason"
					refs = ""
				}
				rr := httptest.NewRecorder()
				h.ServeHTTP(rr, httptest.NewRequest("POST", "/pm/turns/"+turn.ID+"/"+endpoint, strings.NewReader(fmt.Sprintf(`{%q:%q,"lease_token":%q%s}`, field, text, token, refs))))
				return rr
			}
			first := call("finished", claimed.LeaseToken, `,"evidence_refs":["card:1"]`)
			if first.Code != 200 {
				t.Fatalf("initial: %d %s", first.Code, first.Body)
			}
			var saved Turn
			if err := st.get(ctx, "turn", turn.ID, &saved); err != nil {
				t.Fatal(err)
			}
			if saved.LeaseToken != "" || saved.LeaseOwner != "" || !saved.LeaseExpiresAt.IsZero() || saved.TerminalLeaseHash == "" {
				t.Fatalf("terminal state: %+v", saved)
			}
			if strings.Contains(first.Body.String(), claimed.LeaseToken) || strings.Contains(first.Body.String(), "terminal_lease_hash") {
				t.Fatal("replay credential exposed")
			}
			// Restart the service, then replay even beyond the original deadline.
			restarted, err := NewService(st, s.cfg, s.deps)
			if err != nil {
				t.Fatal(err)
			}
			h.Service = restarted
			saved.Deadline = time.Now().UTC().Add(-time.Minute)
			saved.Revision++
			if err := st.cas(ctx, "turn", saved.ID, saved.Revision-1, saved); err != nil {
				t.Fatal(err)
			}
			replay := call("finished", claimed.LeaseToken, `,"evidence_refs":["card:1"]`)
			if replay.Code != 200 {
				t.Fatalf("replay: %d %s", replay.Code, replay.Body)
			}
			for _, tc := range []struct{ text, token string }{{"changed", claimed.LeaseToken}, {"finished", "wrong"}, {"finished", ""}} {
				rr := call(tc.text, tc.token, `,"evidence_refs":["card:1"]`)
				if rr.Code != 409 {
					t.Fatalf("mismatched replay: %d %s", rr.Code, rr.Body)
				}
			}
			if endpoint == "complete" && call("finished", claimed.LeaseToken, `,"evidence_refs":["card:2"]`).Code != 409 {
				t.Fatal("changed evidence accepted")
			}
			var after Turn
			if err := st.get(ctx, "turn", turn.ID, &after); err != nil || !reflect.DeepEqual(saved, after) {
				t.Fatalf("replay mutated durable turn: %+v %v", after, err)
			}
		})
	}
}

func TestRound10ProposalLeaseRecheckedUnderWriteLock(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "c", Title: "Lease race"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "m", Text: "propose"})
	if err != nil {
		t.Fatal(err)
	}
	agent := Principal{WorkspaceID: p.WorkspaceID, ActorID: s.cfg.AgentActorID}
	old := claimTestTurn(t, s, ctx, agent, turn.ID)
	in := DecisionInput{RequestKey: "proposal", WorkRef: "work:1", Scope: "assignment", Instruction: "assign", TargetRevision: "r1"}
	prior, err := s.ProposeForTurn(ctx, agent, turn.ID, in, old.LeaseToken)
	if err != nil {
		t.Fatal(err)
	}
	// Reclaim after the service-level lease check but before the proposal transaction.
	original := s.deps.Authorize
	rotated := false
	s.deps.Authorize = func(ctx context.Context, p Principal, permission, ref string) error {
		if permission == "pm.propose" && !rotated {
			rotated = true
			if _, err := s.ReleaseTurn(ctx, agent, turn.ID, ReleaseInput{RunnerID: turn.ID, LeaseToken: old.LeaseToken}); err != nil {
				return err
			}
			_, err := s.ClaimTurn(ctx, agent, ClaimInput{RunnerID: "replacement"})
			return err
		}
		return original(ctx, p, permission, ref)
	}
	if _, err := s.ProposeForTurn(ctx, agent, turn.ID, in, old.LeaseToken); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale replay race: %v", err)
	}
	in.RequestKey = "new"
	if _, err := s.ProposeForTurn(ctx, agent, turn.ID, in, old.LeaseToken); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale proposal: %v", err)
	}
	decisions, err := listRecords[Decision](ctx, st, "decision", p.WorkspaceID, "", "")
	if err != nil || len(decisions) != 1 || decisions[0].ID != prior.ID {
		t.Fatalf("orphan wrote decisions: %+v %v", decisions, err)
	}
}

func TestRound10ReadOnlyResolutionError(t *testing.T) {
	s, _, p, _ := fixture(t)
	h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return p, nil }}
	for _, path := range []string{"/pm/decisions", "/pm/turns/turn/decisions"} {
		for _, value := range []string{`[]`, `null`, `[{"ref":"card:1"}]`} {
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequest("POST", path, strings.NewReader(`{"payload":{"resolution":`+value+`}}`)))
			if rr.Code != 400 || !strings.Contains(rr.Body.String(), "payload.resolution is read-only; send payload.resolution_refs") {
				t.Fatalf("%s %d %s", path, rr.Code, rr.Body)
			}
		}
	}
}

func TestRound10ConversationBusyMessage(t *testing.T) {
	rr := httptest.NewRecorder()
	writeError(rr, &BusyError{Reason: "conversation", TurnID: "turn-10"})
	var out struct {
		Error struct {
			Code, Message string
			Details       BusyError
		}
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if rr.Code != 429 || out.Error.Code != "busy" || out.Error.Details.Reason != "conversation" || out.Error.Message != "The previous message in this conversation is still queued or being answered (turn turn-10)." {
		t.Fatal(rr.Body.String())
	}
}

func TestRound10GeneratedRequestHelp(t *testing.T) {
	for _, file := range []string{"contracts/gen/meta/commands.json", "contracts/gen/meta/help.json", "cli/internal/registry/help.json"} {
		raw, err := os.ReadFile(filepath.Join("../../..", file))
		if err != nil {
			t.Fatal(err)
		}
		var registry struct {
			Commands []struct {
				ID     string                                               `json:"command_id"`
				Method string                                               `json:"method"`
				Body   struct{ Required, Optional []struct{ Name string } } `json:"body_schema"`
			}
		}
		if err := json.Unmarshal(raw, &registry); err != nil {
			t.Fatal(err)
		}
		found := 0
		for _, cmd := range registry.Commands {
			if cmd.ID != "pm.decisions.create" && cmd.ID != "pm.turns.decisions.create" && cmd.ID != "pm.turns.context" && cmd.ID != "pm.turns.complete" && cmd.ID != "pm.turns.fail" {
				continue
			}
			found++
			fields := map[string]bool{}
			required := map[string]bool{}
			for _, field := range cmd.Body.Required {
				fields[field.Name] = true
				required[field.Name] = true
			}
			for _, field := range cmd.Body.Optional {
				fields[field.Name] = true
			}
			if fields["payload.resolution"] {
				t.Fatalf("%s %s advertises read-only input", file, cmd.ID)
			}
			if strings.HasPrefix(cmd.ID, "pm.turns.") && !required["lease_token"] {
				t.Fatalf("%s %s missing required lease", file, cmd.ID)
			}
			if cmd.ID == "pm.turns.context" {
				if cmd.Method != "POST" || !fields["cursor"] || !fields["query"] || !fields["limit"] {
					t.Fatalf("context input missing: %+v", cmd)
				}
			}
			if strings.Contains(cmd.ID, "decisions.create") && (!fields["payload.phase"] || !fields["payload.resolution_refs"]) {
				t.Fatalf("missing writable payload fields: %+v", cmd)
			}
		}
		if found != 5 {
			t.Fatalf("%s matched %d commands", file, found)
		}
	}
}

func TestRound10ContextBody(t *testing.T) {
	s, _, p, _ := fixture(t)
	ctx := context.Background()
	c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "c", Title: "Context"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "m", Text: "read"})
	if err != nil {
		t.Fatal(err)
	}
	agent := Principal{WorkspaceID: p.WorkspaceID, ActorID: s.cfg.AgentActorID}
	claimed := claimTestTurn(t, s, ctx, agent, turn.ID)
	calls := 0
	s.deps.ReadContextPage = func(_ context.Context, actor Principal, workRef, query, cursor string, limit int) (ContextPage, error) {
		calls++
		if actor.ActorID != p.ActorID || workRef != "" || query != "status" || cursor != "page-2" || limit != 7 {
			t.Fatalf("context args: %+v %q %q %q %d", actor, workRef, query, cursor, limit)
		}
		return ContextPage{Items: []any{"evidence"}, NextCursor: "page-3"}, nil
	}
	h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return agent, nil }}
	for _, token := range []string{"", "wrong", claimed.LeaseToken} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest("POST", "/pm/turns/"+turn.ID+"/context", strings.NewReader(fmt.Sprintf(`{"lease_token":%q,"query":"status","cursor":"page-2","limit":7}`, token))))
		want := 409
		if token == claimed.LeaseToken {
			want = 200
		}
		if rr.Code != want {
			t.Fatalf("context: %d %s", rr.Code, rr.Body)
		}
	}
	if calls != 1 {
		t.Fatalf("unauthorized context reads: %d", calls)
	}
	for _, limit := range []int{0, 51} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest("POST", "/pm/turns/"+turn.ID+"/context", strings.NewReader(fmt.Sprintf(`{"lease_token":%q,"limit":%d}`, claimed.LeaseToken, limit))))
		if rr.Code != 400 {
			t.Fatalf("limit: %d %s", rr.Code, rr.Body)
		}
	}
}
