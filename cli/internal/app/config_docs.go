package app

import "strings"

type cliEnvironmentVariable struct {
	Name      string
	Overrides string
	Summary   string
	Example   string
}

func cliEnvironmentVariables() []cliEnvironmentVariable {
	return []cliEnvironmentVariable{
		{Name: "ANX_AS", Overrides: "derived agent name", Summary: "Select an adapter or persona for this process.", Example: "ANX_AS=reviewer"},
		{Name: "ANX_BASE_URL", Overrides: "core API base URL", Summary: "Select the enrolled workspace core.", Example: "ANX_BASE_URL=https://anx.example.com"},
		{Name: "ANX_TIMEOUT", Overrides: "request timeout", Summary: "Set request timeout as a Go duration.", Example: "ANX_TIMEOUT=30s"},
		{Name: "ANX_JSON", Overrides: "JSON output mode", Summary: "Emit JSON envelopes.", Example: "ANX_JSON=true"},
		{Name: "ANX_ACCESS_TOKEN", Overrides: "explicit bearer", Summary: "Use a caller supplied bearer in a human or test context.", Example: "ANX_ACCESS_TOKEN=<token>"},
	}
}

func envDocText() string {
	return strings.TrimSpace(`ANX environment variables

ANX_AS selects a derived agent. --as wins over ANX_AS. When neither is set, anx checks agentctl run context, then verified harness markers.
ANX_BASE_URL selects the core workspace; ANX_TIMEOUT, ANX_JSON and ANX_NO_COLOR control request and output behavior.
ANX_ACCESS_TOKEN supplies an explicit bearer for controlled human or test contexts. It does not use the host assertion grant.

Run anx config show to inspect effective values without printing secrets.`)
}

func profilesDocText() string {
	return strings.TrimSpace(`Host identity

Enroll once per workspace with anx host enroll. The owner-only host key lives below ~/.config/anx/hosts/<workspace-key>/.
Use --as <name> or ANX_AS to select a derived agent; agentctl run context and verified harness detection are automatic. anx auth whoami reports the selected host, agent and resolution source.

Old ~/.config/anx/profiles/*.json agent profiles are considered only for adoption during host enrollment. Use anx host enroll --plan to inspect them.`)
}
