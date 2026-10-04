package pm

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestNewDecisionsScopePermissionsAndCandidateBound(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	authorizations := 0
	s.deps.Authorize = func(_ context.Context, principal Principal, permission, ref string) error {
		authorizations++
		if permission != "pm.read" || ref != "" {
			t.Fatalf("expected one workspace authorization, got %s %q", permission, ref)
		}
		if principal.ActorID == "other" {
			return ErrForbidden
		}
		return nil
	}
	for _, d := range []Decision{
		{ID: "before", WorkspaceID: p.WorkspaceID, WorkRef: "card:public", CreatedAt: now.Add(-time.Hour)},
		{ID: "public", WorkspaceID: p.WorkspaceID, WorkRef: "card:public", CreatedAt: now.Add(time.Minute), Instruction: "private text"},
		{ID: "hidden", WorkspaceID: p.WorkspaceID, WorkRef: "card:secret", CreatedAt: now.Add(time.Minute)},
		{ID: "other-workspace", WorkspaceID: "other", WorkRef: "card:public", CreatedAt: now.Add(time.Minute)},
		{ID: "future", WorkspaceID: p.WorkspaceID, WorkRef: "card:public", CreatedAt: now.Add(time.Hour)},
	} {
		if _, err := st.insert(ctx, "decision", d.ID, d.WorkspaceID, p.ActorID, "", d); err != nil {
			t.Fatal(err)
		}
	}
	ds, truncated, err := s.NewDecisions(ctx, p, now, now.Add(2*time.Minute))
	if err != nil || truncated || len(ds) != 2 || ds[0].ID != "hidden" || ds[1].ID != "public" || authorizations != 1 {
		t.Fatalf("%+v %v %v", ds, truncated, err)
	}
	for _, d := range ds {
		if d.Instruction != "" || d.WorkspaceID != "" {
			t.Fatalf("candidate contains non-digest fields: %+v", d)
		}
	}
	denied := p
	denied.ActorID = "other"
	if ds, truncated, err := s.NewDecisions(ctx, denied, now, now.Add(2*time.Minute)); !errors.Is(err, ErrForbidden) || ds != nil || truncated {
		t.Fatalf("unauthorized candidate read: %+v %v %v", ds, truncated, err)
	}
	for i := 0; i < 205; i++ {
		d := Decision{ID: fmt.Sprint(i), WorkspaceID: p.WorkspaceID, WorkRef: "card:public", CreatedAt: now.Add(time.Minute)}
		if _, err = st.insert(ctx, "decision", d.ID, d.WorkspaceID, p.ActorID, "", d); err != nil {
			t.Fatal(err)
		}
	}
	authorizations = 0
	ds, truncated, err = s.NewDecisions(ctx, p, now, now.Add(2*time.Minute))
	if err != nil || !truncated || len(ds) != 200 || authorizations != 1 {
		t.Fatalf("len=%d %v %v", len(ds), truncated, err)
	}
}
