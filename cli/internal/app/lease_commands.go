package app

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

// leaseCommand is the parsed form of `anx lease show|acquire|renew|release`.
// One strict parser serves the usage preflight and the runtime.
type leaseCommand struct {
	verb, card, owner, reason string
	handoffTo                 string
	ttlSeconds                int64
	ifEpoch                   *int64
}

var (
	leaseTTLPattern   = regexp.MustCompile(`^([1-9][0-9]*)([smhd])$`)
	leaseOwnerPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@/+-]{0,127}$`)
)

const (
	leaseMinTTL = 60
	leaseMaxTTL = 7 * 24 * 60 * 60
)

func leaseUsage(verb string) string {
	switch verb {
	case "show":
		return "usage: anx lease show <card>"
	case "acquire":
		return "usage: anx lease acquire <card> --owner <id> --ttl <dur> [--reason <text>] [--if-epoch <n>]"
	case "renew":
		return "usage: anx lease renew <card> --owner <id> [--ttl <dur>]"
	case "release":
		return "usage: anx lease release <card> --owner <id> [--reason <text>] [--handoff-to <id> [--ttl <dur>]]"
	}
	return "usage: anx lease show|acquire|renew|release <card> ..."
}

// parseLeaseTTL accepts a positive s/m/h/d duration between 60s and 7d.
func parseLeaseTTL(raw string) (int64, bool) {
	m := leaseTTLPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		return 0, false
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || n > leaseMaxTTL {
		return 0, false
	}
	unit := map[string]int64{"s": 1, "m": 60, "h": 3600, "d": 86400}[m[2]]
	seconds := n * unit
	if seconds < leaseMinTTL || seconds > leaseMaxTTL {
		return 0, false
	}
	return seconds, true
}

func parseLeaseCommand(args []string) (leaseCommand, error) {
	p := leaseCommand{}
	if len(args) < 1 {
		return p, errnorm.Usage("invalid_flags", leaseUsage(""))
	}
	p.verb = args[0]
	allowed := map[string]bool{}
	switch p.verb {
	case "show":
	case "acquire":
		allowed["owner"], allowed["ttl"], allowed["reason"], allowed["if-epoch"] = true, true, true, true
	case "renew":
		allowed["owner"], allowed["ttl"] = true, true
	case "release":
		allowed["owner"], allowed["reason"], allowed["handoff-to"], allowed["ttl"] = true, true, true, true
	default:
		return p, errnorm.Usage("unknown_subcommand", "unknown lease subcommand "+p.verb+"; use show, acquire, renew or release")
	}
	usage := func(message string) (leaseCommand, error) {
		return p, errnorm.Usage("invalid_flags", message+"; "+leaseUsage(p.verb))
	}
	positionals := []string{}
	seen := map[string]bool{}
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		arg := rest[i]
		if !strings.HasPrefix(arg, "--") {
			positionals = append(positionals, arg)
			continue
		}
		key, value, equals := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		if !allowed[key] {
			return usage("unknown flag --" + key + " for lease " + p.verb)
		}
		if seen[key] {
			return usage("duplicate flag --" + key)
		}
		seen[key] = true
		if !equals {
			if i+1 >= len(rest) {
				return usage("missing value for --" + key)
			}
			i++
			value = rest[i]
		}
		switch key {
		case "owner":
			p.owner = strings.TrimSpace(value)
			if !leaseOwnerPattern.MatchString(p.owner) {
				return usage("--owner must be 1 to 128 characters of letters, digits and ._:@/+- starting with a letter or digit")
			}
		case "handoff-to":
			p.handoffTo = strings.TrimSpace(value)
			if !leaseOwnerPattern.MatchString(p.handoffTo) {
				return usage("--handoff-to must be 1 to 128 characters of letters, digits and ._:@/+- starting with a letter or digit")
			}
		case "ttl":
			seconds, ok := parseLeaseTTL(value)
			if !ok {
				return usage("--ttl must be a duration from 60s to 7d, such as 90m or 2h")
			}
			p.ttlSeconds = seconds
		case "reason":
			p.reason = strings.TrimSpace(value)
			if len([]rune(p.reason)) > 200 || strings.ContainsAny(p.reason, "\r\n") {
				return usage("--reason must be one line of at most 200 characters")
			}
		case "if-epoch":
			n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if err != nil || n < 0 {
				return usage("--if-epoch must be a non-negative integer")
			}
			p.ifEpoch = &n
		}
	}
	if len(positionals) != 1 || strings.TrimSpace(positionals[0]) == "" {
		return usage("lease " + p.verb + " takes exactly one card")
	}
	p.card = strings.TrimSpace(positionals[0])
	if p.verb != "show" && p.owner == "" {
		return usage("lease " + p.verb + " requires --owner")
	}
	if p.verb == "release" && p.handoffTo == "" && p.ttlSeconds > 0 {
		return usage("--ttl applies to lease release only with --handoff-to")
	}
	if p.handoffTo != "" && p.handoffTo == p.owner {
		return usage("--handoff-to must differ from --owner")
	}
	if p.verb == "acquire" && p.ttlSeconds == 0 {
		return usage("lease acquire requires --ttl")
	}
	return p, nil
}

func (a *App) runLeaseCommand(ctx context.Context, args []string, cfg config.Resolved) (string, *commandResult, error) {
	p, err := parseLeaseCommand(args)
	name := strings.TrimSpace("lease " + p.verb)
	if err != nil {
		return name, nil, err
	}
	path := "/cards/" + url.PathEscape(p.card) + "/lease"
	method := "GET"
	var body any
	if p.verb != "show" {
		method, path = "POST", path+"/"+p.verb
		req := map[string]any{"owner": p.owner}
		if p.ttlSeconds > 0 {
			req["ttl_seconds"] = p.ttlSeconds
		}
		if p.reason != "" {
			req["reason"] = p.reason
		}
		if p.handoffTo != "" {
			req["handoff_to"] = p.handoffTo
		}
		if p.ifEpoch != nil {
			req["if_epoch"] = *p.ifEpoch
		}
		body = req
	}
	cfg, err = a.cfgWithResolvedAuthToken(ctx, cfg)
	if err != nil {
		return name, nil, err
	}
	out, err := a.hostCall(ctx, cfg, method, path, body, generatedHeaders(cfg))
	if err != nil {
		return name, nil, err
	}
	return name, &commandResult{Text: leaseText(out), Data: out}, nil
}

func leaseText(out map[string]any) string {
	lease := asMap(out["lease"])
	var b strings.Builder
	fmt.Fprintf(&b, "lease card=%s state=%s epoch=%v", anyString(lease["card_ref"]), anyString(lease["state"]), lease["epoch"])
	for _, key := range []string{"owner", "expires_at", "remaining_seconds", "ttl_seconds", "released_at"} {
		if v, ok := lease[key]; ok && v != nil && fmt.Sprint(v) != "" {
			fmt.Fprintf(&b, " %s=%v", key, v)
		}
	}
	if changed, ok := out["changed"].(bool); ok {
		fmt.Fprintf(&b, " changed=%v", changed)
	}
	b.WriteString("\n")
	return b.String()
}
