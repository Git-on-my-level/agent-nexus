package primitives_test

import (
	"context"
	"testing"

	"agent-nexus-core/internal/primitives"
)

func TestGroupCurationBatchIsOneTransaction(t *testing.T) {
	t.Parallel()
	ws, err := initializeTestWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	ctx := context.Background()
	bad := primitives.CurationProposalRecord{Key: "batch-1", ProposalEventID: "prop-1", Targets: []string{"event:ask-a", "not-an-event", "event:ask-c"}}
	if err := s.GroupCurationBatch(ctx, bad); err == nil {
		t.Fatal("expected a rejected member")
	}
	var n int
	if err := ws.DB().QueryRow(`SELECT COUNT(*) FROM curation_batch_members`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("rejected batch left %d members", n)
	}
	good := primitives.CurationProposalRecord{Key: "batch-1", ProposalEventID: "prop-1", Targets: []string{"event:ask-a", "event:ask-b"}}
	if err := s.GroupCurationBatch(ctx, good); err != nil {
		t.Fatal(err)
	}
	if err := ws.DB().QueryRow(`SELECT COUNT(*) FROM curation_batch_members WHERE batch_key='batch-1'`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("members %d %v", n, err)
	}
}
