package primitives

import (
	"context"
	"database/sql/driver"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"

	"modernc.org/sqlite"
)

type summaryPointReadKey struct{}

// A token is admitted only for one immutable SQLite transaction and removed on
// rollback. The map is canonical denial evidence, never a grant supplied by a
// caller. Missing/expired tokens fail closed. This avoids decoding the entire
// cached closure again in every bounded point-lookup statement.
var pointSnapshots sync.Map
var pointSnapshotSequence atomic.Int64

func init() {
	sqlite.MustRegisterScalarFunction("anx_point_snapshot_denied", 3, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		token, ok := args[0].(int64)
		if !ok {
			return int64(1), nil
		}
		value, ok := pointSnapshots.Load(token)
		if !ok {
			return int64(1), nil
		}
		snapshot := value.(*denialSnapshot)
		kind, kindOK := args[1].(string)
		id, idOK := args[2].(string)
		if raw, ok := args[2].([]byte); ok {
			id, idOK = string(raw), true
		}
		if numeric, ok := args[2].(int64); ok {
			id, idOK = strconv.FormatInt(numeric, 10), true
		}
		if !kindOK || !idOK || snapshot.pointIndex == nil {
			return int64(1), nil
		}
		if _, denied := snapshot.pointIndex[denialTarget{kind, id}]; denied {
			return int64(1), nil
		}
		return int64(0), nil
	})
	// These readers expose only the already admitted immutable proof. Missing
	// tokens error instead of returning an empty set (which would grant access).
	sqlite.MustRegisterScalarFunction("anx_point_snapshot_rows", 2, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		token, ok := args[0].(int64)
		value, found := pointSnapshots.Load(token)
		if !ok || !found || value.(*denialSnapshot).pointIndex == nil {
			return nil, fmt.Errorf("point snapshot unavailable")
		}
		snapshot := value.(*denialSnapshot)
		switch args[1] {
		case "all":
			return snapshot.rows, nil
		case "series-count":
			return int64(snapshot.pointSeriesCount), nil
		case "any":
			return int64(len(snapshot.pointIndex)), nil
		default:
			return nil, fmt.Errorf("unknown point snapshot selector")
		}
	})
}

// Point reads share the immutable decoded denial set with ordinary lookups.
// The per-transaction token remains separate from the cached authority.
func (s *denialSnapshot) preparePointIndex() {
	s.pointOnce.Do(func() {
		s.prepareTargetIndex()
		s.pointIndex = s.targetIndex
		for target := range s.pointIndex {
			if target.kind == "series" {
				s.pointSeriesCount++
			}
		}
	})
}

// Summary is a read-only phase even when its caller is a POST projection.
// Admission precedes all canonical filters and ref-kind lookups in that phase.
func (s *Store) beginSummaryRead(ctx context.Context) (context.Context, func(), error) {
	if scope, scoped := accessScopeFrom(ctx); scoped {
		if _, requested := ctx.Value(denialRequestKey{}).(*denialRequestState); !requested {
			ctx = WithRequestAccessScope(ctx, scope)
		}
	}
	ctx = context.WithValue(ctx, summaryPointReadKey{}, true)
	return s.BeginOverviewRead(ctx)
}
