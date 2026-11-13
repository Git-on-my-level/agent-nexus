package auth

import (
	"agent-nexus-core/internal/resourceaccess"
	"context"
	"database/sql"
	"errors"
)

// RequireAskCuratorTx checks current durable authority in the business write
// transaction. Profile visibility must not change the authority decision.
func RequireAskCuratorTx(ctx context.Context, tx Transaction, actor Principal, requester string) error {
	if actor.SeriesAdapter != "" {
		return ErrAuthAdminRequired
	}
	if actor.ActorID != requester {
		return requireAdministrationTx(ctx, tx, actor, false)
	}
	var id string
	err := tx.QueryRowContext(resourceaccess.WithoutPolicy(ctx), `SELECT id FROM agents WHERE id=? AND actor_id=? AND revoked_at IS NULL`, actor.AgentID, actor.ActorID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAuthAdminRequired
	}
	return err
}

// RequireAskRetirerTx admits any live member, human or agent, to retire an
// ask as stale. Retirement is undoable, audited and announced to the
// requester, so it needs no special authority; rewriting an ask still does
// (RequireAskCuratorTx). A series adapter token is never a member.
func RequireAskRetirerTx(ctx context.Context, tx Transaction, actor Principal) error {
	if actor.SeriesAdapter != "" {
		return ErrAuthAdminRequired
	}
	var id string
	err := tx.QueryRowContext(resourceaccess.WithoutPolicy(ctx), `SELECT id FROM agents WHERE id=? AND actor_id=? AND revoked_at IS NULL`, actor.AgentID, actor.ActorID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrAuthAdminRequired
	}
	return err
}
