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
				{Path: "docs message", Target: "<ref>", Examples: []string{"anx docs message doc:runbook --body-file note.md"}, ForbidHelp: []string{"--document-id <document-id>"}},
				{Path: "docs messages", Target: "<ref>", Examples: []string{"anx docs messages doc:runbook"}, ForbidHelp: []string{"--document-id <document-id>"}},
				{Path: "docs reply", Target: "<ref>", Examples: []string{"anx docs reply doc:runbook --to <message-id> --body \"Confirmed\""}, ForbidHelp: []string{"--document-id <document-id>"}},
				{Path: "docs revise", Target: "<ref>", Examples: []string{"anx docs revise doc:runbook --content-file notes.md"}, ForbidHelp: []string{"anx docs revise --document-id <document-id>", "--document-id <document-id>"}},
			},
		},
		{
			Domain:       "cards",
			ThreadBacked: true,
			Commands: []domainSurfaceCommand{
				{Path: "cards message", Target: "<ref>", Examples: []string{"anx cards message card:implement-login --body \"Implemented in 0729e75\""}, ForbidHelp: []string{"--card <card-id>"}},
				{Path: "cards messages", Target: "<ref>", Examples: []string{"anx cards messages card:implement-login"}, ForbidHelp: []string{"--card <card-id>"}},
				{Path: "cards reply", Target: "<ref>", Examples: []string{"anx cards reply card:implement-login --to <message-id> --body \"Confirmed\""}, ForbidHelp: []string{"--card <card-id>"}},
				{Path: "cards revise", Target: "<ref>", Examples: []string{"anx cards revise card:implement-login --content-file card.md"}, ForbidHelp: []string{"anx cards revise --card <card-id>", "--card <card-id>"}},
				{Path: "cards move", Target: "<ref>", Examples: []string{"anx cards move card:implement-login --column review"}, ForbidHelp: []string{"anx cards move --card <card-id>", "--card <card-id>"}},
				{Path: "cards assign", Target: "<ref>", Examples: []string{"anx cards assign card:implement-login --assignee-ref actor:agent-alpha"}, ForbidHelp: []string{"anx cards assign --card <card-id>", "--card <card-id>"}},
				{Path: "cards resolve", Target: "<ref>", Examples: []string{"anx cards resolve card:implement-login --body-file evidence.md"}, ForbidHelp: []string{"anx cards resolve --card <card-id>", "--card <card-id>"}},
				{Path: "cards reopen", Target: "<ref>", Examples: []string{"anx cards reopen card:implement-login"}, ForbidHelp: []string{"anx cards reopen --card <card-id>", "--card <card-id>"}},
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
