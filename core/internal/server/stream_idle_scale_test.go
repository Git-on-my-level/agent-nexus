//go:build unix

package server

import (
	"context"
	"net/http/httptest"
	"os"
	"runtime/pprof"
	"sync"
	"syscall"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testsql"
	"agent-nexus-core/internal/testutil/perfguard"
)

// Opt-in, synthetic only. Profile starts after all five streams complete their
// initial sweep, so fixture construction and initial delivery are excluded.
func TestStreamIdleScaleProfile(t *testing.T) {
	t.Parallel()
	if os.Getenv("ANX_STREAM_PROFILE") == "" {
		t.Skip("set ANX_STREAM_PROFILE to a CPU profile path")
	}
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	if err := perfguard.Seed(context.Background(), env.workspace.DB(), env.workspace.Layout().ArtifactContentDir, "scale-owner", time.Now().UTC().Truncate(time.Hour)); err != nil {
		t.Fatal(err)
	}
	db, counter := testsql.Open("file:" + env.workspace.Layout().DatabasePath)
	defer db.Close()
	db.SetMaxOpenConns(8)
	store := primitives.NewTestStore(db, env.workspace.Layout().ArtifactContentDir)
	var configured *handlerOptions
	_ = NewHandler("profile", WithPrimitiveStore(store), WithStreamPollInterval(time.Second), func(o *handlerOptions) { configured = o })
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	defer func() { cancel(); wg.Wait() }()
	writers := make([]*idleProfileWriter, 5)
	for i := 0; i < 5; i++ {
		w := &idleProfileWriter{ready: make(chan struct{})}
		writers[i] = w
		r := httptest.NewRequest("GET", "/stream/inbox", nil).WithContext(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "scale-owner", PMActorID: "scale-owner"}))
		wg.Add(1)
		go func() { defer wg.Done(); handleInboxStream(w, r, *configured) }()
	}
	for _, w := range writers {
		select {
		case <-w.ready:
		case <-time.After(3 * time.Minute):
			t.Fatal("initial stream sweep timed out")
		}
	}
	f, err := os.Create(os.Getenv("ANX_STREAM_PROFILE"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	counter.Reset()
	var before, after syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &before)
	if err := pprof.StartCPUProfile(f); err != nil {
		t.Fatal(err)
	}
	defer pprof.StopCPUProfile()
	start := time.Now()
	var latencies []time.Duration
	for i := 0; i < 10; i++ {
		time.Sleep(time.Second)
		at := time.Now()
		_, _, err := loadInboxStreamPage(httptest.NewRequest("GET", "/stream/inbox", nil).WithContext(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "scale-owner", PMActorID: "scale-owner"})), handlerOptions{primitiveStore: store}, primitives.DerivedInboxListFilter{})
		if err != nil {
			t.Fatal(err)
		}
		latencies = append(latencies, time.Since(at))
	}
	pprof.StopCPUProfile()
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &after)
	cpu := float64(after.Utime.Sec-before.Utime.Sec+after.Stime.Sec-before.Stime.Sec) + float64(after.Utime.Usec-before.Utime.Usec+after.Stime.Usec-before.Stime.Usec)/1e6
	pages := 0
	for _, q := range counter.Statements() {
		if inboxSelector(q.SQL) {
			pages++
		}
	}
	if os.Getenv("ANX_STREAM_EXPECT_IDLE") == "1" && pages != len(latencies) {
		t.Fatalf("idle streams recomputed %d pages; want only %d latency probes", pages, len(latencies))
	}
	t.Logf("five streams: wall=%s CPU=%.3fs (%.2f%% of one core), inbox page recomputations=%d (includes 10 latency probes), read latencies=%v", time.Since(start), cpu, cpu/time.Since(start).Seconds()*100, pages, latencies)
}
