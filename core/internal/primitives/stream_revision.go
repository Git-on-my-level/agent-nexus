package primitives

import (
	"context"
	"database/sql"
	"fmt"
)

// StreamRevision observes committed database changes on one dedicated connection.
// SQLite data_version is connection-local: retaining this connection, and never
// writing through it, is essential. It catches external writers and non-event
// changes (permissions, projection maintenance, receipts, answer read state).
// No transaction is held open and no workspace records are read.
type StreamRevision struct{ conn *sql.Conn }

func (s *Store) OpenStreamRevision(ctx context.Context) (*StreamRevision, error) {
	if s.streamDB == nil {
		return nil, fmt.Errorf("stream database unavailable")
	}
	// Reserving the only pool connection would deadlock the subsequent page read.
	if s.streamDB.Stats().MaxOpenConnections == 1 {
		return nil, nil
	}
	c, err := s.streamDB.Conn(ctx)
	if err != nil {
		return nil, err
	}
	return &StreamRevision{conn: c}, nil
}

func (s *StreamRevision) Current(ctx context.Context) (int64, error) {
	var version int64
	err := s.conn.QueryRowContext(ctx, "PRAGMA main.data_version").Scan(&version)
	return version, err
}
func (s *StreamRevision) Close() error { return s.conn.Close() }

// StreamReaderScope returns the immutable policy identity, including whether
// authorization is installed. An internal unscoped reader is not anonymous.
func StreamReaderScope(ctx context.Context) (AccessScope, bool) { return accessScopeFrom(ctx) }
