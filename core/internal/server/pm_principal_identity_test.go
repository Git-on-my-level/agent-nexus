package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/pm"
)

type indexedPMPrincipalStore struct {
	round15PrincipalStore
	routes int
}

func (s *indexedPMPrincipalStore) PrincipalForActor(ctx context.Context, _ string) (auth.AuthPrincipalSummary, error) {
	s.routes++
	return s.GetPrincipalSummary(ctx, "agent")
}

func TestPMIndexedIdentityExpiryRetainsFreshAuthority(t *testing.T) {
	ctx := context.Background()
	st := &indexedPMPrincipalStore{round15PrincipalStore: round15PrincipalStore{round4PrincipalStore: round4PrincipalStore{principal: auth.AuthPrincipalSummary{ActorID: "actor", AgentID: "agent", PrincipalKind: "human"}}}}
	l := newPMPrincipalLookup(st)
	now := time.Now()
	l.now = func() time.Time { return now }
	for i := 0; i < 101; i++ {
		if _, err := l.findForAuthorization(ctx, "actor"); err != nil {
			t.Fatal(err)
		}
	}
	if st.lists != 0 || st.routes != 1 || st.gets != 101 {
		t.Fatalf("directory/authority reads: lists=%d routes=%d gets=%d", st.lists, st.routes, st.gets)
	}
	now = now.Add(31 * time.Second)
	st.principal.PrincipalKind = "agent"
	if p, err := l.findForAuthorization(ctx, "actor"); err != nil || p.PrincipalKind != "agent" || st.routes != 2 || st.lists != 0 {
		t.Fatalf("expired routing or stale authority: %+v %v", p, err)
	}
	st.readErr = errors.New("private authority read failure")
	if _, err := l.findForAuthorization(ctx, "actor"); !errors.Is(err, pm.ErrUnavailable) {
		t.Fatalf("authority failure downgraded: %v", err)
	}
	st.readErr, st.principal.Revoked = nil, true
	if _, err := l.findForAuthorization(ctx, "actor"); !errors.Is(err, pm.ErrForbidden) {
		t.Fatalf("revoked identity accepted: %v", err)
	}
	if _, ok := l.identities["actor"]; ok {
		t.Fatal("revoked routing retained")
	}
}
