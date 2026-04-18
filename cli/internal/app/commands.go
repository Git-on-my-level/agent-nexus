package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"agent-nexus-cli/internal/authcli"
	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/httpclient"
	"agent-nexus-cli/internal/registry"
)

func (a *App) runCommand(ctx context.Context, args []string, cfg config.Resolved) (string, *commandResult, error) {
	if len(args) == 0 {
		return "root", nil, errnorm.Usage("command_required", "a command is required")
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
	switch args[0] {
	case "version":
		result, err := a.runVersion(cfg)
		return "version", result, err
	case "doctor":
		result, err := a.runDoctor(ctx, cfg)
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
	case "config":
		result, name, err := a.runConfig(ctx, args[1:], cfg)
		return name, result, err
	case "meta":
		result, name, err := a.runMeta(ctx, args[1:], cfg)
		return name, result, err
	case "notifications":
		result, name, err := a.runNotificationsCommand(ctx, args[1:], cfg)
		return name, result, err
	case "import":
		result, name, err := a.runImportCommand(ctx, args[1:], cfg)
		return name, result, err
	case "draft":
		result, name, err := a.runDraft(ctx, args[1:], cfg)
		return name, result, err
	case "provenance":
		result, name, err := a.runProvenanceCommand(ctx, args[1:], cfg)
		return name, result, err
	case "ask":
		result, err := a.runAskCommand(ctx, args[1:], cfg)
		return "ask", result, err
	case "secret":
		result, name, err := a.runSecretCommand(ctx, args[1:], cfg)
		return name, result, err
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
	case "actors", "threads", "topics", "ref-edges", "cards", "artifacts", "boards", "docs", "events", "inbox", "receipts", "reviews", "derived":
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
	Name       string `json:"name"`
	OK         bool   `json:"ok"`
	Message    string `json:"message"`
	DurationMS int64  `json:"duration_ms"`
}

func (a *App) runDoctor(ctx context.Context, cfg config.Resolved) (*commandResult, error) {
	checks := make([]doctorCheck, 0, 4)
	hasFailure := false

	addCheck := func(name string, fn func() (bool, string, error)) {
		started := time.Now()
		ok, message, err := fn()
		if err != nil {
			ok = false
			if strings.TrimSpace(message) == "" {
				message = err.Error()
			}
		}
		if !ok {
			hasFailure = true
		}
		checks = append(checks, doctorCheck{Name: name, OK: ok, Message: message, DurationMS: time.Since(started).Milliseconds()})
	}

	addCheck("profile_path", func() (bool, string, error) {
		_, err := os.Stat(cfg.ProfilePath)
		if err == nil {
			return true, "profile loaded from " + cfg.ProfilePath, nil
		}
		if os.IsNotExist(err) {
			return true, "profile file not found; using defaults/env/flags", nil
		}
		return false, "", err
	})

	addCheck("base_url", func() (bool, string, error) {
		parsed, err := url.Parse(cfg.BaseURL)
		if err != nil {
			return false, "", err
		}
		if parsed.Scheme == "" || parsed.Host == "" {
			return false, "base url must include scheme and host", nil
		}
		return true, "base url parsed", nil
	})

	client, err := httpclient.New(cfg)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "http_client_init_failed", "failed to initialize HTTP client", err)
	}

	addCheck("core_health", func() (bool, string, error) {
		callCtx, cancel := httpclient.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
		resp, callErr := client.RawCall(callCtx, httpclient.RawRequest{Method: http.MethodGet, Path: "/readyz"})
		if callErr != nil {
			return false, "", callErr
		}
		if resp.StatusCode != http.StatusOK {
			return false, fmt.Sprintf("health status %d", resp.StatusCode), nil
		}
		return true, "core health endpoint reachable", nil
	})

	addCheck("core_handshake", func() (bool, string, error) {
		callCtx, cancel := httpclient.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
		resp, callErr := client.RawCall(callCtx, httpclient.RawRequest{Method: http.MethodGet, Path: "/meta/handshake"})
		if callErr != nil {
			return false, "", callErr
		}
		if resp.StatusCode != http.StatusOK {
			return false, fmt.Sprintf("handshake status %d", resp.StatusCode), nil
		}
		var payload map[string]any
		if err := json.Unmarshal(resp.Body, &payload); err != nil {
			return false, "invalid JSON handshake response", err
		}
		if _, ok := payload["min_cli_version"]; !ok {
			return false, "handshake response missing min_cli_version", nil
		}
		base := strings.TrimSuffix(strings.TrimSpace(cfg.BaseURL), "/")
		if base == "" {
			base = "(base-url)"
		}
		return true, fmt.Sprintf("handshake metadata available (GET %s/meta/handshake; not under /v1/...)", base), nil
	})

	summary := map[string]any{
		"base_url": cfg.BaseURL,
		"agent":    cfg.Agent,
		"checks":   checks,
	}
	textLines := make([]string, 0, len(checks)+1)
	textLines = append(textLines, fmt.Sprintf("Doctor checks for %s", cfg.BaseURL))
	for _, check := range checks {
		state := "PASS"
		if !check.OK {
			state = "FAIL"
		}
		textLines = append(textLines, fmt.Sprintf("[%s] %s (%dms): %s", state, check.Name, check.DurationMS, check.Message))
	}

	result := &commandResult{Data: summary, Text: strings.Join(textLines, "\n")}
	if hasFailure {
		return result, errnorm.WithDetails(errnorm.Local("doctor_failed", "doctor found failing checks"), summary)
	}
	return result, nil
}

func apiCallUsageText() string {
	return strings.TrimSpace(`Local Help: api call

Perform an arbitrary HTTP request against the configured core base URL.

Usage:
  anx api call [--method <method>] [--path <path>] [<method> <path>] [--from-file <file>] [--header key:value] [--raw]

Flags:
  --method <method>     HTTP method (default GET).
  --path <path>         Request path or absolute URL.
  --from-file <path>    Request body from file (stdin otherwise when needed).
  --header key:value    Repeatable request header.
  --raw                 Write raw response body to stdout (not with --json).

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
		rawFlag    trackedBool
		headers    headerList
	)
	fs.Var(&methodFlag, "method", "HTTP method")
	fs.Var(&pathFlag, "path", "Request path or absolute URL")
	fs.Var(&fromFile, "from-file", "Load request body from file path")
	fs.Var(&rawFlag, "raw", "Write raw response body to stdout")
	fs.Var(&headers, "header", "Request header in key:value form (repeatable)")

	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_api_flags", err.Error())
	}

	if rawFlag.value && cfg.JSON {
		return nil, errnorm.Usage("invalid_flag_combination", "--raw cannot be used with --json")
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
		authService := authcli.New(cfg)
		prof, authErr := authService.EnsureAccessToken(ctx)
		if authErr == nil {
			headersMap["Authorization"] = "Bearer " + prof.AccessToken
		} else {
			normalized := errnorm.Normalize(authErr)
			if normalized == nil || normalized.Code != "profile_not_found" {
				return nil, authErr
			}
		}
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
	if rawFlag.value {
		if len(resp.Body) > 0 {
			if _, writeErr := a.Stdout.Write(resp.Body); writeErr != nil {
				return nil, errnorm.Wrap(errnorm.KindLocal, "stdout_write_failed", "failed to write raw response", writeErr)
			}
		}
		if err != nil {
			return &commandResult{RawWritten: true}, errnorm.Wrap(errnorm.KindNetwork, "request_failed", "failed to perform request", err)
		}
		if resp.StatusCode >= http.StatusBadRequest {
			return &commandResult{RawWritten: true}, errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
		}
		return &commandResult{RawWritten: true}, nil
	}
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
	case "/health", "/livez", "/readyz", "/version", "/meta/handshake", "/auth/agents/register", "/auth/token":
		return false
	}
	return true
}
