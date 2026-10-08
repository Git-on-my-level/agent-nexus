package auth

import (
	"agent-nexus-core/internal/resourceaccess"
	"context"
	"database/sql"
	"errors"
)

// IsLocalPMCandidate uses the unique agent_id index and the host primary key,
// never the host roster or a reader-dependent authorization graph.
func (s *Store) IsLocalPMCandidate(ctx context.Context, agentID string) (bool, error) {
	var name, kind string
	err := resourceaccess.NewDB(s.db).QueryRowContext(ctx, `SELECT ha.name,ha.identity_kind FROM host_agents ha JOIN hosts h ON h.id=ha.host_id WHERE ha.agent_id=? AND h.revoked_at IS NULL`, agentID).Scan(&name, &kind)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil && name == "pm" && kind == "derived", err
}
