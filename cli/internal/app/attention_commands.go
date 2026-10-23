package app

import (
	"context"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

const attentionHelp = `Attention records

Usage: anx attention place <subject-ref> --for <principal-ref> --to decide|watch|digest|routine --reason <text> [--rank N] [--request-key KEY]
       anx attention items [--for me] [--surface all] [--since 24h] [--scope-ref <doc>] [--cursor <cursor>]
       anx attention agent get
       anx attention agent set <principal-ref|none>
       anx attention feedback list [--since 24h] [--for <principal-ref>] [--cursor <cursor>]
       anx attention changes --scope-ref <doc|builtin:initiatives>

Choose a surface and explain why in at most 200 characters. Open human asks
always reach Decide. The human-designated attention agent wins, otherwise the
newest placement wins. Principal refs are actor:<actor-id>, human:<principal-id>
or agent:<principal-id>. Read feedback to improve later placements; ANX runs no
models. Reads never move human visit markers. Follow next_cursor to exhaustion,
even on empty pages. Rank orders items within each placement page.
`

type attentionCommand struct {
	name, id string
	body     map[string]any
	query    []queryParam
}

func parseAttentionCommand(args []string) (attentionCommand, error) {
	p := attentionCommand{name: "attention"}
	usage := func(message string) (attentionCommand, error) {
		return p, errnorm.Usage("invalid_args", message+"; run anx help attention")
	}
	if len(args) == 0 {
		return usage("attention subcommand required")
	}
	fs := newSilentFlagSet("attention")
	var positional string
	switch args[0] {
	case "place":
		p.name = "attention place"
		p.id = "attention.place"
		if len(args) < 2 || strings.HasPrefix(args[1], "--") {
			return usage("subject ref required")
		}
		positional = args[1]
		args = args[2:]
	case "agent":
		if len(args) < 2 {
			return usage("agent get or set required")
		}
		p.name = "attention agent " + args[1]
		p.id = "attention.agent." + args[1]
		if args[1] == "get" {
			if len(args) != 2 {
				return usage("agent get takes no arguments")
			}
			return p, nil
		}
		if args[1] != "set" || len(args) != 3 || strings.HasPrefix(args[2], "--") {
			return usage("usage: anx attention agent set <principal-ref|none>")
		}
		var ref any = args[2]
		if args[2] == "none" {
			ref = nil
		}
		p.body = map[string]any{"attention_agent": ref}
		return p, nil
	case "feedback":
		if len(args) < 2 || args[1] != "list" {
			return usage("usage: anx attention feedback list")
		}
		p.name = "attention feedback list"
		p.id = "attention.feedback.list"
		args = args[2:]
	case "items":
		p.name = "attention items"
		p.id = "attention.items"
		args = args[1:]
	case "changes":
		p.name = "attention changes"
		p.id = "attention.changes"
		args = args[1:]
	default:
		return p, errnorm.Usage("unknown_subcommand", "unknown attention subcommand "+args[0])
	}
	var recipient, surface, reason, rank, since, scope, cursor, key string
	if p.id == "attention.place" {
		fs.StringVar(&recipient, "for", "", "Recipient principal ref")
		fs.StringVar(&surface, "to", "", "Attention surface")
		fs.StringVar(&reason, "reason", "", "Placement reason")
		fs.StringVar(&rank, "rank", "0", "Rank within page")
		fs.StringVar(&key, "request-key", "", "Retry identity")
	}
	if p.id == "attention.items" || p.id == "attention.feedback.list" {
		fs.StringVar(&recipient, "for", "", "Recipient principal ref")
		fs.StringVar(&since, "since", "", "RFC3339 or duration")
		fs.StringVar(&cursor, "cursor", "", "Continuation")
	}
	if p.id == "attention.items" {
		fs.StringVar(&surface, "surface", "all", "Attention surface")
	}
	if p.id == "attention.items" || p.id == "attention.changes" {
		fs.StringVar(&scope, "scope-ref", "", "Dashboard document ref or builtin:initiatives")
	}
	if err := fs.Parse(args); err != nil {
		return p, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) != 0 {
		return usage("unexpected positional arguments")
	}
	validSurface := func(s string) bool { return s == "decide" || s == "watch" || s == "digest" || s == "routine" }
	if p.id == "attention.place" {
		if recipient == "" || !validSurface(surface) || strings.TrimSpace(reason) == "" || utf8.RuneCountInString(reason) > 200 {
			return usage("--for, --to and a reason of 1 to 200 characters required")
		}
		n, err := strconv.ParseInt(rank, 10, 32)
		if err != nil {
			return usage("rank must be an integer")
		}
		p.body = map[string]any{"subject_ref": positional, "for": recipient, "to": surface, "reason": reason, "rank": n}
		if key != "" {
			p.body["request_key"] = key
		}
		return p, nil
	}
	if p.id == "attention.items" && surface != "all" && !validSurface(surface) {
		return usage("unknown attention surface")
	}
	if since != "" {
		at, err := time.Parse(time.RFC3339Nano, since)
		if err != nil {
			d, e := time.ParseDuration(since)
			if e != nil || d <= 0 {
				return usage("since must be RFC3339 or a positive duration")
			}
			at = time.Now().UTC().Add(-d)
		}
		if err == nil {
			since = at.UTC().Format(time.RFC3339Nano)
		}
	}
	if p.id == "attention.changes" && scope == "" {
		return usage("--scope-ref required")
	}
	for _, q := range []struct{ name, value string }{{"for", recipient}, {"surface", surface}, {"since", since}, {"scope_ref", scope}, {"cursor", cursor}} {
		if q.value != "" {
			p.query = append(p.query, queryParam{name: q.name, values: []string{q.value}})
		}
	}
	return p, nil
}
func (a *App) runAttentionCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 || isHelpToken(args[0]) {
		return &commandResult{Text: attentionHelp}, "attention", nil
	}
	p, err := parseAttentionCommand(args)
	if err != nil {
		return nil, p.name, err
	}
	result, err := a.invokeTypedJSON(ctx, cfg, p.name, p.id, nil, p.query, p.body)
	return result, p.name, err
}
