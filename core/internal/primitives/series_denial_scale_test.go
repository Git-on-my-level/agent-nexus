package primitives_test

import (
	"context"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/testutil/perfguard"
)

// A restricted reader's series label/point reads seek each stream's identity in
// the persisted admission owners instead of materializing the workspace denial
// closure, and they keep the canonical policy's visibility outcome.
func TestSeriesReadsBoundedBehindDeniedWorkspace(t *testing.T) {
	if testing.Short() {
		t.Skip("real 10k-card denial scale fixture")
	}
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	b, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Private"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", b["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	_, err = ws.DB().ExecContext(ctx, `WITH RECURSIVE n(value) AS (SELECT 1 UNION ALL SELECT value+1 FROM n WHERE value<10000)
 INSERT INTO cards(id,board_id,title,summary,column_key,created_at,created_by,updated_at,updated_by)
 SELECT 'denied-series-'||value,?,'Private','','ready','2050-01-01T00:00:00Z','owner','2050-01-01T00:00:00Z','owner' FROM n`, b["id"])
	if err != nil {
		t.Fatal(err)
	}
	conn, err := ws.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`PRAGMA foreign_keys=OFF`,
		`INSERT INTO series_adapters(name,description,agent_id,host_id,expected_interval,created_at,last_push) VALUES('collector','','a','h',60,'2050-01-01T00:00:00Z','pushed'),('clean','','a','h',60,'2050-01-01T00:00:00Z','pushed'),('unknown','','a','h',60,'2050-01-01T00:00:00Z','pushed')`,
		`INSERT INTO series_definitions(name,adapter,unit,kind) VALUES('metric','collector','count','gauge'),('clean"metric','clean','count','gauge'),('metric2','unknown','count','gauge')`,
		`INSERT INTO series_labels(series,labels) VALUES('clean"metric','{}'),('metric2','{}')`,
		`INSERT INTO resource_access_series_unknown(series,labels) VALUES('metric2','{"gone":"1"}')`,
		// Public, prose-denied, edge-denied and unreconstructable streams.
		`INSERT INTO series_labels(series,labels) VALUES('metric','{}'),('metric','{"ref":"card:denied-series-1"}'),('metric','{"stream":"edge"}'),('metric','{"stream":"unknown"}'),('metric','{"stream":"public"}')`,
		`INSERT INTO series_points(series,labels,ts,value,received_day) SELECT series,labels,1,1,0 FROM series_labels`,
		`INSERT INTO resource_access_series_refs(series,labels,target_ref) VALUES('metric','{"stream":"edge"}','card:denied-series-2')`,
		`INSERT INTO resource_access_series_unknown(series,labels) VALUES('metric','{"stream":"unknown"}')`,
		`PRAGMA foreign_keys=ON`,
	} {
		if _, err = conn.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	conn.Close()

	db, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	q := resourceaccess.NewDB(db)
	read := func(ctx context.Context, query string) (string, perfguard.WorkStats) {
		t.Helper()
		capture.Start()
		var args []any
		if strings.Contains(query, "?") {
			args = append(args, "metric")
		}
		rows, err := q.QueryContext(ctx, query, args...)
		if err != nil {
			t.Fatal(err)
		}
		got := []string{}
		for rows.Next() {
			var labels string
			if err = rows.Scan(&labels); err != nil {
				t.Fatal(err)
			}
			got = append(got, labels)
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		capture.Stop()
		if capture.WorkError() != nil {
			t.Fatal(capture.WorkError())
		}
		return strings.Join(got, "|"), capture.Work()
	}
	for _, query := range []string{
		`SELECT labels FROM series_labels WHERE series=? ORDER BY labels LIMIT 100`,
		`SELECT DISTINCT labels FROM series_points WHERE series=? ORDER BY labels`,
		`SELECT name||'='||COALESCE(last_push,'') FROM series_adapters ORDER BY name`,
	} {
		for _, reader := range []primitives.AccessScope{{ActorID: "reader"}, {ActorID: "owner", PMActorID: "owner"}} {
			canonical, before := read(primitives.WithAccessScope(ctx, reader), query)
			candidate, after := read(primitives.WithRequestAccessScope(ctx, reader), query)
			if canonical != candidate {
				t.Fatalf("%s %q: canonical=%q candidate=%q", reader.ActorID, query, canonical, candidate)
			}
			if reader.ActorID == "reader" && candidate != `{"stream":"public"}|{}` && candidate != `clean=pushed|collector=|unknown=` {
				t.Fatalf("restricted reader streams=%q", candidate)
			}
			if after.VMSteps > 20000 {
				t.Fatalf("%s %q not bounded: VM=%d full_scans=%d (canonical VM=%d)", reader.ActorID, query, after.VMSteps, after.FullScanSteps, before.VMSteps)
			}
			t.Logf("%s %q canonical VM=%d full_scans=%d; candidate VM=%d full_scans=%d", reader.ActorID, query, before.VMSteps, before.FullScanSteps, after.VMSteps, after.FullScanSteps)
		}
	}
}
