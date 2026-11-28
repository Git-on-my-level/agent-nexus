package app

func rulingHelp(topic string) (string, bool) {
	if topic == "rulings" {
		return rulingsUsage() + "\nRead operating rules with list and show. Humans confirm proposals in Decide; an agent confirms only on a recorded human decision.\n" +
			"Use ruling: R-YYYYMMDD-NN in instructions. Run check to catch outdated references.\n" +
			"Help: anx help rulings list|show|pull|check|propose|import|withdraw|confirm\n", true
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
	{Path: "rulings withdraw", Summary: "Withdraw unconfirmed proposals and free their names. Agents withdraw their own; humans any. A ruling still related to one in the set fails the request, so withdraw related proposals together.", QuickStart: "anx rulings withdraw R-YYYYMMDD-NN...", Composition: "To merge proposals, withdraw them, then propose the merged ruling with --alias for each withdrawn date ID and legacy name."},
	{Path: "rulings confirm", Summary: "Confirm proposals on a human decision you recorded. The decision note must carry --decided-by and name every ruling ID it confirms. Without a recorded decision, a human confirms in Decide.", QuickStart: "anx rulings confirm R-YYYYMMDD-NN... --decision event:<id>", Composition: "Record the decision first: anx note <card> \"<decision naming the ruling IDs>\" --decided-by <who>. Pass its note_ref as --decision.", Flags: []localHelperFlag{{Name: "--decision <event-ref>", Description: "The note_ref of the recorded human decision."}}},
	{Path: "rulings import", Summary: "Import a rulings index as proposals for human review in Decide.", QuickStart: "anx rulings import --file index.json [--max-entries <count>] [--dry-run]", Composition: "Version 2 adds required scope (account or workspace:<alias>), topic, body and source_quote to each generated index row. Account rows go to the selected workspace. Supply relationships in relations as {kind, ruling_id} objects. Each legacy supersedes target needs a matching typed relation. Version 1 retains heading-based defaults and emits a warning. --dry-run runs the same live projection checks, then shows counts and up to three rows per workspace without submitting proposals.", Flags: []localHelperFlag{{Name: "--file <path>", Description: "Generated schema version 1 or 2 index."}, {Name: "--max-entries <count>", Description: "Maximum entries per import. Default 1000."}, {Name: "--dry-run", Description: "Validate against live projections and preview without submitting."}}},
}

func init() {
	localHelperTopics = append(localHelperTopics, rulingHelperTopics...)
}
