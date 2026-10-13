package primitives

import (
	"context"
	"database/sql"
	"errors"

	"agent-nexus-core/internal/resourceaccess"
)

type pinnedDenialKey struct{}

var errOverviewSnapshotUnavailable = errors.New("Overview snapshot epoch unavailable")

type pinnedDenial struct {
	db       resourceaccess.QueryRower
	snapshot *denialSnapshot
	scope    AccessScope
}

// BeginResourceRead pins a read-only resource page and uses constant-time
// identity membership where its SQL needs no reference/prose expansion. Queries
// needing the complete policy retain it in the same admitted transaction.
func (s *Store) BeginResourceRead(ctx context.Context) (context.Context, func(), error) {
	if s == nil || s.db == nil {
		return ctx, func() {}, errors.New("resource store database is not initialized")
	}
	return s.beginSummaryRead(ctx)
}

// BeginOverviewRead admits a bounded Overview phase to an immutable SQLite
// snapshot. Admission recomputes canonical denial on an epoch mismatch. Reads
// inside that same transaction still validate the epoch, but need not compile
// the unreachable canonical recursion again for every join and subquery.
func (s *Store) BeginOverviewRead(ctx context.Context) (context.Context, func(), error) {
	if _, pinned := ctx.Value(pinnedDenialKey{}).(pinnedDenial); pinned {
		return ctx, func() {}, nil
	}
	if _, ok := ctx.Value(denialRequestKey{}).(*denialRequestState); !ok {
		return ctx, func() {}, nil
	}
	var pointToken int64
	next, close, err := s.db.PinReads(ctx, func(ctx context.Context, db resourceaccess.QueryRower) (context.Context, error) {
		scope, _ := accessScopeFrom(ctx)
		cached := denialSnapshotFrom(ctx)
		if cached == nil || (cached.epochTable != "" && cached.epochTable != "resource_access_epoch") {
			return ctx, errOverviewSnapshotUnavailable
		}
		// Reading the epoch and closure in this statement establishes the SQLite
		// transaction snapshot even when the shared/request cache is stale.
		current := `(SELECT version FROM main.resource_access_epoch WHERE singleton=1)`
		roots := `SELECT * FROM (` + deniedRootSQL(scope) + `) WHERE COALESCE(` + current + `,-1)<>?`
		query := `WITH RECURSIVE ` + ownershipClosure("_anx_denied", roots, false) + ` SELECT COALESCE(` + current + `,-1),CASE WHEN ` + current + `=? THEN NULL ELSE COALESCE((SELECT json_group_array(json_array(kind,id)) FROM _anx_denied),'[]') END`
		snapshot := &denialSnapshot{}
		var rows sql.NullString
		// This probe establishes the transaction snapshot. A matching epoch
		// proves cached authority in that same snapshot without preparing the
		// large canonical fallback. A miss still evaluates the complete graph.
		if err := db.QueryRowContext(ctx, `SELECT COALESCE((SELECT version FROM main.resource_access_epoch WHERE singleton=1),-1)`).Scan(&snapshot.epoch); err != nil {
			return ctx, err
		}
		if snapshot.epoch != cached.epoch {
			if err := db.QueryRowContext(ctx, query, cached.epoch, cached.epoch).Scan(&snapshot.epoch, &rows); err != nil {
				return ctx, err
			}
		}
		if snapshot.epoch < 0 {
			return ctx, errOverviewSnapshotUnavailable
		}
		if snapshot.epoch == cached.epoch {
			snapshot.rows = cached.rows
			cached.preparePointIndex()
			snapshot.pointOnce.Do(func() { snapshot.pointIndex = cached.pointIndex })
		} else {
			if !rows.Valid {
				return ctx, errOverviewSnapshotUnavailable
			}
			snapshot.rows = rows.String
			snapshot.preparePointIndex()
		}
		if snapshot.pointIndex == nil {
			return context.WithValue(ctx, pinnedDenialKey{}, pinnedDenial{db, snapshot, scope}), nil
		}
		pointToken = pointSnapshotSequence.Add(1)
		snapshot.pointToken = pointToken
		pointSnapshots.Store(pointToken, snapshot)
		return context.WithValue(ctx, pinnedDenialKey{}, pinnedDenial{db, snapshot, scope}), nil
	})
	if errors.Is(err, errOverviewSnapshotUnavailable) {
		return ctx, func() {}, nil
	}
	if err != nil {
		pointSnapshots.Delete(pointToken)
		return next, close, err
	}
	return next, func() {
		close()
		pointSnapshots.Delete(pointToken)
	}, nil
}
