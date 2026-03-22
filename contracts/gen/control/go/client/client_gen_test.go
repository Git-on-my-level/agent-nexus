package client

import "testing"

func TestGeneratedRegistryHasCommands(t *testing.T) {
	if len(CommandRegistry) == 0 {
		t.Fatal("expected non-empty command registry")
	}
}
