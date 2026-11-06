package primitives

import (
	"context"

	"agent-nexus-core/internal/resourceaccess"
)

// trendsHumanReaderSQL reports whether the reader is an active human principal:
// one unique-index seek on agents(actor_id). Every active human is admitted to
// Watch Trends. Humans already administer the workspace (auth admins), Trends
// returns aggregates only, and a person must never be locked out of their own
// workspace's trends by an item someone else, or an agent, keeps private.
// Item-level reads, candidate admission and mutations keep the exact-owner rule.
var trendsHumanReaderSQL = `SELECT EXISTS(SELECT 1 FROM main.agents r WHERE r.actor_id=? AND r.revoked_at IS NULL AND ` + resourceaccess.HumanPrincipalSQL("r") + `)`

// trendsAdmissionSQL is the admission probe for a non-human reader: it returns
// (active, ledger) from admissionLedgerCTE, so an agent is admitted only when
// no root is denied to it (the selected PM sees every conditional root).
func trendsAdmissionSQL(scope AccessScope) (string, []any) {
	return `WITH ` + admissionLedgerCTE(scope) + ` SELECT active,ledger FROM _anx_admission_ledger`, nil
}

// trendsAdmit admits every active human, and otherwise an agent with no denied
// root, inside the calculation's pinned read snapshot.
func trendsAdmit(ctx context.Context, db resourceaccess.QueryRower, scope AccessScope) error {
	if scope.ActorID != "" {
		var human bool
		if err := db.QueryRowContext(ctx, trendsHumanReaderSQL, scope.ActorID).Scan(&human); err != nil {
			return err
		}
		if human {
			return nil
		}
	}
	q, args := trendsAdmissionSQL(scope)
	var active bool
	var ledger int
	if err := db.QueryRowContext(ctx, q, args...).Scan(&active, &ledger); err != nil {
		return err
	}
	if active || ledger < 1 {
		return ErrForbidden
	}
	return nil
}
