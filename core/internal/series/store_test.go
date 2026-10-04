package series

import (
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/storage"
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func fixture(t *testing.T) (Store, auth.Principal, auth.Principal, auth.Principal) {
	return fixtureAt(t, t.TempDir())
}

func fixtureAt(t *testing.T, root string) (Store, auth.Principal, auth.Principal, auth.Principal) {
	t.Helper()
	ctx := context.Background()
	w, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	db := w.DB()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, item := range []struct{ id, kind string }{{"human", "human"}, {"owner", "agent"}, {"other", "agent"}} {
		if _, err = db.Exec(`INSERT INTO actors(id,display_name,created_at) VALUES(?,?,?)`, item.id, item.id, now); err != nil {
			t.Fatal(err)
		}
		if _, err = db.Exec(`INSERT INTO agents(id,actor_id,username,created_at,updated_at,metadata_json) VALUES(?,?,?,?,?,json_object('principal_kind',?))`, item.id, item.id, item.id, now, now, item.kind); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) VALUES('host','fleet','Fleet','test','test','[]',?)`, now); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO host_agents(host_id,name,agent_id,identity_kind) VALUES('host','owner','owner','derived')`); err != nil {
		t.Fatal(err)
	}
	s := Store{DB: db, Auth: auth.NewStore(db)}
	human := auth.Principal{AgentID: "human", ActorID: "human", PrincipalKind: "human"}
	owner := auth.Principal{AgentID: "owner", ActorID: "owner", PrincipalKind: "agent"}
	_, err = s.Declare(ctx, Declaration{Name: "collector", Description: "Test collector", AgentID: "owner", ExpectedInterval: "1m", Series: []Definition{{Name: "builds", Kind: "gauge", Unit: "builds"}, {Name: "health", Kind: "state", Unit: "state"}}}, human)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := s.Auth.IssueSeriesToken(ctx, "collector", owner)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := s.Auth.AuthenticateAccessToken(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	return s, human, owner, writer
}

func reopenStore(t *testing.T, s Store, root string) Store {
	t.Helper()
	if err := s.DB.Close(); err != nil {
		t.Fatal(err)
	}
	w, err := storage.InitializeWorkspace(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	return Store{DB: w.DB(), Auth: auth.NewStore(w.DB())}
}

func TestRestartPreservesDedupeBudgetsAndLabelCardinality(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, _, _, writer := fixtureAt(t, root)
	now := time.Now().UTC().Truncate(time.Second)
	p := Point{Value: number(7), TS: now.Format(time.RFC3339Nano), Labels: map[string]string{"initiative": "launch", "status": "open"}}
	if err := s.Push(ctx, "builds", p, writer, now); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < MaxLabelSets; i++ {
		if _, err := s.DB.Exec(`INSERT INTO series_labels(series,labels) VALUES('builds',json_object('slot',CAST(? AS TEXT)))`, i); err != nil {
			t.Fatal(err)
		}
	}
	s = reopenStore(t, s, root)
	// Reversed map insertion order and an equivalent timezone dedupe identically.
	p.Labels = map[string]string{"status": "open", "initiative": "launch"}
	p.TS = now.In(time.FixedZone("offset", 3600)).Format(time.RFC3339Nano)
	if err := s.Push(ctx, "builds", p, writer, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Push(ctx, "builds", Point{Value: number(1), Labels: map[string]string{"slot": "new"}}, writer, now); !errors.Is(err, ErrCapacity) {
		t.Fatalf("restart reset label cardinality: %v", err)
	}
	for table, want := range map[string]int{"series_points": 1, "series_labels": MaxLabelSets} {
		var n int
		if err := s.DB.QueryRow(`SELECT COUNT(*) FROM ` + table + ` WHERE series='builds'`).Scan(&n); err != nil || n != want {
			t.Fatalf("%s count=%d want=%d error=%v", table, n, want, err)
		}
	}
	var ingested int
	if err := s.DB.QueryRow(`SELECT n FROM series_ingestion_days WHERE day=?`, now.UnixNano()/Day).Scan(&ingested); err != nil || ingested != 1 {
		t.Fatalf("restart lost dedupe: %d %v", ingested, err)
	}
	if _, err := s.DB.Exec(`UPDATE series_request_budgets SET n=? WHERE scope='adapter:collector'`, MaxAdapterRequestsPerMinute); err != nil {
		t.Fatal(err)
	}
	s = reopenStore(t, s, root)
	if err := s.Push(ctx, "builds", p, writer, now); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("restart or exact retry bypassed adapter rate budget: %v", err)
	}
	var workspaceRequests int
	if err := s.DB.QueryRow(`SELECT n FROM series_request_budgets WHERE scope='workspace' AND minute=?`, now.Unix()/60).Scan(&workspaceRequests); err != nil || workspaceRequests != 2 {
		t.Fatalf("denied attempt partially changed budget: %d %v", workspaceRequests, err)
	}
	if err := s.Push(ctx, "builds", p, writer, now.Add(time.Minute)); err != nil {
		t.Fatalf("budget did not reopen at next minute: %v", err)
	}
	if _, err := s.DB.Exec(`UPDATE series_request_budgets SET n=? WHERE scope='workspace'`, MaxRequestsPerMinute); err != nil {
		t.Fatal(err)
	}
	if err := s.Push(ctx, "builds", p, writer, now.Add(time.Minute)); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("workspace rate budget: %v", err)
	}
}

func TestRestartRollupCheckpointIsAtomicIdempotentAndHasNoExpiry(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	s, _, _, writer := fixtureAt(t, root)
	now := time.Now().UTC()
	p := Point{Value: number(9), TS: now.Add(-89 * 24 * time.Hour).Format(time.RFC3339Nano)}
	if err := s.Push(ctx, "builds", p, writer, now); err != nil {
		t.Fatal(err)
	}
	future := now.Add(3 * 24 * time.Hour)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := compact(ctx, tx, future); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	s = reopenStore(t, s, root)
	var raw, daily int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM series_points`).Scan(&raw); err != nil || raw != 1 {
		t.Fatalf("interrupted checkpoint lost raw data: %d %v", raw, err)
	}
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM series_daily`).Scan(&daily); err != nil || daily != 0 {
		t.Fatalf("interrupted checkpoint left partial rollup: %d %v", daily, err)
	}
	if err := s.Compact(ctx, future); err != nil {
		t.Fatal(err)
	}
	s = reopenStore(t, s, root)
	for _, at := range []time.Time{future, future.Add(20 * 365 * 24 * time.Hour)} {
		if err := s.Compact(ctx, at); err != nil {
			t.Fatal(err)
		}
		var n int
		var total float64
		if err := s.DB.QueryRow(`SELECT n,total FROM series_daily WHERE series='builds'`).Scan(&n, &total); err != nil || n != 1 || total != 9 {
			t.Fatalf("replayed/expired daily checkpoint: n=%d total=%v err=%v", n, total, err)
		}
	}
}

func TestConcurrentPushesDedupeAndEnforceDailyBudgetAtomically(t *testing.T) {
	ctx := context.Background()
	s, _, _, writer := fixture(t)
	now := time.Now().UTC()
	p := Point{Value: number(3), TS: now.Format(time.RFC3339Nano)}
	push := func(same bool) map[string]int {
		var wg sync.WaitGroup
		results := make(chan error, 12)
		for i := 0; i < cap(results); i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				point := p
				if !same {
					point.TS = now.Add(time.Duration(i+1) * time.Millisecond).Format(time.RFC3339Nano)
				}
				results <- s.Push(ctx, "builds", point, writer, now)
			}(i)
		}
		wg.Wait()
		close(results)
		counts := map[string]int{}
		for err := range results {
			switch {
			case err == nil:
				counts["accepted"]++
			case errors.Is(err, ErrCapacity):
				counts["capacity"]++
			default:
				t.Fatal(err)
			}
		}
		return counts
	}
	if got := push(true); got["accepted"] != 12 {
		t.Fatal(got)
	}
	var raw int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM series_points`).Scan(&raw); err != nil || raw != 1 {
		t.Fatalf("concurrent dedupe: %d %v", raw, err)
	}
	if _, err := s.DB.Exec(`UPDATE series_ingestion_days SET n=?`, MaxPointsPerDay-1); err != nil {
		t.Fatal(err)
	}
	if got := push(false); got["accepted"] != 1 || got["capacity"] != 11 {
		t.Fatal(got)
	}
	var n int
	if err := s.DB.QueryRow(`SELECT n FROM series_ingestion_days`).Scan(&n); err != nil || n != MaxPointsPerDay {
		t.Fatalf("concurrent daily budget: %d %v", n, err)
	}
}

func TestTimestampAndLabelSafetyBounds(t *testing.T) {
	s, _, _, writer := fixture(t)
	now := time.Now().UTC()
	for _, ts := range []time.Time{now.Add(-Retention - time.Nanosecond), now.Add(5*time.Minute + time.Nanosecond)} {
		if err := s.Push(context.Background(), "builds", Point{Value: number(1), TS: ts.Format(time.RFC3339Nano)}, writer, now); !errors.Is(err, ErrInvalid) {
			t.Fatalf("timestamp outside bound: %v", err)
		}
	}
	labels := map[string]string{}
	for i := 0; i < 9; i++ {
		labels[fmt.Sprintf("k%d", i)] = "v"
	}
	if err := s.Push(context.Background(), "builds", Point{Value: number(1), Labels: labels}, writer, now); !errors.Is(err, ErrCapacity) {
		t.Fatalf("label key cap: %v", err)
	}
}
func number(v float64) *float64 { return &v }
func TestGrantScopeRevocationAndAudit(t *testing.T) {
	ctx := context.Background()
	s, human, owner, writer := fixture(t)
	now := time.Now().UTC()
	for _, capability := range []auth.SeriesCapability{{Operation: "cards.create", Resource: "builds"}, {Operation: auth.SeriesPointsPushOperation, Resource: "undeclared"}} {
		if err := s.Auth.RequireSeriesCapability(ctx, writer, capability); !errors.Is(err, auth.ErrSeriesForbidden) {
			t.Fatalf("unexpected operation/resource capability: %#v %v", capability, err)
		}
	}
	if err := s.Push(ctx, "builds", Point{Value: number(2)}, owner, now); !errors.Is(err, auth.ErrSeriesForbidden) {
		t.Fatalf("ordinary identity can push: %v", err)
	}
	if err := s.Push(ctx, "undeclared", Point{Value: number(2)}, writer, now); !errors.Is(err, auth.ErrSeriesForbidden) {
		t.Fatalf("undeclared series: %v", err)
	}
	if err := s.Push(ctx, "builds", Point{Value: number(2)}, writer, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Auth.IssueSeriesToken(ctx, "collector", writer); !errors.Is(err, auth.ErrSeriesForbidden) {
		t.Fatalf("scoped token can mint: %v", err)
	}
	if err := s.Remove(ctx, "collector", false, writer); !errors.Is(err, auth.ErrSeriesForbidden) {
		t.Fatalf("scoped token can administer: %v", err)
	}
	// Keep the admitted request principal snapshot: transaction must not trust it.
	if err := s.Remove(ctx, "collector", false, human); err != nil {
		t.Fatal(err)
	}
	if err := s.Push(ctx, "builds", Point{Value: number(3)}, writer, now.Add(time.Second)); !errors.Is(err, auth.ErrSeriesForbidden) {
		t.Fatalf("stale admitted request survived removal: %v", err)
	}
	if _, err := s.Auth.IssueSeriesToken(ctx, "collector", owner); !errors.Is(err, auth.ErrSeriesForbidden) {
		t.Fatalf("revoked grant minted: %v", err)
	}
	events, _, err := s.Auth.ListAuditEvents(ctx, auth.AuthAuditListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	types := map[string]bool{}
	for _, e := range events {
		types[e.EventType] = true
	}
	for _, kind := range []string{"series_write_granted", "series_write_token_issued", "series_write_revoked"} {
		if !types[kind] {
			t.Fatalf("missing audit %s", kind)
		}
	}
	if err := s.Remove(ctx, "collector", true, human); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"series_points", "series_daily", "series_labels"} {
		var n int
		if err := s.DB.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("delete left %s: %d %v", table, n, err)
		}
	}
	items, err := s.List(ctx)
	if err != nil || len(items) != 0 {
		t.Fatalf("delete left series: %#v %v", items, err)
	}
}
func TestQueryFreshnessLabelsCapsAndIdempotency(t *testing.T) {
	ctx := context.Background()
	s, _, _, writer := fixture(t)
	now := time.Now().UTC()
	point := Point{Value: number(4), Labels: map[string]string{"initiative": "launch"}, TS: now.Format(time.RFC3339Nano)}
	if err := s.Push(ctx, "builds", point, writer, now); err != nil {
		t.Fatal(err)
	}
	if err := s.Push(ctx, "builds", point, writer, now); err != nil {
		t.Fatal(err)
	}
	point.Value = number(5)
	if err := s.Push(ctx, "builds", point, writer, now); err != nil {
		t.Fatalf("raw correction: %v", err)
	}
	var received int
	if err := s.DB.QueryRow(`SELECT n FROM series_ingestion_days`).Scan(&received); err != nil || received != 2 {
		t.Fatalf("retry/correction accounting: %d %v", received, err)
	}
	for _, age := range []time.Duration{120 * time.Second, 121 * time.Second} {
		r, err := s.Query(ctx, "builds", map[string]string{"initiative": "launch"}, time.Hour, time.Minute, "sum", now.Add(age))
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Streams) != 1 || len(r.Streams[0].Points) != 1 || *r.Streams[0].Points[0].Value != 5 || r.Streams[0].Stale != (age > 120*time.Second) {
			t.Fatalf("query/freshness: %#v", r)
		}
	}
	r, err := s.Query(ctx, "builds", map[string]string{"initiative": "other"}, time.Hour, time.Minute, "last", now)
	if err != nil || len(r.Streams) != 0 {
		t.Fatalf("filter leaked: %#v %v", r, err)
	}
	if _, err = s.Query(ctx, "builds", nil, time.Hour, time.Second, "last", now); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unbounded query: %v", err)
	}
	if _, err = s.DB.Exec(`UPDATE series_ingestion_days SET n=?`, MaxPointsPerDay); err != nil {
		t.Fatal(err)
	}
	if err = s.Push(ctx, "builds", Point{Value: number(5)}, writer, now.Add(time.Second)); !errors.Is(err, ErrCapacity) {
		t.Fatalf("point cap: %v", err)
	}
	if _, err = s.DB.Exec(`UPDATE series_ingestion_days SET n=1`); err != nil {
		t.Fatal(err)
	}
	for i := 1; i < MaxLabelSets; i++ {
		_, err = s.DB.Exec(`INSERT INTO series_labels(series,labels) VALUES('builds',json_object('slot',?))`, i)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Push(ctx, "builds", Point{Value: number(5), Labels: map[string]string{"slot": "new"}}, writer, now); !errors.Is(err, ErrCapacity) {
		t.Fatalf("label cardinality cap: %v", err)
	}
}
func TestDailyRollupsPreserveAggregatesAndLastState(t *testing.T) {
	ctx := context.Background()
	s, _, _, writer := fixture(t)
	now := time.Now().UTC().Truncate(time.Second)
	for i, v := range []float64{2, 6} {
		if err := s.Push(ctx, "builds", Point{Value: number(v), TS: now.Add(-89*24*time.Hour + time.Duration(i)*time.Minute).Format(time.RFC3339Nano)}, writer, now); err != nil {
			t.Fatal(err)
		}
	}
	state := "healthy"
	if err := s.Push(ctx, "health", Point{State: &state, TS: now.Add(-89 * 24 * time.Hour).Format(time.RFC3339Nano)}, writer, now); err != nil {
		t.Fatal(err)
	}
	future := now.Add(3 * 24 * time.Hour)
	for agg, want := range map[string]float64{"sum": 8, "avg": 4, "min": 2, "max": 6, "last": 6, "count": 2} {
		r, err := s.Query(ctx, "builds", nil, 100*24*time.Hour, 24*time.Hour, agg, future)
		if err != nil {
			t.Fatal(err)
		}
		if len(r.Streams) != 1 || len(r.Streams[0].Points) != 1 || *r.Streams[0].Points[0].Value != want {
			t.Fatalf("rollup %s: %#v", agg, r)
		}
	}
	var count int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM series_points`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("raw retention count %d %v", count, err)
	}
	r, err := s.Query(ctx, "health", nil, 100*24*time.Hour, 24*time.Hour, "last", future)
	if err != nil || *r.Streams[0].Points[0].State != "healthy" {
		t.Fatalf("state rollup: %#v %v", r, err)
	}
}
func TestAuthorityRecheckedForHostExclusionAndTokenExpiry(t *testing.T) {
	ctx := context.Background()
	s, _, owner, writer := fixture(t)
	if _, err := s.DB.Exec(`INSERT INTO host_exclusions(host_id,name) VALUES('host','owner')`); err != nil {
		t.Fatal(err)
	}
	if err := s.Push(ctx, "builds", Point{Value: number(1)}, writer, time.Now()); !errors.Is(err, auth.ErrSeriesForbidden) {
		t.Fatalf("excluded owner: %v", err)
	}
	if _, err := s.Auth.IssueSeriesToken(ctx, "collector", owner); !errors.Is(err, auth.ErrSeriesForbidden) {
		t.Fatalf("excluded token exchange: %v", err)
	}
	if _, err := s.DB.Exec(`DELETE FROM host_exclusions`); err != nil {
		t.Fatal(err)
	}
	if err := s.Push(ctx, "builds", Point{Value: number(1)}, writer, time.Now().Add(time.Hour)); !errors.Is(err, auth.ErrSeriesForbidden) {
		t.Fatalf("expired admitted token: %v", err)
	}
}

func TestWorkspaceSeriesCapAndScopedDeclarationDenial(t *testing.T) {
	ctx := context.Background()
	s, human, owner, writer := fixture(t)
	d := Declaration{Name: "extra", Description: "More data", AgentID: owner.AgentID, ExpectedInterval: "1m", Series: []Definition{{Name: "more", Kind: "counter", Unit: "items"}}}
	if _, err := s.Declare(ctx, d, writer); !errors.Is(err, auth.ErrSeriesForbidden) {
		t.Fatalf("scoped declaration: %v", err)
	}
	tx, err := s.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 2; i < MaxSeries; i++ {
		if _, err = tx.Exec(`INSERT INTO series_definitions(name,adapter,unit,kind) VALUES(printf('s%d',?),'collector','items','gauge')`, i); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Declare(ctx, d, human); !errors.Is(err, ErrCapacity) {
		t.Fatalf("series cap: %v", err)
	}
}
