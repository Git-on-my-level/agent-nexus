package primitives

import (
	"database/sql"

	"agent-nexus-core/internal/blob"
)

func NewTestStore(db *sql.DB, artifactContentDir string) *Store {
	var backend blob.Backend
	if artifactContentDir != "" {
		backend = blob.NewFilesystemBackend(artifactContentDir)
	}
	return NewStore(db, backend, artifactContentDir)
}
