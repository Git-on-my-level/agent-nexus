package curation

import (
	"math"
	"sort"
	"strings"
	"time"
)

// AskRecord is one folded ask event; the store loads a bounded slice of these
// so scorecard metrics stay pure and O(events in window).
type AskRecord struct {
	ID          string
	RequestedAt time.Time
	Proposals   []string // response_proposals; index 0 is the recommended one
	Blocks      int      // count of payload.blocks refs (0 when none)
	Proposal    bool     // a curation proposal itself; excluded from S17/S18 so the system does not grade itself
	Response    *AskResponse
	Withdrawal  *AskWithdrawal
}

// AskResponse is the human answer to an ask.
type AskResponse struct {
	At      time.Time
	Text    string
	Outcome string // e.g. answered, approved, rejected, needs_context
}

// AskWithdrawal records the ask being withdrawn by its author.
type AskWithdrawal struct {
	At     time.Time
	Reason string
}

// PromiseRecord is one promise that came due in the window, read from core's
// materialized promise fold (plans.FoldPromise), never from raw notes.
type PromiseRecord struct {
	At         time.Time // the promised time (promise_at)
	Met        bool      // the fold's evidence landed at or before At
	ResolvedAt time.Time // evidence that landed after At; zero while the miss is open
	Moves      int       // promise_moves: next.at changes since the last reset
}

// S17 is median human time-to-decision weighted by blocked refs.
type S17 struct {
	Decisions           int      `json:"decisions"`
	WeightedMedianHours *float64 `json:"weighted_median_hours"`
}

// S18 is ask quality.
type S18 struct {
	Asks                     int      `json:"asks"`
	Answered                 int      `json:"answered"`
	AnsweredRecommendedPct   *float64 `json:"answered_recommended_pct"`
	NeedsContextPct          *float64 `json:"needs_context_pct"`
	WithdrawnMootAfter24hPct *float64 `json:"withdrawn_moot_after_24h_pct"`
}

// S20 is promise calibration.
type S20 struct {
	Promises        int      `json:"promises"`
	Due             int      `json:"due"`
	MetPct          *float64 `json:"met_pct"`
	MedianSlipHours *float64 `json:"median_slip_hours"`
	Moved2PlusPct   *float64 `json:"moved_2plus_pct"`
}

// Metrics is the scorecard payload.
type Metrics struct {
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`
	S17         S17       `json:"s17"`
	S18         S18       `json:"s18"`
	S20         S20       `json:"s20"`
	Truncated   bool      `json:"truncated"` // set by the caller when the event slice hit its bound
}

// ComputeMetrics derives S17, S18 and S20 for [windowStart, now].
func ComputeMetrics(asks []AskRecord, promises []PromiseRecord, windowStart, now time.Time) Metrics {
	m := Metrics{WindowStart: windowStart, WindowEnd: now}
	inWindow := func(t time.Time) bool { return !t.Before(windowStart) && !t.After(now) }

	// S17
	var vals, weights []float64
	for _, a := range asks {
		if a.Proposal || a.Response == nil || !inWindow(a.Response.At) {
			continue
		}
		vals = append(vals, a.Response.At.Sub(a.RequestedAt).Hours())
		weights = append(weights, float64(1+max(a.Blocks, 0)))
	}
	m.S17.Decisions = len(vals)
	if v, ok := WeightedMedian(vals, weights); ok {
		m.S17.WeightedMedianHours = ptr(round(v, 2))
	}

	// S18
	var recommended, needsCtx, moot int
	for _, a := range asks {
		if a.Proposal || !inWindow(a.RequestedAt) {
			continue
		}
		m.S18.Asks++
		if a.Withdrawal != nil && IsMootReason(a.Withdrawal.Reason) && a.Withdrawal.At.Sub(a.RequestedAt) >= 24*time.Hour {
			moot++
		}
		if a.Response == nil {
			continue
		}
		m.S18.Answered++
		if a.Response.Outcome == "needs_context" {
			needsCtx++
			continue
		}
		if len(a.Proposals) > 0 && strings.EqualFold(strings.TrimSpace(a.Response.Text), strings.TrimSpace(a.Proposals[0])) {
			recommended++
		}
	}
	m.S18.AnsweredRecommendedPct = pct(recommended, m.S18.Answered)
	m.S18.NeedsContextPct = pct(needsCtx, m.S18.Answered)
	m.S18.WithdrawnMootAfter24hPct = pct(moot, m.S18.Asks)

	m.S20 = computeS20(promises, now)
	return m
}

// computeS20 scores promises that came due in the window. Every record is
// due, so Promises equals Due. Met is the fold's own verdict; a missed
// promise slips until its late evidence landed, or until now while open.
func computeS20(promises []PromiseRecord, now time.Time) S20 {
	var out S20
	var met, moved int
	var slips []float64
	for _, p := range promises {
		out.Promises++
		out.Due++
		if p.Moves >= 2 {
			moved++
		}
		if p.Met {
			met++
			continue
		}
		end := now
		if !p.ResolvedAt.IsZero() && p.ResolvedAt.After(p.At) && !p.ResolvedAt.After(now) {
			end = p.ResolvedAt
		}
		slips = append(slips, end.Sub(p.At).Hours())
	}
	out.MetPct = pct(met, out.Due)
	out.Moved2PlusPct = pct(moved, out.Promises)
	if len(slips) > 0 {
		sort.Float64s(slips)
		mid := len(slips) / 2
		v := slips[mid]
		if len(slips)%2 == 0 {
			v = (slips[mid-1] + slips[mid]) / 2
		}
		out.MedianSlipHours = ptr(round(v, 2))
	}
	return out
}

// WeightedMedian returns the smallest value whose cumulative weight reaches
// half the total, so heavier (more-blocking) items pull the median toward them.
// Non-positive weights are ignored; ok is false when nothing contributes.
func WeightedMedian(values, weights []float64) (float64, bool) {
	type vw struct{ v, w float64 }
	n := min(len(values), len(weights))
	items := make([]vw, 0, n)
	total := 0.0
	for i := 0; i < n; i++ {
		if weights[i] > 0 {
			items = append(items, vw{values[i], weights[i]})
			total += weights[i]
		}
	}
	if len(items) == 0 {
		return 0, false
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].v < items[j].v })
	cum := 0.0
	for _, it := range items {
		cum += it.w
		if cum >= total/2 {
			return it.v, true
		}
	}
	return items[len(items)-1].v, true
}

// IsMootReason reports whether a withdrawal reason says the ask was overtaken
// by events rather than withdrawn for being badly posed.
func IsMootReason(reason string) bool {
	r := strings.ToLower(reason)
	return strings.Contains(r, "moot") || strings.Contains(r, "overtaken")
}

func pct(n, d int) *float64 {
	if d == 0 {
		return nil
	}
	return ptr(round(float64(n)*100/float64(d), 1))
}

func round(v float64, places int) float64 {
	p := math.Pow10(places)
	return math.Round(v*p) / p
}

func ptr(v float64) *float64 { return &v }
