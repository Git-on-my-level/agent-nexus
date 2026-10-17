package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"agent-nexus-core/internal/resourceaccess"
)

type BridgeJob struct {
	Kind    string `json:"kind"`
	Enabled bool   `json:"enabled"`
	Ready   bool   `json:"ready"`
}
type BridgeAgent struct {
	ActorID string      `json:"actor_id"`
	Jobs    []BridgeJob `json:"jobs"`
}
type BridgeCheckIn struct {
	InstanceID     string        `json:"instance_id"`
	Sequence       int64         `json:"sequence"`
	ConfigRevision int64         `json:"config_revision"`
	Version        string        `json:"version"`
	Agents         []BridgeAgent `json:"agents"`
}
type BridgePresence struct {
	HostID     string `json:"host_id"`
	InstanceID string `json:"instance_id"`
	Sequence   int64  `json:"sequence"`
	Version    string `json:"version"`
	LastSeenAt string `json:"last_seen_at"`
	ExpiresAt  string `json:"expires_at"`
}
type BridgeStatus struct {
	HostID          string          `json:"host_id"`
	RevokedAt       *string         `json:"revoked_at"`
	Bridge          *BridgePresence `json:"bridge"`
	Agents          []BridgeAgent   `json:"agents"`
	AgentsTruncated bool            `json:"agents_truncated"`
}

func bridgeASCII(s string, limit int) bool {
	if len(s) == 0 || len(s) > limit {
		return false
	}
	for _, b := range []byte(s) {
		if b < 33 || b > 126 {
			return false
		}
	}
	return true
}

// Host proof has already authenticated the exact bytes and signed timestamp.
// Revalidate enrollment/actors inside the write transaction to close revocation races.
func (s *Store) CheckInHostBridge(ctx context.Context, id, signed string, in BridgeCheckIn) (BridgePresence, error) {
	var out BridgePresence
	at, err := time.Parse(time.RFC3339Nano, signed)
	if err != nil || !bridgeASCII(in.InstanceID, 128) || !bridgeASCII(in.Version, 64) || in.Sequence < 1 || in.ConfigRevision < 0 || in.Agents == nil || len(in.Agents) > 256 {
		return out, ErrInvalidRequest
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var revoked sql.NullString
	if err = tx.QueryRowContext(ctx, `SELECT revoked_at FROM hosts WHERE id=?`, id).Scan(&revoked); err != nil {
		return out, err
	}
	if revoked.Valid {
		return out, ErrHostRevoked
	}
	seen := map[string]bool{}
	actorIDs := []string{}
	jobRows := []map[string]any{}
	for _, a := range in.Agents {
		if !bridgeASCII(a.ActorID, 256) || seen[a.ActorID] || len(a.Jobs) > 5 || a.Jobs == nil {
			return out, ErrInvalidRequest
		}
		seen[a.ActorID] = true
		actorIDs = append(actorIDs, a.ActorID)
		jobs := map[string]bool{}
		for _, j := range a.Jobs {
			if jobs[j.Kind] || j.Ready && !j.Enabled {
				return out, ErrInvalidRequest
			}
			jobs[j.Kind] = true
			jobRows = append(jobRows, map[string]any{"actor_id": a.ActorID, "kind": j.Kind, "enabled": j.Enabled, "ready": j.Ready})
			switch j.Kind {
			case "answers", "wake", "git", "files", "spool":
			default:
				return out, ErrInvalidRequest
			}
		}
	}
	admitted, _ := json.Marshal(actorIDs)
	rows, err := tx.QueryContext(ctx, `SELECT a.actor_id FROM json_each(?) selected CROSS JOIN agents a ON a.actor_id=selected.value JOIN host_agents ha ON ha.agent_id=a.id AND ha.host_id=? WHERE a.revoked_at IS NULL AND NOT EXISTS(SELECT 1 FROM host_exclusions hx WHERE hx.host_id=ha.host_id AND hx.name=ha.name)`, string(admitted), id)
	if err != nil {
		return out, err
	}
	valid := 0
	for rows.Next() {
		var actor string
		if err = rows.Scan(&actor); err != nil {
			rows.Close()
			return out, err
		}
		valid++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if valid != len(actorIDs) {
		return out, ErrAgentExcluded
	}
	var oldInstance, oldSigned string
	var seq int64
	err = tx.QueryRowContext(ctx, `SELECT instance_id,sequence,signed_at FROM host_bridge_presence WHERE host_id=?`, id).Scan(&oldInstance, &seq, &oldSigned)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	if err == nil {
		prev, _ := time.Parse(time.RFC3339Nano, oldSigned)
		if !at.After(prev) || oldInstance == in.InstanceID && in.Sequence <= seq {
			return out, ErrInvalidRequest
		}
	}
	if oldInstance != "" && oldInstance != in.InstanceID {
		var retired int
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM host_bridge_retired_instances WHERE host_id=? AND instance_id=?`, id, in.InstanceID).Scan(&retired)
		if err == nil {
			return out, ErrInvalidRequest
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return out, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO host_bridge_retired_instances(host_id,instance_id) VALUES(?,?)`, id, oldInstance); err != nil {
			return out, err
		}
	}
	now := time.Now().UTC()
	out = BridgePresence{id, in.InstanceID, in.Sequence, in.Version, now.Format(time.RFC3339Nano), now.Add(300 * time.Second).Format(time.RFC3339Nano)}
	_, err = tx.ExecContext(ctx, `INSERT INTO host_bridge_presence(host_id,instance_id,sequence,config_revision,version,signed_at,last_seen_at,expires_at) VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(host_id) DO UPDATE SET instance_id=excluded.instance_id,sequence=excluded.sequence,config_revision=excluded.config_revision,version=excluded.version,signed_at=excluded.signed_at,last_seen_at=excluded.last_seen_at,expires_at=excluded.expires_at`, id, in.InstanceID, in.Sequence, in.ConfigRevision, in.Version, signed, out.LastSeenAt, out.ExpiresAt)
	if err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM host_bridge_jobs WHERE host_id=?`, id); err != nil {
		return out, err
	}
	jobJSON, _ := json.Marshal(jobRows)
	if _, err = tx.ExecContext(ctx, `INSERT INTO host_bridge_jobs(host_id,actor_id,kind,enabled,ready) SELECT ?,json_extract(value,'$.actor_id'),json_extract(value,'$.kind'),json_extract(value,'$.enabled'),json_extract(value,'$.ready') FROM json_each(?)`, id, string(jobJSON)); err != nil {
		return out, err
	}
	return out, tx.Commit()
}

// Explicit IDs admit the host cohort before profile filtering. Per host the PK
// admits at most 33 actor groups (165 jobs); denied groups never cause a refill.
// No roster or workspace history is read. Presence is independent of agent count.
func (s *Store) HostBridgeStatus(ctx context.Context, ids []string) ([]BridgeStatus, error) {
	if len(ids) > 100 {
		return nil, ErrInvalidRequest
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if id == "" || len(id) > 256 || seen[id] {
			return nil, ErrInvalidRequest
		}
		seen[id] = true
	}
	raw, _ := json.Marshal(ids)
	db := resourceaccess.NewDB(s.db)
	rows, err := db.QueryContext(ctx, `SELECT h.id,h.revoked_at,p.instance_id,p.sequence,p.version,p.last_seen_at,p.expires_at FROM json_each(?) selected CROSS JOIN hosts h ON h.id=selected.value LEFT JOIN host_bridge_presence p ON p.host_id=h.id ORDER BY h.id`, string(raw))
	if err != nil {
		return nil, err
	}
	out := []BridgeStatus{}
	positions := map[string]int{}
	for rows.Next() {
		var v BridgeStatus
		var rev, instance, version, last, expiry sql.NullString
		var seq sql.NullInt64
		if err = rows.Scan(&v.HostID, &rev, &instance, &seq, &version, &last, &expiry); err != nil {
			rows.Close()
			return nil, err
		}
		if rev.Valid {
			v.RevokedAt = &rev.String
		}
		v.Agents = []BridgeAgent{}
		if instance.Valid {
			v.Bridge = &BridgePresence{v.HostID, instance.String, seq.Int64, version.String, last.String, expiry.String}
		}
		positions[v.HostID] = len(out)
		out = append(out, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// This query starts at the explicit host IDs and the (host,actor,kind) PK.
	rows, err = db.QueryContext(ctx, `SELECT h.id,j.actor_id,j.kind,j.enabled,j.ready,CASE WHEN ha.agent_id IS NOT NULL AND hx.name IS NULL THEN a.actor_id END FROM json_each(?) selected CROSS JOIN hosts h ON h.id=selected.value CROSS JOIN host_bridge_jobs j ON j.host_id=h.id AND j.actor_id IN (SELECT actor_id FROM host_bridge_jobs admitted WHERE admitted.host_id=h.id GROUP BY actor_id ORDER BY actor_id LIMIT 33) LEFT JOIN agents a ON a.actor_id=j.actor_id AND a.revoked_at IS NULL LEFT JOIN host_agents ha ON ha.agent_id=a.id AND ha.host_id=h.id LEFT JOIN host_exclusions hx ON hx.host_id=ha.host_id AND hx.name=ha.name ORDER BY h.id,j.actor_id,j.kind`, string(raw))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	previous := map[string]string{}
	for rows.Next() {
		var host, actor, kind string
		var enabled, ready bool
		var visible sql.NullString
		if err = rows.Scan(&host, &actor, &kind, &enabled, &ready, &visible); err != nil {
			return nil, err
		}
		i, ok := positions[host]
		if !ok {
			continue
		}
		if previous[host] != actor {
			counts[host]++
			previous[host] = actor
		}
		if counts[host] > 32 {
			out[i].AgentsTruncated = true
			continue
		}
		if !visible.Valid || out[i].RevokedAt != nil {
			continue
		}
		agents := &out[i].Agents
		if len(*agents) == 0 || (*agents)[len(*agents)-1].ActorID != actor {
			*agents = append(*agents, BridgeAgent{actor, []BridgeJob{}})
		}
		n := len(*agents) - 1
		(*agents)[n].Jobs = append((*agents)[n].Jobs, BridgeJob{kind, enabled, ready})
	}
	return out, rows.Err()
}

// SQL fragment for a known host and actor; never derive wake coverage from the
// mere existence of a host answer consumer.
func wakePresenceSQL(host, actor string) string {
	return `(SELECT p.expires_at FROM host_bridge_presence p JOIN host_bridge_jobs j ON j.host_id=p.host_id AND j.actor_id=` + actor + ` AND j.kind='wake' AND j.enabled=1 AND j.ready=1 WHERE p.host_id=` + host + ` AND NOT EXISTS(SELECT 1 FROM host_exclusions hx WHERE hx.host_id=p.host_id AND hx.name=ha.name))`
}

// ListHostSummaries is a bounded machine selector with no roster enrichment.
func (s *Store) ListHostSummaries(ctx context.Context, limit int) ([]Host, error) {
	if limit < 1 || limit > 100 {
		return nil, ErrInvalidRequest
	}
	rows, e := resourceaccess.NewDB(s.db).QueryContext(ctx, `SELECT id,slug,display_name,revoked_at FROM hosts ORDER BY created_at DESC,id LIMIT ?`, limit)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []Host{}
	for rows.Next() {
		var h Host
		var revoked sql.NullString
		if e = rows.Scan(&h.ID, &h.Slug, &h.DisplayName, &revoked); e != nil {
			return nil, e
		}
		h.Ref = "host:" + h.ID
		h.Handle = h.Slug
		h.Agents = []HostAgent{}
		h.ExcludedNames = []string{}
		h.DiscoveredAdapters = []string{}
		if revoked.Valid {
			h.RevokedAt = &revoked.String
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
