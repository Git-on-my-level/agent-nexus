package app

type domainSurface struct {
	Domain       string
	ThreadBacked bool
	Commands     []domainSurfaceCommand
}

type domainSurfaceCommand struct {
	Path       string
	Target     string
	Examples   []string
	ForbidHelp []string
}

func agentDomainSurfaces() []domainSurface {
	return []domainSurface{
		{
			Domain:       "topics",
			ThreadBacked: true,
			Commands: []domainSurfaceCommand{
				{Path: "topics message", Target: "<ref>", Examples: []string{"anx topics message topic:launch --body-file message.md"}, ForbidHelp: []string{"--topic <topic-id>"}},
				{Path: "topics messages", Target: "<ref>", Examples: []string{"anx topics messages topic:launch"}, ForbidHelp: []string{"--topic <topic-id>"}},
				{Path: "topics reply", Target: "<ref>", Examples: []string{"anx topics reply topic:launch --to <message-id> --body \"Confirmed\""}, ForbidHelp: []string{"--topic <topic-id>"}},
			},
		},
		{
			Domain:       "docs",
			ThreadBacked: true,
			Commands: []domainSurfaceCommand{
				{Path: "docs search", Target: "<q>", Examples: []string{"anx docs search \"runbook\" --knowledge --host m4-air"}, ForbidHelp: []string{}},
				{Path: "docs put", Target: "<path>", Examples: []string{"anx docs put - --handle kb-shared --title \"Note\" --tags knowledge"}, ForbidHelp: []string{}},
				{Path: "docs ingest", Target: "<path>", Examples: []string{"anx docs ingest ./kb --source https://example.invalid/kb"}, ForbidHelp: []string{}},
				{Path: "docs get", Target: "<ref>", Examples: []string{"anx docs get kb-shared --format md"}, ForbidHelp: []string{}},
				{Path: "docs comment", Target: "<ref>", Examples: []string{"anx docs comment doc:runbook \"Host B found this\""}, ForbidHelp: []string{"anx docs comment --document-id <document-id>", "--document-id <document-id>"}},
				{Path: "docs comments", Target: "<ref>", Examples: []string{"anx docs comments doc:runbook"}, ForbidHelp: []string{"anx docs comments --document-id <document-id>", "--document-id <document-id>"}},
				{Path: "docs message", Target: "<ref>", Examples: []string{"anx docs message doc:runbook --body-file note.md"}, ForbidHelp: []string{"anx docs message --document-id <document-id>", "--document-id <document-id>"}},
				{Path: "docs messages", Target: "<ref>", Examples: []string{"anx docs messages doc:runbook"}, ForbidHelp: []string{"anx docs messages --document-id <document-id>", "--document-id <document-id>"}},
				{Path: "docs reply", Target: "<ref>", Examples: []string{"anx docs reply doc:runbook --to <message-id> --body \"Confirmed\""}, ForbidHelp: []string{"anx docs reply --document-id <document-id>", "--document-id <document-id>"}},
				{Path: "docs revise", Target: "<ref>", Examples: []string{"anx docs revise doc:runbook --body-file notes.md"}, ForbidHelp: []string{"anx docs revise --document-id <document-id>", "--document-id <document-id>"}},
			},
		},
		{
			Domain:       "cards",
			ThreadBacked: true,
			Commands: []domainSurfaceCommand{
				{Path: "cards message", Target: "<ref>", Examples: []string{"anx cards message card:implement-login --body \"Implemented in 0729e75\""}, ForbidHelp: []string{"anx cards message --card-id <card-id>", "--card-id <card-id>"}},
				{Path: "cards messages", Target: "<ref>", Examples: []string{"anx cards messages card:implement-login"}, ForbidHelp: []string{"anx cards messages --card-id <card-id>", "--card-id <card-id>"}},
				{Path: "cards reply", Target: "<ref>", Examples: []string{"anx cards reply card:implement-login --to <message-id> --body \"Confirmed\""}, ForbidHelp: []string{"anx cards reply --card-id <card-id>", "--card-id <card-id>"}},
				{Path: "cards revise", Target: "<ref>", Examples: []string{"anx cards revise card:implement-login --body-file card.md"}, ForbidHelp: []string{"anx cards revise --card-id <card-id>", "--card-id <card-id>"}},
				{Path: "cards move", Target: "<ref>", Examples: []string{"anx cards move card:implement-login --column review"}, ForbidHelp: []string{"anx cards move --card-id <card-id>", "--card-id <card-id>"}},
				{Path: "cards assign", Target: "<ref>", Examples: []string{"anx cards assign card:implement-login --assignee-ref actor:agent-alpha"}, ForbidHelp: []string{"anx cards assign --card-id <card-id>", "--card-id <card-id>"}},
				{Path: "cards resolve", Target: "<ref>", Examples: []string{"anx cards resolve card:implement-login --reason \"ok\" --body \"Validated in staging\"", "anx cards resolve card:implement-login --resolution-ref event:<event-id>"}, ForbidHelp: []string{"anx cards resolve --card-id <card-id>", "--card-id <card-id>"}},
				{Path: "cards reopen", Target: "<ref>", Examples: []string{"anx cards reopen card:implement-login"}, ForbidHelp: []string{"anx cards reopen --card-id <card-id>", "--card-id <card-id>"}},
			},
		},
		{
			Domain:       "threads",
			ThreadBacked: true,
			Commands: []domainSurfaceCommand{
				{Path: "threads message", Target: "<thread-id>", Examples: []string{"anx threads message <thread-id> --body-file note.md"}, ForbidHelp: []string{"--thread <thread-id>"}},
				{Path: "threads reply", Target: "<thread-id>", Examples: []string{"anx threads reply <thread-id> --to <message-id> --body \"Confirmed\""}, ForbidHelp: []string{"--thread <thread-id>"}},
			},
		},
	}
}
