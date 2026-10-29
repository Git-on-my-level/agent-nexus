package app

import (
	"fmt"
	"strings"
	"testing"
)

func TestHumanSessionHelpOffline(t *testing.T) {
	for _, topic := range []string{"host exclude", "host include", "pm decisions create", "pm decisions answer", "pm decisions dispatch", "pm bindings create", "pm bindings list", "pm actions reconcile", "pm actions acknowledge", "secret list", "secret create", "cards purge", "artifacts purge", "auth invites list", "notifications settings set", "debug derived rebuild"} {
		t.Run(topic, func(t *testing.T) {
			args := append([]string{"--json", "help"}, strings.Fields(topic)...)
			payload := assertEnvelopeOK(t, runCLIForTest(t, t.TempDir(), nil, nil, args))
			raw := fmt.Sprint(payload["result"])
			if !strings.Contains(raw, "signed-in browser session") {
				t.Fatalf("missing human-session guidance: %s", raw)
			}
			if topic == "pm decisions create" && !strings.Contains(raw, "pm turns propose") {
				t.Fatalf("missing agent proposal path: %s", raw)
			}
		})
	}
}

func TestEveryHumanCommandHasSessionHelp(t *testing.T) {
	// Auth ceremonies exist in API metadata but are deliberately not CLI
	// commands. Audit every command the runtime actually exposes.
	for _, entry := range buildRuntimeHelpCatalog().GeneratedCommands {
		if !commandRequiresHuman(entry.Command) {
			continue
		}
		topic := entry.RuntimePath
		text, ok := helpTopicText(topic)
		if !ok || !strings.Contains(text, "signed-in browser session") {
			t.Errorf("%s lacks human-session guidance", topic)
		}
	}
}
