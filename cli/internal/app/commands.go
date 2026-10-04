package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/httpclient"
	"agent-nexus-cli/internal/registry"
)

func (a *App) runCommand(ctx context.Context, args []string, cfg config.Resolved) (string, *commandResult, error) {
	return a.runCommandWithDebug(ctx, args, cfg, false)
}

func isDiagnosticGroup(group string) bool {
	switch group {
	case "threads", "events", "ref-edges", "derived", "actors", "meta":
		return true
	}
	return false
}

func isDebugGroup(group string) bool {
	return group == "inbox" || isDiagnosticGroup(group)
}

func (a *App) runCommandWithDebug(ctx context.Context, args []string, cfg config.Resolved, debug bool) (string, *commandResult, error) {
	if len(args) == 0 {
		return "root", nil, errnorm.Usage("command_required", "a command is required")
	}
	if args[0] == "debug" {
		if len(args) < 2 || !isDebugGroup(args[1]) {
			return "debug", nil, errnorm.Usage("unknown_subcommand", "unknown debug group")
		}
		name, result, err := a.runCommandWithDebug(ctx, args[1:], cfg, true)
		return "debug " + name, result, err
	}
	if args[0] == "inbox" && !debug {
		result, name, err := a.runFirstClassInboxCommand(ctx, args[1:], cfg)
		return name, result, err
	}
	if args[0] == "inbox" && debug {
		result, name, err := a.runInboxCommand(ctx, args[1:], cfg, true)
		return name, result, err
	}
	if isDiagnosticGroup(args[0]) && !debug {
		return args[0], nil, errnorm.Usage("unknown_command", "unknown command "+args[0]+"; use anx debug "+args[0])
	}
	if rewritten, ok := applyCommandShapeCompatibilityAlias(args); ok {
		args = rewritten
	}
	if len(args) >= 2 && isHelpToken(args[len(args)-1]) {
		topic := strings.Join(args[:len(args)-1], " ")
		if text, ok := helpTopicText(topic); ok {
			return "help", &commandResult{Text: text, Data: map[string]any{"help_text": text}}, nil
		}
	}
	if args[0] == "orient" {
		result, err := a.runOrient(ctx, args[1:], cfg)
		return "orient", result, err
	}
	if args[0] == "work" && len(args) >= 2 && isDailyWorkVerb(args[1]) {
		result, err := a.runDailyWork(ctx, args[1], args[2:], cfg)
		return "work " + args[1], result, err
	}
	if args[0] == "ask" && len(args) > 1 && args[1] == "withdraw" {
		result, err := a.runAskWithdraw(ctx, args[2:], cfg)
		return "ask withdraw", result, err
	}
	if args[0] == "ask" || args[0] == "review" || args[0] == "escalate" {
		result, err := a.runHumanAttentionCommand(ctx, args[0], args[1:], cfg)
		return args[0], result, err
	}
	if args[0] == "await" {
		result, err := a.runAwait(ctx, args[1:], cfg)
		return "await", result, err
	}
	if args[0] == "plan" {
		result, name, err := a.runPlanCommand(ctx, args, cfg)
		return name, result, err
	}
	if args[0] == "refs" {
		result, err := a.runRefResolve(ctx, args, cfg)
		return "refs resolve", result, err
	}
	if isWorkCommandRoot(args[0]) && !isReportLocalCommand(args) {
		result, name, err := a.runWorkCommand(ctx, args, cfg)
		return name, result, err
	}
	switch args[0] {
	case "series", "adapters":
		return a.runSeriesCommand(ctx, args, cfg)
	case "version":
		result, err := a.runVersion(cfg)
		return "version", result, err
	case "doctor":
		result, err := a.runHostDoctor(ctx, cfg)
		return "doctor", result, err
	case "update":
		result, err := a.runUpdate(ctx, args[1:], cfg)
		return "update", result, err
	case "bridge":
		result, name, err := a.runBridgeCommand(ctx, args[1:], cfg)
		return name, result, err
	case "auth":
		result, name, err := a.runAuth(ctx, args[1:], cfg)
		return name, result, err
	case "host":
		result, name, err := a.runHost(ctx, args[1:], cfg)
		return name, result, err
	case "runs":
		result, name, err := a.runRuns(ctx, args[1:], cfg)
		return name, result, err
	case "config":
		result, name, err := a.runConfig(ctx, args[1:], cfg)
		return name, result, err
	case "meta":
		result, name, err := a.runMeta(ctx, args[1:], cfg)
		return name, result, err
	case "report":
		name, result, err := a.runReportCommand(ctx, args[1:], cfg)
		return name, result, err
	case "notifications":
		result, name, err := a.runNotificationsCommand(ctx, args[1:], cfg)
		return name, result, err
	case "import":
		result, name, err := a.runImportCommand(ctx, args[1:], cfg)
		return name, result, err
	case "skills":
		result, name, err := a.runSkills(args[1:], cfg)
		return name, result, err
	case "install":
		result, name, err := a.runInstallCommand(args[1:])
		return name, result, err
	case "draft":
		result, name, err := a.runDraft(ctx, args[1:], cfg)
		return name, result, err
	case "provenance":
		result, name, err := a.runProvenanceCommand(ctx, args[1:], cfg)
		return name, result, err
	case "secret":
		result, name, err := a.runSecretCommand(ctx, args[1:], cfg)
		return name, result, err
	case "overview":
		if len(args) != 1 {
			return "overview", nil, errnorm.Usage("invalid_args", "anx overview takes no arguments")
		}
		result, err := a.invokeTypedJSON(ctx, cfg, "overview", "overview.get", nil, nil, nil)
		return "overview", result, err
	case "workspace":
		result, name, err := a.runWorkspaceCommand(ctx, args[1:], cfg)
		return name, result, err
	case "read":
		result, err := a.runReadCommand(ctx, args[1:], cfg)
		return "read", result, err
	case "url":
		result, err := a.runURLCommand(ctx, args[1:], cfg)
		return "url", result, err
	case "concepts":
		if len(args) > 1 {
			return "concepts", nil, errnorm.Usage("invalid_args", "unexpected positional arguments for `anx concepts`")
		}
		text := conceptsGuideText()
		return "concepts", &commandResult{Text: text, Data: conceptsGuideData()}, nil
	case "primitives":
		if len(args) == 1 || isHelpToken(args[1]) {
			text := conceptsGuideText()
			return "primitives guide", &commandResult{Text: text, Data: conceptsGuideData()}, nil
		}
		if len(args) == 2 && strings.TrimSpace(args[1]) == "guide" {
			text := conceptsGuideText()
			return "primitives guide", &commandResult{Text: text, Data: conceptsGuideData()}, nil
		}
		topic := strings.TrimSpace(args[0])
		if len(args) > 1 {
			topic = strings.TrimSpace(args[0] + " " + args[1])
		}
		return "primitives", nil, errnorm.Usage("unknown_command", fmt.Sprintf("unknown command %q", topic))
	case "actors", "threads", "topics", "ref-edges", "cards", "artifacts", "boards", "docs", "events", "inbox", "derived":
		result, name, err := a.runTypedResource(ctx, args[0], args[1:], cfg)
		return name, result, err
	case "api":
		if len(args) < 2 {
			return "api", nil, apiSubcommandSpec.requiredError()
		}
		if apiSubcommandSpec.normalize(args[1]) != "call" {
			return "api", nil, apiSubcommandSpec.unknownError(args[1])
		}
		result, err := a.runAPICall(ctx, args[2:], cfg)
		return "api call", result, err
	case "help", "--help", "-h":
		if len(args) > 1 {
			if len(args) == 2 && args[1] == "--all" {
				text := a.rootUsageTextAll()
				return "help", &commandResult{Text: text, Data: map[string]any{"help_text": text}}, nil
			}
			topic := strings.Join(args[1:], " ")
			if text, ok := helpTopicText(topic); ok {
				return "help", &commandResult{Text: text, Data: map[string]any{"help_text": text}}, nil
			}
			return "help", nil, errnorm.Usage("unknown_command", fmt.Sprintf("unknown help topic %q", topic))
		}
		text := a.rootUsageText()
		return "help", &commandResult{Text: text, Data: map[string]any{"help_text": text}}, nil
	default:
		return args[0], nil, errnorm.Usage("unknown_command", fmt.Sprintf("unknown command %q", args[0]))
	}
}

func (a *App) runVersion(cfg config.Resolved) (*commandResult, error) {
	meta, err := registry.LoadEmbedded()
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindInternal, "registry_unavailable", "failed to load embedded command registry", err)
	}
	data := map[string]any{
		"cli_version":           httpclient.CLIVersion,
		"base_url":              cfg.BaseURL,
		"agent":                 cfg.Agent,
		"timeout":               cfg.Timeout.String(),
		"registry_commands":     meta.CommandCount,
		"contract_version":      meta.ContractVersion,
		"openapi_version":       meta.OpenAPIVersion,
		"registry_generated_by": meta.GeneratedBy,
	}

	lines := []string{
		"CLI version: " + httpclient.CLIVersion,
		"Base URL: " + cfg.BaseURL,
		"Agent: " + cfg.Agent,
		"Timeout: " + cfg.Timeout.String(),
		fmt.Sprintf("Registry commands: %d", meta.CommandCount),
		"Contract version: " + meta.ContractVersion,
	}
	return &commandResult{Data: data, Text: strings.Join(lines, "\n")}, nil
}

type doctorCheck struct {
	Name                  string `json:"name"`
	OK                    bool   `json:"ok"`
	Status                string `json:"status,omitempty"`
	Message               string `json:"message"`
	RecommendedCLIVersion string `json:"recommended_cli_version,omitempty"`
	DurationMS            int64  `json:"duration_ms"`
}

func apiCallUsageText() string {
	return strings.TrimSpace(`Local Help: api call

Perform an arbitrary HTTP request against the configured core base URL.

Usage:
  anx api call [--method <method>] [--path <path>] [<method> <path>] [--from-file <file>] [--header key:value]

Flags:
  --method <method>     HTTP method (default GET).
  --path <path>         Request path or absolute URL.
  --from-file <path>    Request body from file (stdin otherwise when needed).
  --header key:value    Repeatable request header.

Examples:
  anx api call --method GET --path /readyz
  anx api call POST /events --from-file body.json`)
}

func (a *App) runAPICall(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("api call")
	var (
		methodFlag trackedString
		pathFlag   trackedString
		fromFile   trackedString
		headers    headerList
	)
	fs.Var(&methodFlag, "method", "HTTP method")
	fs.Var(&pathFlag, "path", "Request path or absolute URL")
	fs.Var(&fromFile, "from-file", "Load request body from file path")
	fs.Var(&headers, "header", "Request header in key:value form (repeatable)")

	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_api_flags", err.Error())
	}

	positionals := fs.Args()
	method := strings.TrimSpace(methodFlag.value)
	requestPath := strings.TrimSpace(pathFlag.value)
	if !methodFlag.set && len(positionals) > 0 {
		method = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if !pathFlag.set && len(positionals) > 0 {
		requestPath = strings.TrimSpace(positionals[0])
		positionals = positionals[1:]
	}
	if len(positionals) > 0 {
		return nil, errnorm.Usage("invalid_api_args", "unexpected positional arguments for `anx api call`")
	}
	if method == "" {
		method = http.MethodGet
	}
	if requestPath == "" {
		return nil, errnorm.Usage("invalid_request", "path is required; use --path or positional path")
	}

	headersMap, err := parseHeaders(headers)
	if err != nil {
		return nil, errnorm.Usage("invalid_header", err.Error())
	}
	if _, hasAuthorization := headersMap["Authorization"]; !hasAuthorization && shouldAutoAttachAuth(requestPath) {
		if cfg.AccessToken == "" {
			if _, _, identityErr := a.identityName(cfg); identityErr == nil {
				if resolved, resolveErr := a.resolveHostAgent(ctx, cfg); resolveErr == nil {
					cfg = resolved
				}
			}
		}
		if cfg.AccessToken != "" {
			headersMap["Authorization"] = "Bearer " + cfg.AccessToken
		}
	}
	if cfg.RunID != "" && strings.ToUpper(method) != "GET" && strings.ToUpper(method) != "HEAD" {
		headersMap["X-ANX-Run-Id"] = cfg.RunID
	}
	requestBody, err := a.readBodyInput(strings.TrimSpace(fromFile.value))
	if err != nil {
		return nil, err
	}

	client, err := httpclient.New(cfg)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}
	callCtx, cancel := httpclient.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	resp, err := client.RawCall(callCtx, httpclient.RawRequest{
		Method:  method,
		Path:    requestPath,
		Headers: headersMap,
		Body:    requestBody,
	})
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindNetwork, "request_failed", "failed to perform request", err)
	}

	parsedBody := parseResponseBody(resp.Body)
	headersSorted := map[string][]string(resp.Headers)
	if resp.StatusCode >= http.StatusBadRequest {
		return &commandResult{Data: map[string]any{
			"method":      strings.ToUpper(method),
			"path":        requestPath,
			"status_code": resp.StatusCode,
			"headers":     headersSorted,
			"body":        parsedBody,
		}}, errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
	}
	data := map[string]any{
		"method":      strings.ToUpper(method),
		"path":        requestPath,
		"status_code": resp.StatusCode,
		"headers":     headersSorted,
		"body":        parsedBody,
	}
	text := formatAPICallText(strings.ToUpper(method), requestPath, resp.StatusCode, headersSorted, resp.Body)
	return &commandResult{Data: data, Text: text}, nil
}

const stdinReadTimeout = 2 * time.Second

func (a *App) readStdinBody() ([]byte, error) {
	if a.Stdin == nil {
		return nil, nil
	}
	if a.StdinIsTTY != nil && a.StdinIsTTY() {
		return nil, nil
	}

	type readResult struct {
		data []byte
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		data, err := io.ReadAll(a.Stdin)
		ch <- readResult{data, err}
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			return nil, res.err
		}
		if len(strings.TrimSpace(string(res.data))) == 0 {
			return nil, nil
		}
		return res.data, nil
	case <-time.After(stdinReadTimeout):
		return nil, errnorm.Usage("stdin_timeout",
			"no input received on stdin within timeout; pipe JSON input or use --from-file")
	}
}

func parseResponseBody(body []byte) any {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}
	var parsed any
	if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
		return parsed
	}
	return trimmed
}

func formatAPICallText(method string, requestPath string, statusCode int, headers map[string][]string, body []byte) string {
	lines := []string{fmt.Sprintf("%s %s", method, requestPath), fmt.Sprintf("status: %d", statusCode)}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("header %s: %s", key, strings.Join(headers[key], ", ")))
	}
	if len(body) > 0 {
		lines = append(lines, "")
		lines = append(lines, string(body))
	}
	return strings.Join(lines, "\n")
}

func shouldAutoAttachAuth(requestPath string) bool {
	requestPath = strings.TrimSpace(requestPath)
	if requestPath == "" {
		return false
	}
	if strings.HasPrefix(requestPath, "http://") || strings.HasPrefix(requestPath, "https://") {
		parsed, err := url.Parse(requestPath)
		if err != nil {
			return false
		}
		requestPath = parsed.Path
	}
	if !strings.HasPrefix(requestPath, "/") {
		requestPath = "/" + requestPath
	}
	switch requestPath {
	case "/health", "/livez", "/readyz", "/version", "/meta/handshake", "/auth/token":
		return false
	}
	return true
}
