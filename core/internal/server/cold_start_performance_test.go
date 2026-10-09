package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime/pprof"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/testutil/perfguard"
)

// Reopen the pool and rebuild the native handler for each route/principal.
// The fixture deliberately runs ANALYZE with mostly NULL optional parents:
// SQLite must not turn their recursive point probes into table scans.
func TestPerformanceColdStartReads(t *testing.T) {
	// Serial: performance samples must not compete with parallel fixtures.
	requirePerformanceTest(t)
	env := newPerformanceEnv(t)
	if path := os.Getenv("ANX_COLD_START_PROFILE"); path != "" {
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err = pprof.StartCPUProfile(f); err != nil {
			f.Close()
			t.Fatal(err)
		}
		defer func() { pprof.StopCPUProfile(); f.Close() }()
	}
	for _, b := range performanceBudgets(t) {
		if b.Path != "/inbox" && b.Path != "/overview" {
			continue
		}
		for pi, principal := range env.principals {
			label := []string{"owner", "denied"}[pi]
			t.Run(b.Path+"/"+label, func(t *testing.T) {
				preparePerformanceVisit(t, env, b, principal, pi)
				h, capture, _, closePool := env.fresh(t)
				defer closePool()
				var firstScans, firstVM uint64
				for sample := 0; sample < 2; sample++ {
					req := httptest.NewRequest(http.MethodGet, b.Path, nil)
					req.Header.Set("Authorization", "Bearer "+principal.AccessToken)
					w := httptest.NewRecorder()
					capture.Start()
					start := time.Now()
					h.ServeHTTP(w, req)
					elapsed := time.Since(start)
					statements, queries, rows := capture.Stop()
					work := capture.Work()
					if w.Code != http.StatusOK || capture.WorkError() != nil {
						t.Fatalf("status=%d instrumentation=%v", w.Code, capture.WorkError())
					}
					validatePerformanceVisit(t, b, sample, w.Body.Bytes())
					if strings.Contains(w.Body.String(), "PrivatePerformanceSecret") || pi == 1 && strings.Contains(w.Body.String(), "Private scale target") {
						t.Fatal("cold or warm response exposed a private fixture")
					}
					visible := "Scale inbox item"
					if b.Path == "/overview" {
						visible = "Scale card"
					}
					if !strings.Contains(w.Body.String(), visible) {
						t.Fatalf("cold or warm response omitted public content: %s", visible)
					}
					if sample == 0 {
						firstScans, firstVM = work.FullScanSteps, work.VMSteps
						closures := 0
						for _, s := range statements {
							if !strings.Contains(s.SQL, "json_group_array(json_array(kind,id))") {
								continue
							}
							// Inspect the actual submitted closure, including its fallback.
							closures++
							plan, err := perfguard.Explain(context.Background(), env.db, s)
							if err != nil {
								t.Fatal(err)
							}
							joined := strings.Join(plan, "\n")
							for _, probe := range []string{
								"SEARCH r USING INDEX idx_cards_parent_thread_id (parent_thread_id=?)",
								"SEARCH r USING INDEX idx_wakeups_access_trigger_event (trigger_event_id=?)",
								"SEARCH m USING INDEX sqlite_autoindex_work_metadata_1 (card_id=?)",
							} {
								if !strings.Contains(joined, probe) {
									t.Fatalf("missing indexed recursive probe: %s", probe)
								}
							}
							for _, detail := range plan {
								if detail == "SCAN r" || strings.HasPrefix(detail, "SCAN m USING INDEX idx_work_source_identity") {
									t.Fatalf("recursive authorization scanned unrelated records: %s", detail)
								}
							}
						}
						if closures == 0 {
							t.Fatal("fresh handler did not capture an authorization closure")
						}
					} else {
						// Existing collection/visit work has separate scale baselines.
						// Bound only the added cold traversal, without widening those.
						if firstScans > work.FullScanSteps+10000 || firstVM > work.VMSteps+1500000 {
							t.Fatalf("cold traversal overhead: first scans/vm=%d/%d second=%d/%d", firstScans, firstVM, work.FullScanSteps, work.VMSteps)
						}
					}
					t.Logf("sample=%d elapsed=%s queries=%d rows=%d vm=%d fullscan=%d", sample, elapsed, queries, rows, work.VMSteps, work.FullScanSteps)
				}
			})
		}
	}
}
