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

func TestRound8AcknowledgedActionReconciles(t *testing.T) {
	for _, outcome := range []Status{Verified, Failed} {
		t.Run(string(outcome), func(t *testing.T) {
			s, st, p, _ := fixture(t)
			ctx := context.Background()
			d := round4Approved(t, s, p)
			s.deps.Execute = func(context.Context, Action) (Receipt, error) {
				return Receipt{Status: Unknown, Detail: "write committed, read timed out"}, nil
			}
			a, err := s.DispatchDecision(ctx, p, d.ID)
			if err != nil || a.Status != Unknown || !hasSentAttempt(a) {
				t.Fatalf("dispatch: %+v %v", a, err)
			}
			s.deps.Reconcile = func(context.Context, Action) (Receipt, error) { return Receipt{}, context.DeadlineExceeded }
			ack, err := s.AcknowledgeAction(ctx, p, a.ID)
			if err != nil || ack.Status != Acknowledged {
				t.Fatalf("ack: %+v %v", ack, err)
			}
			receipt := Receipt{Status: outcome, ExternalID: "source-1", EvidenceRefs: []string{"card:source-1"}, IndependentlyVerified: true, Detail: "later source state"}
			calls := 0
			s.deps.Reconcile = func(context.Context, Action) (Receipt, error) { calls++; return receipt, nil }
			got, err := s.ReconcileAction(ctx, p, a.ID)
			if err != nil || calls != 1 || got.Status != outcome || got.ReconciliationConflict || !reflect.DeepEqual(got.Receipt, receipt) {
				t.Fatalf("reconcile: %+v %v calls=%d", got, err, calls)
			}
			if got.AcknowledgedBy != ack.AcknowledgedBy || !got.AcknowledgedAt.Equal(*ack.AcknowledgedAt) || !reflect.DeepEqual(got.Attempts, a.Attempts) {
				t.Fatalf("evidence/ack lost: %+v", got)
			}
			var saved Action
			if err := st.get(ctx, "action", a.ID, &saved); err != nil || !reflect.DeepEqual(saved, got) {
				t.Fatalf("persistence: %+v %v", saved, err)
			}
			replay, err := s.AcknowledgeAction(ctx, p, a.ID)
			if err != nil || !reflect.DeepEqual(replay, got) {
				t.Fatalf("ack replay: %+v %v", replay, err)
			}
		})
	}
}

func TestRound8AcknowledgedUnsentFailureRefusesReconcile(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	d := round4Approved(t, s, p)
	var a Action
	if err := st.get(ctx, "action", d.ActionID, &a); err != nil {
		t.Fatal(err)
	}
	a.Status, a.Receipt = Failed, Receipt{Status: Failed, Detail: "rejected before handoff"}
	a.Attempts = []Attempt{{StartedAt: time.Now().UTC(), Status: Failed, Receipt: a.Receipt}}
	a.Revision++
	if err := st.cas(ctx, "action", a.ID, a.Revision-1, a); err != nil {
		t.Fatal(err)
	}
	ack, err := s.AcknowledgeAction(ctx, p, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	s.deps.Reconcile = func(context.Context, Action) (Receipt, error) {
		t.Fatal("unsent action read back")
		return Receipt{}, nil
	}
	if _, err := s.ReconcileAction(ctx, p, a.ID); !errors.Is(err, ErrNothingDelivered) {
		t.Fatalf("reconcile: %v", err)
	}
	var saved Action
	if err := st.get(ctx, "action", a.ID, &saved); err != nil || !reflect.DeepEqual(saved, ack) {
		t.Fatalf("mutated: %+v %v", saved, err)
	}
}

func TestRound8LeaseHTTP(t *testing.T) {
	s, st, human, _ := fixture(t)
	ctx := context.Background()
	s.deps.Dispatch = nil
	s.cfg.MaxConcurrent = 1
	c, err := s.CreateConversation(ctx, human, CreateConversation{RequestKey: "r8", Title: "Queue"})
	if err != nil {
		t.Fatal(err)
	}
	turn, err := s.PostMessage(ctx, human, c.ID, MessageInput{RequestKey: "r8", Text: "Work"})
	if err != nil {
		t.Fatal(err)
	}
	caller := Principal{WorkspaceID: human.WorkspaceID, ActorID: "pm-agent"}
	h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return caller, nil }}
	request := func(path, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest("POST", "/pm/turns/"+path, strings.NewReader(body)))
		if rr.Code != status {
			t.Fatalf("%s: %d %s", path, rr.Code, rr.Body)
		}
		return rr
	}
	first := request("claim", `{"runner_id":"runner-a"}`, 200)
	var claimed Turn
	if err := json.Unmarshal(first.Body.Bytes(), &claimed); err != nil {
		t.Fatal(err)
	}
	if claimed.ID != turn.ID || claimed.LeaseToken == "" {
		t.Fatalf("claim: %+v", claimed)
	}
	// Recovery is a replay even at capacity: token, expiry, revision and time stay fixed.
	recovered := request("claim", `{"runner_id":"runner-a"}`, 200)
	if first.Body.String() != recovered.Body.String() {
		t.Fatalf("recovery changed lease: %s", recovered.Body)
	}
	request("claim", `{"runner_id":"runner-b"}`, 204)
	release := turn.ID + "/release"
	body := fmt.Sprintf(`{"runner_id":"runner-a","lease_token":%q}`, claimed.LeaseToken)
	request(release, fmt.Sprintf(`{"runner_id":"runner-b","lease_token":%q}`, claimed.LeaseToken), 403)
	request(release, `{"runner_id":"runner-a","lease_token":"wrong"}`, 403)
	request(release, `{"runner_id":"runner-a"}`, 400)
	caller = human
	request(release, body, 403)
	caller = Principal{WorkspaceID: "elsewhere", ActorID: "pm-agent"}
	request(release, body, 403)
	caller = Principal{WorkspaceID: human.WorkspaceID, ActorID: "pm-agent"}
	released := request(release, body, 200)
	var out map[string]any
	if err := json.Unmarshal(released.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["status"] != "sending" || out["claimed"] != false || out["claimed_at"] == nil {
		t.Fatalf("release: %s", released.Body)
	}
	for _, key := range []string{"lease_token", "lease_owner", "lease_expires_at"} {
		if _, ok := out[key]; ok {
			t.Fatalf("credential leak: %s", released.Body)
		}
	}
	var saved Turn
	if err := st.get(ctx, "turn", turn.ID, &saved); err != nil || saved.LeaseToken != "" || saved.LeaseOwner != "" || !saved.LeaseExpiresAt.IsZero() || !saved.ClaimedAt.Equal(*claimed.ClaimedAt) {
		t.Fatalf("release persistence: %+v %v", saved, err)
	}
	request(release, body, 409)
	var next Turn
	if err := json.Unmarshal(request("claim", `{"runner_id":"runner-b"}`, 200).Body.Bytes(), &next); err != nil {
		t.Fatal(err)
	}
	if next.ID != claimed.ID || next.LeaseToken == claimed.LeaseToken {
		t.Fatalf("reclaim: %+v", next)
	}
	request(release, body, 403)
	// A lease can expire before the turn deadline, allowing a fresh owner/token.
	next.LeaseExpiresAt = time.Now().UTC().Add(-time.Second)
	next.Revision++
	if err := st.cas(ctx, "turn", next.ID, next.Revision-1, next); err != nil {
		t.Fatal(err)
	}
	request(release, fmt.Sprintf(`{"runner_id":"runner-b","lease_token":%q}`, next.LeaseToken), 409)
	var reclaimed Turn
	if err := json.Unmarshal(request("claim", `{"runner_id":"runner-c"}`, 200).Body.Bytes(), &reclaimed); err != nil {
		t.Fatal(err)
	}
	if reclaimed.ID != turn.ID || reclaimed.LeaseToken == next.LeaseToken || reclaimed.LeaseOwner != "runner-c" {
		t.Fatalf("expired reclaim: %+v", reclaimed)
	}
	// The turn deadline still wins over lease recovery and release.
	reclaimed.Deadline = time.Now().UTC().Add(-time.Second)
	reclaimed.Revision++
	if err := st.cas(ctx, "turn", turn.ID, reclaimed.Revision-1, reclaimed); err != nil {
		t.Fatal(err)
	}
	closed := request(release, fmt.Sprintf(`{"runner_id":"runner-c","lease_token":%q}`, reclaimed.LeaseToken), 409)
	if !strings.Contains(closed.Body.String(), `"failure_kind":"expired"`) || !strings.Contains(closed.Body.String(), "expired at "+reclaimed.Deadline.Format(time.RFC3339Nano)) {
		t.Fatalf("expiry: %s", closed.Body)
	}
	request("claim", `{"runner_id":"runner-c"}`, 204)
}

func TestRound8TurnExpiryClassification(t *testing.T) {
	for _, mode := range []string{"read", "sweep", "legacy", "ordinary"} {
		t.Run(mode, func(t *testing.T) {
			s, st, p, _ := fixture(t)
			ctx := context.Background()
			c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "expiry", Title: "Expiry"})
			if err != nil {
				t.Fatal(err)
			}
			turn := Turn{ConversationID: c.ID, ID: "expiry", WorkspaceID: p.WorkspaceID, ActorID: p.ActorID, AgentActorID: "pm-agent", Status: Sending, Deadline: time.Now().UTC().Add(-time.Minute), Revision: 1}
			if mode == "legacy" {
				turn.Status, turn.Failure = Failed, turnDeadlineFailure
			}
			if mode == "ordinary" {
				turn.Status, turn.Failure = Failed, "harness rejected request"
			}
			if _, err := st.insert(ctx, "turn", turn.ID, p.WorkspaceID, p.ActorID, "", turn); err != nil {
				t.Fatal(err)
			}
			if mode == "sweep" {
				if err := s.ExpireTurns(ctx, time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			rr := httptest.NewRecorder()
			Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return p, nil }}.ServeHTTP(rr, httptest.NewRequest("GET", "/pm/turns/"+turn.ID, nil))
			var got Turn
			if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil || rr.Code != 200 {
				t.Fatalf("read: %d %s %v", rr.Code, rr.Body, err)
			}
			want := "expired"
			if mode == "ordinary" {
				want = ""
			}
			if got.FailureKind != want || got.Status != Failed {
				t.Fatalf("classification: %+v", got)
			}
			if mode == "read" || mode == "sweep" {
				var saved Turn
				if err := st.get(ctx, "turn", turn.ID, &saved); err != nil || saved.FailureKind != "expired" {
					t.Fatalf("not durable: %+v %v", saved, err)
				}
			}
		})
	}
}
