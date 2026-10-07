package server

import (
	"testing"
	"time"

	"agent-nexus-core/internal/testutil/perfguard"
)

func TestPerformanceTimingResistsSingleLoadSpike(t *testing.T) {
	// The contested-host observation from independent review is a real spike,
	// not an increase in statements, rows or work. Timing is secondary.
	values := []time.Duration{2010 * time.Millisecond, 2470 * time.Millisecond, 10040 * time.Millisecond, 2200 * time.Millisecond, 2400 * time.Millisecond}
	median, p95, maximum := performanceLatency(values)
	if median != 2400*time.Millisecond || p95 != 10040*time.Millisecond || maximum > performanceMaxSample(7200) {
		t.Fatalf("load-spike policy: median=%v p95=%v max=%v", median, p95, maximum)
	}
	if performanceCountsExceeded(routeBudget{MaxQueries: 9, MaxRows: 58, MaxVMSteps: 100}, 9, 58, 101) == false {
		t.Fatal("wall-clock policy must never relax deterministic work")
	}
	_, _, catastrophic := performanceLatency([]time.Duration{time.Second, time.Second, time.Minute})
	if catastrophic <= performanceMaxSample(7200) {
		t.Fatal("catastrophic single-sample stall must still fail")
	}
}

func TestPerformanceWorkRejectsIndexedAggregateMutation(t *testing.T) {
	db, capture, err := perfguard.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE runs(id TEXT PRIMARY KEY); WITH RECURSIVE n(x) AS (SELECT 0 UNION ALL SELECT x+1 FROM n WHERE x<4095) INSERT INTO runs SELECT printf('run-%04d',x) FROM n`); err != nil {
		t.Fatal(err)
	}
	measure := func(query string) (int, int, uint64) {
		t.Helper()
		capture.Start()
		var result int
		if err := db.QueryRow(query).Scan(&result); err != nil {
			t.Fatal(err)
		}
		_, queries, rows := capture.Stop()
		if err := capture.WorkError(); err != nil {
			t.Fatal(err)
		}
		return queries, rows, capture.Work().VMSteps
	}
	queries, rows, bounded := measure(`SELECT SUM(LENGTH(id)) FROM runs WHERE id='run-0001'`)
	budget := routeBudget{MaxQueries: queries, MaxRows: rows, MaxVMSteps: bounded + 100}
	queries, rows, aggregate := measure(`SELECT SUM(LENGTH(id)) FROM runs WHERE id >= ''`)
	if queries != 1 || rows != 1 || !performanceCountsExceeded(budget, queries, rows, aggregate) {
		t.Fatalf("aggregate escaped work gate: SQL=%d rows=%d bounded=%d aggregate=%d", queries, rows, bounded, aggregate)
	}
}
