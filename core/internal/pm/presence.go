package pm

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Presence contains no turn, runner, or principal data. Only authenticated,
// accepted claims and lease heartbeats update the configured PM's exact key.
type Presence struct {
	Configured bool   `json:"configured"`
	Connected  bool   `json:"connected"`
	LastSeenAt string `json:"last_seen_at,omitempty"`
	Signal     string `json:"signal,omitempty"`
}

func (s *Service) Presence(ctx context.Context, p Principal) (Presence, error) {
	if err := s.authorize(ctx, p, "pm.presence", ""); err != nil {
		return Presence{}, err
	}
	out := Presence{Configured: s.cfg.AgentActorID != ""}
	if !out.Configured {
		return out, nil
	}
	err := s.store.database().QueryRowContext(ctx, `SELECT last_seen_at,signal FROM pm_presence WHERE workspace_id=? AND actor_id=?`, p.WorkspaceID, s.cfg.AgentActorID).Scan(&out.LastSeenAt, &out.Signal)
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return Presence{}, err
	}
	last, err := time.Parse(time.RFC3339Nano, out.LastSeenAt)
	if err != nil {
		return Presence{}, err
	}
	age := time.Since(last)
	out.Connected = age >= 0 && age <= 90*time.Second
	return out, nil
}

func (s *Service) notePresence(ctx context.Context, p Principal, signal string) error {
	// This separate projection does not invalidate resource authorization epochs.
	// Throttle idle polls to one write per 15 seconds; heartbeats remain fresh.
	_, err := s.store.database().ExecContext(ctx, `INSERT INTO pm_presence(workspace_id,actor_id,last_seen_at,signal) VALUES(?,?,?,?) ON CONFLICT(workspace_id,actor_id) DO UPDATE SET last_seen_at=excluded.last_seen_at,signal=excluded.signal WHERE julianday(excluded.last_seen_at)-julianday(pm_presence.last_seen_at)>=15.0/86400.0`, p.WorkspaceID, p.ActorID, time.Now().UTC().Format(time.RFC3339Nano), signal)
	return err
}
