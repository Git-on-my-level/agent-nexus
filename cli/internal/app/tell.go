package app

import (
	"context"
	"strings"
	"unicode/utf8"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/output"
)

// anx tell is how an agent reaches a human with something that wants no
// answer: a digest, a finding, a heads-up. It is stored as an fyi on a card
// and shown on Watch. An ask is for a choice only the human can make; before
// tell existed, every message to a human had to be shaped as one.
const (
	tellHeadlineMaxRunes = 160
	tellBodyMaxRunes     = 4000
)

func tellUsage() string {
	return `usage: anx tell <card> "<headline>" [--body "<text>"] [--evidence <url|ref>]...`
}

type tellArgs struct {
	cardRef, headline, body string
	evidence                []string
}

// parseTellArgs is shared by preflight and execution so a malformed tell
// fails before identity resolution or any request.
func parseTellArgs(args []string) (tellArgs, error) {
	var out tellArgs
	leading, flagArgs := splitLeadingPositionals(args)
	fs := newSilentFlagSet("tell")
	var body trackedString
	var evidence trackedStrings
	fs.Var(&body, "body", "Supporting detail in plain words, at most 4000 characters")
	fs.Var(&evidence, "evidence", "Evidence URL or typed ref (repeatable, at most 8)")
	if err := fs.Parse(flagArgs); err != nil {
		return out, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := append(leading, fs.Args()...)
	if len(positionals) != 2 || strings.TrimSpace(positionals[0]) == "" || strings.TrimSpace(positionals[1]) == "" {
		return out, errnorm.Usage("invalid_args", tellUsage()+" (quote the headline)")
	}
	out.cardRef, out.headline, out.body = strings.TrimSpace(positionals[0]), strings.TrimSpace(positionals[1]), strings.TrimSpace(body.value)
	if strings.ContainsAny(out.headline, "\r\n") || utf8.RuneCountInString(out.headline) > tellHeadlineMaxRunes {
		return out, errnorm.Usage("invalid_request", "the headline is one line of at most 160 characters; put detail in --body")
	}
	if utf8.RuneCountInString(out.body) > tellBodyMaxRunes {
		return out, errnorm.Usage("invalid_request", "--body is limited to 4000 characters; link longer material with --evidence")
	}
	if len(evidence.values) > maxNoteEvidenceRefs {
		return out, errnorm.Usage("invalid_request", "at most 8 --evidence refs")
	}
	for _, raw := range evidence.values {
		if err := validateEvidenceRef(raw); err != nil {
			return out, errnorm.Usage("invalid_request", "--evidence: "+err.Error())
		}
		out.evidence = append(out.evidence, strings.TrimSpace(raw))
	}
	return out, nil
}

func (a *App) runTell(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	parsed, err := parseTellArgs(args)
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
	text := firstNonEmpty(parsed.body, parsed.headline)
	body, err := buildMessagePostedBody(cfg, "", target, text, "", nil, "")
	if err != nil {
		return nil, err
	}
	payload := asMap(asMap(body["event"])["payload"])
	payload["kind"], payload["title"] = "fyi", parsed.headline
	if len(parsed.evidence) > 0 {
		payload["evidence_refs"] = parsed.evidence
	}
	if err := validateEventsCreateInput(body, "tell"); err != nil {
		return nil, err
	}
	// The pending-note store gives tell the same retry safety as note: a
	// rerun after a timeout resends the same request key and never posts twice.
	result, sent, err := a.postNote(ctx, cfg, noteArgs{cardRef: parsed.cardRef, text: "tell\x00" + parsed.headline + "\x00" + parsed.body, evidence: parsed.evidence}, body)
	if err != nil {
		return nil, err
	}
	data := map[string]any{"card_ref": cardRef, "headline": parsed.headline, "shown": "Watch"}
	if id := anyString(asMap(commandResultBody(result)["event"])["id"]); id != "" {
		data["tell_ref"] = "event:" + id
	}
	var warnings []output.Warning
	warnings = append(warnings, sent.annotate(data)...)
	return &commandResult{Data: data, Warnings: warnings}, nil
}
