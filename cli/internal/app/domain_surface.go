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
				{Path: "topics message", Target: "<topic-id>", Examples: []string{"anx topics message <topic-id> --body-file message.md"}, ForbidHelp: []string{"--topic <topic-id>"}},
				{Path: "topics messages", Target: "<topic-id>", Examples: []string{"anx topics messages <topic-id>"}, ForbidHelp: []string{"--topic <topic-id>"}},
				{Path: "topics reply", Target: "<topic-id>", Examples: []string{"anx topics reply <topic-id> --to <message-id> --body \"Confirmed\""}, ForbidHelp: []string{"--topic <topic-id>"}},
			},
		},
		{
			Domain:       "docs",
			ThreadBacked: true,
			Commands: []domainSurfaceCommand{
				{Path: "docs message", Target: "<document-id>", Examples: []string{"anx docs message <document-id> --body-file note.md"}, ForbidHelp: []string{"--document-id <document-id>"}},
				{Path: "docs messages", Target: "<document-id>", Examples: []string{"anx docs messages <document-id>"}, ForbidHelp: []string{"--document-id <document-id>"}},
				{Path: "docs reply", Target: "<document-id>", Examples: []string{"anx docs reply <document-id> --to <message-id> --body \"Confirmed\""}, ForbidHelp: []string{"--document-id <document-id>"}},
				{Path: "docs revise", Target: "<document-id>", Examples: []string{"anx docs revise <document-id> --content-file notes.md"}, ForbidHelp: []string{"anx docs revise --document-id <document-id>", "--document-id <document-id>"}},
			},
		},
		{
			Domain:       "cards",
			ThreadBacked: true,
			Commands: []domainSurfaceCommand{
				{Path: "cards message", Target: "<card-id>", Examples: []string{"anx cards message <card-id> --body \"Implemented in 0729e75\""}, ForbidHelp: []string{"--card <card-id>"}},
				{Path: "cards messages", Target: "<card-id>", Examples: []string{"anx cards messages <card-id>"}, ForbidHelp: []string{"--card <card-id>"}},
				{Path: "cards reply", Target: "<card-id>", Examples: []string{"anx cards reply <card-id> --to <message-id> --body \"Confirmed\""}, ForbidHelp: []string{"--card <card-id>"}},
				{Path: "cards revise", Target: "<card-id>", Examples: []string{"anx cards revise <card-id> --content-file card.md"}, ForbidHelp: []string{"anx cards revise --card <card-id>", "--card <card-id>"}},
				{Path: "cards move", Target: "<card-id>", Examples: []string{"anx cards move <card-id> --column review"}, ForbidHelp: []string{"anx cards move --card <card-id>", "--card <card-id>"}},
				{Path: "cards assign", Target: "<card-id>", Examples: []string{"anx cards assign <card-id> --assignee-ref actor:<actor-id>"}, ForbidHelp: []string{"anx cards assign --card <card-id>", "--card <card-id>"}},
				{Path: "cards resolve", Target: "<card-id>", Examples: []string{"anx cards resolve <card-id> --reason \"Works as expected\""}, ForbidHelp: []string{"anx cards resolve --card <card-id>", "--card <card-id>"}},
				{Path: "cards reopen", Target: "<card-id>", Examples: []string{"anx cards reopen <card-id>"}, ForbidHelp: []string{"anx cards reopen --card <card-id>", "--card <card-id>"}},
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
