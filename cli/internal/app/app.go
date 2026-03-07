package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

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
	if helpRequested {
		a.printRootUsage()
		return 0
	}
	if len(remaining) == 0 {
		a.printRootUsage()
		return 0
	}

	resolved, err := config.Resolve(overrides, config.Environment{
		Getenv:      a.Getenv,
		UserHomeDir: a.UserHomeDir,
		ReadFile:    a.ReadFile,
	})
	if err != nil {
		return a.renderError(resolveMachineCommandIdentity("root"), overrides.JSON != nil && *overrides.JSON, errnorm.Wrap(errnorm.KindLocal, "config_resolution_failed", "failed to resolve cli config", err))
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
			envelope.Data = result.Data
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

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return config.Overrides{}, nil, true, nil
		}
		return config.Overrides{}, nil, false, errnorm.Usage("invalid_flags", err.Error())
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
	remaining, err := normalizeTrailingGlobalFlags(fs.Args(), &overrides)
	if err != nil {
		return overrides, nil, false, err
	}
	return overrides, remaining, false, nil
}

var globalFlagValueHints = map[string]string{
	"base-url": "<url>",
	"agent":    "<name>",
	"timeout":  "<duration>",
}

func normalizeTrailingGlobalFlags(args []string, overrides *config.Overrides) ([]string, error) {
	filtered := make([]string, 0, len(args))
	commandPath := commandPathPrefix(args)
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
		case "base-url", "agent", "no-color", "verbose", "headers", "timeout":
			return nil, misplacedGlobalFlagError(name, commandPath)
		default:
			filtered = append(filtered, token)
		}
	}
	return filtered, nil
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

func commandPathPrefix(args []string) string {
	parts := make([]string, 0, len(args))
	for _, token := range args {
		trimmed := strings.TrimSpace(token)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "-") {
			break
		}
		parts = append(parts, trimmed)
	}
	return strings.Join(parts, " ")
}

func misplacedGlobalFlagError(name string, commandPath string) error {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		trimmedName = "flag"
	}

	exampleParts := []string{"oar", "--" + trimmedName}
	if hint, ok := globalFlagValueHints[trimmedName]; ok && strings.TrimSpace(hint) != "" {
		exampleParts = append(exampleParts, hint)
	}
	commandPath = strings.TrimSpace(commandPath)
	if commandPath == "" {
		commandPath = "<command>"
	}
	exampleParts = append(exampleParts, commandPath, "...")
	return errnorm.Usage("invalid_flags", fmt.Sprintf("--%s is a global flag; use: %s", trimmedName, strings.Join(exampleParts, " ")))
}
