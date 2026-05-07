package app

import (
	"fmt"
	"strings"
)

type cliEnvironmentVariable struct {
	Name      string
	Overrides string
	Summary   string
	Example   string
}

func cliEnvironmentVariables() []cliEnvironmentVariable {
	return []cliEnvironmentVariable{
		{Name: "ANX_AGENT", Overrides: "active profile selection", Summary: "Select the active profile for this process.", Example: "ANX_AGENT=engineering"},
		{Name: "ANX_BASE_URL", Overrides: "core API base URL", Summary: "Point commands at a core API origin.", Example: "ANX_BASE_URL=https://anx.example.com/ws/org/workspace"},
		{Name: "ANX_TIMEOUT", Overrides: "request timeout", Summary: "Set request timeout as a Go duration.", Example: "ANX_TIMEOUT=30s"},
		{Name: "ANX_NO_COLOR", Overrides: "color output", Summary: "Disable colored text output when true.", Example: "ANX_NO_COLOR=1"},
		{Name: "ANX_JSON", Overrides: "JSON output mode", Summary: "Emit JSON envelopes when true.", Example: "ANX_JSON=true"},
		{Name: "ANX_PROFILE_PATH", Overrides: "profile file path", Summary: "Read profile data from an explicit file.", Example: "ANX_PROFILE_PATH=/path/to/profile.json"},
		{Name: "ANX_ACCESS_TOKEN", Overrides: "bearer access token", Summary: "Use an explicit bearer token for this process.", Example: "ANX_ACCESS_TOKEN=<token>"},
	}
}

func envDocText() string {
	var b strings.Builder
	b.WriteString("ANX environment variables\n\n")
	b.WriteString("Use environment variables as per-process defaults for shells, scripts, services, and same-machine multi-agent setups. Explicit command flags still win for one command.\n\n")
	b.WriteString("Precedence:\n")
	b.WriteString("  command flags > environment variables > profile/default marker/autodiscovery > built-in defaults\n\n")
	b.WriteString("Supported variables:\n\n")
	b.WriteString("Variable          Overrides                 Example\n")
	b.WriteString("--------          ---------                 -------\n")
	for _, envVar := range cliEnvironmentVariables() {
		b.WriteString(fmt.Sprintf("%-16s  %-24s  %s\n", envVar.Name, envVar.Overrides, envVar.Example))
	}
	b.WriteString("\nNotes:\n")
	b.WriteString("- Prefer `ANX_AGENT` in long-running agent services so each process has its own identity without rewriting `~/.config/anx/default-profile`.\n")
	b.WriteString("- Prefer `ANX_BASE_URL` in CI and scripts when the target workspace is known outside the profile file.\n")
	b.WriteString("- Use `ANX_PROFILE_PATH` for isolated launch contexts that should not read the normal profile directory.\n")
	b.WriteString("- Treat `ANX_ACCESS_TOKEN` as secret material. Profile-backed auth is usually easier to refresh and audit.\n\n")
	b.WriteString("Inspect effective values and sources with `anx config show`.\n")
	return strings.TrimSpace(b.String())
}

func profilesDocText() string {
	return strings.TrimSpace(`ANX CLI profiles

A profile is a local identity file with the base URL, token material, key metadata, and agent identity used by the CLI. Profiles normally live under:

  ~/.config/anx/profiles/<profile>.json

Active profile resolution:

  1. --agent <profile>       Explicit command flag for one invocation
  2. ANX_AGENT=<profile>     Per-process default for shells, scripts, services
  3. default-profile marker  ~/.config/anx/default-profile written by config use/auth default
  4. single profile          Auto-selected only when exactly one profile exists
  5. none                    Multiple profiles without selection fail config resolution

Precedence:

  command flags > environment variables > profile/default marker/autodiscovery > built-in defaults

Use anx config use <profile> for a durable workstation default. It writes ~/.config/anx/default-profile.

Use ANX_AGENT=<profile> for multi-agent machines, launchd/systemd services, CI jobs, or any process that should not mutate a shared default marker:

  ANX_AGENT=engineering anx auth whoami
  ANX_AGENT=reviewer anx cards list

Use --agent <profile> for a one-off command that should override both the process environment and the persisted default:

  ANX_AGENT=engineering anx --agent reviewer auth whoami

Inspect the effective profile, base URL, token redaction status, and per-field sources with:

  anx config show

Switch or clear the persisted default with:

  anx config use <profile>
  anx config unset
  anx auth list

Bridge isolation:

Bridge configs are agent-isolated by their own handle/config path and imported auth material. Multiple bridges can coexist on one machine; prefer explicit bridge configs plus ANX_AGENT or --agent during setup so the CLI default profile does not become hidden shared state.

Related docs:
  anx meta doc env
  anx help config
  anx auth list`)
}
