package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
	"agent-nexus-core/internal/testutil/perfguard"
)

// Explicit local root only. Seeding and maintenance are outside measurement.
func TestBriefLoadProfile(t *testing.T) {
	root := os.Getenv("ANX_BRIEF_LOAD_ROOT")
	if root == "" {
		t.Skip("requires disposable synthetic ANX_BRIEF_LOAD_ROOT")
	}
	ctx := context.Background()
	w, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	s := primitives.NewTestStore(w.DB(), w.Layout().ArtifactContentDir)
	const owner = "brief-load-owner"
	if os.Getenv("ANX_BRIEF_LOAD_SEED") == "1" {
		var seeded bool
		if err := w.DB().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM cards WHERE id='scale-card-0')`).Scan(&seeded); err != nil {
			t.Fatal(err)
		}
		if !seeded {
			seedHumanPrincipalForLockoutTest(t, ctx, w.DB(), "brief-load-human", owner, "brief.load", "brief-load-token")
			if err := perfguard.Seed(ctx, w.DB(), w.Layout().ArtifactContentDir, owner, time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
		}
		tx, err := w.DB().BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		now := time.Now().UTC()
		for i := 0; i < 48; i++ {
			id := fmt.Sprintf("scale-card-%d", i+1)
			if _, err := tx.ExecContext(ctx, `INSERT INTO work_metadata(card_id,metadata_json,updated_at,updated_by) VALUES(?,?,?,?) ON CONFLICT(card_id) DO UPDATE SET metadata_json=excluded.metadata_json,updated_at=excluded.updated_at`, id, `{"cadence":{"every_seconds":3600},"goal":{"metric":"score","direction":"up"}}`, now.Format(time.RFC3339Nano), owner); err != nil {
				t.Fatal(err)
			}
			for j := 0; j < 200; j++ {
				body, _ := json.Marshal(map[string]any{"subject_ref": "card:" + id, "summary": "Synthetic receipt", "payload": map[string]any{"run_id": fmt.Sprintf("%d-%d", i, j), "outcome": "measured", "metrics": map[string]any{"score": j}}})
				if _, err := tx.ExecContext(ctx, `INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES(?,'run_recorded',?,?,?,json_array(?),?)`, fmt.Sprintf("brief-load-run-%d-%d", i, j), now.Add(-time.Duration(200-j)*time.Second).Format(time.RFC3339Nano), owner, fmt.Sprintf("scale-thread-%d", i+1), "card:"+id, string(body)); err != nil {
					t.Fatal(err)
				}
			}
		}
		for i := 0; i < 600; i++ {
			body, _ := json.Marshal(map[string]any{"payload": map[string]any{"kind": "ask", "title": fmt.Sprintf("Synthetic ask %d", i), "requester_actor_id": "scale-principal-1", "subject_ref": fmt.Sprintf("card:scale-card-%d", i%48+1)}})
			if _, err := tx.ExecContext(ctx, `INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES(?,'human_attention_requested',?,'scale-principal-1',?,json_array(?),?)`, fmt.Sprintf("brief-load-ask-%d", i), now.Add(-time.Duration(i)*time.Second).Format(time.RFC3339Nano), fmt.Sprintf("scale-thread-%d", i%48+1), fmt.Sprintf("card:scale-card-%d", i%48+1), string(body)); err != nil {
				t.Fatal(err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		for done := false; !done; {
			done, err = s.RefreshBriefInitiatives(ctx, 200)
			if err != nil {
				t.Fatal(err)
			}
		}
		t.Log("seeded 4096 cards, 14296 events, 48 loops / 9600 receipts, 600 asks")
		return
	}
	pool, capture, err := perfguard.Open("file:" + w.Layout().DatabasePath + "?_pragma=busy_timeout(20000)")
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	measured := primitives.NewTestStore(pool, w.Layout().ArtifactContentDir)
	h := NewHandler("brief-load", WithPrimitiveStore(measured), WithAuthStore(auth.NewStore(pool)), WithActorRegistry(actors.NewStore(pool)))
	if dir := os.Getenv("ANX_BRIEF_LOAD_PROFILES"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		var anchor string
		if err := w.DB().QueryRow(`SELECT MAX(ts) FROM events`).Scan(&anchor); err != nil {
			t.Fatal(err)
		}
		now, err := time.Parse(time.RFC3339Nano, anchor)
		if err != nil {
			t.Fatal(err)
		}
		read, closeRead, err := measured.BeginResourceRead(primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: owner}))
		if err != nil {
			t.Fatal(err)
		}
		golden, err := measured.Brief(read, primitives.BriefReader{ActorID: owner, Kind: "human", Visible: func(_, _ string) bool { return true }}, now.Add(time.Minute))
		closeRead()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.MarshalIndent(golden, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "brief.golden.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	request := func() {
		r := httptest.NewRequest("GET", "/brief", nil)
		r.Header.Set("Authorization", "Bearer brief-load-token")
		response := httptest.NewRecorder()
		h.ServeHTTP(response, r)
		if response.Code != 200 {
			t.Fatalf("brief status %d: %s", response.Code, response.Body.String())
		}
	}
	request()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	var samples []float64
	var queries int
	for i := 0; i < 40; i++ {
		capture.Start()
		start := time.Now()
		request()
		samples = append(samples, float64(time.Since(start))/float64(time.Millisecond))
		_, queries, _ = capture.Stop()
		if capture.WorkError() != nil {
			t.Fatal(capture.WorkError())
		}
	}
	runtime.ReadMemStats(&after)
	sort.Float64s(samples)
	t.Logf("BRIEF p50_ms=%.3f p95_ms=%.3f SQL=%d bytes_per_read=%d allocs_per_read=%d", samples[19], samples[37], queries, (after.TotalAlloc-before.TotalAlloc)/40, (after.Mallocs-before.Mallocs)/40)
	logMemory := func(label string) {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		rss, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("MEMORY %s rss_kib=%s heap_alloc=%d heap_inuse=%d total_alloc=%d", label, strings.TrimSpace(string(rss)), m.HeapAlloc, m.HeapInuse, m.TotalAlloc)
	}
	logMemory("warm")
	for wave := 0; wave < 6; wave++ {
		for i := 0; i < 10; i++ {
			_, err := s.AppendEvent(ctx, owner, map[string]any{"type": "run_recorded", "summary": "Load receipt", "refs": []string{"card:scale-card-1"}, "payload": map[string]any{"run_id": fmt.Sprintf("load-%d-%d-%d", time.Now().UnixNano(), wave, i), "outcome": "measured", "metrics": map[string]any{"score": i}}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.RefreshBriefInitiatives(ctx, 200); err != nil {
				t.Fatal(err)
			}
			request()
			time.Sleep(time.Second)
		}
		logMemory(fmt.Sprintf("wave%d", wave))
	}
	if dir := os.Getenv("ANX_BRIEF_LOAD_PROFILES"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"heap", "allocs"} {
			f, err := os.Create(filepath.Join(dir, name+".pprof"))
			if err != nil {
				t.Fatal(err)
			}
			if err = pprof.Lookup(name).WriteTo(f, 0); err != nil {
				t.Fatal(err)
			}
			if err = f.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
	runtime.GC()
	logMemory("after_gc")
	for _, pragma := range []string{"cache_size", "mmap_size"} {
		var n int64
		if err := w.DB().QueryRow("PRAGMA " + pragma).Scan(&n); err != nil {
			t.Fatal(err)
		}
		t.Logf("sqlite_%s=%d", pragma, n)
	}
}
