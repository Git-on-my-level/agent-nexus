package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"modernc.org/libc"
	sqlite3 "modernc.org/sqlite/lib"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
	"agent-nexus-core/internal/storage"
	"agent-nexus-core/internal/testutil/perfguard"
)

// This opt-in probe opens only an explicitly supplied disposable LOCAL copy.
// Never point it at a serving workspace: setup and Overview visits write to it.
func TestPerformanceResidentMemory(t *testing.T) {
	requirePerformanceTest(t)
	root := os.Getenv("ANX_MEMORY_WORKSPACE")
	if root == "" {
		t.Skip("requires ANX_MEMORY_WORKSPACE: disposable local workspace copy")
	}
	applyProbeCgroupLimit()
	stopSampler := startMemoryPeakSampler(t)
	defer stopSampler()
	tls := libc.NewTLS()
	defer tls.Close()
	args := tls.Alloc(8)
	nativeCounters := os.Getenv("ANX_MEMORY_HOSTED_MIX") != "1"
	if nativeCounters {
		if rc := sqlite3.Xsqlite3_config(tls, sqlite3.SQLITE_CONFIG_MEMSTATUS, libc.VaList(args, int32(1))); rc != sqlite3.SQLITE_OK {
			t.Fatalf("enable SQLite memory counters before initialization: %d", rc)
		}
	}
	ctx := context.Background()
	w, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	db := w.DB()
	ps := primitives.NewStore(db, blob.NewFilesystemBackend(w.Layout().ArtifactContentDir), w.Layout().ArtifactContentDir)
	if os.Getenv("ANX_MEMORY_WARM_MAINTENANCE") == "1" {
		warmCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		for {
			done, err := w.MaintainAskSubjectsBatch(warmCtx)
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) && warmCtx.Err() == nil {
					continue
				}
				t.Fatal(err)
			}
			if done {
				break
			}
		}
		for {
			done, err := w.MaintainInboxLifecycleBatch(warmCtx, 200)
			if err != nil {
				if errors.Is(err, context.DeadlineExceeded) && warmCtx.Err() == nil {
					continue
				}
				t.Fatal(err)
			}
			if done {
				break
			}
		}
		t.Log("fixture background backfills completed before measured activity")
		return
	}
	if os.Getenv("ANX_MEMORY_PUBLIC_HISTORY") == "1" {
		if _, err := db.ExecContext(ctx, `UPDATE threads SET body_json=json_remove(body_json,'$.pm_actor_id') WHERE json_extract(body_json,'$.title')='hidden subject'`); err != nil {
			t.Fatal(err)
		}
		t.Log("prepared public archived synthetic history")
		return
	}
	if os.Getenv("ANX_MEMORY_PRIVATE_HISTORY") == "1" {
		if _, err := db.ExecContext(ctx, `UPDATE threads SET body_json=json_set(body_json,'$.pm_actor_id','memory-private-owner') WHERE json_extract(body_json,'$.title')='hidden subject'`); err != nil {
			t.Fatal(err)
		}
		t.Log("prepared inherited-private synthetic history")
		return
	}
	if os.Getenv("ANX_MEMORY_SEED") == "1" {
		seedHumanPrincipalForLockoutTest(t, ctx, db, "memory-owner", "memory-owner-actor", "memory.owner", "memory-owner-token")
		seedMachinePrincipalForLockoutTest(t, ctx, db, "memory-stranger", "memory-stranger-actor", "memory.stranger", "memory-stranger-token")
		if err := perfguard.Seed(ctx, db, w.Layout().ArtifactContentDir, "memory-owner-actor", time.Now().UTC().Truncate(time.Hour)); err != nil {
			t.Fatal(err)
		}
		if os.Getenv("ANX_MEMORY_HOSTED_SEED") == "1" {
			if _, err := db.ExecContext(ctx, `DELETE FROM pm_records WHERE kind='turn' AND id LIKE 'scale-pm-turn-%'`); err != nil {
				t.Fatal(err)
			}
			t.Log("removed read-only synthetic PM turn placeholders; live turns will use PM service")
		}
		t.Log("prepared synthetic fixture; rerun without ANX_MEMORY_SEED on identical copies")
		return
	}
	contract, err := schema.Load("../../../contracts/anx-schema.yaml")
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler("memory-probe", WithPrimitiveStore(ps), WithAuthStore(auth.NewStore(db)), WithActorRegistry(actors.NewStore(db)), WithSchemaContract(contract), WithHealthCheck(w.Ping))
	if os.Getenv("ANX_MEMORY_HOSTED_MIX") == "1" {
		var closeMix func()
		h, closeMix = startHostedMemoryMix(t, w, ps, contract)
		defer closeMix()
	}
	paths := []string{"/overview", "/cards/scale-card-1", "/inbox?limit=50"}
	var mu sync.Mutex
	latencies := map[string][]time.Duration{}
	request := func(path string, stranger bool) {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		token := "memory-owner-token"
		if stranger {
			token = "memory-stranger-token"
		}
		req.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		start := time.Now()
		h.ServeHTTP(response, req)
		elapsed := time.Since(start)
		if response.Code != http.StatusOK {
			t.Errorf("path=%s status=%d", path, response.Code)
		}
		if strings.Contains(response.Body.String(), `"HIDDEN"`) {
			t.Error("private fixture disclosed")
		}
		mu.Lock()
		latencies[path] = append(latencies[path], elapsed)
		mu.Unlock()
	}
	for _, path := range paths {
		request(path, false)
		request(path, true)
	}
	logMemory := func(label string) {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		rss, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s rss_kib=%s heap_alloc=%d heap_inuse=%d heap_sys=%d heap_released=%d stack_inuse=%d gc_target=%d total_alloc=%d num_gc=%d pool=%+v", label, strings.TrimSpace(string(rss)), m.HeapAlloc, m.HeapInuse, m.HeapSys, m.HeapReleased, m.StackInuse, m.NextGC, m.TotalAlloc, m.NumGC, db.Stats())
		if nativeCounters {
			t.Logf("%s sqlite_native_used=%d sqlite_native_highwater=%d", label, sqlite3.Xsqlite3_memory_used(tls), sqlite3.Xsqlite3_memory_highwater(tls, 0))
		}
		conn, err := db.Conn(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		for _, pragma := range []string{"cache_size", "mmap_size", "page_size", "page_count", "temp_store"} {
			var value int64
			if err := conn.QueryRowContext(ctx, "PRAGMA "+pragma).Scan(&value); err != nil {
				t.Fatal(err)
			}
			t.Logf("%s sqlite_%s=%d", label, pragma, value)
		}
	}
	logMemory("warm")
	workers := 4
	if value := os.Getenv("ANX_MEMORY_WORKERS"); value != "" {
		var err error
		workers, err = strconv.Atoi(value)
		if err != nil || workers < 1 || workers > 32 {
			t.Fatal("ANX_MEMORY_WORKERS must be 1..32")
		}
	}
	for wave := 0; wave < 3; wave++ {
		var wg sync.WaitGroup
		for worker := 0; worker < workers; worker++ {
			wg.Add(1)
			go func(worker int) {
				defer wg.Done()
				for _, path := range paths {
					request(path, worker%2 == 1)
				}
			}(worker)
		}
		wg.Wait()
		logMemory(fmt.Sprintf("wave%d", wave))
	}
	for _, path := range paths {
		samples := latencies[path]
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		t.Logf("path=%s n=%d median=%s p95=%s", path, len(samples), samples[len(samples)/2], samples[(len(samples)*95-1)/100])
	}
	if dir := os.Getenv("ANX_MEMORY_PROFILE_DIR"); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"heap", "allocs"} {
			f, err := os.Create(filepath.Join(dir, name+".pprof"))
			if err != nil {
				t.Fatal(err)
			}
			if err := pprof.Lookup(name).WriteTo(f, 0); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
	if os.Getenv("ANX_MEMORY_HOSTED_MIX") == "1" {
		time.Sleep(2 * time.Second)
		logMemory("retained_without_forced_gc")
	}
	runtime.GC()
	logMemory("after_gc")
	// Keep explicit positive/negative privacy controls outside the timed samples.
	for _, control := range []struct {
		token  string
		status int
	}{
		{"memory-owner-token", http.StatusOK}, {"memory-stranger-token", http.StatusNotFound},
	} {
		req := httptest.NewRequest(http.MethodGet, "/cards/scale-card-0", nil)
		req.Header.Set("Authorization", "Bearer "+control.token)
		response := httptest.NewRecorder()
		h.ServeHTTP(response, req)
		if response.Code != control.status {
			t.Fatalf("private card control status=%d want=%d", response.Code, control.status)
		}
	}
}
