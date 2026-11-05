package auth

import (
	"agent-nexus-core/internal/resourceaccess"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"time"
)

// Persona archive.
//
// Agents mint host personas freely (the first host_assertion for a new name
// creates agent, actor and host_agents rows), so a host's roster grows with
// one-off names. Archive is the human answer to that sprawl. It is neither
// revoke (permanent, principal-wide) nor delete (history must keep its
// attribution):
//
//   - host_agents.archived_at is stamped; the agent, actor, events and runs
//     stay untouched, and the (host_id, name) row keeps the name reserved.
//   - In the same transaction every access token and refresh session of the
//     principal is revoked, exactly as an exclusion does.
//   - While archived, AuthenticateAccessToken refuses the principal
//     (ErrAgentArchived), IssueHostAgentToken returns ErrAgentArchived for the
//     name instead of minting or re-creating it, bridge check-ins naming the
//     actor are refused like exclusions, wake presence ignores it, and wake
//     routing treats it as excluded.
//
// Restore clears the stamp. Revoked tokens stay revoked; the next
// host_assertion mints a fresh token for the same actor.

var ErrAgentArchived = errors.New("agent_archived")

const (
	ArchiveReasonManual   = "manual"
	ArchiveReasonInactive = "inactive"

	maxSimilarHostNames      = 5
	similarHostNameScanLimit = 64

	defaultPersonaSweepBatchSize  = 100
	defaultPersonaSweepMaxBatches = 20
)

// HostAgent carries only public identity data. Registered and SimilarNames
// are set by IssueHostAgentToken for the token response and never serialized
// with the agent itself.
type HostAgent struct {
	ID               string   `json:"id"`
	Ref              string   `json:"ref"`
	ActorID          string   `json:"actor_id"`
	HostID           *string  `json:"host_id"`
	HostSlug         *string  `json:"host_slug"`
	Name             string   `json:"name"`
	Handle           string   `json:"handle"`
	DisplayName      string   `json:"display_name"`
	IdentityKind     string   `json:"identity_kind"`
	State            string   `json:"state"`
	BridgeOnline     bool     `json:"bridge_online"`
	CurrentCardRef   *string  `json:"current_card_ref"`
	CurrentCardTitle *string  `json:"current_card_title"`
	LastProgressNote *string  `json:"last_progress_note"`
	LastProgressAt   *string  `json:"last_progress_at"`
	ActiveRun        any      `json:"active_run"`
	OpenAsksCount    int      `json:"open_asks_count"`
	LastSignalAt     *string  `json:"last_signal_at"`
	RevokedAt        *string  `json:"revoked_at"`
	ArchivedAt       *string  `json:"archived_at,omitempty"`
	Registered       bool     `json:"-"`
	SimilarNames     []string `json:"-"`
}

func revokeChildSessions(ctx context.Context, tx Transaction, id, now string) error {
	for _, table := range []string{"auth_access_tokens", "auth_refresh_sessions"} {
		_, err := tx.ExecContext(ctx, `UPDATE `+table+` SET revoked_at=? WHERE agent_id=? AND revoked_at IS NULL`, now, id)
		if err != nil {
			return err
		}
	}
	return nil
}

// KnownAdapterNames are the agent tools core names on its own. They are
// always shown and never archived by the sweep.
var KnownAdapterNames = map[string]bool{"claude": true, "codex": true, "cursor": true, "omp": true, "generic": true}

var personaRevisionSuffix = regexp.MustCompile(`-(?:r|v)?[0-9]+$`)

// PersonaStem drops one trailing revision suffix (-rN, -vN or -N), so
// codex-spkfinal-r2 and codex-spkfinal share the stem codex-spkfinal.
func PersonaStem(name string) string {
	stem := personaRevisionSuffix.ReplaceAllString(name, "")
	if stem == "" {
		return name
	}
	return stem
}

// similarHostNames lists active names on the host that share name's stem.
// It seeks the (host_id, name) primary key from the stem and reads at most
// similarHostNameScanLimit rows. The result is never nil, so callers can use
// nil to mean "nothing was registered".
func similarHostNames(ctx context.Context, tx Transaction, hostID, name string) ([]string, error) {
	out := []string{}
	if KnownAdapterNames[name] {
		return out, nil
	}
	stem := PersonaStem(name)
	// Agent names use [a-z0-9-]; every name with this prefix sorts below stem+"~".
	rows, err := tx.QueryContext(ctx, `SELECT ha.name FROM host_agents ha JOIN agents a ON a.id=ha.agent_id
 WHERE ha.host_id=? AND ha.name>=? AND ha.name<? AND ha.name<>? AND ha.archived_at IS NULL AND a.revoked_at IS NULL
 AND NOT EXISTS (SELECT 1 FROM host_exclusions hx WHERE hx.host_id=ha.host_id AND hx.name=ha.name)
 ORDER BY ha.name LIMIT ?`, hostID, stem, stem+"~", name, similarHostNameScanLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var candidate string
		if err := rows.Scan(&candidate); err != nil {
			return nil, err
		}
		if PersonaStem(candidate) == stem && len(out) < maxSimilarHostNames {
			out = append(out, candidate)
		}
	}
	return out, rows.Err()
}

// ArchiveHostAgent archives one persona on a host. Human auth-admin only.
func (s *Store) ArchiveHostAgent(ctx context.Context, hostRef, name string, admin Principal) (Host, error) {
	return s.setHostAgentArchive(ctx, hostRef, name, admin, true)
}

// RestoreHostAgent clears a persona's archive. Human auth-admin only.
func (s *Store) RestoreHostAgent(ctx context.Context, hostRef, name string, admin Principal) (Host, error) {
	return s.setHostAgentArchive(ctx, hostRef, name, admin, false)
}

func (s *Store) setHostAgentArchive(ctx context.Context, hostRef, name string, admin Principal, archive bool) (Host, error) {
	if !validAgentName(name) {
		return Host{}, ErrInvalidRequest
	}
	h, err := s.GetHost(ctx, hostRef)
	if err != nil {
		return h, err
	}
	tx, err := resourceaccess.NewDB(s.db).BeginTx(ctx, nil)
	if err != nil {
		return h, err
	}
	defer tx.Rollback()
	if err := requireAdministrationTx(ctx, tx, admin, true); err != nil {
		return h, err
	}
	var hostRevoked sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT revoked_at FROM hosts WHERE id=?`, h.ID).Scan(&hostRevoked); err != nil {
		return h, err
	}
	if hostRevoked.Valid {
		return h, ErrHostRevoked
	}
	var agentID, actorID string
	var archived sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT ha.agent_id,a.actor_id,ha.archived_at FROM host_agents ha JOIN agents a ON a.id=ha.agent_id WHERE ha.host_id=? AND ha.name=?`, h.ID, name).Scan(&agentID, &actorID, &archived)
	if errors.Is(err, sql.ErrNoRows) {
		return h, ErrHostNotFound
	}
	if err != nil {
		return h, err
	}
	if archived.Valid == archive {
		return h, nil // already in the requested state
	}
	now := hostNow()
	event := "host_agent_restored"
	if archive {
		event = "host_agent_archived"
		if _, err = tx.ExecContext(ctx, `UPDATE host_agents SET archived_at=?,archived_reason=? WHERE host_id=? AND name=? AND archived_at IS NULL`, now, ArchiveReasonManual, h.ID, name); err != nil {
			return h, err
		}
		if err = revokeChildSessions(ctx, tx, agentID, now); err != nil {
			return h, err
		}
	} else if _, err = tx.ExecContext(ctx, `UPDATE host_agents SET archived_at=NULL,archived_reason=NULL WHERE host_id=? AND name=?`, h.ID, name); err != nil {
		return h, err
	}
	audit := AuthAuditEventInput{EventType: event, ActorAgentID: admin.AgentID, ActorActorID: admin.ActorID, SubjectAgentID: agentID, SubjectActorID: actorID, Metadata: map[string]any{"host_id": h.ID, "name": name}}
	if archive {
		audit.Metadata["reason"] = ArchiveReasonManual
	}
	if err = s.recordAuthAuditEventTx(ctx, tx, audit); err != nil {
		return h, err
	}
	if err = tx.Commit(); err != nil {
		return h, err
	}
	return s.GetHost(ctx, h.ID)
}

// PersonaSweepCursor is the (host_id, name) position after the last persona
// a sweep examined. The zero value starts from the beginning.
type PersonaSweepCursor struct{ HostID, Name string }

// PersonaSweepOptions bounds one sweep. InactiveFor <= 0 disables it.
type PersonaSweepOptions struct {
	InactiveFor time.Duration
	BatchSize   int
	MaxBatches  int
	Now         time.Time
	After       PersonaSweepCursor
}

// PersonaSweepResult reports one sweep. Next is where the following sweep
// continues; Wrapped means the roster end was reached and Next is zero.
type PersonaSweepResult struct {
	Scanned  int
	Archived int
	Next     PersonaSweepCursor
	Wrapped  bool
}

// ArchiveInactivePersonas archives derived personas nobody has used for
// InactiveFor. It reads at most BatchSize*MaxBatches host_agents rows by
// primary-key keyset, each with indexed point lookups, and archives each
// eligible persona in its own short transaction that re-checks inactivity.
//
// A persona is eligible only when all of these hold:
//   - derived (never adopted), not archived, not revoked, host not revoked;
//   - not a known adapter name and not an adapter the host discovered;
//   - the host bridge serves no job for its actor;
//   - its newest access token (kept by the purge) exists and is older than
//     the cutoff, and its presence and newest run are older too.
//
// A persona with no recorded token is never archived: no evidence is not
// evidence of disuse.
func (s *Store) ArchiveInactivePersonas(ctx context.Context, opts PersonaSweepOptions) (PersonaSweepResult, error) {
	result := PersonaSweepResult{Next: opts.After}
	if s == nil || s.db == nil || opts.InactiveFor <= 0 {
		return result, nil
	}
	batch, maxBatches := opts.BatchSize, opts.MaxBatches
	if batch <= 0 {
		batch = defaultPersonaSweepBatchSize
	}
	if maxBatches <= 0 {
		maxBatches = defaultPersonaSweepMaxBatches
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	cutoff := now.UTC().Add(-opts.InactiveFor).Format(time.RFC3339Nano)
	cursor := opts.After
	for i := 0; i < maxBatches; i++ {
		candidates, scanned, last, err := s.personaSweepPage(ctx, cursor, batch, cutoff)
		if err != nil {
			return result, err
		}
		result.Scanned += scanned
		for _, c := range candidates {
			archived, err := s.archiveInactivePersona(ctx, c, cutoff)
			if err != nil {
				return result, err
			}
			if archived {
				result.Archived++
			}
		}
		if scanned < batch {
			result.Next, result.Wrapped = PersonaSweepCursor{}, true
			return result, nil
		}
		cursor = last
		result.Next = cursor
	}
	return result, nil
}

type personaSweepCandidate struct{ hostID, name, agentID, actorID string }

func (s *Store) personaSweepPage(ctx context.Context, after PersonaSweepCursor, limit int, cutoff string) ([]personaSweepCandidate, int, PersonaSweepCursor, error) {
	rows, err := resourceaccess.NewDB(s.db).QueryContext(ctx, `SELECT ha.host_id,ha.name,ha.agent_id,a.actor_id,
 ha.identity_kind='derived' AND ha.archived_at IS NULL AND a.revoked_at IS NULL AND h.revoked_at IS NULL,
 h.discovered_adapters_json,
 COALESCE((SELECT MAX(t.created_at) FROM auth_access_tokens t WHERE t.agent_id=ha.agent_id),''),
 EXISTS(SELECT 1 FROM host_bridge_jobs j WHERE j.host_id=ha.host_id AND j.actor_id=a.actor_id),
 COALESCE((SELECT p.observed_at FROM agent_presence p WHERE p.agent_id=ha.agent_id),''),
 COALESCE((SELECT MAX(r.last_observed_at) FROM runs r WHERE r.agent_id=ha.agent_id),'')
 FROM host_agents ha JOIN agents a ON a.id=ha.agent_id JOIN hosts h ON h.id=ha.host_id
 WHERE ha.host_id>? OR (ha.host_id=? AND ha.name>?)
 ORDER BY ha.host_id,ha.name LIMIT ?`, after.HostID, after.HostID, after.Name, limit)
	if err != nil {
		return nil, 0, after, err
	}
	defer rows.Close()
	out := []personaSweepCandidate{}
	scanned, last := 0, after
	for rows.Next() {
		var c personaSweepCandidate
		var live, served bool
		var adapters, lastUsed, presence, lastRun string
		if err := rows.Scan(&c.hostID, &c.name, &c.agentID, &c.actorID, &live, &adapters, &lastUsed, &served, &presence, &lastRun); err != nil {
			return nil, 0, after, err
		}
		scanned++
		last = PersonaSweepCursor{HostID: c.hostID, Name: c.name}
		if !live || served || KnownAdapterNames[c.name] || discoveredAdapter(adapters, c.name) {
			continue
		}
		if !olderThan(lastUsed, cutoff) || presence != "" && !olderThan(presence, cutoff) || lastRun != "" && !olderThan(lastRun, cutoff) {
			continue
		}
		out = append(out, c)
	}
	return out, scanned, last, rows.Err()
}

func discoveredAdapter(raw, name string) bool {
	var adapters []string
	if json.Unmarshal([]byte(raw), &adapters) != nil {
		return true // unreadable: treat as an adapter and leave it alone
	}
	for _, adapter := range adapters {
		if adapter == name {
			return true
		}
	}
	return false
}

// olderThan reports whether an RFC3339 timestamp is before cutoff. An empty
// or unparseable value is never older.
func olderThan(raw, cutoff string) bool {
	at, err := time.Parse(time.RFC3339Nano, raw)
	limit, cutErr := time.Parse(time.RFC3339Nano, cutoff)
	return err == nil && cutErr == nil && at.Before(limit)
}

// archiveInactivePersona archives one candidate if, inside the write
// transaction, it is still unarchived and its newest token is still older
// than the cutoff. Token issue for the name runs in its own write
// transaction, so a persona used after the page was read is never archived.
func (s *Store) archiveInactivePersona(ctx context.Context, c personaSweepCandidate, cutoff string) (bool, error) {
	tx, err := resourceaccess.NewDB(s.db).BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var lastUsed string
	var archived, revoked sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT ha.archived_at,a.revoked_at,COALESCE((SELECT MAX(t.created_at) FROM auth_access_tokens t WHERE t.agent_id=ha.agent_id),'') FROM host_agents ha JOIN agents a ON a.id=ha.agent_id WHERE ha.host_id=? AND ha.name=? AND ha.agent_id=?`, c.hostID, c.name, c.agentID).Scan(&archived, &revoked, &lastUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if archived.Valid || revoked.Valid || !olderThan(lastUsed, cutoff) {
		return false, nil
	}
	now := hostNow()
	if _, err = tx.ExecContext(ctx, `UPDATE host_agents SET archived_at=?,archived_reason=? WHERE host_id=? AND name=? AND archived_at IS NULL`, now, ArchiveReasonInactive, c.hostID, c.name); err != nil {
		return false, err
	}
	if err = revokeChildSessions(ctx, tx, c.agentID, now); err != nil {
		return false, err
	}
	if err = s.recordAuthAuditEventTx(ctx, tx, AuthAuditEventInput{EventType: "host_agent_archived", SubjectAgentID: c.agentID, SubjectActorID: c.actorID, Metadata: map[string]any{"host_id": c.hostID, "name": c.name, "reason": ArchiveReasonInactive, "last_used_at": lastUsed}}); err != nil {
		return false, err
	}
	return true, tx.Commit()
}
