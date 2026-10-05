package buildinfo

import "testing"

func TestMinCompatibleCLIIsAnExplicitFloor(t *testing.T) {
	if MinCompatibleCLI == "" {
		t.Fatal("compatibility floor is empty")
	}
	if MinCompatibleCLI == Current {
		t.Fatalf("MinCompatibleCLI = %q tracks the release version; raise it only for a wire break", Current)
	}
}
