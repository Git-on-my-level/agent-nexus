package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/output"
)

// Persona sprawl guard.
//
// Every unknown --as/ANX_AS name becomes a new persona on its host the first
// time it is used, and nothing removes it. The CLI stays non-interactive and
// never blocks registration; instead it points agents at names the host
// already has, and warns when a new name looks like a revision of one.

const maxListedHostNames = 12

// personaNameSimilarCode is the warning code for a newly registered name that
// shares a stem with an existing persona on the same host.
const personaNameSimilarCode = "persona_name_similar"

const unresolvedIdentityRepair = "pass --as <agent-name> or set ANX_AS=<agent-name>: reuse a name already used on this host when one fits, otherwise use the lowercase name of the agent tool you are running in. A new name becomes a new persona on its first call, so never invent one per task. Stop only if you cannot tell which agent tool you are running in"

// unresolvedIdentityRepairFor adds the names known on this machine when there
// are any. They come from local token caches, so they are names this machine
// has used, not a statement that the persona is active.
func unresolvedIdentityRepairFor(names []string) string {
	if len(names) == 0 {
		return unresolvedIdentityRepair
	}
	return strings.Replace(unresolvedIdentityRepair, "already used on this host", "already used on this host (names known on this machine: "+strings.Join(names, ", ")+")", 1)
}

func unresolvedIdentityError(reason string) error {
	return errnorm.Usage("identity_unresolved", reason+"; "+unresolvedIdentityRepair)
}

// knownHostPersonaNames lists the names this configuration has used on its
// enrolled host, from the per-name token caches beside the host key. It never
// calls the server and returns nil when the host is not enrolled.
func (a *App) knownHostPersonaNames(cfg config.Resolved) []string {
	host, err := a.resolvedHost(cfg)
	if err != nil || host.PrivateKeyPath == "" {
		return nil
	}
	entries, err := os.ReadDir(filepath.Dir(host.PrivateKeyPath))
	if err != nil {
		return nil
	}
	names := []string{}
	for _, entry := range entries {
		file := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(file, "token-") || !strings.HasSuffix(file, ".json") {
			continue
		}
		if name := strings.TrimSuffix(strings.TrimPrefix(file, "token-"), ".json"); agentNamePattern.MatchString(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) > maxListedHostNames {
		names = names[:maxListedHostNames]
	}
	return names
}

// withKnownHostNames rewrites an identity_unresolved error so its repair
// lists the names known on this machine.
func (a *App) withKnownHostNames(cfg config.Resolved, err error) error {
	var typed *errnorm.Error
	if !errors.As(err, &typed) || typed.Code != "identity_unresolved" || !strings.Contains(typed.Message, unresolvedIdentityRepair) {
		return err
	}
	names := a.knownHostPersonaNames(cfg)
	if len(names) == 0 {
		return err
	}
	copy := *typed
	copy.Message = strings.Replace(typed.Message, unresolvedIdentityRepair, unresolvedIdentityRepairFor(names), 1)
	return &copy
}

// archivedNameError explains an agent_archived refusal (a human archived the
// name; sweep archives are cleared by use and never reach here) with the
// other names known on this machine.
func (a *App) archivedNameError(cfg config.Resolved, name string, err *errnorm.Error) error {
	others := []string{}
	for _, known := range a.knownHostPersonaNames(cfg) {
		if known != name {
			others = append(others, known)
		}
	}
	message := fmt.Sprintf("agent name %q was archived by a human on this host and stays reserved; use another name already used on this host", name)
	if len(others) > 0 {
		message += " (names known on this machine: " + strings.Join(others, ", ") + ")"
	}
	err.Message = message + ", or ask a human to restore it on the Access page"
	return err
}

// notePersonaReuse warns, without blocking, that a just-registered name looks
// like a revision of an existing persona on this host.
func (a *App) notePersonaReuse(name string, similar []string) {
	if len(similar) == 0 {
		return
	}
	verb, pronoun := "exists", "it"
	if len(similar) > 1 {
		verb, pronoun = "exist", "one"
	}
	message := fmt.Sprintf("%s %s on this host. Reuse %s, or name this persona for its purpose.", strings.Join(similar, ", "), verb, pronoun)
	if a.Stderr != nil {
		a.writeOutput(a.Stderr, "! "+message+"\n")
	}
	a.identityWarnings = append(a.identityWarnings, output.Warning{Code: personaNameSimilarCode, Message: message, Details: map[string]any{"name": name, "similar_names": similar}})
}
