package delivery_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"agent-nexus-core/internal/delivery"
	"agent-nexus-core/internal/storage"
)

var baseNow = time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)

type env struct {
	db    *sql.DB
	store delivery.Store
	now   *time.Time
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ws, err := storage.InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("init workspace: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	now := baseNow
	e := &env{db: ws.DB(), now: &now}
	e.store = delivery.Store{DB: e.db, Now: func() time.Time { return *e.now }}
	return e
}

func (e *env) advance(d time.Duration) { *e.now = e.now.Add(d) }

func rfc(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000000000Z") }

func (e *env) addAsk(t *testing.T, id string, ts time.Time, by string, blocks int, title string) {
	t.Helper()
	bl := make([]string, blocks)
	for i := range bl {
		bl[i] = "thread:" + string(rune('a'+i))
	}
	payload, _ := json.Marshal(map[string]any{"payload": map[string]any{"title": title, "requester_actor_id": "actor-a", "by": by, "blocks": bl}})
	if _, err := e.db.Exec(`INSERT INTO events(id,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES(?, 'human_attention_requested', ?, 'actor-a', 'thread-1', '[]', ?)`, id, rfc(ts), string(payload)); err != nil {
		t.Fatalf("insert ask: %v", err)
	}
}

func (e *env) askRowCount(t *testing.T, id string) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM ask_deadlines WHERE request_event_id=?`, id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *env) setChannel(t *testing.T, principal, channel, address string, push, digest bool) {
	t.Helper()
	b := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	if _, err := e.db.Exec(`INSERT OR REPLACE INTO notification_channel_settings(principal_id,channel,address,push_enabled,digest_enabled,updated_at) VALUES(?,?,?,?,?,?)`,
		principal, channel, address, b(push), b(digest), rfc(*e.now)); err != nil {
		t.Fatal(err)
	}
}

func (e *env) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := e.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// refreshUntilComplete runs the readability refresh until every notified
// principal's counts are complete at the current access epoch.
func (e *env) refreshUntilComplete(t *testing.T, canRead func(context.Context, string, string) bool) {
	t.Helper()
	ctx := context.Background()
	for i := 0; i < 200; i++ {
		if err := e.store.RefreshReadability(ctx, *e.now, canRead); err != nil {
			t.Fatal(err)
		}
		var stale int
		if err := e.db.QueryRow(`SELECT (SELECT COUNT(*) FROM principal_ask_readability WHERE checked_epoch < COALESCE((SELECT version FROM resource_access_epoch WHERE singleton=1),0))
			+ (SELECT COUNT(*) FROM ask_readability_queue) + (SELECT COUNT(*) FROM principal_readability_coverage WHERE complete=0)`).Scan(&stale); err != nil {
			t.Fatal(err)
		}
		if stale == 0 {
			return
		}
	}
	t.Fatal("readability never completed")
}
