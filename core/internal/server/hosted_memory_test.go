package server

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/commandcenter"
	"agent-nexus-core/internal/heartbeat"
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/runtimememory"
	"agent-nexus-core/internal/schema"
	"agent-nexus-core/internal/series"
	"agent-nexus-core/internal/storage"
)

// Synthetic, opt-in load companion to TestPerformanceResidentMemory. Uses real
// HTTP SSE transport, maintenance implementations, signed heartbeat publisher,
// durable wake lifecycle and PM turns. No external service or model is called.
func startHostedMemoryMix(t *testing.T, w *storage.Workspace, ps *primitives.Store, contract *schema.Contract) (http.Handler, func()) {
	t.Helper()
	db := w.DB()
	as := auth.NewStore(db)
	seedMachinePrincipalForLockoutTest(t, context.Background(), db, "memory-pm", "memory-pm-actor", "memory.pm", "memory-pm-token")
	rt, err := newOnboardedPMRuntime(t, db, ps, as, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main", AgentActorID: "memory-pm-actor"}})
	if err != nil {
		t.Fatal(err)
	}
	maintainer := NewProjectionMaintainer(ProjectionMaintainerConfig{PrimitiveStore: ps, Contract: contract, Mode: ProjectionModeBackground, PollInterval: 5 * time.Second, DirtyBatchSize: 50, SystemActorID: actors.SystemActorID})
	h := NewHandler("hosted-memory", WithPrimitiveStore(ps), WithAuthStore(as), WithActorRegistry(actors.NewStore(db)), WithSchemaContract(contract), WithHealthCheck(w.Ping), WithPMRuntime(rt), WithProjectionMaintainer(maintainer), WithSeriesStore(&series.Store{DB: db, Auth: as}), WithRunStore(commandcenter.NewStore(db, commandcenter.SQLIdentities{DB: db})))
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	launch := func(fn func(context.Context)) { wg.Add(1); go func() { defer wg.Done(); fn(ctx) }() }
	owner := pm.Principal{WorkspaceID: "ws_main", ActorID: "memory-owner-actor", Human: true}
	agent := pm.Principal{WorkspaceID: "ws_main", ActorID: "memory-pm-actor"}
	conversation, err := rt.Service.CreateConversation(ctx, owner, pm.CreateConversation{RequestKey: "memory-mix", Title: "Synthetic PM memory load", WorkRef: "card:scale-card-1"})
	if err != nil {
		cancel()
		wg.Wait()
		t.Fatal(err)
	}
	launch(maintainer.Run)
	var retries atomic.Int64
	retry := func(err error) {
		if ctx.Err() == nil {
			retries.Add(1)
			t.Logf("bounded maintenance retry: %v", err)
		}
	}
	launch(func(c context.Context) { ps.RunAskLifecycle(c, retry) })
	launch(func(c context.Context) { w.RunAskSubjectsMaintenance(c, retry) })
	launch(func(c context.Context) { w.RunInboxLifecycleMaintenance(c, retry) })
	launch(func(c context.Context) {
		m := NewAnswerWakeMaintainer(AnswerWakeMaintainerConfig{PrimitiveStore: ps, WorkspaceID: "ws_main", WorkspaceName: "synthetic", QuietWindow: DefaultAnswerWakeQuietWindow, FlushWhenNoOpenAsks: true})
		m.Run(c)
	})
	var beats, wakes, turns, streams atomic.Int64
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		beats.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publisher := &heartbeat.Publisher{URL: collector.URL, WorkspaceID: "ws_main", Identity: heartbeat.Identity{ID: "synthetic-core", PrivateKey: key}, Interval: 30 * time.Second, Snapshot: func(c context.Context) heartbeat.Snapshot {
		usage, err := ps.GetWorkspaceUsageSummary(c)
		if err != nil && c.Err() == nil {
			t.Errorf("heartbeat usage: %v", err)
		}
		return heartbeat.Snapshot{Version: "memory-probe", HealthSummary: map[string]any{"ok": w.Ping(c) == nil}, UsageSummary: memorySnapshotMap(usage), ProjectionMaintenanceSummary: memorySnapshotMap(maintainer.Snapshot(c, time.Now().UTC()))}
	}}
	launch(publisher.Run)
	launch(func(c context.Context) {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for i := 0; ; i++ {
			select {
			case <-c.Done():
				return
			case <-ticker.C:
			}
			err := func() error {
				event, err := ps.AppendEvent(c, "memory-owner-actor", map[string]any{"type": "message_posted", "thread_id": "scale-thread-1", "refs": []string{"card:scale-card-1"}, "payload": map[string]any{"text": "synthetic agent heartbeat"}})
				if err != nil {
					return err
				}
				id := fmt.Sprintf("memory-wake-%d", i)
				_, err = ps.UpsertAgentWakeup(c, primitives.AgentWakeup{WakeupID: id, ThreadID: "scale-thread-1", TargetActorID: agent.ActorID, TargetHandle: "memory.pm", Status: primitives.AgentWakeupStatusRequested, TriggerEventID: anyString(event["id"]), TriggerText: "synthetic wake", Refs: []string{"card:scale-card-1"}})
				if err != nil {
					return err
				}
				if _, err = ps.ClaimAgentWakeup(c, id, agent.ActorID, "synthetic-bridge"); err != nil {
					return err
				}
				if _, err = ps.CompleteAgentWakeup(c, id, agent.ActorID, "synthetic-bridge"); err != nil {
					return err
				}
				wakes.Add(1)
				if _, err = rt.Service.PostMessage(c, owner, conversation.ID, pm.MessageInput{RequestKey: fmt.Sprint(i), Text: "synthetic PM turn"}); err != nil {
					return err
				}
				claimed, err := rt.Service.ClaimTurn(c, agent, pm.ClaimInput{RunnerID: "synthetic-bridge"})
				if err != nil {
					return err
				}
				if _, err = rt.Service.HeartbeatTurn(c, agent, claimed.ID, pm.HeartbeatInput{LeaseToken: claimed.LeaseToken}); err != nil {
					return err
				}
				if _, err = rt.Service.CompleteTurnWithLease(c, agent, claimed.ID, "synthetic answer", nil, claimed.LeaseToken); err != nil {
					return err
				}
				turns.Add(1)
				return rt.Tick(c)
			}()
			if err != nil && c.Err() == nil {
				t.Errorf("agent/PM cycle: %v", err)
				return
			}
		}
	})
	ts := httptest.NewServer(h)
	ready := make(chan struct{}, 5)
	for _, path := range []string{"/stream/events", "/stream/events", "/stream/inbox", "/stream/inbox", "/stream/agent-notification-receipts?thread_id=scale-thread-1"} {
		path := path
		launch(func(c context.Context) {
			r, err := http.NewRequestWithContext(c, "GET", ts.URL+path, nil)
			if err != nil {
				t.Error(err)
				ready <- struct{}{}
				return
			}
			r.Header.Set("Authorization", "Bearer memory-owner-token")
			response, err := http.DefaultClient.Do(r)
			ready <- struct{}{}
			if err != nil {
				if c.Err() == nil {
					t.Errorf("SSE %s: %v", path, err)
				}
				return
			}
			defer response.Body.Close()
			if response.StatusCode != 200 {
				t.Errorf("SSE %s status=%d", path, response.StatusCode)
				return
			}
			streams.Add(1)
			_, _ = io.Copy(io.Discard, response.Body)
		})
	}
	for i := 0; i < 5; i++ {
		select {
		case <-ready:
		case <-time.After(time.Minute):
			cancel()
			wg.Wait()
			ts.Close()
			collector.Close()
			t.Fatal("stream admission timed out")
		}
	}
	return h, func() {
		cancel()
		wg.Wait()
		ts.Close()
		collector.Close()
		t.Logf("hosted mix: streams=%d signed_heartbeats=%d completed_wakes=%d completed_PM_turns=%d maintenance_retries=%d", streams.Load(), beats.Load(), wakes.Load(), turns.Load(), retries.Load())
		if streams.Load() != 5 || beats.Load() == 0 || wakes.Load() == 0 || turns.Load() == 0 {
			t.Error("hosted mix did not exercise every component")
		}
	}
}

func startMemoryPeakSampler(t *testing.T) func() {
	t.Helper()
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		var peakRSS, peakHeap uint64
		var atRSS, atHeap runtime.MemStats
		var profileHeap uint64
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				t.Logf("peak rss_bytes=%d at_rss_heap_inuse=%d at_rss_heap_released=%d at_rss_heap_alloc=%d peak_heap_inuse=%d at_heap_heap_released=%d", peakRSS, atRSS.HeapInuse, atRSS.HeapReleased, atRSS.HeapAlloc, peakHeap, atHeap.HeapReleased)
				return
			case <-ticker.C:
			}
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			if raw, err := os.ReadFile("/proc/self/status"); err == nil {
				for _, line := range strings.Split(string(raw), "\n") {
					if strings.HasPrefix(line, "VmRSS:") {
						f := strings.Fields(line)
						n, _ := strconv.ParseUint(f[1], 10, 64)
						if n*1024 > peakRSS {
							peakRSS = n * 1024
							atRSS = m
						}
					}
				}
			}
			if m.HeapInuse > peakHeap {
				peakHeap = m.HeapInuse
				atHeap = m
			}
			if dir := os.Getenv("ANX_MEMORY_PROFILE_DIR"); dir != "" && m.HeapInuse > profileHeap+16<<20 {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Error(err)
					continue
				}
				if f, err := os.Create(filepath.Join(dir, "peak-heap.pprof")); err == nil {
					_ = pprof.WriteHeapProfile(f)
					_ = f.Close()
					profileHeap = m.HeapInuse
				}
			}
		}
	}()
	return func() { close(stop); <-done }
}

func applyProbeCgroupLimit() {
	if os.Getenv("ANX_MEMORY_APPLY_LIMIT") == "1" {
		runtimememory.Configure(os.Stdout)
	}
}

func memorySnapshotMap(value any) map[string]any {
	raw, _ := json.Marshal(value)
	var result map[string]any
	_ = json.Unmarshal(raw, &result)
	return result
}
