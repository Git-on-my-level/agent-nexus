package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
)

// Exercise ancillary filters, which reference the denial graph multiple times,
// with thousands of denied identities rather than only public history.
func TestResourceAccessLargeDenialPrepareAndPMRoutes(t *testing.T) {
	// Serial: performance samples must not compete with parallel fixtures.
	requirePerformanceTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	store := env.primitiveStore.(*primitives.Store)
	reader := seedHumanPrincipalForLockoutTest(t, ctx, db, "prepare-reader", "prepare-reader-actor", "prepare-reader", "prepare-reader-token")
	if _, err := db.Exec(`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) VALUES('prepare-host','prepare-host','Public host','user','machine','[]','now'); INSERT INTO host_keys VALUES('prepare-key','prepare-host','public','now',NULL); INSERT INTO host_agents VALUES('prepare-host','reader',?,'adopted')`, reader.AgentID); err != nil {
		t.Fatal(err)
	}
	rt, err := newOnboardedPMRuntime(t, db, store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main"}})
	if err != nil {
		t.Fatal(err)
	}
	pmServer := httptest.NewServer(rt)
	defer pmServer.Close()
	board, err := store.CreateBoard(ctx, reader.ActorID, map[string]any{"title": "Public prepare control"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateWork(ctx, reader.ActorID, anyString(board["id"]), map[string]any{"title": "Public prepare work"})
	if err != nil {
		t.Fatal(err)
	}
	bulk, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer bulk.Rollback()
	for i := 0; i < 1000; i++ {
		id := fmt.Sprintf("denied-%04d", i)
		for _, q := range []string{
			`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES(?,'now','owner','{"pm_actor_id":"owner"}')`,
			`INSERT INTO boards(id,title,thread_id,column_schema_json,created_at,created_by,updated_at,updated_by) VALUES(?,'PrivatePrepareSentinel',?,'[]','now','owner','now','owner')`,
			`INSERT INTO cards(id,title,board_id,handle,head_revision_id,created_at,created_by,updated_at,updated_by) VALUES(?,'PrivatePrepareSentinel',?,?,'head','now','owner','now','owner')`,
			`INSERT INTO documents(id,title,thread_id,head_revision_id,head_revision_number,created_at,created_by,updated_at,updated_by) VALUES(?,'PrivatePrepareSentinel',?,'head',1,'now','owner','now','owner')`,
			`INSERT INTO pm_records VALUES('decision',?,'ws_main','owner','',1,?)`,
		} {
			args := []any{id}
			switch {
			case strings.Contains(q, "INSERT INTO boards"), strings.Contains(q, "INSERT INTO documents"):
				args = append(args, id)
			case strings.Contains(q, "INSERT INTO cards"):
				args = append(args, id, id)
			case strings.Contains(q, "INSERT INTO pm_records"):
				args = append(args, fmt.Sprintf(`{"id":%q,"work_ref":%q,"instruction":"PrivatePrepareSentinel"}`, id, "card:"+id))
			}
			if _, err = bulk.Exec(q, args...); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err = bulk.Commit(); err != nil {
		t.Fatal(err)
	}
	scope := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: reader.ActorID})
	policy, _ := resourceaccess.PolicyFrom(scope)
	hostQuery := `SELECT h.id,h.slug,h.display_name,h.os_user,h.hostname,h.discovered_adapters_json,h.created_at,h.revoked_at,h.bridge_expires_at,COALESCE((SELECT id FROM host_keys WHERE host_id=h.id ORDER BY created_at DESC LIMIT 1),'') FROM hosts h WHERE h.id=? OR h.slug=?`
	query, args := policy.ReadOnDB(scope, db, hostQuery, []any{"prepare-host", "prepare-host"})
	if len(query) > 32000 || strings.Contains(query, "denied-0999") {
		t.Fatalf("denial data expanded SQL: %d bytes", len(query))
	}
	var prepared []time.Duration
	// modernc defers actual sqlite3_prepare_v2 until Query; EXPLAIN compiles the
	// real statement without executing its ownership/content scans.
	for i := 0; i < 27; i++ {
		start := time.Now()
		rows, err := db.QueryContext(ctx, "EXPLAIN "+query, args...)
		if err != nil {
			t.Fatal(err)
		}
		rows.Close()
		if i >= 2 {
			prepared = append(prepared, time.Since(start))
		}
	}
	p95 := performanceP95(prepared)
	t.Logf("rewritten bytes=%d; prepare p95=%s", len(query), p95)
	if p95 > 200*time.Millisecond {
		t.Fatalf("prepare p95=%s exceeds 200ms", p95)
	}
	// Bound values must preserve the real GetHost projection and correlated key.
	host, err := env.authStore.GetHost(scope, "prepare-host")
	if err != nil || host.KeyID != "prepare-key" {
		t.Fatalf("host projection: %v %v", host.KeyID, err)
	}
	for _, col := range []string{"display_name", "os_user", "hostname", "discovered_adapters_json"} {
		var value any = "card:denied-0999"
		if strings.HasSuffix(col, "_json") {
			value = `["card:denied-0999"]`
		}
		if _, err = db.Exec("UPDATE hosts SET "+col+"=? WHERE id='prepare-host'", value); err != nil {
			t.Fatal(err)
		}
		// The already-captured request must reject an epoch-invalidated profile.
		if _, err = env.authStore.GetHost(scope, "prepare-host"); err != auth.ErrHostNotFound {
			t.Fatalf("private %s visible: %v", col, err)
		}
		clean := "public"
		if strings.HasSuffix(col, "_json") {
			clean = "[]"
		}
		if _, err = db.Exec("UPDATE hosts SET "+col+"=? WHERE id='prepare-host'", clean); err != nil {
			t.Fatal(err)
		}
	}
	client := &http.Client{Timeout: 5 * time.Second}
	for _, tc := range []struct{ base, path string }{
		{env.server.URL, "/overview"},
		{pmServer.URL, "/pm/context?work_ref=" + anyString(work["ref"])},
		{pmServer.URL, "/pm/decisions?limit=20"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			var samples, denialSamples []time.Duration
			for i := 0; i < 27; i++ {
				// Pair with one fresh denial construction: large private graphs
				// have legitimate root/closure cost under concurrent CI load.
				fresh := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: reader.ActorID})
				baselineStart := time.Now()
				if err := store.CheckResourceValues(fresh, []string{anyString(work["ref"])}); err != nil {
					t.Fatal(err)
				}
				if i >= 2 {
					denialSamples = append(denialSamples, time.Since(baselineStart))
				}
				req, _ := http.NewRequest("GET", tc.base+tc.path, nil)
				req.Header.Set("Authorization", "Bearer "+reader.AccessToken)
				start := time.Now()
				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil || resp.StatusCode != 200 {
					t.Fatalf("route status=%d err=%v", resp.StatusCode, err)
				}
				if strings.Contains(string(body), "PrivatePrepareSentinel") {
					t.Fatal("private denial fixture leaked")
				}
				if !json.Valid(body) {
					t.Fatal("invalid response")
				}
				if i == 0 {
					t.Logf("cold HTTP=%s", time.Since(start))
				}
				if i >= 2 {
					samples = append(samples, time.Since(start))
				}
			}
			p95 := performanceP95(samples)
			t.Logf("HTTP p95=%s", p95)
			budget := 1500 * time.Millisecond
			if relative := 12*performanceP95(denialSamples) + 500*time.Millisecond; relative > budget {
				budget = relative
			}
			t.Logf("fresh denial p95=%s; route budget=%s", performanceP95(denialSamples), budget)
			if p95 > budget {
				t.Fatalf("large-denial route p95=%s exceeds %s", p95, budget)
			}
		})
	}
}
