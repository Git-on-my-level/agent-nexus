package primitives

import (
	"agent-nexus-core/internal/blob"
	"database/sql"
)

// NewScopeExperimentBridgeStore is an experiment-only constructor on a non-merge
// branch. The real bridge must replace startup backfill and supervise durable jobs.
// Construct without a backend so the existing synchronous backfill cannot do I/O,
// then attach it for normal serving. Unknown manifests remain fail-closed.
func NewScopeExperimentBridgeStore(db *sql.DB, backend blob.Backend, root string) *Store {
	s := NewStore(db, nil, root)
	s.blob = backend
	return s
}
