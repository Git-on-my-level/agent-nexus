package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"organization-autorunner-cli/internal/config"
	"organization-autorunner-cli/internal/errnorm"
	"organization-autorunner-cli/internal/output"
)

type App struct {
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	Getenv      func(string) string
	UserHomeDir func() (string, error)
	ReadFile    func(string) ([]byte, error)
	StdinIsTTY  func() bool
}

func isAPICallHelpOnly(remaining []string) bool {
	if len(remaining) < 3 || remaining[0] != "api" || remaining[1] != "call" {
		return false
	}
	for _, tok := range remaining[2:] {
		if !isHelpToken(tok) {
			return false
		}
	}
	return true
}

// isTrailingHelpOnlyInvocation is true when the user only wants subcommand help:
// a command prefix with no non-help flags, followed by one or more help tokens
// (for example: inbox ack --help). This avoids requiring a resolvable agent profile
// on hosts with multiple local profiles.
func isTrailingHelpOnlyInvocation(remaining []string) bool {
	if len(remaining) == 0 {
		return false
	}
	end := len(remaining)
	for end > 0 && isHelpToken(remaining[end-1]) {
		end--
	}
	if end == len(remaining) {
		return false
	}
	for _, tok := range remaining[:end] {
		if strings.HasPrefix(tok, "-") && !isHelpToken(tok) {
			return false
		}
	}
	return end > 0
}

func New() *App {
	app := &App{
		Stdin:       os.Stdin,
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		Getenv:      os.Getenv,
		UserHomeDir: os.UserHomeDir,
		ReadFile:    os.ReadFile,
	}
	app.StdinIsTTY = func() bool {
		file, ok := app.Stdin.(*os.File)
		if !ok {
			return false
		}
		info, err := file.Stat()
		if err != nil {
			return false
		}
		return (info.Mode() & os.ModeCharDevice) != 0
	}
	return app
}

func (a *App) Run(args []string) int {
	overrides, remaining, helpRequested, parseErr := parseGlobalFlags(args)
	if parseErr != nil {
		return a.renderError(resolveMachineCommandIdentity("root"), overrides.JSON != nil && *overrides.JSON, parseErr)
	}

	jsonMode := overrides.JSON != nil && *overrides.JSON

	if helpRequested || len(remaining) == 0 {
		text := a.rootUsageText()
		if jsonMode {
			envelope := output.Envelope{OK: true, Command: "help", Data: map[string]any{"help_text": text}}
			_ = output.WriteEnvelopeJSON(a.Stdout, envelope)
		} else {
			_, _ = io.WriteString(a.Stdout, text)
		}
		return 0
	}

	cmdPeek := remaining[0]
	subPeek := ""
	if len(remaining) > 1 {
		subPeek = remaining[1]
	}
	configLenient := cmdPeek == "version" || cmdPeek == "help" || cmdPeek == "--help" || cmdPeek == "-h" || cmdPeek == "meta" || cmdPeek == "update" || cmdPeek == "bridge" || cmdPeek == "concepts" || cmdPeek == "primitives" ||
		(cmdPeek == "import" && isConfigLenientImportCommand(remaining[1:])) ||
		(cmdPeek == "auth" && (subPeek == "list" || subPeek == "ls" || subPeek == "profiles" || subPeek == "default")) ||
		isAPICallHelpOnly(remaining) ||
		isTrailingHelpOnlyInvocation(remaining)

	resolved, err := config.Resolve(overrides, config.Environment{
		Getenv:      a.Getenv,
		UserHomeDir: a.UserHomeDir,
		ReadFile:    a.ReadFile,
	})
	if err != nil {
		if configLenient {
			resolved = config.Defaults(overrides)
		} else {
			details := map[string]any{"cause": err.Error()}
			if strings.Contains(err.Error(), "multiple local profiles") {
				details["reason"] = "ambiguous_agent_profile"
			}
			wrapped := errnorm.WithDetails(errnorm.Wrap(errnorm.KindLocal, "config_resolution_failed", "failed to resolve cli config", err), details)
			return a.renderError(resolveMachineCommandIdentity("root"), jsonMode, wrapped)
		}
	}

	commandName, result, runErr := a.runCommand(context.Background(), remaining, resolved)
	identity := resolveMachineCommandIdentity(commandName)
	if runErr != nil {
		if result != nil && strings.TrimSpace(result.Text) != "" && !resolved.JSON {
			_, _ = io.WriteString(a.Stderr, result.Text+"\n")
		}
		return a.renderError(identity, resolved.JSON, runErr)
	}

	if result != nil && result.RawWritten {
		return 0
	}
	if resolved.JSON {
		envelope := output.Envelope{OK: true, Command: identity.Command, CommandID: identity.CommandID, Data: nil}
		if result != nil {
			envelope.Data = flattenEnvelopeData(result.Data, resolved.Headers || resolved.Verbose)
		}
		if err := output.WriteEnvelopeJSON(a.Stdout, envelope); err != nil {
			_, _ = io.WriteString(a.Stderr, "failed to write JSON envelope: "+err.Error()+"\n")
			return 1
		}
		return 0
	}

	if result != nil && strings.TrimSpace(result.Text) != "" {
		_, _ = io.WriteString(a.Stdout, result.Text+"\n")
	}
	return 0
}

type commandResult struct {
	Data       any
	Text       string
	RawWritten bool
}

func (a *App) renderError(identity machineCommandIdentity, jsonMode bool, err error) int {
	normalized := errnorm.Normalize(err)
	if jsonMode {
		envelope := output.Envelope{
			OK:        false,
			Command:   identity.Command,
			CommandID: identity.CommandID,
			Error: &output.ErrorPayload{
				Code:        normalized.Code,
				Message:     normalized.Message,
				Recoverable: errnorm.RecoverableValue(normalized),
				Hint:        normalized.Hint,
				Details:     normalized.Details,
			},
		}
		_ = output.WriteEnvelopeJSON(a.Stdout, envelope)
	} else {
		_, _ = io.WriteString(a.Stderr, fmt.Sprintf("Error (%s): %s\n", normalized.Code, normalized.Message))
	}
	return errnorm.ExitCode(err)
}

func parseGlobalFlags(args []string) (config.Overrides, []string, bool, error) {
	fs := newSilentFlagSet("oar")
	var (
		jsonFlag    trackedBool
		baseURLFlag trackedString
		agentFlag   trackedString
		noColorFlag trackedBool
		verboseFlag trackedBool
		headersFlag trackedBool
		timeoutFlag trackedDuration
	)
	fs.Var(&jsonFlag, "json", "Emit JSON envelope output")
	fs.Var(&baseURLFlag, "base-url", "Core base URL")
	fs.Var(&agentFlag, "agent", "Agent profile name")
	fs.Var(&noColorFlag, "no-color", "Disable colorized output")
	fs.Var(&verboseFlag, "verbose", "Show the full response payload for human-readable commands")
	fs.Var(&headersFlag, "headers", "Include response status and headers in human-readable output")
	fs.Var(&timeoutFlag, "timeout", "HTTP timeout duration")

	var helpRequested bool
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			helpRequested = true
		} else {
			return config.Overrides{}, nil, false, errnorm.Usage("invalid_flags", err.Error())
		}
	}
	overrides := config.Overrides{}
	if jsonFlag.set {
		overrides.JSON = &jsonFlag.value
	}
	if baseURLFlag.set {
		overrides.BaseURL = &baseURLFlag.value
	}
	if agentFlag.set {
		overrides.Agent = &agentFlag.value
	}
	if noColorFlag.set {
		overrides.NoColor = &noColorFlag.value
	}
	if verboseFlag.set {
		overrides.Verbose = &verboseFlag.value
	}
	if headersFlag.set {
		overrides.Headers = &headersFlag.value
	}
	if timeoutFlag.set {
		overrides.Timeout = &timeoutFlag.value
	}
	captureTrailingJSONOverride(fs.Args(), &overrides)
	remaining, err := normalizeTrailingGlobalFlags(fs.Args(), &overrides)
	if err != nil {
		return overrides, nil, false, err
	}
	return overrides, remaining, helpRequested, nil
}

func captureTrailingJSONOverride(args []string, overrides *config.Overrides) {
	for _, token := range args {
		if strings.TrimSpace(token) == "--" {
			break
		}
		name, value, hasValue, isFlag := parseLongOptionToken(token)
		if !isFlag || name != "json" {
			continue
		}
		jsonValue := true
		if hasValue {
			parsed, err := strconvParseBool(value)
			if err != nil {
				continue
			}
			jsonValue = parsed
		}
		overrides.JSON = &jsonValue
	}
}

func normalizeTrailingGlobalFlags(args []string, overrides *config.Overrides) ([]string, error) {
	filtered := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		token := args[i]
		if strings.TrimSpace(token) == "--" {
			filtered = append(filtered, args[i:]...)
			break
		}
		name, value, hasValue, isFlag := parseLongOptionToken(token)
		if !isFlag {
			filtered = append(filtered, token)
			continue
		}

		readValue := func(flagName string) (string, error) {
			if hasValue {
				return value, nil
			}
			if i+1 >= len(args) {
				return "", errnorm.Usage("invalid_flags", fmt.Sprintf("--%s requires a value", flagName))
			}
			next := strings.TrimSpace(args[i+1])
			if next == "" || next == "--" || strings.HasPrefix(next, "-") {
				return "", errnorm.Usage("invalid_flags", fmt.Sprintf("--%s requires a value", flagName))
			}
			i++
			return next, nil
		}

		switch name {
		case "json":
			jsonValue := true
			if hasValue {
				parsed, err := strconvParseBool(value)
				if err != nil {
					return nil, errnorm.Usage("invalid_flags", fmt.Sprintf("invalid value for --json: %s", err.Error()))
				}
				jsonValue = parsed
			}
			overrides.JSON = &jsonValue
		case "base-url":
			rawValue, err := readValue(name)
			if err != nil {
				return nil, err
			}
			parsed := strings.TrimSpace(rawValue)
			overrides.BaseURL = &parsed
		case "agent":
			rawValue, err := readValue(name)
			if err != nil {
				return nil, err
			}
			parsed := strings.TrimSpace(rawValue)
			overrides.Agent = &parsed
		case "timeout":
			rawValue, err := readValue(name)
			if err != nil {
				return nil, err
			}
			parsed, err := time.ParseDuration(strings.TrimSpace(rawValue))
			if err != nil {
				return nil, errnorm.Usage("invalid_flags", fmt.Sprintf("invalid value for --timeout: %s", err.Error()))
			}
			overrides.Timeout = &parsed
		case "no-color", "verbose", "headers":
			flagValue := true
			if hasValue {
				parsed, err := strconvParseBool(value)
				if err != nil {
					return nil, errnorm.Usage("invalid_flags", fmt.Sprintf("invalid value for --%s: %s", name, err.Error()))
				}
				flagValue = parsed
			}
			switch name {
			case "no-color":
				overrides.NoColor = &flagValue
			case "verbose":
				overrides.Verbose = &flagValue
			case "headers":
				overrides.Headers = &flagValue
			}
		default:
			filtered = append(filtered, token)
		}
	}
	return filtered, nil
}

func flattenEnvelopeData(data any, includeTransport bool) any {
	m, ok := data.(map[string]any)
	if !ok {
		return data
	}
	body, hasBody := m["body"]
	_, hasStatusCode := m["status_code"]
	_, hasHeaders := m["headers"]
	if !hasBody || !hasStatusCode || !hasHeaders {
		return data
	}
	out := make(map[string]any, len(m))
	if bodyMap, ok := body.(map[string]any); ok {
		for k, v := range bodyMap {
			out[k] = v
		}
	} else {
		out["body"] = body
	}
	if includeTransport {
		out["status_code"] = m["status_code"]
		out["headers"] = m["headers"]
	}
	for k, v := range m {
		if k == "body" || k == "status_code" || k == "headers" {
			continue
		}
		out[k] = v
	}
	return out
}

func parseLongOptionToken(token string) (name string, value string, hasValue bool, isFlag bool) {
	token = strings.TrimSpace(token)
	if token == "" || token == "-" || token == "--" {
		return "", "", false, false
	}
	if strings.HasPrefix(token, "--") {
		token = token[2:]
	} else if strings.HasPrefix(token, "-") {
		token = token[1:]
	} else {
		return "", "", false, false
	}
	if token == "" {
		return "", "", false, false
	}
	if idx := strings.IndexRune(token, '='); idx >= 0 {
		return strings.TrimSpace(token[:idx]), token[idx+1:], true, true
	}
	return strings.TrimSpace(token), "", false, true
}

func isConfigLenientImportCommand(args []string) bool {
	if len(args) == 0 {
		return true
	}
	for _, arg := range args {
		if isHelpToken(arg) {
			return true
		}
	}
	if importSubcommandSpec.normalize(args[0]) != "apply" {
		return true
	}
	return !hasTrueBoolFlag(args[1:], "execute")
}

func hasTrueBoolFlag(args []string, flagName string) bool {
	for _, arg := range args {
		name, value, hasValue, isFlag := parseLongOptionToken(arg)
		if !isFlag || name != flagName {
			continue
		}
		if !hasValue {
			return true
		}
		parsed, err := strconvParseBool(value)
		if err != nil {
			return true
		}
		return parsed
	}
	return false
}
