package buildinfo

import "testing"

func TestMinCompatibleCLIIsAnExplicitFloor(t *testing.T) {
	if MinCompatibleCLI != "v0.11.0" {
		t.Fatalf("MinCompatibleCLI = %q, want v0.11.0 (oldest CLI that does not call removed POST /auth/agents/register)", MinCompatibleCLI)
	}
	if MinCompatibleCLI == Current {
		t.Fatalf("MinCompatibleCLI = %q tracks the release version; raise it only for a wire break", Current)
	}
}
