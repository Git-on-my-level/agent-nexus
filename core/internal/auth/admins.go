package auth

import (
	"agent-nexus-core/internal/resourceaccess"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
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

// Transaction accepts either a raw auth transaction or a scoped business transaction.
type Transaction interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// requireAdministrationTx reads durable authority inside the same transaction as
// the mutation. Request-context principals are snapshots and cannot authorize a
// write after a concurrent grant removal. SQLite serializes the read/write
// transaction against grant changes (a stale read snapshot cannot be upgraded).
func requireAdministrationTx(ctx context.Context, tx Transaction, actor Principal, humanOnly bool) error {
	// Durable authority is independent of resource visibility; no profile data escapes.
	ctx = resourceaccess.WithoutPolicy(ctx)
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

type AuthAdminPage struct {
	Admins     []AuthAdmin `json:"admins"`
	NextCursor string      `json:"next_cursor"`
	HasMore    bool        `json:"has_more"`
}

func (s *Store) ListAuthAdmins(ctx context.Context) ([]AuthAdmin, error) {
	out := []AuthAdmin{}
	cursor := ""
	for {
		page, err := s.AuthAdminPage(ctx, 200, cursor)
		if err != nil {
			return nil, err
		}
		out = append(out, page.Admins...)
		if !page.HasMore {
			return out, nil
		}
		cursor = page.NextCursor
	}
}

func (s *Store) AuthAdminPage(ctx context.Context, limit int, cursor string) (AuthAdminPage, error) {
	out := AuthAdminPage{Admins: []AuthAdmin{}}
	if limit < 1 || limit > 200 || len(cursor) > 2048 {
		return out, ErrInvalidRequest
	}
	var before struct{ Username, ID string }
	if cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || json.Unmarshal(raw, &before) != nil || before.ID == "" {
			return out, ErrInvalidRequest
		}
	}
	query := `SELECT id,actor_id,username FROM agents a WHERE ` + principalKindExpr("a") + `='agent' AND revoked_at IS NULL AND COALESCE(json_extract(metadata_json,'$.auth_admin'),0)=1`
	args := []any{}
	if cursor != "" {
		query += ` AND (username,id)>(?,?)`
		args = append(args, before.Username, before.ID)
	}
	query += ` ORDER BY username,id LIMIT ?`
	args = append(args, limit+1)
	rows, err := resourceaccess.NewDB(s.db).QueryContext(ctx, `SELECT a.id,a.actor_id,a.username,COALESCE(h.id,''),COALESCE(h.slug,''),COALESCE(CASE WHEN h.id IS NOT NULL THEN ha.name END,'') FROM (`+query+`) a LEFT JOIN host_agents ha ON ha.agent_id=a.id LEFT JOIN hosts h ON h.id=ha.host_id ORDER BY a.username,a.id`, args...)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var item AuthAdmin
		if err = rows.Scan(&item.PrincipalID, &item.ActorID, &item.Username, &item.HostID, &item.HostSlug, &item.AgentName); err != nil {
			return out, err
		}
		if len(out.Admins) == limit {
			out.HasMore = true
			last := out.Admins[len(out.Admins)-1]
			before.Username, before.ID = last.Username, last.PrincipalID
			raw, _ := json.Marshal(before)
			out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
			break
		}
		item.AuthAdmin = true
		out.Admins = append(out.Admins, item)
	}
	return out, rows.Err()
}

// SetAuthAdmin changes only an explicit agent grant, never human administration.
// Metadata is read by AuthenticateAccessToken on every request, not cached in tokens.
func (s *Store) SetAuthAdmin(ctx context.Context, target string, grant bool, actor Principal) (AuthAdmin, error) {
	tx, err := resourceaccess.NewDB(s.db).BeginTx(ctx, nil)
	if err != nil {
		return AuthAdmin{}, err
	}
	defer tx.Rollback()
	out, err := s.SetAuthAdminTx(ctx, tx, target, grant, actor)
	if err != nil {
		return out, err
	}
	return out, tx.Commit()
}

// SetAuthAdminTx shares the grant boundary with transactional access decisions.
// It rechecks human authority and target liveness within the caller's transaction.
func (s *Store) SetAuthAdminTx(ctx context.Context, tx Transaction, target string, grant bool, actor Principal) (AuthAdmin, error) {
	return ApplyAuthAdminTx(ctx, tx, target, grant, actor)
}

// ApplyAuthAdminTx retains the caller's transaction and its resource scope.
func ApplyAuthAdminTx(ctx context.Context, tx Transaction, target string, grant bool, actor Principal) (AuthAdmin, error) {
	if err := requireAdministrationTx(ctx, tx, actor, true); err != nil {
		return AuthAdmin{}, err
	}
	var kind string
	var out AuthAdmin
	var revoked sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT id,actor_id,username,`+principalKindExpr("a")+`,COALESCE(json_extract(metadata_json,'$.auth_admin'),0),revoked_at FROM agents a WHERE id=? OR username=? ORDER BY CASE WHEN id=? THEN 0 ELSE 1 END LIMIT 1`, strings.TrimSpace(target), strings.TrimSpace(target), strings.TrimSpace(target)).Scan(&out.PrincipalID, &out.ActorID, &out.Username, &kind, &out.AuthAdmin, &revoked)
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
		if err = recordAuthAuditEventTx(ctx, tx, AuthAuditEventInput{EventType: event, ActorAgentID: actor.AgentID, ActorActorID: actor.ActorID, SubjectAgentID: out.PrincipalID, SubjectActorID: out.ActorID, Metadata: map[string]any{"host_id": out.HostID, "host_slug": out.HostSlug, "agent_name": out.AgentName}}); err != nil {
			return out, err
		}
	}
	out.AuthAdmin = grant
	return out, nil
}

func boolJSON(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

// RequireHumanTx rechecks durable human authority for an access decision.
func RequireHumanTx(ctx context.Context, tx Transaction, actor Principal) error {
	return requireAdministrationTx(ctx, tx, actor, true)
}

// RequireAgentTx permits only the active, authenticated agent itself.
func RequireAgentTx(ctx context.Context, tx Transaction, actor Principal) error {
	if actor.SeriesAdapter != "" || actor.PrincipalKind != string(PrincipalKindAgent) {
		return ErrInvalidRequest
	}
	var kind string
	err := tx.QueryRowContext(ctx, `SELECT `+principalKindExpr("a")+` FROM agents a WHERE id=? AND actor_id=? AND revoked_at IS NULL`, actor.AgentID, actor.ActorID).Scan(&kind)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAgentNotFound
	}
	if err != nil {
		return err
	}
	if kind != string(PrincipalKindAgent) {
		return ErrInvalidRequest
	}
	return nil
}
