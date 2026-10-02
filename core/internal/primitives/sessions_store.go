package primitives

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

// SessionActivityLease is liveness evidence, never an ownership or dispatch lock.
const SessionActivityLease = 120 * time.Second
const maxSessionSequence int64 = 9007199254740991

var ErrInvalidSessionRequest = errors.New("invalid session request")
var sessionHashPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

type SessionCapabilities struct {
	Resume  string `json:"resume"`
	History string `json:"history"`
	Logs    string `json:"logs"`
}
type SessionRegistration struct {
	Provider            string              `json:"provider"`
	HostScope           string              `json:"host_scope,omitempty"`
	NativeSessionID     string              `json:"native_session_id"`
	NativeSessionIDKind string              `json:"native_session_id_kind,omitempty"`
	Capabilities        SessionCapabilities `json:"capabilities"`
	Activity            string              `json:"activity"`
	Sequence            *int64              `json:"sequence"`
}
type AgentSession struct {
	SessionID           string              `json:"session_id"`
	AgentID             string              `json:"agent_id"`
	ActorID             string              `json:"actor_id"`
	Provider            string              `json:"provider"`
	HostScope           string              `json:"host_scope"`
	NativeSessionID     string              `json:"native_session_id"`
	NativeSessionIDKind string              `json:"native_session_id_kind"`
	Capabilities        SessionCapabilities `json:"capabilities"`
	Sequence            int64               `json:"sequence"`
	Activity            string              `json:"activity"`
	Active              bool                `json:"active"`
	CreatedAt           string              `json:"created_at"`
	LastSeenAt          string              `json:"last_seen_at"`
	ExpiresAt           string              `json:"expires_at"`
}
type WorkParticipantRegistration struct {
	SessionID string `json:"session_id"`
	Activity  string `json:"activity"`
	Sequence  *int64 `json:"sequence"`
}
type WorkParticipant struct {
	ParticipantID string `json:"participant_id"`
	AgentID       string `json:"agent_id"`
	ActorID       string `json:"actor_id"`
	SessionID     string `json:"session_id,omitempty"`
	Sequence      int64  `json:"sequence"`
	Activity      string `json:"activity"`
	Active        bool   `json:"active"`
	CreatedAt     string `json:"created_at"`
	LastSeenAt    string `json:"last_seen_at"`
	ExpiresAt     string `json:"expires_at"`
}
type WorkParticipantPage struct {
	Participants []WorkParticipant `json:"participants"`
	NextCursor   string            `json:"next_cursor"`
}

func sessionInvalid(message string) error {
	return fmt.Errorf("%w: %s", ErrInvalidSessionRequest, message)
}
func sessionString(v string, max int) bool {
	return v != "" && v == strings.TrimSpace(v) && len(v) <= max && !strings.ContainsFunc(v, unicode.IsControl)
}
func validSessionSequence(v *int64) bool { return v != nil && *v >= 0 && *v <= maxSessionSequence }
func capabilityState(v string) bool      { return v == "supported" || v == "unsupported" || v == "unknown" }
func sessionActivityAt(activity, expiry string, now time.Time) string {
	if activity == "closed" || activity == "left" {
		return activity
	}
	expires, err := time.Parse(time.RFC3339Nano, expiry)
	if err != nil || !expires.After(now) {
		return "stale"
	}
	return activity
}

// UpsertSession binds only metadata to an existing authenticated identity. An
// enrolled agent cannot choose another host's namespace. No auth state is written.
func (s *Store) UpsertSession(ctx context.Context, agentID, actorID string, in SessionRegistration) (AgentSession, error) {
	if !sessionString(in.Provider, 64) || !sessionString(in.NativeSessionID, 512) || !validSessionSequence(in.Sequence) {
		return AgentSession{}, sessionInvalid("provider, native_session_id, and nonnegative sequence are required and must respect length limits")
	}
	if in.Activity != "active" && in.Activity != "idle" && in.Activity != "closed" {
		return AgentSession{}, sessionInvalid("activity must be active, idle, or closed")
	}
	if !capabilityState(in.Capabilities.Resume) || !capabilityState(in.Capabilities.History) || !capabilityState(in.Capabilities.Logs) {
		return AgentSession{}, sessionInvalid("resume, history, and logs capabilities must each be supported, unsupported, or unknown")
	}
	if in.NativeSessionIDKind == "" {
		in.NativeSessionIDKind = "opaque"
	}
	if in.NativeSessionIDKind != "opaque" && in.NativeSessionIDKind != "provider_session_sha256" {
		return AgentSession{}, sessionInvalid("native_session_id_kind must be opaque or provider_session_sha256")
	}
	if in.NativeSessionIDKind == "provider_session_sha256" && !sessionHashPattern.MatchString(in.NativeSessionID) {
		return AgentSession{}, sessionInvalid("provider_session_sha256 requires sha256: followed by 64 lowercase hexadecimal characters")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AgentSession{}, err
	}
	defer tx.Rollback()
	var hostID, hostSlug string
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(ha.host_id,''),COALESCE(h.slug,'') FROM agents a LEFT JOIN host_agents ha ON ha.agent_id=a.id LEFT JOIN hosts h ON h.id=ha.host_id WHERE a.id=? AND a.actor_id=? AND a.revoked_at IS NULL AND h.revoked_at IS NULL`, agentID, actorID).Scan(&hostID, &hostSlug)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentSession{}, ErrForbidden
	}
	if err != nil {
		return AgentSession{}, err
	}
	if hostID != "" {
		if in.HostScope != "" && in.HostScope != hostID && in.HostScope != hostSlug {
			return AgentSession{}, ErrForbidden
		}
		in.HostScope = hostID
	}
	if !sessionString(in.HostScope, 128) {
		return AgentSession{}, sessionInvalid("host_scope is required for standalone agents and must be at most 128 bytes")
	}
	rawBytes, _ := json.Marshal(in)
	raw := string(rawBytes)
	var id, oldRaw, oldActivity, oldKind string
	var seq int64
	err = tx.QueryRowContext(ctx, `SELECT id,sequence,request_json,activity,native_session_id_kind FROM agent_sessions WHERE agent_id=? AND provider=? AND host_scope=? AND native_session_id=?`, agentID, in.Provider, in.HostScope, in.NativeSessionID).Scan(&id, &seq, &oldRaw, &oldActivity, &oldKind)
	now := time.Now().UTC()
	stamp := now.Format(time.RFC3339Nano)
	expiry := now.Add(SessionActivityLease).Format(time.RFC3339Nano)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		id = uuid.NewString()
		capabilities, _ := json.Marshal(in.Capabilities)
		_, err = tx.ExecContext(ctx, `INSERT INTO agent_sessions(id,agent_id,actor_id,provider,host_scope,native_session_id,native_session_id_kind,capabilities_json,activity,sequence,request_json,created_at,last_seen_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, id, agentID, actorID, in.Provider, in.HostScope, in.NativeSessionID, in.NativeSessionIDKind, string(capabilities), in.Activity, *in.Sequence, raw, stamp, stamp, expiry)
	case err != nil:
		return AgentSession{}, err
	case *in.Sequence == seq && raw == oldRaw: // Exact retry does not renew liveness.
	case *in.Sequence <= seq || oldActivity == "closed" || in.NativeSessionIDKind != oldKind:
		return AgentSession{}, ErrConflict
	default:
		capabilities, _ := json.Marshal(in.Capabilities)
		_, err = tx.ExecContext(ctx, `UPDATE agent_sessions SET capabilities_json=?,activity=?,sequence=?,request_json=?,last_seen_at=?,expires_at=? WHERE id=?`, string(capabilities), in.Activity, *in.Sequence, raw, stamp, expiry, id)
	}
	if err != nil {
		return AgentSession{}, err
	}
	if err = tx.Commit(); err != nil {
		return AgentSession{}, err
	}
	return s.GetSession(ctx, agentID, id)
}

// GetSession intentionally has no workspace-wide counterpart: session identity
// and runtime capabilities remain private even when a task participation is shared.
func (s *Store) GetSession(ctx context.Context, agentID, id string) (AgentSession, error) {
	var v AgentSession
	var capabilities string
	err := s.db.QueryRowContext(ctx, `SELECT id,agent_id,actor_id,provider,host_scope,native_session_id,native_session_id_kind,capabilities_json,sequence,activity,created_at,last_seen_at,expires_at FROM agent_sessions WHERE id=? AND agent_id=?`, id, agentID).Scan(&v.SessionID, &v.AgentID, &v.ActorID, &v.Provider, &v.HostScope, &v.NativeSessionID, &v.NativeSessionIDKind, &capabilities, &v.Sequence, &v.Activity, &v.CreatedAt, &v.LastSeenAt, &v.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	if err = json.Unmarshal([]byte(capabilities), &v.Capabilities); err != nil {
		return v, err
	}
	v.Activity = sessionActivityAt(v.Activity, v.ExpiresAt, time.Now().UTC())
	v.Active = v.Activity == "active"
	return v, nil
}

func (s *Store) UpsertWorkParticipant(ctx context.Context, agentID, cardID string, in WorkParticipantRegistration) (WorkParticipant, error) {
	if !sessionString(in.SessionID, 128) || !validSessionSequence(in.Sequence) {
		return WorkParticipant{}, sessionInvalid("session_id and nonnegative sequence are required")
	}
	if in.Activity != "active" && in.Activity != "idle" && in.Activity != "left" {
		return WorkParticipant{}, sessionInvalid("activity must be active, idle, or left")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return WorkParticipant{}, err
	}
	defer tx.Rollback()
	var activity string
	err = tx.QueryRowContext(ctx, `SELECT activity FROM agent_sessions WHERE id=? AND agent_id=?`, in.SessionID, agentID).Scan(&activity)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkParticipant{}, ErrNotFound
	}
	if err != nil {
		return WorkParticipant{}, err
	}
	var exists int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM cards WHERE id=? AND trashed_at IS NULL`, cardID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return WorkParticipant{}, ErrNotFound
	}
	if err != nil {
		return WorkParticipant{}, err
	}
	rawBytes, _ := json.Marshal(in)
	raw := string(rawBytes)
	var id, oldRaw string
	var seq int64
	err = tx.QueryRowContext(ctx, `SELECT id,sequence,request_json FROM work_participants WHERE card_id=? AND session_id=?`, cardID, in.SessionID).Scan(&id, &seq, &oldRaw)
	now := time.Now().UTC()
	stamp := now.Format(time.RFC3339Nano)
	expiry := now.Add(SessionActivityLease).Format(time.RFC3339Nano)
	switch {
	case err == nil && *in.Sequence == seq && raw == oldRaw: // Replay remains valid after session closure, with closed projection.
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return WorkParticipant{}, err
	case err == nil && *in.Sequence <= seq:
		return WorkParticipant{}, ErrConflict
	case activity == "closed" && in.Activity != "left":
		return WorkParticipant{}, ErrConflict
	case errors.Is(err, sql.ErrNoRows):
		id = uuid.NewString()
		_, err = tx.ExecContext(ctx, `INSERT INTO work_participants(id,card_id,session_id,activity,sequence,request_json,created_at,last_seen_at,expires_at) VALUES(?,?,?,?,?,?,?,?,?)`, id, cardID, in.SessionID, in.Activity, *in.Sequence, raw, stamp, stamp, expiry)
	default:
		_, err = tx.ExecContext(ctx, `UPDATE work_participants SET activity=?,sequence=?,request_json=?,last_seen_at=?,expires_at=? WHERE id=?`, in.Activity, *in.Sequence, raw, stamp, expiry, id)
	}
	if err != nil {
		return WorkParticipant{}, err
	}
	if err = tx.Commit(); err != nil {
		return WorkParticipant{}, err
	}
	return s.getWorkParticipant(ctx, agentID, cardID, id)
}

const participantSelect = `SELECT p.id,s.agent_id,s.actor_id,s.id,p.sequence,p.activity,p.created_at,p.last_seen_at,p.expires_at,s.activity,s.expires_at FROM work_participants p JOIN agent_sessions s ON s.id=p.session_id`

func scanWorkParticipant(row interface{ Scan(...any) error }, viewer string, now time.Time) (WorkParticipant, error) {
	var v WorkParticipant
	var sessionActivity, sessionExpiry string
	err := row.Scan(&v.ParticipantID, &v.AgentID, &v.ActorID, &v.SessionID, &v.Sequence, &v.Activity, &v.CreatedAt, &v.LastSeenAt, &v.ExpiresAt, &sessionActivity, &sessionExpiry)
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	if err != nil {
		return v, err
	}
	participantExpiry, pe := time.Parse(time.RFC3339Nano, v.ExpiresAt)
	sessionTime, se := time.Parse(time.RFC3339Nano, sessionExpiry)
	if pe != nil || se != nil {
		return v, fmt.Errorf("invalid activity lease timestamp")
	}
	if sessionTime.Before(participantExpiry) {
		v.ExpiresAt = sessionExpiry
	}
	if v.Activity != "left" && sessionActivity == "closed" {
		v.Activity = "closed"
	} else if v.Activity == "active" && sessionActivity == "idle" {
		v.Activity = "idle"
	}
	v.Activity = sessionActivityAt(v.Activity, v.ExpiresAt, now)
	v.Active = v.Activity == "active"
	if v.AgentID != viewer {
		v.SessionID = ""
	}
	return v, nil
}
func (s *Store) getWorkParticipant(ctx context.Context, viewer, cardID, id string) (WorkParticipant, error) {
	return scanWorkParticipant(s.db.QueryRowContext(ctx, participantSelect+` WHERE p.card_id=? AND p.id=?`, cardID, id), viewer, time.Now().UTC())
}
func (s *Store) ListWorkParticipants(ctx context.Context, viewer, cardID string, limit int, cursor string) (WorkParticipantPage, error) {
	page := WorkParticipantPage{Participants: []WorkParticipant{}}
	if limit < 1 || limit > 200 {
		return page, sessionInvalid("limit must be 1..200")
	}
	after := ""
	if cursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil {
			return page, ErrInvalidCursor
		}
		parts := strings.Split(string(decoded), "\x00")
		if len(parts) != 2 || parts[0] != cardID || parts[1] == "" {
			return page, ErrInvalidCursor
		}
		after = parts[1]
	}
	rows, err := s.db.QueryContext(ctx, participantSelect+` WHERE p.card_id=? AND p.id>? ORDER BY p.id LIMIT ?`, cardID, after, limit+1)
	if err != nil {
		return page, err
	}
	defer rows.Close()
	now := time.Now().UTC()
	for rows.Next() {
		v, err := scanWorkParticipant(rows, viewer, now)
		if err != nil {
			return page, err
		}
		page.Participants = append(page.Participants, v)
	}
	if err = rows.Err(); err != nil {
		return page, err
	}
	if len(page.Participants) > limit {
		page.Participants = page.Participants[:limit]
		page.NextCursor = base64.RawURLEncoding.EncodeToString([]byte(cardID + "\x00" + page.Participants[limit-1].ParticipantID))
	}
	return page, nil
}
