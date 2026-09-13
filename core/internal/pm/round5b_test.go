package pm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPTurnClosedLifecycle(t *testing.T) {
	for _, state := range []string{"live", "expired-pending", "expired-sending", "expired-unknown", "expired-swept", "delivered", "failed"} {
		for _, op := range []struct {
			path, method, body string
			success            int
		}{
			{"context", "GET", "", 200},
			{"decisions", "POST", `{"request_key":"proposal","work_ref":"work:1","scope":"work.phase","instruction":"Move","target_revision":"r1","payload":{"phase":"ready"}}`, 200},
			{"complete", "POST", `{"text":"Done"}`, 200},
			{"fail", "POST", `{"reason":"Harness failed"}`, 200},
		} {
			t.Run(state+"/"+op.path, func(t *testing.T) {
				s, st, p, _ := fixture(t)
				ctx := context.Background()
				c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "c", Title: "Lifecycle", WorkRef: "work:1"})
				if err != nil {
					t.Fatal(err)
				}
				turn, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "m", Text: "Status?"})
				if err != nil {
					t.Fatal(err)
				}
				old := turn.Revision
				if strings.HasPrefix(state, "expired-") {
					turn.Deadline = time.Now().UTC().Add(-time.Minute)
				}
				switch state {
				case "expired-pending":
					turn.Status = Pending
				case "expired-unknown":
					turn.Status = Unknown
				case "delivered":
					turn.Status, turn.Response = Delivered, "Earlier response"
				case "failed":
					turn.Status, turn.Failure = Failed, "Earlier failure"
				}
				turn.Revision++
				if err := st.cas(ctx, "turn", turn.ID, old, turn); err != nil {
					t.Fatal(err)
				}
				if state == "expired-swept" {
					if err := s.ExpireTurns(ctx, time.Now()); err != nil {
						t.Fatal(err)
					}
				}
				agent := Principal{WorkspaceID: p.WorkspaceID, ActorID: "pm-agent"}
				h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return agent, nil }}
				// Repeated requests to an expired turn must retain the same typed failure.
				attempts := 1
				if state != "live" {
					attempts = 2
				}
				for i := 0; i < attempts; i++ {
					w := httptest.NewRecorder()
					h.ServeHTTP(w, httptest.NewRequest(op.method, "/pm/turns/"+turn.ID+"/"+op.path, strings.NewReader(op.body)))
					if state == "live" {
						if w.Code != op.success {
							t.Fatalf("live: %d %s", w.Code, w.Body.String())
						}
						continue
					}
					var out struct {
						Error struct {
							Code, Message string
							Details       TurnClosedError
						}
					}
					if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
						t.Fatal(err)
					}
					wantStatus := Failed
					if state == "delivered" {
						wantStatus = Delivered
					}
					if w.Code != 409 || out.Error.Code != "turn_closed" || out.Error.Details.TurnID != turn.ID || !out.Error.Details.Deadline.Equal(turn.Deadline) || out.Error.Details.Status != wantStatus {
						t.Fatalf("closed: %d %s", w.Code, w.Body.String())
					}
					if strings.HasPrefix(state, "expired-") && (out.Error.Details.FailureKind != "expired" || !strings.Contains(out.Error.Message, "expired at "+turn.Deadline.Format(time.RFC3339Nano))) {
						t.Fatalf("message: %q", out.Error.Message)
					}
				}
				if state != "live" {
					var stored Turn
					if err := st.get(ctx, "turn", turn.ID, &stored); err != nil {
						t.Fatal(err)
					}
					if strings.HasPrefix(state, "expired-") && (stored.Status != Failed || stored.Failure != turnDeadlineFailure || stored.LeaseToken != "") {
						t.Fatalf("expiry not persisted: %+v", stored)
					}
					decisions, err := s.ListDecisions(ctx, p)
					if err != nil || len(decisions) != 0 {
						t.Fatalf("closed turn proposed: %+v %v", decisions, err)
					}
				}
			})
		}
	}
}

func TestTurnClosedErrorIsDistinctAndWrapSafe(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", closedTurnError(Turn{ID: "turn-1", Deadline: time.Now(), Status: Failed, Failure: turnDeadlineFailure}))
	var closed *TurnClosedError
	if !errors.Is(err, ErrTurnClosed) || errors.Is(err, ErrStale) || !errors.As(err, &closed) {
		t.Fatalf("typed error: %v", err)
	}
	w := httptest.NewRecorder()
	writeError(w, err)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"turn_id":"turn-1"`) || !strings.Contains(w.Body.String(), `"code":"turn_closed"`) {
		t.Fatalf("wrapped mapping: %d %s", w.Code, w.Body.String())
	}
}
