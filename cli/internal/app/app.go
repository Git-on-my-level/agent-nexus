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
		return a.renderError("root", overrides.JSON != nil && *overrides.JSON, parseErr)
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
		return a.renderError("root", overrides.JSON != nil && *overrides.JSON, errnorm.Wrap(errnorm.KindLocal, "config_resolution_failed", "failed to resolve cli config", err))
	}

	commandName, result, runErr := a.runCommand(context.Background(), remaining, resolved)
	if runErr != nil {
		if result != nil && strings.TrimSpace(result.Text) != "" && !resolved.JSON {
			_, _ = io.WriteString(a.Stderr, result.Text+"\n")
		}
		return a.renderError(commandName, resolved.JSON, runErr)
	}

	if result != nil && result.RawWritten {
		return 0
	}
	if resolved.JSON {
		envelope := output.Envelope{OK: true, Command: commandName, Data: nil}
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

func (a *App) renderError(command string, jsonMode bool, err error) int {
	normalized := errnorm.Normalize(err)
	if jsonMode {
		envelope := output.Envelope{
			OK:      false,
			Command: command,
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
		timeoutFlag trackedDuration
	)
	fs.Var(&jsonFlag, "json", "Emit JSON envelope output")
	fs.Var(&baseURLFlag, "base-url", "Core base URL")
	fs.Var(&agentFlag, "agent", "Agent profile name")
	fs.Var(&noColorFlag, "no-color", "Disable colorized output")
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
	if timeoutFlag.set {
		overrides.Timeout = &timeoutFlag.value
	}
	return overrides, fs.Args(), false, nil
}
