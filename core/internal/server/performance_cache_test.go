package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/testutil/perfguard"
)

// Exercise the actual cache boundary, rather than trusting a report's label.
func TestPerformanceColdStateIsolation(t *testing.T) {
	requirePerformanceTest(t)
	env := newPerformanceEnv(t)
	h, capture, _, closePool := env.fresh(t)
	defer closePool()
	read := func(handler http.Handler, c *perfguard.Capture) []perfguard.Statement {
		req := httptest.NewRequest("GET", "/runs", nil)
		req.Header.Set("Authorization", "Bearer "+env.principals[1].AccessToken)
		w := httptest.NewRecorder()
		c.Start()
		handler.ServeHTTP(w, req)
		statements, _, _ := c.Stop()
		if w.Code != http.StatusOK || c.WorkError() != nil {
			t.Fatalf("cache control request: status%d work%v", w.Code, c.WorkError())
		}
		return statements
	}
	closure := func(statements []perfguard.Statement, cached bool, candidate int64) bool {
		for _, s := range statements {
			if !strings.Contains(s.SQL, "json_group_array") {
				continue
			}
			isCached := strings.Contains(s.SQL, "_anx_snapshot_epoch")
			if isCached != cached {
				continue
			}
			if !cached {
				return true
			}
			for _, arg := range s.Args {
				if value, ok := arg.(int64); ok && value == candidate {
					return true
				}
			}
		}
		return false
	}
	if !closure(read(h, capture), false, 0) {
		t.Fatal("fresh pool did not execute uncached ownership closure")
	}
	var epoch int64
	if err := env.db.QueryRow(`SELECT version FROM resource_access_epoch WHERE singleton=1`).Scan(&epoch); err != nil {
		t.Fatal(err)
	}
	if !closure(read(h, capture), true, epoch) {
		t.Fatal("same pool did not retain the warm closure")
	}
	before, after := invalidatePerformanceCache(t, env)
	if before != epoch || after <= epoch || !closure(read(h, capture), true, epoch) {
		t.Fatal("post-invalidation did not exercise a stale candidate epoch")
	}
	h2, c2, _, close2 := env.fresh(t)
	defer close2()
	if !closure(read(h2, c2), false, 0) {
		t.Fatal("a second fresh pool inherited the first pool's cache")
	}
	if performanceBaselineKey("GET", "/runs", "", "unauthorized", "first_read") == performanceBaselineKey("GET", "/runs", "", "unauthorized", "warm") {
		t.Fatal("cold allowances can widen warm ceilings")
	}
}

func preparePerformanceVisit(t *testing.T, env performanceEnv, b routeBudget, principal lockoutPrincipalSeed, pi int) {
	t.Helper()
	if b.Path != "/overview" && b.Path != "/overview/changes" {
		return
	}
	kind := "human"
	if pi == 1 {
		kind = "agent"
	}
	principalID := kind + ":" + principal.AgentID
	if _, err := env.db.Exec(`DELETE FROM overview_visits WHERE principal_id=?`, principalID); err != nil {
		t.Fatal(err)
	}
	if b.Case != "returning-visit" {
		return
	}
	// Persist an actual scoped snapshot outside the measured pool, then place
	// the visit just before the fixture's one recent private decision. The old
	// synthetic PM history remains outside that interval.
	req := httptest.NewRequest("GET", "/overview", nil)
	req.Header.Set("Authorization", "Bearer "+principal.AccessToken)
	w := httptest.NewRecorder()
	env.setupHandler.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("returning-visit setup: %d", w.Code)
	}
	var created string
	if err := env.db.QueryRow(`SELECT json_extract(body,'$.created_at') FROM pm_records WHERE kind='decision' AND id='scale-target-decision'`).Scan(&created); err != nil {
		t.Fatal(err)
	}
	at, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		t.Fatal(err)
	}
	r, err := env.db.Exec(`UPDATE overview_visits SET visited_at=? WHERE principal_id=?`, at.Add(-time.Second).Format(time.RFC3339Nano), principalID)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := r.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("missing scoped returning visit: %d %v", n, err)
	}
}

func validatePerformanceVisit(t *testing.T, b routeBudget, sample int, body []byte) {
	t.Helper()
	if b.Path != "/overview" && b.Path != "/overview/changes" {
		return
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if b.Path == "/overview" {
		if err := json.Unmarshal(payload["since_you_last_looked"], &payload); err != nil {
			t.Fatal(err)
		}
	}
	since, ok := payload["since"]
	if !ok {
		t.Fatal("overview omitted visit-state control")
	}
	returning := b.Case == "returning-visit" || (b.Path == "/overview" && sample > 0)
	if (string(since) != "null") != returning {
		t.Fatalf("overview visit-state changed: returning=%v since=%s", returning, since)
	}
}
