package primitives

import (
	"testing"
	"time"
)

// The day counts are derived from positions alone: after inserts, re-timed
// events, trashing, restoring and deletes they must equal a fresh recount.
func TestWatchTrendDaysMatchPositions(t *testing.T) {
	t.Parallel()
	f := newTrendsFixture(t)
	day := 24 * time.Hour
	work := f.card("Counted", "", nil)
	other := f.card("Removed", "", nil)
	f.ask(work, "agent-1", 2*day, time.Hour)
	f.ask(work, "agent-1", 9*day, 0)
	f.ask(other, "agent-2", 3*day, 0)
	f.run(work, day, "acted")
	f.run(work, 4*day, "infra_error")
	f.run(work, 5*day, "measured")
	f.exec(`UPDATE events SET ts=? WHERE type='run_recorded' AND ts=(SELECT MIN(ts) FROM events WHERE type='run_recorded')`, loopTS(f.now.Add(-12*day)))
	f.exec(`UPDATE events SET trashed_at=? WHERE id=(SELECT MAX(id) FROM events WHERE type='run_recorded')`, loopTS(f.now))
	f.exec(`UPDATE events SET trashed_at=NULL WHERE id=(SELECT MAX(id) FROM events WHERE type='run_recorded')`)
	f.exec(`UPDATE cards SET archived_at=? WHERE id=?`, loopTS(f.now), other["id"])
	f.exec(`DELETE FROM threads WHERE id=?`, other["thread_id"])

	type row struct {
		kind, day string
		n         int
	}
	read := func(q string) map[row]bool {
		t.Helper()
		rows, err := f.store.db.QueryContext(f.ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := map[row]bool{}
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.kind, &r.day, &r.n); err != nil {
				t.Fatal(err)
			}
			out[r] = true
		}
		return out
	}
	got := read(`SELECT kind,day,n FROM watch_trend_days WHERE n<>0`)
	want := read(`SELECT kind,substr(at_key,1,10),COUNT(*) FROM watch_trend_positions WHERE kind IN ('ask','answer','run','run_failed','card','done_event') AND at_key<>'' GROUP BY 1,2`)
	if len(got) != len(want) {
		t.Fatalf("day counts = %v, recount = %v", got, want)
	}
	for r := range want {
		if !got[r] {
			t.Fatalf("day counts = %v, recount = %v (missing %v)", got, want, r)
		}
	}
	asks := 0
	for r := range got {
		if r.kind == "ask" {
			asks += r.n
		}
	}
	if asks != 2 {
		t.Fatalf("asks counted = %d, want 2 (the purged thread's ask is dropped)", asks)
	}
}
