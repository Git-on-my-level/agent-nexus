package server

import (
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/decide"
	"agent-nexus-core/internal/primitives"
	"database/sql"
	"testing"
)

// Domain integration fixtures model an existing decision runtime.
func newOnboardedDecideRuntime(t *testing.T, db *sql.DB, store *primitives.Store, as *auth.Store, cfg DecideRuntimeConfig) (*DecideRuntime, error) {
	t.Helper()
	return NewDecideRuntime(db, store, as, cfg)
}

func newOnboardedPMService(t *testing.T, db *sql.DB, store *decide.Store, cfg decide.Config, deps decide.Dependencies) (*decide.Service, error) {
	t.Helper()
	return decide.NewService(store, cfg, deps)
}
