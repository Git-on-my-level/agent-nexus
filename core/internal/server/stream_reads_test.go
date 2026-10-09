package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testsql"
)

func TestFiveIdleInboxStreamsSharePageAndObserveNonEventWrites(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	thread := seedStreamPrivacyThread(t, env.primitiveStore.(*primitives.Store), "owner", false)
	seedStreamPrivacyInbox(t, env.primitiveStore.(*primitives.Store), thread, streamPrivacyInboxItem(thread, "idle-item", "Initial"))
	db, counter := testsql.Open("file:" + env.workspace.Layout().DatabasePath)
	defer db.Close()
	db.SetMaxOpenConns(8)
	store := primitives.NewTestStore(db, env.workspace.Layout().ArtifactContentDir)
	opts := handlerOptions{primitiveStore: store, streamReads: &streamReadHub{}, streamPollInterval: 10 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	defer func() { cancel(); wg.Wait() }()
	writers := make([]*idleProfileWriter, 5)
	for i := range writers {
		writers[i] = &idleProfileWriter{ready: make(chan struct{}), frames: make(chan string, 64)}
		w := writers[i]
		wg.Add(1)
		go func() {
			defer wg.Done()
			handleInboxStream(w, httptest.NewRequest("GET", "/stream/inbox", nil).WithContext(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "reader"})), opts)
		}()
	}
	for _, w := range writers {
		awaitStreamText(t, w.frames, `"body":"Initial"`)
	}
	pages := 0
	for _, q := range counter.Statements() {
		if inboxSelector(q.SQL) {
			pages++
		}
	}
	if pages != 1 {
		t.Fatalf("five initial subscribers computed %d pages; want 1", pages)
	}
	counter.Reset()
	// Each stream must complete several ticks, with keepalives and no page SQL.
	for _, w := range writers {
		for i := 0; i < 3; i++ {
			awaitStreamText(t, w.frames, ": keepalive")
		}
	}
	for _, q := range counter.Statements() {
		if inboxSelector(q.SQL) {
			t.Fatal("idle stream recomputed inbox")
		}
	}
	if _, err := env.workspace.DB().Exec(`UPDATE derived_inbox_items SET data_json=json_set(data_json,'$.body','Updated') WHERE id='idle-item'`); err != nil {
		t.Fatal(err)
	}
	for _, w := range writers {
		awaitStreamText(t, w.frames, `"body":"Updated"`)
	}
	pages = 0
	for _, q := range counter.Statements() {
		if inboxSelector(q.SQL) {
			pages++
		}
	}
	if pages != 1 {
		t.Fatalf("one non-event commit caused %d shared page reads; want 1", pages)
	}
	cancel()
	wg.Wait()
	if opts.streamReads.refs != 0 || opts.streamReads.clock != nil || opts.streamReads.pages != nil {
		t.Fatal("observer/cache leaked after last subscriber left")
	}
}

type cancelingInboxStore struct {
	*primitives.Store
	calls   atomic.Int64
	entered chan struct{}
}

func (s *cancelingInboxStore) ListDerivedInboxItems(ctx context.Context, f primitives.DerivedInboxListFilter) ([]primitives.DerivedInboxItem, error) {
	if s.calls.Add(1) == 1 {
		close(s.entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return s.Store.ListDerivedInboxItems(ctx, f)
}

func TestSharedInboxCanceledLeaderDoesNotClosePeer(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	for _, deadline := range []bool{false, true} {
		t.Run(fmt.Sprint("deadline=", deadline), func(t *testing.T) {
			env := newAuthIntegrationEnv(t, authIntegrationOptions{})
			base := env.primitiveStore.(*primitives.Store)
			thread := seedStreamPrivacyThread(t, base, "owner", false)
			seedStreamPrivacyInbox(t, base, thread, streamPrivacyInboxItem(thread, "peer-item", "Alive"))
			store := &cancelingInboxStore{Store: base, entered: make(chan struct{})}
			hub := &streamReadHub{}
			release, err := hub.acquire(context.Background(), store)
			if err != nil {
				t.Fatal(err)
			}
			defer release()
			v, observed, err := hub.revision(context.Background())
			if err != nil || !observed {
				t.Fatal(err)
			}
			var ctx context.Context
			var cancel context.CancelFunc
			if deadline {
				ctx, cancel = context.WithTimeout(context.Background(), 150*time.Millisecond)
			} else {
				ctx, cancel = context.WithCancel(context.Background())
			}
			defer cancel()
			opts := handlerOptions{primitiveStore: store, streamReads: hub, streamPollInterval: time.Hour}
			load := func(ctx context.Context) error {
				records, _, err := hub.inboxPage(httptest.NewRequest("GET", "/stream/inbox", nil).WithContext(ctx), opts, primitives.DerivedInboxListFilter{}, v, observed)
				if err == nil && len(records) != 1 {
					return fmt.Errorf("got %d records", len(records))
				}
				return err
			}
			leader, peer := make(chan error, 1), make(chan error, 1)
			go func() { leader <- load(ctx) }()
			<-store.entered
			go func() { peer <- load(context.Background()) }()
			select {
			case err := <-peer:
				t.Fatalf("peer did not wait for shared load: %v", err)
			case <-time.After(30 * time.Millisecond):
			}
			if !deadline {
				cancel()
			}
			if err := <-leader; err == nil {
				t.Fatal("canceled leader succeeded")
			}
			select {
			case err := <-peer:
				if err != nil {
					t.Fatalf("live peer terminated: %v", err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("peer stuck")
			}
			if store.calls.Load() != 2 {
				t.Fatalf("calls=%d; want canceled load + retry", store.calls.Load())
			}
		})
	}
}

func awaitStreamText(t *testing.T, frames <-chan string, want string) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case frame := <-frames:
			if strings.Contains(frame, want) {
				return
			}
			if strings.Contains(frame, "event: error") {
				t.Fatalf("stream failed: %s", frame)
			}
		case <-timer.C:
			t.Fatalf("no frame containing %q", want)
		}
	}
}

func TestSharedInboxPageScopeIsolationAndPermissionCommit(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	store := env.primitiveStore.(*primitives.Store)
	thread := seedStreamPrivacyThread(t, store, "owner", false)
	if _, err := store.PatchThread(context.Background(), "owner", thread, map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	seedStreamPrivacyInbox(t, store, thread, streamPrivacyInboxItem(thread, "private-item", "Secret"))
	hub := &streamReadHub{}
	release, err := hub.acquire(context.Background(), store)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	opts := handlerOptions{primitiveStore: store, streamReads: hub, streamPollInterval: time.Hour}
	read := func(actor string) []inboxStreamRecord {
		r := httptest.NewRequest("GET", "/stream/inbox", nil).WithContext(primitives.WithAccessScope(context.Background(), primitives.AccessScope{ActorID: actor}))
		v, observed, err := hub.revision(r.Context())
		if err != nil || !observed {
			t.Fatalf("revision: %v observed=%t", err, observed)
		}
		rows, _, err := hub.inboxPage(r, opts, primitives.DerivedInboxListFilter{}, v, observed)
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	if rows := read("owner"); len(rows) != 1 {
		t.Fatalf("owner page: %+v", rows)
	}
	if rows := read("reader"); len(rows) != 0 {
		t.Fatalf("shared cache disclosed private item: %+v", rows)
	}
	if _, err := store.PatchThread(context.Background(), "owner", thread, map[string]any{"pm_actor_id": "reader"}, nil); err != nil {
		t.Fatal(err)
	}
	if rows := read("owner"); len(rows) != 0 {
		t.Fatalf("stale permission page: %+v", rows)
	}
	if rows := read("reader"); len(rows) != 1 {
		t.Fatalf("new owner did not receive item: %+v", rows)
	}
}

func TestStreamPollIntervalMinimum(t *testing.T) {
	t.Parallel()
	for _, input := range []time.Duration{0, -1, time.Nanosecond, 10 * time.Millisecond} {
		if got := streamPollInterval(handlerOptions{streamPollInterval: input}); got != minimumStreamPollInterval {
			t.Fatal(fmt.Sprint(input, " => ", got))
		}
	}
	if got := streamPollInterval(handlerOptions{streamPollInterval: time.Second}); got != time.Second {
		t.Fatal(got)
	}
}

func TestSharedInboxLateSubscriberGetsCurrentAges(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	store := env.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	card, err := store.CreateWork(ctx, "owner", "", map[string]any{"title": "Current age", "phase": "in_progress"})
	if err != nil {
		t.Fatal(err)
	}
	thread := anyString(card["thread_id"])
	item := streamPrivacyInboxItem(thread, "aged-item", "Age")
	item.Data["subject_ref"], item.Data["related_refs"] = card["ref"], []any{card["ref"]}
	seedStreamPrivacyInbox(t, store, thread, item)
	hub := &streamReadHub{}
	release, err := hub.acquire(ctx, store)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	r := httptest.NewRequest("GET", "/stream/inbox", nil).WithContext(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "owner"}))
	v, observed, err := hub.revision(ctx)
	if err != nil || !observed {
		t.Fatal(err)
	}
	opts := handlerOptions{primitiveStore: store, streamPollInterval: 100 * time.Millisecond}
	first, _, err := hub.inboxPage(r, opts, primitives.DerivedInboxListFilter{}, v, observed)
	if err != nil || len(first) != 1 {
		t.Fatalf("first page: %v", err)
	}
	age := *first[0].data["related_cards"].([]primitives.RefPreview)[0].Summary.Age
	time.Sleep(1100 * time.Millisecond)
	next, stillObserved, err := hub.revision(ctx)
	if err != nil || !stillObserved || next != v {
		t.Fatalf("timer changed database revision: %d => %d", v, next)
	}
	second, _, err := hub.inboxPage(r, opts, primitives.DerivedInboxListFilter{}, v, observed)
	if err != nil || len(second) != 1 {
		t.Fatalf("second page: %v", err)
	}
	if *second[0].data["related_cards"].([]primitives.RefPreview)[0].Summary.Age <= age {
		t.Fatal("late subscriber received stale age")
	}
	if first[0].eventID != second[0].eventID {
		t.Fatal("timer changed stream identity")
	}
}

func TestInboxCommitDuringSweepRevisitsEarlierPage(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	base := env.primitiveStore.(*primitives.Store)
	thread := seedStreamPrivacyThread(t, base, "owner", false)
	items := make([]primitives.DerivedInboxItem, 405)
	for i := range items {
		items[i] = streamPrivacyInboxItem(thread, fmt.Sprintf("sweep-%03d", i), "Initial")
	}
	seedStreamPrivacyInbox(t, base, thread, items...)
	db, counter := testsql.Open("file:" + env.workspace.Layout().DatabasePath)
	defer db.Close()
	db.SetMaxOpenConns(8)
	var pages atomic.Int64
	counter.BeforeQuery(func(q string) {
		if inboxSelector(q) && pages.Add(1) == 2 {
			if _, err := env.workspace.DB().Exec(`UPDATE derived_inbox_items SET data_json=json_set(data_json,'$.body','Changed early item') WHERE id='sweep-000'`); err != nil {
				panic(err)
			}
		}
	})
	store := primitives.NewTestStore(db, env.workspace.Layout().ArtifactContentDir)
	opts := handlerOptions{primitiveStore: store, streamReads: &streamReadHub{}, streamPollInterval: 100 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w := &idleProfileWriter{ready: make(chan struct{}), frames: make(chan string, 64)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		handleInboxStream(w, httptest.NewRequest("GET", "/stream/inbox", nil).WithContext(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "reader"})), opts)
	}()
	defer func() { cancel(); <-done }()
	awaitStreamText(t, w.frames, `"body":"Changed early item"`)
	if pages.Load() < 4 {
		t.Fatalf("early-page update did not require resweep: pages=%d", pages.Load())
	}
}

func inboxSelector(sql string) bool {
	return strings.Contains(sql, "SELECT id, thread_id, category, trigger_at")
}

type idleProfileWriter struct {
	ready  chan struct{}
	frames chan string
	once   sync.Once
	chunk  strings.Builder
}

func (w *idleProfileWriter) Header() http.Header         { return http.Header{} }
func (w *idleProfileWriter) WriteHeader(int)             {}
func (w *idleProfileWriter) Write(p []byte) (int, error) { return w.chunk.Write(p) }
func (w *idleProfileWriter) Flush() {
	s := w.chunk.String()
	if w.frames != nil && s != "" {
		select {
		case w.frames <- s:
		default:
		}
	}
	if strings.Contains(s, ": keepalive") || strings.Contains(s, `"partial":false`) {
		w.once.Do(func() { close(w.ready) })
	}
	w.chunk.Reset()
}
