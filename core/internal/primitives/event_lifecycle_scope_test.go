package primitives

import (
	"context"
	"strings"
	"testing"
)

func TestEventLifecycleRawCandidatesKeepScopedPointProof(t *testing.T) {
	ctx := WithAccessScope(context.Background(), AccessScope{ActorID: "reader"})
	ctx = context.WithValue(ctx, summaryPointReadKey{}, true)
	snapshot := &denialSnapshot{epoch: 1, rows: `[["event","private"]]`, pointToken: 123}
	query := scopeReadSnapshot(ctx, eventDescendantsQuery, snapshot, true)
	if !strings.Contains(query, "anx_point_snapshot_denied") || strings.Contains(query, "ref_edges AS (SELECT") {
		t.Fatal("raw candidate probes caused an unused shadow or lost scoped event membership")
	}
	// Mixed queries must still filter every unqualified edge relation.
	query = scopeReadSnapshot(ctx, `SELECT source_id FROM ref_edges UNION SELECT source_id FROM main.ref_edges`, snapshot, true)
	if !strings.Contains(query, "ref_edges AS (SELECT") || !strings.Contains(query, "_anx_denied_refs") {
		t.Fatal("mixed scoped/canonical query lost its edge policy")
	}
}
