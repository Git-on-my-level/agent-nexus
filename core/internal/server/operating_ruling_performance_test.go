package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPerformanceOperatingRulingsRoutes(t *testing.T) {
	requirePerformanceTest(t)
	env := newPerformanceEnv(t)
	tx, err := env.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	var mostRecent string
	for i := 0; i < 4096; i++ {
		id := fmt.Sprintf("R-%s-%02d", start.AddDate(0, 0, i/99).Format("20060102"), i%99+1)
		status := "active"
		if i > 4080 {
			status = "proposed"
		}
		if _, err := tx.Exec(`INSERT INTO standing_rulings(id,title_key,kind,question,created_by,created_at,ruling_id,ruling_title,body,source_quote,status,topic)
		 VALUES(?,'','operating','','scale-author',?,?,'Scale ruling','Operating rule.','Source quote.',?,?)`, id, start.Add(time.Duration(i)*time.Second).Format(time.RFC3339), id, status, fmt.Sprintf("ruling-topic-%d", i)); err != nil {
			t.Fatal(err)
		}
		mostRecent = id
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	for _, budget := range performanceBudgets(t) {
		if budget.Path != "/rulings" && budget.Path != "/rulings/projection" && budget.Path != "/brief" {
			continue
		}
		for i, principal := range env.principals {
			t.Run(fmt.Sprintf("%s/principal-%d", budget.Path, i), func(t *testing.T) {
				get := func(w *httptest.ResponseRecorder) {
					req := httptest.NewRequest(http.MethodGet, budget.Path, nil)
					req.Header.Set("Authorization", "Bearer "+principal.AccessToken)
					env.handler.ServeHTTP(w, req)
				}
				get(httptest.NewRecorder())
				env.capture.Start()
				w := httptest.NewRecorder()
				begin := time.Now()
				get(w)
				elapsed := time.Since(begin)
				_, queries, rows := env.capture.Stop()
				work := env.capture.Work()
				if err := env.capture.WorkError(); err != nil {
					t.Fatal(err)
				}
				if w.Code != 200 || performanceCountsExceeded(budget, queries, rows, work.VMSteps) || elapsed > time.Duration(budget.LatencyMS)*time.Millisecond {
					t.Fatalf("status=%d queries=%d rows=%d VM=%d elapsed=%s; budget=%+v", w.Code, queries, rows, work.VMSteps, elapsed, budget)
				}
				if budget.Path == "/brief" && i == 0 && !strings.Contains(w.Body.String(), mostRecent) {
					t.Fatal("human brief omitted the newest pending ruling")
				}
				if budget.Path == "/rulings/projection" && (!strings.Contains(w.Body.String(), "next_cursor") || strings.Contains(w.Body.String(), mostRecent)) {
					t.Fatal("projection did not limit the first page")
				}
				t.Logf("queries=%d rows=%d VM=%d elapsed=%s", queries, rows, work.VMSteps, elapsed)
			})
		}
	}
}
