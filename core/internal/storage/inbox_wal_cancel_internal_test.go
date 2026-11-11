package storage_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/server"
	"agent-nexus-core/internal/storage"
)

// cancelAfterFirstNextDriver sits beneath the production scope connector. It
// cancels the request context only after SQLite has produced the first row for
// the selected table, so the cancellation is deterministic and happens while a
// result cursor is live rather than before the query starts.
type cancelAfterFirstNextDriver struct {
	driver.Driver
	table  string
	cancel context.CancelFunc
	armed  *atomic.Bool
	fired  *atomic.Bool
}

func (d *cancelAfterFirstNextDriver) Open(name string) (driver.Conn, error) {
	conn, err := d.Driver.Open(name)
	if err != nil {
		return nil, err
	}
	return &cancelAfterFirstNextConn{Conn: conn, driver: d}, nil
}

type cancelAfterFirstNextConn struct {
	driver.Conn
	driver *cancelAfterFirstNextDriver
}

func (c *cancelAfterFirstNextConn) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	if conn, ok := c.Conn.(driver.ConnBeginTx); ok {
		return conn.BeginTx(ctx, options)
	}
	if options.ReadOnly || options.Isolation != 0 {
		return nil, fmt.Errorf("unsupported transaction options")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.Conn.Begin()
}

func (c *cancelAfterFirstNextConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if conn, ok := c.Conn.(driver.ConnPrepareContext); ok {
		return conn.PrepareContext(ctx, query)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return c.Conn.Prepare(query)
}

func (c *cancelAfterFirstNextConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if conn, ok := c.Conn.(driver.ExecerContext); ok {
		return conn.ExecContext(ctx, query, args)
	}
	return nil, driver.ErrSkip
}

func (c *cancelAfterFirstNextConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	conn, ok := c.Conn.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	rows, err := conn.QueryContext(ctx, query, args)
	if err != nil {
		return nil, err
	}
	if c.driver.armed.Load() && strings.Contains(query, c.driver.table) {
		return &cancelAfterFirstNextRows{Rows: rows, driver: c.driver}, nil
	}
	return rows, nil
}

func (c *cancelAfterFirstNextConn) CheckNamedValue(value *driver.NamedValue) error {
	if conn, ok := c.Conn.(driver.NamedValueChecker); ok {
		return conn.CheckNamedValue(value)
	}
	return driver.ErrSkip
}

func (c *cancelAfterFirstNextConn) ResetSession(ctx context.Context) error {
	if conn, ok := c.Conn.(driver.SessionResetter); ok {
		return conn.ResetSession(ctx)
	}
	return ctx.Err()
}

func (c *cancelAfterFirstNextConn) IsValid() bool {
	if conn, ok := c.Conn.(driver.Validator); ok {
		return conn.IsValid()
	}
	return true
}

func (c *cancelAfterFirstNextConn) Ping(ctx context.Context) error {
	if conn, ok := c.Conn.(driver.Pinger); ok {
		return conn.Ping(ctx)
	}
	return ctx.Err()
}

type cancelAfterFirstNextRows struct {
	driver.Rows
	driver *cancelAfterFirstNextDriver
	once   sync.Once
}

func (r *cancelAfterFirstNextRows) Next(dest []driver.Value) error {
	err := r.Rows.Next(dest)
	if err == nil {
		r.once.Do(func() {
			r.driver.fired.Store(true)
			r.driver.cancel()
		})
	}
	return err
}

func (r *cancelAfterFirstNextRows) ColumnTypeDatabaseTypeName(index int) string {
	if rows, ok := r.Rows.(driver.RowsColumnTypeDatabaseTypeName); ok {
		return rows.ColumnTypeDatabaseTypeName(index)
	}
	return ""
}

func (r *cancelAfterFirstNextRows) ColumnTypeLength(index int) (int64, bool) {
	if rows, ok := r.Rows.(driver.RowsColumnTypeLength); ok {
		return rows.ColumnTypeLength(index)
	}
	return 0, false
}

func (r *cancelAfterFirstNextRows) ColumnTypeNullable(index int) (bool, bool) {
	if rows, ok := r.Rows.(driver.RowsColumnTypeNullable); ok {
		return rows.ColumnTypeNullable(index)
	}
	return false, false
}

func (r *cancelAfterFirstNextRows) ColumnTypePrecisionScale(index int) (int64, int64, bool) {
	if rows, ok := r.Rows.(driver.RowsColumnTypePrecisionScale); ok {
		return rows.ColumnTypePrecisionScale(index)
	}
	return 0, 0, false
}

func (r *cancelAfterFirstNextRows) ColumnTypeScanType(index int) reflect.Type {
	if rows, ok := r.Rows.(driver.RowsColumnTypeScanType); ok {
		return rows.ColumnTypeScanType(index)
	}
	return reflect.TypeOf("")
}

func (r *cancelAfterFirstNextRows) HasNextResultSet() bool {
	if rows, ok := r.Rows.(driver.RowsNextResultSet); ok {
		return rows.HasNextResultSet()
	}
	return false
}

func (r *cancelAfterFirstNextRows) NextResultSet() error {
	if rows, ok := r.Rows.(driver.RowsNextResultSet); ok {
		return rows.NextResultSet()
	}
	return io.EOF
}

func seedWALInboxFixture(t *testing.T, ctx context.Context, ws *storage.Workspace) {
	t.Helper()
	const threadID = "thread-wal"
	const itemID = "wal-item"
	data := `{"kind":"ask","title":"WAL snapshot test","thread_id":"thread-wal"}`
	if _, err := ws.DB().ExecContext(ctx,
		`INSERT INTO derived_inbox_items(id,thread_id,category,trigger_at,generated_at,data_json,lifecycle_ready) VALUES(?,?,?,?,?,?,1)`,
		itemID, threadID, "ask", "2026-11-11T00:00:00Z", "2026-11-11T00:00:00Z", data,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.DB().ExecContext(ctx,
		`INSERT INTO derived_topic_views(thread_id,stale,generated_at,data_json) VALUES(?,0,?,?)`,
		threadID, "2026-11-11T00:00:00Z", `{"thread_id":"thread-wal"}`,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.DB().ExecContext(ctx,
		`INSERT INTO topic_projection_refresh_status(thread_id,desired_generation,materialized_generation,updated_at) VALUES(?,1,1,?)`,
		threadID, "2026-11-11T00:00:00Z",
	); err != nil {
		t.Fatal(err)
	}
}

func openWALCheckpointPool(t *testing.T, ws *storage.Workspace) *sql.DB {
	t.Helper()
	pool, err := sql.Open("sqlite", storage.SQLiteDSNForTest(ws.Layout().DatabasePath))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	if err := pool.Ping(); err != nil {
		t.Fatal(err)
	}
	return pool
}

func truncateWAL(t *testing.T, pool *sql.DB) {
	t.Helper()
	var busy, logFrames, checkpointed int64
	if err := pool.QueryRow(`PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logFrames, &checkpointed); err != nil {
		t.Fatal(err)
	}
	if busy != 0 {
		t.Fatalf("baseline checkpoint busy=%d", busy)
	}
}

func writeWALFrame(t *testing.T, pool *sql.DB, suffix string) {
	t.Helper()
	if _, err := pool.Exec(
		`INSERT INTO actors(id,display_name,tags_json,created_at,metadata_json) VALUES(?,?,?,?,?)`,
		"wal-writer-"+suffix, "WAL Writer", "[]", time.Now().UTC().Format(time.RFC3339Nano), "{}",
	); err != nil {
		t.Fatal(err)
	}
}

func passiveCheckpoint(t *testing.T, pool *sql.DB) (busy, logFrames, checkpointed int64) {
	t.Helper()
	if err := pool.QueryRow(`PRAGMA wal_checkpoint(PASSIVE)`).Scan(&busy, &logFrames, &checkpointed); err != nil {
		t.Fatal(err)
	}
	return busy, logFrames, checkpointed
}

func newWALInboxHandler(ws *storage.Workspace) http.Handler {
	store := primitives.NewStore(
		ws.DB(),
		blob.NewFilesystemBackend(ws.Layout().ArtifactContentDir),
		ws.Layout().ArtifactContentDir,
	)
	return server.NewHandler("wal-test", server.WithPrimitiveStore(store), server.WithEnableDevActorMode(true))
}

// TestInboxCancellationReleasesWALSnapshot drives the real /inbox/{id} handler
// through the production scope connector, cancels the request after the first
// row of a selected projection table, then proves a later write can be fully
// checkpointed. A still-pinned old snapshot would leave log > checkpointed.
func TestInboxCancellationReleasesWALSnapshot(t *testing.T) {
	for _, table := range []string{"derived_topic_views", "topic_projection_refresh_status"} {
		t.Run(table, func(t *testing.T) {
			ctx := context.Background()
			requestCtx, cancel := context.WithCancel(context.Background())
			defer cancel()
			armed := &atomic.Bool{}
			fired := &atomic.Bool{}
			ws := storage.OpenWorkspaceWithDriverForTest(t, t.TempDir(), func(d driver.Driver) driver.Driver {
				return &cancelAfterFirstNextDriver{Driver: d, table: table, cancel: cancel, armed: armed, fired: fired}
			})
			seedWALInboxFixture(t, ctx, ws)
			pool := openWALCheckpointPool(t, ws)
			truncateWAL(t, pool)

			// Hold one connection so the handler's first read must open a second
			// connection. The canceled read fully consumes its single-row cursor, so
			// the connection is safe to return to the pool and keeps its lease.
			held, err := ws.DB().Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer held.Close()
			beforeRefs := storage.LeaseRefsForTest(ws)

			armed.Store(true)
			handler := newWALInboxHandler(ws)
			req := httptest.NewRequest(http.MethodGet, "/inbox/wal-item", nil).WithContext(requestCtx)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if !fired.Load() {
				t.Fatalf("cancellation hook did not fire; status=%d body=%s", rec.Code, rec.Body.String())
			}
			if requestCtx.Err() == nil {
				t.Fatal("request context was not canceled")
			}

			writeWALFrame(t, pool, table)
			busy, logFrames, checkpointed := passiveCheckpoint(t, pool)
			if busy != 0 {
				t.Errorf("checkpoint busy=%d, want 0", busy)
			}
			if logFrames <= 0 {
				t.Errorf("checkpoint log frames=%d, want >0", logFrames)
			}
			if logFrames != checkpointed {
				t.Errorf("WAL snapshot still pinned: log=%d checkpointed=%d", logFrames, checkpointed)
			}
			if got := storage.LeaseRefsForTest(ws); got != beforeRefs+1 {
				t.Errorf("process lease refs after fully consumed canceled read = %d, want %d", got, beforeRefs+1)
			}
		})
	}
}

// TestInboxReadWithoutCancellationKeepsWALCheckpointable is the control: an
// ordinary read must still succeed and leave the WAL fully checkpointable.
func TestInboxReadWithoutCancellationKeepsWALCheckpointable(t *testing.T) {
	ctx := context.Background()
	ws := storage.OpenWorkspaceWithDriverForTest(t, t.TempDir(), nil)
	seedWALInboxFixture(t, ctx, ws)
	pool := openWALCheckpointPool(t, ws)
	truncateWAL(t, pool)

	handler := newWALInboxHandler(ws)
	req := httptest.NewRequest(http.MethodGet, "/inbox/wal-item", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	writeWALFrame(t, pool, "control")
	busy, logFrames, checkpointed := passiveCheckpoint(t, pool)
	if busy != 0 {
		t.Fatalf("checkpoint busy=%d, want 0", busy)
	}
	if logFrames <= 0 {
		t.Fatalf("checkpoint log frames=%d, want >0", logFrames)
	}
	if logFrames != checkpointed {
		t.Fatalf("control read left WAL pinned: log=%d checkpointed=%d", logFrames, checkpointed)
	}
}
