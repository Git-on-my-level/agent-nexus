package curation

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func h(n float64) time.Time { return t0.Add(time.Duration(n * float64(time.Hour))) }

func wantPtr(t *testing.T, name string, got *float64, want *float64) {
	t.Helper()
	if (got == nil) != (want == nil) || (got != nil && *got != *want) {
		g, w := "nil", "nil"
		if got != nil {
			g = ftoa(*got)
		}
		if want != nil {
			w = ftoa(*want)
		}
		t.Errorf("%s = %s, want %s", name, g, w)
	}
}

func ftoa(f float64) string { return time.Duration(f * float64(time.Millisecond)).String() }

func f(v float64) *float64 { return &v }

func TestWeightedMedian(t *testing.T) {
	cases := []struct {
		name string
		v, w []float64
		want float64
		ok   bool
	}{
		{"empty", nil, nil, 0, false},
		{"single", []float64{5}, []float64{1}, 5, true},
		{"odd equal weights", []float64{3, 1, 2}, []float64{1, 1, 1}, 2, true},
		{"even equal weights takes lower", []float64{1, 2, 3, 4}, []float64{1, 1, 1, 1}, 2, true},
		{"weights shift median", []float64{1, 2, 10}, []float64{1, 1, 5}, 10, true},
		{"light tail", []float64{1, 2, 10}, []float64{5, 1, 1}, 1, true},
		{"non-positive weights ignored", []float64{1, 100}, []float64{1, 0}, 1, true},
		{"all zero weights", []float64{1}, []float64{0}, 0, false},
	}
	for _, c := range cases {
		got, ok := WeightedMedian(c.v, c.w)
		if ok != c.ok || got != c.want {
			t.Errorf("%s: got (%v,%v) want (%v,%v)", c.name, got, ok, c.want, c.ok)
		}
	}
}

func TestIsMootReason(t *testing.T) {
	for r, want := range map[string]bool{"Moot now": true, "OVERTAKEN by events": true, "no longer needed": false, "": false} {
		if IsMootReason(r) != want {
			t.Errorf("IsMootReason(%q) != %v", r, want)
		}
	}
}

func TestMetricS17(t *testing.T) {
	now := h(100)
	asks := []AskRecord{
		{ID: "a", RequestedAt: h(0), Response: &AskResponse{At: h(1)}},                  // 1h w1
		{ID: "b", RequestedAt: h(0), Response: &AskResponse{At: h(10)}, Blocks: 4},      // 10h w5
		{ID: "c", RequestedAt: h(0), Response: &AskResponse{At: h(2)}},                  // 2h w1
		{ID: "p", RequestedAt: h(0), Response: &AskResponse{At: h(50)}, Proposal: true}, // excluded
		{ID: "u", RequestedAt: h(0)},                                                    // unanswered
		{ID: "o", RequestedAt: h(0), Response: &AskResponse{At: h(200)}},                // after now
	}
	m := ComputeMetrics(asks, nil, h(0), now)
	if m.S17.Decisions != 3 {
		t.Fatalf("decisions = %d", m.S17.Decisions)
	}
	wantPtr(t, "median", m.S17.WeightedMedianHours, f(10))

	m = ComputeMetrics(asks[:1], nil, h(0), now)
	wantPtr(t, "median single", m.S17.WeightedMedianHours, f(1))

	m = ComputeMetrics(nil, nil, h(0), now)
	wantPtr(t, "median empty", m.S17.WeightedMedianHours, nil)
	if m.Truncated {
		t.Error("truncated must be false")
	}
}

func TestMetricS18(t *testing.T) {
	now := h(200)
	props := []string{"Ship it", "Hold"}
	asks := []AskRecord{
		{ID: "1", RequestedAt: h(0), Proposals: props, Response: &AskResponse{At: h(1), Text: "  ship IT ", Outcome: "answered"}},
		{ID: "2", RequestedAt: h(0), Proposals: props, Response: &AskResponse{At: h(1), Text: "Hold", Outcome: "answered"}},
		{ID: "3", RequestedAt: h(0), Proposals: props, Response: &AskResponse{At: h(1), Text: "Ship it", Outcome: "needs_context"}},
		{ID: "4", RequestedAt: h(0), Withdrawal: &AskWithdrawal{At: h(23), Reason: "moot"}},
		{ID: "5", RequestedAt: h(0), Withdrawal: &AskWithdrawal{At: h(24), Reason: "Overtaken by #12"}},
		{ID: "6", RequestedAt: h(0), Withdrawal: &AskWithdrawal{At: h(48), Reason: "mistake"}},
		{ID: "7", RequestedAt: h(0), Proposal: true, Response: &AskResponse{At: h(1), Text: "x"}},
		{ID: "8", RequestedAt: h(-500), Response: &AskResponse{At: h(1), Text: "x"}}, // outside window
	}
	m := ComputeMetrics(asks, nil, h(0), now).S18
	if m.Asks != 6 || m.Answered != 3 {
		t.Fatalf("asks=%d answered=%d", m.Asks, m.Answered)
	}
	wantPtr(t, "recommended", m.AnsweredRecommendedPct, f(33.3))
	wantPtr(t, "needs_context", m.NeedsContextPct, f(33.3))
	wantPtr(t, "moot", m.WithdrawnMootAfter24hPct, f(16.7))

	z := ComputeMetrics(nil, nil, h(0), now).S18
	wantPtr(t, "z rec", z.AnsweredRecommendedPct, nil)
	wantPtr(t, "z nc", z.NeedsContextPct, nil)
	wantPtr(t, "z moot", z.WithdrawnMootAfter24hPct, nil)
}

func TestMetricS18NoAnswersStillMoot(t *testing.T) {
	asks := []AskRecord{{ID: "1", RequestedAt: h(0), Withdrawal: &AskWithdrawal{At: h(30), Reason: "moot"}}}
	m := ComputeMetrics(asks, nil, h(0), h(100)).S18
	wantPtr(t, "rec", m.AnsweredRecommendedPct, nil)
	wantPtr(t, "moot", m.WithdrawnMootAfter24hPct, f(100))
}

func TestMetricS20(t *testing.T) {
	now := h(100)
	cases := []struct {
		name             string
		notes            []NoteRecord
		promises, due    int
		met, slip, moved *float64
	}{
		{"none", nil, 0, 0, nil, nil, nil},
		{"met before at",
			[]NoteRecord{{ThreadID: "a", At: h(0), NextAt: h(10)}, {ThreadID: "a", At: h(5), Reset: true}},
			1, 1, f(100), nil, f(0)},
		{"slip after at",
			[]NoteRecord{{ThreadID: "a", At: h(0), NextAt: h(10)}, {ThreadID: "a", At: h(14), Reset: true}},
			1, 1, f(0), f(4), f(0)},
		{"moved twice measured from original",
			[]NoteRecord{
				{ThreadID: "a", At: h(0), NextAt: h(10)},
				{ThreadID: "a", At: h(8), NextAt: h(20)},
				{ThreadID: "a", At: h(18), NextAt: h(30)},
				{ThreadID: "a", At: h(25), Reset: true},
			},
			1, 1, f(0), f(15), f(100)},
		{"same NextAt is not a move",
			[]NoteRecord{
				{ThreadID: "a", At: h(0), NextAt: h(10)},
				{ThreadID: "a", At: h(1), NextAt: h(10)},
				{ThreadID: "a", At: h(2), Reset: true},
			},
			1, 1, f(100), nil, f(0)},
		{"open past due counts to now",
			[]NoteRecord{{ThreadID: "a", At: h(0), NextAt: h(40)}},
			1, 1, f(0), f(60), f(0)},
		{"open not yet due",
			[]NoteRecord{{ThreadID: "a", At: h(0), NextAt: h(500)}},
			1, 0, nil, nil, f(0)},
		{"reset with new NextAt opens fresh promise",
			[]NoteRecord{
				{ThreadID: "a", At: h(0), NextAt: h(10)},
				{ThreadID: "a", At: h(12), NextAt: h(20), Reset: true}, // slip 2, new promise
				{ThreadID: "a", At: h(15), Reset: true},                // met
			},
			2, 2, f(50), f(2), f(0)},
		{"threads independent, unsorted input",
			[]NoteRecord{
				{ThreadID: "b", At: h(5), Reset: true},
				{ThreadID: "a", At: h(1), NextAt: h(10)},
				{ThreadID: "b", At: h(0), NextAt: h(10)},
				{ThreadID: "a", At: h(3), NextAt: h(20)},
				{ThreadID: "a", At: h(4), NextAt: h(30)},
			},
			2, 2, f(50), f(90), f(50)},
		{"reset without promise ignored",
			[]NoteRecord{{ThreadID: "a", At: h(1), Reset: true}},
			0, 0, nil, nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := ComputeMetrics(nil, c.notes, h(0), now).S20
			if s.Promises != c.promises || s.Due != c.due {
				t.Fatalf("promises=%d due=%d, want %d/%d", s.Promises, s.Due, c.promises, c.due)
			}
			wantPtr(t, "met", s.MetPct, c.met)
			wantPtr(t, "slip", s.MedianSlipHours, c.slip)
			wantPtr(t, "moved", s.Moved2PlusPct, c.moved)
		})
	}
}

func TestMetricS20MedianSlipEven(t *testing.T) {
	notes := []NoteRecord{
		{ThreadID: "a", At: h(0), NextAt: h(10)}, {ThreadID: "a", At: h(12), Reset: true}, // 2
		{ThreadID: "b", At: h(0), NextAt: h(10)}, {ThreadID: "b", At: h(16), Reset: true}, // 6
	}
	s := ComputeMetrics(nil, notes, h(0), h(100)).S20
	wantPtr(t, "slip", s.MedianSlipHours, f(4))
}
