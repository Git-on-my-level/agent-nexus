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

func TestRound6Acknowledgement(t *testing.T) {
	for _, state := range []Status{Failed, Unknown, Pending, Sending, Delivered, Acknowledged, Reported, Verified} {
		t.Run(string(state), func(t *testing.T) {
			s, st, p, _ := fixture(t)
			ctx := context.Background()
			d := round4Approved(t, s, p)
			var a Action
			if err := st.get(ctx, "action", d.ActionID, &a); err != nil {
				t.Fatal(err)
			}
			a.Status = state
			a.Receipt = Receipt{Status: state, Detail: "original receipt"}
			now := time.Now().UTC()
			a.Attempts = []Attempt{{Status: state, StartedAt: now, SentAt: &now, Receipt: a.Receipt}}
			a.Revision++
			if err := st.cas(ctx, "action", a.ID, a.Revision-1, a); err != nil {
				t.Fatal(err)
			}
			s.deps.Reconcile = func(context.Context, Action) (Receipt, error) {
				return Receipt{Status: Unknown, Detail: "inconclusive"}, nil
			}
			h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return p, nil }}
			call := func() *httptest.ResponseRecorder {
				rr := httptest.NewRecorder()
				h.ServeHTTP(rr, httptest.NewRequest("POST", "/pm/actions/"+a.ID+"/acknowledge", strings.NewReader("{}")))
				return rr
			}
			// A different authorized workspace human still cannot acknowledge.
			p.ActorID = "second-human"
			if rr := call(); rr.Code != 403 {
				t.Fatalf("other actor: %d %s", rr.Code, rr.Body)
			}
			p.ActorID = "human"
			rr := call()
			if state != Failed && state != Unknown {
				if rr.Code != 409 {
					t.Fatalf("non-failed: %d %s", rr.Code, rr.Body)
				}
				return
			}
			if rr.Code != 200 {
				t.Fatalf("%d %s", rr.Code, rr.Body)
			}
			var got Action
			if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Status != Acknowledged || got.AcknowledgedBy != p.ActorID || got.AcknowledgedAt == nil || !reflect.DeepEqual(got.Receipt, a.Receipt) || !reflect.DeepEqual(got.Attempts, a.Attempts) {
				t.Fatalf("%+v", got)
			}
			if replay := call(); replay.Body.String() != rr.Body.String() {
				t.Fatalf("non-idempotent replay: %s", replay.Body)
			}
			// Read-back can refresh evidence without erasing human handling.
			again, err := s.ReconcileAction(ctx, p, a.ID)
			if err != nil || again.Status != Acknowledged || again.AcknowledgedBy != got.AcknowledgedBy || !again.AcknowledgedAt.Equal(*got.AcknowledgedAt) || again.Receipt.Detail != "inconclusive" {
				t.Fatalf("reconcile after ack: %+v %v", again, err)
			}
			if err = st.get(ctx, "action", a.ID, &again); err != nil || again.AcknowledgedAt == nil || !reflect.DeepEqual(again.Attempts, a.Attempts) {
				t.Fatalf("persisted: %+v %v", again, err)
			}
		})
	}
}

func TestRound6UnknownAcknowledgementReadBackGate(t *testing.T) {
	for _, read := range []string{"advanced", "unavailable", "no_reader"} {
		t.Run(read, func(t *testing.T) {
			s, st, p, _ := fixture(t)
			ctx := context.Background()
			d := round4Approved(t, s, p)
			var a Action
			if err := st.get(ctx, "action", d.ActionID, &a); err != nil {
				t.Fatal(err)
			}
			a.Status = Unknown
			a.Receipt = Receipt{Status: Unknown, Detail: "original"}
			a.Revision++
			if err := st.cas(ctx, "action", a.ID, a.Revision-1, a); err != nil {
				t.Fatal(err)
			}
			if read != "no_reader" {
				s.deps.Reconcile = func(context.Context, Action) (Receipt, error) {
					if read == "unavailable" {
						return Receipt{}, ErrUnavailable
					}
					return Receipt{Status: Failed, Detail: "now resolved"}, nil
				}
			}
			got, err := s.AcknowledgeAction(ctx, p, a.ID)
			if read == "advanced" {
				if !errors.Is(err, ErrConflict) {
					t.Fatal(err)
				}
			} else if err != nil || got.Status != Acknowledged {
				t.Fatalf("%+v %v", got, err)
			}
			var saved Action
			if err := st.get(ctx, "action", a.ID, &saved); err != nil || !reflect.DeepEqual(saved.Receipt, a.Receipt) {
				t.Fatalf("receipt changed: %+v %v", saved, err)
			}
		})
	}
}

func TestRound6HandlerAuthenticationAndAuthorization(t *testing.T) {
	s, _, p, _ := fixture(t)
	for _, header := range []string{"", "Bearer expired", "Bearer invalid"} {
		h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return Principal{}, errors.New("authentication failed") }}
		rr := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/pm/actions", nil)
		r.Header.Set("Authorization", header)
		h.ServeHTTP(rr, r)
		code := "invalid_token"
		if header == "" {
			code = "auth_required"
		}
		if rr.Code != 401 || !strings.Contains(rr.Body.String(), code) || !strings.Contains(rr.Body.String(), `"recoverable":true`) {
			t.Fatalf("%d %s", rr.Code, rr.Body)
		}
	}
	s.deps.Authorize = func(context.Context, Principal, string, string) error { return ErrForbidden }
	rr := httptest.NewRecorder()
	Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return p, nil }}.ServeHTTP(rr, httptest.NewRequest("GET", "/pm/actions", nil))
	if rr.Code != 403 {
		t.Fatalf("%d %s", rr.Code, rr.Body)
	}
}
