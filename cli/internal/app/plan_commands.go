package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
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
		return "anx refs resolve <ref>... | --from-file <path|-> (JSON {refs:[]}); at most 200 refs, preserving unknown entries. Series refs accept URL-encoded labels, _range (default 30d, max 90d) and _baseline; resolved metric includes latest, baseline, change and freshness.\n", true
	}
	usage := map[string]string{
		"plan show":        "anx plan show <card>",
		"plan set":         "anx plan set <card> --from-file <path|-> [--if-updated-at <timestamp>] (JSON {steps:[]})",
		"plan step add":    "anx plan step add <card> --title <title> [--step-id <slug>] [--ref <ref|url>] [--after a,b] [--due <date>] [--status done|active|blocked|not_started]",
		"plan step update": "anx plan step update <card> <step-id> [--title ...] [--ref ...] [--after a,b] [--due ...] [--status done|active|blocked|not_started]",
		"plan step rm":     "anx plan step rm <card> (<step-id> | --ref <card-ref>)",
	}
	if text, ok := usage[topic]; ok {
		text += "\nCard accepts a card: ref, handle or id (or --card-id). Text is compact; --verbose shows every field; --json keeps the complete envelope.\n"
		if topic != "plan show" {
			text += "Writes read the latest plan and use its concurrency token. Conflicts require reading anx plan show <card> and reconciling before retry.\n"
		}
		if topic == "plan step add" {
			text += "Omitting --step-id derives a slug from --title. --after is a comma-separated list of step ids.\n"
		}
		if topic == "plan step add" || topic == "plan step update" {
			text += planRefHelp
		}
		if topic == "plan set" {
			text += planSetRefHelp
		}
		if topic == "plan step update" {
			text += "Pass at least one field; an empty --after clears dependencies. Source state takes precedence over fallback --status.\n"
		}
		if topic == "plan step rm" {
			text += "Removing a depended-on step is rejected; update dependencies first.\n"
		}
		return text, true
	}
	return "One plan per initiative card. Add linked steps; never select a view.\n" +
		"anx plan show <card>\n" +
		"anx plan set <card> --from-file <path|-> [--if-updated-at <timestamp>] (JSON {steps:[]})\n" +
		"anx plan step add <card> --title <title> [--step-id <slug>] [--ref <ref|url>] [--after a,b] [--due <date>] [--status done|active|blocked|not_started]\n" +
		"anx plan step update <card> <step-id> [--title ...] [--ref ...] [--after a,b] [--due ...] [--status ...]\n" +
		"anx plan step rm <card> (<step-id> | --ref <card-ref>)\n" +
		planRefHelp +
		planSetRefHelp +
		"Writes read the latest plan then use its concurrency token. Conflicts require an explicit retry. Removing a depended-on step is rejected; update dependencies first.\n", true
}

const planRefHelp = "--ref accepts a typed ref (card:, doc:, document:, topic:, or series:) or an http(s) URL such as a Linear issue or GitHub pull request. Bare tracker ids (SCA-123) and pull numbers (#123) are rejected; pass the full URL.\n"

const planSetRefHelp = "Each step ref in --from-file is a typed ref or an http(s) URL. A bare tracker id (SCA-123) or pull number (#123) is a usage error that names the step; pass the full URL.\n"

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
			desc := "Step " + k
			if k == "ref" {
				desc = "Typed ref or http(s) URL; not a bare tracker id (SCA-123) or pull number (#123)"
			}
			fs.Func(k, desc, func(v string) error { p.fields[k] = v; return nil })
		}
	}
	if p.name == "plan step rm" {
		fs.Func("ref", "Remove the step for this exact card ref", func(v string) error { p.fields["ref"] = v; return nil })
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
		if p.step == "" && (p.name != "plan step rm" || p.fields["ref"] == "") {
			return p, errnorm.Usage("invalid_request", "step id is required")
		}
	}
	if p.name == "plan step rm" && p.fields["ref"] != "" && (p.step != "" || !strings.HasPrefix(p.fields["ref"], "card:")) {
		return p, errnorm.Usage("invalid_request", "use a step id or --ref card:<slug>, not both")
	}
	if p.card == "" || len(positions) > 0 {
		return p, errnorm.Usage("invalid_request", "one card ref is required")
	}
	if p.name == "plan set" && p.file == "" {
		return p, errnorm.Usage("invalid_request", "--from-file is required")
	}
	if p.name == "plan set" {
		if err := rejectBarePlanRefsInFile(p.file); err != nil {
			return p, err
		}
	}
	if p.name == "plan step add" && strings.TrimSpace(p.fields["title"]) == "" {
		return p, errnorm.Usage("invalid_request", "--title is required")
	}
	if p.name == "plan step update" && len(p.fields) == 0 {
		return p, errnorm.Usage("invalid_request", "at least one step field is required")
	}
	if value, ok := p.fields["status"]; ok && value != "" && value != "done" && value != "active" && value != "blocked" && value != "not_started" {
		return p, errnorm.Usage("invalid_request", "--status must be done, active, blocked or not_started; use active for work in progress")
	}
	if p.token != "" {
		if _, err := time.Parse(time.RFC3339Nano, p.token); err != nil {
			return p, errnorm.Usage("invalid_request", "invalid --if-updated-at timestamp")
		}
	}
	if err := rejectBarePlanRef(p.fields["ref"]); err != nil {
		return p, err
	}
	return p, nil
}

func rejectBarePlanRef(ref string) error {
	ref = strings.TrimSpace(ref)
	if !isBarePlanRef(ref) {
		return nil
	}
	return errnorm.Usage("invalid_request", fmt.Sprintf("--ref %q is not a ref or URL. Pass the full URL (for example the Linear or GitHub URL) or a typed ref such as card:<slug>.", ref))
}

func isBarePlanRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	return ref != "" && (bareTrackerID.MatchString(ref) || barePRNumber.MatchString(ref))
}

// rejectBarePlanRefsInFile checks a plan file before identity resolution.
// Stdin is validated when the body is read. A missing or invalid file keeps
// the existing read error.
func rejectBarePlanRefsInFile(path string) error {
	path = strings.TrimSpace(path)
	if path == "" || path == "-" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var plan map[string]any
	if json.Unmarshal(raw, &plan) != nil || plan == nil {
		return nil
	}
	return rejectBarePlanStepRefs(plan)
}

func rejectBarePlanStepRefs(plan map[string]any) error {
	if plan == nil {
		return nil
	}
	steps, ok := plan["steps"].([]any)
	if !ok {
		return nil
	}
	for i, raw := range steps {
		step, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		ref := strings.TrimSpace(anyString(step["ref"]))
		if !isBarePlanRef(ref) {
			continue
		}
		name := strings.TrimSpace(anyString(step["id"]))
		if name == "" {
			name = fmt.Sprintf("steps[%d]", i)
		} else {
			name = strconv.Quote(name)
		}
		return errnorm.Usage("invalid_request", fmt.Sprintf("step %s ref %q is not a ref or URL. Pass the full URL (for example the Linear or GitHub URL) or a typed ref such as card:<slug>.", name, ref))
	}
	return nil
}

var (
	planSlugNonWord = regexp.MustCompile(`[^a-z0-9]+`)
	bareTrackerID   = regexp.MustCompile(`^[A-Z][A-Z0-9]+-\d+$`)
	barePRNumber    = regexp.MustCompile(`^#\d+$`)
)

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
	addedStepID := ""
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
		if err := rejectBarePlanStepRefs(plan); err != nil {
			return nil, p.name, err
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
			if step, ok := raw.(map[string]any); ok && (step["id"] == p.step || p.name == "plan step rm" && p.fields["ref"] != "" && step["ref"] == p.fields["ref"]) {
				if index >= 0 {
					return nil, p.name, errnorm.Usage("ambiguous_step", "more than one plan step matches; choose a step id")
				}
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
					return nil, p.name, errnorm.WithDetails(errnorm.Usage("invalid_request", fmt.Sprintf("step id %q already exists; update that step or choose a different --step-id", id)), map[string]any{"card_ref": p.card})
				}
			}
			steps = append(steps, map[string]any{"id": id, "title": p.fields["title"], "after": []string{}})
			index = len(steps) - 1
			addedStepID = id
		} else if index < 0 {
			return nil, p.name, errnorm.WithDetails(errnorm.Usage("not_found", fmt.Sprintf("step id %q does not exist; read the plan to find its step ids", p.step)), map[string]any{"card_ref": p.card})
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
	if err != nil {
		errnorm.AnnotateDetail(err, "card_ref", p.card)
		return result, p.name, err
	}
	if addedStepID != "" && result != nil {
		cardRef := firstNonEmpty(anyString(commandResultBody(result)["card_ref"]), p.card)
		if data := asMap(result.Data); data != nil {
			if ref := planStepRef(cardRef, addedStepID); ref != "" {
				data["ref"] = ref
			}
		}
	}
	return result, p.name, nil
}

func planStepRef(cardRef, stepID string) string {
	cardRef = strings.TrimSpace(cardRef)
	stepID = strings.TrimSpace(stepID)
	if cardRef == "" || stepID == "" {
		return ""
	}
	if !strings.Contains(cardRef, ":") {
		cardRef = "card:" + cardRef
	}
	return cardRef + "#" + stepID
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
