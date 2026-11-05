package storage

import (
	"context"
	"database/sql"
	"fmt"

	"agent-nexus-core/internal/resourceaccess"
)

// installHostSponsors records the human each host acts for (see the sponsor
// rules in internal/auth/host_sponsors.go): the issuer of a headless
// enrollment token, or a device-enrollment approver who was the workspace's
// sole active human. Only humans sponsor. The sponsor is fixed at enrollment.
//
// Existing hosts are backfilled once from the audit log, because completed
// host_enrollments rows are deleted an hour after creation. Historical device
// approvals carry no proof the approver operated the host, so they are
// backfilled only when exactly one human was active both at approval and at
// completion (judged from principal created_at/revoked_at). A host
// whose sponsor cannot be proven stays unsponsored: that only withholds
// delegation (fail closed).
func installHostSponsors(ctx context.Context, tx *sql.Tx) error {
	exec := func(q string, args ...any) error {
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return fmt.Errorf("host sponsors: %w", err)
		}
		return nil
	}
	if err := exec(`CREATE TABLE IF NOT EXISTS host_sponsors(host_id TEXT PRIMARY KEY REFERENCES hosts(id) ON DELETE CASCADE,sponsor_actor_id TEXT NOT NULL CHECK(sponsor_actor_id<>''),source TEXT NOT NULL CHECK(source IN ('device_approval','enrollment_token')),recorded_at TEXT NOT NULL) WITHOUT ROWID`); err != nil {
		return err
	}
	if err := exec(`CREATE INDEX IF NOT EXISTS idx_host_sponsors_sponsor ON host_sponsors(sponsor_actor_id,host_id)`); err != nil {
		return err
	}
	has, err := sqliteTableHasColumn(ctx, tx, "host_enrollments", "approved_by_actor_id")
	if err != nil {
		return err
	}
	if !has {
		if err := exec(`ALTER TABLE host_enrollments ADD COLUMN approved_by_actor_id TEXT`); err != nil {
			return err
		}
	}
	now := `strftime('%Y-%m-%dT%H:%M:%fZ','now')`
	// Approvals still awaiting completion keep their approver for the live path.
	if err := exec(`UPDATE host_enrollments SET approved_by_actor_id=(
 SELECT a.actor_id FROM auth_audit_events ev JOIN agents a ON a.id=ev.actor_agent_id AND a.actor_id=ev.actor_actor_id
 WHERE ev.event_type='host_enroll_approved' AND json_extract(ev.metadata_json,'$.enrollment_id')=host_enrollments.id AND a.revoked_at IS NULL AND ` + resourceaccess.HumanPrincipalSQL("a") + `
  AND ` + resourceaccess.SoleHumanSQL("ev.occurred_at") + `
 ORDER BY ev.occurred_at DESC,ev.id DESC LIMIT 1)
 WHERE approved_by_actor_id IS NULL AND status='approved'`); err != nil {
		return err
	}
	// Headless enrollment: the human who issued the consumed token.
	if err := exec(`INSERT OR IGNORE INTO host_sponsors(host_id,sponsor_actor_id,source,recorded_at)
 SELECT h.id,a.actor_id,'enrollment_token',` + now + ` FROM auth_audit_events ev
 JOIN hosts h ON h.id=json_extract(ev.metadata_json,'$.host_id')
 JOIN host_enrollment_tokens t ON t.id=json_extract(ev.metadata_json,'$.token_id')
 JOIN agents a ON a.id=t.created_by_agent_id
 WHERE ev.event_type='host_enrollment_token_consumed' AND a.actor_id<>'' AND ` + resourceaccess.HumanPrincipalSQL("a")); err != nil {
		return err
	}
	// Device flow, single-human workspaces only: pair each host_enroll_completed
	// with the latest approval of the same slug that precedes it within the
	// ten-minute enrollment lifetime (plus a minute of clock slack). Hosts that
	// consumed a token are excluded (already sponsored or deliberately not).
	return exec(`INSERT OR IGNORE INTO host_sponsors(host_id,sponsor_actor_id,source,recorded_at)
 SELECT hs.id,(SELECT a.actor_id FROM auth_audit_events ap JOIN agents a ON a.id=ap.actor_agent_id AND a.actor_id=ap.actor_actor_id
   WHERE ap.event_type='host_enroll_approved' AND json_extract(ap.metadata_json,'$.slug')=json_extract(c.metadata_json,'$.slug')
    AND julianday(ap.occurred_at)<=julianday(c.occurred_at) AND julianday(c.occurred_at)-julianday(ap.occurred_at)<=11.0/1440
    AND a.actor_id<>'' AND a.revoked_at IS NULL AND ` + resourceaccess.HumanPrincipalSQL("a") + `
    AND ` + resourceaccess.SoleHumanSQL("ap.occurred_at") + ` AND ` + resourceaccess.SoleHumanSQL("c.occurred_at") + `
   ORDER BY ap.occurred_at DESC,ap.id DESC LIMIT 1) AS sponsor,'device_approval',` + now + `
 FROM auth_audit_events c JOIN hosts hs ON hs.id=json_extract(c.metadata_json,'$.host_id')
 WHERE c.event_type='host_enroll_completed' AND sponsor IS NOT NULL
  AND NOT EXISTS(SELECT 1 FROM auth_audit_events t WHERE t.event_type='host_enrollment_token_consumed' AND json_extract(t.metadata_json,'$.host_id')=hs.id)`)
}
