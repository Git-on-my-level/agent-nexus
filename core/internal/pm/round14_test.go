package pm

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestRound14PaginationAfterPermissionFilter(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	// In descending order, several hidden rows span batches before, between,
	// and after three visible rows. The callback re-reads on the single DB connection.
	for i, actor := range []string{"foreign", "foreign", "human", "foreign", "foreign", "foreign", "human", "foreign", "human", "foreign", "foreign"} {
		d := Decision{ID: fmt.Sprint(i), WorkspaceID: p.WorkspaceID, ActorID: actor, WorkRef: fmt.Sprint(i), CreatedAt: time.Date(2026, 9, 13, 0, 0, i, 0, time.UTC)}
		if _, err := st.insert(ctx, "decision", d.ID, p.WorkspaceID, actor, "", d); err != nil {
			t.Fatal(err)
		}
	}
	s.deps.Authorize = func(ctx context.Context, p Principal, permission, ref string) error {
		if ref == "" {
			return nil
		}
		var d Decision
		if err := st.get(ctx, "decision", ref, &d); err != nil {
			return err
		}
		if d.ActorID != p.ActorID {
			return ErrForbidden
		}
		return nil
	}
	for _, limit := range []int{1, 2, 3, 4} {
		cursor := ""
		var ids []string
		for pageNo := 0; pageNo < 5; pageNo++ {
			page, err := s.DecisionPage(ctx, p, limit, cursor)
			if err != nil {
				t.Fatal(err)
			}
			for _, d := range page.Items {
				ids = append(ids, d.ID)
			}
			more := len(ids) < 3
			if len(page.Items) == 0 || page.HasMore != more || (page.NextCursor != "") != more {
				t.Fatalf("limit %d: %+v", limit, page)
			}
			if !page.HasMore {
				break
			}
			cursor = page.NextCursor
		}
		if fmt.Sprint(ids) != "[8 6 2]" {
			t.Fatal(ids)
		}
	}
}

func TestRound14MissingWorkNeverCallsSource(t *testing.T) {
	for _, authorizeMissing := range []bool{false, true} {
		t.Run(fmt.Sprint(authorizeMissing), func(t *testing.T) {
			s, st, p, _ := fixture(t)
			ctx := context.Background()
			d, err := s.ProposeDecision(ctx, p, DecisionInput{RequestKey: "d", WorkRef: "work:1", Scope: "work.phase", Instruction: "Block", TargetRevision: "r1", Payload: &ActionPayload{Phase: "blocked"}})
			if err != nil {
				t.Fatal(err)
			}
			d, err = s.AnswerDecision(ctx, p, d.ID, AnswerInput{Revision: 1, Approve: true, Text: "yes"})
			if err != nil {
				t.Fatal(err)
			}
			s.deps.DecisionWork = func(context.Context, Principal, string) (DecisionWork, error) { return DecisionWork{}, ErrNotFound }
			if authorizeMissing {
				s.deps.Authorize = func(_ context.Context, _ Principal, permission, ref string) error {
					if permission == "pm.action.work.phase" && ref != "" {
						return ErrNotFound
					}
					return nil
				}
			}
			calls := 0
			s.deps.Execute = func(context.Context, Action) (Receipt, error) { calls++; return Receipt{}, nil }
			s.deps.Reconcile = func(context.Context, Action) (Receipt, error) { calls++; return Receipt{}, nil }
			assertMissing := func(err error) {
				t.Helper()
				var target *ApprovalTargetError
				if !errors.As(err, &target) || target.Reason != "work_missing" || target.ApprovedRevision != "r1" || target.CurrentRevision != nil {
					t.Fatal(err)
				}
			}
			_, err = s.DispatchDecision(ctx, p, d.ID)
			assertMissing(err)
			var a Action
			if err := st.get(ctx, "action", d.ActionID, &a); err != nil {
				t.Fatal(err)
			}
			if len(a.Attempts) != 1 || a.Status != Failed || a.Attempts[0].SentAt != nil {
				t.Fatal(a)
			}
			a.Status = Unknown
			old := a.Revision
			a.Revision++
			if err := st.cas(ctx, "action", a.ID, old, a); err != nil {
				t.Fatal(err)
			}
			before := round13Body(t, st, "action", a.ID)
			_, err = s.ReconcileAction(ctx, p, a.ID)
			assertMissing(err)
			if calls != 0 || before != round13Body(t, st, "action", a.ID) {
				t.Fatalf("calls=%d", calls)
			}
		})
	}
}

func TestRound14MissingWorkAuthorizationPrivacy(t *testing.T) {
	s, _, p, _ := fixture(t)
	s.deps.Authorize = func(_ context.Context, p Principal, permission, ref string) error {
		if p.ActorID == "other" {
			return ErrNotFound
		}
		if ref != "" {
			return ErrNotFound
		}
		return nil
	}
	for _, permission := range []string{"pm.propose", "pm.action.work.phase"} {
		if err := s.authorize(context.Background(), p, permission, "missing"); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
		other := p
		other.ActorID = "other"
		if err := s.authorize(context.Background(), other, permission, "missing"); !errors.Is(err, ErrForbidden) {
			t.Fatal(err)
		}
	}
	if err := s.authorize(context.Background(), p, "pm.read", "missing"); !errors.Is(err, ErrForbidden) {
		t.Fatal(err)
	}
}
