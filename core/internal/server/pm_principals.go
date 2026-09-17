package server

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/pm"
)

type pmPrincipalStore interface {
	ListPrincipals(context.Context, auth.AuthPrincipalListFilter) ([]auth.AuthPrincipalSummary, string, error)
	GetPrincipalSummary(context.Context, string) (auth.AuthPrincipalSummary, error)
}

type pmPrincipalIdentity struct {
	agentID string
	expires time.Time
}

// Cache only identity routing, never authority or wake-routing state. Every hit
// reloads the principal from auth so revocation takes effect on the next check,
// including revocations committed by another Store or database connection.
type pmPrincipalLookup struct {
	mu         sync.Mutex
	store      pmPrincipalStore
	identities map[string]pmPrincipalIdentity
	now        func() time.Time
}

func newPMPrincipalLookup(store pmPrincipalStore) *pmPrincipalLookup {
	return &pmPrincipalLookup{store: store, identities: make(map[string]pmPrincipalIdentity), now: time.Now}
}

func (l *pmPrincipalLookup) find(ctx context.Context, actorID string) (auth.AuthPrincipalSummary, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if cached, ok := l.identities[actorID]; ok {
		if now.Before(cached.expires) {
			principal, err := l.store.GetPrincipalSummary(ctx, cached.agentID)
			if err == nil && principal.ActorID == actorID && !principal.Revoked {
				return principal, nil
			}
			delete(l.identities, actorID)
			if err != nil {
				return auth.AuthPrincipalSummary{}, err
			}
		} else {
			delete(l.identities, actorID)
		}
	}
	limit, cursor := 200, ""
	for {
		items, next, err := l.store.ListPrincipals(ctx, auth.AuthPrincipalListFilter{Limit: &limit, Cursor: cursor})
		if err != nil {
			return auth.AuthPrincipalSummary{}, err
		}
		for _, item := range items {
			if item.ActorID == actorID && !item.Revoked {
				if len(l.identities) >= 256 {
					clear(l.identities)
				}
				l.identities[actorID] = pmPrincipalIdentity{agentID: item.AgentID, expires: now.Add(30 * time.Second)}
				return item, nil
			}
		}
		if next == "" {
			return auth.AuthPrincipalSummary{}, pm.ErrForbidden
		}
		cursor = next
	}
}

// An unavailable authority reader cannot establish a permission denial.
func (l *pmPrincipalLookup) findForAuthorization(ctx context.Context, actorID string) (auth.AuthPrincipalSummary, error) {
	principal, err := l.find(ctx, actorID)
	if err == nil {
		return principal, nil
	}
	if errors.Is(err, pm.ErrForbidden) || errors.Is(err, auth.ErrAgentNotFound) {
		return auth.AuthPrincipalSummary{}, pm.ErrForbidden
	}
	return auth.AuthPrincipalSummary{}, fmt.Errorf("%w: principal authorization could not be read; retry the request", pm.ErrUnavailable)
}
