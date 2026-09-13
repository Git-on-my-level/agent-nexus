package pm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestPMListPagesNewestFirst(t *testing.T) {
	for _, kind := range []string{"decision", "action", "conversation"} {
		t.Run(kind, func(t *testing.T) {
			s, st, p, _ := fixture(t)
			ctx := context.Background()
			type expected struct {
				id  string
				at  time.Time
				row int
			}
			want := make([]expected, 0, 60)
			base := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
			for i := 0; i < 60; i++ {
				// Shuffled timestamps, ties, whole seconds and nanoseconds: rowid alone
				// and SQLite's millisecond-rounded dates cannot produce this order.
				at := base.Add(time.Duration(((i*17)%60)/3) * time.Nanosecond)
				key := fmt.Sprint(i)
				var id string
				if kind == "conversation" {
					c, err := s.CreateConversation(ctx, p, CreateConversation{RequestKey: key, Title: key})
					if err != nil {
						t.Fatal(err)
					}
					id = c.ID
				} else {
					d, err := s.ProposeDecision(ctx, p, DecisionInput{RequestKey: key, WorkRef: "work:" + key, Scope: "assignment", Instruction: "Assign", TargetRevision: "r1"})
					if err != nil {
						t.Fatal(err)
					}
					id = d.ID
					if kind == "action" {
						d, err = s.AnswerDecision(ctx, p, d.ID, AnswerInput{Revision: 1, Approve: true, Text: "Yes"})
						if err != nil {
							t.Fatal(err)
						}
						id = d.ActionID
						var action Action
						if err := st.get(ctx, "action", id, &action); err != nil || action.CreatedAt == nil || action.CreatedAt.IsZero() {
							t.Fatalf("missing action creation time: %+v %v", action, err)
						}
					}
				}
				if _, err := st.db.Exec(`UPDATE pm_records SET body=json_set(body,'$.created_at',?) WHERE kind=? AND id=?`, at.Format(time.RFC3339Nano), kind, id); err != nil {
					t.Fatal(err)
				}
				want = append(want, expected{id, at, i})
			}
			sort.Slice(want, func(i, j int) bool {
				if want[i].at.Equal(want[j].at) {
					return want[i].row > want[j].row
				}
				return want[i].at.After(want[j].at)
			})
			h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return p, nil }}
			read := func(cursor string) Page[struct {
				ID string `json:"id"`
			}] {
				t.Helper()
				w := httptest.NewRecorder()
				h.ServeHTTP(w, httptest.NewRequest("GET", "/pm/"+kind+"s?cursor="+cursor, nil))
				if w.Code != 200 {
					t.Fatalf("%d: %s", w.Code, w.Body.String())
				}
				var page Page[struct {
					ID string `json:"id"`
				}]
				if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
					t.Fatal(err)
				}
				return page
			}
			first := read("") // default page size 50, as used by the Inbox
			if len(first.Items) != 50 || !first.HasMore || first.NextCursor == "" {
				t.Fatalf("first page: %+v", first)
			}
			for i, item := range first.Items {
				if item.ID != want[i].id {
					t.Fatalf("item %d = %s, want %s", i, item.ID, want[i].id)
				}
			}
			// Delete the boundary and insert a newer record. Cursor values must still
			// find the ten older rows, without duplicates or newly inserted rows.
			if _, err := st.db.Exec(`DELETE FROM pm_records WHERE kind=? AND id=?`, kind, want[49].id); err != nil {
				t.Fatal(err)
			}
			if _, err := st.insert(ctx, kind, "newest", p.WorkspaceID, p.ActorID, "", map[string]any{"id": "newest", "created_at": base.Add(time.Hour), "work_ref": "work:newest"}); err != nil {
				t.Fatal(err)
			}
			second := read(first.NextCursor)
			if len(second.Items) != 10 || second.HasMore || second.NextCursor != "" {
				t.Fatalf("second page: %+v", second)
			}
			for i, item := range second.Items {
				if item.ID != want[i+50].id {
					t.Fatalf("older item %d = %s, want %s", i, item.ID, want[i+50].id)
				}
			}
			if fresh := read(""); fresh.Items[0].ID != "newest" {
				t.Fatalf("new record hidden: %+v", fresh)
			}
		})
	}
}

func TestPMPageLegacyActionsAndInvalidCursors(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	for i, stamp := range []string{"2026-09-13T00:00:00Z", "2026-09-12T00:00:00Z"} {
		d := Decision{ID: fmt.Sprint("d", i), CreatedAt: mustRound3Time(t, stamp)}
		if _, err := st.insert(ctx, "decision", d.ID, p.WorkspaceID, p.ActorID, "", d); err != nil {
			t.Fatal(err)
		}
		a := Action{ID: fmt.Sprint("a", i), DecisionID: d.ID}
		if _, err := st.insert(ctx, "action", a.ID, p.WorkspaceID, p.ActorID, d.ID, a); err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.ActionPage(ctx, p, 1, "")
	if err != nil || len(page.Items) != 1 || page.Items[0].ID != "a0" || !page.HasMore {
		t.Fatalf("legacy page: %+v %v", page, err)
	}
	next, err := s.ActionPage(ctx, p, 1, page.NextCursor)
	if err != nil || len(next.Items) != 1 || next.Items[0].ID != "a1" || next.HasMore {
		t.Fatalf("legacy continuation: %+v %v", next, err)
	}
	for _, cursor := range []string{"!", base64.RawURLEncoding.EncodeToString([]byte(`{}`)), base64.RawURLEncoding.EncodeToString([]byte(stableID("action", p.WorkspaceID, p.ActorID) + ":1"))} {
		if _, err := s.ActionPage(ctx, p, 1, cursor); !errors.Is(err, ErrInvalid) {
			t.Fatalf("cursor %q: %v", cursor, err)
		}
	}
}

func mustRound3Time(t *testing.T, stamp string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		t.Fatal(err)
	}
	return at
}

func TestStaleApprovalPersistsFailedAttempt(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	d, err := s.ProposeDecision(ctx, p, DecisionInput{RequestKey: "stale", WorkRef: "work:stale", Scope: "assignment", Instruction: "Assign", TargetRevision: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	d, err = s.AnswerDecision(ctx, p, d.ID, AnswerInput{Revision: 1, Approve: true, Text: "Yes"})
	if err != nil {
		t.Fatal(err)
	}
	var approved Decision
	if err := st.get(ctx, "decision", d.ID, &approved); err != nil {
		t.Fatal(err)
	}
	var pending Action
	if err := st.get(ctx, "action", d.ActionID, &pending); err != nil {
		t.Fatal(err)
	}
	s.deps.CurrentRevision = func(context.Context, Principal, string) (string, error) { return "r2", nil }
	s.deps.Execute = func(context.Context, Action) (Receipt, error) {
		t.Fatal("stale action executed")
		return Receipt{}, nil
	}
	h := Handler{Service: s, Authenticate: func(*http.Request) (Principal, error) { return p, nil }}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/pm/decisions/"+d.ID+"/dispatch", strings.NewReader(`{}`)))
	if w.Code != 409 {
		t.Fatalf("expected unchanged stale HTTP status: %d %s", w.Code, w.Body.String())
	}
	var response struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Error.Code != "source_revision_changed" {
		t.Fatalf("stale response: %s %v", w.Body.String(), err)
	}
	var failed Action
	if err := st.get(ctx, "action", d.ActionID, &failed); err != nil {
		t.Fatal(err)
	}
	detail := "Approved source revision has changed (approved at r1, source now r2). This approval will not be sent; a fresh proposal and approval are needed."
	if failed.Status != Failed || failed.Revision != pending.Revision+1 || len(failed.Attempts) != 1 || failed.Receipt.Status != Failed || failed.Receipt.Detail != detail {
		t.Fatalf("stale not recorded: %+v", failed)
	}
	attempt := failed.Attempts[0]
	if attempt.Status != Failed || attempt.StartedAt.IsZero() || attempt.FinishedAt == nil || attempt.FinishedAt.Before(attempt.StartedAt) || !reflect.DeepEqual(attempt.Receipt, failed.Receipt) {
		t.Fatalf("incomplete attempt: %+v", attempt)
	}
	var stillAnswered Decision
	if err := st.get(ctx, "decision", d.ID, &stillAnswered); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(approved, stillAnswered) || stillAnswered.Status != Answered {
		t.Fatalf("decision changed: %+v", stillAnswered)
	}
	// Restart and even restore the source revision: never revive failed approval.
	s.deps.CurrentRevision = func(context.Context, Principal, string) (string, error) { return "r1", nil }
	restarted, err := NewService(st, s.cfg, s.deps)
	if err != nil {
		t.Fatal(err)
	}
	again, err := restarted.DispatchDecision(ctx, p, d.ID)
	if err != nil || again.Status != Failed || len(again.Attempts) != 1 {
		t.Fatalf("retry revived approval: %+v %v", again, err)
	}
	var persisted Action
	if err := st.get(ctx, "action", d.ActionID, &persisted); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(failed, persisted) {
		t.Fatal("repeat dispatch changed durable failure")
	}
	page, err := restarted.ActionPage(ctx, p, 50, "")
	if err != nil || len(page.Items) != 1 || page.Items[0].Receipt.Detail != detail || page.Items[0].Status != Failed {
		t.Fatalf("failure hidden from list: %+v %v", page, err)
	}
}

func TestStaleApprovalReturnsErrStaleOnlyAfterPersistence(t *testing.T) {
	for _, writeFails := range []bool{false, true} {
		t.Run(fmt.Sprint("writeFails=", writeFails), func(t *testing.T) {
			s, st, p, _ := fixture(t)
			ctx := context.Background()
			d, err := s.ProposeDecision(ctx, p, DecisionInput{RequestKey: "stale", WorkRef: "work:1", Scope: "assignment", Instruction: "Assign", TargetRevision: "r1"})
			if err != nil {
				t.Fatal(err)
			}
			d, err = s.AnswerDecision(ctx, p, d.ID, AnswerInput{Revision: 1, Approve: true, Text: "Yes"})
			if err != nil {
				t.Fatal(err)
			}
			s.deps.CurrentRevision = func(context.Context, Principal, string) (string, error) { return "r2", nil }
			s.deps.Execute = func(context.Context, Action) (Receipt, error) {
				t.Fatal("stale action executed")
				return Receipt{}, nil
			}
			if writeFails {
				if _, err := st.db.Exec(`CREATE TRIGGER reject_stale BEFORE UPDATE ON pm_records WHEN NEW.kind='action' BEGIN SELECT RAISE(ABORT,'fixture write failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			a, err := s.DispatchDecision(ctx, p, d.ID)
			if writeFails {
				if err == nil || errors.Is(err, ErrStale) {
					t.Fatalf("write failure hidden: %v", err)
				}
				var stored Action
				if err := st.get(ctx, "action", d.ActionID, &stored); err != nil {
					t.Fatal(err)
				}
				if stored.Status != Pending || len(stored.Attempts) != 0 || stored.Revision != 1 {
					t.Fatalf("partial write: %+v", stored)
				}
			} else if !errors.Is(err, ErrStale) || a.Status != Failed {
				t.Fatalf("stale result: %+v %v", a, err)
			}
		})
	}
}
