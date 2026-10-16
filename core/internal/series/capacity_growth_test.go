package series

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/testutil/perfguard"
)

func measuredCapacityStore(t *testing.T, s Store) (Store, *perfguard.Capture) {
	t.Helper()
	var seq int
	var name, path string
	if err := s.DB.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	db, capture, err := perfguard.Open("file:" + path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return Store{DB: db, Auth: auth.NewStore(db)}, capture
}

func checkCapacityBudget(t *testing.T, capture *perfguard.Capture, db *sql.DB, op string) {
	t.Helper()
	statements, _, _ := capture.Stop()
	var budgets map[string]uint64
	raw, err := os.ReadFile("testdata/capacity_budget.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &budgets); err != nil {
		t.Fatal(err)
	}
	work := capture.Work()
	if capture.WorkError() != nil || work.VMSteps > budgets[op] || work.FullScanSteps > budgets[op+"_full_scan_steps"] {
		t.Fatalf("%s: work=%+v error=%v", op, work, capture.WorkError())
	}
	indexSeen := false
	for _, statement := range statements {
		if !strings.Contains(statement.SQL, "SUM(definition_count)") && !strings.Contains(statement.SQL, "last_point_ts>=? LIMIT") {
			continue
		}
		plans, err := perfguard.Explain(context.Background(), db, statement)
		if err != nil {
			t.Fatal(err)
		}
		expected := "idx_series_adapters_capacity"
		if op == "push" {
			expected = "idx_series_labels_capacity"
		}
		if !strings.Contains(strings.Join(plans, "\n"), expected) {
			t.Fatalf("missing %s: %v", expected, plans)
		}
		indexSeen = true
	}
	if !indexSeen {
		t.Fatal("capacity query was not measured")
	}
	t.Logf("%s: VM steps=%d full scans=%d", op, work.VMSteps, work.FullScanSteps)
}

func TestSeriesCapacityTenThousandHistoricalStreamsBudget(t *testing.T) {
	ctx := context.Background()
	s, _, _, writer := fixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	old := now.Add(-Retention - time.Hour).UnixNano()
	if _, err := s.DB.Exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<10000)
 INSERT INTO series_labels(series,labels,last_point_ts) SELECT 'builds',json_object('slot',printf('old-%05d',x)),? FROM n;
 INSERT INTO series_daily(series,labels,day,n,total,low,high,last_ts,last_value) SELECT series,labels,0,1,1,1,1,last_point_ts,1 FROM series_labels WHERE series='builds'`, old); err != nil {
		t.Fatal(err)
	}
	measured, capture := measuredCapacityStore(t, s)
	capture.Start()
	err := measured.Push(ctx, "builds", Point{Value: number(2), Labels: map[string]string{"slot": "fresh"}}, writer, now)
	if err != nil {
		t.Fatal(err)
	}
	checkCapacityBudget(t, capture, s.DB, "push")
	var history int
	if err = s.DB.QueryRow(`SELECT COUNT(*) FROM series_labels WHERE series='builds'`).Scan(&history); err != nil || history != 10001 {
		t.Fatal(history, err)
	}
}

func TestSeriesCapacityTenThousandSerieslessAdaptersBudget(t *testing.T) {
	ctx := context.Background()
	s, human, owner, _ := fixture(t)
	if _, err := s.DB.Exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<10000)
 INSERT INTO series_adapters(name,description,agent_id,host_id,expected_interval,created_at)
 SELECT printf('empty-%05d',x),'Delivery only','owner','host',60,'2026-10-16T00:00:00Z' FROM n`); err != nil {
		t.Fatal(err)
	}
	measured, capture := measuredCapacityStore(t, s)
	capture.Start()
	_, err := measured.Declare(ctx, Declaration{Name: "populated", Description: "Populated", AgentID: owner.AgentID, ExpectedInterval: "1m", Series: []Definition{{Name: "fresh_metric", Kind: "gauge"}}}, human)
	if err != nil {
		t.Fatal(err)
	}
	checkCapacityBudget(t, capture, s.DB, "declare")
}

func TestExpiredLabelReactivationHonorsCapacity(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, _, _, writer := fixtureAt(t, root)
	now := time.Now().UTC().Truncate(time.Second)
	oldNow := now.Add(-Retention - 24*time.Hour)
	expired := Point{Value: number(1), Labels: map[string]string{"slot": "expired"}}
	if err := s.Push(ctx, "builds", expired, writer, oldNow); err != nil {
		t.Fatal(err)
	}
	if err := s.Compact(ctx, now); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < MaxLabelSets; i++ {
		if err := s.Push(ctx, "builds", Point{Value: number(2), Labels: map[string]string{"slot": fmt.Sprint(i)}}, writer, now); err != nil {
			t.Fatal(i, err)
		}
	}
	s = reopenStore(t, s, root)
	measured, capture := measuredCapacityStore(t, s)
	capture.Start()
	if err := measured.Push(ctx, "builds", expired, writer, now); !errors.Is(err, ErrCapacity) {
		t.Fatalf("reactivation at cap: %v", err)
	}
	checkCapacityBudget(t, capture, s.DB, "push")
	var count int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM series_labels WHERE series='builds' AND last_point_ts>=?`, now.Add(-Retention).UnixNano()).Scan(&count); err != nil || count != MaxLabelSets {
		t.Fatal(count, err)
	}
	// Extend this fixture credential so clock-driven capacity expiry, rather than
	// credential expiry, is the admission under test.
	if _, err := s.DB.Exec(`UPDATE auth_access_tokens SET expires_at=? WHERE id=?`, now.Add(2*Retention).Format(time.RFC3339Nano), writer.AccessTokenID); err != nil {
		t.Fatal(err)
	}
	// Once capacity expires, the retained label can become active again.
	if err := s.Push(ctx, "builds", expired, writer, now.Add(Retention+time.Hour)); err != nil {
		t.Fatalf("reactivation after expiry: %v", err)
	}
}
