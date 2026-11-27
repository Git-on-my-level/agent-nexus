package app

func rulingHelp(topic string) (string, bool) {
	if topic == "rulings" {
		return rulingsUsage() + "\nRead operating rules with list and show. Humans confirm proposals in Decide.\n" +
			"Use ruling: R-YYYYMMDD-NN in instructions. Run check to catch outdated references.\n" +
			"Help: anx help rulings list|show|pull|check|propose|import\n", true
	}
	for _, command := range rulingHelperTopics {
		if command.Path == topic {
			return command.QuickStart + "\n" + command.Summary + "\n" + command.Composition + "\n", true
		}
	}
	return "", false
}

var rulingHelperTopics = []localHelperTopic{
	{Path: "rulings list", Summary: "Read active operating and answer rulings. Include history with --include-revoked.", QuickStart: "anx rulings list [--include-revoked]", Flags: []localHelperFlag{{Name: "--include-revoked", Description: "Include inactive history and proposals."}}},
	{Path: "rulings show", Summary: "Look up a ruling by date ID or alias.", QuickStart: "anx rulings show <id-or-alias>"},
	{Path: "rulings pull", Summary: "Refresh the local projection, even within its TTL.", QuickStart: "anx rulings pull"},
	{Path: "rulings check", Summary: "Check local citations. Exit 0 for clean, 1 for findings, and 2 for usage or index errors.", QuickStart: "anx rulings check [paths...] [--surfaces FILE] [--strict-legacy]", Flags: []localHelperFlag{{Name: "--surfaces <file>", Description: "Select tracked files by include, exclude and live_include globs."}, {Name: "--strict-legacy", Description: "Flag bare active legacy citations."}}},
	{Path: "rulings propose", Summary: "Send an operating ruling for human confirmation in Decide.", QuickStart: "anx rulings propose --id-date YYYY-MM-DD --scope account|workspace --topic <topic> --body-file <md> --quote <text>", Composition: "--id-date also accepts a pinned R-YYYYMMDD-NN. Repeat --relation supersedes|amends|independent:R-YYYYMMDD-NN and --alias as needed.", Flags: []localHelperFlag{{Name: "--id-date <id-or-date>"}, {Name: "--scope <scope>"}, {Name: "--topic <topic>"}, {Name: "--body-file <path>"}, {Name: "--quote <text>"}, {Name: "--alias <name>"}, {Name: "--relation <kind:id>"}, {Name: "--title <title>"}}},
	{Path: "rulings import", Summary: "Import a generated Phase 0 index as proposals for human review in Decide.", QuickStart: "anx rulings import --file index.json", Flags: []localHelperFlag{{Name: "--file <path>", Description: "Generated schema version 1 index."}}},
}

func init() {
	localHelperTopics = append(localHelperTopics, rulingHelperTopics...)
}
