// Unknown command/subcommand suggestion matching used by error next_actions.

package app

func closestCommand(input string, candidates []string) string {
	best, distance := "", 3
	for _, candidate := range candidates {
		d := editDistance(input, candidate)
		if d < distance || d == distance && candidate < best {
			best, distance = candidate, d
		}
	}
	return best
}

func editDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current := make([]int, len(b)+1)
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			current[j] = min(current[j-1]+1, previous[j]+1, previous[j-1]+cost)
		}
		previous = current
	}
	return previous[len(b)]
}

func validSubcommands(group string) []string {
	switch group {
	case "cards":
		return cardsSubcommandSpec.valid
	case "docs":
		return docsSubcommandSpec.valid
	case "topics":
		return topicsSubcommandSpec.valid
	case "boards":
		return boardsSubcommandSpec.valid
	case "notifications":
		return notificationsSubcommandSpec.valid
	case "inbox":
		return inboxSubcommandSpec.valid
	case "auth":
		return authSubcommandSpec.valid
	case "config":
		return configSubcommandSpec.valid
	case "config forge":
		return configForgeSubcommandSpec.valid
	case "debug":
		return []string{"threads", "events", "ref-edges", "derived", "actors", "inbox", "meta"}
	case "debug threads":
		return threadsSubcommandSpec.valid
	case "debug events":
		return eventsSubcommandSpec.valid
	case "debug inbox":
		return inboxSubcommandSpec.valid
	case "debug actors":
		return actorsSubcommandSpec.valid
	case "debug derived":
		return derivedSubcommandSpec.valid
	case "debug meta":
		return metaSubcommandSpec.valid
	case "auth invites":
		return authInvitesSubcommandSpec.valid
	case "auth bootstrap":
		return authBootstrapSubcommandSpec.valid
	case "auth principals":
		return authPrincipalsSubcommandSpec.valid
	case "auth audit":
		return authAuditSubcommandSpec.valid
	}
	return nil
}
