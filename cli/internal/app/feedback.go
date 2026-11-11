package app

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/httpclient"
)

// anx feedback is the documented path for an agent to report a problem with
// ANX itself. Findings used to reach the maintainer only through chat.
//
// The note lands on one well-known card per workspace, card:anx-feedback
// ("ANX product feedback"), created in Unsorted on first use, so the people
// who run the workspace see it in Watch and anx tree. The result also carries
// a prefilled issue link for the public repository, for feedback the
// upstream maintainer should see.

const (
	feedbackCardHandle = "anx-feedback"
	feedbackCardRef    = "card:" + feedbackCardHandle
	feedbackIssueBase  = "https://github.com/Git-on-my-level/agent-nexus/issues/new"
	feedbackMaxRunes   = 4000
)

func feedbackUsage() string {
	return `usage: anx feedback "<what went wrong or what you need>" [--evidence <url|ref>]... [--dry-run]`
}

type feedbackArgs struct {
	text     string
	evidence []string
	dryRun   bool
}

func parseFeedbackArgs(args []string) (feedbackArgs, error) {
	var out feedbackArgs
	leading, flagArgs := splitLeadingPositionals(args)
	fs := newSilentFlagSet("feedback")
	var evidence trackedStrings
	var dryRun trackedBool
	fs.Var(&evidence, "evidence", "Evidence URL or typed ref (repeatable, at most 8)")
	fs.Var(&dryRun, "dry-run", "Show what would be filed without writing")
	if err := fs.Parse(flagArgs); err != nil {
		return out, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := append(leading, fs.Args()...)
	if len(positionals) != 1 || strings.TrimSpace(positionals[0]) == "" {
		return out, errnorm.Usage("invalid_args", feedbackUsage()+" (quote the feedback text)")
	}
	out.text = strings.TrimSpace(positionals[0])
	if len([]rune(out.text)) > feedbackMaxRunes {
		return out, errnorm.Usage("invalid_request", "feedback is limited to 4000 characters; link longer material with --evidence")
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
	out.dryRun = dryRun.value
	return out, nil
}

func feedbackIssueURL(text, version string) string {
	title := text
	if r := []rune(title); len(r) > 80 {
		title = string(r[:77]) + "..."
	}
	q := url.Values{}
	q.Set("title", "Feedback: "+title)
	q.Set("body", text+"\n\nFiled with anx feedback (CLI "+version+").")
	q.Set("labels", "feedback")
	return feedbackIssueBase + "?" + q.Encode()
}

func (a *App) runFeedback(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	parsed, err := parseFeedbackArgs(args)
	if err != nil {
		return nil, err
	}
	text := "ANX feedback: " + parsed.text
	data := map[string]any{"card_ref": feedbackCardRef, "feedback": parsed.text, "cli_version": httpclient.CLIVersion, "upstream_issue_url": feedbackIssueURL(parsed.text, httpclient.CLIVersion)}
	if len(parsed.evidence) > 0 {
		data["evidence_refs"] = parsed.evidence
	}
	if parsed.dryRun {
		data["dry_run"] = true
		return &commandResult{Data: data}, nil
	}
	card, created, err := a.feedbackCard(ctx, cfg)
	if err != nil {
		return nil, err
	}
	data["card_created"] = created
	target, err := cardMessageTarget(card, feedbackCardRef)
	if err != nil {
		return nil, err
	}
	body, err := buildMessagePostedBody(cfg, "", target, text, "", nil, "")
	if err != nil {
		return nil, err
	}
	payload := asMap(asMap(body["event"])["payload"])
	payload["feedback"] = map[string]any{"cli_version": httpclient.CLIVersion}
	if len(parsed.evidence) > 0 {
		payload["evidence_refs"] = parsed.evidence
	}
	if err := validateEventsCreateInput(body, "note"); err != nil {
		return nil, err
	}
	// Same idempotent, bounded write as anx note: a rerun never files twice.
	result, _, err := a.postNote(ctx, cfg, noteArgs{cardRef: feedbackCardRef, text: text, evidence: parsed.evidence}, body)
	if err != nil {
		return nil, err
	}
	if id := anyString(asMap(commandResultBody(result)["event"])["id"]); id != "" {
		data["note_ref"] = "event:" + id
	}
	return &commandResult{Data: data}, nil
}

// feedbackCard reads card:anx-feedback, creating it in Unsorted once. Two
// writers racing to create it settle on the one that won the handle.
func (a *App) feedbackCard(ctx context.Context, cfg config.Resolved) (map[string]any, bool, error) {
	card, err := a.fetchCardBody(ctx, cfg, feedbackCardRef)
	if err == nil {
		return card, false, nil
	}
	if !feedbackMissing(err) {
		return nil, false, err
	}
	body := map[string]any{
		"title":          "ANX product feedback",
		"handle":         feedbackCardHandle,
		"summary":        "Problems and requests about ANX itself, filed by agents with anx feedback. Read, then file each actionable item with the ANX maintainers.",
		"create_surface": "work.create",
		"request_key":    "anx-feedback-card:" + time.Now().UTC().Format("2006-01-02"),
	}
	if _, createErr := a.invokeTypedJSON(ctx, cfg, "work create", "work.create", nil, nil, body); createErr != nil {
		var e *errnorm.Error
		if !errors.As(createErr, &e) || e.Code != "conflict" && !strings.Contains(e.Code, "handle") {
			return nil, false, createErr
		}
	}
	card, err = a.fetchCardBody(ctx, cfg, feedbackCardRef)
	return card, err == nil, err
}

func feedbackMissing(err error) bool {
	var e *errnorm.Error
	return errors.As(err, &e) && (e.Code == "not_found" || strings.HasSuffix(e.Code, "_not_found"))
}
