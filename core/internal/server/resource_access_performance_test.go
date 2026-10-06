package server

import (
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"testing"
	"time"
)

// This deliberately exceeds a medium workspace, with prose-bearing rows in
// every canonical collection. The former closure matched all stored prose once
// per denied identity, making even identity and bounded collection reads stall.
func TestResourceAccessCommonReadPerformance(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	s := env.primitiveStore.(*primitives.Store)
	if _, err := pm.NewStore(env.workspace.DB()); err != nil {
		t.Fatal(err)
	}
	stranger := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "perf-reader", "perf-reader-actor", "perf-reader", "perf-reader-token")
	for i := 0; i < 10; i++ {
		board, err := s.CreateBoard(ctx, "private-owner", map[string]any{"title": fmt.Sprintf("private-%d", i)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.PatchThread(ctx, "private-owner", anyString(board["thread_id"]), map[string]any{"pm_actor_id": "private-owner"}, nil); err != nil {
			t.Fatal(err)
		}
		if _, err = s.CreateBoardCard(ctx, "private-owner", anyString(board["id"]), primitives.AddBoardCardInput{Title: "PrivatePerformanceSecret", Body: "secret", ColumnKey: "ready"}); err != nil {
			t.Fatal(err)
		}
	}
	publicBoard, err := s.CreateBoard(ctx, "public-writer", map[string]any{"title": "public fixture"})
	if err != nil {
		t.Fatal(err)
	}
	readerAgent := seedMachinePrincipalForLockoutTest(t, ctx, env.workspace.DB(), "perf-agent", "perf-agent-actor", "perf.agent", "perf-agent-token")
	tx, err := env.workspace.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	queries := []string{
		`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES(?,'2026-01-01T00:00:00Z','public-writer','{}')`,
		`INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) VALUES(?,'message_posted','2026-01-01T00:00:00Z','public-writer','[]',?)`,
		`INSERT INTO artifacts(id,kind,created_at,created_by,content_type,content_hash,content_refs_json,metadata_json) VALUES(?,'note','2026-01-01T00:00:00Z','public-writer','text','fixture','[]',?)`,
		`INSERT INTO documents(id,title,head_revision_id,head_revision_number,created_at,created_by,updated_at,updated_by,search_text) VALUES(?,'Public fixture','fixture',1,'2026-01-01T00:00:00Z','public-writer','2026-01-01T00:00:00Z','public-writer',?)`,
		`INSERT INTO cards(id,title,created_at,created_by,updated_at,updated_by,summary,board_id,handle,head_revision_id) VALUES(?,'Public fixture','2026-01-01T00:00:00Z','public-writer','2026-01-01T00:00:00Z','public-writer',?,?,?,'fixture')`,
	}
	for table, q := range queries {
		stmt, err := tx.PrepareContext(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		count := 3000
		if table == 0 {
			count = 20
		}
		if table == 4 {
			count = 1000
		}
		for i := 0; i < count; i++ {
			id := fmt.Sprintf("%08x-row-%d", i, table)
			text := fmt.Sprintf("Public fixture links to document:%08x-absent. More public text.", i)
			args := []any{id}
			if table > 0 {
				if table == 1 || table == 2 {
					text = fmt.Sprintf(`{"text":%q}`, text)
				}
				args = append(args, text)
			}
			if table == 4 {
				args = append(args, publicBoard["id"], id)
			}
			if _, err := stmt.ExecContext(ctx, args...); err != nil {
				t.Fatal(err)
			}
		}
		stmt.Close()
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// Real PM record kinds reference public boards, cards and their plans. Both
	// PM bodies and inbox bodies used to take separate read-time scan paths.
	for _, kind := range []string{"conversation", "decision", "action"} {
		bulk, err := env.workspace.DB().BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		stmt, err := bulk.Prepare(`INSERT INTO pm_records VALUES(?,?,'ws_main','public-writer','',1,?)`)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 1000; i++ {
			ref := fmt.Sprintf("card:%08x-row-4", i)
			if kind == "conversation" {
				ref = "board:" + anyString(publicBoard["id"])
			}
			if kind == "action" {
				ref = fmt.Sprintf("plan:%08x-row-4", i)
			}
			body, _ := json.Marshal(map[string]any{"id": fmt.Sprintf("perf-%s-%d", kind, i), "work_ref": ref, "text": "Public PM evidence"})
			if _, err = stmt.Exec(kind, fmt.Sprintf("perf-%s-%d", kind, i), body); err != nil {
				t.Fatal(err)
			}
		}
		stmt.Close()
		if err = bulk.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	bulk, err := env.workspace.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 1000; i++ {
		if _, err = bulk.Exec(`INSERT INTO card_plans VALUES(?,'{}','now')`, fmt.Sprintf("%08x-row-4", i)); err != nil {
			t.Fatal(err)
		}
		item := streamPrivacyInboxItem(anyString(publicBoard["thread_id"]), fmt.Sprintf("perf-inbox-%d", i), "Public inbox evidence")
		item.SourceEventID = fmt.Sprintf("%08x-row-1", i)
		body, _ := json.Marshal(item.Data)
		if _, err = bulk.Exec(`INSERT INTO derived_inbox_items(id,thread_id,source_event_id,category,trigger_at,generated_at,data_json) VALUES(?,?,?,'ask','2026-01-01T00:00:00Z','now',?)`, item.ID, item.ThreadID, item.SourceEventID, body); err != nil {
			t.Fatal(err)
		}
	}
	if err = bulk.Commit(); err != nil {
		t.Fatal(err)
	}
	t.Run("pm-point", func(t *testing.T) {
		db := resourceaccess.NewDB(env.workspace.DB())
		scope := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: stranger.ActorID})
		var samples []time.Duration
		for i := 0; i < 27; i++ {
			start := time.Now()
			var body []byte
			if err := db.QueryRowContext(scope, `SELECT body FROM pm_records WHERE kind='decision' AND id='perf-decision-0'`).Scan(&body); err != nil {
				t.Fatal(err)
			}
			if i >= 2 {
				samples = append(samples, time.Since(start))
			}
		}
		p95 := performanceP95(samples)
		t.Logf("p95=%s", p95)
		if p95 > 200*time.Millisecond {
			t.Fatalf("PM point p95=%s exceeds 200ms", p95)
		}
	})
	t.Run("selector-and-stream-poll", func(t *testing.T) {
		samples := []time.Duration{}
		for i := 0; i < 27; i++ {
			// A fresh scope on every tick mirrors stream readers, which must not
			// reuse a connection-wide decision after private ownership changes.
			scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: stranger.ActorID})
			start := time.Now()
			if err := s.CheckResourceValues(scope, []string{"card:missing-public"}); err != nil {
				t.Fatal(err)
			}
			limit := 50
			if _, err := s.ListEvents(scope, primitives.EventListFilter{Limit: limit}); err != nil {
				t.Fatal(err)
			}
			if i >= 2 {
				samples = append(samples, time.Since(start))
			}
		}
		if p95 := performanceP95(samples); p95 > 200*time.Millisecond {
			t.Fatalf("selector/poll p95=%s exceeds 200ms", p95)
		} else {
			t.Logf("p95=%s", p95)
		}
	})
	client := &http.Client{Timeout: 5 * time.Second}
	for _, path := range []string{"/agents/me", "/inbox", "/inbox/summary", "/work?limit=20", "/overview", "/events?limit=50"} {
		t.Run(path, func(t *testing.T) {
			samples := make([]time.Duration, 0, 25)
			var projectionSamples []time.Duration
			for i := 0; i < 27; i++ {
				// Pair the overview with its canonical projection so sustained CI
				// CPU contention raises both measurements. Bounded selector/PM
				// budgets still catch workspace-wide authorization scans.
				if path == "/overview" {
					start := time.Now()
					baseline, err := s.Overview(ctx, nil, nil)
					if err != nil {
						t.Fatal(err)
					}
					if _, err = json.Marshal(baseline); err != nil {
						t.Fatal(err)
					}
					if i >= 2 {
						projectionSamples = append(projectionSamples, time.Since(start))
					}
				}
				start := time.Now()
				req, err := http.NewRequest("GET", env.server.URL+path, nil)
				if err != nil {
					t.Fatal(err)
				}
				token := stranger.AccessToken
				if path == "/agents/me" {
					token = readerAgent.AccessToken
				}
				req.Header.Set("Authorization", "Bearer "+token)
				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != 200 {
					t.Fatalf("status=%d: %s", resp.StatusCode, body)
				}
				if strings.Contains(string(body), "PrivatePerformanceSecret") {
					t.Fatal("private content leaked")
				}
				if i >= 2 {
					samples = append(samples, time.Since(start))
				}
			}
			p95 := performanceP95(samples)
			t.Logf("p95=%s", p95)
			// Overview computes and serializes 1,000 initiatives; give that
			// projection a separate measured budget. All remain far below the
			// former authorization stall, even on CI's small runners.
			budget := 500 * time.Millisecond
			if path == "/overview" {
				budget = 1500 * time.Millisecond
				projection := performanceP95(projectionSamples)
				if relative := 4*projection + 500*time.Millisecond; relative > budget {
					budget = relative
				}
				t.Logf("canonical projection p95=%s; overview budget=%s", projection, budget)
			}
			if p95 > budget {
				t.Fatalf("p95=%s exceeds %s read budget", p95, budget)
			}
		})
	}
}

func performanceP95(samples []time.Duration) time.Duration {
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	return samples[(95*len(samples)+99)/100-1]
}
