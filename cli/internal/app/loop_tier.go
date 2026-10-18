package app

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/output"
)

// Loop tier: the handful of top-level verbs an agent uses every turn. These
// are the only commands the participant skill teaches; their shapes are
// stable (see loopTierStability). Everything else is setup (anx help <job>)
// or scripting (anx api <operation>).
var loopTierCommands = []string{"orient", "context", "note", "ask", "await", "done", "plan", "series push"}

func loopTierStability(path string) (string, bool) {
	path = strings.Join(strings.Fields(path), " ")
	for _, name := range loopTierCommands {
		if path == name {
			return "stable", true
		}
	}
	return "", false
}

// promiseHorizon is the write-time warning horizon for --next. It is a
// warning, never a state (contracts/anx-schema.yaml `promises`).
const promiseHorizon = 48 * time.Hour

const (
	maxNoteEvidenceRefs = 8
	maxAskBlocksRefs    = 20
	maxPromiseWhatRunes = 160
)

var promiseDurationPattern = regexp.MustCompile(`^([1-9][0-9]*)([smhd])$`)

// parsePromiseWhen mirrors the review_by grammar core resolves: a UTC date
// (YYYY-MM-DD), a zoned RFC3339 timestamp, or a positive s/m/h/d duration of
// at most 3650d from `from`.
func parsePromiseWhen(raw string, from time.Time) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	if m := promiseDurationPattern.FindStringSubmatch(raw); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return time.Time{}, false
		}
		unit := map[string]time.Duration{"s": time.Second, "m": time.Minute, "h": time.Hour, "d": 24 * time.Hour}[m[2]]
		d := time.Duration(n) * unit
		if d <= 0 || d > 3650*24*time.Hour || d/unit != time.Duration(n) {
			return time.Time{}, false
		}
		return from.Add(d).UTC(), true
	}
	if at, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return at.UTC(), true
	}
	if at, err := time.Parse("2006-01-02", raw); err == nil {
		// A calendar date is the whole day: "by Oct 15" is still open until
		// 23:59:59 UTC, matching core's plans.ParseWhen.
		return at.Add(24*time.Hour - time.Second).UTC(), true
	}
	return time.Time{}, false
}

// validateEvidenceRef accepts an http(s) URL or a typed ref.
func validateEvidenceRef(raw string) error {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return fmt.Errorf("evidence %q is not a valid http(s) URL", raw)
		}
		return nil
	}
	return validateTypedRef(raw)
}

// extractNextFlag pulls `--next <when> [<what>]` (or `--next=<when>`) out of
// args. The optional <what> is the token right after <when> when it is not a
// flag. The flag may appear once.
func extractNextFlag(args []string) (rest []string, when string, what string, set bool, err error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		name, inline, hasInline := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if !strings.HasPrefix(arg, "-") || name != "next" {
			rest = append(rest, arg)
			continue
		}
		if set {
			return nil, "", "", false, errnorm.Usage("invalid_flags", "--next may be given once")
		}
		set = true
		if hasInline {
			when = inline
		} else {
			if i+1 >= len(args) {
				return nil, "", "", false, errnorm.Usage("invalid_flags", "flag needs an argument: --next <when> [\"<what>\"]")
			}
			i++
			when = args[i]
		}
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			i++
			what = args[i]
		}
	}
	return rest, strings.TrimSpace(when), strings.TrimSpace(what), set, nil
}

func noteUsage() string {
	return `usage: anx note <card> ["<text>"] [--next <when> ["<what>"]] [--evidence <url|ref>]...`
}

func (a *App) runNote(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	parsed, err := parseNoteArgs(args, a.clockNow())
	if err != nil {
		return nil, err
	}
	card, err := a.fetchCardBody(ctx, cfg, parsed.cardRef)
	if err != nil {
		return nil, err
	}
	target, err := cardMessageTarget(card, parsed.cardRef)
	if err != nil {
		return nil, err
	}
	cardRef := firstNonEmpty(anyString(card["ref"]), parsed.cardRef)
	if !strings.HasPrefix(cardRef, "card:") {
		cardRef = "card:" + cardRef
	}
	var warnings []output.Warning
	if !parsed.nextSet {
		warnings = append(warnings, noteWarning("note_no_next", "no --next; a card renews by noting its next promise", []string{"anx", "note", cardRef, "--next", "<when>", "<what>"}))
		if prev, ok := a.previousOwnPromise(ctx, cfg, target.ThreadID); ok {
			argv := []string{"anx", "note", cardRef, "--next", prev.at.Format(time.RFC3339)}
			if prev.what != "" {
				argv = append(argv, prev.what)
			}
			warnings = append(warnings, noteWarning("note_clears_promise", fmt.Sprintf("this note clears your promise %q due %s; carry it forward with --next", prev.what, prev.at.Format(time.RFC3339)), argv))
		}
	} else {
		if len(stringList(card["assignee_refs"])) == 0 && anyString(card["assignee"]) == "" && anyString(card["owner"]) == "" {
			warnings = append(warnings, noteWarning("note_promise_unowned", "promise set on an unowned card; nobody will be prompted to renew it", []string{"anx", "work", "start", cardRef}))
		}
		if parsed.nextAt.Sub(a.clockNow()) > promiseHorizon {
			warnings = append(warnings, noteWarning("note_beyond_horizon", fmt.Sprintf("--next %s is beyond the 2-day horizon; promise a nearer checkpoint", parsed.nextWhen), []string{"anx", "note", cardRef, "--next", "2d", "<checkpoint>"}))
		}
		if len(parsed.evidence) == 0 {
			if prev, ok := a.previousOwnNext(ctx, cfg, target.ThreadID); ok && !prev.Equal(parsed.nextAt) {
				warnings = append(warnings, noteWarning("note_moved_without_evidence", "promise moved without evidence; only an --evidence ref resets the clock", []string{"anx", "note", cardRef, "--evidence", "<url|ref>", "--next", parsed.nextWhen}))
			}
		}
	}
	body, err := buildMessagePostedBody(cfg, "", target, parsed.text, "", nil, "")
	if err != nil {
		return nil, err
	}
	payload := asMap(asMap(body["event"])["payload"])
	if parsed.nextSet {
		next := map[string]any{"at": parsed.nextWhen}
		if parsed.nextWhat != "" {
			next["what"] = parsed.nextWhat
		}
		payload["next"] = next
	}
	if len(parsed.evidence) > 0 {
		payload["evidence_refs"] = parsed.evidence
	}
	if err := validateEventsCreateInput(body, "note"); err != nil {
		return nil, err
	}
	result, err := a.invokeTypedJSON(ctx, cfg, "note", "events.create", nil, nil, body)
	if err != nil {
		return nil, err
	}
	event := asMap(commandResultBody(result)["event"])
	// Presence is a convenience; the note is already written.
	_, _ = a.invokeRawJSON(ctx, cfg, "note presence", "PATCH", "/agents/me/presence", map[string]any{"current_card_ref": cardRef, "note": truncateRunes(parsed.text, 500)})
	data := map[string]any{"card_ref": cardRef, "note": parsed.text}
	if id := anyString(event["id"]); id != "" {
		data["note_ref"] = "event:" + id
	}
	if parsed.nextSet {
		data["next"] = map[string]any{"at": parsed.nextAt.Format(time.RFC3339), "what": parsed.nextWhat}
	}
	if len(parsed.evidence) > 0 {
		data["evidence_refs"] = parsed.evidence
	}
	return &commandResult{Data: data, Warnings: warnings}, nil
}

type noteArgs struct {
	cardRef, text      string
	nextSet            bool
	nextWhen, nextWhat string
	nextAt             time.Time
	evidence           []string
}

// parseNoteArgs is shared by preflight and execution so malformed notes fail
// before identity resolution or any request.
func parseNoteArgs(args []string, now time.Time) (noteArgs, error) {
	var out noteArgs
	rest, when, what, set, err := extractNextFlag(args)
	if err != nil {
		return out, err
	}
	leading, flagArgs := splitLeadingPositionals(rest)
	fs := newSilentFlagSet("note")
	var evidence trackedStrings
	fs.Var(&evidence, "evidence", "Evidence URL or typed ref (repeatable, at most 8)")
	if err := fs.Parse(flagArgs); err != nil {
		return out, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := append(leading, fs.Args()...)
	if len(positionals) == 0 {
		return out, errnorm.Usage("invalid_args", noteUsage())
	}
	if len(positionals) > 2 {
		return out, errnorm.Usage("invalid_args", noteUsage()+" (quote the note text)")
	}
	out.cardRef = strings.TrimSpace(positionals[0])
	if out.cardRef == "" {
		return out, errnorm.Usage("invalid_args", noteUsage())
	}
	if len(positionals) == 2 {
		out.text = strings.TrimSpace(positionals[1])
	}
	if set {
		at, ok := parsePromiseWhen(when, now)
		if !ok {
			return out, errnorm.Usage("invalid_next", fmt.Sprintf("--next %q must be a UTC date (YYYY-MM-DD), an RFC3339 timestamp, or a positive duration like 36h or 2d", when))
		}
		out.nextSet, out.nextWhen, out.nextAt = true, when, at
		if utf8.RuneCountInString(what) > maxPromiseWhatRunes {
			what = truncateRunes(what, maxPromiseWhatRunes)
		}
		out.nextWhat = what
	}
	if len(evidence.values) > maxNoteEvidenceRefs {
		return out, errnorm.Usage("invalid_request", fmt.Sprintf("at most %d --evidence refs", maxNoteEvidenceRefs))
	}
	for _, raw := range evidence.values {
		if err := validateEvidenceRef(raw); err != nil {
			return out, errnorm.Usage("invalid_request", "--evidence: "+err.Error())
		}
		out.evidence = append(out.evidence, strings.TrimSpace(raw))
	}
	if out.text == "" {
		switch {
		case out.nextSet && out.nextWhat != "":
			out.text = "Next by " + out.nextWhen + ": " + out.nextWhat
		case out.nextSet:
			out.text = "Next by " + out.nextWhen
		case len(out.evidence) > 0:
			out.text = "Evidence: " + strings.Join(out.evidence, ", ")
		default:
			return out, errnorm.Usage("invalid_request", "note needs text, --next or --evidence; "+noteUsage())
		}
	}
	return out, nil
}

func noteWarning(id, message string, argv []string) output.Warning {
	return output.Warning{Code: id, Message: message, Details: map[string]any{"suggest": argv}}
}

// ownPromise is the caller's latest note promise. A newer note without next
// clears it; the warning read never searches back through older promises.
type ownPromise struct {
	at   time.Time
	what string
}

func (a *App) previousOwnNext(ctx context.Context, cfg config.Resolved, threadID string) (time.Time, bool) {
	p, ok := a.previousOwnPromise(ctx, cfg, threadID)
	return p.at, ok
}

func (a *App) previousOwnPromise(ctx context.Context, cfg config.Resolved, threadID string) (ownPromise, bool) {
	actor := strings.TrimSpace(cfg.ActorID)
	if actor == "" || strings.TrimSpace(threadID) == "" {
		return ownPromise{}, false
	}
	path := "/events?thread_id=" + url.QueryEscape(threadID) + "&type=message_posted&limit=50"
	result, err := a.invokeRawJSON(ctx, cfg, "note previous", "GET", path, nil)
	if err != nil {
		return ownPromise{}, false
	}
	events := asSlice(commandResultBody(result)["events"])
	if len(events) == 0 {
		return ownPromise{}, false
	}
	// Bound candidates before filtering authors, matching core's 50-note fold.
	// An actor predicate could scan a busy thread looking for one old note.
	var ev map[string]any
	for _, raw := range events[:min(50, len(events))] {
		candidate := asMap(raw)
		if strings.TrimSpace(anyString(candidate["actor_id"])) == actor {
			ev = candidate
			break
		}
	}
	if ev == nil {
		return ownPromise{}, false
	}
	next := asMap(asMap(ev["payload"])["next"])
	ts, err := time.Parse(time.RFC3339Nano, anyString(ev["ts"]))
	if err != nil {
		return ownPromise{}, false
	}
	at, ok := parsePromiseWhen(anyString(next["at"]), ts)
	return ownPromise{at: at, what: anyString(next["what"])}, ok
}

func doneUsage() string {
	return "usage: anx done [<card>] --evidence <url|event:ref|artifact:ref> (repeatable)"
}

// runDone resolves a card with evidence and clears presence. Omitting the
// card uses the current card from presence.
func (a *App) runDone(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	cardRef, evidence, err := parseDoneArgs(args)
	if err != nil {
		return nil, err
	}
	if cardRef == "" {
		cardRef, err = a.currentCardRef(ctx, cfg)
		if err != nil {
			return nil, err
		}
	}
	card, err := a.fetchCardBody(ctx, cfg, cardRef)
	if err != nil {
		return nil, err
	}
	cardRef = firstNonEmpty(anyString(card["ref"]), cardRef)
	if !strings.HasPrefix(cardRef, "card:") {
		cardRef = "card:" + cardRef
	}
	resolveArgs := []string{"resolve", cardRef}
	var urls []string
	for _, e := range evidence {
		if strings.HasPrefix(e, "event:") || strings.HasPrefix(e, "artifact:") {
			resolveArgs = append(resolveArgs, "--resolution-ref", e)
		} else {
			urls = append(urls, e)
		}
	}
	if len(urls) > 0 {
		resolveArgs = append(resolveArgs, "--body", strings.Join(urls, "\n"))
	}
	if _, _, err := a.runCardsCommand(ctx, resolveArgs, cfg); err != nil {
		return nil, err
	}
	if _, err := a.invokeRawJSON(ctx, cfg, "done presence", "PATCH", "/agents/me/presence", map[string]any{"current_card_ref": nil, "note": nil}); err != nil {
		return nil, err
	}
	return &commandResult{Data: map[string]any{"card_ref": cardRef, "phase": "done", "evidence": evidence}}, nil
}

func parseDoneArgs(args []string) (string, []string, error) {
	leading, rest := splitLeadingPositionals(args)
	fs := newSilentFlagSet("done")
	var evidence trackedStrings
	fs.Var(&evidence, "evidence", "Evidence URL or event:/artifact: ref (repeatable)")
	if err := fs.Parse(rest); err != nil {
		return "", nil, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := append(leading, fs.Args()...)
	if len(positionals) > 1 {
		return "", nil, errnorm.Usage("invalid_args", doneUsage())
	}
	if len(evidence.values) == 0 {
		return "", nil, errnorm.Usage("invalid_request", "done requires --evidence <url|ref>; "+doneUsage())
	}
	var out []string
	for _, e := range evidence.values {
		e = strings.TrimSpace(e)
		if strings.HasPrefix(e, "event:") || strings.HasPrefix(e, "artifact:") {
			out = append(out, e)
			continue
		}
		u, parseErr := url.Parse(e)
		if parseErr != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return "", nil, errnorm.Usage("invalid_request", "--evidence must be an http(s) URL or event:/artifact: ref")
		}
		out = append(out, e)
	}
	card := ""
	if len(positionals) == 1 {
		card = strings.TrimSpace(positionals[0])
	}
	return card, out, nil
}

// runContext is "show me everything about this initiative": the card, a
// bounded observation page and refresh state, composed from read-only calls.
func (a *App) runContext(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	result, _, err := a.runWorkCommand(ctx, append([]string{"work", "context"}, args...), cfg)
	return result, err
}

// runOrient reads the caller's brief.v1 (GET /brief). JSON mode prints the
// object verbatim in result, with local workspace metadata beside it in the envelope.
func (a *App) runOrient(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	if err := parseOrientArgs(args); err != nil {
		return nil, err
	}
	result, err := a.invokeTypedJSON(ctx, cfg, "orient", "brief.get", nil, nil, nil)
	if err != nil {
		return nil, err
	}
	brief := commandResultBody(result)
	if brief == nil {
		return nil, errnorm.Local("invalid_response", "brief response is not an object")
	}
	// Resume-mode asks need a resident executor; say so when nothing on this
	// host will deliver their answers. One bounded agent read, best effort.
	var warnings []output.Warning
	var agent, detail map[string]any
	if ag, det, agentErr := a.dailyAgent(ctx, cfg); agentErr == nil {
		agent, detail = ag, det
		_, warnings = a.orientDeliveryHealth(cfg, asSlice(detail["open_asks"]))
	}
	bridgeActions := []output.NextAction{}
	bridgeRecommended := a.answerBridgeState(cfg) != answerBridgeRunning
	if bridgeRecommended {
		bridgeActions = append(bridgeActions, action("Recommended: install host bridge", "anx", "bridge", "install"))
		if agentNamePattern.MatchString(cfg.As) {
			bridgeActions = append(bridgeActions, action("Register this agent", "anx", "bridge", "add", "--as", cfg.As))
		}
	}
	workspace := a.orientWorkspace(cfg)
	if !cfg.JSON {
		markOrientAgentInitiatives(brief, agent, detail)
		text := fmt.Sprintf("workspace: %s source=%s other_aliases=%s\n", anyString(workspace["alias"]), anyString(workspace["source"]), strings.Join(stringList(workspace["other_aliases"]), ",")) + renderOrientText(brief, a.orientIdentityLine(cfg))
		if bridgeRecommended {
			text += "recommended next: anx bridge install; then register each intended agent/workspace with anx bridge add. Without answer delivery, unattended resumes wait; Git/drain remain manual. Await/batch work without a bridge.\n"
		}
		for _, w := range warnings {
			text += "warn: " + w.Message + "\n"
		}
		a.writeOutput(a.Stdout, text)
		return &commandResult{RawWritten: true}, nil
	}
	return &commandResult{Data: brief, Workspace: workspace, Warnings: warnings, NextActions: bridgeActions}, nil
}

func parseOrientArgs(args []string) error {
	fs := newSilentFlagSet("orient")
	if err := fs.Parse(args); err != nil {
		return errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) != 0 {
		return errnorm.Usage("invalid_args", "orient takes no positional arguments")
	}
	return nil
}

func (a *App) orientIdentityLine(cfg config.Resolved) string {
	name := strings.TrimSpace(cfg.Agent)
	if name == "" {
		name, _, _ = a.identityName(cfg)
	}
	host, workspace := "", ""
	if h, err := a.resolvedHost(cfg); err == nil {
		host, workspace = strings.TrimSpace(h.Slug), strings.TrimSpace(h.WorkspaceSlug)
	}
	parts := []string{}
	if name != "" {
		who := name
		if host != "" {
			who += " on " + host
		}
		parts = append(parts, who)
	}
	if workspace != "" {
		parts = append(parts, "workspace "+workspace)
	}
	return strings.Join(parts, " · ")
}

// renderOrientText renders brief.v1 as a few lines an agent can act on:
// who it is, what was answered for it, what waits on a human, its
// initiatives against their promises, and one next command.
func renderOrientText(brief map[string]any, identity string) string {
	var b strings.Builder
	principal := asMap(brief["principal"])
	if identity == "" {
		identity = firstNonEmpty(anyString(principal["display"]), anyString(principal["id"]))
	}
	fmt.Fprintf(&b, "you: %s\n", identity)
	answered := asSlice(brief["answered_for_you"])
	if len(answered) == 0 {
		b.WriteString("answered for you:  none\n")
	}
	for _, raw := range answered {
		item := asMap(raw)
		line := fmt.Sprintf("answered for you:  %s %q → %q", briefShortRef(item), anyString(item["title"]), anyString(item["answer"]))
		if item["followed_recommendation"] == false {
			line += " (not your recommendation)"
		}
		b.WriteString(line + "\n")
	}
	waiting := asSlice(brief["your_asks"])
	if len(waiting) == 0 {
		b.WriteString("waiting on human:  none\n")
	}
	for _, raw := range waiting {
		item := asMap(raw)
		line := fmt.Sprintf("waiting on human:  %s %q", briefShortRef(item), anyString(item["title"]))
		if age := briefSeconds(item["age_seconds"]); age > 0 {
			line += " " + compactDuration(age)
		}
		if by := anyString(item["decide_by"]); by != "" {
			line += " · by " + briefDay(by)
		}
		if over := briefSeconds(item["overdue_for_seconds"]); over > 0 {
			line += " · overdue " + compactDuration(over)
		}
		if n := briefSeconds(item["blocks_count"]); n > 0 {
			line += fmt.Sprintf(" · blocks %d", n)
		}
		b.WriteString(line + "\n")
	}
	initiatives := asSlice(brief["initiatives"])
	agentReader := strings.EqualFold(anyString(principal["kind"]), "agent")
	var yours, workspace []map[string]any
	for _, raw := range initiatives {
		item := asMap(raw)
		if !agentReader || orientInitiativeIsYours(item, principal) {
			yours = append(yours, item)
		} else {
			workspace = append(workspace, item)
		}
	}
	if len(yours) == 0 {
		b.WriteString("your initiatives:  none\n")
	}
	next := ""
	for _, item := range yours {
		ref := anyString(item["ref"])
		state := anyString(item["state"])
		b.WriteString(formatOrientInitiativeLine("your initiatives:", item) + "\n")
		if next == "" {
			switch state {
			case "past_promise":
				next = "anx note " + ref + " --evidence <ref> --next <date> \"<what>\""
			case "no_promise":
				next = "anx note " + ref + " --next <date> \"<what>\""
			}
		}
	}
	for _, item := range workspace {
		b.WriteString(formatOrientInitiativeLine("workspace:", item) + "\n")
	}
	if next == "" {
		for _, raw := range answered {
			if ref := briefShortRef(asMap(raw)); ref != "" {
				next = "anx inbox read " + ref
				break
			}
		}
	}
	if next == "" {
		next = "anx work list"
	}
	b.WriteString("next: " + next + "\n")
	return b.String()
}

func formatOrientInitiativeLine(heading string, item map[string]any) string {
	ref := anyString(item["ref"])
	line := fmt.Sprintf("%s  %s  %s", heading, strings.TrimPrefix(ref, "card:"), briefStateLabel(item))
	if moves := briefSeconds(item["promise_moves"]); moves > 0 {
		line += fmt.Sprintf(" · moved %d×", moves)
	}
	if asMap(item["possibly_moot"])["flagged"] == true {
		line += " · possibly moot"
	}
	return line
}

func stripOrientPrincipalPrefix(raw string) string {
	raw = strings.TrimSpace(raw)
	for _, prefix := range []string{"actor:", "agent:", "human:"} {
		if strings.HasPrefix(raw, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(raw, prefix))
		}
	}
	return raw
}

func orientMatchesPrincipal(raw any, principal map[string]any) bool {
	if raw == nil || principal == nil {
		return false
	}
	pID := strings.TrimSpace(anyString(principal["id"]))
	pBare := stripOrientPrincipalPrefix(pID)
	pDisplay := strings.TrimSpace(anyString(principal["display"]))
	matchID := func(s string) bool {
		s = strings.TrimSpace(s)
		if s == "" || pID == "" {
			return false
		}
		bare := stripOrientPrincipalPrefix(s)
		return s == pID || (pBare != "" && bare == pBare)
	}
	matchDisplay := func(s string) bool {
		s = strings.TrimSpace(s)
		return s != "" && pDisplay != "" && s == pDisplay
	}
	matchScalar := func(s string) bool {
		if pID != "" {
			return matchID(s)
		}
		return matchDisplay(s)
	}
	switch v := raw.(type) {
	case string:
		return matchScalar(v)
	case map[string]any:
		id := strings.TrimSpace(anyString(v["id"]))
		actorID := strings.TrimSpace(anyString(v["actor_id"]))
		agentID := strings.TrimSpace(anyString(v["agent_id"]))
		if id != "" || actorID != "" || agentID != "" {
			if pID != "" {
				return matchID(id) || matchID(actorID) || matchID(agentID)
			}
			return matchDisplay(id) || matchDisplay(actorID) || matchDisplay(agentID) || matchDisplay(anyString(v["display"]))
		}
		display := strings.TrimSpace(anyString(v["display"]))
		return matchID(display) || matchDisplay(display)
	case []any:
		for _, elem := range v {
			if orientMatchesPrincipal(elem, principal) {
				return true
			}
		}
	case []string:
		for _, elem := range v {
			if matchScalar(elem) {
				return true
			}
		}
	}
	return false
}

func orientInitiativeIsYours(item map[string]any, principal map[string]any) bool {
	if item == nil {
		return false
	}
	if yours, ok := item["yours"].(bool); ok && yours {
		return true
	}
	if promise := asMap(item["promise"]); promise != nil && orientMatchesPrincipal(promise["author"], principal) {
		return true
	}
	for _, field := range []string{"assignee", "owner", "assignee_refs", "participants", "participant_refs"} {
		if orientMatchesPrincipal(item[field], principal) {
			return true
		}
	}
	return false
}

func markOrientAgentInitiatives(brief, agent, detail map[string]any) {
	if brief == nil || (agent == nil && detail == nil) {
		return
	}
	ownedRefs := map[string]bool{}
	addRef := func(r string) {
		r = strings.TrimSpace(r)
		if r == "" {
			return
		}
		ownedRefs[r] = true
		if !strings.HasPrefix(r, "card:") {
			ownedRefs["card:"+r] = true
		}
	}
	addRef(anyString(agent["current_card_ref"]))
	for _, raw := range asSlice(detail["recent_cards"]) {
		c := asMap(raw)
		addRef(anyString(c["ref"]))
		addRef(anyString(c["id"]))
		addRef(anyString(c["handle"]))
	}
	if len(ownedRefs) == 0 {
		return
	}
	for _, raw := range asSlice(brief["initiatives"]) {
		item := asMap(raw)
		if item != nil && ownedRefs[anyString(item["ref"])] {
			item["yours"] = true
		}
	}
}

func briefShortRef(item map[string]any) string {
	ref := anyString(item["ref"])
	if strings.HasPrefix(ref, "inbox:") {
		id := strings.TrimPrefix(ref, "inbox:")
		if strings.HasPrefix(id, "ask-") {
			return "ask/" + strings.TrimPrefix(id, "ask-")
		}
		return id
	}
	return ref
}

func briefStateLabel(item map[string]any) string {
	promise := asMap(item["promise"])
	switch anyString(item["state"]) {
	case "past_promise":
		label := "PAST PROMISE"
		if s := briefSeconds(item["past_promise_for_seconds"]); s > 0 {
			label += " " + compactDuration(s)
		}
		if at := anyString(promise["at"]); at != "" {
			label += fmt.Sprintf(" (%s %s)", briefDay(at), firstNonEmpty(anyString(promise["what"]), "promise"))
		}
		return label
	case "moving":
		label := "moving"
		if at := anyString(promise["at"]); at != "" {
			label += fmt.Sprintf(" · next %s %s", briefDay(at), anyString(promise["what"]))
		}
		return strings.TrimSpace(label)
	case "no_promise":
		label := "no promise"
		if at := anyString(item["last_evidence_at"]); at != "" {
			label += " · last evidence " + briefDay(at)
		}
		return label
	case "done":
		return "done"
	default:
		return firstNonEmpty(anyString(item["state"]), "unknown")
	}
}

func briefSeconds(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	}
	return 0
}

func compactDuration(seconds int64) string {
	switch {
	case seconds >= 86400:
		return fmt.Sprintf("%dd", seconds/86400)
	case seconds >= 3600:
		return fmt.Sprintf("%dh", seconds/3600)
	default:
		return fmt.Sprintf("%dm", max(seconds/60, 1))
	}
}

func briefDay(raw string) string {
	at, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return raw
	}
	return at.UTC().Format("Jan 2")
}

// askDecision carries the optional ask fields core uses to order decisions:
// by (the requester's deadline for the human), blocks and priority.
type askDecision struct {
	by       string
	blocks   []string
	priority string
}

func (d askDecision) empty() bool { return d.by == "" && len(d.blocks) == 0 }

func (d askDecision) apply(payload map[string]any) {
	if d.by != "" {
		payload["by"] = d.by
	}
	if len(d.blocks) > 0 {
		payload["blocks"] = d.blocks
	}
	if d.priority != "" {
		payload["priority"] = d.priority
	}
}

// parseAskDecisionFlags validates only malformed values; a missing value is
// never an error.
func parseAskDecisionFlags(by trackedString, blocks []string, priority trackedString, now time.Time) (askDecision, error) {
	var out askDecision
	if by.set {
		raw := strings.TrimSpace(by.value)
		if _, ok := parsePromiseWhen(raw, now); !ok {
			return out, errnorm.Usage("invalid_by", fmt.Sprintf("--by %q must be a UTC date (YYYY-MM-DD), an RFC3339 timestamp, or a positive duration like 36h or 2d", by.value))
		}
		out.by = raw
	}
	if len(blocks) > maxAskBlocksRefs {
		return out, errnorm.Usage("invalid_request", fmt.Sprintf("at most %d --blocks refs", maxAskBlocksRefs))
	}
	for _, raw := range blocks {
		ref := strings.TrimSpace(raw)
		if err := validateTypedRef(ref); err != nil || strings.HasPrefix(ref, "http") {
			return out, errnorm.Usage("invalid_request", fmt.Sprintf("--blocks %q must be a typed ref such as card:<slug>", raw))
		}
		out.blocks = append(out.blocks, ref)
	}
	out.blocks = uniqueStringsInOrder(out.blocks)
	if priority.set {
		p := strings.ToLower(strings.TrimSpace(priority.value))
		switch p {
		case "p0", "p1", "p2", "p3", "critical", "high", "medium", "low":
			out.priority = p
		default:
			return out, errnorm.Usage("invalid_request", "--priority must be p0..p3 or critical/high/medium/low")
		}
	}
	return out, nil
}

func askWarning(id, message string, argv []string) output.Warning {
	w := output.Warning{Code: id, Message: message}
	if len(argv) > 0 {
		w.Details = map[string]any{"suggest": argv}
	}
	return w
}
