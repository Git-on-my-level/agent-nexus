package server

import (
	"context"
	"testing"
	"time"

	"agent-nexus-core/internal/auth"
)

type principalBlockKey struct{}
type blockingPrincipalStore struct{ entered, release chan struct{} }

func (s *blockingPrincipalStore) read(ctx context.Context) (auth.AuthPrincipalSummary, error) {
	if ctx.Value(principalBlockKey{}) != nil {
		close(s.entered)
		select {
		case <-s.release:
		case <-ctx.Done():
			return auth.AuthPrincipalSummary{}, ctx.Err()
		}
	}
	return auth.AuthPrincipalSummary{ActorID: "reader", AgentID: "agent"}, nil
}
func (s *blockingPrincipalStore) ListPrincipals(ctx context.Context, _ auth.AuthPrincipalListFilter) ([]auth.AuthPrincipalSummary, string, error) {
	p, err := s.read(ctx)
	return []auth.AuthPrincipalSummary{p}, "", err
}
func (s *blockingPrincipalStore) GetPrincipalSummary(ctx context.Context, _ string) (auth.AuthPrincipalSummary, error) {
	return s.read(ctx)
}

func TestPMPrincipalSlowReadDoesNotBlockOtherRequests(t *testing.T) {
	t.Parallel()
	for _, warm := range []bool{false, true} {
		t.Run(map[bool]string{false: "cold", true: "warm"}[warm], func(t *testing.T) {
			st := &blockingPrincipalStore{entered: make(chan struct{}), release: make(chan struct{})}
			lookup := newPMPrincipalLookup(st)
			if warm {
				if _, err := lookup.find(context.Background(), "reader"); err != nil {
					t.Fatal(err)
				}
			}
			ctx, cancel := context.WithCancel(context.WithValue(context.Background(), principalBlockKey{}, true))
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := lookup.find(ctx, "reader"); done <- err }()
			<-st.entered
			other := make(chan error, 1)
			go func() { _, err := lookup.find(context.Background(), "reader"); other <- err }()
			select {
			case err := <-other:
				if err != nil {
					t.Error(err)
				}
			case <-time.After(time.Second):
				t.Error("independent lookup waits behind authority I/O")
			}
			close(st.release)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		})
	}
}
