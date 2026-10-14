package pm

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
)

func controlFixture(t *testing.T) (*Service, *Store, Principal, Decision, Action) {
	t.Helper()
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	d, err := s.ProposeDecision(ctx, p, DecisionInput{RequestKey: "control-proposal", WorkRef: "work:1", Instruction: "Assign owner", Scope: "assignment", TargetRevision: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	d, err = s.AnswerDecision(ctx, p, d.ID, AnswerInput{Revision: d.Revision, Approve: true, Text: "Approved"})
	if err != nil {
		t.Fatal(err)
	}
	var a Action
	if err = st.get(ctx, "action", d.ActionID, &a); err != nil {
		t.Fatal(err)
	}
	return s, st, p, d, a
}
func TestControlOriginalReceiptAtomicAndFrozen(t *testing.T) {
	for _, op := range []string{"dispatch", "reconcile", "acknowledge"} {
		t.Run(op, func(t *testing.T) {
			s, st, p, d, a := controlFixture(t)
			ctx := context.Background()
			var calls atomic.Int32
			s.deps.Execute = func(context.Context, Action) (Receipt, error) {
				calls.Add(1)
				return Receipt{Status: Delivered, ExternalID: "remote"}, nil
			}
			s.deps.Reconcile = func(context.Context, Action) (Receipt, error) {
				calls.Add(1)
				return Receipt{Status: Delivered, ExternalID: "remote"}, nil
			}
			if op != "dispatch" {
				a.Status = Failed
				if op == "reconcile" {
					a.Status = Delivered
					a.Attempts = []Attempt{{Status: Delivered}}
				}
				old := a.Revision
				a.Revision++
				if err := st.cas(ctx, "action", a.ID, old, a); err != nil {
					t.Fatal(err)
				}
			}
			target := a.ID
			in := ControlInput{RequestKey: "control", IfRevision: &a.Revision}
			if op == "dispatch" {
				target = d.ID
				in.IfRevision = &d.Revision
				in.IfActionRevision = &a.Revision
			}
			if _, err := st.db.Exec(`CREATE TRIGGER reject_control BEFORE INSERT ON idempotency_replays BEGIN SELECT RAISE(ABORT,'receipt failure'); END`); err != nil {
				t.Fatal(err)
			}
			if _, err := s.control(ctx, p, op, target, in); err == nil {
				t.Fatal("receipt failure accepted")
			}
			var unchanged Action
			if err := st.get(ctx, "action", a.ID, &unchanged); err != nil || !reflect.DeepEqual(a, unchanged) {
				t.Fatalf("partial mutation: %+v %v", unchanged, err)
			}
			if op == "dispatch" && calls.Load() != 0 {
				t.Fatal("executed before durable receipt")
			}
			if _, err := st.db.Exec(`DROP TRIGGER reject_control`); err != nil {
				t.Fatal(err)
			}
			first, err := s.control(ctx, p, op, target, in)
			if err != nil {
				t.Fatal(err)
			}
			if op == "dispatch" && (first.Status != Sending || first.Attempts[0].FinishedAt != nil) {
				t.Fatalf("admission changed by delivery: %+v", first)
			}
			total := calls.Load()
			again, err := s.control(ctx, p, op, target, in)
			if err != nil || !reflect.DeepEqual(first, again) || calls.Load() != total {
				t.Fatalf("replay changed or repeated source IO: %+v %v", again, err)
			}
			changed := in
			r := *in.IfRevision + 1
			changed.IfRevision = &r
			if _, err = s.control(ctx, p, op, target, changed); !errors.Is(err, ErrConflict) {
				t.Fatalf("changed key reuse: %v", err)
			}
			s.deps.Authorize = func(context.Context, Principal, string, string) error { return ErrForbidden }
			if _, err = s.control(ctx, p, op, target, in); !errors.Is(err, ErrForbidden) {
				t.Fatalf("revoked replay: %v", err)
			}
		})
	}
}
func TestControlCapturedBaseComparedAtWriter(t *testing.T) {
	for _, op := range []string{"dispatch", "reconcile", "acknowledge"} {
		t.Run(op, func(t *testing.T) {
			s, st, p, d, a := controlFixture(t)
			ctx := context.Background()
			a.Status = Unknown
			a.Attempts = []Attempt{{Status: Unknown}}
			if op == "dispatch" {
				a.Status = Pending
				a.Attempts = nil
			}
			old := a.Revision
			a.Revision++
			if err := st.cas(ctx, "action", a.ID, old, a); err != nil {
				t.Fatal(err)
			}
			concurrent := func() {
				var newer Action
				if err := st.get(ctx, "action", a.ID, &newer); err != nil {
					t.Fatal(err)
				}
				old := newer.Revision
				newer.Revision++
				newer.Receipt.Detail = "newer result"
				if err := st.cas(ctx, "action", a.ID, old, newer); err != nil {
					t.Fatal(err)
				}
			}
			s.deps.CurrentRevision = func(context.Context, Principal, string) (string, error) { concurrent(); return "r1", nil }
			s.deps.Reconcile = func(context.Context, Action) (Receipt, error) { concurrent(); return Receipt{Status: Unknown}, nil }
			var calls int
			s.deps.Execute = func(context.Context, Action) (Receipt, error) {
				calls++
				return Receipt{Status: Delivered, ExternalID: "remote"}, nil
			}
			target := a.ID
			in := ControlInput{RequestKey: "race", IfRevision: &a.Revision}
			if op == "dispatch" {
				target = d.ID
				in.IfRevision = &d.Revision
				in.IfActionRevision = &a.Revision
			}
			if _, err := s.control(ctx, p, op, target, in); !errors.Is(err, ErrConflict) {
				t.Fatalf("race: %v", err)
			}
			var current Action
			_ = st.get(ctx, "action", a.ID, &current)
			if current.Receipt.Detail != "newer result" || calls != 0 {
				t.Fatalf("overwrote concurrent action: %+v", current)
			}
			var count int
			_ = st.db.QueryRow(`SELECT count(*) FROM idempotency_replays WHERE scope LIKE 'pm.controls.%'`).Scan(&count)
			if count != 0 {
				t.Fatal("conflict left receipt")
			}
		})
	}
}
func TestConcurrentControlDispatchAdmitsOnce(t *testing.T) {
	s, _, p, d, a := controlFixture(t)
	ctx := context.Background()
	var count atomic.Int32
	s.deps.Execute = func(context.Context, Action) (Receipt, error) {
		count.Add(1)
		return Receipt{Status: Delivered, ExternalID: "remote"}, nil
	}
	in := ControlInput{RequestKey: "same", IfRevision: &d.Revision, IfActionRevision: &a.Revision}
	type result struct {
		a   Action
		err error
	}
	results := make(chan result, 8)
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() { <-start; out, err := s.control(ctx, p, "dispatch", d.ID, in); results <- result{out, err} }()
	}
	close(start)
	first := <-results
	if first.err != nil {
		t.Fatal(first.err)
	}
	for i := 1; i < 8; i++ {
		next := <-results
		if next.err != nil || !reflect.DeepEqual(first.a, next.a) {
			t.Fatalf("concurrent receipts differ: %+v", next)
		}
	}
	if count.Load() != 1 {
		t.Fatalf("source calls: %d", count.Load())
	}
}
