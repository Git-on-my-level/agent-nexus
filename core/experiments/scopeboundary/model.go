// Package scopeboundary is a non-merge executable sketch, not a production store.
package scopeboundary

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync/atomic"

	"database/sql/driver"
	"modernc.org/sqlite"
)

var Visits atomic.Int64

func init() {
	sqlite.MustRegisterScalarFunction("scope_probe", 1, func(_ *sqlite.FunctionContext, _ []driver.Value) (driver.Value, error) {
		Visits.Add(1)
		return int64(1), nil
	})
}

var ErrUpdating = errors.New("scope updating; totals unavailable")
var ErrBudget = errors.New("request exceeds scope/stream bound")
var ErrDerivation = errors.New("cross-scope derivation requires publication")

type Model struct{ DB *sql.DB }

func Open(path string) (*Model, error) {
	db, e := sql.Open("sqlite", path)
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	_, e = db.Exec(`PRAGMA journal_mode=WAL;
 CREATE TABLE IF NOT EXISTS scopes(id INTEGER PRIMARY KEY,state TEXT NOT NULL DEFAULT 'active',generation INTEGER NOT NULL DEFAULT 1);
 CREATE INDEX IF NOT EXISTS scope_state ON scopes(state,id);
 CREATE TABLE IF NOT EXISTS grants(principal TEXT,scope INTEGER,PRIMARY KEY(principal,scope)) WITHOUT ROWID;
 CREATE TABLE IF NOT EXISTS resources(id INTEGER PRIMARY KEY,scope INTEGER,parent INTEGER,title TEXT,archived INTEGER DEFAULT 0);
 CREATE INDEX IF NOT EXISTS resource_scope ON resources(scope,id);
 CREATE TABLE IF NOT EXISTS feed(scope INTEGER,family TEXT,audience TEXT,seq INTEGER,id INTEGER,parent INTEGER,PRIMARY KEY(scope,family,audience,seq,id)) WITHOUT ROWID;
 CREATE TABLE IF NOT EXISTS counters(scope INTEGER PRIMARY KEY,n INTEGER);
 CREATE TABLE IF NOT EXISTS jobs(scope INTEGER PRIMARY KEY,cursor INTEGER DEFAULT 0);
 CREATE TABLE IF NOT EXISTS changes(scope INTEGER,family TEXT,audience TEXT,seq INTEGER,id TEXT,PRIMARY KEY(scope,family,audience,seq)) WITHOUT ROWID;
 CREATE TABLE IF NOT EXISTS aliases(scope INTEGER,name TEXT,rid TEXT,retired INTEGER DEFAULT 0,PRIMARY KEY(scope,name)) WITHOUT ROWID;
 CREATE TABLE IF NOT EXISTS replays(principal TEXT,token TEXT,result TEXT,request TEXT,PRIMARY KEY(principal,token)) WITHOUT ROWID;
 CREATE TABLE IF NOT EXISTS ordering(scope INTEGER,col TEXT,rank INTEGER,id INTEGER,PRIMARY KEY(scope,col,rank,id)) WITHOUT ROWID;
 CREATE TABLE IF NOT EXISTS comments(id INTEGER PRIMARY KEY,thread INTEGER,body TEXT);
 CREATE INDEX IF NOT EXISTS comments_thread ON comments(thread,id);
 CREATE TABLE IF NOT EXISTS postings(resource INTEGER,term TEXT,PRIMARY KEY(resource,term)) WITHOUT ROWID;
 CREATE TABLE IF NOT EXISTS projections(scope INTEGER,id TEXT,body TEXT,PRIMARY KEY(scope,id)) WITHOUT ROWID;`)
	if e != nil {
		db.Close()
		return nil, e
	}
	return &Model{db}, nil
}
func opaque() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}

// IDs never depend on aliases in other scopes. A same-scope conflict is visible authority.
func (m *Model) Create(principal, token, alias string, scope int) (string, error) {
	tx, e := m.DB.Begin()
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	var n int
	if e = tx.QueryRow(`SELECT count(*) FROM grants WHERE principal=? AND scope=?`, principal, scope).Scan(&n); e != nil || n != 1 {
		return "", errors.New("denied")
	}
	var result, request string
	expected := fmt.Sprintf("%d:%s", scope, alias)
	e = tx.QueryRow(`SELECT result,request FROM replays WHERE principal=? AND token=?`, principal, token).Scan(&result, &request)
	if e == nil {
		if request != expected {
			return "", errors.New("replay input mismatch")
		}
		return result, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return "", e
	}
	result = opaque()
	_, e = tx.Exec(`INSERT INTO aliases VALUES(?,?,?,0)`, scope, alias, result)
	if e != nil {
		return "", errors.New("scope alias unavailable")
	}
	_, e = tx.Exec(`INSERT INTO replays VALUES(?,?,?,?)`, principal, token, result, expected)
	if e != nil {
		return "", e
	}
	return result, tx.Commit()
}

// No state filter in this range. Unavailable granted scopes remain explicit directory slots.
func (m *Model) Directory(principal string, after int) ([]int, error) {
	rows, e := m.DB.Query(`SELECT scope FROM grants WHERE principal=? AND scope>? AND scope_probe(scope) ORDER BY scope LIMIT 65`, principal, after)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var id int
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

type Stream struct {
	Scope            int
	Family, Audience string
	After            int
}

func (m *Model) PollPage(principal string, streams []Stream, p int) ([]string, []Stream, error) {
	if len(streams) > 256 || p < 1 || p > 100 {
		return nil, nil, ErrBudget
	}
	scopes := map[int]int{}
	seen := map[Stream]bool{}
	for _, s := range streams {
		key := s
		key.After = 0
		if seen[key] {
			return nil, nil, ErrBudget
		}
		seen[key] = true
		scopes[s.Scope]++
		if scopes[s.Scope] > 4 {
			return nil, nil, ErrBudget
		}
	}
	if len(scopes) > 64 {
		return nil, nil, ErrBudget
	}
	tx, e := m.DB.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return nil, nil, e
	}
	defer tx.Rollback()
	for id := range scopes {
		var state string
		e = tx.QueryRow(`SELECT s.state FROM scopes s JOIN grants g ON g.scope=s.id WHERE g.principal=? AND s.id=?`, principal, id).Scan(&state)
		if e != nil {
			return nil, nil, errors.New("denied")
		}
		if state != "active" {
			return nil, nil, ErrUpdating
		}
	}
	type item struct {
		id          string
		seq, stream int
	}
	var result []item
	for streamIndex, s := range streams { // Prototype supports broadcast and the exact requesting principal only.
		if s.Audience != "all" && s.Audience != "person:"+principal {
			return nil, nil, errors.New("audience denied")
		}
		rows, e := tx.Query(`SELECT id,seq FROM changes WHERE scope=? AND family=? AND audience=? AND seq>? AND scope_probe(seq) ORDER BY seq LIMIT ?`, s.Scope, s.Family, s.Audience, s.After, p+1)
		if e != nil {
			return nil, nil, e
		}
		for rows.Next() {
			var id string
			var seq int
			if e = rows.Scan(&id, &seq); e != nil {
				rows.Close()
				return nil, nil, e
			}
			result = append(result, item{id, seq, streamIndex})
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, nil, e
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].seq == result[j].seq {
			return result[i].stream < result[j].stream
		}
		return result[i].seq < result[j].seq
	})
	if len(result) > p {
		result = result[:p]
	}
	next := append([]Stream(nil), streams...)
	var out []string
	for _, r := range result {
		next[r.stream].After = r.seq
		out = append(out, r.id)
	}
	return out, next, tx.Commit()
}
func (m *Model) Poll(principal string, streams []Stream, p int) ([]string, error) {
	out, _, e := m.PollPage(principal, streams, p)
	return out, e
}

func (m *Model) Count(scope int) (int, error) {
	var state string
	var n int
	e := m.DB.QueryRow(`SELECT s.state,COALESCE(c.n,0) FROM scopes s LEFT JOIN counters c ON c.scope=s.id WHERE s.id=?`, scope).Scan(&state, &n)
	if e != nil {
		return 0, e
	}
	if state != "active" {
		return 0, ErrUpdating
	}
	return n, nil

}
func (m *Model) Archive(parent, scope int) error {
	tx, e := m.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`UPDATE scopes SET state='transitioning' WHERE id=? AND state='active'`, scope); e != nil {
		return e
	}
	if _, e = tx.Exec(`UPDATE resources SET archived=1 WHERE id=? AND scope=?`, parent, scope); e != nil {
		return e
	}
	if _, e = tx.Exec(`INSERT INTO jobs(scope) VALUES(?)`, scope); e != nil {
		return e
	}
	return tx.Commit()
}

// Same-scope, one-level fixture; production must validate bounded ancestor depth.
func (m *Model) Step(scope, j int) (int, bool, error) {
	tx, e := m.DB.Begin()
	if e != nil {
		return 0, false, e
	}
	defer tx.Rollback()
	var cursor int
	if e = tx.QueryRow(`SELECT cursor FROM jobs WHERE scope=?`, scope).Scan(&cursor); e != nil {
		return 0, false, e
	}
	rows, e := tx.Query(`SELECT id,parent,archived FROM resources WHERE scope=? AND id>? ORDER BY id LIMIT ?`, scope, cursor, j)
	if e != nil {
		return 0, false, e
	}
	type row struct{ id, parent, archived int }
	var batch []row
	for rows.Next() {
		var r row
		if e = rows.Scan(&r.id, &r.parent, &r.archived); e != nil {
			rows.Close()
			return 0, false, e
		}
		batch = append(batch, r)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return 0, false, e
	}
	for _, r := range batch {
		hidden := r.archived != 0
		if r.parent != 0 {
			var a int
			if e = tx.QueryRow(`SELECT archived FROM resources WHERE id=? AND scope=?`, r.parent, scope).Scan(&a); e != nil {
				return 0, false, e
			}
			hidden = hidden || a != 0
		}
		if hidden {
			result, e := tx.Exec(`DELETE FROM feed WHERE scope=? AND family='work' AND audience='all' AND seq=? AND id=?`, scope, r.id, r.id)
			if e != nil {
				return 0, false, e
			}
			n, _ := result.RowsAffected()
			if _, e = tx.Exec(`UPDATE counters SET n=n-? WHERE scope=?`, n, scope); e != nil {
				return 0, false, e
			}
		}
		cursor = r.id
	}
	if len(batch) == 0 {
		_, e = tx.Exec(`DELETE FROM jobs WHERE scope=?`, scope)
		if e == nil {
			_, e = tx.Exec(`UPDATE scopes SET state='active',generation=generation+1 WHERE id=?`, scope)
		}
	} else {
		_, e = tx.Exec(`UPDATE jobs SET cursor=? WHERE scope=?`, cursor, scope)
	}
	if e != nil {
		return 0, false, e
	}
	return len(batch), len(batch) == 0, tx.Commit()
}
func (m *Model) Page(scope, p int) ([]int, error) {
	tx, e := m.DB.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	var state string
	if e = tx.QueryRow(`SELECT state FROM scopes WHERE id=?`, scope).Scan(&state); e != nil {
		return nil, e
	}
	if state != "active" {
		return nil, ErrUpdating
	}
	rows, e := tx.Query(`SELECT id FROM feed WHERE scope=? AND family='work' AND audience='all' AND scope_probe(id) ORDER BY seq,id LIMIT ?`, scope, p)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var id int
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// A computation's output destination is fixed even when its principal owns both scopes.
// Value is an opaque handle; it contains no plaintext and offers no string/byte conversion.
type Computation struct {
	title    func(int) (Value, error)
	constant func(string) Value
	persist  func(int, string, Value) error
}
type Value struct {
	token  string
	origin *Computation
}

// Compute is trusted dispatcher setup, not an API available inside a business
// computation. Production requires the analyzer to enforce that boundary.
func (m *Model) Compute(principal string, scope int) (*Computation, error) {
	var n int
	e := m.DB.QueryRow(`SELECT count(*) FROM grants WHERE principal=? AND scope=?`, principal, scope).Scan(&n)
	if e != nil || n != 1 {
		return nil, errors.New("denied")
	}
	// Closure captures cannot be traversed by fmt/JSON/reflection on a handle.
	// The capability itself contains neither a DB pointer nor plaintext values.
	values := map[string]string{}
	c := &Computation{}
	c.constant = func(body string) Value {
		v := Value{opaque(), c}
		values[v.token] = body
		return v
	}
	c.title = func(id int) (Value, error) {
		var body string
		e := m.DB.QueryRow(`SELECT title FROM resources WHERE id=? AND scope=?`, id, scope).Scan(&body)
		if e != nil {
			return Value{}, e
		}
		return c.constant(body), nil
	}
	c.persist = func(destination int, id string, v Value) error {
		if destination != scope || v.origin != c {
			return ErrDerivation
		}
		body, ok := values[v.token]
		if !ok {
			return ErrDerivation
		}
		_, e := m.DB.Exec(`INSERT INTO projections VALUES(?,?,?)`, destination, id, body)
		return e
	}
	return c, nil
}
func (c *Computation) Title(id int) (Value, error) { return c.title(id) }
func (c *Computation) Constant(body string) Value  { return c.constant(body) }
func (c *Computation) Persist(destination int, id string, v Value) error {
	return c.persist(destination, id, v)
}
func (m *Model) RankBetween(scope int, col string, after int64) (int64, error) {
	if after < 0 || after > math.MaxInt64-1024 {
		return 0, errors.New("rank_gap_exhausted")
	}
	var next int64
	e := m.DB.QueryRow(`SELECT rank FROM ordering WHERE scope=? AND col=? AND rank>? AND scope_probe(id) ORDER BY rank LIMIT 1`, scope, col, after).Scan(&next)
	if e == sql.ErrNoRows {
		return after + 1024, nil
	}
	if e != nil {
		return 0, e
	}
	if next-after < 2 {
		return 0, errors.New("rank_gap_exhausted")
	}
	return after + (next-after)/2, nil
}
func (m *Model) AppendComment(id, thread int, body string) error {
	if len(body) > SearchBytes || len(strings.Fields(body)) > 4096 {
		return ErrBudget
	}
	tx, e := m.DB.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	_, e = tx.Exec(`INSERT INTO comments VALUES(?,?,?)`, id, thread, body)
	if e != nil {
		return e
	}
	for _, term := range strings.Fields(body) {
		_, e = tx.Exec(`INSERT OR IGNORE INTO postings VALUES(?,?)`, id, term)
		if e != nil {
			return e
		}
	}
	return tx.Commit()
}

// Prefix coverage is defined in bytes; an oversized candidate is fully verified within its advertised searchable prefix.
const SearchBytes = 64 * 1024

type Candidate struct {
	ID      int
	Version int
	Body    string
}
type SearchCursor struct{ Index, Generation int }

func Search(candidates []Candidate, phrase string, cursor SearchCursor, generation int, budget int) ([]int, SearchCursor, error) {
	if cursor.Generation != generation {
		return nil, cursor, errors.New("restart search generation")
	}
	var out []int
	used := 0
	for cursor.Index < len(candidates) {
		c := candidates[cursor.Index]
		b := c.Body
		if len(b) > SearchBytes {
			b = b[:SearchBytes]
		}
		if used+len(b) > budget {
			break
		}
		used += len(b)
		if strings.Contains(b, phrase) {
			out = append(out, c.ID)
		}
		cursor.Index++
	}
	if used == 0 && cursor.Index < len(candidates) && budget < SearchBytes {
		return nil, cursor, fmt.Errorf("verification budget below searchable resource cap")
	}
	return out, cursor, nil
}
