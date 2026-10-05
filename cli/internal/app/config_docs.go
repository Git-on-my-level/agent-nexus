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
		{Name: "ANX_CONFIG_DIR", Overrides: "local host config root", Summary: "Absolute directory containing workspace preferences, enrolled hosts and callback logs.", Example: "ANX_CONFIG_DIR=/Users/me/.config/anx"},
		{Name: "ANX_TIMEOUT", Overrides: "request timeout", Summary: "Set request timeout as a Go duration.", Example: "ANX_TIMEOUT=30s"},
		{Name: "ANX_UPDATE_POLICY", Overrides: "automatic CLI release policy", Summary: "Override saved update policy with auto, notify, or off.", Example: "ANX_UPDATE_POLICY=off"},
		{Name: "ANX_JSON", Overrides: "JSON output mode", Summary: "Emit JSON envelopes.", Example: "ANX_JSON=true"},
		{Name: "ANX_ACCESS_TOKEN", Overrides: "explicit bearer", Summary: "Use a caller supplied bearer in a human or test context.", Example: "ANX_ACCESS_TOKEN=<token>"},
	}
}

func envDocText() string {
	return strings.TrimSpace(`ANX environment variables

ANX_AS selects a derived agent. --as wins over ANX_AS. When neither is set, anx checks agentctl run context, then verified harness markers.
ANX_BASE_URL selects the core workspace. ANX_CONFIG_DIR or --config-dir selects the absolute host config directory when HOME is unavailable, including agentctl command callbacks. ANX_TIMEOUT, ANX_JSON and ANX_NO_COLOR control request and output behavior.
ANX_UPDATE_POLICY overrides the saved CLI release policy: auto (default), notify, or off. Read-only commands never trigger binary maintenance. Inspect anx update status or anx help update.
ANX_ACCESS_TOKEN supplies an explicit bearer for controlled human or test contexts. It does not use the host assertion grant.

Run anx config workspaces when unsure which workspace applies. Use anx config use <alias|url> to set a user-global default, or anx config map "~/work/project/**" <alias|url> for a directory rule. anx config unmap "~/work/project/**" removes a rule. Quote globs so the shell does not expand them.
Selection: --base-url or --workspace > ANX_BASE_URL > most-specific directory rule > configured default > single enrolled host > localhost only with zero enrolled hosts. --workspace is the alias equivalent of --base-url; pass only one. Multiple enrolled workspaces with no selection fail before a network request and show exact repair commands. Do not hardcode --base-url in agent prompts.
Run anx config show to inspect effective values and sources without printing secrets. Preferences live in ~/.config/anx/workspaces.json (or the selected ANX_CONFIG_DIR), never in git repositories.`)
}

func hostIdentityDocText() string {
	return strings.TrimSpace(`Host identity

Enroll once per workspace with anx host enroll. The owner-only host key lives below ~/.config/anx/hosts/<workspace-key>/.
For fleet hosts, a granted auth-admin agent runs anx --json host tokens create --label host-b --expires-in 1h and pipes .result.token securely to anx host enroll --token-stdin on host B. Configure the workspace base URL on both hosts; never log the token.
Only a human can anx auth admins grant|revoke <principal>. Granted agents can anx host enrollments list|approve|deny, host tokens create|list|revoke, and host revoke <host>. An agent cannot revoke its own host.
Enrollment stores that workspace core in host.json, persists a workspace alias and prints anx config use <alias> to make it default. Enrollment never changes the configured default.
Run anx config workspaces to inspect aliases, enrolled workspaces and the directory rule for cwd. Selection follows --base-url or --workspace, ANX_BASE_URL, directory rule, configured default, then a single enrolled host (source bridge:auto-single). With several enrolled workspaces and no selection, commands fail with repair instructions.
Use --as <name> or ANX_AS to select a derived agent; agentctl run context and verified harness detection are automatic. anx auth whoami reports the selected host, agent and resolution source.

Old ~/.config/anx/profiles/*.json agent profiles are considered only for adoption during host enrollment. Use anx host enroll --plan to inspect them.`)
}
