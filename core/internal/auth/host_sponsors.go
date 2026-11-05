package auth

import (
	"context"

	"agent-nexus-core/internal/resourceaccess"
)

// A host's sponsor is the human the host's agents and personas act for. Core
// uses it only to let Watch Trends admit the sponsor despite private resources
// owned by those agents (see core/docs/resource-authorization.md); it grants no
// item access. The sponsor is set once, when the host is created, and is never
// transferred. Only a human sponsors; an auth-admin agent sponsors nothing.
//
//   - Headless enrollment: the human who issued the consumed token. They minted
//     the host's credential, so the host acts for them.
//   - Device enrollment: the approver, but only when the approver is the
//     workspace's sole active human at approval time. Any human may approve
//     any pending enrollment by ID, and the pending list shows every user
//     code, so neither approval nor a typed code proves the approver operates
//     the host. With one human there is nobody else the host could act for.
//     Approval still admits the host in every case; it just records no sponsor.

// approverActorSQL resolves the approver's actor ID when the decision is an
// approval by an active human who is the only active human principal, else
// NULL. Arguments: approve (bool), agent ID, actor ID.
const approverActorSQL = `(SELECT a.actor_id FROM agents a WHERE ?<>0 AND a.id=? AND a.actor_id=? AND a.actor_id<>'' AND a.revoked_at IS NULL AND `

func approverActor() string {
	return approverActorSQL + resourceaccess.HumanPrincipalSQL("a") + ` AND ` + resourceaccess.SoleHumanSQL("") + `)`
}

// completeEnrollmentTx marks a device enrollment completed and records its
// recorded approver, if any, as the new host's sponsor. The sole-human
// condition is checked again here, so a second human who joins between
// approval and completion cancels the sponsorship. Recording a sponsor grants
// nothing by itself: every Trends request re-checks, live, that the reader is
// still an active human (trendsAdmissionSQL).
func completeEnrollmentTx(ctx context.Context, tx Transaction, enrollmentID, hostID string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE host_enrollments SET status='completed',completed_host_id=? WHERE id=?`, hostID, enrollmentID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO host_sponsors(host_id,sponsor_actor_id,source,recorded_at)
 SELECT ?,approved_by_actor_id,'device_approval',? FROM host_enrollments WHERE id=? AND COALESCE(approved_by_actor_id,'')<>'' AND `+resourceaccess.SoleHumanSQL(""), hostID, hostNow(), enrollmentID)
	return err
}

// consumeEnrollmentTokenTx consumes a headless enrollment token and records its
// issuer as the new host's sponsor when the issuer is an active human.
func consumeEnrollmentTokenTx(ctx context.Context, tx Transaction, tokenID, hostID string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE host_enrollment_tokens SET consumed_at=? WHERE id=? AND consumed_at IS NULL`, hostNow(), tokenID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO host_sponsors(host_id,sponsor_actor_id,source,recorded_at)
 SELECT ?,a.actor_id,'enrollment_token',? FROM host_enrollment_tokens t JOIN agents a ON a.id=t.created_by_agent_id
 WHERE t.id=? AND a.actor_id<>'' AND a.revoked_at IS NULL AND `+resourceaccess.HumanPrincipalSQL("a"), hostID, hostNow(), tokenID)
	return err
}
