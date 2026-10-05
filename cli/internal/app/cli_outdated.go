package app

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/httpclient"
)

// cliOutdatedRetryEnv marks the single retry after a managed self-update so a
// still-outdated process does not update again.
const cliOutdatedRetryEnv = "ANX_CLI_OUTDATED_RETRY"

var runVerifiedSelfUpdate = func(a *App, ctx context.Context, cfg config.Resolved, version string) (*commandResult, error) {
	return a.performManagedUpdate(ctx, cfg, updateOptions{verb: "now", version: version})
}

var execUpdatedCommand = func(path string, args []string, env []string) (int, error) {
	cmd := exec.Command(path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return 1, err
}

func cliOutdatedUpdateCommand(version string) string {
	version = normalizeReleaseTag(version)
	if version == "" {
		return "anx update"
	}
	return "anx update --version " + version
}

func cliOutdatedRepairVersion(err error) string {
	typed := errnorm.Normalize(err)
	if typed == nil {
		return ""
	}
	details, _ := typed.Details.(map[string]any)
	parsed, _ := details["parsed"].(map[string]any)
	upgrade, _ := parsed["upgrade"].(map[string]any)
	for _, version := range []string{
		anyString(details["recommended_cli_version"]),
		anyString(upgrade["recommended_cli_version"]),
		anyString(details["min_cli_version"]),
		anyString(upgrade["min_cli_version"]),
	} {
		if version = strings.TrimSpace(version); version != "" {
			return version
		}
	}
	return ""
}

func annotateCLIOutdatedCommand(err error) error {
	typed := errnorm.Normalize(err)
	if typed == nil || typed.Code != "cli_outdated" {
		return err
	}
	version := cliOutdatedRepairVersion(typed)
	command := cliOutdatedUpdateCommand(version)
	details, _ := typed.Details.(map[string]any)
	if details == nil {
		details = map[string]any{}
		typed.Details = details
	}
	if version != "" && strings.TrimSpace(anyString(details["recommended_cli_version"])) == "" {
		details["recommended_cli_version"] = normalizeReleaseTag(version)
	}
	if !strings.Contains(typed.Message, command) {
		typed.Message = strings.TrimRight(typed.Message, ".; ") + "; run " + command
	}
	return typed
}

// recoverCLIOutdated updates a managed auto install and retries the original
// command once. notify, off, and unmanaged installs keep the error and name
// the exact anx update command. retried means the updated binary already ran.
func (a *App) recoverCLIOutdated(ctx context.Context, args []string, cfg config.Resolved, command string, runErr error) (int, error, bool) {
	typed := errnorm.Normalize(runErr)
	if typed == nil || typed.Code != "cli_outdated" || strings.HasPrefix(strings.TrimSpace(command), "update") {
		return 0, nil, false
	}
	annotated := annotateCLIOutdatedCommand(typed)
	if strings.TrimSpace(a.Getenv(cliOutdatedRetryEnv)) == "1" {
		return 0, annotated, false
	}
	dir, err := a.updateDirectory(cfg)
	if err != nil {
		return 0, annotated, false
	}
	policy, _, err := a.updatePolicy(dir)
	if err != nil || policy != "auto" {
		return 0, annotated, false
	}
	path, _, _, reason := inspectUpdateInstall()
	if reason != "" {
		return 0, annotated, false
	}
	version := ""
	if candidate := normalizeReleaseTag(cliOutdatedRepairVersion(typed)); candidate != "" {
		if _, parseErr := parseSemanticVersion(candidate); parseErr == nil {
			cmp, cmpErr := compareSemanticVersions(httpclient.CLIVersion, candidate)
			if cmpErr != nil || cmp < 0 {
				version = candidate
			}
		}
	}
	result, updateErr := runVerifiedSelfUpdate(a, ctx, cfg, version)
	if updateErr != nil {
		return 0, updateErr, false
	}
	if path == "" {
		path, _, _, reason = inspectUpdateInstall(false)
		if reason != "" || path == "" {
			return 0, annotated, false
		}
	}
	if result != nil && !asBool(asMap(result.Data)["updated"]) && asBool(asMap(result.Data)["already_current"]) {
		return 0, annotated, false
	}
	code, execErr := execUpdatedCommand(path, args, cliOutdatedRetryEnvList())
	if execErr != nil {
		return 0, errnorm.Wrap(errnorm.KindLocal, "update_retry_failed", "CLI updated but the original command could not be retried; run it again", execErr), false
	}
	return code, nil, true
}

func cliOutdatedRetryEnvList() []string {
	const prefix = cliOutdatedRetryEnv + "="
	env := make([]string, 0, len(os.Environ())+1)
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, prefix) {
			continue
		}
		env = append(env, item)
	}
	return append(env, prefix+"1")
}
