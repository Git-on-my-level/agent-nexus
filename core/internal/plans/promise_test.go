package plans

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)

func day(n int) time.Time { return t0.Add(time.Duration(n) * 24 * time.Hour) }

func TestFoldPromiseNotePromiseCountsUntilReset(t *testing.T) {
	in := PromiseInput{Ref: "card:a", Notes: []PromiseNote{{EventID: "e1", At: day(0), Author: "agent:x", NextAt: day(2), NextWhat: "ship"}}}
	f := FoldPromise(in)
	if f.Promise == nil || f.Promise.Source != "note" || !f.Promise.At.Equal(day(2)) || f.Promise.SourceRef != "event:e1" {
		t.Fatalf("promise = %+v", f.Promise)
	}
	if s := f.At(day(1)); s.State != PromiseMoving {
		t.Fatalf("state = %s", s.State)
	}
	s := f.At(day(5))
	if s.State != PromisePast || *s.PastPromiseFor != 3*24*time.Hour {
		t.Fatalf("past = %+v", s)
	}
	// Evidence before at meets the promise.
	in.Notes = append(in.Notes, PromiseNote{At: day(1), Author: "agent:x", Evidence: true})
	f = FoldPromise(in)
	if f.Promise != nil || !f.LastEvidenceAt.Equal(day(1)) {
		t.Fatalf("met promise still counts: %+v", f)
	}
	if f.At(day(5)).State != PromiseNone {
		t.Fatal("expected no_promise")
	}
}

func TestFoldPromiseMovesWithoutEvidence(t *testing.T) {
	in := PromiseInput{Notes: []PromiseNote{
		{At: day(0), Author: "a", NextAt: day(2)},
		{At: day(2), Author: "a", NextAt: day(4)},
		{At: day(4), Author: "a", NextAt: day(6)},
	}}
	f := FoldPromise(in)
	if f.Moves != 2 || !f.Promise.At.Equal(day(6)) {
		t.Fatalf("fold = %+v", f)
	}
	// A note with evidence and a new next resets the streak.
	in.Notes = append(in.Notes, PromiseNote{At: day(5), Author: "a", NextAt: day(7), Evidence: true})
	f = FoldPromise(in)
	if f.Moves != 0 || !f.Promise.At.Equal(day(7)) {
		t.Fatalf("after evidence = %+v", f)
	}
}

func TestFoldPromiseEarliestAcrossSources(t *testing.T) {
	in := PromiseInput{Ref: "card:a",
		Notes:   []PromiseNote{{At: day(0), Author: "a", NextAt: day(5)}},
		Steps:   []PromiseStep{{ID: "s1", Title: "draft", Due: day(3)}},
		CardDue: day(9), Wake: day(4)}
	f := FoldPromise(in)
	if f.Promise.Source != "note" || !f.Promise.At.Equal(day(5)) {
		t.Fatalf("note promise outranks the earlier step: %+v", f.Promise)
	}
	// A step change inside the horizon before the step due meets the step,
	// and a status change after the note meets the note. The wake remains.
	in.StepChanges = []time.Time{day(2)}
	f = FoldPromise(in)
	if f.Promise.Source != "wake_condition" {
		t.Fatalf("expected wake after met step, got %+v", f.Promise)
	}
}

func TestFoldPromisePlanEditLeavesTheNote(t *testing.T) {
	in := PromiseInput{Ref: "card:a",
		Notes:     []PromiseNote{{EventID: "e1", At: day(1), Author: "a", NextAt: day(8), NextWhat: "ledger"}},
		Steps:     []PromiseStep{{ID: "s1", Title: "retire", Due: day(3)}},
		PlanEdits: []time.Time{day(2)}}
	f := FoldPromise(in)
	if f.Promise == nil || f.Promise.Source != "note" || f.Promise.What != "ledger" || !f.Promise.At.Equal(day(8)) {
		t.Fatalf("plan edit must not replace the note: %+v", f.Promise)
	}
	if !f.LastEvidenceAt.Equal(day(2)) {
		t.Fatalf("plan edit is still last evidence: %s", f.LastEvidenceAt)
	}
	in.PlanEdits = nil
	in.Notes = nil
	f = FoldPromise(in)
	if f.Promise == nil || f.Promise.Source != "plan_step" || f.Promise.What != "retire" {
		t.Fatalf("no note falls back to the step: %+v", f.Promise)
	}
}

func TestFoldPromiseDone(t *testing.T) {
	f := FoldPromise(PromiseInput{Done: true, Steps: []PromiseStep{{Due: day(1)}}})
	if s := f.At(day(9)); s.State != PromiseDone || s.Promise != nil {
		t.Fatalf("done = %+v", s)
	}
}

func TestParseWhen(t *testing.T) {
	for raw, want := range map[string]time.Time{"2d": day(2), "36h": t0.Add(36 * time.Hour), "2026-10-12": day(2).Add(24*time.Hour - time.Second), "2026-10-12T00:00:00+00:00": day(2)} {
		got, ok := ParseWhen(raw, t0)
		if !ok || !got.Equal(want) {
			t.Fatalf("%s = %v %v", raw, got, ok)
		}
	}
	for _, raw := range []string{"", "0d", "-1d", "soon", "3651d"} {
		if _, ok := ParseWhen(raw, t0); ok {
			t.Fatalf("%q parsed", raw)
		}
	}
}
