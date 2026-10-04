package pm

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestNewDecisionsScopePermissionsAndCandidateBound(t *testing.T) {
	s, st, p, _ := fixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	s.deps.Authorize = func(_ context.Context, _ Principal, _ string, ref string) error {
		if ref == "card:secret" {
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
	if err != nil || truncated || len(ds) != 1 || ds[0].ID != "public" || ds[0].Instruction != "" {
		t.Fatalf("%+v %v %v", ds, truncated, err)
	}
	for i := 0; i < 205; i++ {
		d := Decision{ID: fmt.Sprint(i), WorkspaceID: p.WorkspaceID, WorkRef: "card:public", CreatedAt: now.Add(time.Minute)}
		if _, err = st.insert(ctx, "decision", d.ID, d.WorkspaceID, p.ActorID, "", d); err != nil {
			t.Fatal(err)
		}
	}
	ds, truncated, err = s.NewDecisions(ctx, p, now, now.Add(2*time.Minute))
	if err != nil || !truncated || len(ds) != 200 {
		t.Fatalf("len=%d %v %v", len(ds), truncated, err)
	}
}
