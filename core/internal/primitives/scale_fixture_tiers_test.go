package primitives_test

import (
	"os"
	"testing"

	"agent-nexus-core/internal/testutil/perfguard"
)

func primitivesCorrectnessCorpusRows() int {
	return primitivesFixtureRows(256, perfguard.Rows)
}

func primitivesFixtureRows(bounded, full int) int {
	if os.Getenv("ANX_PERFORMANCE_TEST") == "1" {
		return full
	}
	return bounded
}

// The advisory workflow discovers this name and sets ANX_PERFORMANCE_TEST=1.
// Run the exact correctness tests and assertions at the original full scale;
// the regular CI partition uses their bounded corpus instead of reseeding two
// complete workspaces. The 10k-document fixture stays full-size in both tiers.
func TestPerformancePrimitivesLifecycleMembershipAndWrites(t *testing.T) {
	if testing.Short() || os.Getenv("ANX_PERFORMANCE_TEST") != "1" {
		t.Skip("full 4096-row lifecycle and membership scale corpus")
	}
	t.Run("lifecycle", TestEventLifecycleBoundedCascadePlanAndCost)
	t.Run("membership", TestFrozenHomeReadMembershipPlanAndCost)
	t.Run("project-write", TestTopicLifecycleWriteSeeksProjectIndex)
}
