package app

import "strings"

// Setup tier: one short how-to per job, reached through `anx help <job>` and
// the next actions `anx doctor` returns. Each lists only commands that exist.
var setupJobs = map[string]string{
	"enroll": `Enroll a machine (fleet enrollment)

  anx host discover                          find workspaces this machine can join
  anx host enroll [--name <host>]            request enrollment; a human or auth-admin agent approves
  anx host enroll --token-stdin              enroll headlessly with a one-time fleet token
  anx host enrollments list|approve|deny     (auth-admin) review pending enrollments
  anx host exclude|include <harness>         control which harnesses derive agents here
  anx auth whoami                            confirm host, derived agent and resolution source
  anx bridge install                        recommended next: one service for this host
  anx bridge add --as <name>                 register each intended agent/workspace
  anx doctor                                 check workspace resolution and preconditions

Without answer delivery, unattended resumes wait; Git/drain remain manual.
Await/batch and direct commands work without a bridge.
`,
	"tokens": `Issue and revoke headless tokens

  anx host tokens create --label fleet --expires-in 1h   (auth-admin) one-time fleet enrollment token
  anx host tokens list                                   list issued tokens
  anx host tokens revoke <token-id>                      revoke a token
  anx host token                                         print this host's short-lived access token
  anx auth admins list|grant|revoke                      manage who may issue tokens
`,
	"secrets": `Store and use workspace secrets

  anx secret create <name> --from-stdin      create a secret (value from stdin)
  anx secret list                            list secret names and metadata
  anx secret get <name> [--reveal]           read metadata, or the value with --reveal
  anx secret update <name> --from-stdin      rotate a value
  anx secret delete <name>                   delete a secret
  anx secret exec --secret NAME -- <cmd>     run a command with secrets in its environment
`,
	"adapters": `Declare host-local live data sources (adapters and series)

  anx adapters declare --body-file <path|->  declare an adapter and its allowed series
  anx adapters list                          list declared sources and grant state
  anx adapters token <name>                  exchange owner identity for a push-only token
  anx adapters revoke <name>                 revoke a grant immediately; keep observations
  anx adapters delete <name>                 delete a source and its series history
  anx series push <name> <value> [--adapter <name>]  push one point (loop tier)
`,
	"pm": `Install and run the workspace PM

  anx --as pm pm install [--runner <runner>] install the local PM runner (interactive wizard on a terminal)
  anx pm status                              show PM connection and runner state
  anx pm serve                               run the PM turn loop in the foreground
  anx pm channels doctor                     check channel bindings
  anx pm uninstall                           remove the local PM runner
`,
	"bridge": `Deliver answers to this host (agent bridge)

  anx bridge install                        recommended: install the single host service
  anx bridge add --as <name>                 register this agent in the selected workspace
  anx bridge start|stop                      start or stop the bridge service
  anx bridge status                          show bridge state
  anx bridge doctor                          diagnose delivery for this host
  anx bridge run                             run all registered host jobs in the foreground

Without registered answer delivery, unattended resumes wait. Git and spool drain
remain manual without their jobs. Await/batch and direct commands need no bridge.
`,
}

func setupJobNames() []string {
	return []string{"enroll", "tokens", "secrets", "adapters", "pm", "bridge"}
}

// setupJobHelp returns the how-to for a setup job. For jobs that share a name
// with a command group the group's own help follows the how-to.
func setupJobHelp(topic string, groupHelp func(string) (string, bool)) (string, bool) {
	text, ok := setupJobs[strings.TrimSpace(topic)]
	if !ok {
		return "", false
	}
	if groupHelp != nil {
		if more, found := groupHelp(topic); found && strings.TrimSpace(more) != "" {
			text += "\n" + more
		}
	}
	return text, true
}
