package pm

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode"
)

// Presence exposes bounded connection labels and timestamps, never turn data
// or commands. Connect, accepted claims and lease heartbeats update the PM key.
type Presence struct {
	State      string  `json:"state"`
	LastSeen   *string `json:"last_seen"`
	Runner     *string `json:"runner"`
	Host       *string `json:"host"`
	Configured bool    `json:"configured"`
	Connected  bool    `json:"connected"`
	LastSeenAt string  `json:"last_seen_at,omitempty"`
	Signal     string  `json:"signal,omitempty"`
}

type bootstrapConnectionKey struct{}

type ConnectionInput struct {
	Runner string `json:"runner"`
	Host   string `json:"host"`
}

func (s *Service) AgentActorID() string {
	if s.cfg.AgentActorID != "" {
		return s.cfg.AgentActorID
	}
	actor, _ := s.selected.Load().(string)
	return actor
}

func connectionLabel(v string) bool {
	return strings.TrimSpace(v) == v && validText(v, 80) && !strings.ContainsAny(v, "\"'\\/") && !strings.Contains(v, "://") && !strings.ContainsFunc(v, unicode.IsControl)
}

// Connect is the bootstrap boundary, not a PM conversation. Authorization must
// establish the selected actor or the reserved host-derived pm identity first.
func (s *Service) Connect(ctx context.Context, p Principal, in ConnectionInput) (Presence, error) {
	if p.Human {
		return Presence{}, ErrForbidden
	}
	if err := s.authorize(ctx, p, "pm.connect", ""); err != nil {
		return Presence{}, err
	}
	if !connectionLabel(in.Runner) || !connectionLabel(in.Host) {
		return Presence{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if actor := s.AgentActorID(); actor != "" && actor != p.ActorID {
		return Presence{}, ErrConflict
	}
	_, err := s.store.database().ExecContext(ctx, `INSERT INTO pm_registration(workspace_id,actor_id,runner,host) VALUES(?,?,?,?) ON CONFLICT(workspace_id) DO UPDATE SET actor_id=excluded.actor_id,runner=excluded.runner,host=excluded.host WHERE pm_registration.actor_id=excluded.actor_id OR ?=excluded.actor_id`, p.WorkspaceID, p.ActorID, in.Runner, in.Host, s.cfg.AgentActorID)
	if err != nil {
		return Presence{}, err
	}
	var actor string
	if err = s.store.database().QueryRowContext(ctx, `SELECT actor_id FROM pm_registration WHERE workspace_id=?`, p.WorkspaceID).Scan(&actor); err != nil {
		return Presence{}, err
	}
	s.selected.Store(actor)
	if actor != p.ActorID {
		return Presence{}, ErrConflict
	}
	if err = s.notePresence(ctx, p, "connect"); err != nil {
		return Presence{}, err
	}
	return s.presence(ctx)
}

func (s *Service) RequireOnboarded(ctx context.Context) error {
	out, err := s.presence(ctx)
	if err != nil {
		return err
	}
	if out.State == "not_onboarded" {
		return ErrNotOnboarded
	}
	return nil
}

func (s *Service) Presence(ctx context.Context, p Principal) (Presence, error) {
	if err := s.authorize(ctx, p, "pm.presence", ""); err != nil {
		return Presence{}, err
	}
	return s.presence(ctx)
}

func (s *Service) presence(ctx context.Context) (Presence, error) {
	out := Presence{State: "not_onboarded", Configured: s.AgentActorID() != ""}
	var runner, host sql.NullString
	err := s.store.database().QueryRowContext(ctx, `SELECT p.last_seen_at,p.signal,r.runner,r.host FROM pm_presence p LEFT JOIN pm_registration r ON r.workspace_id=p.workspace_id AND r.actor_id=p.actor_id WHERE p.workspace_id=? AND p.actor_id=?`, s.cfg.WorkspaceID, s.AgentActorID()).Scan(&out.LastSeenAt, &out.Signal, &runner, &host)
	if errors.Is(err, sql.ErrNoRows) {
		var first, last sql.NullString
		err = s.store.database().QueryRowContext(ctx, `SELECT first_seen_at,last_seen_at FROM pm_onboarding_backfill WHERE workspace_id=? AND (actor_id=? OR (actor_id='' AND NOT EXISTS (SELECT 1 FROM pm_registration WHERE workspace_id=?)))`, s.cfg.WorkspaceID, s.AgentActorID(), s.cfg.WorkspaceID).Scan(&first, &last)
		if errors.Is(err, sql.ErrNoRows) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		if first.Valid && last.Valid {
			out.State = "offline"
			out.LastSeenAt = last.String
			out.LastSeen = &out.LastSeenAt
		}
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
	out.State = "offline"
	if out.Connected {
		out.State = "connected"
	}
	out.LastSeen = &out.LastSeenAt
	if runner.Valid {
		out.Runner = &runner.String
	}
	if host.Valid {
		out.Host = &host.String
	}
	return out, nil
}

func (s *Service) notePresence(ctx context.Context, p Principal, signal string) error {
	// This separate projection does not invalidate resource authorization epochs.
	// Throttle idle polls to one write per 15 seconds; heartbeats remain fresh.
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.store.database().ExecContext(ctx, `INSERT INTO pm_presence(workspace_id,actor_id,last_seen_at,signal,first_seen_at) VALUES(?,?,?,?,?) ON CONFLICT(workspace_id,actor_id) DO UPDATE SET first_seen_at=CASE WHEN pm_presence.first_seen_at='' THEN pm_presence.last_seen_at ELSE pm_presence.first_seen_at END,last_seen_at=excluded.last_seen_at,signal=excluded.signal WHERE julianday(excluded.last_seen_at)-julianday(pm_presence.last_seen_at)>=15.0/86400.0`, p.WorkspaceID, p.ActorID, now, signal, now)
	return err
}
