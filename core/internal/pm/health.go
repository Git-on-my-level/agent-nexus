package pm

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// WatcherUnhealthy exposes connection/deadline state only, never conversation
// data. Disconnect removes the expectation. A later successful turn is the
// recovery evidence for a missed deadline; claim polling alone cannot clear it.
func (s *Service) WatcherUnhealthy(ctx context.Context, now time.Time, offlineAfter time.Duration) (bool, error) {
	actor := s.AgentActorID()
	if actor == "" {
		return false, nil
	}
	var last, signal, first string
	err := s.store.database().QueryRowContext(ctx, `SELECT p.last_seen_at,p.signal,p.first_seen_at FROM pm_registration r JOIN pm_presence p ON p.workspace_id=r.workspace_id AND p.actor_id=r.actor_id WHERE r.workspace_id=? AND r.actor_id=?`, s.cfg.WorkspaceID, actor).Scan(&last, &signal, &first)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if signal == "disconnect" {
		return false, nil
	}
	seen, err := time.Parse(time.RFC3339Nano, last)
	if err != nil {
		return false, err
	}
	if now.Sub(seen) >= 90*time.Second+offlineAfter {
		return true, nil
	}
	var failed, success sql.NullString
	// Turn creation orders requests independently of timeout changes. UTC keys
	// strip Z so whole-second and fractional timestamps retain exact ordering.
	// An older success with a longer deadline must never hide a newer expiry.
	err = s.store.database().QueryRowContext(ctx, `SELECT
 (SELECT json_extract(body,'$.created_at') FROM pm_records WHERE kind='turn' AND workspace_id=? AND json_extract(body,'$.agent_actor_id')=? AND json_extract(body,'$.status')='failed' AND json_extract(body,'$.failure_kind')='expired' AND rtrim(COALESCE(json_extract(body,'$.created_at'),''),'Z')>=? ORDER BY rtrim(COALESCE(json_extract(body,'$.created_at'),''),'Z') DESC,id DESC LIMIT 1),
 (SELECT json_extract(body,'$.created_at') FROM pm_records WHERE kind='turn' AND workspace_id=? AND json_extract(body,'$.agent_actor_id')=? AND json_extract(body,'$.status')='delivered' ORDER BY rtrim(COALESCE(json_extract(body,'$.created_at'),''),'Z') DESC,id DESC LIMIT 1)`, s.cfg.WorkspaceID, actor, strings.TrimSuffix(first, "Z"), s.cfg.WorkspaceID, actor).Scan(&failed, &success)
	if err != nil {
		return false, err
	}
	if !failed.Valid {
		return false, nil
	}
	created, err := time.Parse(time.RFC3339Nano, failed.String)
	if err != nil {
		return false, err
	}
	recovered, _ := time.Parse(time.RFC3339Nano, success.String)
	return !created.Before(recovered), nil
}
