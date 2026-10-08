package app

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/registry"
)

const pmActivityBudget = 250 * time.Millisecond

var pmActivityRef = regexp.MustCompile(`^(card|work|topic|board|doc|document|event|artifact|thread|decision):[A-Za-z0-9][A-Za-z0-9_-]{0,140}$`)

// Only known command vocabulary and declared resource selectors are public.
// Positional free text, URLs, paths, bodies, search terms and credentials are
// never inferred as targets. Local/help/transport-management commands are silent.
func pmToolStep(args []string) (string, string) {
	name, tail, known := matchPreflightFlagSpec(args)
	// Commands without flags are absent from the preflight map. Use canonical
	// command vocabulary too, taking the longest exact prefix only.
	for _, command := range registry.CommandSpecs() {
		words := strings.Fields(command.CLIPath)
		if len(words) == 0 || len(words) > len(args) || (known && len(words) <= len(strings.Fields(name))) {
			continue
		}
		match := true
		for i, word := range words {
			if args[i] != word {
				match = false
				break
			}
		}
		if match {
			name, tail, known = command.CLIPath, args[len(words):], true
		}
	}
	if !known || workspaceIndependentCommand(args) {
		return "", ""
	}
	if name == "pm serve" || name == "pm turns heartbeat" || name == "pm turns claim" || name == "pm turns complete" || name == "pm turns fail" || name == "pm turns release" || strings.HasPrefix(name, "secret ") || strings.HasPrefix(name, "auth ") || strings.HasPrefix(name, "host ") || strings.HasPrefix(name, "api ") {
		return "", ""
	}
	target := ""
	// Only explicitly resource-valued flags may name a target.
	allowed := map[string]bool{"--work-ref": true, "--context-ref": true, "--card-id": true, "--topic-id": true, "--document-id": true, "--board-id": true, "--thread-id": true, "--event-id": true, "--artifact-id": true}
	spec := preflightFlagSpecs()[name]
	for i := 0; i < len(tail); i++ {
		token := tail[i]
		if token == "--" {
			break
		}
		if !strings.HasPrefix(token, "-") {
			continue
		}
		flag, value, eq := strings.Cut(token, "=")
		definition, exists := spec[strings.TrimLeft(flag, "-")]
		if !exists {
			return "anx " + name, ""
		}
		if definition.kind != preflightFlagBool && !eq && i+1 < len(tail) {
			i++
			value = tail[i]
		}
		if allowed[flag] && pmActivityRef.MatchString(value) {
			target = value
			break
		}
	}

	// Resource get/inspect and turn-context calls have a leading resource ref.
	if target == "" && len(tail) > 0 && (strings.HasSuffix(name, " get") || strings.HasSuffix(name, " inspect") || name == "work context" || name == "cards messages" || name == "topics messages") && pmActivityRef.MatchString(tail[0]) {
		target = tail[0]
	}
	return "anx " + name, target
}

func (a *App) emitPMToolActivity(args []string, cfg config.Resolved) {
	if a.Getenv("ANX_PM_ACTIVITY_ENABLED") != "1" {
		return
	}
	id, token := a.Getenv("ANX_PM_TURN_ID"), a.Getenv("ANX_PM_LEASE_TOKEN")
	if id == "" || len(id) > 256 || token == "" || strings.TrimRight(cfg.BaseURL, "/") != strings.TrimRight(a.Getenv("ANX_PM_BASE_URL"), "/") || cfg.Agent != a.Getenv("ANX_PM_AGENT") {
		return
	}
	label, target := pmToolStep(args)
	if label == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), pmActivityBudget)
	defer cancel()
	_, _ = a.invokeRawJSON(ctx, cfg, "pm turns heartbeat", http.MethodPost, "/pm/turns/"+url.PathEscape(id)+"/heartbeat", map[string]any{
		"lease_token": token, "activity_append": []map[string]any{{"kind": "tool", "label": label, "target": target}},
	})
}

func (a *App) emitPMProgress(ctx context.Context, cfg config.Resolved, id, token string, p *pmProgress) {
	bounded, cancel := context.WithTimeout(ctx, pmActivityBudget)
	defer cancel()
	_, _ = a.invokeRawJSON(bounded, cfg, "pm turns heartbeat", http.MethodPost, "/pm/turns/"+url.PathEscape(id)+"/heartbeat", p.body(token))
}
