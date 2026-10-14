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
		promises         []PromiseRecord
		n                int
		met, slip, moved *float64
	}{
		{"none", nil, 0, nil, nil, nil},
		{"met on time",
			[]PromiseRecord{{At: h(10), Met: true}},
			1, f(100), nil, f(0)},
		{"late evidence slips until it landed",
			[]PromiseRecord{{At: h(10), ResolvedAt: h(14)}},
			1, f(0), f(4), f(0)},
		{"open miss slips to now",
			[]PromiseRecord{{At: h(40)}},
			1, f(0), f(60), f(0)},
		{"evidence before the promise does not shorten the slip",
			[]PromiseRecord{{At: h(40), ResolvedAt: h(30)}},
			1, f(0), f(60), f(0)},
		{"moved twice",
			[]PromiseRecord{{At: h(10), ResolvedAt: h(25), Moves: 2}, {At: h(10), Met: true, Moves: 1}},
			2, f(50), f(15), f(50)},
		{"mixed met and misses",
			[]PromiseRecord{{At: h(10), Met: true}, {At: h(10), ResolvedAt: h(12)}, {At: h(10), ResolvedAt: h(16)}, {At: h(90)}},
			4, f(25), f(6), f(0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := ComputeMetrics(nil, c.promises, h(0), now).S20
			if s.Promises != c.n || s.Due != c.n {
				t.Fatalf("promises=%d due=%d, want %d", s.Promises, s.Due, c.n)
			}
			wantPtr(t, "met", s.MetPct, c.met)
			wantPtr(t, "slip", s.MedianSlipHours, c.slip)
			wantPtr(t, "moved", s.Moved2PlusPct, c.moved)
		})
	}
}
