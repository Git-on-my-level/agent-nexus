package auth

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

var ErrHumanRequired = errors.New("human_required")
var ErrAuthAdminRequired = errors.New("auth_admin_required")
var ErrHostSelfRevoke = errors.New("host_self_revoke")

type AuthAdmin struct {
	PrincipalID string `json:"principal_id"`
	ActorID     string `json:"actor_id"`
	Username    string `json:"username"`
	AuthAdmin   bool   `json:"auth_admin"`
	HostID      string `json:"host_id,omitempty"`
	HostSlug    string `json:"host_slug,omitempty"`
	AgentName   string `json:"agent_name,omitempty"`
}

// requireAdministrationTx reads durable authority inside the same transaction as
// the mutation. Request-context principals are snapshots and cannot authorize a
// write after a concurrent grant removal. SQLite serializes the read/write
// transaction against grant changes (a stale read snapshot cannot be upgraded).
func requireAdministrationTx(ctx context.Context, tx *sql.Tx, actor Principal, humanOnly bool) error {
	denied := ErrAuthAdminRequired
	if humanOnly {
		denied = ErrHumanRequired
	}
	if actor.SeriesAdapter != "" {
		return denied
	}
	var kind string
	var granted bool
	err := tx.QueryRowContext(ctx, `SELECT `+principalKindExpr("a")+`,COALESCE(json_extract(metadata_json,'$.auth_admin'),0) FROM agents a WHERE id=? AND actor_id=? AND revoked_at IS NULL`, actor.AgentID, actor.ActorID).Scan(&kind, &granted)
	if errors.Is(err, sql.ErrNoRows) {
		return denied
	}
	if err != nil {
		return err
	}
	if kind == string(PrincipalKindHuman) || (!humanOnly && kind == string(PrincipalKindAgent) && granted) {
		return nil
	}
	return denied
}

func authAdminHostScope(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, admin *AuthAdmin) error {
	err := q.QueryRowContext(ctx, `SELECT h.id,h.slug,ha.name FROM host_agents ha JOIN hosts h ON h.id=ha.host_id WHERE ha.agent_id=?`, admin.PrincipalID).Scan(&admin.HostID, &admin.HostSlug, &admin.AgentName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

func (s *Store) ListAuthAdmins(ctx context.Context) ([]AuthAdmin, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,actor_id,username FROM agents a WHERE `+principalKindExpr("a")+`='agent' AND revoked_at IS NULL AND COALESCE(json_extract(metadata_json,'$.auth_admin'),0)=1 ORDER BY username,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuthAdmin{}
	for rows.Next() {
		var item AuthAdmin
		if err := rows.Scan(&item.PrincipalID, &item.ActorID, &item.Username); err != nil {
			return nil, err
		}
		item.AuthAdmin = true
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	for i := range out {
		if err := authAdminHostScope(ctx, s.db, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// SetAuthAdmin changes only an explicit agent grant, never human administration.
// Metadata is read by AuthenticateAccessToken on every request, not cached in tokens.
func (s *Store) SetAuthAdmin(ctx context.Context, target string, grant bool, actor Principal) (AuthAdmin, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AuthAdmin{}, err
	}
	defer tx.Rollback()
	if err := requireAdministrationTx(ctx, tx, actor, true); err != nil {
		return AuthAdmin{}, err
	}
	var kind string
	var out AuthAdmin
	var revoked sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT id,actor_id,username,`+principalKindExpr("a")+`,COALESCE(json_extract(metadata_json,'$.auth_admin'),0),revoked_at FROM agents a WHERE id=? OR username=? ORDER BY CASE WHEN id=? THEN 0 ELSE 1 END LIMIT 1`, strings.TrimSpace(target), strings.TrimSpace(target), strings.TrimSpace(target)).Scan(&out.PrincipalID, &out.ActorID, &out.Username, &kind, &out.AuthAdmin, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return out, ErrAgentNotFound
	}
	if err != nil {
		return out, err
	}
	if kind != string(PrincipalKindAgent) || revoked.Valid {
		return out, ErrInvalidRequest
	}
	if err := authAdminHostScope(ctx, tx, &out); err != nil {
		return out, err
	}
	if out.AuthAdmin != grant {
		if _, err = tx.ExecContext(ctx, `UPDATE agents SET metadata_json=json_set(metadata_json,'$.auth_admin',json(?)),updated_at=? WHERE id=?`, boolJSON(grant), hostNow(), out.PrincipalID); err != nil {
			return out, err
		}
		event := "auth_admin_revoked"
		if grant {
			event = "auth_admin_granted"
		}
		if err = s.recordAuthAuditEventTx(ctx, tx, AuthAuditEventInput{EventType: event, ActorAgentID: actor.AgentID, ActorActorID: actor.ActorID, SubjectAgentID: out.PrincipalID, SubjectActorID: out.ActorID, Metadata: map[string]any{"host_id": out.HostID, "host_slug": out.HostSlug, "agent_name": out.AgentName}}); err != nil {
			return out, err
		}
	}
	out.AuthAdmin = grant
	return out, tx.Commit()
}

func boolJSON(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
