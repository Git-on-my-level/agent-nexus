package series

import (
	"context"
	"testing"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/resourceaccess"
)

// Use the real principal policy from HTTP in server tests; this focused test
// exercises scoped prepared/bulk queries and compaction with a deterministic
// denied-reference policy, independent of canonical fixture construction.
func TestSeriesScopedQueriesRetainPrivateContributors(t *testing.T) {
	s, human, _, writer := fixture(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	str := func(s string) *string { return &s }
	for _, point := range []Point{
		{Labels: map[string]string{"ref": "card:private"}, State: str("SECRET"), TS: now.Add(-time.Minute).Format(time.RFC3339Nano)},
		{Labels: map[string]string{"stream": "private-state"}, State: str("card:private"), TS: now.Add(-2 * time.Minute).Format(time.RFC3339Nano)},
		{Labels: map[string]string{"stream": "private-state"}, State: str("ok"), TS: now.Add(-time.Minute).Format(time.RFC3339Nano)},
		{Labels: map[string]string{"stream": "public"}, State: str("ok"), TS: now.Add(-time.Minute).Format(time.RFC3339Nano)},
	} {
		if err := s.Push(ctx, "health", point, writer, now); err != nil {
			t.Fatal(err)
		}
	}
	policy := resourceaccess.Policy{Read: func(q string) string {
		prefix := `WITH denied AS (SELECT series,labels FROM main.resource_access_series_refs WHERE target_ref='card:private'), series_labels AS (SELECT * FROM main.series_labels WHERE NOT EXISTS (SELECT 1 FROM denied d WHERE d.series=series_labels.series AND d.labels=series_labels.labels)), series_points AS (SELECT * FROM main.series_points WHERE NOT EXISTS (SELECT 1 FROM denied d WHERE d.series=series_points.series AND d.labels=series_points.labels)), series_daily AS (SELECT * FROM main.series_daily WHERE NOT EXISTS (SELECT 1 FROM denied d WHERE d.series=series_daily.series AND d.labels=series_daily.labels)), series_live_daily AS (SELECT * FROM main.series_live_daily WHERE NOT EXISTS (SELECT 1 FROM denied d WHERE d.series=series_live_daily.series AND d.labels=series_live_daily.labels)) `
		if len(q) > 4 && q[:4] == "WITH" {
			return prefix + "," + q[4:]
		}
		return prefix + q
	}}
	scoped := resourceaccess.WithPolicy(ctx, policy)
	raw, err := s.Query(scoped, "health", nil, 2*time.Hour, time.Minute, "last", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw.Streams) != 1 || raw.Streams[0].Labels["stream"] != "public" {
		t.Fatalf("prepared raw buckets leaked: %#v", raw.Streams)
	}
	for _, future := range []time.Time{now, now.Add(Retention + time.Hour)} {
		if future.After(now) {
			tx, err := s.DB.BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			if err = compact(ctx, tx, future); err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
		}
		result, err := s.Query(scoped, "health", nil, 200*24*time.Hour, 24*time.Hour, "last", future)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Streams) != 1 || result.Streams[0].Labels["stream"] != "public" {
			t.Fatalf("private aggregate escaped: %#v", result.Streams)
		}
		owner, err := s.Query(ctx, "health", nil, 200*24*time.Hour, 24*time.Hour, "last", future)
		if err != nil {
			t.Fatal(err)
		}
		if len(owner.Streams) != 3 {
			t.Fatalf("authorized streams=%d", len(owner.Streams))
		}
	}
	if _, err := s.DB.Exec(`INSERT INTO resource_access_series_unknown VALUES('health','{}')`); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(ctx, "collector", true, human); err != nil {
		t.Fatal(err)
	}
	var remaining int
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM resource_access_series_refs`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("deleted adapter retained refs: %d %v", remaining, err)
	}
	if err := s.DB.QueryRow(`SELECT COUNT(*) FROM resource_access_series_unknown`).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatalf("deleted adapter retained uncertainty: %d %v", remaining, err)
	}
	if _, err := s.Declare(ctx, Declaration{Name: "collector-new", Description: "replacement", AgentID: "owner", ExpectedInterval: "1m", Series: []Definition{{Name: "health", Kind: "state", Unit: "state"}}}, human); err != nil {
		t.Fatal(err)
	}
	tokens, err := s.Auth.IssueSeriesToken(ctx, "collector-new", auth.Principal{AgentID: "owner", ActorID: "owner", PrincipalKind: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := s.Auth.AuthenticateAccessToken(ctx, tokens.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Push(ctx, "health", Point{Labels: map[string]string{"stream": "private-state"}, State: str("public now"), TS: now.Format(time.RFC3339Nano)}, replacement, now); err != nil {
		t.Fatal(err)
	}
	result, err := s.Query(scoped, "health", nil, 2*time.Hour, time.Minute, "last", now)
	if err != nil || len(result.Streams) != 1 {
		t.Fatalf("series name reuse retained old privacy: %#v %v", result, err)
	}

}
