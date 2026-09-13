package pm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRound5ExecutionOutcomes(t *testing.T) {
	for _, kind := range []string{"prewrite", "postwrite_verified", "postwrite_failed", "readback_error", "external"} {
		t.Run(kind, func(t *testing.T) {
			s, st, p, _ := fixture(t)
			d := round4Approved(t, s, p)
			calls := 0
			s.deps.Execute = func(context.Context, Action) (Receipt, error) {
				cause := errors.New("mutation failed: fixture cause")
				if kind == "external" {
					return Receipt{}, cause
				}
				return Receipt{}, &NativeExecutionError{Cause: cause, WriteStarted: kind != "prewrite"}
			}
			s.deps.Reconcile = func(ctx context.Context, a Action) (Receipt, error) {
				calls++
				if ctx.Err() != nil {
					t.Fatal(ctx.Err())
				}
				if kind == "readback_error" {
					return Receipt{}, errors.New("read unavailable")
				}
				if kind == "postwrite_verified" {
					return Receipt{Status: Verified, ExternalID: a.ID, EvidenceRefs: []string{a.WorkRef}, IndependentlyVerified: true}, nil
				}
				return Receipt{Status: Failed, Detail: "canonical fields do not match"}, nil
			}
			a, err := s.DispatchDecision(context.Background(), p, d.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := Failed
			if kind == "postwrite_verified" {
				want = Verified
			}
			if kind == "external" || kind == "readback_error" {
				want = Unknown
			}
			if a.Status != want || (a.Attempts[0].SentAt == nil) != (kind == "prewrite") {
				t.Fatalf("%+v", a)
			}
			if kind == "prewrite" || kind == "postwrite_failed" {
				if !strings.Contains(a.Receipt.Detail, "fixture cause") {
					t.Fatal(a.Receipt)
				}
			}
			if kind == "external" && a.Receipt.Detail != "Source handoff outcome is unknown; reconcile to establish whether it was delivered. This approval will not be sent again; if another send is needed, a fresh proposal and a new approval are required." {
				t.Fatal(a.Receipt)
			}
			if kind != "external" && strings.Contains(a.Receipt.Detail, "Source handoff") {
				t.Fatal(a.Receipt)
			}
			if (calls == 0) != (kind == "prewrite" || kind == "external") {
				t.Fatal(calls)
			}
			var saved Action
			if err = st.get(context.Background(), "action", a.ID, &saved); err != nil || saved.Status != want || (saved.Attempts[0].SentAt == nil) != (kind == "prewrite") {
				t.Fatalf("saved=%+v err=%v", saved, err)
			}
			if kind == "prewrite" {
				if _, err = s.ReconcileAction(context.Background(), p, a.ID); !errors.Is(err, ErrNothingDelivered) {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestRound5SupersededConflictDetails(t *testing.T) {
	s, st, p, _ := fixture(t)
	in := DecisionInput{RequestKey: "first", WorkRef: "work:1", Scope: "work.phase", TargetRevision: "1", Instruction: "ready", Payload: &ActionPayload{Phase: "ready"}}
	d, err := s.ProposeDecision(context.Background(), p, in)
	if err != nil {
		t.Fatal(err)
	}
	in.RequestKey = "second"
	in.Payload = &ActionPayload{Phase: "review"}
	replacement, err := s.ProposeDecision(context.Background(), p, in)
	if err != nil {
		t.Fatal(err)
	}
	// The answer CAS must also identify a replacement created after the service read.
	var conflict *SupersededDecisionError
	if err := st.answer(context.Background(), d, nil, d.Revision); !errors.As(err, &conflict) || conflict.SupersededBy != replacement.ID {
		t.Fatalf("raced answer: %v", err)
	}
	h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return p, nil }}
	for _, endpoint := range []string{"answer", "dispatch"} {
		rr := httptest.NewRecorder()
		body := "{}"
		if endpoint == "answer" {
			body = `{"revision":1,"approve":true,"text":"yes"}`
		}
		h.ServeHTTP(rr, httptest.NewRequest("POST", "/pm/decisions/"+d.ID+"/"+endpoint, strings.NewReader(body)))
		var response struct {
			Error struct {
				Code    string
				Details map[string]string
			}
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		if rr.Code != 409 || response.Error.Code != "conflict" || response.Error.Details["superseded_by"] != replacement.ID || response.Error.Details["status"] != "superseded" {
			t.Fatalf("%s: %d %s", endpoint, rr.Code, rr.Body)
		}
	}
}

func TestRound5LegacyInvalidPayloadFailsBeforeSend(t *testing.T) {
	s, st, p, _ := fixture(t)
	d := round4Approved(t, s, p)
	var a Action
	if err := st.get(context.Background(), "action", d.ActionID, &a); err != nil {
		t.Fatal(err)
	}
	// Reconstruct a legacy approved phase action without structured payload.
	d.Scope = "work.phase"
	a.Scope = d.Scope
	d.Revision++
	a.Revision++
	if err := st.cas(context.Background(), "decision", d.ID, d.Revision-1, d); err != nil {
		t.Fatal(err)
	}
	if err := st.cas(context.Background(), "action", a.ID, a.Revision-1, a); err != nil {
		t.Fatal(err)
	}
	s.deps.Execute = func(context.Context, Action) (Receipt, error) {
		t.Fatal("invalid payload executed")
		return Receipt{}, nil
	}
	a, err := s.DispatchDecision(context.Background(), p, d.ID)
	if err != nil || a.Status != Failed || a.Attempts[0].SentAt != nil || a.Receipt.Detail != "Invalid work.phase payload: phase must be supported and resolution_refs are required only for done. This approval will not be sent; a fresh proposal with a valid payload and a new approval are needed." {
		t.Fatalf("%+v %v", a, err)
	}
}
