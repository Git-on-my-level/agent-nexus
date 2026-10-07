package primitives

import (
	"agent-nexus-core/internal/resourceaccess"
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
)

type denialTarget struct{ kind, id string }

type denialSnapshot struct {
	epoch       int64
	epochTable  string
	refs        string
	refOnce     sync.Once
	refIndex    map[string]struct{}
	rows        string
	indexOnce   sync.Once
	targetIndex map[denialTarget]struct{}
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
			if strings.HasPrefix(target.kind, "filter/") || target.kind == "plan" || target.kind == "work_evidence_record" || target.kind == "work_evidence_alias" {
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
		var rows [][2]string
		if json.Unmarshal([]byte(s.rows), &rows) != nil {
			return
		}
		index := make(map[denialTarget]struct{}, len(rows))
		for _, row := range rows {
			index[denialTarget{row[0], row[1]}] = struct{}{}
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
// insertions. DB identity separates
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
	if readDenials.rows[key] != nil {
		return
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
// Business transactions, visit writes and streams evaluate the canonical graph
// directly. Shared read snapshots are bounded and isolated by DB, scope and epoch.
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
			sql := `WITH RECURSIVE ` + ownershipClosure("_anx_denied", deniedRootSQL(scope), false) + ` SELECT (SELECT version FROM main.resource_access_epoch WHERE singleton=1),COALESCE((SELECT json_group_array(json_array(kind,id)) FROM _anx_denied),'[]')`
			var captureArgs []any
			if cached != nil {
				current := `(SELECT version FROM _anx_snapshot_epoch)`
				roots := `SELECT * FROM (` + deniedRootSQL(scope) + `) WHERE COALESCE(` + current + `,-1)<>?`
				sql = `WITH RECURSIVE _anx_snapshot_epoch(version) AS MATERIALIZED (SELECT version FROM main.resource_access_epoch WHERE singleton=1), ` + ownershipClosure("_anx_denied", roots, false) + ` SELECT ` + current + `,CASE WHEN ` + current + `=? THEN ? ELSE COALESCE((SELECT json_group_array(json_array(kind,id)) FROM _anx_denied),'[]') END`
				captureArgs = []any{cached.epoch, cached.epoch, cached.rows}
			}
			sql = strings.ReplaceAll(sql, "main.resource_access_epoch", "main."+epochTable)
			dest := []any{&candidate.epoch, &candidate.rows}
			if epochTable == "receipt_access_epoch" {
				refSQL := `COALESCE((SELECT json_group_array(json_array(kind,ref)) FROM (WITH ` + ownershipRefs("_anx_receipt_refs", "_anx_denied") + ` SELECT kind,ref FROM _anx_receipt_refs WHERE ref<>id)),'[]')`
				if cached != nil {
					refSQL = `CASE WHEN (SELECT version FROM _anx_snapshot_epoch)=? THEN ? ELSE ` + refSQL + ` END`
					captureArgs = append(captureArgs, cached.epoch, cached.refs)
				}
				sql += "," + refSQL
				dest = append(dest, &candidate.refs)
			}
			if err := db.QueryRowContext(c, sql, captureArgs...).Scan(dest...); err == nil {
				if cached != nil && candidate.epoch == cached.epoch {
					state.snapshot = cached
				} else {
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
