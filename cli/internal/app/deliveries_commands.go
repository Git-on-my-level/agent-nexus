package app

import (
	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// deliveriesCommand is the parsed form of `anx deliveries ...` and
// `anx notifications settings ...`. Both groups share one strict parser so the
// usage preflight and the runtime agree.
type deliveriesCommand struct {
	group, verb, deliveryID, channel, claimToken, outcome, errText, bodyFile string
	limit, leaseSeconds                                                      int
	retryable, retryableSet                                                  bool
}

func parseDeliveriesCommand(args []string) (deliveriesCommand, error) {
	p := deliveriesCommand{group: "deliveries"}
	usage := func(message string) (deliveriesCommand, error) { return p, errnorm.Usage("invalid_flags", message) }
	if len(args) < 1 {
		return usage("use anx deliveries list|claim|receipt")
	}
	p.verb = args[0]
	switch p.verb {
	case "list", "claim", "receipt":
	default:
		return p, errnorm.Usage("unknown_subcommand", "unknown deliveries subcommand "+p.verb)
	}
	return parseDeliveriesFlags(p, args[1:], usage)
}

func parseNotificationSettingsCommand(args []string) (deliveriesCommand, error) {
	p := deliveriesCommand{group: "notifications settings"}
	usage := func(message string) (deliveriesCommand, error) { return p, errnorm.Usage("invalid_flags", message) }
	if len(args) < 1 {
		return usage("use anx notifications settings get|set")
	}
	p.verb = args[0]
	if p.verb != "get" && p.verb != "set" {
		return p, errnorm.Usage("unknown_subcommand", "unknown notifications settings subcommand "+p.verb)
	}
	return parseDeliveriesFlags(p, args[1:], usage)
}

func parseDeliveriesFlags(p deliveriesCommand, args []string, usage func(string) (deliveriesCommand, error)) (deliveriesCommand, error) {
	position := []string{}
	seen := map[string]bool{}
	isSettings := p.group == "notifications settings"
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			position = append(position, arg)
			continue
		}
		key, value, equals := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
		if !equals {
			if i+1 >= len(args) {
				return usage("missing value for --" + key)
			}
			i++
			value = args[i]
		}
		if seen[key] {
			return usage("duplicate flag --" + key)
		}
		seen[key] = true
		allowed := map[string]bool{}
		switch {
		case isSettings && p.verb == "set":
			allowed["body-file"] = true
		case !isSettings && p.verb == "list":
			allowed["limit"] = true
		case !isSettings && p.verb == "claim":
			allowed["channel"], allowed["limit"], allowed["lease-seconds"] = true, true, true
		case !isSettings && p.verb == "receipt":
			allowed["claim-token"], allowed["outcome"], allowed["error"], allowed["retryable"] = true, true, true, true
		}
		if !allowed[key] {
			return usage("unknown flag --" + key + " for " + p.group + " " + p.verb)
		}
		switch key {
		case "body-file":
			p.bodyFile = value
		case "channel":
			p.channel = value
		case "claim-token":
			p.claimToken = value
		case "outcome":
			p.outcome = value
		case "error":
			p.errText = value
		case "limit", "lease-seconds":
			n, err := strconv.Atoi(value)
			if err != nil || n < 1 {
				return usage("--" + key + " must be a positive integer")
			}
			if key == "limit" {
				p.limit = n
			} else {
				p.leaseSeconds = n
			}
		case "retryable":
			b, err := strconv.ParseBool(value)
			if err != nil {
				return usage("--retryable must be true or false")
			}
			p.retryable, p.retryableSet = b, true
		}
	}
	if !isSettings && p.verb == "receipt" {
		if len(position) != 1 || strings.TrimSpace(position[0]) == "" {
			return usage("deliveries receipt requires <delivery_id>")
		}
		p.deliveryID = position[0]
		position = nil
	}
	if len(position) > 0 {
		return usage("unexpected positional arguments")
	}
	switch {
	case isSettings && p.verb == "set" && p.bodyFile == "":
		return usage("notifications settings set requires --body-file <path|->")
	case !isSettings && p.verb == "claim" && strings.TrimSpace(p.channel) == "":
		return usage("deliveries claim requires --channel")
	case !isSettings && p.verb == "receipt":
		if p.claimToken == "" {
			return usage("deliveries receipt requires --claim-token")
		}
		if p.outcome != "delivered" && p.outcome != "failed" && p.outcome != "skipped" {
			return usage("--outcome must be delivered, failed or skipped")
		}
		if p.outcome == "delivered" && (p.errText != "" || p.retryableSet) {
			return usage("--error and --retryable apply only to --outcome failed")
		}
		if p.outcome == "skipped" && p.retryableSet {
			return usage("--retryable applies only to --outcome failed")
		}
	}
	return p, nil
}

func (a *App) runDeliveriesCommand(ctx context.Context, args []string, cfg config.Resolved) (string, *commandResult, error) {
	p, err := parseDeliveriesCommand(args)
	return a.runDeliveriesParsed(ctx, p, err, cfg)
}

func (a *App) runNotificationSettingsCommand(ctx context.Context, args []string, cfg config.Resolved) (string, *commandResult, error) {
	p, err := parseNotificationSettingsCommand(args)
	return a.runDeliveriesParsed(ctx, p, err, cfg)
}

func (a *App) runDeliveriesParsed(ctx context.Context, p deliveriesCommand, parseErr error, cfg config.Resolved) (string, *commandResult, error) {
	name := p.group + " " + p.verb
	if parseErr != nil {
		return name, nil, parseErr
	}
	method, path := "GET", ""
	var body any
	switch {
	case p.group == "notifications settings":
		path = "/notification-settings"
		if p.verb == "set" {
			method = "PUT"
			var raw []byte
			var err error
			if p.bodyFile == "-" {
				raw, err = a.readStdinBytes(64*1024 + 1)
			} else {
				raw, err = a.readInputFile(p.bodyFile)
			}
			if err != nil {
				return name, nil, err
			}
			if len(raw) > 64*1024 || json.Unmarshal(raw, &body) != nil {
				return name, nil, errnorm.Usage("invalid_body", "invalid notification settings JSON")
			}
		}
	case p.verb == "list":
		path = "/deliveries"
		if p.limit > 0 {
			path += "?limit=" + strconv.Itoa(p.limit)
		}
	case p.verb == "claim":
		method, path = "POST", "/deliveries/claim"
		req := map[string]any{"channel": strings.TrimSpace(p.channel)}
		if p.limit > 0 {
			req["limit"] = p.limit
		}
		if p.leaseSeconds > 0 {
			req["lease_seconds"] = p.leaseSeconds
		}
		body = req
	case p.verb == "receipt":
		method, path = "POST", "/deliveries/"+url.PathEscape(p.deliveryID)+"/receipt"
		req := map[string]any{"claim_token": p.claimToken, "outcome": p.outcome}
		if p.errText != "" {
			req["error"] = p.errText
		}
		if p.outcome == "failed" {
			req["retryable"] = p.retryable
		}
		body = req
	}
	cfg, err := a.cfgWithResolvedAuthToken(ctx, cfg)
	if err != nil {
		return name, nil, err
	}
	out, err := a.hostCall(ctx, cfg, method, path, body, generatedHeaders(cfg))
	if err != nil {
		return name, nil, err
	}
	return name, &commandResult{Text: deliveriesText(p, out), Data: map[string]any{"body": out}}, nil
}

func deliveriesText(p deliveriesCommand, out map[string]any) string {
	var b strings.Builder
	switch {
	case p.group == "notifications settings":
		for _, raw := range asSlice(out["channels"]) {
			row := asMap(raw)
			fmt.Fprintf(&b, "notification_channel channel=%q address=%q push=%v digest=%v\n", anyString(row["channel"]), anyString(row["address"]), row["push_enabled"], row["digest_enabled"])
		}
		fmt.Fprintf(&b, "notification_settings channels=%d available=%d\n", len(asSlice(out["channels"])), len(asSlice(out["available_channels"])))
	case p.verb == "list":
		rows := asSlice(out["deliveries"])
		for _, raw := range rows {
			row := asMap(raw)
			d := asMap(row["delivery"])
			fmt.Fprintf(&b, "delivery id=%q channel=%q kind=%q status=%q attempt=%v receipts=%d\n", anyString(d["delivery_id"]), anyString(d["channel"]), anyString(d["kind"]), anyString(d["status"]), d["attempt"], len(asSlice(row["receipts"])))
		}
		fmt.Fprintf(&b, "deliveries count=%d\n", len(rows))
	case p.verb == "claim":
		rows := asSlice(out["deliveries"])
		for _, raw := range rows {
			d := asMap(raw)
			fmt.Fprintf(&b, "claimed id=%q channel=%q kind=%q attempt=%v lease_expires_at=%q\n", anyString(d["delivery_id"]), anyString(d["channel"]), anyString(d["kind"]), d["attempt"], anyString(d["lease_expires_at"]))
		}
		fmt.Fprintf(&b, "deliveries claimed=%d\n", len(rows))
	default:
		d := asMap(out["delivery"])
		fmt.Fprintf(&b, "delivery id=%q status=%q attempt=%v\n", anyString(d["delivery_id"]), anyString(d["status"]), d["attempt"])
	}
	return b.String()
}
