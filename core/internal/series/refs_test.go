package series

import (
	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/seriesref"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestParseSeriesRef(t *testing.T) {
	for _, raw := range []string{"series:builds", "series:builds?loop=cost%20loop&_range=90d&_baseline=-2", "series:health?host.name=worker%2Fone"} {
		if _, err := seriesref.ParseRef(raw); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
	}
	for _, raw := range []string{"series:", "series:Upper", "series:builds?_range=91d", "series:builds?_range=0s", "series:builds?_baseline=NaN", "series:builds?_baseline=Inf", "series:builds?a=1&a=2", "series:builds?_unknown=1", "series:builds?a=%zz"} {
		if _, err := seriesref.ParseRef(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	r, err := seriesref.ParseRef("series:builds?loop=cost%20loop&_baseline=10")
	if err != nil || r.Labels["loop"] != "cost loop" || *r.Baseline != 10 || r.Range != 30*24*time.Hour {
		t.Fatal(r, err)
	}
}
func TestSeriesRefComparisonEndpointsAndPrivacy(t *testing.T) {
	s, _, _, _ := fixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	for _, labels := range []string{`{"loop":"one","host.name":"worker"}`, `{"loop":"two"}`} {
		if _, err := s.DB.Exec(`INSERT INTO series_labels(series,labels) VALUES('builds',?)`, labels); err != nil {
			t.Fatal(err)
		}
		for i, v := range []float64{10, 100, 15} {
			if _, err := s.DB.Exec(`INSERT INTO series_points(series,labels,ts,value,received_day) VALUES('builds',?,?,?,0)`, labels, now.Add(time.Duration(i-3)*time.Hour).UnixNano(), v); err != nil {
				t.Fatal(err)
			}
		}
	}
	parse := func(raw string) seriesref.Ref {
		r, err := seriesref.ParseRef(raw)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	refs := []seriesref.Ref{parse("series:builds?loop=one"), parse("series:builds?loop=one&_baseline=0"), parse("series:builds?loop=one&_baseline=-10"), parse("series:builds?host.name=worker"), parse("series:builds"), parse("series:builds?loop=missing")}
	got, err := seriesref.CompareRefs(ctx, resourceaccess.NewDB(s.DB), refs, now)
	if err != nil {
		t.Fatal(err)
	}
	m := got[refs[0].Ref]
	if m.Latest == nil || *m.Latest.Value != 15 || *m.Baseline.Value != 10 || *m.Change != 5 || *m.ChangePercent != 50 || m.Age != 3600 || m.Status != "stale" {
		t.Fatalf("%+v", m)
	}
	if got[refs[1].Ref].ChangePercent != nil || *got[refs[1].Ref].Change != 15 {
		t.Fatal(got)
	}
	if *got[refs[2].Ref].ChangePercent != 250 {
		t.Fatal(got)
	}
	if got[refs[3].Ref].Latest == nil {
		t.Fatal("dotted selector failed")
	}
	if got[refs[4].Ref].Status != "ambiguous" || got[refs[4].Ref].Latest != nil || got[refs[4].Ref].ObservedAt != "" {
		t.Fatal("ambiguous leaked", got)
	}
	if got[refs[5].Ref].Latest != nil {
		t.Fatal("missing stream has value")
	}
	policy := resourceaccess.Policy{Read: func(q string) string {
		prefix := `WITH series_labels AS (SELECT * FROM main.series_labels WHERE labels NOT LIKE '%one%'),series_points AS (SELECT * FROM main.series_points WHERE labels NOT LIKE '%one%') `
		if strings.HasPrefix(q, "WITH") {
			return prefix + "," + q[4:]
		}
		return prefix + q
	}}
	hidden, err := seriesref.CompareRefs(resourceaccess.WithPolicy(ctx, policy), resourceaccess.NewDB(s.DB), refs[:1], now)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(hidden)
	if strings.Contains(string(raw), `"value"`) || strings.Contains(string(raw), `"observed_at"`) {
		t.Fatalf("private stream leaked: %s", raw)
	}
}

func TestSeriesRefStateBaselineDoesNotPoisonBatch(t *testing.T) {
	s, _, _, _ := fixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for _, name := range []string{"builds", "health"} {
		if _, e := s.DB.Exec(`INSERT INTO series_labels(series,labels) VALUES(?,'{}')`, name); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := s.DB.Exec(`INSERT INTO series_points(series,labels,ts,value,received_day) VALUES('builds','{}',?,4,0)`, now.Add(-time.Minute).UnixNano()); e != nil {
		t.Fatal(e)
	}
	if _, e := s.DB.Exec(`INSERT INTO series_points(series,labels,ts,state,received_day) VALUES('health','{}',?,'green',0)`, now.Add(-time.Minute).UnixNano()); e != nil {
		t.Fatal(e)
	}
	refs := []seriesref.Ref{}
	for _, ref := range []string{"series:builds", "series:health", "series:health?_baseline=2"} {
		r, _ := seriesref.ParseRef(ref)
		refs = append(refs, r)
	}
	m, e := seriesref.CompareRefs(ctx, resourceaccess.NewDB(s.DB), refs, now)
	if e != nil {
		t.Fatal(e)
	}
	if m[refs[0].Ref].Latest == nil || m[refs[1].Ref].Changed == nil || *m[refs[1].Ref].Changed || m[refs[2].Ref].Latest != nil {
		t.Fatal(m)
	}
}
func TestCardSeriesRefsPlanAndAdmission(t *testing.T) {
	s, _, _, _ := fixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	store := primitives.NewTestStore(s.DB, t.TempDir())
	var adapter string
	if e := s.DB.QueryRow(`SELECT adapter FROM series_definitions WHERE name='builds'`).Scan(&adapter); e != nil {
		t.Fatal(e)
	}
	refs := []string{}
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("metric%d", i)
		refs = append(refs, "series:"+name)
		if _, e := s.DB.Exec(`INSERT INTO series_definitions(name,adapter,unit,kind) VALUES(?,?,'units','gauge')`, name, adapter); e != nil {
			t.Fatal(e)
		}
		if _, e := s.DB.Exec(`INSERT INTO series_labels(series,labels) VALUES(?,'{}')`, name); e != nil {
			t.Fatal(e)
		}
		if _, e := s.DB.Exec(`INSERT INTO series_points(series,labels,ts,value,received_day) VALUES(?,'{}',?,7,0)`, name, now.Add(-time.Minute).UnixNano()); e != nil {
			t.Fatal(e)
		}
	}
	card, e := store.CreateWork(ctx, "owner", "", map[string]any{"title": "Metrics", "related_refs": append(refs, "series:broken?_range=91d")})
	if e != nil {
		t.Fatal(e)
	}
	ref := fmt.Sprint(card["ref"])
	plan := plans.Plan{Steps: []plans.Step{{ID: "measure", Title: "Measure", Ref: refs[0], After: []string{}, Status: "active"}}}
	if e = store.SetCardPlan(ctx, "owner", fmt.Sprint(card["id"]), fmt.Sprint(card["updated_at"]), plan); e != nil {
		t.Fatal(e)
	}
	out, e := store.ResolveRefs(ctx, []string{ref, refs[0], "series:broken?_range=91d"}, nil, now)
	if e != nil {
		t.Fatal(e)
	}
	if len(out[0].Summary.Metrics) != 8 || !out[0].Summary.MetricsTruncated || out[1].Metric == nil || out[2].Resolvable {
		t.Fatalf("%+v", out)
	}
	if out[0].NextStep == nil || out[0].NextStep.Ref != refs[0] {
		t.Fatal("series plan step hidden", out[0])
	}
}

func TestCardCanonicalSeriesRefs(t *testing.T) {
	s, _, _, _ := fixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	store := primitives.NewTestStore(s.DB, t.TempDir())
	card, e := store.CreateWork(ctx, "owner", "", map[string]any{"title": "Canonical refs"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec(`UPDATE cards SET refs_json='["series:builds"]' WHERE id=?`, card["id"]); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec(`INSERT INTO series_labels(series,labels) VALUES('builds','{}')`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec(`INSERT INTO series_points(series,labels,ts,value,received_day) VALUES('builds','{}',?,8,0)`, now.Add(-time.Minute).UnixNano()); e != nil {
		t.Fatal(e)
	}
	out, e := store.ResolveRefs(ctx, []string{fmt.Sprint(card["ref"])}, nil, now)
	if e != nil {
		t.Fatal(e)
	}
	if len(out[0].Summary.Metrics) != 1 || *out[0].Summary.Metrics[0].Latest.Value != 8 {
		t.Fatal(out)
	}
}
