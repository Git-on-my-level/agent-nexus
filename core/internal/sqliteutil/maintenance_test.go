package sqliteutil

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"testing"
)

func TestSetConnectionBusyTimeoutWriteFailureDoesNotLeak(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		failExecs       int
		applyBeforeFail int
		wantOpens       int
	}{
		{name: "restore", failExecs: 1, applyBeforeFail: 1, wantOpens: 1},
		{name: "discard", failExecs: 2, applyBeforeFail: 1, wantOpens: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			state := &busyTimeoutState{initial: 20000, failExecs: tt.failExecs, applyBeforeFail: tt.applyBeforeFail}
			db := openBusyTimeoutDB(t, state)
			defer db.Close()
			ctx := context.Background()
			conn, err := db.Conn(ctx)
			if err != nil {
				t.Fatal(err)
			}
			restore, err := SetConnectionBusyTimeout(ctx, conn, 0)
			if err == nil {
				t.Fatal("write reported success")
			}
			if restore != nil {
				t.Fatal("restore returned after a failed write")
			}
			if closeErr := conn.Close(); closeErr != nil && !errors.Is(closeErr, sql.ErrConnDone) {
				t.Fatal(closeErr)
			}
			var timeout int
			if err := db.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&timeout); err != nil {
				t.Fatal(err)
			}
			if timeout != 20000 {
				t.Fatalf("busy_timeout = %d, want 20000", timeout)
			}
			if opens := state.opensCount(); opens != tt.wantOpens {
				t.Fatalf("opens = %d, want %d", opens, tt.wantOpens)
			}
		})
	}
}

var busyTimeoutDriverSeq atomic.Int64

func openBusyTimeoutDB(t *testing.T, state *busyTimeoutState) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("sqliteutil-busy-timeout-%d", busyTimeoutDriverSeq.Add(1))
	sql.Register(name, &busyTimeoutDriver{state: state})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	return db
}

// busyTimeoutState simulates a connection-local busy timeout whose write can
// apply and then fail, which is how modernc.org/sqlite reports a deadline.
type busyTimeoutState struct {
	mu              sync.Mutex
	initial         int
	failExecs       int
	applyBeforeFail int
	opens           int
}

func (s *busyTimeoutState) opensCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.opens
}

type busyTimeoutDriver struct {
	state *busyTimeoutState
}

func (d *busyTimeoutDriver) Open(string) (driver.Conn, error) {
	d.state.mu.Lock()
	defer d.state.mu.Unlock()
	d.state.opens++
	return &busyTimeoutConn{state: d.state, timeout: d.state.initial}, nil
}

type busyTimeoutConn struct {
	state   *busyTimeoutState
	timeout int
}

func (c *busyTimeoutConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not prepared")
}

func (c *busyTimeoutConn) Close() error { return nil }

func (c *busyTimeoutConn) Begin() (driver.Tx, error) {
	return nil, errors.New("no transaction")
}

func (c *busyTimeoutConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	if query != `PRAGMA busy_timeout` {
		return nil, fmt.Errorf("unexpected query %q", query)
	}
	c.state.mu.Lock()
	timeout := c.timeout
	c.state.mu.Unlock()
	return &busyTimeoutRows{value: int64(timeout)}, nil
}

func (c *busyTimeoutConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	var timeout int
	if _, err := fmt.Sscanf(query, "PRAGMA busy_timeout=%d", &timeout); err != nil {
		return nil, fmt.Errorf("unexpected exec %q", query)
	}
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	if c.state.failExecs > 0 {
		c.state.failExecs--
		if c.state.applyBeforeFail > 0 {
			c.state.applyBeforeFail--
			c.timeout = timeout
		}
		return nil, errors.New("busy timeout write failed")
	}
	c.timeout = timeout
	return busyTimeoutResult{}, nil
}

type busyTimeoutResult struct{}

func (busyTimeoutResult) LastInsertId() (int64, error) { return 0, nil }
func (busyTimeoutResult) RowsAffected() (int64, error) { return 0, nil }

type busyTimeoutRows struct {
	value int64
	done  bool
}

func (r *busyTimeoutRows) Columns() []string { return []string{"timeout"} }
func (r *busyTimeoutRows) Close() error      { return nil }
func (r *busyTimeoutRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	dest[0] = r.value
	return nil
}
