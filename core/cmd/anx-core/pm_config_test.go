package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestConfiguredPMActorWarnsWhenMissing(t *testing.T) {
	for _, actor := range []string{"", "  ", "pm-agent"} {
		t.Setenv("ANX_PM_AGENT_ACTOR_ID", actor)
		var stderr bytes.Buffer
		got := configuredPMActor(&stderr)
		if got != strings.TrimSpace(actor) {
			t.Fatalf("actor %q", got)
		}
		warned := strings.Contains(stderr.String(), "ANX_PM_AGENT_ACTOR_ID")
		if warned != (got == "") {
			t.Fatalf("warning %q for %q", stderr.String(), got)
		}
	}
}
