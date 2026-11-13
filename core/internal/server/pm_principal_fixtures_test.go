package server

import (
	"context"
	"testing"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
)

type round4PrincipalStore struct {
	principal   auth.AuthPrincipalSummary
	lists, gets int
}

func (s *round4PrincipalStore) ListPrincipals(context.Context, auth.AuthPrincipalListFilter) ([]auth.AuthPrincipalSummary, string, error) {
	s.lists++
	return []auth.AuthPrincipalSummary{s.principal}, "", nil
}
func (s *round4PrincipalStore) GetPrincipalSummary(context.Context, string) (auth.AuthPrincipalSummary, error) {
	s.gets++
	return s.principal, nil
}

type round15PrincipalStore struct {
	round4PrincipalStore
	readErr error
}

func (s *round15PrincipalStore) ListPrincipals(ctx context.Context, f auth.AuthPrincipalListFilter) ([]auth.AuthPrincipalSummary, string, error) {
	if s.readErr != nil {
		return nil, "", s.readErr
	}
	return s.round4PrincipalStore.ListPrincipals(ctx, f)
}

func (s *round15PrincipalStore) GetPrincipalSummary(ctx context.Context, id string) (auth.AuthPrincipalSummary, error) {
	if s.readErr != nil {
		return auth.AuthPrincipalSummary{}, s.readErr
	}
	return s.round4PrincipalStore.GetPrincipalSummary(ctx, id)
}

func newPMStoreTestEnv(t *testing.T) authIntegrationEnv {
	t.Helper()
	ws, err := initializeTestWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	registry := actors.NewStore(ws.DB())
	if _, err = registry.EnsureSystemActor(context.Background(), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	return authIntegrationEnv{workspace: ws, registry: registry, authStore: auth.NewStore(ws.DB()), primitiveStore: primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)}
}
