package server

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

func TestEventsStreamBoundedResumeFramesAndHiddenPositions(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	reader := seedHumanPrincipalForLockoutTest(t, ctx, db, "page-reader", "page-reader-actor", "page-reader", "page-reader-token")
	if _, err := db.Exec(`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('hidden-page','now','owner','{"pm_actor_id":"owner"}')`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= 401; i++ {
		var thread any
		var trashed any
		if i > 0 && i <= 200 {
			thread = "hidden-page"
		}
		if i == 201 {
			trashed = "2026-01-02T00:00:00Z"
		}
		if _, err := db.Exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json,trashed_at) VALUES(?,'message_posted','2026-01-01T00:00:00Z','owner',?,'[]','{}',?)`, fmt.Sprintf("%04d", i), thread, trashed); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		name, query, header string
	}{
		{"header-precedence", "0401", "0000"},
		{"query-resume", "0000", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			streamCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(streamCtx, "GET", env.server.URL+"/stream/events?last_event_id="+test.query, nil)
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+reader.AccessToken)
			req.Header.Set("Last-Event-ID", test.header)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != 200 {
				resp.Body.Close()
				t.Fatalf("status=%d", resp.StatusCode)
			}
			frames, stop := startSSEReader(resp.Body)
			defer stop()
			seen := map[string]bool{}
			controls := 0
			for len(seen) < 200 || controls < 1 {
				frame := awaitSSEEvent(t, frames, 5*time.Second)
				if frame.Event == "resume" {
					controls++
					if len(seen) != 200 || len(frame.Data) != 0 || frame.ID != "0401" {
						t.Fatalf("hidden position in control: %+v", frame)
					}
					continue
				}
				if frame.Event != "event" || frame.ID < "0202" || frame.ID > "0401" || seen[frame.ID] {
					t.Fatalf("leak/duplicate: %+v", frame)
				}
				seen[frame.ID] = true
			}
			if controls != 1 {
				t.Fatalf("controls=%d want=1", controls)
			}
			select {
			case frame := <-frames:
				t.Fatalf("unexpected extra frame: %+v", frame)
			case <-time.After(100 * time.Millisecond):
			}
		})
	}
}

func eventPrivacyHTTPServer(t *testing.T, env authIntegrationEnv, interval time.Duration) *httptest.Server {
	t.Helper()
	contract, err := schema.Load("../../../contracts/anx-schema.yaml")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewHandler("test", WithActorRegistry(env.registry), WithAuthStore(env.authStore), WithPrimitiveStore(env.primitiveStore), WithSchemaContract(contract), WithWorkspaceID("ws_main"), WithStreamPollInterval(interval)))
	t.Cleanup(srv.Close)
	return srv
}

func readQuietEventStream(t *testing.T, url, token, id string, header bool) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if !header {
		url += "?last_event_id=" + id
	}
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if header {
		req.Header.Set("Last-Event-ID", id)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("unexpected response: %d %v", resp.StatusCode, resp.Header)
	}
	reader := bufio.NewReader(resp.Body)
	var wire strings.Builder
	for i := 0; i < 3; i++ {
		frame := round4SSEFrame(t, reader)
		if frame != ": keepalive\n\n" {
			t.Fatalf("quiet stream disclosed activity: %q", frame)
		}
		wire.WriteString(frame)
	}
	return wire.String()
}

func TestEventsStreamHiddenAndUnknownResumeIDsAreIdenticalOverHTTP(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	reader := seedHumanPrincipalForLockoutTest(t, context.Background(), env.workspace.DB(), "probe-reader", "probe-reader-actor", "probe-reader", "probe-reader-token")
	for _, query := range []string{
		`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('private','now','owner','{"pm_actor_id":"owner"}')`,
		`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json,trashed_at) VALUES
		('private-probe','message_posted','2026-01-01T00:00:00Z','owner','private','[]','{}',NULL),
		('trashed-probe','message_posted','2026-01-01T00:00:01Z','owner',NULL,'[]','{}','now'),
		('public-later','message_posted','2026-01-01T00:00:02Z','owner',NULL,'[]','{}',NULL)`,
	} {
		if _, err := env.workspace.DB().Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	srv := eventPrivacyHTTPServer(t, env, 20*time.Millisecond)
	for _, header := range []bool{true, false} {
		want := readQuietEventStream(t, srv.URL+"/stream/events", reader.AccessToken, "unknown-probe", header)
		for _, id := range []string{"private-probe", "trashed-probe"} {
			if got := readQuietEventStream(t, srv.URL+"/stream/events", reader.AccessToken, id, header); got != want {
				t.Fatalf("%s differs from unknown: %q %q", id, got, want)
			}
		}
	}
}

func TestEventsStreamHiddenOnlyHistoryMatchesIdleOverHTTP(t *testing.T) {
	for _, hidden := range []int{0, 401, 10000} {
		t.Run(fmt.Sprint(hidden), func(t *testing.T) {
			env := newAuthIntegrationEnv(t, authIntegrationOptions{})
			db := env.workspace.DB()
			reader := seedHumanPrincipalForLockoutTest(t, context.Background(), db, "quiet-reader", "quiet-reader-actor", "quiet-reader", "quiet-reader-token")
			for _, q := range []string{
				`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('private','now','owner','{"pm_actor_id":"owner"}')`,
				`INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) VALUES('000000','message_posted','2026-01-01T00:00:00Z','owner','[]','{}')`,
			} {
				if _, err := db.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			if hidden > 0 {
				if _, err := db.Exec(`WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<?)
				INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) SELECT printf('%06d',i),'message_posted','2026-01-01T00:00:00Z','owner','private','[]','{}' FROM n`, hidden); err != nil {
					t.Fatal(err)
				}
			}
			srv := eventPrivacyHTTPServer(t, env, 20*time.Millisecond)
			if got := readQuietEventStream(t, srv.URL+"/stream/events", reader.AccessToken, "000000", true); got != strings.Repeat(": keepalive\n\n", 3) {
				t.Fatalf("wire=%q", got)
			}
		})
	}
}

type eventTickBudgetStore struct {
	PrimitiveStore
	calls        chan int
	gate         <-chan struct{}
	hiddenPages  int
	visiblePages int
	pages        int
}

func (s *eventTickBudgetStore) EventStreamCursor(context.Context, string) (primitives.EventCursor, error) {
	return primitives.EventCursor{}, nil
}
func (s *eventTickBudgetStore) ListEventStreamPage(ctx context.Context, _ primitives.EventListFilter, cursor primitives.EventCursor) (primitives.EventStreamPage, error) {
	select {
	case s.calls <- 1:
	case <-ctx.Done():
		return primitives.EventStreamPage{}, ctx.Err()
	}
	s.pages++
	if s.gate != nil {
		select {
		case <-s.gate:
		case <-ctx.Done():
			return primitives.EventStreamPage{}, ctx.Err()
		}
	}
	if s.pages > s.hiddenPages && (s.hiddenPages > 0 || s.visiblePages > 0) {
		visible := s.pages - s.hiddenPages
		id := "public-probe"
		if visible > 1 {
			id = fmt.Sprintf("public-probe-%d", visible)
		}
		return primitives.EventStreamPage{Events: []map[string]any{{"id": id}}, Cursor: cursor, HasMore: visible < s.visiblePages}, nil
	}
	return primitives.EventStreamPage{Cursor: cursor, HasMore: true}, nil
}

// A one-hour poll interval makes any timer wait during hidden catch-up fail.
func TestEventsStreamChunksContinueWithoutPollWait(t *testing.T) {
	for _, fixture := range []struct{ hidden, visible int }{{50, 1}, {0, 3}, {10, 3}} {
		t.Run(fmt.Sprintf("hidden-%d-visible-%d", fixture.hidden, fixture.visible), func(t *testing.T) {
			s := &eventTickBudgetStore{calls: make(chan int, 60), hiddenPages: fixture.hidden, visiblePages: fixture.visible}
			done := make(chan struct{})
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				defer close(done)
				handleEventsStream(w, r, handlerOptions{primitiveStore: s, contract: &schema.Contract{}, streamPollInterval: time.Hour})
			}))
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, "GET", srv.URL+"/stream/events", nil)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			reader := bufio.NewReader(resp.Body)
			if frame := round4SSEFrame(t, reader); frame != ": keepalive\n\n" {
				t.Fatalf("initial frame: %q", frame)
			}
			for i := 1; i <= fixture.visible; i++ {
				id := "public-probe"
				if i > 1 {
					id = fmt.Sprintf("public-probe-%d", i)
				}
				if frame := round4SSEFrame(t, reader); !strings.Contains(frame, "id: "+id+"\n") || !strings.Contains(frame, "event: event\n") {
					t.Fatalf("catch-up emitted a control or lost/duplicated the probe: %q", frame)
				}
			}
			cancel()
			resp.Body.Close()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("cancelled handler did not join scanner")
			}
			if got := len(s.calls); got != fixture.hidden+fixture.visible {
				t.Fatalf("read %d pages, want %d", got, fixture.hidden+fixture.visible)
			}
		})
	}
}

// Keepalives must continue even while a hidden-page read has not returned.
// Blocking instead of sleeping makes this independent of machine/query timing.
func TestEventsStreamKeepalivesContinueDuringHiddenScanAndCancelWorker(t *testing.T) {
	s := &eventTickBudgetStore{calls: make(chan int, 20), gate: make(chan struct{})}
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		handleEventsStream(w, r, handlerOptions{primitiveStore: s, contract: &schema.Contract{}, streamPollInterval: 20 * time.Millisecond})
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", srv.URL+"/stream/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	select {
	case <-s.calls:
	case <-ctx.Done():
		t.Fatal("scanner did not start")
	}
	reader := bufio.NewReader(resp.Body)
	for i := 0; i < 4; i++ {
		if frame := round4SSEFrame(t, reader); frame != ": keepalive\n\n" {
			t.Fatalf("hidden activity frame: %q", frame)
		}
	}
	select {
	case <-s.calls:
		t.Fatal("overlapping scan request")
	default:
	}
	cancel()
	resp.Body.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled handler did not join scanner")
	}
}

// Measure actual page work so CI load and the accepted authorization cost do
// not masquerade as timer delays. The allowance excludes initialization before
// headers; authorized resume validation warms the identical principal cache.
type timedEventPageStore struct {
	PrimitiveStore
	readNanos atomic.Int64
}

func (s *timedEventPageStore) ListEventStreamPage(ctx context.Context, filter primitives.EventListFilter, cursor primitives.EventCursor) (primitives.EventStreamPage, error) {
	start := time.Now()
	page, err := s.PrimitiveStore.ListEventStreamPage(ctx, filter, cursor)
	s.readNanos.Add(int64(time.Since(start)))
	return page, err
}

func TestEventsStreamPublicProbeLatencyDoesNotWaitForHiddenPollTicks(t *testing.T) {
	const schedulingTolerance = 100 * time.Millisecond
	idleLatency := map[time.Duration]time.Duration{}
	for _, hidden := range []int{0, 10000} {
		t.Run(fmt.Sprint(hidden), func(t *testing.T) {
			env := newAuthIntegrationEnv(t, authIntegrationOptions{})
			db := env.workspace.DB()
			reader := seedHumanPrincipalForLockoutTest(t, context.Background(), db, "latency-reader", "latency-reader-actor", "latency-reader", "latency-reader-token")
			for _, query := range []string{
				`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('private','now','owner','{"pm_actor_id":"owner"}')`,
				`INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) VALUES('000000','message_posted','2026-01-01T00:00:00Z','owner','[]','{}')`,
				`INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) VALUES('public-probe','message_posted','2026-01-01T00:00:01Z','owner','[]','{}')`,
			} {
				if _, err := db.Exec(query); err != nil {
					t.Fatal(err)
				}
			}
			if hidden > 0 {
				if _, err := db.Exec(`WITH RECURSIVE n(i) AS (VALUES(1) UNION ALL SELECT i+1 FROM n WHERE i<?)
				INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) SELECT printf('%06d',i),'message_posted','2026-01-01T00:00:00Z','owner','private','[]','{}' FROM n`, hidden); err != nil {
					t.Fatal(err)
				}
			}
			store := &timedEventPageStore{PrimitiveStore: env.primitiveStore}
			env.primitiveStore = store
			// The 100ms run primes the identical principal/epoch cache. The
			// default-interval run also reports warm request-to-delivery time;
			// canonical cold capture remains a separate accepted cost.
			for _, pollInterval := range []time.Duration{100 * time.Millisecond, time.Second} {
				t.Run(pollInterval.String(), func(t *testing.T) {
					store.readNanos.Store(0)
					srv := eventPrivacyHTTPServer(t, env, pollInterval)
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					req, err := http.NewRequestWithContext(ctx, "GET", srv.URL+"/stream/events", nil)
					if err != nil {
						t.Fatal(err)
					}
					req.Header.Set("Authorization", "Bearer "+reader.AccessToken)
					req.Header.Set("Last-Event-ID", "000000")
					requestStart := time.Now()
					resp, err := http.DefaultClient.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					defer resp.Body.Close()
					if resp.StatusCode != 200 {
						t.Fatalf("status=%d", resp.StatusCode)
					}
					start := time.Now()
					wire := bufio.NewReader(resp.Body)
					for {
						frame := round4SSEFrame(t, wire)
						if frame == ": keepalive\n\n" {
							continue
						}
						if !strings.Contains(frame, "id: public-probe\n") || !strings.Contains(frame, "event: event\n") {
							t.Fatalf("unexpected frame before public probe: %q", frame)
						}
						break
					}
					latency := time.Since(start)
					pageWork := time.Duration(store.readNanos.Load())
					t.Logf("hidden=%d poll=%s request_to_delivery=%s after_headers=%s page_work=%s", hidden, pollInterval, time.Since(requestStart), latency, pageWork)
					if hidden == 0 {
						idleLatency[pollInterval] = latency
						return
					}
					// Pending history may cost more SQL, but must not add five
					// poll intervals. Allow framing and scheduler variance.
					if latency > idleLatency[pollInterval]+pageWork+schedulingTolerance {
						t.Fatalf("hidden backlog added timer delay: idle=%s hidden=%s work=%s tolerance=%s", idleLatency[pollInterval], latency, pageWork, schedulingTolerance)
					}
				})
			}
		})
	}
}
