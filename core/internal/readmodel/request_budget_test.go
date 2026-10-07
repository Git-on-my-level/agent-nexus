package readmodel_test

import (
	"agent-nexus-core/internal/readmodel"
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"agent-nexus-core/internal/scopedrepo"
)

// Test-local instrumentation counts every submitted statement and returned row,
// including authority preparation inside ReadFeed. It does not stand in for
// SCA-661's full production HTTP + legacy-denial preparation acceptance harness.
type requestCapture struct {
	mu        sync.Mutex
	enabled   bool
	sql, rows int
}

func (c *requestCapture) statement() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.enabled {
		c.sql++
	}
}
func (c *requestCapture) row() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.enabled {
		c.rows++
	}
}
func (c *requestCapture) start() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sql = 0
	c.rows = 0
	c.enabled = true
}
func (c *requestCapture) stop() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enabled = false
	return c.sql, c.rows
}

type requestDriver struct {
	base    driver.Driver
	capture *requestCapture
}

func (d requestDriver) Open(name string) (driver.Conn, error) {
	c, err := d.base.Open(name)
	if err != nil {
		return nil, err
	}
	return requestConn{Conn: c, capture: d.capture}, nil
}

type requestConn struct {
	driver.Conn
	capture *requestCapture
}

func (c requestConn) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, o)
}
func (c requestConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	c.capture.statement()
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, q, args)
}
func (c requestConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	c.capture.statement()
	rows, err := c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
	if err != nil {
		return nil, err
	}
	return requestRows{Rows: rows, capture: c.capture}, nil
}
func (c requestConn) Prepare(q string) (driver.Stmt, error) {
	s, err := c.Conn.Prepare(q)
	if err != nil {
		return nil, err
	}
	return requestStmt{Stmt: s, capture: c.capture}, nil
}

type requestStmt struct {
	driver.Stmt
	capture *requestCapture
}

func (s requestStmt) Exec(args []driver.Value) (driver.Result, error) {
	s.capture.statement()
	return s.Stmt.Exec(args)
}
func (s requestStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.capture.statement()
	r, err := s.Stmt.Query(args)
	if err != nil {
		return nil, err
	}
	return requestRows{Rows: r, capture: s.capture}, nil
}

type requestRows struct {
	driver.Rows
	capture *requestCapture
}

func (r requestRows) Next(dest []driver.Value) error {
	err := r.Rows.Next(dest)
	if err == nil {
		r.capture.row()
	}
	return err
}

var requestDriverID atomic.Int64

func countedFixture(t *testing.T) (*sql.DB, *requestCapture) { return countedDSN(t, ":memory:") }

func countedDSN(t *testing.T, dsn string) (*sql.DB, *requestCapture) {
	t.Helper()
	base := openFixture(t, ":memory:")
	d := base.Driver()
	base.Close()
	c := &requestCapture{}
	name := fmt.Sprintf("readmodel-budget-%d", requestDriverID.Add(1))
	sql.Register(name, requestDriver{base: d, capture: c})
	db, err := sql.Open(name, dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	return db, c
}

func TestMeasuredDependencyRequestAndBatchProposalBudgets(t *testing.T) {
	db, c := countedFixture(t)
	_, repo, request, streams := adapterFixtureOnDB(t, db, 64, 4)
	ctx := context.Background()
	// Populate all 256 exact streams with 101 distinct real registry identities,
	// plus wrong-audience rows. This is the maximum candidate/row return probe.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i, s := range streams {
		for n := 3; n < 101; n++ {
			opaque := fmt.Sprintf("extra-%d-%d", i, n)
			if _, err = tx.Exec(`INSERT INTO scope_resources VALUES(?,'card',?,?,1)`, s.Scope, opaque, opaque); err != nil {
				t.Fatal(err)
			}
			var rid int64
			if err = tx.QueryRow(`SELECT rid FROM scope_resource_rids WHERE scope_id=? AND resource_id=?`, s.Scope, opaque).Scan(&rid); err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(readmodel.InsertFeed, s.Scope, 1, s.Family, s.Audience, n*256+i, rid, 1); err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(readmodel.InsertPayload, s.Scope, 1, s.Family, s.Audience, rid, 1, `{}`); err != nil {
				t.Fatal(err)
			}
		}
		for _, b := range []string{"two", "three", "four"} {
			if _, err = tx.Exec(readmodel.IncrementCounter, s.Scope, 1, s.Family, s.Audience, b, 101); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err = tx.Exec(`WITH RECURSIVE n(x) AS(SELECT 1 UNION ALL SELECT x+1 FROM n WHERE x<64000) INSERT INTO scope_feed SELECT 'scope-00',1,'family-0','wrong',x,100000+x,1 FROM n`); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	c.start()
	start := time.Now()
	err = repo.ReadFeed(ctx, request, streams, func(r scopedrepo.FeedReader) error {
		a := feedAdapter{reader: r}
		p, err := readmodel.Read(ctx, a, codec(t), 100, "")
		if err != nil {
			return err
		}
		if len(p.Items) != 100 {
			t.Fatal(len(p.Items))
		}
		_, err = readmodel.Count(ctx, a, []string{"total", "two", "three", "four"})
		return err
	})
	elapsed := time.Since(start)
	statements, rows := c.stop()
	if err != nil {
		t.Fatal(err)
	}
	if statements != 643 || rows != 27365 {
		t.Fatal("incomplete dependency accounting", statements, rows)
	}
	t.Logf("measured published dependency: SQL=%d rows=%d elapsed=%s; 100 SQL/1024 rows FAIL; legacy preparation not yet included", statements, rows, elapsed)

	// Execute proposed batches against the same fixture. This is SQL-shape
	// evidence for A, not an authorized repository or an enablement receipt.
	authorized := make([]AuthorizedStream, len(streams))
	for i, s := range streams {
		authorized[i] = AuthorizedStream{Stream: s, Generation: 1}
	}
	authorityQ, authorityArgs, err := BatchAuthorityProposal(request.Principal, request.ScopeIDs)
	if err != nil {
		t.Fatal(err)
	}
	bindingQ, bindingArgs, err := BatchBindingProposal(request.Principal, authorized)
	if err != nil {
		t.Fatal(err)
	}
	candidateQ, candidateArgs, err := BatchCandidatesProposal(authorized, 100)
	if err != nil {
		t.Fatal(err)
	}
	counterQ, counterArgs, err := AggregateBucketsProposal(authorized, []string{"total", "two", "three", "four"})
	if err != nil {
		t.Fatal(err)
	}
	c.start()
	start = time.Now()
	queries := []struct {
		q    string
		args []any
		want int
	}{{authorityQ, authorityArgs, 64}, {`SELECT version FROM resource_access_epoch WHERE singleton=1`, nil, 1}, {bindingQ, bindingArgs, 256}, {candidateQ, candidateArgs, 101}, {counterQ, counterArgs, 4}}
	for _, probe := range queries {
		result, err := db.QueryContext(ctx, probe.q, probe.args...)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for result.Next() {
			n++
		}
		err = result.Err()
		result.Close()
		if err != nil || n != probe.want {
			t.Fatal(n, probe.want, err)
		}
	}
	elapsed = time.Since(start)
	statements, rows = c.stop()
	if statements != 5 || rows != 426 {
		t.Fatal(statements, rows)
	}
	t.Logf("measured SQL proposals before hydration+legacy preparation: SQL=%d rows=%d elapsed=%s; not production acceptance", statements, rows, elapsed)
	// Hydration is one <=100-row batch: proposed repository subtotal <=6/526.
	// A must still include directory and denial preparation in the real request.
}
