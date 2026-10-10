package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

var pmServiceResolveAuth = func(a *App, ctx context.Context, cfg config.Resolved) (config.Resolved, error) {
	cfg.AccessToken = ""
	cfg.As = firstNonEmpty(cfg.As, "pm")
	return a.resolveHostAgent(ctx, cfg)
}

func (a *App) pmInstallWizard(ctx context.Context, cfg config.Resolved) (config.Resolved, string, error) {
	catalog, _, err := a.workspaceCatalog(cfg)
	if err != nil {
		return cfg, "", err
	}
	input := a.readStdinScanner(64 * 1024)
	choose := func(prompt string, max int) (int, error) {
		fmt.Fprint(a.Stderr, prompt)
		if !input.Scan() {
			return 0, errnorm.Usage("onboarding_cancelled", "PM setup cancelled before installation")
		}
		n, e := strconv.Atoi(strings.TrimSpace(input.Text()))
		if e != nil || n < 1 || n > max {
			return 0, errnorm.Usage("invalid_choice", "choose a numbered option")
		}
		return n, nil
	}
	explicitWorkspace := cfg.Sources["base_url"] == "flag:--base-url" || cfg.Sources["base_url"] == "flag:--workspace"
	if !explicitWorkspace && len(catalog.Workspaces) == 0 {
		return cfg, "", errnorm.Usage("workspace_required", "Enroll this computer first with anx host enroll; then run anx --as pm pm install")
	}
	var n int
	if !explicitWorkspace {
		fmt.Fprintln(a.Stderr, "Set up the PM on this computer. Choose a workspace:")
		for i, ws := range catalog.Workspaces {
			fmt.Fprintf(a.Stderr, "%d. %s (%s)\n", i+1, ws.Alias, ws.BaseURL)
		}
		n, err = choose("Workspace number: ", len(catalog.Workspaces))
		if err != nil {
			return cfg, "", err
		}
		cfg.BaseURL = catalog.Workspaces[n-1].BaseURL
		cfg.Sources["base_url"] = "wizard"

	} else {
		fmt.Fprintf(a.Stderr, "Set up the PM for %s on this computer.\n", cfg.BaseURL)
	}
	cfg.As = firstNonEmpty(cfg.As, "pm")
	fmt.Fprintln(a.Stderr, "1. Hermes\n2. Claude Code\n3. Custom command")
	n, err = choose("Runner number: ", 3)
	if err != nil {
		return cfg, "", err
	}
	runner := "hermes chat --query-file {prompt_file} -Q"
	if n == 2 {
		runner = `sh -c 'exec claude -p < "$1"' sh {prompt_file}`
	}
	if n == 3 {
		fmt.Fprint(a.Stderr, "Runner command (use {prompt_file} for a private file): ")
		if !input.Scan() {
			return cfg, "", errnorm.Usage("onboarding_cancelled", "PM setup cancelled before installation")
		}
		runner = strings.TrimSpace(input.Text())
	}
	argv, err := splitRunnerArgv(runner)
	if err != nil || len(argv) == 0 {
		return cfg, "", errnorm.Usage("invalid_runner", "runner must be a valid command")
	}
	cfg, err = pmServiceResolveAuth(a, ctx, cfg)
	if err != nil {
		return cfg, "", err
	}
	fmt.Fprintln(a.Stderr, "Testing the runner once (up to 120 seconds)…")
	if err = a.testPMRunner(ctx, cfg, argv); err != nil {
		return cfg, "", err
	}
	fmt.Fprintln(a.Stderr, "Runner test passed. Installing the local service and waiting for its first connection…")
	return cfg, runner, nil
}

func (a *App) testPMRunner(ctx context.Context, cfg config.Resolved, argv []string) error {
	dir, err := os.MkdirTemp("", "anx-pm-test-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	prompt := filepath.Join(dir, "prompt.txt")
	if err = writePMPrivateFile(prompt, []byte("Reply exactly: PM runner ready. Do not use tools or read files.")); err != nil {
		return err
	}
	runCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()
	command := expandPromptPlaceholder(argv, prompt)
	if !runnerUsesPromptPlaceholder(argv) {
		binary, e := lookPath("agentctl")
		if e != nil {
			return errnorm.Usage("dependency_unavailable", "agentctl is required for a runner without {prompt_file}")
		}
		command = append([]string{binary, "run", "--prompt-file", prompt, "--"}, argv...)
	}
	previous := harnessLogWriter
	harnessLogWriter = nil
	defer func() { harnessLogWriter = previous }()
	stdout, _, err := runCmd(runCtx, command[0], command[1:], dir, harnessChildEnv(cfg, os.Environ()))
	// Runner output may contain workspace content or secrets. Never echo it into
	// wizard diagnostics; the test has only a bounded, synthetic prompt.
	if err != nil || strings.TrimSpace(assistantTextFromRunnerOutput(stdout)) == "" {
		return errnorm.New(errnorm.KindLocal, "runner_test_failed", "Runner test failed; verify the runner and its local provider credentials before installing")
	}
	return nil
}

func (a *App) waitPMConnection(ctx context.Context, cfg config.Resolved, budget time.Duration, stateDir string, since time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	var err error
	cfg, err = pmServiceResolveAuth(a, ctx, cfg)
	if err != nil {
		return err
	}
	for {
		state, err := a.invokeRawJSON(ctx, cfg, "pm presence", "GET", "/pm/presence", nil)
		body := map[string]any{}
		if err == nil {
			body = asMap(asMap(state.Data)["body"])
		}
		claim, _ := os.ReadFile(filepath.Join(stateDir, "last-claim"))
		claimedAt, _ := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(claim)))
		lastSeen, _ := time.Parse(time.RFC3339Nano, anyString(body["last_seen"]))
		if err == nil && anyString(body["state"]) == "connected" && !claimedAt.Before(since) && !lastSeen.Before(since) {
			return nil
		}
		if ctx.Err() != nil {
			return errnorm.New(errnorm.KindNetwork, "timeout", "Local service installed, but no PM connection was confirmed. Inspect anx --as pm pm status and its logs; do not reinstall blindly")
		}
		if err != nil {
			return err
		}
		if err = sleepCtx(ctx, time.Second); err != nil {
			return errnorm.New(errnorm.KindNetwork, "timeout", "Local service installed, but no PM connection was confirmed. Inspect anx --as pm pm status and its logs")
		}
	}
}

func pmRunnerLabel(argv []string) string {
	if len(argv) == 0 {
		return "custom"
	}
	if len(argv) == 5 && argv[0] == "sh" && argv[1] == "-c" && argv[2] == `exec claude -p < "$1"` && argv[3] == "sh" && argv[4] == "{prompt_file}" {
		return "Claude Code"
	}
	switch filepath.Base(argv[0]) {
	case "hermes":
		return "Hermes"
	case "claude":
		return "Claude Code"
	default:
		return "custom"
	}
}
func safePMConnectionLabel(label, fallback string) string {
	label = strings.TrimSpace(label)
	var out strings.Builder
	for _, r := range label {
		if r > 32 && r < 127 && !strings.ContainsRune("\"'\\/", r) {
			out.WriteRune(r)
		}
	}
	label = out.String()
	if len(label) > 80 {
		label = label[:80]
	}
	if label == "" {
		return fallback
	}
	return label
}
