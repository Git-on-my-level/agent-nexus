// `anx work` command surface: the work command table, shared parsing used by
// preflight and execution, dispatch, and registry classification. Text
// projections live in work_text.go and work_pm_text.go, PM request-body
// assembly in work_pm_body.go.

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/runcontext"
)

// The central API owns work, ordering, authorization and evidence. This table only
// defines the CLI grammar for the canonical routes; it never caches durable state.
type workCommandSpec struct {
	path, method, idFlag, summary string
	body                          bool
	optionalBody                  bool
	filters                       []string
}

var workCommands = map[string]workCommandSpec{
	"initiative promote":         {path: "/work/{id}/promote", method: "POST", idFlag: "card-id", optionalBody: true, summary: "<card> --reason text: Deprecated: use anx work move <card> --root --reason text."},
	"initiative retire":          {path: "/work/{id}/retire", method: "POST", idFlag: "card-id", optionalBody: true, summary: "<card> --reason text [--to-board board:ref] [--under card:initiative]: Deprecated: use anx work move <card> --under <parent> --reason text."},
	"work cadence":               {method: "PATCH", summary: "Set cadence: <card> --every 1d [--budget 7d] [--stop-rule text], or --clear."},
	"work goal":                  {method: "PATCH", summary: "Set goal: <card> --metric score --direction up [--target n] [--baseline n] [--label text] [--unit unit] [--series series:ref], or --clear."},
	"work receipt":               {method: "POST", summary: "Record a run: <card> --run-id id --outcome acted|measured|no_op|infra_error --summary text [--metric k=v] [--candidate X --decision kept|reverted|rejected --score-before n --score-after n] [--ref ref] [--started-at RFC3339]. Prints original event id on replay."},
	"pm card":                    {method: "POST", summary: "Read any visible card in full under the requesting reader."},
	"pm propose":                 {method: "POST", summary: "Propose a card status or note for human approval in Inbox."},
	"pm disconnect":              {path: "/pm/disconnect", method: "POST", summary: "Reset workspace PM onboarding after explicit uninstall."},
	"pm connect":                 {path: "/pm/connect", method: "POST", body: true, summary: "Register a local PM connection with bounded runner and host labels."},
	"pm presence":                {path: "/pm/presence", method: "GET", summary: "Read recent PM connection state."},
	"report render":              {path: "/docs/{id}/report", method: "GET", idFlag: "document-id", summary: "Materialize authorized report panels; --panels id,id selects individual panels.", filters: []string{"panels"}},
	"sessions register":          {path: "/sessions", method: "POST", body: true, summary: "Register or refresh a private provider session with a monotonic sequence; never creates an agent credential or assigns work."},
	"sessions get":               {path: "/sessions/{id}", method: "GET", idFlag: "session-id", summary: "Read your own registered provider session and bounded activity; does not expose conversation history."},
	"work participants register": {path: "/work/{id}/participants", method: "POST", idFlag: "work-id", body: true, summary: "Record nonlocking task participation with session_id and monotonic sequence; never changes task assignees, phase or completion."},
	"work participants list":     {path: "/work/{id}/participants", method: "GET", idFlag: "work-id", summary: "List task-scoped participation and bounded activity without private session details or unrelated task links.", filters: []string{"limit", "cursor"}},
	"work presence":              {path: "/agents/me/presence", method: "PATCH", body: true, summary: "Set the current derived agent's card and progress note."},
	"work list":                  {path: "/work", method: "GET", summary: "List work cards across sources in the authenticated workspace.", filters: []string{"project-ref", "source", "owner", "phase", "freshness", "q", "limit", "cursor"}},
	"work resolve":               {path: "/work/resolve", method: "GET", summary: "Resolve a path alias to a canonical card ref.", filters: []string{"path"}},
	"work get":                   {path: "/work/{id}", method: "GET", idFlag: "work-id", summary: "Read one work card, source authority, executions and current evidence."},
	"work group":                 {path: "/work/{id}/group", method: "POST", idFlag: "work-id", optionalBody: true, summary: "<card> --under <parent> [--after <step-id>] [--request-key key]: Deprecated: use anx work move <card> --under <parent>."},
	"work fold":                  {path: "/work/{id}/fold", method: "POST", idFlag: "work-id", optionalBody: true, summary: "<card> --into <parent> [--request-key key]: transfer references, carry the promise and archive."},
	"work unfold":                {path: "/work/{id}/unfold", method: "POST", idFlag: "work-id", optionalBody: true, summary: "<card> [--request-key key]: reverse a fold without overwriting conflicting edits."},
	"work ungrouped":             {path: "/work/ungrouped", method: "GET", summary: "Recent ungrouped cards and deterministic likely homes.", filters: []string{"limit"}},
	"work create":                {path: "/work", method: "POST", optionalBody: true, summary: "Create under a named parent, or --initiative for top level. Without either the card enters Unsorted."},
	"work patch":                 {path: "/work/{id}", method: "PATCH", idFlag: "work-id", body: true, summary: "Update work metadata with if_version; external status remains source-owned."},
	"work context":               {path: "/work/{id}", method: "GET", idFlag: "work-id", summary: "Compose work, a bounded observation page and refresh status using read-only requests.", filters: []string{"limit", "cursor"}},
	"work freshness":             {path: "/work/{id}", method: "GET", idFlag: "work-id", summary: "Inspect last observed, source activity and meaningful progress independently."},
	"work refresh get":           {path: "/work/{id}/refresh", method: "GET", idFlag: "work-id", summary: "Read refresh state without queueing work."},
	"work refresh request":       {path: "/work/{id}/refresh", method: "POST", idFlag: "work-id", summary: "Request a bounded refresh; queued is not a successful observation."},
	"work capabilities":          {path: "/work/capabilities", method: "GET", summary: "Read capabilities actually advertised by the authenticated central API."},
	"work observations list":     {path: "/work/{id}/observations", method: "GET", idFlag: "work-id", summary: "Read append-only evidence for a work card, preserving pagination and uncertainty.", filters: []string{"limit", "cursor"}},
	"work observations submit":   {path: "/work/{id}/observations", method: "POST", idFlag: "work-id", body: true, summary: "Submit an authenticated remote observation; preserve its idempotency key on retry."},
	"pm context":                 {path: "/pm/context", method: "GET", summary: "Read PM context. Inside a claimed turn use no flags; legacy filters apply only outside turns.", filters: []string{"work-ref", "query", "limit", "cursor"}},
	"pm conversations list":      {path: "/pm/conversations", method: "GET", summary: "List durable PM conversations with principal-bound pagination.", filters: []string{"limit", "cursor"}},
	"pm conversations create":    {path: "/pm/conversations", method: "POST", body: true, summary: "Create a durable conversation using request_key, title and optional work_ref."},
	"pm conversations get":       {path: "/pm/conversations/{id}", method: "GET", idFlag: "conversation-id", summary: "Read a conversation and its durable turns."},
	"pm conversations message":   {path: "/pm/conversations/{id}/messages", method: "POST", idFlag: "conversation-id", body: true, summary: "Queue a PM message using request_key and text; an accepted turn is not a completed outcome."},
	"pm decisions list":          {path: "/pm/decisions", method: "GET", summary: "List durable decisions with principal-bound pagination.", filters: []string{"limit", "cursor"}},
	"pm decisions get":           {path: "/pm/decisions/{id}", method: "GET", idFlag: "decision-id", summary: "Read an instruction, authorization scope, revision and answer status."},
	"pm decisions create":        {path: "/pm/decisions", method: "POST", body: true, summary: "Human proposes an instruction bound to work, scope and target_revision; never approves it. Agents use pm turns propose within a claimed PM turn."},
	"pm decisions answer":        {path: "/pm/decisions/{id}/answer", method: "POST", idFlag: "decision-id", body: true, summary: "Answer with revision, approve and text; the server requires an authorized human principal."},
	"pm decisions dispatch":      {path: "/pm/decisions/{id}/dispatch", method: "POST", idFlag: "decision-id", summary: "Explicitly dispatch authorized intent; inspect action receipt for actual outcome."},
	"pm actions list":            {path: "/pm/actions", method: "GET", summary: "Report durable action and receipt statuses with principal-bound pagination.", filters: []string{"limit", "cursor"}},
	"pm bindings list":           {path: "/pm/bindings", method: "GET", summary: "List channel identity bindings for this workspace; an operator check, never a send.", filters: []string{"limit", "cursor"}},
	"pm bindings create":         {path: "/pm/bindings", method: "POST", body: true, summary: "Bind an exact channel identity (transport, tenant, channel, user) to a workspace principal; humans only."},
	"pm actions get":             {path: "/pm/actions/{id}", method: "GET", idFlag: "action-id", summary: "Read authorization, attempts and receipt; source_reported is not verified."},
	"pm actions reconcile":       {path: "/pm/actions/{id}/reconcile", method: "POST", idFlag: "action-id", summary: "Request authoritative read-back of an action receipt; does not resend the action."},
	"pm actions acknowledge":     {path: "/pm/actions/{id}/acknowledge", method: "POST", idFlag: "action-id", summary: "Acknowledge a failed or unresolvable action."},
	"pm turns context":           {path: "/pm/turns/{id}/context", method: "POST", idFlag: "turn-id", summary: "Read context as the requesting actor; only the selected PM agent may call this.", filters: []string{"query", "limit", "cursor"}},
	"pm turns claim":             {path: "/pm/turns/claim", method: "POST", optionalBody: true, summary: "Claim the next queued turn with an exclusive runner lease. 204 means none."},
	"pm turns fail":              {path: "/pm/turns/{id}/fail", method: "POST", idFlag: "turn-id", body: true, summary: "Mark a claimed turn failed with a reason; does not complete work."},
	"pm turns propose":           {path: "/pm/turns/{id}/decisions", method: "POST", idFlag: "turn-id", body: true, summary: "Selected PM agent proposes an instruction for the requesting actor, never approval."},
	"pm turns complete":          {path: "/pm/turns/{id}/complete", method: "POST", idFlag: "turn-id", body: true, summary: "Selected PM agent records response text and evidence_refs; does not complete work."},
	"pm turns release":           {path: "/pm/turns/{id}/release", method: "POST", idFlag: "turn-id", body: true, summary: "Lease owner returns a claimed turn to the queue."},
	"pm turns heartbeat":         {path: "/pm/turns/{id}/heartbeat", method: "POST", idFlag: "turn-id", body: true, summary: "Lease owner renews a claimed turn's lease; renew at less than half the lease TTL."},
	"pm turns get":               {path: "/pm/turns/{id}", method: "GET", idFlag: "turn-id", summary: "Read a PM conversation turn."},
}

type parsedWorkCommand struct {
	spec                           workCommandSpec
	name, id, fromFile             string
	under, into, after, requestKey string
	handle                         string
	reason, toBoard                string
	topLevel, initiative           bool
	title                          string
	leaseToken                     string
	runnerID                       string
	query                          url.Values
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
// identity resolution, token grant, file reads or network activity.
func parseWorkCommand(args []string) (parsedWorkCommand, error) {
	if len(args) >= 2 && isLoopWork(strings.Join(args[:2], " ")) {
		name := strings.Join(args[:2], " ")
		id, _, err := parseLoopWork(name, args[2:])
		return parsedWorkCommand{name: name, id: id, spec: workCommands[name]}, err
	}
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
	var id, fromFile, leaseToken, runnerID, handle, under, into, after, requestKey trackedString
	var reason, toBoard, title trackedString
	var limit trackedInt
	var topLevel, initiative trackedBool

	values := map[string]*trackedString{}
	if spec.idFlag != "" {
		fs.Var(&id, spec.idFlag, "Public ref, handle or id")
	}
	if spec.body || spec.optionalBody {
		fs.Var(&fromFile, "from-file", "JSON request from path or - for stdin")
	}
	if out.name == "initiative retire" {
		fs.Var(&under, "under", "Group atomically under another initiative")
		fs.Var(&toBoard, "to-board", "Non-initiatives destination board")
	}
	if strings.HasPrefix(out.name, "initiative ") {
		fs.Var(&reason, "reason", "Required audit reason")
	}
	if out.name == "work create" || out.name == "work group" {
		fs.Var(&under, "under", "Stored parent card ref or path alias")
		fs.Var(&after, "after", "Preceding plan step id")
	}
	if out.name == "work fold" {
		fs.Var(&into, "into", "Parent card ref")
		fs.Var(&after, "after", "Preceding plan step id")
	}
	if out.name == "work create" || out.name == "work group" || out.name == "work fold" || out.name == "work unfold" || strings.HasPrefix(out.name, "initiative ") {
		fs.Var(&requestKey, "request-key", "Retain the original key and body until confirmed")
	}
	if out.name == "work create" {
		fs.Var(&topLevel, "top-level", "Deprecated and ignored; use --initiative")
		fs.Var(&initiative, "initiative", "Explicitly create a top-level initiative")
		fs.Var(&title, "title", "Card title (or use --from-file)")
		fs.Var(&handle, "handle", "Client-chosen lowercase card slug (1-64 characters)")
	}
	if workCommandUsesLeaseToken(out.name) {
		fs.Var(&leaseToken, "lease-token", "Active PM lease token")
	}
	if workCommandUsesRunnerID(out.name) {
		fs.Var(&runnerID, "runner-id", "Runner identity; defaults to the authenticated actor id")
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
	if out.name == "work list" {
		for _, arg := range tail {
			if arg == "--board" || strings.HasPrefix(arg, "--board=") {
				return out, errnorm.Usage("invalid_flags", "`anx work list` does not accept --board; use `anx boards cards list --board <board-ref>`")
			}
		}
	}
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
		if out.id == "." || out.id == ".." || strings.ContainsAny(out.id, "\\?#%") || strings.ContainsFunc(out.id, unicode.IsControl) || (strings.Contains(out.id, "/") && (!strings.HasPrefix(out.name, "work ") || !validWorkPath(out.id))) {
			return out, errnorm.Usage("invalid_request", "resource id must be a ref, handle or id, not a URL or path")
		}
	}
	if limit.set {
		maxLimit := 200
		if out.name == "work ungrouped" {
			maxLimit = 50
		}
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
	if handle.set {
		if err := validateCardHandle(handle.value); err != nil {
			return out, err
		}
		out.handle = handle.value
	}
	out.reason, out.toBoard = strings.TrimSpace(reason.value), strings.TrimSpace(toBoard.value)
	if strings.HasPrefix(out.name, "initiative ") && (out.reason == "" || len([]rune(out.reason)) > 2000) {
		return out, errnorm.Usage("invalid_request", "--reason (1..2000 characters) is required")
	}
	out.topLevel = topLevel.value
	out.initiative, out.title = initiative.value, strings.TrimSpace(title.value)
	if initiative.value && under.set {
		return out, errnorm.Usage("invalid_request", "--initiative and --under cannot be combined")
	}
	if out.name == "work create" && fromFile.value == "" && out.title == "" {
		return out, errnorm.Usage("invalid_request", "--title or --from-file is required")
	}
	if topLevel.value && under.set {
		return out, errnorm.Usage("invalid_request", "--top-level and --under cannot be combined")
	}
	out.under, out.into, out.after, out.requestKey = strings.TrimSpace(under.value), strings.TrimSpace(into.value), strings.TrimSpace(after.value), strings.TrimSpace(requestKey.value)
	if out.name == "work group" && out.under == "" && fromFile.value == "" {
		return out, errnorm.Usage("invalid_request", "--under <card> is required")
	}
	if out.name == "work fold" && out.into == "" && fromFile.value == "" {
		return out, errnorm.Usage("invalid_request", "--into <card> is required")
	}
	out.fromFile = strings.TrimSpace(fromFile.value)
	out.leaseToken = strings.TrimSpace(leaseToken.value)
	out.runnerID = strings.TrimSpace(runnerID.value)
	if spec.body && out.fromFile == "" {
		return out, errnorm.Usage("invalid_request", "--from-file <path|-> is required for anx "+out.name)
	}
	return out, nil
}

func (a *App) runWorkCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) >= 2 && isLoopWork(strings.Join(args[:2], " ")) {
		name := strings.Join(args[:2], " ")
		result, err := a.runLoopWork(ctx, name, args[2:], cfg)
		return result, name, err
	}
	if len(args) >= 2 && args[0] == "pm" {
		switch args[1] {
		case "propose":
			r, err := a.runPMPropose(ctx, args[2:], cfg)
			return r, "pm propose", err
		case "card":
			if err := validatePMCardArgs(args[2:]); err != nil {
				return nil, "pm card", err
			}
			r, err := a.runPMCardRead(ctx, args[2:], cfg)
			if err == nil {
				finishPMCardRead(r)
			}
			return r, "pm card", err
		case "context":
			if a.Getenv("ANX_PM_TURN_ID") != "" || a.Getenv("ANX_PM_LEASE_TOKEN") != "" {
				if err := validatePMContextArgs(args[2:]); err != nil {
					return nil, "pm context", err
				}
				r, err := a.runPMCardRead(ctx, nil, cfg)
				if err == nil {
					finishPMCardRead(r)
				}
				return r, "pm context", err
			}
		}
	}
	if len(args) >= 2 && args[0] == "pm" {
		switch args[1] {
		case "install", "status", "uninstall":
			result, err := a.runPMService(ctx, args[1], args[2:], cfg)
			return result, "pm " + args[1], err
		case "serve":
			result, err := a.runPMServe(ctx, args[2:], cfg)
			return result, "pm serve", err
		case "curate":
			result, err := a.runPMCurate(ctx, args[2:], cfg)
			return result, "pm curate", err
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
	if len(args) >= 2 && args[0] == "work" && args[1] == "presence" {
		return nil, "work presence", errnorm.Internal("not_implemented", "work presence awaits the derived-agent CLI implementation")
	}
	parsed, err := parseWorkCommand(args)
	if err != nil {
		return nil, parsed.name, err
	}
	var body any
	if parsed.spec.body || (parsed.spec.optionalBody && parsed.fromFile != "") {
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
		if parsed.handle != "" {
			object["handle"] = parsed.handle
		}
		body = object
	} else if parsed.spec.method == "POST" {
		body = map[string]any{}
	}
	if object, ok := body.(map[string]any); ok {
		if parsed.name == "work patch" {
			if _, legacy := asMap(object["patch"])["top_level"]; legacy {
				return nil, parsed.name, errnorm.Usage("invalid_request", "top_level is replaced by the work tree: use anx work move <ref> --root --reason <why> or --unsorted")
			}
		}
		if parsed.handle != "" {
			object["handle"] = parsed.handle
		}
		for key, value := range map[string]string{"under": parsed.under, "into": parsed.into, "after": parsed.after, "request_key": parsed.requestKey, "reason": parsed.reason, "to_board": parsed.toBoard} {
			if value != "" {
				object[key] = value
			}
		}
		if parsed.name == "work create" {
			object["create_surface"] = "work.create"
			if parsed.title != "" {
				object["title"] = parsed.title
			}
			if parsed.initiative {
				object["initiative"] = true
			}
			if object["initiative"] == true && anyString(object["under"]) != "" {
				return nil, parsed.name, errnorm.Usage("invalid_request", "initiative and under cannot be combined")
			}
		}
		switch parsed.name {
		case "pm turns claim":
			applyPMTurnClaimBody(parsed, cfg, object)
		case "pm turns context":
			applyPMTurnContextBody(a, parsed, object)
		case "pm turns propose":
			applyPMLeaseToken(a, parsed, object)
		}
	}
	if strings.HasPrefix(parsed.name, "work ") {
		if parsed.id != "" {
			parsed.id, err = a.resolveWorkPath(ctx, cfg, parsed.id)
			if err != nil {
				return nil, parsed.name, err
			}
		}
		if object, ok := body.(map[string]any); ok && anyString(object["under"]) != "" {
			object["under"], err = a.resolveWorkPath(ctx, cfg, anyString(object["under"]))
			if err != nil {
				return nil, parsed.name, err
			}
		}
	}
	method := parsed.spec.method
	path := strings.ReplaceAll(parsed.spec.path, "{id}", url.PathEscape(parsed.id))
	query := parsed.query
	if parsed.name == "work context" || parsed.name == "pm turns context" {
		query = nil
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	var started time.Time
	if parsed.name == "pm decisions dispatch" {
		started = time.Now().UTC()
	}
	// A work create without an explicit home always enters Unsorted.
	ctx = runcontext.WithTopLevel(ctx, parsed.topLevel || parsed.name == "work create")
	if object, ok := body.(map[string]any); ok && (parsed.name == "work create" || parsed.name == "work group" || parsed.name == "work fold" || parsed.name == "work unfold" || strings.HasPrefix(parsed.name, "initiative ")) {
		if err := ensureGroupingRequestKey(object); err != nil {
			return nil, parsed.name, err
		}
	}
	result, err := a.invokeRawJSON(ctx, cfg, parsed.name, method, path, body)
	if parsed.name == "pm turns claim" {
		if cap, ok := parsePMClaimCapacity(result, err); ok {
			return applyPMClaimCapacityResult(result, cap), parsed.name, nil
		}
		if err == nil {
			status, _ := asMap(result.Data)["status_code"].(int)
			if status == 204 {
				data := asMap(result.Data)
				data["body"] = map[string]any{"claimed": false, "reason": "nothing to claim"}
				result.Text = "No claimable turn"
				return result, parsed.name, nil
			}
		}
	}
	if err != nil {
		if parsed.id != "" && (parsed.name == "pm actions reconcile" || parsed.name == "pm actions acknowledge") {
			errnorm.AnnotateDetail(err, "action_id", parsed.id)
		}
		return result, parsed.name, err
	}
	if parsed.name == "work create" {
		result = addResourceURLToResult(cfg, "work.create", result)
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
	if parsed.name == "pm decisions dispatch" {
		result.Text = formatPMDispatchText(commandResultBody(result), started, "")
	} else {
		result.Text = formatWorkCommandText(parsed.name, commandResultBody(result))
	}
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

// isInternalWorkCommand reports registry entries that only back the top-level
// loop verb (anx context). They are not a user-facing `anx work` subcommand and
// must not appear in help or the generated runtime reference.
func isInternalWorkCommand(name string) bool {
	return name == "work context"
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
