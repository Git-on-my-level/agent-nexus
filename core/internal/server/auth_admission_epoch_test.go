package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

// Interleave committed canonical writes with independent requests on one pool.
// Epoch changes must not turn a bounded read into a workspace closure capture.
func TestPerformanceCanonicalEpochChurn(t *testing.T) {
	requirePerformanceTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	raw := env.workspace.DB()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, raw, "churn-owner", "scale-owner-actor", "churn.owner", "churn-owner-token")
	reader := seedMachinePrincipalForLockoutTest(t, ctx, raw, "churn-reader", "scale-stranger-actor", "churn.reader", "churn-reader-token")
	if err := perfguard.Seed(ctx, raw, env.workspace.Layout().ArtifactContentDir, owner.ActorID, time.Now().UTC().Truncate(time.Hour)); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"idle", "thread_attribution", "public_card_title", "public_event_append"} {
		t.Run(mode, func(t *testing.T) {
			db, capture, err := perfguard.Open("file:" + env.workspace.Layout().DatabasePath + "?_pragma=busy_timeout(20000)&_pragma=journal_mode(WAL)")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			store := primitives.NewTestStore(db, env.workspace.Layout().ArtifactContentDir)
			h := NewHandler("epoch-churn", WithPrimitiveStore(store), WithAuthStore(auth.NewStore(db)), WithActorRegistry(actors.NewStore(db)))
			epoch := func() int64 {
				t.Helper()
				var n int64
				if err := raw.QueryRow(`SELECT version FROM resource_access_epoch WHERE singleton=1`).Scan(&n); err != nil {
					t.Fatal(err)
				}
				return n
			}
			changes, misses, probes := 0, 0, 0
			for sample := 0; sample < 9; sample++ {
				before := epoch()
				capture.Start()
				writeStarted := time.Now()
				if sample > 0 {
					var err error
					switch mode {
					case "thread_attribution":
						_, err = db.Exec(`UPDATE threads SET updated_by=? WHERE id='scale-thread-4095'`, fmt.Sprintf("background-writer-%d", sample))
					case "public_card_title":
						_, err = db.Exec(`UPDATE cards SET title=? WHERE id='scale-card-4095'`, fmt.Sprintf("Unrelated public edit %d", sample))
					case "public_event_append":
						_, err = db.Exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES(?,'message_posted',?,'scale-owner-actor','scale-thread-4095','[]','{"text":"ordinary background activity"}')`, fmt.Sprintf("churn-event-%d", sample), perfguard.Timestamp)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				writeElapsed := time.Since(writeStarted)
				capture.Stop()
				writeWork := capture.Work()
				if capture.WorkError() != nil {
					t.Fatal(capture.WorkError())
				}
				if writeWork.VMSteps > 50000 {
					t.Errorf("unrelated source write exceeded 50,000 VM: %d", writeWork.VMSteps)
				}
				current := epoch()
				if current != before {
					changes++
				}
				r := httptest.NewRequest(http.MethodGet, "/cards/scale-card-1", nil)
				r.Header.Set("Authorization", "Bearer "+reader.AccessToken)
				w := httptest.NewRecorder()
				capture.Start()
				started := time.Now()
				h.ServeHTTP(w, r)
				elapsed := time.Since(started)
				statements, _, _ := capture.Stop()
				captures := 0
				for _, stmt := range statements {
					if strings.Contains(stmt.SQL, "json_group_array(json_array(kind,id)) FROM _anx_denied") {
						probes++
						// A CASE-guarded probe with the current cached epoch does
						// not evaluate the recursive closure. A request may probe
						// twice, so count the actual stale bindings, not requests.
						if len(stmt.Args) == 0 || fmt.Sprint(stmt.Args[0]) != fmt.Sprint(current) {
							captures++
						}
					}
				}
				misses += captures
				work := capture.Work()
				t.Logf("sample=%d epoch=%d delta=%d closure_captures=%d VM=%d elapsed=%s write_VM=%d write_elapsed=%s", sample, current, current-before, captures, work.VMSteps, elapsed, writeWork.VMSteps, writeElapsed)
				if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Scale card 1") || capture.WorkError() != nil {
					t.Fatalf("status=%d instrumentation=%v body=%.200s", w.Code, capture.WorkError(), w.Body.String())
				}
				if captures != 0 || work.VMSteps > 50000 {
					t.Errorf("bounded read rebuilt closure or exceeded unchanged 50,000 ceiling: captures=%d VM=%d", captures, work.VMSteps)
				}
			}
			wantChanges := 8
			if mode == "idle" {
				wantChanges = 0
			}
			if changes != wantChanges {
				t.Errorf("epoch changes=%d want=%d", changes, wantChanges)
			}
			t.Logf("summary requests=9 interleaved_writes=%d epoch_changes=%d closure_probes=%d closure_misses=%d", wantChanges, changes, probes, misses)
		})
	}
}
