package resourceaccess

import (
	"context"

	"agent-nexus-core/internal/sqliteutil"
)

// BeginMaintenanceChunk retains the scoped transaction wrapper while using the
// shared pinned-connection, nonblocking SQLite maintenance protocol.
func (d *DB) BeginMaintenanceChunk(ctx context.Context) (*Tx, func(), error) {
	tx, cleanup, err := sqliteutil.BeginMaintenanceChunk(ctx, d.raw)
	if err != nil {
		return nil, nil, err
	}
	return &Tx{raw: tx}, cleanup, nil
}
