package primitives

import (
	"context"

	"agent-nexus-core/internal/resourceaccess"
)

// ScopeInboxLegacyReadSQL names the ordinary legacy inbox relation for the
// off-request comparison worker. It includes recipient, report-review and
// lifecycle eligibility plus the current ownership policy. This oracle can
// construct the expensive legacy graph and MUST NOT run on a certified request.
// It proves base-row eligibility, not HTTP enrichment or freshness parity.
// The authenticated scope supplies the recipient; callers cannot substitute it.
func ScopeInboxLegacyReadSQL(ctx context.Context) (string, []any, error) {
	scope, ok := accessScopeFrom(ctx)
	if !ok {
		return "", nil, ErrScopeInboxProjection
	}
	// Rebuild the policy from the scope so a surrounding callback cannot
	// suppress ownership rewriting while leaving the identity context intact.
	ctx = WithAccessScope(ctx, scope)
	query := `SELECT i.id FROM derived_inbox_items i WHERE
 (COALESCE(json_extract(i.data_json,'$.recipient_actor_id'),'')='' OR json_extract(i.data_json,'$.recipient_actor_id')=?)
 AND (` + currentReportReviewSQL + `) AND (` + inboxReadSQL(ctx) + `)`
	return resourceaccess.ReadQuery(ctx, query), []any{scope.ActorID}, nil
}
