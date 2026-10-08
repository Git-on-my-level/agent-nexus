package primitives

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

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
}

// SQLite identities can be TEXT or INTEGER (evidence-alias row IDs). Preserve
// their equality spelling and skip NULL identities, which cannot match a row.
func (s *denialSnapshot) preparePointIndex() {
	s.pointOnce.Do(func() {
		if !utf8.ValidString(s.rows) || !strings.HasPrefix(strings.TrimSpace(s.rows), "[") {
			return
		}
		decoder := json.NewDecoder(strings.NewReader(s.rows))
		decoder.UseNumber()
		var rows [][]any
		if decoder.Decode(&rows) != nil {
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
		s.pointIndex = index
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
