package primitives

import (
	"agent-nexus-core/internal/resourceaccess"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

type denialTarget struct{ kind, id string }

type denialSnapshot struct {
	mutation         bool
	pointOnce        sync.Once
	pointIndex       map[denialTarget]struct{}
	pointToken       int64
	pointSeriesCount int
	epoch            int64
	epochTable       string
	refs             string
	refOnce          sync.Once
	refIndex         map[string]struct{}
	rows             string
	indexOnce        sync.Once
	targetIndex      map[denialTarget]struct{}
}

// deniesAtom uses the canonical exact-atom and identity indexes. Preparing
// aliases and bare ID keys once avoids scanning the denial closure per page.
func (s *denialSnapshot) deniesAtom(key string) bool {
	if s == nil {
		return false
	}
	s.refOnce.Do(func() {
		s.prepareTargetIndex()
		s.refIndex = map[string]struct{}{}
		for target := range s.targetIndex {
			if strings.HasPrefix(target.kind, "filter/") || target.kind == "brief_loop" || target.kind == "plan" || target.kind == "work_evidence_record" || target.kind == "work_evidence_alias" || target.kind == resourceaccess.DocumentHistoryKind {
				continue
			}
			s.refIndex[resourceaccess.AtomKey(target.id)] = struct{}{}
			if target.kind != "external_key" {
				s.refIndex[resourceaccess.AtomKey(target.kind+":"+target.id)] = struct{}{}
			}
		}
		var refs [][2]string
		if json.Unmarshal([]byte(s.refs), &refs) == nil {
			for _, ref := range refs {
				s.refIndex[resourceaccess.AtomKey(ref[0]+":"+ref[1])] = struct{}{}
				if ref[0] == "document" {
					s.refIndex[resourceaccess.AtomKey("doc:"+ref[1])] = struct{}{}
				}
				if strings.HasPrefix(ref[1], "http://") || strings.HasPrefix(ref[1], "https://") {
					s.refIndex[resourceaccess.AtomKey(ref[1])] = struct{}{}
				}
			}
		}
	})
	_, denied := s.refIndex[key]
	return denied
}

// Index once when a closure is cached, rather than decoding every denied
// resource again on each warm stream poll.
func (s *denialSnapshot) prepareTargetIndex() {
	s.indexOnce.Do(func() {
		if !utf8.ValidString(s.rows) || !strings.HasPrefix(strings.TrimSpace(s.rows), "[") {
			return
		}
		decoder := json.NewDecoder(strings.NewReader(s.rows))
		decoder.UseNumber()
		var rows [][]any
		if decoder.Decode(&rows) != nil || decoder.Decode(new(any)) != io.EOF {
			return
		}
		index := make(map[denialTarget]struct{}, len(rows))
		for _, row := range rows {
			if len(row) != 2 {
				return
			}
			if row[0] == nil || row[1] == nil {
				continue
			}
			kind, ok := row[0].(string)
			if !ok {
				return
			}
			var id string
			switch value := row[1].(type) {
			case string:
				id = value
			case json.Number:
				numeric, err := value.Int64()
				if err != nil {
					return
				}
				id = strconv.FormatInt(numeric, 10)
			default:
				return
			}
			index[denialTarget{kind, id}] = struct{}{}
		}
		s.targetIndex = index
	})
}

func (s *denialSnapshot) denies(kind, id string) bool {
	if s == nil || id == "" {
		return false
	}
	s.prepareTargetIndex()
	if s.targetIndex == nil {
		return true
	}
	_, denied := s.targetIndex[denialTarget{kind, id}]
	return denied
}

// deniesWakeup applies the cached closure to one receipt. A wakeup created
// after the closure was captured still inherits its thread and trigger event.
func (s *denialSnapshot) deniesWakeup(wakeupID, threadID, triggerEventID string) bool {
	if s == nil {
		return false
	}
	s.prepareTargetIndex()
	if s.targetIndex == nil {
		return true
	}
	if s.denies("wakeup", wakeupID) || s.denies("thread", threadID) || s.denies("event", triggerEventID) {
		return true
	}
	return false
}

type denialRequestState struct {
	sync.Mutex
	snapshot *denialSnapshot
}
type denialRequestKey struct{}

type denialCacheKey struct {
	db         *sql.DB
	scope      AccessScope
	epoch      int64
	epochTable string
}

// Hold at most 32 immutable closures and 8 MiB of encoded rows, evicting oldest
// insertions, and only the newest epoch per database/principal/epoch table.
// Active requests retain their own immutable snapshots. DB identity separates
// workspaces even when their epochs and principal names happen to coincide.
var readDenials = struct {
	sync.Mutex
	keys  []denialCacheKey
	rows  map[denialCacheKey]*denialSnapshot
	bytes int
}{rows: make(map[denialCacheKey]*denialSnapshot)}

func cachedReadDenial(db *sql.DB, scope AccessScope) *denialSnapshot {
	return cachedReadDenialForEpoch(db, scope, "resource_access_epoch")
}
func cachedReadDenialForEpoch(db *sql.DB, scope AccessScope, epochTable string) *denialSnapshot {
	readDenials.Lock()
	defer readDenials.Unlock()
	var newest *denialSnapshot
	// Captures can finish out of order; insertion order is not epoch order.
	for _, key := range readDenials.keys {
		if key.db == db && key.scope == scope && key.epochTable == epochTable && (newest == nil || key.epoch > newest.epoch) {
			newest = readDenials.rows[key]
		}
	}
	return newest
}

func rememberReadDenial(db *sql.DB, scope AccessScope, snapshot *denialSnapshot) {
	if db == nil || snapshot == nil || len(snapshot.rows)+len(snapshot.refs) > 8<<20 {
		return
	}
	snapshot.prepareTargetIndex()
	epochTable := snapshot.epochTable
	if epochTable == "" {
		epochTable = "resource_access_epoch"
	}
	key := denialCacheKey{db, scope, snapshot.epoch, epochTable}
	readDenials.Lock()
	defer readDenials.Unlock()
	for i, old := range readDenials.keys {
		if old.db != db || old.scope != scope || old.epochTable != epochTable {
			continue
		}
		// A delayed capture must neither replace nor retain an obsolete epoch.
		if old.epoch >= snapshot.epoch {
			return
		}
		previous := readDenials.rows[old]
		readDenials.bytes -= len(previous.rows) + len(previous.refs)
		delete(readDenials.rows, old)
		readDenials.keys = slices.Delete(readDenials.keys, i, i+1)
		break
	}
	for len(readDenials.keys) >= 32 || readDenials.bytes+len(snapshot.rows)+len(snapshot.refs) > 8<<20 {
		old := readDenials.keys[0]
		readDenials.keys = readDenials.keys[1:]
		readDenials.bytes -= len(readDenials.rows[old].rows) + len(readDenials.rows[old].refs)
		delete(readDenials.rows, old)
	}
	readDenials.keys = append(readDenials.keys, key)
	readDenials.rows[key] = snapshot
	readDenials.bytes += len(snapshot.rows) + len(snapshot.refs)
}

// WithRequestAccessScope caches the denial closure for a regular read request.
// Every consuming statement validates the authorization epoch IN its own SQL
// snapshot and falls back to the canonical graph if any ownership write occurred.
// Business transactions evaluate the canonical graph directly. The visit ledger
// can reuse the read closure only after checking its epoch in the transaction.
// An inbox poll may use a fresh request boundary; never share request state
// across ticks. Shared snapshots are bounded and isolated by DB, scope and epoch.
func WithRequestAccessScope(ctx context.Context, scope AccessScope) context.Context {
	return withRequestAccessEpoch(ctx, scope, "resource_access_epoch")
}

// epochTable is an internal schema identifier, never caller input.
func withRequestAccessEpoch(ctx context.Context, scope AccessScope, epochTable string) context.Context {
	ctx = WithAccessScope(ctx, scope)
	state := &denialRequestState{}
	ctx = context.WithValue(ctx, denialRequestKey{}, state)
	p, _ := resourceaccess.PolicyFrom(ctx)
	p.ReadOnDB = func(c context.Context, db resourceaccess.QueryRower, query string, args []any) (string, []any) {
		if pin, ok := c.Value(pinnedDenialKey{}).(pinnedDenial); ok && pin.db == db && pin.scope == scope && epochTable == "resource_access_epoch" && resourceaccess.AnonymousSQLParameters(query) {
			scoped := scopeReadSnapshot(c, query, pin.snapshot, true)
			value := any(pin.snapshot.rows)
			if strings.HasPrefix(scoped, "WITH RECURSIVE _anx_point_snapshot(") {
				value = pin.snapshot.pointToken
			}
			bound := append([]any{value}, args...)
			if accessCTEs(scope, query) == "" {
				return scopeRead(c, query), args
			}
			return scoped, bound
		}
		// Identity/service SQL with no resource relation needs no closure.
		if accessCTEs(scope, query) == "" {
			return scopeRead(c, query), args
		}
		state.Lock()
		if state.snapshot == nil {
			// Validate a possible cache hit and capture a stale or missing closure
			// in one statement snapshot. CASE skips graph construction on a hit.
			// Transactions continue to evaluate their current graph directly.
			raw, _ := db.(*sql.DB)
			var cached *denialSnapshot
			if raw != nil {
				cached = cachedReadDenialForEpoch(raw, scope, epochTable)
			}
			candidate := &denialSnapshot{epochTable: epochTable}
			var rows, refs sql.NullString
			sql := `WITH RECURSIVE ` + ownershipClosure("_anx_denied", deniedRootSQL(scope), false) + ` SELECT (SELECT version FROM main.resource_access_epoch WHERE singleton=1),COALESCE((SELECT json_group_array(json_array(kind,id)) FROM _anx_denied),'[]')`
			var captureArgs []any
			if cached != nil {
				current := `(SELECT version FROM _anx_snapshot_epoch)`
				roots := `SELECT * FROM (` + deniedRootSQL(scope) + `) WHERE COALESCE(` + current + `,-1)<>?`
				sql = `WITH RECURSIVE _anx_snapshot_epoch(version) AS MATERIALIZED (SELECT version FROM main.resource_access_epoch WHERE singleton=1), ` + ownershipClosure("_anx_denied", roots, false) + ` SELECT ` + current + `,CASE WHEN ` + current + `=? THEN NULL ELSE COALESCE((SELECT json_group_array(json_array(kind,id)) FROM _anx_denied),'[]') END`
				captureArgs = []any{cached.epoch, cached.epoch}
			}
			sql = strings.ReplaceAll(sql, "main.resource_access_epoch", "main."+epochTable)
			dest := []any{&candidate.epoch, &rows}
			if epochTable == "receipt_access_epoch" {
				refSQL := `COALESCE((SELECT json_group_array(json_array(kind,ref)) FROM (WITH ` + ownershipRefs("_anx_receipt_refs", "_anx_denied") + ` SELECT kind,ref FROM _anx_receipt_refs WHERE ref<>id)),'[]')`
				if cached != nil {
					refSQL = `CASE WHEN (SELECT version FROM _anx_snapshot_epoch)=? THEN NULL ELSE ` + refSQL + ` END`
					captureArgs = append(captureArgs, cached.epoch)
				}
				sql += "," + refSQL
				dest = append(dest, &refs)
			}
			if err := db.QueryRowContext(c, sql, captureArgs...).Scan(dest...); err == nil {
				if cached != nil && candidate.epoch == cached.epoch {
					state.snapshot = cached
				} else if rows.Valid && (epochTable != "receipt_access_epoch" || refs.Valid) {
					candidate.rows, candidate.refs = rows.String, refs.String
					state.snapshot = candidate
					rememberReadDenial(raw, scope, candidate)
				}
			}
		}
		state.Unlock()
		if snapshot := denialSnapshotFrom(c); snapshot != nil && resourceaccess.AnonymousSQLParameters(query) {
			// The snapshot is data, never SQL syntax. Its one binding precedes
			// the statement's anonymous bindings, preserving their order.
			bound := make([]any, 0, len(args)+1)
			bound = append(bound, snapshot.rows)
			bound = append(bound, args...)
			return scopeReadWithSnapshot(c, query, snapshot), bound
		}
		return scopeRead(c, query), args
	}
	return resourceaccess.WithPolicy(ctx, p)
}
func denialSnapshotFrom(ctx context.Context) *denialSnapshot {
	state, ok := ctx.Value(denialRequestKey{}).(*denialRequestState)
	if !ok {
		return nil
	}
	state.Lock()
	defer state.Unlock()
	return state.snapshot
}

// WithReadTickSnapshot starts a new epoch-validated read boundary for one poll.
// Recreate it each tick; never attach this state to the long-lived stream.
// Each consuming SQL statement still checks its own epoch and falls back to
// the canonical graph after revocation. Transactional writes stay canonical.
func WithReadTickSnapshot(ctx context.Context) context.Context {
	if scope, ok := accessScopeFrom(ctx); ok {
		return WithRequestAccessScope(ctx, scope)
	}
	return ctx
}
