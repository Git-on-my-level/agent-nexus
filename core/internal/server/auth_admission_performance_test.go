package server

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/series"
	"agent-nexus-core/internal/testutil/perfguard"
)

// Complete requests on the shared corpus, with a fresh pool for each route and
// reader. No other request may warm canonical authority before the first sample.
func TestPerformanceCanonicalAdmission(t *testing.T) {
	requirePerformanceTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	raw := env.workspace.DB()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, raw, "scale-owner", "scale-owner-actor", "scale.owner", "scale-owner-token")
	reader := seedMachinePrincipalForLockoutTest(t, ctx, raw, "scale-stranger", "scale-stranger-actor", "scale.stranger", "scale-stranger-token")
	if err := perfguard.Seed(ctx, raw, env.workspace.Layout().ArtifactContentDir, owner.ActorID, time.Now().UTC().Truncate(time.Hour)); err != nil {
		t.Fatal(err)
	}
	assertCanonicalAdmissionScaleParity(t, raw)
	for _, path := range []string{"/work?limit=20", "/brief", "/cards?limit=20", "/boards?limit=20", "/boards/scale-board-0/cards?limit=20", "/cards?summary=1&limit=20", "/boards/scale-board-0/cards?summary=1&limit=20", "/cards/scale-card-1", "/boards/scale-board-1"} {
		for pi, principal := range []lockoutPrincipalSeed{owner, reader} {
			label := []string{"owner", "partial"}[pi]
			t.Run(path+"/"+label, func(t *testing.T) {
				db, capture, err := perfguard.Open("file:" + env.workspace.Layout().DatabasePath + "?_pragma=busy_timeout(20000)&_pragma=journal_mode(WAL)&_txlock=immediate")
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				store := primitives.NewStore(db, blob.NewFilesystemBackend(env.workspace.Layout().ArtifactContentDir), env.workspace.Layout().ArtifactContentDir)
				as := auth.NewStore(db)
				h := NewHandler("admission-scale", WithPrimitiveStore(store), WithAuthStore(as), WithActorRegistry(actors.NewStore(db)), WithSeriesStore(&series.Store{DB: db, Auth: as}))
				for sample, phase := range []string{"first", "warm", "invalidated"} {
					if sample == 2 {
						var before, after int64
						if err := raw.QueryRow(`SELECT version FROM resource_access_epoch WHERE singleton=1`).Scan(&before); err != nil {
							t.Fatal(err)
						}
						if _, err := raw.Exec(`UPDATE threads SET updated_by=updated_by WHERE id='scale-thread-1'`); err != nil {
							t.Fatal(err)
						}
						if err := raw.QueryRow(`SELECT version FROM resource_access_epoch WHERE singleton=1`).Scan(&after); err != nil || after <= before {
							t.Fatalf("epoch %d -> %d: %v", before, after, err)
						}
					}
					r := httptest.NewRequest(http.MethodGet, path, nil)
					r.Header.Set("Authorization", "Bearer "+principal.AccessToken)
					w := httptest.NewRecorder()
					capture.Start()
					h.ServeHTTP(w, r)
					_, queries, rows := capture.Stop()
					work := capture.Work()
					t.Logf("%s status=%d queries=%d rows=%d VM=%d scans=%d", phase, w.Code, queries, rows, work.VMSteps, work.FullScanSteps)
					want := http.StatusOK
					if path == "/boards/scale-board-1" && pi == 1 {
						want = http.StatusNotFound
					}
					if w.Code != want || capture.WorkError() != nil {
						t.Fatalf("status=%d want=%d instrumentation=%v body=%.300s", w.Code, want, capture.WorkError(), w.Body.String())
					}
					if pi == 1 && (strings.Contains(w.Body.String(), `"id":"scale-card-0"`) || strings.Contains(w.Body.String(), `"ref":"card:scale-card-0"`) || strings.Contains(w.Body.String(), `"id":"scale-board-1"`)) {
						t.Fatal("private row leaked")
					}
					if path == "/cards/scale-card-1" && !strings.Contains(w.Body.String(), "Scale card 1") {
						t.Fatal("positive point control missing")
					}
					if work.VMSteps > 50000 {
						t.Errorf("%s exceeds unchanged 50,000 VM ceiling: %d", phase, work.VMSteps)
					}
				}
			})
		}
	}
}

func assertCanonicalAdmissionScaleParity(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	for _, scope := range []primitives.AccessScope{{ActorID: "scale-owner-actor"}, {ActorID: "scale-stranger-actor"}, {ActorID: "scale-owner-actor", PMActorID: "scale-owner-actor"}, {}} {
		read := func(rows *sql.Rows, err error) map[[2]string]bool {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			out := map[[2]string]bool{}
			for rows.Next() {
				var key [2]string
				if err := rows.Scan(&key[0], &key[1]); err != nil {
					t.Fatal(err)
				}
				out[key] = true
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			return out
		}
		want := read(resourceaccess.NewDB(db).QueryContext(primitives.WithAccessScope(ctx, scope), `SELECT kind,CAST(id AS TEXT) FROM _anx_denied`))
		constraint := "r.unconditional=1"
		var args []any
		if scope.ActorID == "" || scope.ActorID != scope.PMActorID {
			constraint += " OR r.owner<>?"
			args = []any{scope.ActorID}
		}
		got := read(db.QueryContext(ctx, `SELECT DISTINCT r.kind,r.id FROM resource_access_admission_owners r WHERE r.ledger=CASE WHEN `+resourceaccess.CompactReaderSQL+` THEN 2 WHEN NOT (`+resourceaccess.CompactReaderSQL+`) THEN 1 ELSE -1 END AND (`+constraint+`)`, args...))
		if !reflect.DeepEqual(got, want) {
			for key := range want {
				if !got[key] {
					t.Errorf("scope=%+v missing %s:%s", scope, key[0], key[1])
				}
			}
			for key := range got {
				if !want[key] {
					t.Errorf("scope=%+v extra %s:%s", scope, key[0], key[1])
				}
			}
			t.Fatal("scale canonical admission differs")
		}
		t.Logf("scale denial parity actor=%q pm=%q identities=%d", scope.ActorID, scope.PMActorID, len(got))
	}
}
