package server

import (
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/primitives"
	"database/sql"
	"testing"
	"time"
)

// Domain integration fixtures model an existing onboarded PM. Bootstrap/gating
// regressions deliberately call NewPMRuntime directly without this seed.
func newOnboardedPMRuntime(t *testing.T, db *sql.DB, store *primitives.Store, as *auth.Store, cfg PMRuntimeConfig) (*PMRuntime, error) {
	t.Helper()
	if cfg.PM.AgentActorID == "" {
		cfg.PM.AgentActorID = "fixture-pm"
	}
	rt, err := NewPMRuntime(db, store, as, cfg)
	if err == nil && cfg.PM.AgentActorID != "" {
		_, err = db.Exec(`INSERT OR IGNORE INTO pm_presence(workspace_id,actor_id,last_seen_at,signal) VALUES(?,?,?,'connect')`, cfg.PM.WorkspaceID, cfg.PM.AgentActorID, time.Now().UTC().Format(time.RFC3339Nano))
	}
	return rt, err
}

func newOnboardedPMService(t *testing.T, db *sql.DB, store *pm.Store, cfg pm.Config, deps pm.Dependencies) (*pm.Service, error) {
	t.Helper()
	if cfg.AgentActorID == "" {
		cfg.AgentActorID = "fixture-pm"
	}
	svc, err := pm.NewService(store, cfg, deps)
	if err == nil {
		_, err = db.Exec(`INSERT OR IGNORE INTO pm_presence(workspace_id,actor_id,last_seen_at,signal) VALUES(?,?,?,'connect')`, cfg.WorkspaceID, cfg.AgentActorID, time.Now().UTC().Format(time.RFC3339Nano))
	}
	return svc, err
}
