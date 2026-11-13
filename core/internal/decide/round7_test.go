package decide

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRound7DeclinedDecisionLifecycle(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "legacy"}[legacy], func(t *testing.T) {
			s, st, p, _ := fixture(t)
			ctx := context.Background()
			in := DecisionInput{RequestKey: "decline", WorkRef: "work:1", Scope: "assignment", Instruction: "Assign", TargetRevision: "r1"}
			d, err := s.ProposeDecision(ctx, p, in)
			if err != nil {
				t.Fatal(err)
			}
			answer := AnswerInput{Revision: d.Revision, Text: "No"}
			h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return p, nil }}
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequest("POST", "/pm/decisions/"+d.ID+"/answer", strings.NewReader(`{"revision":1,"approve":false,"text":"No"}`)))
			if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"status":"declined"`) {
				t.Fatalf("%d %s", rr.Code, rr.Body)
			}
			if err := st.get(ctx, "decision", d.ID, &d); err != nil {
				t.Fatal(err)
			}
			if d.Status != Declined || d.SupersededBy != "" || d.ActionID != "" || d.CanAnswer {
				t.Fatal(d)
			}
			if legacy {
				// Reconstruct the old on-disk rejection shape without changing its revision.
				if _, err := st.db.Exec(`UPDATE pm_records SET body=json_set(body,'$.status','superseded') WHERE kind='decision' AND id=?`, d.ID); err != nil {
					t.Fatal(err)
				}
			}
			var before string
			if err := st.db.QueryRow(`SELECT body FROM pm_records WHERE kind='decision' AND id=?`, d.ID).Scan(&before); err != nil {
				t.Fatal(err)
			}
			// Restore the service and exercise public reads and both replay paths.
			s, err = NewService(st, s.cfg, s.deps)
			if err != nil {
				t.Fatal(err)
			}
			h.Service = s
			for _, path := range []string{"/pm/decisions/" + d.ID, "/pm/decisions"} {
				rr := httptest.NewRecorder()
				h.ServeHTTP(rr, httptest.NewRequest("GET", path, nil))
				if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"status":"declined"`) || strings.Contains(rr.Body.String(), `"can_answer":true`) {
					t.Fatalf("%s: %d %s", path, rr.Code, rr.Body)
				}
			}
			rows, err := s.ListDecisions(ctx, p)
			if err != nil || len(rows) != 1 || rows[0].Status != Declined {
				t.Fatalf("%+v %v", rows, err)
			}
			for _, replay := range []func() (Decision, error){
				func() (Decision, error) { return s.AnswerDecision(ctx, p, d.ID, answer) },
				func() (Decision, error) { return s.ProposeDecision(ctx, p, in) },
			} {
				got, err := replay()
				if err != nil || got.Status != Declined || got.Revision != d.Revision || got.AnsweredBy != p.ActorID || got.Answer != "No" {
					t.Fatalf("%+v %v", got, err)
				}
			}
			for _, approve := range []bool{false, true} {
				_, err := s.AnswerDecision(ctx, p, d.ID, AnswerInput{Revision: d.Revision, Approve: approve, Text: "Changed"})
				var superseded *SupersededDecisionError
				if !errors.Is(err, ErrConflict) || errors.As(err, &superseded) {
					t.Fatalf("terminal answer: %v", err)
				}
			}
			if _, err := s.DispatchDecision(ctx, p, d.ID); err != ErrConflict {
				t.Fatalf("dispatch: %v", err)
			}
			// A losing answer CAS must not mislabel a legacy decline as a replacement.
			var superseded *SupersededDecisionError
			if err := st.answer(ctx, d, nil, answer.Revision); !errors.Is(err, ErrConflict) || errors.As(err, &superseded) {
				t.Fatalf("raced decline: %v", err)
			}
			var after string
			if err := st.db.QueryRow(`SELECT body FROM pm_records WHERE kind='decision' AND id=?`, d.ID).Scan(&after); err != nil {
				t.Fatal(err)
			}
			if after != before {
				t.Fatal("reads/replays rewrote the durable rejection")
			}
			actions, err := s.ListActions(ctx, p)
			if err != nil || len(actions) != 0 {
				t.Fatalf("decline created actions: %+v %v", actions, err)
			}
		})
	}
}
