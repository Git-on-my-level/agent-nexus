package app

import "strings"

type cliEnvironmentVariable struct {
	Name      string
	Overrides string
	Summary   string
	Example   string
}

func envDocText() string {
	return strings.TrimSpace(`ANX environment variables

ANX_AS selects a derived agent. --as wins over ANX_AS. When neither is set, anx checks agentctl run context, then verified harness markers.
ANX_WORKSPACE selects a configured workspace alias; ANX_BASE_URL selects the core workspace URL. ANX_CONFIG_DIR or --config-dir selects the absolute host config directory when HOME is unavailable, including agentctl command callbacks. ANX_TIMEOUT, ANX_JSON and ANX_NO_COLOR control request and output behavior.
ANX_UPDATE_POLICY overrides the saved CLI release policy: auto (default), notify, or off. Auto checks once per UTC day from interactive terminals or recognized non-interactive agent invocations. Known CI provider markers always skip updates; recognized harness markers can override generic CI=1, while --as/ANX_AS alone cannot. Ordinary scripts are skipped. Checks run in a quiet detached worker after command output. anx update status stays offline. Inspect anx update status or anx help update.
ANX_ACCESS_TOKEN supplies an explicit bearer for controlled human or test contexts. It does not use the host assertion grant.

Run anx config workspaces when unsure which workspace applies. Use anx config use <alias|url> to set a user-global default, or anx config map "~/work/project/**" <alias|url> for a directory rule. anx config unmap "~/work/project/**" removes a rule. Quote globs so the shell does not expand them.
Selection: --base-url or --workspace > ANX_WORKSPACE > ANX_BASE_URL > most-specific directory rule > configured default > single enrolled host > localhost only with zero enrolled hosts. --workspace is the alias equivalent of --base-url; pass only one. Multiple enrolled workspaces with no selection fail before a network request and show exact repair commands. Do not hardcode --base-url in agent prompts.
Run anx config show to inspect effective values and sources without printing secrets. Preferences live in ~/.config/anx/workspaces.json (or the selected ANX_CONFIG_DIR), never in git repositories.`)
}

func hostIdentityDocText() string {
	return strings.TrimSpace(`Host identity

Enroll once per workspace with anx host enroll. The owner-only host key lives below ~/.config/anx/hosts/<workspace-key>/.
For fleet hosts, a granted auth-admin agent creates a one-time token with anx host tokens create --label <destination-label> and pipes it to anx host enroll --name <destination-slug> --token-stdin on the destination machine. Replace both placeholders with the actual destination values. Configure the selected workspace URL on both machines; never log the token.
Only a human can anx auth admins grant|revoke <principal>. Granted agents can anx host enrollments list|approve|deny, host tokens create|list|revoke, and host revoke <host>. An agent cannot revoke its own host.
Enrollment stores that workspace core in host.json and persists a workspace alias. The first enrolled workspace becomes the default when none is configured; later enrollments preserve the current default and print anx config use <alias> to select the new workspace.
Run anx config workspaces to inspect aliases, enrolled workspaces and the directory rule for cwd. Selection follows --base-url or --workspace, ANX_WORKSPACE, ANX_BASE_URL, directory rule, configured default, then a single enrolled host (source bridge:auto-single). With several enrolled workspaces and no selection, commands fail with repair instructions.
Use --as <name> or ANX_AS to select a derived agent; agentctl run context and verified harness detection are automatic. anx auth whoami reports the selected host, agent and resolution source.

Old ~/.config/anx/profiles/*.json agent profiles are considered only for adoption during host enrollment. Use anx host enroll --plan to inspect them.`)
}
