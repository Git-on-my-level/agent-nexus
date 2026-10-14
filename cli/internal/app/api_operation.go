package app

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/registry"
)

// `anx api <operationId|command-id>` is the scripting tier: any contract
// operation by name, without a dedicated verb. It looks the operation up in
// the embedded command metadata (method and path template), fills path
// parameters from --param, sends the remaining --param pairs as query
// parameters and --json-body as the request body.

type apiOperationArgs struct {
	operation string
	params    map[string]string
	order     []string
	jsonBody  string
}

func apiOperationUsage() string {
	return "usage: anx api <operationId|command-id> [--param name=value]... [--json-body <json>|@<file>|-]"
}

func parseAPIOperationArgs(args []string) (apiOperationArgs, error) {
	out := apiOperationArgs{params: map[string]string{}}
	leading, rest := splitLeadingPositionals(args)
	fs := newSilentFlagSet("api")
	var params trackedStrings
	var jsonBody trackedString
	fs.Var(&params, "param", "Path or query parameter name=value (repeatable)")
	fs.Var(&jsonBody, "json-body", "JSON request body, @file, or - for stdin")
	if err := fs.Parse(rest); err != nil {
		return out, errnorm.Usage("invalid_flags", err.Error())
	}
	positionals := append(leading, fs.Args()...)
	if len(positionals) != 1 || strings.TrimSpace(positionals[0]) == "" {
		return out, errnorm.Usage("invalid_args", apiOperationUsage())
	}
	out.operation = strings.TrimSpace(positionals[0])
	for _, raw := range params.values {
		name, value, ok := strings.Cut(raw, "=")
		name = strings.TrimSpace(name)
		if !ok || name == "" {
			return out, errnorm.Usage("invalid_request", fmt.Sprintf("--param %q must be name=value", raw))
		}
		if _, seen := out.params[name]; !seen {
			out.order = append(out.order, name)
		}
		out.params[name] = value
	}
	out.jsonBody = jsonBody.value
	return out, nil
}

func lookupAPIOperation(name string) (registry.Command, bool) {
	meta, err := registry.LoadEmbedded()
	if err != nil {
		return registry.Command{}, false
	}
	for _, cmd := range meta.Commands {
		if cmd.OperationID == name || cmd.CommandID == name {
			return cmd, true
		}
	}
	return registry.Command{}, false
}

// renderAPIOperationPath fills {name} placeholders and returns the remaining
// params as an encoded query string.
func renderAPIOperationPath(cmd registry.Command, parsed apiOperationArgs) (string, error) {
	path := cmd.Path
	used := map[string]bool{}
	var missing []string
	for {
		start := strings.Index(path, "{")
		if start < 0 {
			break
		}
		end := strings.Index(path[start:], "}")
		if end < 0 {
			break
		}
		name := path[start+1 : start+end]
		value, ok := parsed.params[name]
		if !ok || strings.TrimSpace(value) == "" {
			missing = append(missing, name)
			value = ""
		}
		used[name] = true
		path = path[:start] + url.PathEscape(value) + path[start+end+1:]
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return "", errnorm.Usage("invalid_request", fmt.Sprintf("%s needs --param %s=<value>", firstNonEmpty(cmd.OperationID, cmd.CommandID), strings.Join(missing, "=<value> --param ")))
	}
	query := url.Values{}
	for _, name := range parsed.order {
		if !used[name] {
			query.Add(name, parsed.params[name])
		}
	}
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	return path, nil
}

func (a *App) runAPIOperation(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	parsed, err := parseAPIOperationArgs(args)
	if err != nil {
		return nil, err
	}
	cmd, ok := lookupAPIOperation(parsed.operation)
	if !ok {
		return nil, errnorm.Usage("unknown_operation", fmt.Sprintf("unknown operation %q; list operations with anx debug meta commands", parsed.operation))
	}
	path, err := renderAPIOperationPath(cmd, parsed)
	if err != nil {
		return nil, err
	}
	var body any
	if raw := strings.TrimSpace(parsed.jsonBody); raw != "" {
		content := []byte(raw)
		if strings.HasPrefix(raw, "@") || raw == "-" {
			content, err = a.readBodyInput(strings.TrimPrefix(raw, "@"))
			if err != nil {
				return nil, err
			}
		}
		body, err = decodeJSONPayload(content)
		if err != nil {
			return nil, errnorm.Usage("invalid_request", "--json-body must be JSON: "+err.Error())
		}
	}
	result, err := a.invokeRawJSON(ctx, cfg, "api", strings.ToUpper(cmd.Method), path, body)
	if err != nil {
		return nil, err
	}
	data := asMap(result.Data)
	data["operation"] = map[string]any{"operation_id": cmd.OperationID, "command_id": cmd.CommandID, "method": strings.ToUpper(cmd.Method), "path": path}
	return result, nil
}
