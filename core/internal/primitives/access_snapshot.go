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
type candidateTextTarget struct{ mode, value string }

type denialSnapshot struct {
	candidates         bool
	candidateChanges   int64
	candidateAllPublic bool
	inventory          bool
	candidateMu        sync.Mutex
	candidateIndex     map[denialTarget]bool
	candidateComplete  map[denialTarget]struct{}
	candidateTextIndex map[candidateTextTarget]bool
	candidateBytes     int
	mutation           bool
	pointOnce          sync.Once
	pointIndex         map[denialTarget]struct{}
	pointToken         int64
	pointSeriesCount   int
	epoch              int64
	epochTable         string
	refs               string
	refOnce            sync.Once
	refIndex           map[string]struct{}
	rows               string
	indexOnce          sync.Once
	targetIndex        map[denialTarget]struct{}
}

// deniesAtom uses the canonical exact-atom and identity indexes. Preparing
// aliases and bare ID keys once avoids scanning the denial closure per page.
func (s *denialSnapshot) deniesAtom(key string) bool {
	if s == nil {
		return false
	}
	if s.candidates {
		return true
	} // A lazy request marker is not a denial proof.
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
	snapshot          *denialSnapshot
	candidatePrepared bool
	candidateMode     bool
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

// The exact-edge ledger each pool's last denial capture observed, falling back
// to the process's latest observation for a new pool (pools of one workspace
// share its selection). A capture built for one ledger confirms it in the same
// statement, so a stale entry costs one recomputation, never a closure from
// the inactive ledger.
var accessLedgers = struct {
	sync.Mutex
	keys    []*sql.DB
	ledgers map[*sql.DB]resourceaccess.Ledger
	latest  resourceaccess.Ledger
}{ledgers: make(map[*sql.DB]resourceaccess.Ledger)}

func predictedAccessLedger(db *sql.DB) resourceaccess.Ledger {
	if db == nil {
		return resourceaccess.SelectedLedger
	}
	accessLedgers.Lock()
	defer accessLedgers.Unlock()
	if ledger, ok := accessLedgers.ledgers[db]; ok {
		return ledger
	}
	return accessLedgers.latest
}

func rememberAccessLedger(db *sql.DB, ledger resourceaccess.Ledger) {
	if db == nil {
		return
	}
	accessLedgers.Lock()
	defer accessLedgers.Unlock()
	if _, ok := accessLedgers.ledgers[db]; !ok {
		if len(accessLedgers.keys) >= 32 {
			delete(accessLedgers.ledgers, accessLedgers.keys[0])
			accessLedgers.keys = slices.Delete(accessLedgers.keys, 0, 1)
		}
		accessLedgers.keys = append(accessLedgers.keys, db)
	}
	accessLedgers.ledgers[db] = ledger
	accessLedgers.latest = ledger
}

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

// WithRequestAccessScope admits ordinary reads through indexed candidate
// ancestry in each statement's current SQLite snapshot. It does not capture a
// workspace denial closure before page admission. Business transactions and
// arbitrary legacy projections retain their complete canonical policy.
// Never share the request state across independent stream ticks.
func WithRequestAccessScope(ctx context.Context, scope AccessScope) context.Context {
	return withRequestAccessPolicy(ctx, scope, "resource_access_epoch", true)
}

// epochTable is an internal schema identifier, never caller input.
func withRequestAccessEpoch(ctx context.Context, scope AccessScope, epochTable string) context.Context {
	return withRequestAccessPolicy(ctx, scope, epochTable, false)
}

func withRequestAccessPolicy(ctx context.Context, scope AccessScope, epochTable string, candidates bool) context.Context {
	ctx = WithAccessScope(ctx, scope)
	state := &denialRequestState{candidateMode: candidates}
	ctx = context.WithValue(ctx, denialRequestKey{}, state)
	p, _ := resourceaccess.PolicyFrom(ctx)
	p.ReadOnDB = func(c context.Context, db resourceaccess.QueryRower, query string, args []any) (string, []any) {
		if pin, ok := c.Value(pinnedDenialKey{}).(pinnedDenial); ok && (!candidates || pin.snapshot.inventory) && !pin.snapshot.candidates && pin.db == db && pin.scope == scope && epochTable == "resource_access_epoch" && resourceaccess.AnonymousSQLParameters(query) {
			scoped, needsBinding := scopedReadSnapshotQuery(c, query, pin.snapshot, true)
			if !needsBinding {
				return scoped, args
			}
			value := any(pin.snapshot.rows)
			if strings.HasPrefix(scoped, "WITH RECURSIVE _anx_point_snapshot(") {
				value = pin.snapshot.pointToken
			}
			bound := append([]any{value}, args...)
			return scoped, bound
		}
		if candidates && epochTable == "resource_access_epoch" {
			snapshot := &denialSnapshot{candidates: true}
			if pin, ok := c.Value(pinnedDenialKey{}).(pinnedDenial); ok && pin.snapshot.candidates && pin.db == db && pin.scope == scope {
				snapshot = pin.snapshot
			}
			scoped, usesScope := scopedReadSnapshotQuery(c, query, snapshot, false)
			if !usesScope {
				return scoped, args
			}
			state.Lock()
			if !state.candidatePrepared {
				var epoch int64
				var active bool
				if db.QueryRowContext(c, `WITH `+admissionLedgerCTE(scope)+` SELECT COALESCE((SELECT version FROM main.resource_access_epoch WHERE singleton=1),-1),active FROM _anx_admission_ledger`).Scan(&epoch, &active) == nil && !active && epoch >= 0 {
					// Root absence proves the complete closure is empty without
					// building it. Retain this proof for legacy visit validation.
					state.snapshot = &denialSnapshot{epoch: epoch, epochTable: epochTable, rows: "[]"}
				}
				state.candidatePrepared = true
			}
			empty := state.snapshot
			state.Unlock()
			if visit, _ := c.Value(overviewVisitValidationKey{}).(bool); visit && empty != nil {
				return scopeReadWithSnapshot(c, query, empty), append([]any{empty.rows}, args...)
			}
			return scoped, args
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
			// Probe only the exact-edge ledger last observed. The capture rereads
			// the live selection in its own snapshot; a stale guess is discarded
			// and recomputed with the in-statement selection.
			ledger := predictedAccessLedger(raw)
			for {
				candidate := &denialSnapshot{epochTable: epochTable}
				var rows, refs sql.NullString
				var reader sql.NullInt64
				sql := `WITH RECURSIVE ` + ownershipLedgerClosure("_anx_denied", deniedRootSQL(scope), false, ledger) + ` SELECT (SELECT version FROM main.resource_access_epoch WHERE singleton=1),COALESCE((SELECT json_group_array(json_array(kind,id)) FROM _anx_denied),'[]')`
				var captureArgs []any
				if cached != nil {
					current := `(SELECT version FROM _anx_snapshot_epoch)`
					roots := `SELECT * FROM (` + deniedRootSQL(scope) + `) WHERE COALESCE(` + current + `,-1)<>?`
					sql = `WITH RECURSIVE _anx_snapshot_epoch(version) AS MATERIALIZED (SELECT version FROM main.resource_access_epoch WHERE singleton=1), ` + ownershipLedgerClosure("_anx_denied", roots, false, ledger) + ` SELECT ` + current + `,CASE WHEN ` + current + `=? THEN NULL ELSE COALESCE((SELECT json_group_array(json_array(kind,id)) FROM _anx_denied),'[]') END`
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
				sql += "," + resourceaccess.CompactReaderSQL
				dest = append(dest, &reader)
				if err := db.QueryRowContext(c, sql, captureArgs...).Scan(dest...); err != nil {
					break
				}
				observed := resourceaccess.ReaderLedger(reader.Valid, reader.Int64)
				rememberAccessLedger(raw, observed)
				if cached != nil && candidate.epoch == cached.epoch {
					// The cached closure was validated; this one was not built.
					state.snapshot = cached
				} else if ledger != resourceaccess.SelectedLedger && ledger != observed {
					ledger = resourceaccess.SelectedLedger
					continue
				} else if rows.Valid && (epochTable != "receipt_access_epoch" || refs.Valid) {
					candidate.rows, candidate.refs = rows.String, refs.String
					state.snapshot = candidate
					rememberReadDenial(raw, scope, candidate)
				}
				break
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

// WithReadTickSnapshot starts a new scoped read boundary for one poll.
// Recreate it each tick; never attach this state to the long-lived stream.
// Each consuming SQL statement sees its current canonical ancestry and roots.
// Transactional writes stay canonical.
func WithReadTickSnapshot(ctx context.Context) context.Context {
	if scope, ok := accessScopeFrom(ctx); ok {
		return WithRequestAccessScope(ctx, scope)
	}
	return ctx
}
