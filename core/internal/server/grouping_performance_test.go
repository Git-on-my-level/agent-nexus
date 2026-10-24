package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

// Measure the complete collection request, including authentication and cold
// canonical admission. The general route corpus is shared; unrelated PM and
// stream runtime setup is unnecessary for this finite work collection.
func TestPerformanceGroupingFeedBudget(t *testing.T) {
	requirePerformanceTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	raw := env.workspace.DB()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, raw, "scale-owner", "scale-owner-actor", "scale.owner", "scale-owner-token")
	stranger := seedMachinePrincipalForLockoutTest(t, ctx, raw, "scale-stranger", "scale-stranger-actor", "scale.stranger", "scale-stranger-token")
	if err := perfguard.Seed(ctx, raw, env.workspace.Layout().ArtifactContentDir, owner.ActorID, time.Now().UTC().Truncate(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var budget routeBudget
	for _, b := range performanceBudgets(t) {
		if b.Method == http.MethodGet && b.Path == "/work/ungrouped" {
			budget = b
		}
	}
	if budget.Path == "" {
		t.Fatal("missing grouping route budget")
	}
	for pi, principal := range []lockoutPrincipalSeed{owner, stranger} {
		label := "authorized"
		if pi == 1 {
			label = "unauthorized"
		}
		t.Run(label, func(t *testing.T) {
			db, capture, err := perfguard.Open("file:" + env.workspace.Layout().DatabasePath + "?_pragma=busy_timeout(20000)&_pragma=journal_mode(WAL)&_txlock=immediate")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			store := primitives.NewStore(db, blob.NewFilesystemBackend(env.workspace.Layout().ArtifactContentDir), env.workspace.Layout().ArtifactContentDir)
			h := NewHandler("grouping-scale", WithPrimitiveStore(store), WithAuthStore(auth.NewStore(db)), WithActorRegistry(actors.NewStore(db)))
			for sample := 0; sample < 7; sample++ {
				phase := "warm"
				if sample == 0 {
					phase = "first_read"
				} else if sample == 6 {
					phase = "post_invalidation"
					var before, after int64
					if err := raw.QueryRow(`SELECT version FROM resource_access_epoch WHERE singleton=1`).Scan(&before); err != nil {
						t.Fatal(err)
					}
					if _, err := raw.Exec(`UPDATE threads SET updated_by=updated_by WHERE id='scale-thread-1'`); err != nil {
						t.Fatal(err)
					}
					if err := raw.QueryRow(`SELECT version FROM resource_access_epoch WHERE singleton=1`).Scan(&after); err != nil || after <= before {
						t.Fatalf("invalid epoch control: %d -> %d err=%v", before, after, err)
					}
				}
				requestCtx, cancel := context.WithTimeout(ctx, performanceMaxSample(budget.LatencyMS))
				r := httptest.NewRequest(http.MethodGet, budget.Path+"?"+budget.Query, nil).WithContext(requestCtx)
				r.Header.Set("Authorization", "Bearer "+principal.AccessToken)
				w := httptest.NewRecorder()
				capture.Start()
				started := time.Now()
				h.ServeHTTP(w, r)
				elapsed := time.Since(started)
				_, queries, rows := capture.Stop()
				work := capture.Work()
				cancel()
				t.Logf("%s sample=%d queries=%d rows=%d VM=%d scans=%d elapsed=%v status=%d", phase, sample, queries, rows, work.VMSteps, work.FullScanSteps, elapsed, w.Code)
				if capture.WorkError() != nil || w.Code != http.StatusOK {
					t.Fatalf("instrumentation=%v status=%d body=%.500s", capture.WorkError(), w.Code, w.Body.String())
				}
				var page struct {
					Work []struct {
						Ref string `json:"ref"`
					} `json:"work"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil || len(page.Work) != 20 {
					t.Fatalf("missing positive feed control: rows=%d err=%v", len(page.Work), err)
				}
				for _, item := range page.Work {
					n, err := strconv.Atoi(strings.TrimPrefix(item.Ref, "card:scale-card-"))
					if err != nil || pi == 1 && n%16 == 0 {
						t.Fatalf("unexpected or private card: %q", item.Ref)
					}
				}
				if performanceCountsExceeded(budget, queries, rows, work.VMSteps) {
					t.Errorf("%s exceeds ordinary budget: queries=%d rows=%d VM=%d", phase, queries, rows, work.VMSteps)
				}
			}
		})
	}
}
