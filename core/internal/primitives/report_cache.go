package primitives

import (
	"context"
	"encoding/json"
	"fmt"
)

// Each worker owns its request cache and SQLite read snapshot. Visibility state
// is never shared between concurrently materialized panels.
func (s *Store) BeginReportRead(ctx context.Context) (context.Context, func(), error) {
	if scope, ok := accessScopeFrom(ctx); ok {
		ctx = WithRequestAccessScope(ctx, scope)
	}
	return s.BeginResourceRead(ctx)
}
func (s *Store) ReportRenderToken(ctx context.Context) (string, error) {
	scope, ok := accessScopeFrom(ctx)
	if !ok {
		return "", nil
	}
	var epoch int64
	if err := s.db.QueryRowContext(ctx, `SELECT version FROM resource_access_epoch WHERE singleton=1`).Scan(&epoch); err != nil {
		return "", err
	}
	key, _ := json.Marshal(scope)
	return fmt.Sprintf("%d:%s", epoch, key), nil
}
