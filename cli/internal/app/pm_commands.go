package app

import (
	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

type pmProposalOptions struct {
	ref, status, note, why string
	evidence               trackedStrings
}

func parsePMProposal(args []string) (pmProposalOptions, error) {
	o := pmProposalOptions{}
	repair := "usage: anx pm propose <card-ref> (--status <backlog|ready|in_progress|blocked|review|done> | --note <text>) --why <reason> [--evidence <ref> ...]"
	if len(args) == 0 || !strings.HasPrefix(args[0], "card:") || !pmActivityRef.MatchString(args[0]) {
		return o, errnorm.Usage("invalid_request", repair)
	}
	o.ref = args[0]
	fs := newSilentFlagSet("pm propose")
	fs.StringVar(&o.status, "status", "", "Proposed status")
	fs.StringVar(&o.note, "note", "", "Proposed note")
	fs.StringVar(&o.why, "why", "", "Reason")
	fs.Var(&o.evidence, "evidence", "Evidence ref (repeatable)")
	if err := fs.Parse(args[1:]); err != nil {
		return o, errnorm.Usage("invalid_flags", err.Error()+"; "+repair)
	}
	if len(fs.Args()) > 0 || (o.status == "") == (strings.TrimSpace(o.note) == "") || strings.TrimSpace(o.why) == "" {
		return o, errnorm.Usage("invalid_request", repair)
	}
	if o.status != "" && !strings.Contains("|backlog|ready|in_progress|blocked|review|done|", "|"+o.status+"|") {
		return o, errnorm.Usage("invalid_request", repair)
	}
	for _, ref := range o.evidence.values {
		if !pmActivityRef.MatchString(ref) {
			return o, errnorm.Usage("invalid_request", "Evidence must be a typed ref; "+repair)
		}
	}
	hasCompletionEvidence := false
	for _, ref := range o.evidence.values {
		if strings.HasPrefix(ref, "event:") || strings.HasPrefix(ref, "artifact:") {
			hasCompletionEvidence = true
		}
	}
	if len(o.evidence.values) > 32 {
		return o, errnorm.Usage("invalid_request", "Use at most 32 evidence refs; "+repair)
	}
	if o.status == "done" && !hasCompletionEvidence {
		return o, errnorm.Usage("invalid_request", fmt.Sprintf("Completion requires evidence: anx pm propose %s --status done --why %q --evidence <artifact-or-event-ref>", o.ref, o.why))
	}
	return o, nil
}
func (a *App) pmTurnEnvironment() (string, string, error) {
	id, token := a.Getenv("ANX_PM_TURN_ID"), a.Getenv("ANX_PM_LEASE_TOKEN")
	if id == "" || token == "" {
		return "", "", errnorm.Usage("needs_turn", "This command runs inside a claimed PM turn; ask through Inbox or `anx pm ask`.")
	}
	return id, token, nil
}
func (a *App) runPMCardRead(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	id, token, err := a.pmTurnEnvironment()
	if err != nil {
		return nil, err
	}
	view, name := "cards", "pm context"
	body := map[string]any{"lease_token": token, "view": view, "limit": 8}
	if len(args) > 0 {
		name = "pm card"
		body["view"] = "card"
		body["context_ref"] = args[0]
	}
	r, err := a.invokeRawJSON(ctx, cfg, name, "POST", "/pm/turns/"+url.PathEscape(id)+"/context", body)
	if err != nil {
		return nil, pmCommandError(err)
	}
	if commandResultBody(r) == nil {
		return nil, errnorm.Internal("invalid_response", "PM card context is unavailable")
	}
	return r, nil
}
func (a *App) runPMPropose(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	o, err := parsePMProposal(args)
	if err != nil {
		return nil, err
	}
	id, token, err := a.pmTurnEnvironment()
	if err != nil {
		return nil, err
	}
	read, err := a.runPMCardRead(ctx, []string{o.ref}, cfg)
	if err != nil {
		return nil, err
	}
	page := asMap(commandResultBody(read))
	items, _ := page["items"].([]any)
	if len(items) != 1 {
		return nil, errnorm.Usage("card_unavailable", "Card is unavailable in this conversation; read `anx pm context` and choose a pinned card.")
	}
	card := asMap(items[0])
	revision := anyString(card["decision_revision"])
	ref := anyString(card["ref"])
	if revision == "" || ref == "" {
		return nil, errnorm.Internal("invalid_response", "Card revision is unavailable; run anx pm card "+o.ref)
	}
	payload := map[string]any{}
	if o.status != "done" && len(o.evidence.values) > 0 {
		payload["evidence_refs"] = o.evidence.values
	}
	scope := "work.phase"
	if o.status != "" {
		payload["phase"] = o.status
		if o.status == "done" {
			payload["resolution_refs"] = o.evidence.values
		}
	} else {
		scope = "work.note"
		payload["note"] = o.note
	}
	key := make([]byte, 16)
	if _, err = rand.Read(key); err != nil {
		return nil, err
	}
	r, err := a.invokeRawJSON(ctx, cfg, "pm propose", "POST", "/pm/turns/"+url.PathEscape(id)+"/decisions", map[string]any{"lease_token": token, "request_key": hex.EncodeToString(key), "work_ref": ref, "scope": scope, "target_revision": revision, "instruction": o.why, "payload": payload})
	if err != nil {
		return r, pmCommandError(err)
	}
	r.Text = fmt.Sprintf("Proposed for %s; awaiting your decision in Inbox.\n", ref)
	asMap(r.Data)["body"] = pmFacingValue(commandResultBody(r))
	return r, nil
}

// PM output vocabulary is a projection; API field names remain compatible.
func pmFacingValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, val := range x {
			if k == "freshness" || k == "refresh" || k == "decision_revision" || k == "target_revision" {
				continue
			}
			key := strings.TrimPrefix(k, "work_")
			if key != k {
				key = "card_" + key
			}
			switch k {
			case "work_summary":
				key = "card_summary"
			case "work_ref":
				key = "card_ref"
			case "phase":
				key = "status"
			}
			if k == "scope" {
				switch val {
				case "work.phase":
					val = "status"
				case "work.note":
					val = "note"
				case "work.annotate":
					val = "annotation"
				}
			}
			out[key] = pmFacingValue(val)
		}
		return out
	case []any:
		out := []any{}
		for _, item := range x {
			out = append(out, pmFacingValue(item))
		}
		return out
	default:
		return v
	}
}
func finishPMCardRead(r *commandResult) {
	body := pmFacingValue(commandResultBody(r))
	asMap(r.Data)["body"] = body
	var b strings.Builder
	page := asMap(body)
	items, _ := page["items"].([]any)
	for _, item := range items {
		card := asMap(item)
		fmt.Fprintf(&b, "card %s %s status=%s\n", anyString(card["ref"]), anyString(card["title"]), anyString(card["status"]))
		keys := []string{}
		for key := range card {
			if key != "ref" && key != "title" && key != "status" && key != "activity_partial" && key != "asks_partial" && key != "decisions_partial" {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			if value, ok := card[key]; ok && value != nil {
				raw, _ := json.Marshal(value)
				if string(raw) != "[]" && string(raw) != "{}" {
					fmt.Fprintf(&b, "  %s: %s\n", key, raw)
				}
			}
		}
		if card["activity_partial"] == true {
			b.WriteString("  Recent activity is a bounded preview.\n")
		}
		if card["asks_partial"] == true || card["decisions_partial"] == true {
			b.WriteString("  More open asks or decisions may exist.\n")
		}
	}
	if limits, ok := page["limitations"].([]any); ok {
		for _, limit := range limits {
			fmt.Fprintf(&b, "context: %v\n", limit)
		}
	}
	r.Text = b.String()
}

func validatePMCardArgs(args []string) error {
	if len(args) != 1 || !strings.HasPrefix(args[0], "card:") || !pmActivityRef.MatchString(args[0]) {
		return errnorm.Usage("invalid_request", "usage: anx pm card <card-ref>; choose a card from anx pm context")
	}
	return nil
}

var pmPlumbingWord = regexp.MustCompile(`\b(work_ref|work|task|commitment)\b`)

func pmCommandError(err error) error {
	var e *errnorm.Error
	if errors.As(err, &e) {
		e.Message = pmPlumbingWord.ReplaceAllString(e.Message, "card")
		e.Hint = "Read pinned cards with `anx pm context`; inspect one with `anx pm card <card-ref>`."
		e.Details = pmFacingValue(e.Details)
	}
	return err
}
