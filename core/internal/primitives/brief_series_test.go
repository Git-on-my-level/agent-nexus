package primitives

import (
	"database/sql"
	"testing"
	"time"

	"agent-nexus-core/internal/seriesref"
)

func TestBriefFlatThresholdAndExcursions(t *testing.T) {
	latest := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	value := 0.0
	metric := seriesref.Metric{Latest: &seriesref.Observation{TS: latest.Format(time.RFC3339Nano), Value: &value}}
	days := map[int64]briefMetricDay{}
	for i := 0; i <= 3; i++ {
		at := latest.Add(-time.Duration(i) * 24 * time.Hour)
		day := at.Truncate(24 * time.Hour).UnixNano()
		days[day] = briefMetricDay{Day: day, LastTS: at.UnixNano(), Low: sql.NullFloat64{Valid: true}, High: sql.NullFloat64{Valid: true}}
	}
	if got := computeBriefMetricFlat(metric, days); !got.Flat || got.ForSeconds != 259200 {
		t.Fatal(got)
	}
	oldest := latest.Add(-72 * time.Hour).Truncate(24 * time.Hour).UnixNano()
	d := days[oldest]
	d.LastTS++
	days[oldest] = d
	if got := computeBriefMetricFlat(metric, days); got.Flat {
		t.Fatal("rounded a short span up to three days", got)
	}
	day := latest.Add(-24 * time.Hour).Truncate(24 * time.Hour).UnixNano()
	d = days[day]
	d.High = sql.NullFloat64{Float64: 1e-8, Valid: true}
	days[day] = d
	if got := computeBriefMetricFlat(metric, days); got.Flat || got.ForSeconds != 0 {
		t.Fatal("zero baseline excursion hidden", got)
	}
}
