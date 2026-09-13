package pm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
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

func TestRound7ReplacementAttribution(t *testing.T) {
	for _, kind := range []string{"human_replaces_pm", "pm_replaces_human", "legacy"} {
		t.Run(kind, func(t *testing.T) {
			s, st, p, _ := fixture(t)
			ctx := context.Background()
			c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: "c", Title: "Discuss"})
			if err != nil {
				t.Fatal(err)
			}
			turn, err := s.PostMessage(ctx, p, c.ID, MessageInput{RequestKey: "m", Text: "Propose"})
			if err != nil {
				t.Fatal(err)
			}
			agent := Principal{WorkspaceID: p.WorkspaceID, ActorID: "pm-agent"}
			claimTestTurn(t, s, ctx, agent, turn.ID)
			in := DecisionInput{RequestKey: "first", WorkRef: "work:1", Scope: "assignment", Instruction: "Assign", TargetRevision: "r1"}
			var prior Decision
			if kind == "human_replaces_pm" {
				claimTestTurn(t, s, ctx, agent, turn.ID)
				prior, err = s.ProposeForTurn(ctx, agent, turn.ID, in)
			} else {
				prior, err = s.ProposeDecision(ctx, p, in)
			}
			if err != nil {
				t.Fatal(err)
			}
			if prior.Supersedes != "" || prior.SupersedesProposedBy != "" || prior.SupersedesOriginKind != "" {
				t.Fatal(prior)
			}
			if kind == "legacy" {
				prior.ProposedBy, prior.OriginKind = "", ""
				prior.Revision++
				if err := st.cas(ctx, "decision", prior.ID, prior.Revision-1, prior); err != nil {
					t.Fatal(err)
				}
			}
			caller, path := p, "/pm/decisions"
			wantStatus := http.StatusCreated
			if kind == "pm_replaces_human" {
				caller, path = agent, "/pm/turns/"+turn.ID+"/decisions"
				wantStatus = http.StatusOK
			}
			h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return caller, nil }}
			in.RequestKey = "replacement"
			raw, err := json.Marshal(in)
			if err != nil {
				t.Fatal(err)
			}
			var replacement Decision
			for i := 0; i < 2; i++ {
				rr := httptest.NewRecorder()
				h.ServeHTTP(rr, httptest.NewRequest("POST", path, strings.NewReader(string(raw))))
				if rr.Code != wantStatus {
					t.Fatalf("%d %s", rr.Code, rr.Body)
				}
				var got Decision
				if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
					t.Fatal(err)
				}
				if got.ID == prior.ID || got.Supersedes != prior.ID || got.SupersedesProposedBy != prior.ProposedBy || got.SupersedesOriginKind != prior.OriginKind {
					t.Fatalf("%+v", got)
				}
				if kind == "legacy" && (strings.Contains(rr.Body.String(), `"supersedes_proposed_by"`) || strings.Contains(rr.Body.String(), `"supersedes_origin_kind"`)) {
					t.Fatalf("invented provenance: %s", rr.Body)
				}
				if i == 1 && !reflect.DeepEqual(replacement, got) {
					t.Fatal("replay lost attribution")
				}
				replacement = got
			}
			var old Decision
			if err := st.get(ctx, "decision", prior.ID, &old); err != nil {
				t.Fatal(err)
			}
			if old.SupersededByProposedBy != replacement.ProposedBy || old.SupersededByOriginKind != replacement.OriginKind || old.Status != Superseded || old.SupersededBy != replacement.ID || old.ProposedBy != prior.ProposedBy || old.OriginKind != prior.OriginKind {
				t.Fatal(old)
			}
			s, err = NewService(st, s.cfg, s.deps)
			if err != nil {
				t.Fatal(err)
			}
			got, err := s.decision(ctx, p, replacement.ID, "pm.read")
			if err != nil || got.Supersedes != prior.ID || got.SupersedesProposedBy != prior.ProposedBy || got.SupersedesOriginKind != prior.OriginKind {
				t.Fatalf("persisted replacement: %+v %v", got, err)
			}
			oldRead, err := s.decision(ctx, p, prior.ID, "pm.read")
			if err != nil || oldRead.SupersededByProposedBy != replacement.ProposedBy || oldRead.SupersededByOriginKind != replacement.OriginKind {
				t.Fatalf("old attribution: %+v %v", oldRead, err)
			}
			page, err := s.DecisionPage(ctx, p, 50, "")
			if err != nil || len(page.Items) != 2 || page.Items[0].Supersedes != prior.ID || page.Items[1].Status != Superseded || page.Items[1].SupersededByProposedBy != replacement.ProposedBy || page.Items[1].SupersededByOriginKind != replacement.OriginKind {
				t.Fatalf("page: %+v %v", page, err)
			}
		})
	}
}
