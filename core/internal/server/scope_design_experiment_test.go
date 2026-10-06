package server

import (
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// EXPERIMENT ONLY: current real HTTP handlers versus a read-model SQL kernel.
// No production changes, no hosted data, no claim of wire or policy equivalence.
func TestScopeDesignExperiment(t *testing.T) {
	runtime.GOMAXPROCS(1)
	for _, scale := range []int{1, 10} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			env := newAuthIntegrationEnv(t, authIntegrationOptions{})
			ctx := context.Background()
			s := env.primitiveStore.(*primitives.Store)
			if _, err := pm.NewStore(env.workspace.DB()); err != nil {
				t.Fatal(err)
			}
			stranger := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "perf-reader", "perf-reader-actor", "perf-reader", "perf-reader-token")
			for i := 0; i < 10*scale; i++ {
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
			_ = seedMachinePrincipalForLockoutTest(t, ctx, env.workspace.DB(), "perf-agent", "perf-agent-actor", "perf.agent", "perf-agent-token")
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
				count := []int{499, 2759, 806, 26, 433}[table] * scale
				for i := 0; i < count; i++ {
					id := fmt.Sprintf("%08x-row-%d", i, table)
					text := "Public fixture. "
					for j := 0; j < 60; j++ {
						text += fmt.Sprintf(" document:%08x-row-3", (i+j)%(26*scale))
					}
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
				for i := 0; i < 43*scale; i++ {
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
			for i := 0; i < 12*scale; i++ {
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

			db := env.workspace.DB()
			var pages, size, accessBytes, edges int64
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			must(db.QueryRow("PRAGMA page_count").Scan(&pages))
			must(db.QueryRow("PRAGMA page_size").Scan(&size))
			must(db.QueryRow(`SELECT coalesce(sum(pgsize),0) FROM dbstat WHERE name IN (SELECT name FROM sqlite_master WHERE tbl_name LIKE 'resource_access_%')`).Scan(&accessBytes))
			must(db.QueryRow(`SELECT count(*) FROM resource_access_edges`).Scan(&edges))
			t.Logf("BASE scale=%d logical_bytes=%d access_bytes=%d edges=%d", scale, pages*size, accessBytes, edges)
			if os.Getenv("ANX_SCOPE_STORAGE_ONLY") == "1" {
				return
			}
			p, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "proposal.db"))
			must(err)
			defer p.Close()
			p.SetMaxOpenConns(1)
			var journal string
			must(db.QueryRow("PRAGMA journal_mode").Scan(&journal))
			_, err = p.Exec("PRAGMA journal_mode=" + journal)
			must(err)
			var sync int
			must(db.QueryRow("PRAGMA synchronous").Scan(&sync))
			_, err = p.Exec(fmt.Sprintf("PRAGMA synchronous=%d", sync))
			must(err)
			t.Logf("journal=%s synchronous=%d", journal, sync)
			_, err = p.Exec(`CREATE TABLE records(kind TEXT,id TEXT,scope INTEGER NOT NULL,seq INTEGER NOT NULL,title TEXT,body TEXT,PRIMARY KEY(kind,id));
 CREATE INDEX record_page ON records(scope,kind,seq DESC,id);
 CREATE TABLE feed(scope INTEGER,kind TEXT,seq INTEGER,id TEXT,title TEXT,PRIMARY KEY(scope,kind,seq,id)) WITHOUT ROWID;
 CREATE TABLE totals(scope INTEGER,kind TEXT,n INTEGER,PRIMARY KEY(scope,kind)) WITHOUT ROWID;`)
			must(err)
			ptx, err := p.Begin()
			must(err)
			// Same synthetic canonical payloads; uniform public rows plus private controls.
			for _, spec := range []struct{ kind, q string }{
				{"work", `SELECT id,title,summary FROM cards`}, {"inbox", `SELECT id,category,data_json FROM derived_inbox_items`},
				{"document", `SELECT id,title,search_text FROM documents`}, {"event", `SELECT id,type,payload_json FROM events`},
				{"artifact", `SELECT id,kind,metadata_json FROM artifacts`}, {"thread", `SELECT id,'',body_json FROM threads`},
				{"pm", `SELECT kind||':'||id,kind,body FROM pm_records`}, {"plan", `SELECT card_id,'',body_json FROM card_plans`},
			} {
				rows, e := db.Query(spec.q)
				must(e)
				i := 0
				for rows.Next() {
					var id, title, body string
					must(rows.Scan(&id, &title, &body))
					scope := 1
					if strings.Contains(title, "PrivatePerformanceSecret") || strings.Contains(body, `"pm_actor_id":"private-owner"`) {
						scope = 2
					}
					_, e = ptx.Exec(`INSERT INTO records VALUES(?,?,?,?,?,?)`, spec.kind, id, scope, i, title, body)
					must(e)
					if spec.kind == "work" || spec.kind == "inbox" {
						_, e = ptx.Exec(`INSERT INTO feed VALUES(?,?,?,?,?)`, scope, spec.kind, i, id, title)
						must(e)
					}
					i++
				}
				must(rows.Err())
				rows.Close()
			}
			_, err = ptx.Exec(`INSERT INTO totals SELECT scope,kind,count(*) FROM records GROUP BY scope,kind`)
			must(err)
			must(ptx.Commit())
			must(p.QueryRow("PRAGMA page_count").Scan(&pages))
			must(p.QueryRow("PRAGMA page_size").Scan(&size))
			var derived int64
			must(p.QueryRow(`SELECT sum(pgsize) FROM dbstat WHERE name IN ('feed','totals','record_page')`).Scan(&derived))
			t.Logf("PROPOSAL scale=%d logical_bytes=%d derived_and_scope_index_bytes=%d", scale, pages*size, derived)
			client := &http.Client{Timeout: 30 * time.Second}
			for _, path := range []string{"/inbox", "/overview", "/work?limit=20"} {
				var samples []time.Duration
				for i := 0; i < 27; i++ {
					start := time.Now()
					req, e := http.NewRequest("GET", env.server.URL+path, nil)
					must(e)
					req.Header.Set("Authorization", "Bearer "+stranger.AccessToken)
					resp, e := client.Do(req)
					must(e)
					b, e := io.ReadAll(resp.Body)
					resp.Body.Close()
					must(e)
					if resp.StatusCode != 200 || strings.Contains(string(b), "PrivatePerformanceSecret") {
						t.Fatalf("bad response %s %d %.200s", path, resp.StatusCode, b)
					}
					if i >= 2 {
						samples = append(samples, time.Since(start))
					}
				}
				t.Logf("HTTP scale=%d path=%s samples=25 p95=%s", scale, path, performanceP95(samples))
			}
			for _, kind := range []string{"inbox", "overview", "work"} {
				var samples []time.Duration
				for i := 0; i < 27; i++ {
					start := time.Now()
					tx, e := p.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
					must(e)
					query := `SELECT id,title FROM feed WHERE scope=? AND kind=? ORDER BY seq DESC,id DESC LIMIT 20`
					args := []any{1, kind}
					if kind == "overview" {
						args[1] = "work"
						rows, e := tx.Query(`SELECT kind,n FROM totals WHERE scope=?`, 1)
						must(e)
						var totals []any
						for rows.Next() {
							var k string
							var n int
							must(rows.Scan(&k, &n))
							totals = append(totals, []any{k, n})
						}
						must(rows.Err())
						rows.Close()
						_, e = json.Marshal(totals)
						must(e)
					}
					rows, e := tx.Query(query, args...)
					must(e)
					var result []any
					for rows.Next() {
						var id, title string
						must(rows.Scan(&id, &title))
						if title == "PrivatePerformanceSecret" {
							t.Fatal("private kernel row")
						}
						result = append(result, []string{id, title})
					}
					must(rows.Err())
					rows.Close()
					_, e = json.Marshal(result)
					must(e)
					must(tx.Commit())
					if i >= 2 {
						samples = append(samples, time.Since(start))
					}
				}
				t.Logf("KERNEL scale=%d kind=%s samples=25 p95=%s", scale, kind, performanceP95(samples))
				rows, e := p.Query(`EXPLAIN QUERY PLAN SELECT id,title FROM feed WHERE scope=1 AND kind=? ORDER BY seq DESC,id DESC LIMIT 20`, kind)
				must(e)
				for rows.Next() {
					var a, b, c int
					var detail string
					must(rows.Scan(&a, &b, &c, &detail))
					t.Logf("PLAN %s", detail)
					if strings.Contains(detail, "SCAN") || strings.Contains(detail, "TEMP B-TREE") {
						t.Fatal(detail)
					}
				}
				rows.Close()
			}
			for _, mode := range []string{"baseline", "proposal"} {
				var samples []time.Duration
				for i := 0; i < 27; i++ {
					id := fmt.Sprintf("write-%s-%d", mode, i)
					body := `{"text":"public write references document:00000000-row-3"}`
					start := time.Now()
					if mode == "baseline" {
						_, err = db.Exec(`INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) VALUES(?,'message_posted','2026-01-01T00:00:00Z','public-writer','[]',?)`, id, body)
						must(err)
					} else {
						tx, e := p.Begin()
						must(e)
						_, e = tx.Exec(`INSERT INTO records VALUES('event',?,1,?,'message_posted',?)`, id, i, body)
						must(e)
						_, e = tx.Exec(`INSERT INTO feed VALUES(1,'event',?,?,?)`, i, id, "message_posted")
						must(e)
						_, e = tx.Exec(`UPDATE totals SET n=n+1 WHERE scope=1 AND kind='event'`)
						must(e)
						must(tx.Commit())
					}
					if i >= 2 {
						samples = append(samples, time.Since(start))
					}
				}
				t.Logf("WRITE scale=%d mode=%s samples=25 p95=%s", scale, mode, performanceP95(samples))
			}
		})
	}
}
