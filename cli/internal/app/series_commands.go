package app

import (
	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type seriesCommand struct {
	group, verb, name, value, adapter, ts, window, step, agg, bodyFile string
	labels                                                             map[string]string
	fromCommand                                                        bool
	command                                                            []string
}

func parseSeriesCommand(args []string) (seriesCommand, error) {
	p := seriesCommand{labels: map[string]string{}}
	usage := func(message string) (seriesCommand, error) { return p, errnorm.Usage("invalid_flags", message) }
	if len(args) < 2 {
		return usage("use anx series list|show|query|push or anx adapters declare|list|revoke|delete|token")
	}
	p.group, p.verb = args[0], args[1]
	allowed := map[string]bool{}
	if p.group == "series" {
		allowed = map[string]bool{"list": true, "show": true, "query": true, "push": true}
	} else {
		allowed = map[string]bool{"declare": true, "list": true, "revoke": true, "delete": true, "token": true}
	}
	if !allowed[p.verb] {
		return p, errnorm.Usage("unknown_subcommand", "unknown "+p.group+" subcommand "+p.verb)
	}
	position := []string{}
	seen := map[string]bool{}
	for i := 2; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			p.command = args[i+1:]
			break
		}
		if arg == "--from-command" {
			if p.verb != "push" || p.fromCommand {
				return usage("--from-command is only accepted once on series push")
			}
			p.fromCommand = true
			continue
		}
		if strings.HasPrefix(arg, "--") {
			key, value, equals := strings.Cut(strings.TrimPrefix(arg, "--"), "=")
			if !equals {
				if i+1 >= len(args) {
					return usage("missing value for --" + key)
				}
				i++
				value = args[i]
			}
			if seen[key] && key != "label" {
				return usage("duplicate flag --" + key)
			}
			seen[key] = true
			switch key {
			case "adapter":
				if p.verb != "push" {
					return usage("--adapter requires push")
				}
				p.adapter = value
			case "series":
				if p.verb != "push" {
					return usage("--series requires push")
				}
				p.name = value
			case "ts":
				if p.verb != "push" {
					return usage("--ts requires push")
				}
				p.ts = value
			case "label":
				if p.group != "series" || p.verb == "list" {
					return usage("--label requires series push, show or query")
				}
				k, v, ok := strings.Cut(value, "=")
				if !ok || k == "" {
					return usage("labels must be k=v")
				}
				if _, exists := p.labels[k]; exists {
					return usage("duplicate label key")
				}
				p.labels[k] = v
			case "range", "step", "agg":
				if p.group != "series" || (p.verb != "query" && p.verb != "show") {
					return usage("query flags require show or query")
				}
				switch key {
				case "range":
					p.window = value
				case "step":
					p.step = value
				case "agg":
					p.agg = value
				}
			case "body-file":
				if p.group != "adapters" || p.verb != "declare" {
					return usage("--body-file requires adapters declare")
				}
				p.bodyFile = value
			default:
				return usage("unknown flag --" + key)
			}
		} else {
			position = append(position, arg)
		}
	}
	if p.name == "" && len(position) > 0 {
		p.name = position[0]
		position = position[1:]
	}
	if p.verb == "push" && !p.fromCommand {
		if len(position) != 1 || p.name == "" {
			return usage("series push requires <name> <value>")
		}
		p.value = position[0]
		if v, err := strconv.ParseFloat(p.value, 64); err == nil {
			if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 1e12 {
				return usage("numeric value must be finite within ±1e12")
			}
		} else if strings.TrimSpace(p.value) == "" || len(p.value) > 128 {
			return usage("state must be a nonempty string up to 128 bytes")
		}
		position = nil
	}
	if len(position) > 0 {
		return usage("unexpected positional arguments")
	}
	if p.fromCommand && len(p.command) == 0 {
		return usage("--from-command requires -- <cmd> [args]")
	}
	if !p.fromCommand && len(p.command) > 0 {
		return usage("command argv requires --from-command")
	}
	switch p.verb {
	case "show", "query", "revoke", "delete", "token":
		if p.name == "" {
			return usage("a name is required")
		}
	case "declare":
		if p.bodyFile == "" || p.name != "" {
			return usage("adapters declare requires --body-file <path|->")
		}
	case "list":
		if p.name != "" {
			return usage("list takes no name")
		}
	}
	if p.ts != "" {
		if _, err := time.Parse(time.RFC3339Nano, p.ts); err != nil {
			return usage("--ts must be RFC3339")
		}
	}
	return p, nil
}

// A command's output is bounded even when it produces an accidental log stream.
type limitedSeriesOutput struct{ bytes.Buffer }

func (b *limitedSeriesOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 65536 {
		return 0, fmt.Errorf("command stdout exceeds 64 KiB")
	}
	return b.Buffer.Write(p)
}
func decodeSeriesOutput(raw []byte) (map[string]any, error) {
	var value any
	if err := json.Unmarshal(bytes.TrimSpace(raw), &value); err != nil {
		return nil, errnorm.Usage("invalid_command_output", "command stdout must be a number or a JSON series point")
	}
	if number, ok := value.(float64); ok && math.Abs(number) <= 1e12 {
		return map[string]any{"value": number}, nil
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, errnorm.Usage("invalid_command_output", "expected a number or JSON object")
	}
	for key := range object {
		if key != "series" && key != "value" && key != "state" && key != "ts" && key != "labels" {
			return nil, errnorm.Usage("invalid_command_output", "unexpected command output field")
		}
	}
	_, numeric := object["value"].(float64)
	_, state := object["state"].(string)
	if numeric == state {
		return nil, errnorm.Usage("invalid_command_output", "output must contain exactly one value or state")
	}
	return object, nil
}
func (a *App) runSeriesCommand(ctx context.Context, args []string, cfg config.Resolved) (string, *commandResult, error) {
	p, err := parseSeriesCommand(args)
	name := p.group + " " + p.verb
	if err != nil {
		return name, nil, err
	}
	var body any
	method := "GET"
	path := "/" + p.group
	if p.group == "adapters" {
		switch p.verb {
		case "declare":
			method = "POST"
			var raw []byte
			if p.bodyFile == "-" {
				raw, err = a.readStdinBytes(128*1024 + 1)
			} else {
				raw, err = a.ReadFile(p.bodyFile)
			}
			if err != nil {
				return name, nil, err
			}
			if len(raw) > 128*1024 || json.Unmarshal(raw, &body) != nil {
				return name, nil, errnorm.Usage("invalid_body", "invalid adapter declaration JSON")
			}
		case "revoke", "token":
			method = "POST"
			path += "/" + url.PathEscape(p.name) + "/" + p.verb
		case "delete":
			method = "DELETE"
			path += "/" + url.PathEscape(p.name)
		}
	} else if p.verb != "list" {
		if p.verb == "push" {
			method = "POST"
			point := map[string]any{}
			if p.fromCommand {
				callCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
				defer cancel()
				cmd := exec.CommandContext(callCtx, p.command[0], p.command[1:]...)
				out := &limitedSeriesOutput{}
				cmd.Stdout = out
				cmd.Stderr = a.Stderr
				if err = cmd.Run(); err != nil {
					return name, nil, errnorm.Wrap(errnorm.KindLocal, "command_failed", "series source command failed", err)
				}
				point, err = decodeSeriesOutput(out.Bytes())
				if err != nil {
					return name, nil, err
				}
				if outputName, exists := point["series"]; exists {
					s, ok := outputName.(string)
					if !ok || s == "" || p.name != "" && p.name != s {
						return name, nil, errnorm.Usage("invalid_command_output", "output series disagrees with command name")
					}
					p.name = s
					delete(point, "series")
				}
			} else {
				v, err := strconv.ParseFloat(p.value, 64)
				if err == nil {
					point["value"] = v
				} else {
					point["state"] = p.value
				}
			}
			if p.name == "" {
				return name, nil, errnorm.Usage("series_required", "number stdout requires --series <name>; JSON may supply series")
			}
			if p.ts != "" {
				point["ts"] = p.ts
			}
			if len(p.labels) > 0 {
				existing := asMap(point["labels"])
				if existing == nil {
					existing = map[string]any{}
				}
				for k, v := range p.labels {
					existing[k] = v
				}
				point["labels"] = existing
			}
			body = point
			path += "/" + url.PathEscape(p.name) + "/points"
		} else {
			path += "/" + url.PathEscape(p.name)
			if p.verb == "query" {
				path += "/query"
			}
			q := url.Values{}
			if p.window != "" {
				q.Set("range", p.window)
			}
			if p.step != "" {
				q.Set("step", p.step)
			}
			if p.agg != "" {
				q.Set("agg", p.agg)
			}
			for k, v := range p.labels {
				q.Add("label", k+"="+v)
			}
			if len(q) > 0 {
				path += "?" + q.Encode()
			}
		}
	}
	cfg, err = a.cfgWithResolvedAuthToken(ctx, cfg)
	if err != nil {
		return name, nil, err
	}
	if p.group == "series" && p.verb == "push" {
		if p.adapter == "" {
			inventory, err := a.hostCall(ctx, cfg, "GET", "/series", nil, generatedHeaders(cfg))
			if err != nil {
				return name, nil, err
			}
			for _, raw := range asSlice(inventory["series"]) {
				row := asMap(raw)
				if anyString(row["name"]) == p.name {
					p.adapter = anyString(row["adapter"])
					break
				}
			}
			if p.adapter == "" {
				return name, nil, errnorm.Usage("adapter_required", "declare an adapter for this series before pushing")
			}
		}
		token, err := a.hostCall(ctx, cfg, "POST", "/adapters/"+url.PathEscape(p.adapter)+"/token", nil, generatedHeaders(cfg))
		if err != nil {
			return name, nil, err
		}
		cfg.AccessToken = anyString(asMap(token["tokens"])["access_token"])
		if cfg.AccessToken == "" {
			return name, nil, errnorm.Usage("invalid_response", "missing scoped token")
		}
	}
	out, err := a.hostCall(ctx, cfg, method, path, body, generatedHeaders(cfg))
	if err != nil {
		return name, nil, err
	}
	text := ""
	rows := asSlice(out[p.group])
	for _, raw := range rows {
		row := asMap(raw)
		text += fmt.Sprintf("%s name=%q adapter=%q kind=%q host=%q\n", p.group, anyString(row["name"]), anyString(row["adapter"]), anyString(row["kind"]), anyString(row["host"]))
	}
	if len(rows) == 0 {
		if p.verb == "list" {
			text = p.group + " count=0\n"
		} else {
			raw, _ := json.Marshal(out)
			text = fmt.Sprintf("%s name=%q result=%s\n", p.group, p.name, raw)
		}
	}
	return name, &commandResult{Text: text, Data: map[string]any{"body": out}}, nil
}
