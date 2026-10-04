package app

import (
	"context"
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

type planCommand struct {
	name, card, step, file, token string
	fields                        map[string]string
}

func planHelpText(topic string) (string, bool) {
	if topic != "plan" && topic != "plan step" && topic != "plan show" && topic != "plan set" && topic != "plan step add" && topic != "plan step update" && topic != "plan step rm" && topic != "refs" && topic != "refs resolve" {
		return "", false
	}
	if strings.HasPrefix(topic, "refs") {
		return "anx refs resolve <ref>... | --from-file <path|-> (JSON {refs:[]}); at most 200 refs, preserving unknown entries.\n", true
	}
	return "One plan per initiative card. Add linked steps; never select a view.\n" +
		"anx plan show <card>\n" +
		"anx plan set <card> --from-file <path|-> [--if-updated-at <timestamp>] (JSON {steps:[]})\n" +
		"anx plan step add <card> --title <title> [--step-id <slug>] [--ref <ref|url>] [--after a,b] [--due <date>] [--status done|active|blocked|not_started]\n" +
		"anx plan step update <card> <step-id> [--title ...] [--ref ...] [--after a,b] [--due ...] [--status ...]\n" +
		"anx plan step rm <card> <step-id>\n" +
		"Writes read the latest plan then use its concurrency token. Conflicts require an explicit retry. Removing a depended-on step is rejected; update dependencies first.\n", true
}

func parsePlanCommand(args []string) (planCommand, error) {
	p := planCommand{fields: map[string]string{}, name: "plan"}
	if len(args) < 2 {
		return p, errnorm.Usage("subcommand_required", "run anx help plan")
	}
	p.name = "plan " + args[1]
	width := 2
	if args[1] == "step" {
		if len(args) < 3 {
			return p, errnorm.Usage("subcommand_required", "run anx help plan step")
		}
		p.name += " " + args[2]
		width = 3
	}
	switch p.name {
	case "plan show", "plan set", "plan step add", "plan step update", "plan step rm":
	default:
		return p, errnorm.Usage("unknown_subcommand", "unknown plan command; run anx help plan")
	}
	fs := newSilentFlagSet(p.name)
	fs.StringVar(&p.card, "card-id", "", "Card ref, handle or id")
	if p.name == "plan set" {
		fs.StringVar(&p.file, "from-file", "", "Plan JSON file or -")
		fs.StringVar(&p.token, "if-updated-at", "", "Concurrency token from anx plan show")
	}
	if p.name == "plan step add" || p.name == "plan step update" {
		keys := []string{"title", "ref", "after", "due", "status"}
		if p.name == "plan step add" {
			keys = append(keys, "step-id")
		}
		for _, key := range keys {
			k := key
			fs.Func(k, "Step "+k, func(v string) error { p.fields[k] = v; return nil })
		}
	}
	tail := args[width:]
	positions := []string{}
	for len(tail) > 0 && !strings.HasPrefix(tail[0], "-") {
		positions = append(positions, tail[0])
		tail = tail[1:]
	}
	if err := fs.Parse(tail); err != nil {
		return p, errnorm.Usage("invalid_flags", err.Error())
	}
	positions = append(positions, fs.Args()...)
	if len(positions) > 0 && (p.card == "" || (p.name != "plan step update" && p.name != "plan step rm")) {
		if p.card != "" {
			return p, errnorm.Usage("invalid_request", "use a positional card or --card-id, not both")
		}
		p.card = positions[0]
		positions = positions[1:]
	}
	if p.name == "plan step update" || p.name == "plan step rm" {
		if len(positions) > 0 {
			p.step = positions[0]
			positions = positions[1:]
		}
		if p.step == "" {
			return p, errnorm.Usage("invalid_request", "step id is required")
		}
	}
	if p.card == "" || len(positions) > 0 {
		return p, errnorm.Usage("invalid_request", "one card ref is required")
	}
	if p.name == "plan set" && p.file == "" {
		return p, errnorm.Usage("invalid_request", "--from-file is required")
	}
	if p.name == "plan step add" && strings.TrimSpace(p.fields["title"]) == "" {
		return p, errnorm.Usage("invalid_request", "--title is required")
	}
	if p.name == "plan step update" && len(p.fields) == 0 {
		return p, errnorm.Usage("invalid_request", "at least one step field is required")
	}
	if value, ok := p.fields["status"]; ok && value != "" && value != "done" && value != "active" && value != "blocked" && value != "not_started" {
		return p, errnorm.Usage("invalid_request", "invalid step status")
	}
	if p.token != "" {
		if _, err := time.Parse(time.RFC3339Nano, p.token); err != nil {
			return p, errnorm.Usage("invalid_request", "invalid --if-updated-at timestamp")
		}
	}
	return p, nil
}

var planSlugNonWord = regexp.MustCompile(`[^a-z0-9]+`)

func init() {
	for _, verb := range []string{"add", "update", "rm"} {
		localHelperTopics = append(localHelperTopics, localHelperTopic{
			Path:        "plan step " + verb,
			Summary:     "Edit a linked initiative step with a card concurrency token.",
			Composition: "Read the current plan, then PUT the edited graph. Conflicts require reconciliation; graph shape is computed.",
			JSONShape:   "`card_ref`, `plan`, `plan_state`, `if_updated_at`",
			Examples:    []string{"anx plan step add card:launch --title QA --after build"},
		})
	}
}

func (a *App) runPlanCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	p, err := parsePlanCommand(args)
	if err != nil {
		return nil, p.name, err
	}
	path := "/cards/" + url.PathEscape(p.card) + "/plan"
	var plan map[string]any
	if p.name == "plan set" {
		raw, err := a.readBodyInput(p.file)
		if err != nil {
			return nil, p.name, err
		}
		if err = json.Unmarshal(raw, &plan); err != nil || plan == nil {
			return nil, p.name, errnorm.Usage("invalid_json", "--from-file must contain a plan object {steps:[]}")
		}
		if _, ok := plan["steps"].([]any); !ok {
			return nil, p.name, errnorm.Usage("invalid_json", "plan steps must be an array")
		}
	}
	current, err := a.invokeRawJSON(ctx, cfg, "plan show", "GET", path, nil)
	if err != nil {
		return nil, p.name, err
	}
	if p.name == "plan show" {
		return current, p.name, nil
	}
	body, ok := planResultData(current.Data)["body"].(map[string]any)
	if !ok {
		return nil, p.name, errnorm.Internal("invalid_response", "plan show returned an invalid object")
	}
	if p.token == "" {
		p.token, _ = body["if_updated_at"].(string)
	}
	if p.token == "" {
		return nil, p.name, errnorm.Internal("invalid_response", "plan show omitted the concurrency token")
	}
	if p.name != "plan set" {
		plan, _ = body["plan"].(map[string]any)
		if plan == nil {
			plan = map[string]any{"steps": []any{}}
		}
		steps, ok := plan["steps"].([]any)
		if !ok {
			return nil, p.name, errnorm.Internal("invalid_response", "plan steps must be an array")
		}
		index := -1
		for i, raw := range steps {
			if step, ok := raw.(map[string]any); ok && step["id"] == p.step {
				index = i
			}
		}
		if p.name == "plan step add" {
			id := p.fields["step-id"]
			if id == "" {
				id = strings.Trim(planSlugNonWord.ReplaceAllString(strings.ToLower(p.fields["title"]), "-"), "-")
			}
			if id == "" || len(id) > 64 {
				return nil, p.name, errnorm.Usage("invalid_request", "choose a slug with --step-id (1..64 bytes)")
			}
			for _, raw := range steps {
				if step, ok := raw.(map[string]any); ok && step["id"] == id {
					return nil, p.name, errnorm.Usage("invalid_request", "step id already exists; choose --step-id")
				}
			}
			steps = append(steps, map[string]any{"id": id, "title": p.fields["title"], "after": []string{}})
			index = len(steps) - 1
		} else if index < 0 {
			return nil, p.name, errnorm.Usage("not_found", "step id does not exist")
		}
		if p.name == "plan step rm" {
			steps = append(steps[:index], steps[index+1:]...)
		} else {
			step, ok := steps[index].(map[string]any)
			if !ok {
				return nil, p.name, errnorm.Internal("invalid_response", "invalid step")
			}
			for key, value := range p.fields {
				if key == "step-id" {
					continue
				}
				if key == "after" {
					after := []string{}
					if value != "" {
						for _, dep := range strings.Split(value, ",") {
							after = append(after, strings.TrimSpace(dep))
						}
					}
					step[key] = after
				} else if value == "" {
					delete(step, key)
				} else {
					step[key] = value
				}
			}
		}
		plan["steps"] = steps
	}
	result, err := a.invokeRawJSON(ctx, cfg, p.name, "PUT", path, map[string]any{"plan": plan, "if_updated_at": p.token})
	return result, p.name, err
}

func parseRefResolve(args []string) ([]string, string, error) {
	if len(args) < 2 || args[1] != "resolve" {
		return nil, "", errnorm.Usage("unknown_subcommand", "run anx help refs resolve")
	}
	fs := newSilentFlagSet("refs resolve")
	var file string
	fs.StringVar(&file, "from-file", "", "JSON {refs:[]} from path or -")
	tail := args[2:]
	refs := []string{}
	for len(tail) > 0 && !strings.HasPrefix(tail[0], "-") {
		refs = append(refs, tail[0])
		tail = tail[1:]
	}
	if err := fs.Parse(tail); err != nil {
		return nil, "", errnorm.Usage("invalid_flags", err.Error())
	}
	refs = append(refs, fs.Args()...)
	if len(refs) > 200 || (file != "" && len(refs) > 0) || (file == "" && len(refs) == 0) {
		return nil, "", errnorm.Usage("invalid_request", "supply 1..200 refs or --from-file")
	}
	return refs, file, nil
}

func (a *App) runRefResolve(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	refs, file, err := parseRefResolve(args)
	if err != nil {
		return nil, err
	}
	if file != "" {
		raw, err := a.readBodyInput(file)
		if err != nil {
			return nil, err
		}
		var body struct {
			Refs []string `json:"refs"`
		}
		if json.Unmarshal(raw, &body) != nil || body.Refs == nil || len(body.Refs) > 200 {
			return nil, errnorm.Usage("invalid_json", "expected {refs:[]} with at most 200 strings")
		}
		refs = body.Refs
	}
	return a.invokeRawJSON(ctx, cfg, "refs resolve", "POST", "/refs/resolve", map[string]any{"refs": refs})
}

func planResultData(data any) map[string]any {
	out, _ := data.(map[string]any)
	return out
}
