package primitives

import "time"

// Count series fall back to per-day totals once their capped page fills.
// The capped page still supplies the recent buckets (and everything that
// needs rows: rankings, answer times, withdrawals); watch_trend_days fills
// only the buckets the page did not reach. It reads at most one index entry
// per day of the range for each family, independent of workspace size.

// trendDayKinds maps a count series key to its watch_trend_days kind.
var trendDayKinds = map[string]string{
	"opened":   "ask",
	"answered": "answer",
	"runs":     "run",
	"failed":   "run_failed",
	"started":  "card",
	"finished": "done_event",
}

// loadDays reads the per-bucket day counts for each series whose family was
// truncated (a non-zero horizon). Untruncated families read nothing more.
func (b *trendsBuild) loadDays(horizons map[string]time.Time) error {
	if b.days == nil {
		b.days = map[string][]float64{}
	}
	start := b.w.start().Format(time.DateOnly)
	end := b.w.end.Format(time.DateOnly)
	stepDays := int(b.w.step / (24 * time.Hour))
	for key, horizon := range horizons {
		kind, ok := trendDayKinds[key]
		if !ok || horizon.IsZero() {
			continue
		}
		rows, err := b.s.db.QueryContext(b.ctx, `SELECT CAST((julianday(day)-julianday(?))/? AS INTEGER),SUM(n)
		 FROM watch_trend_days WHERE kind=? AND day>=? AND day<? GROUP BY 1`, start, stepDays, kind, start, end)
		if err != nil {
			return err
		}
		counts := make([]float64, 2*b.w.n)
		for rows.Next() {
			var i int
			var n float64
			if err = rows.Scan(&i, &n); err != nil {
				break
			}
			if i >= 0 && i < len(counts) {
				counts[i] = n
			}
		}
		if err == nil {
			err = rows.Err()
		}
		rows.Close()
		if err != nil {
			return err
		}
		b.days[key] = counts
	}
	return nil
}

// fillDays replaces a count series' unknown buckets with their day counts.
func (b *trendsBuild) fillDays(s trendSeries) trendSeries {
	counts := b.days[s.key]
	if counts == nil {
		return s
	}
	for i, p := range s.points {
		if p == nil && counts[i] >= 0 {
			s.points[i] = num(counts[i])
		}
	}
	return s
}

// seriesUnknown reports whether any series still has a bucket nobody read.
func (b *trendsBuild) seriesUnknown(series []trendSeries) bool {
	for _, s := range series {
		for _, p := range s.points {
			if p == nil {
				return true
			}
		}
	}
	return false
}
