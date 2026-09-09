package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"unicode"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/registry"
)

// The central API owns work, ordering, authorization and evidence. This table only
// defines the CLI grammar for the canonical routes; it never caches durable state.
type workCommandSpec struct {
	path, method, idFlag, summary string
	body                          bool
	filters                       []string
}

var workCommands = map[string]workCommandSpec{
	"work list":                {path: "/work", method: "GET", summary: "List work cards across sources in the authenticated workspace.", filters: []string{"project-ref", "source", "owner", "phase", "freshness", "q", "limit", "cursor"}},
	"work get":                 {path: "/work/{id}", method: "GET", idFlag: "work-id", summary: "Read one work card, source authority, executions and current evidence."},
	"work create":              {path: "/work", method: "POST", body: true, summary: "Register a native commitment or canonical external source on an existing board."},
	"work patch":               {path: "/work/{id}", method: "PATCH", idFlag: "work-id", body: true, summary: "Update work metadata with if_version; external status remains source-owned."},
	"work context":             {path: "/work/{id}", method: "GET", idFlag: "work-id", summary: "Compose work, a bounded observation page and refresh status using read-only requests.", filters: []string{"limit", "cursor"}},
	"work freshness":           {path: "/work/{id}", method: "GET", idFlag: "work-id", summary: "Inspect last observed, source activity and meaningful progress independently."},
	"work refresh get":         {path: "/work/{id}/refresh", method: "GET", idFlag: "work-id", summary: "Read refresh state without queueing work."},
	"work refresh request":     {path: "/work/{id}/refresh", method: "POST", idFlag: "work-id", summary: "Request a bounded refresh; queued is not a successful observation."},
	"work capabilities":        {path: "/work/capabilities", method: "GET", summary: "Read capabilities actually advertised by the authenticated central API."},
	"work observations list":   {path: "/work/{id}/observations", method: "GET", idFlag: "work-id", summary: "Read append-only evidence for a work card, preserving pagination and uncertainty.", filters: []string{"limit", "cursor"}},
	"work observations submit": {path: "/work/{id}/observations", method: "POST", idFlag: "work-id", body: true, summary: "Submit an authenticated remote observation; preserve its idempotency key on retry."},
	"pm context":               {path: "/pm/context", method: "GET", summary: "Read bounded authorized PM context; partial coverage stays explicit.", filters: []string{"work-ref", "query", "limit"}},
	"pm conversations list":    {path: "/pm/conversations", method: "GET", summary: "List durable PM conversations with principal-bound pagination.", filters: []string{"limit", "cursor"}},
	"pm conversations create":  {path: "/pm/conversations", method: "POST", body: true, summary: "Create a durable conversation using request_key, title and optional work_ref."},
	"pm conversations get":     {path: "/pm/conversations/{id}", method: "GET", idFlag: "conversation-id", summary: "Read a conversation and its durable turns."},
	"pm conversations message": {path: "/pm/conversations/{id}/messages", method: "POST", idFlag: "conversation-id", body: true, summary: "Queue a PM message using request_key and text; an accepted turn is not a completed outcome."},
	"pm decisions list":        {path: "/pm/decisions", method: "GET", summary: "List durable decisions with principal-bound pagination.", filters: []string{"limit", "cursor"}},
	"pm decisions get":         {path: "/pm/decisions/{id}", method: "GET", idFlag: "decision-id", summary: "Read an instruction, authorization scope, revision and answer status."},
	"pm decisions create":      {path: "/pm/decisions", method: "POST", body: true, summary: "Propose an instruction bound to work, scope and target_revision; never approves it."},
	"pm decisions answer":      {path: "/pm/decisions/{id}/answer", method: "POST", idFlag: "decision-id", body: true, summary: "Answer with revision, approve and text; the server requires an authorized human principal."},
	"pm decisions dispatch":    {path: "/pm/decisions/{id}/dispatch", method: "POST", idFlag: "decision-id", summary: "Explicitly dispatch authorized intent; inspect action receipt for actual outcome."},
	"pm actions list":          {path: "/pm/actions", method: "GET", summary: "Report durable action and receipt statuses with principal-bound pagination.", filters: []string{"limit", "cursor"}},
	"pm actions get":           {path: "/pm/actions/{id}", method: "GET", idFlag: "action-id", summary: "Read authorization, attempts and receipt; source_reported is not verified."},
	"pm actions reconcile":     {path: "/pm/actions/{id}/reconcile", method: "POST", idFlag: "action-id", summary: "Request authoritative read-back of an action receipt; does not resend the action."},
	"pm turns context":         {path: "/pm/turns/{id}/context", method: "GET", idFlag: "turn-id", summary: "Read context as the requesting actor; only the selected PM agent may call this.", filters: []string{"query", "limit"}},
	"pm turns claim":           {path: "/pm/turns/claim", method: "POST", summary: "Claim the next queued turn with an exclusive runner lease. 204 means none."},
	"pm turns fail":            {path: "/pm/turns/{id}/fail", method: "POST", idFlag: "turn-id", body: true, summary: "Mark a claimed turn failed with a reason; does not complete work."},
	"pm turns propose":         {path: "/pm/turns/{id}/decisions", method: "POST", idFlag: "turn-id", body: true, summary: "Selected PM agent proposes an instruction for the requesting actor, never approval."},
	"pm turns complete":        {path: "/pm/turns/{id}/complete", method: "POST", idFlag: "turn-id", body: true, summary: "Selected PM agent records response text and evidence_refs; does not complete work."},
}

type parsedWorkCommand struct {
	spec               workCommandSpec
	name, id, fromFile string
	query              url.Values
}

func isWorkCommandRoot(root string) bool {
	for name := range workCommands {
		if strings.SplitN(name, " ", 2)[0] == root {
			return true
		}
	}
	return false
}

// Parsing is shared by preflight and execution, so malformed requests fail before
// profile resolution, token refresh, file reads or network activity.
func parseWorkCommand(args []string) (parsedWorkCommand, error) {
	out := parsedWorkCommand{query: url.Values{}}
	if len(args) < 2 {
		return out, errnorm.Usage("subcommand_required", "a subcommand is required; run anx help "+args[0])
	}
	width := 2
	var spec workCommandSpec
	ok := false
	for n := len(args); n >= 2; n-- {
		if candidate, found := workCommands[strings.Join(args[:n], " ")]; found {
			spec, width, ok = candidate, n, true
			break
		}
	}
	out.name = strings.Join(args[:width], " ")
	if !ok {
		return out, errnorm.Usage("unknown_subcommand", fmt.Sprintf("unknown %s subcommand %q; run anx help %s", args[0], args[1], args[0]))
	}
	out.spec = spec
	fs := newSilentFlagSet(out.name)
	var id, fromFile trackedString
	var limit trackedInt

	values := map[string]*trackedString{}
	if spec.idFlag != "" {
		fs.Var(&id, spec.idFlag, "Public ref, handle or id")
	}
	if spec.body {
		fs.Var(&fromFile, "from-file", "JSON request from path or - for stdin")
	}
	for _, key := range spec.filters {
		if key == "limit" {
			fs.Var(&limit, key, "Page size (1..200)")
		} else {
			v := new(trackedString)
			values[key] = v
			fs.Var(v, key, "Filter or opaque cursor")
		}
	}
	tail := args[width:]
	var leading []string
	// Resource ids lead the command in the public convention. Go flag stops at
	// positionals, so lift only leading positionals before parsing the flag tail.
	for len(tail) > 0 && !strings.HasPrefix(tail[0], "-") {
		leading = append(leading, tail[0])
		tail = tail[1:]
	}
	if err := fs.Parse(tail); err != nil {
		return out, errnorm.Usage("invalid_flags", err.Error())
	}
	positions := append(leading, fs.Args()...)
	if spec.idFlag != "" && len(positions) > 0 {
		if id.set {
			return out, errnorm.Usage("invalid_request", "use a leading resource id or --"+spec.idFlag+", not both")
		}
		id.value = positions[0]
		positions = positions[1:]
	}
	if len(positions) > 0 {
		return out, errnorm.Usage("invalid_args", "unexpected positional arguments for anx "+out.name)
	}
	if spec.idFlag != "" {
		out.id = strings.TrimSpace(id.value)
		if out.id == "" {
			return out, errnorm.Usage("invalid_request", "a resource ref is required; use a leading ref or --"+spec.idFlag)
		}
		if out.id == "." || out.id == ".." || strings.ContainsAny(out.id, "/\\?#%") || strings.ContainsFunc(out.id, unicode.IsControl) {
			return out, errnorm.Usage("invalid_request", "resource id must be a ref, handle or id, not a URL or path")
		}
	}
	if limit.set {
		maxLimit := 200
		if out.name == "pm context" || out.name == "pm turns context" {
			maxLimit = 50
		}
		if limit.value < 1 || limit.value > maxLimit {
			return out, errnorm.Usage("invalid_request", fmt.Sprintf("--limit must be between 1 and %d", maxLimit))
		}
		out.query.Set("limit", fmt.Sprint(limit.value))
	}
	for k, v := range values {
		if v.set {
			if strings.TrimSpace(v.value) == "" {
				return out, errnorm.Usage("invalid_request", "--"+k+" must not be empty")
			}
			out.query.Set(strings.ReplaceAll(k, "-", "_"), v.value)
		}
	}
	out.fromFile = strings.TrimSpace(fromFile.value)
	if spec.body && out.fromFile == "" {
		return out, errnorm.Usage("invalid_request", "--from-file <path|-> is required for anx "+out.name)
	}
	return out, nil
}

func (a *App) runWorkCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) >= 2 && args[0] == "pm" {
		switch args[1] {
		case "serve":
			result, err := a.runPMServe(ctx, args[2:], cfg)
			return result, "pm serve", err
		case "ask":
			result, err := a.runPMAsk(ctx, args[2:], cfg)
			return result, "pm ask", err
		case "channels":
			if len(args) >= 3 && args[2] == "doctor" {
				result, err := a.runPMChannelsDoctor(ctx, args[3:], cfg)
				return result, "pm channels doctor", err
			}
			return nil, "pm channels", errnorm.Usage("subcommand_required", "usage: anx pm channels doctor")
		}
	}
	if topic := strings.Join(args, " "); isWorkCommandGroup(topic) {
		text, _ := workHelpText(topic)
		return &commandResult{Text: text, Data: map[string]any{"help_text": text}}, topic, nil
	}
	parsed, err := parseWorkCommand(args)
	if err != nil {
		return nil, parsed.name, err
	}
	var body any
	if parsed.spec.body {
		raw, readErr := a.readBodyInput(parsed.fromFile)
		if readErr != nil {
			return nil, parsed.name, readErr
		}
		var object map[string]any
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.UseNumber()
		if err := decoder.Decode(&object); err != nil || object == nil {
			return nil, parsed.name, errnorm.Usage("invalid_json", "--from-file must contain one JSON object")
		}
		// json.Valid rejects trailing values while UseNumber above preserves sequence integers.
		if !json.Valid(raw) {
			return nil, parsed.name, errnorm.Usage("invalid_json", "--from-file must contain one JSON object")
		}
		body = object
	} else if parsed.spec.method == "POST" {
		body = map[string]any{}
	}
	method := parsed.spec.method
	path := strings.ReplaceAll(parsed.spec.path, "{id}", url.PathEscape(parsed.id))
	query := parsed.query
	if parsed.name == "work context" {
		query = nil
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	result, err := a.invokeRawJSON(ctx, cfg, parsed.name, method, path, body)
	if err != nil {
		return result, parsed.name, err
	}
	if parsed.name == "pm turns claim" {
		status, _ := asMap(result.Data)["status_code"].(int)
		if status == 204 {
			result.Text = "No claimable turn"
			return result, parsed.name, nil
		}
	}
	if commandResultBody(result) == nil {
		return nil, parsed.name, errnorm.New(errnorm.KindRemote, "invalid_response", "central API returned a non-object response; verify the configured API endpoint")
	}
	if parsed.name == "work context" {
		observationPath := path + "/observations"
		if len(parsed.query) > 0 {
			observationPath += "?" + parsed.query.Encode()
		}
		observations, readErr := a.invokeRawJSON(ctx, cfg, "work observations list", "GET", observationPath, nil)
		if readErr != nil {
			return nil, parsed.name, readErr
		}
		refresh, readErr := a.invokeRawJSON(ctx, cfg, "work refresh get", "GET", path+"/refresh", nil)
		if readErr != nil {
			return nil, parsed.name, readErr
		}
		composed := map[string]any{"work": asMap(commandResultBody(result))["work"], "observations": commandResultBody(observations), "refresh": asMap(commandResultBody(refresh))["refresh"]}
		asMap(result.Data)["body"] = composed
	}
	if parsed.name == "work freshness" {
		work := asMap(asMap(commandResultBody(result))["work"])
		asMap(result.Data)["body"] = map[string]any{"ref": work["ref"], "freshness": work["freshness"], "latest_observation": work["latest_observation"], "refresh": work["refresh"]}
	}
	result.Text = formatWorkCommandText(parsed.name, commandResultBody(result))
	if cfg.Verbose {
		result.Text = formatPrettyBody(commandResultBody(result))
	}
	if cfg.Headers {
		data := asMap(result.Data)
		status, _ := data["status_code"].(int)
		headers, _ := data["headers"].(map[string][]string)
		result.Text = formatBodyWithHeaders(status, headers, result.Text)
	}
	return result, parsed.name, nil
}

func workHelpText(topic string) (string, bool) {
	spec, exact := workCommands[topic]
	if !exact && !isWorkCommandGroup(topic) {
		return "", false
	}
	var b strings.Builder
	meta, _ := registry.LoadEmbedded()
	if cmd, found := commandByCLIPath(meta.Commands, mapRuntimePathToRegistryPath(topic)); found && exact {
		b.WriteString(formatGeneratedCommandHelp(topic, cmd, false))
		b.WriteString("\n\n")
	} else {
		fmt.Fprintf(&b, "Local Help: %s\n\n", topic)
	}
	b.WriteString("Work is an existing card; projects are topics. Scope and identity come from the selected authenticated workspace profile. No local tracker database.\n\n")
	if exact {
		fmt.Fprintf(&b, "%s\n\nUsage: anx %s", spec.summary, topic)
		if spec.idFlag != "" {
			fmt.Fprintf(&b, " <ref> (or --%s <ref>)", spec.idFlag)
		}
		if spec.body {
			b.WriteString(" --from-file <path|->")
		}
		b.WriteString("\n")
		for _, f := range spec.filters {
			fmt.Fprintf(&b, "  --%s <value>\n", f)
		}
		if spec.body {
			b.WriteString("\nJSON body follows the central API contract; use anx meta commands for generated schemas. Server validates scope, versions and evidence.\n")
		}
		if topic == "work observations submit" {
			b.WriteString("Body: {\"observation\":{\"idempotency_key\":\"stable-report-key\",\"reader_id\":\"reader\",\"reader_revision\":\"v1\",\"observed_at\":\"RFC3339 timestamp\",\"status\":\"reported\",\"facts\":{},\"evidence\":[]}}\nPreserve source_sequence and idempotency_key on retry; received_at and actor_id are server-owned. Remote verified labels remain claims.\n")
		}
	} else {
		names := []string{}
		for name := range workCommands {
			if strings.HasPrefix(name, topic+" ") {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Fprintf(&b, "  anx %-24s %s\n", name, workCommands[name].summary)
		}
	}
	if strings.HasPrefix(topic, "pm") {
		b.WriteString("\nPM lists accept --limit 1..200 and opaque --cursor; preserve next_cursor and has_more. Cursors are bound to the current workspace, principal and record kind. Context limits are 1..50. An answered decision is not proof of delivery or execution; inspect pm actions get. Agent keys cannot inherit human approval authority.\n")
	} else {
		b.WriteString("\nLists preserve next_cursor; pass it unchanged with --cursor. Reading does not refresh or mutate sources.\n")
	}
	b.WriteString("\nUse --json for one machine-readable envelope.\n")
	return b.String(), true
}

func formatWorkCommandText(name string, body any) string {
	root := asMap(body)
	if name == "work list" {
		rows, _ := root["work"].([]any)
		lines := []string{fmt.Sprintf("Work: %d", len(rows))}
		for _, row := range rows {
			lines = append(lines, renderWorkLine(asMap(row)))
		}
		if cursor := anyString(root["next_cursor"]); cursor != "" {
			lines = append(lines, "next_cursor: "+cursor)
		}
		return strings.Join(lines, "\n")
	}
	if name == "work observations list" {
		rows, _ := root["observations"].([]any)
		lines := []string{fmt.Sprintf("Observations: %d", len(rows))}
		for _, row := range rows {
			observation := asMap(row)
			lines = append(lines, fmt.Sprintf("%s  status=%s verification=%s observed=%s reader=%s", anyString(observation["id"]), anyString(observation["status"]), firstNonEmpty(anyString(observation["verification"]), "reported"), anyString(observation["observed_at"]), anyString(observation["reader_id"])))
			for _, key := range []string{"uncertainty", "coverage", "error"} {
				if value, ok := observation[key]; ok && value != nil {
					raw, _ := json.Marshal(value)
					lines = append(lines, "  "+key+": "+string(raw))
				}
			}
		}
		if cursor := anyString(root["next_cursor"]); cursor != "" {
			lines = append(lines, "next_cursor: "+cursor)
		}
		return strings.Join(lines, "\n")
	}
	if name == "pm decisions list" || name == "pm actions list" || name == "pm conversations list" {
		rows, _ := root["items"].([]any)
		lines := []string{fmt.Sprintf("%s: %d", strings.TrimPrefix(strings.TrimSuffix(name, " list"), "pm "), len(rows))}
		for _, row := range rows {
			item := asMap(row)
			line := fmt.Sprintf("%s  %s  status=%s", anyString(item["id"]), anyString(item["work_ref"]), firstNonEmpty(anyString(item["status"]), "unknown"))
			if title := firstNonEmpty(anyString(item["title"]), anyString(item["instruction"])); title != "" {
				line += "  " + strings.Join(strings.Fields(title), " ")
			}
			if name == "pm actions list" {
				receipt := asMap(item["receipt"])
				line += fmt.Sprintf(" verified=%t", receipt["independently_verified"] == true)
			}
			lines = append(lines, line)
		}
		if more, _ := root["has_more"].(bool); more {
			lines = append(lines, "has_more: true")
		}
		if cursor := anyString(root["next_cursor"]); cursor != "" {
			lines = append(lines, "next_cursor: "+cursor)
		}
		return strings.Join(lines, "\n")
	}
	if name == "work get" || name == "work create" || name == "work patch" {
		work := asMap(root["work"])
		lines := []string{renderWorkLine(work)}
		for _, key := range []string{"project_ref", "next_actor", "next_action", "wake_condition"} {
			if value := anyString(work[key]); value != "" {
				lines = append(lines, key+": "+value)
			}
		}
		// Keep evidence and acceptance visible; a phase is not a verification claim.
		for _, key := range []string{"source", "freshness", "definition_of_done", "blockers", "relations", "executions", "latest_observation", "refresh"} {
			if value, ok := work[key]; ok && value != nil {
				encoded, _ := json.Marshal(value)
				lines = append(lines, key+": "+string(encoded))
			}
		}
		return strings.Join(lines, "\n")
	}
	return formatPrettyBody(body)
}

func renderWorkLine(work map[string]any) string {
	return fmt.Sprintf("%s  %s  phase=%s source=%s freshness=%s", firstNonEmpty(anyString(work["ref"]), anyString(work["handle"]), anyString(work["id"])), anyString(work["title"]), firstNonEmpty(anyString(work["phase"]), "unknown"), firstNonEmpty(anyString(asMap(work["source"])["authority"]), "unknown"), firstNonEmpty(anyString(asMap(work["freshness"])["status"]), "unknown"))
}

func isWorkCommandGroup(topic string) bool {
	if _, exact := workCommands[topic]; exact {
		return false
	}
	for name := range workCommands {
		if strings.HasPrefix(name, topic+" ") {
			return true
		}
	}
	return false
}
