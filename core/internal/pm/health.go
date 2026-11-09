package pm

import (
	"context"
	"database/sql"
	"errors"
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
	// Each branch fixes the status and failure kind before the deadline seek.
	err = s.store.database().QueryRowContext(ctx, `SELECT
 (SELECT json_extract(body,'$.deadline') FROM pm_records WHERE kind='turn' AND workspace_id=? AND json_extract(body,'$.agent_actor_id')=? AND json_extract(body,'$.status')='failed' AND json_extract(body,'$.failure_kind')='expired' AND julianday(json_extract(body,'$.deadline'))>=julianday(?) ORDER BY julianday(json_extract(body,'$.deadline')) DESC,id DESC LIMIT 1),
 (SELECT json_extract(body,'$.deadline') FROM pm_records WHERE kind='turn' AND workspace_id=? AND json_extract(body,'$.agent_actor_id')=? AND json_extract(body,'$.status')='delivered' ORDER BY julianday(json_extract(body,'$.deadline')) DESC,id DESC LIMIT 1)`, s.cfg.WorkspaceID, actor, first, s.cfg.WorkspaceID, actor).Scan(&failed, &success)
	if err != nil {
		return false, err
	}
	if !failed.Valid {
		return false, nil
	}
	deadline, err := time.Parse(time.RFC3339Nano, failed.String)
	if err != nil {
		return false, err
	}
	recovered, _ := time.Parse(time.RFC3339Nano, success.String)
	return deadline.After(recovered), nil
}
