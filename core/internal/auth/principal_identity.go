package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"agent-nexus-core/internal/resourceaccess"
)

// PrincipalForActor routes directly to the newest non-revoked identity, matching
// ListPrincipals order. Routing never supplies cached authority: the selected
// principal is freshly loaded through the normal authority reader.
func (s *Store) PrincipalForActor(ctx context.Context, actorID string) (AuthPrincipalSummary, error) {
	if s == nil || s.db == nil {
		return AuthPrincipalSummary{}, fmt.Errorf("auth store database is not initialized")
	}
	var id string
	err := resourceaccess.NewDB(s.db).QueryRowContext(ctx, `SELECT id FROM agents WHERE actor_id=? AND revoked_at IS NULL ORDER BY created_at DESC,id DESC LIMIT 1`, actorID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return AuthPrincipalSummary{}, ErrAgentNotFound
	}
	if err != nil {
		return AuthPrincipalSummary{}, fmt.Errorf("route auth principal identity: %w", err)
	}
	principal, err := s.GetPrincipalSummary(ctx, id)
	if err != nil {
		return AuthPrincipalSummary{}, err
	}
	if principal.ActorID != actorID || principal.Revoked {
		return AuthPrincipalSummary{}, ErrAgentNotFound
	}
	selected := []AuthPrincipalSummary{principal}
	if err := s.enrichPrincipalHosts(ctx, selected); err != nil {
		return AuthPrincipalSummary{}, err
	}
	return selected[0], nil
}
