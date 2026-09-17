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

func TestRound6BindingKeysetPaging(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	var ids []string
	for i := 0; i < 5; i++ {
		b, err := s.BindChannel(ctx, p, Binding{WorkspaceID: p.WorkspaceID, ActorID: fmt.Sprint("actor-", i), Origin: Origin{Transport: "telegram", TenantID: "bot", ChannelID: fmt.Sprint(i), ExternalUserID: "user"}, Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, b.ID)
	}
	h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return p, nil }}
	get := func(query string) Page[Binding] {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest("GET", "/pm/bindings?"+query, nil))
		if rr.Code != 200 {
			t.Fatalf("%d %s", rr.Code, rr.Body)
		}
		var page Page[Binding]
		if err := json.Unmarshal(rr.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		return page
	}
	first := get("limit=2")
	if len(first.Items) != 2 || !first.HasMore || first.NextCursor == "" {
		t.Fatalf("%+v", first)
	}
	// Deleting the cursor row and adding a new row cannot duplicate/skip old rows.
	if _, err := st.db.Exec(`DELETE FROM pm_records WHERE kind='binding' AND id=?`, first.Items[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BindChannel(ctx, p, Binding{WorkspaceID: p.WorkspaceID, ActorID: p.ActorID, Origin: Origin{Transport: "telegram", TenantID: "bot", ChannelID: "new", ExternalUserID: "user"}}); err != nil {
		t.Fatal(err)
	}
	second := get("limit=2&cursor=" + first.NextCursor)
	third := get("limit=2&cursor=" + second.NextCursor)
	got := []string{first.Items[0].ID, first.Items[1].ID}
	for _, page := range []Page[Binding]{second, third} {
		for _, b := range page.Items {
			got = append(got, b.ID)
		}
	}
	want := []string{ids[4], ids[3], ids[2], ids[1], ids[0]}
	if !reflect.DeepEqual(got, want) || third.HasMore || third.NextCursor != "" {
		t.Fatalf("got %v want %v", got, want)
	}
	for _, query := range []string{"limit=0", "limit=201", "cursor=bad"} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest("GET", "/pm/bindings?"+query, nil))
		if rr.Code != 400 {
			t.Fatalf("%s: %d", query, rr.Code)
		}
	}
	other := p
	other.ActorID = "second-human"
	if _, err := s.BindingPage(ctx, other, 2, first.NextCursor); !errors.Is(err, ErrInvalid) {
		t.Fatalf("cross actor cursor: %v", err)
	}
	if _, err := s.DecisionPage(ctx, p, 2, first.NextCursor); !errors.Is(err, ErrInvalid) {
		t.Fatalf("cross kind cursor: %v", err)
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
