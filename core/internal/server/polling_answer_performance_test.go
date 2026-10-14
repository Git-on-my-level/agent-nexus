package server

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"runtime/pprof"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

// Exercise the route gate's synthetic canonical corpus on a single CPU. Setup
// is excluded from the optional CPU profile; no production data is accessed.
func TestPerformancePollingAnswersSingleCPU(t *testing.T) {
	requirePerformanceTest(t)
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	env := newPerformanceEnv(t)
	owner := env.principals[0]
	// Extend the handled-reader fixture's large event bodies with the route
	// corpus's thousands of cards, PM records, groups and ownership controls.
	payload, _ := json.Marshal(map[string]any{"payload": map[string]any{"body": strings.Repeat("synthetic ", 2048), "related_refs": []string{"card:scale-card-1"}}})
	if _, err := env.db.Exec(`WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i+1 FROM n WHERE i<4095)
     INSERT INTO events(id,handle,type,ts,actor_id,thread_id,refs_json,payload_json)
     SELECT 'poll-noise-'||i,'poll-noise-'||i,'message_posted','2026-01-01T00:00:00Z',?,'scale-thread-1','["card:scale-card-1"]',? FROM n`, owner.ActorID, string(payload)); err != nil {
		t.Fatal(err)
	}
	var pages, pageSize int64
	if err := env.db.QueryRow(`PRAGMA page_count`).Scan(&pages); err != nil {
		t.Fatal(err)
	}
	if err := env.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil {
		t.Fatal(err)
	}
	t.Logf("synthetic database bytes=%d cards=4096 events>8192 PM records>4096", pages*pageSize)
	asks := make([]primitives.DerivedInboxItem, 20)
	for i := range asks {
		event, err := env.store.AppendEvent(context.Background(), owner.ActorID, map[string]any{
			"type": "human_attention_requested", "thread_id": "scale-thread-1", "refs": []string{"card:scale-card-1"},
			"payload": map[string]any{"kind": "ask", "title": "Synthetic decision", "subject_ref": "card:scale-card-1", "requester_actor_id": env.agent.ActorID, "response_proposals": []any{"Proceed", "Wait"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		item, ok := deriveHumanAttentionInboxItem(event)
		if !ok {
			t.Fatal("invalid synthetic ask")
		}
		asks[i] = primitives.DerivedInboxItem{ID: item.ID, ThreadID: "scale-thread-1", Category: item.Category, SourceEventID: anyString(event["id"]), TriggerAt: item.TriggerAt.Format(time.RFC3339Nano), GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), Data: item.Data}
	}
	if err := env.store.ReplaceDerivedInboxItems(context.Background(), "scale-thread-1", asks); err != nil {
		t.Fatal(err)
	}
	if _, err := env.db.Exec(`INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at,metadata_json) VALUES('poll-topic-board','topic','scale-topic-1','board','scale-board-0','ref','now','{}')`); err != nil {
		t.Fatal(err)
	}
	if profile := os.Getenv("ANX_POLL_CPU_PROFILE"); profile != "" {
		f, err := os.Create(profile)
		if err != nil {
			t.Fatal(err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			f.Close()
			t.Fatal(err)
		}
		defer func() { pprof.StopCPUProfile(); f.Close() }()
	}
	server := httptest.NewServer(env.handler)
	defer server.Close()
	client := &http.Client{Timeout: 60 * time.Second}
	do := func(method, path string, body []byte) (int, []byte, error) {
		req, err := http.NewRequest(method, server.URL+path, bytes.NewReader(body))
		if err != nil {
			return 0, nil, err
		}
		req.Header.Set("Authorization", "Bearer "+owner.AccessToken)
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			return 0, nil, err
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		return resp.StatusCode, data, err
	}
	paths := []string{"/inbox?limit=50", "/home/unread", "/events?limit=50", "/work?limit=50", "/pm/actions?limit=50", "/pm/decisions?limit=50", "/topics/scale-topic-1/workspace", "/inbox?status=completed&limit=50"}
	for _, path := range paths {
		env.capture.Start()
		start := time.Now()
		status, data, err := do("GET", path, nil)
		statements, queries, rows := env.capture.Stop()
		t.Logf("read path=%s status=%d elapsed=%s queries=%d rows=%d", path, status, time.Since(start), queries, rows)
		if err != nil || status != 200 {
			t.Errorf("read %s: status=%d err=%v body=%.300s", path, status, err, data)
		}
		if time.Since(start) > 500*time.Millisecond {
			t.Errorf("read %s exceeds 500ms", path)
		}
		if strings.Contains(string(data), "PrivatePerformanceSecret") {
			t.Errorf("private control leaked from %s", path)
		}
		if queries > 40 {
			groups := map[string]int{}
			for _, stmt := range statements {
				groups[stmt.SQL[max(0, len(stmt.SQL)-180):]]++
			}
			for suffix, n := range groups {
				t.Logf("SQL count=%d suffix=%s", n, suffix)
			}
			t.Errorf("read %s queries=%d exceeds 40", path, queries)
		}
	}
	// Exercise both the inventory report and a live activity panel. A 200
	// response containing an unavailable panel is not a successful read.
	for _, panelType := range []string{"live-fleet-health", "live-activity"} {
		report := map[string]any{"report": map[string]any{"kind": "anx.visual-report", "schema_version": 1, "title": "Synthetic report", "summary": "Synthetic evidence", "generated_at": "2026-01-01T00:00:00Z", "sources": []any{}, "projects": []any{map[string]any{"id": "workspace", "title": "Workspace", "summary": "Synthetic", "outcome": "Evidence"}}, "panels": []any{map[string]any{"id": "activity", "type": panelType, "title": "Activity", "project_id": "workspace", "data": map[string]any{}, "author": "ANX", "provenance": "reported", "observed_at": nil, "freshness": "unavailable", "source_ids": []any{}}}}}
		body, _ := json.Marshal(report)
		env.capture.Start()
		start := time.Now()
		status, data, err := do("POST", "/reports/preview", body)
		_, queries, rows := env.capture.Stop()
		elapsed := time.Since(start)
		t.Logf("report panel=%s status=%d elapsed=%s queries=%d rows=%d", panelType, status, elapsed, queries, rows)
		if err != nil || status != 200 || strings.Contains(string(data), `"status":"unavailable"`) {
			t.Errorf("report %s status=%d err=%v body=%.300s", panelType, status, err, data)
		}
		if queries > 100 || elapsed > time.Second {
			t.Errorf("report %s exceeded budget queries=%d elapsed=%s", panelType, queries, elapsed)
		}
	}
	reading := make(chan struct{})
	var firstRead sync.Once
	env.capture.BeforeBegin(func(o driver.TxOptions) {
		if o.ReadOnly {
			firstRead.Do(func() { close(reading) })
		}
	})
	var workers sync.WaitGroup
	failures := make(chan error, 3)
	started := make(chan struct{}, 3)
	stop := make(chan struct{})
	for _, path := range paths[:3] {
		workers.Add(1)
		go func(path string) {
			defer workers.Done()
			started <- struct{}{}
			for {
				select {
				case <-stop:
					return
				default:
				}
				status, _, err := do("GET", path, nil)
				if err != nil || status != 200 {
					failures <- fmt.Errorf("poll %s status=%d err=%v", path, status, err)
					return
				}
			}
		}(path)
	}
	for i := 0; i < 3; i++ {
		<-started
	}
	select {
	case <-reading:
	case <-time.After(5 * time.Second):
		t.Error("polling readers did not reach a read snapshot")
	}
	samples := []time.Duration{}
	replays := []time.Duration{}
	for i, ask := range asks {
		body, _ := json.Marshal(map[string]any{"idempotency_key": fmt.Sprintf("single-cpu-%d", i), "response_text": "Proceed", "outcome": "answered", "notify_mode": "none"})
		start := time.Now()
		status, data, err := do("POST", "/inbox/"+ask.ID+"/respond", body)
		samples = append(samples, time.Since(start))
		t.Logf("answer sample=%d status=%d elapsed=%s", i, status, samples[len(samples)-1])
		if err != nil || status != 201 {
			t.Errorf("answer %d status=%d err=%v body=%.300s", i, status, err, data)
			break
		}
		start = time.Now()
		replayStatus, replay, replayErr := do("POST", "/inbox/"+ask.ID+"/respond", body)
		replays = append(replays, time.Since(start))
		t.Logf("replay sample=%d elapsed=%s", i, replays[len(replays)-1])
		if replayErr != nil || replayStatus != 201 || strings.TrimSpace(string(replay)) != strings.TrimSpace(string(data)) {
			t.Errorf("replay %d status=%d err=%v", i, replayStatus, replayErr)
			break
		}
		if samples[len(samples)-1] > 5*time.Second {
			t.Errorf("catastrophic answer delay exceeds 5s")
			break
		}
	}
	close(stop)
	workers.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	env.capture.BeforeBegin(nil)
	sort.Slice(replays, func(i, j int) bool { return replays[i] < replays[j] })
	if len(replays) > 0 {
		p95 := replays[(len(replays)*95+99)/100-1]
		t.Logf("replay count=%d p95=%s max=%s", len(replays), p95, replays[len(replays)-1])
		if p95 >= time.Second {
			t.Errorf("replay p95=%s exceeds 1s", p95)
		}
	}
	var claims int
	if err := env.db.QueryRow(`SELECT count(*) FROM human_attention_response_claims WHERE request_key LIKE 'single-cpu-%'`).Scan(&claims); err != nil || claims != len(samples) {
		t.Errorf("claims=%d samples=%d err=%v", claims, len(samples), err)
	}
	if len(samples) != 20 {
		t.Errorf("only %d/20 answers completed", len(samples))
	}
	if len(samples) > 0 {
		p95 := samples[(len(samples)*95+99)/100-1]
		t.Logf("single CPU answers count=%d p95=%s max=%s", len(samples), p95, samples[len(samples)-1])
		if p95 >= time.Second {
			t.Errorf("answer p95=%s exceeds 1s", p95)
		}
	}
}
