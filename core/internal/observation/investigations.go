package observation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// PresetResolver resolves the existing deployment agent/provider preset. It must
// not create a provider registry, silently substitute a model, or grant new tools.
type PresetResolver func(context.Context, string) error
type SavedInvestigation struct {
	ID          string            `json:"id"`
	WorkspaceID string            `json:"workspace_id"`
	Version     int64             `json:"version"`
	Spec        InvestigationSpec `json:"spec"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

// InvestigationStore uses the existing canonical workspace SQL connection. It
// stores bounded specifications only; saving one does not dispatch a model.
// Executing a model requires a separately enforced read-only bridge/runner. This
// package does not claim prompt instructions enforce a model's tool permissions.
type InvestigationStore struct {
	db      *sql.DB
	resolve PresetResolver
}

func NewInvestigationStore(db *sql.DB, resolve PresetResolver) (*InvestigationStore, error) {
	if db == nil || resolve == nil {
		return nil, failure(ErrConfiguration, "investigation storage requires canonical DB and existing-preset resolver")
	}
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS observation_investigation_specs (
 workspace_id TEXT NOT NULL,
 id TEXT NOT NULL,
 version INTEGER NOT NULL CHECK(version > 0),
 spec_json TEXT NOT NULL,
 updated_at TEXT NOT NULL,
 PRIMARY KEY(workspace_id,id)
 )`)
	if err != nil {
		return nil, err
	}
	return &InvestigationStore{db: db, resolve: resolve}, nil
}
func (s *InvestigationStore) Get(ctx context.Context, workspaceID, id string) (SavedInvestigation, error) {
	out := SavedInvestigation{ID: id, WorkspaceID: workspaceID}
	if workspaceID == "" || !validComponent(id) {
		return out, failure(ErrPermission, "authenticated workspace and investigation ID required")
	}
	var raw, updated string
	err := s.db.QueryRowContext(ctx, `SELECT version,spec_json,updated_at FROM observation_investigation_specs WHERE workspace_id=? AND id=?`, workspaceID, id).Scan(&out.Version, &raw, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return out, failure(ErrNotFound, "investigation not found in authenticated workspace")
	}
	if err != nil {
		return out, err
	}
	if len(raw) > 65536 || json.Unmarshal([]byte(raw), &out.Spec) != nil || out.Spec.Validate() != nil || out.Spec.Target.WorkspaceID != workspaceID {
		return out, failure(ErrInvalidOutput, "stored investigation failed scope validation")
	}
	out.UpdatedAt, err = time.Parse(time.RFC3339Nano, updated)
	return out, err
}
func (s *InvestigationStore) Save(ctx context.Context, workspaceID, id string, spec InvestigationSpec, ifVersion int64) (SavedInvestigation, error) {
	out := SavedInvestigation{ID: id, WorkspaceID: workspaceID, Spec: spec, Version: ifVersion + 1, UpdatedAt: time.Now().UTC()}
	if workspaceID == "" || spec.Target.WorkspaceID != workspaceID || !validComponent(id) || ifVersion < 0 || ifVersion > 1<<40 {
		return out, failure(ErrPermission, "invalid investigation workspace, ID or version")
	}
	if err := spec.Validate(); err != nil {
		return out, err
	}
	if err := s.resolve(ctx, spec.PresetRef); err != nil {
		return out, failure(ErrConfiguration, "existing investigation preset is unavailable; no model fallback")
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		return out, err
	}
	if len(raw) > 65536 {
		return out, failure(ErrLimit, "investigation specification exceeds 64KiB")
	}
	if ifVersion == 0 {
		_, err = s.db.ExecContext(ctx, `INSERT INTO observation_investigation_specs(workspace_id,id,version,spec_json,updated_at) VALUES(?,?,1,?,?)`, workspaceID, id, string(raw), out.UpdatedAt.Format(time.RFC3339Nano))
		if err != nil {
			return out, failure(ErrConfiguration, "investigation create failed or ID already exists")
		}
	} else {
		result, err := s.db.ExecContext(ctx, `UPDATE observation_investigation_specs SET version=version+1,spec_json=?,updated_at=? WHERE workspace_id=? AND id=? AND version=?`, string(raw), out.UpdatedAt.Format(time.RFC3339Nano), workspaceID, id, ifVersion)
		if err != nil {
			return out, err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return out, err
		}
		if count != 1 {
			return out, failure(ErrConfiguration, "investigation version conflict")
		}
	}
	return out, nil
}
