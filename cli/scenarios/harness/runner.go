package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var templateTokenPattern = regexp.MustCompile(`\{\{([a-zA-Z0-9_.-]+)\}\}`)

func LoadScenario(path string) (Scenario, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return Scenario{}, fmt.Errorf("scenario path is required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Scenario{}, fmt.Errorf("read scenario: %w", err)
	}
	var scenario Scenario
	if err := json.Unmarshal(raw, &scenario); err != nil {
		return Scenario{}, fmt.Errorf("decode scenario: %w", err)
	}
	if strings.TrimSpace(scenario.Name) == "" {
		return Scenario{}, fmt.Errorf("scenario.name is required")
	}
	if len(scenario.Agents) == 0 {
		return Scenario{}, fmt.Errorf("scenario.agents must not be empty")
	}
	return scenario, nil
}

func Run(ctx context.Context, cfg Config) (Report, error) {
	scenario, err := LoadScenario(cfg.ScenarioPath)
	if err != nil {
		return Report{}, err
	}
	if err := validateScenario(scenario); err != nil {
		return Report{}, fmt.Errorf("scenario validation failed: %w", err)
	}
	mode := cfg.Mode
	if mode == "" {
		mode = ModeDeterministic
	}
	if mode != ModeDeterministic && mode != ModeLLM {
		return Report{}, fmt.Errorf("unsupported mode %q", mode)
	}

	baseURL := strings.TrimSpace(cfg.BaseURLOverride)
	if baseURL == "" {
		baseURL = strings.TrimSpace(scenario.BaseURL)
	}
	if baseURL == "" {
		return Report{}, fmt.Errorf("base URL is required (scenario.base_url or --base-url)")
	}
	oarBinary := strings.TrimSpace(cfg.OARBinary)
	if oarBinary == "" {
		oarBinary = "./oar"
	}

	runID := time.Now().UTC().Format("20060102T150405.000000000")
	report := Report{
		Scenario:  scenario.Name,
		Mode:      mode,
		RunID:     runID,
		StartedAt: time.Now().UTC(),
		BaseURL:   baseURL,
		Agents:    make([]AgentReport, 0, len(scenario.Agents)),
		Captures:  make(map[string]map[string]any, len(scenario.Agents)+1),
	}
	report.Captures["run"] = map[string]any{"id": runID}

	captures := report.Captures
	agentUsers := make(map[string]string, len(scenario.Agents))
	history := make(map[string][]CommandResult, len(scenario.Agents))

	for _, agent := range scenario.Agents {
		agentName := strings.TrimSpace(agent.Name)
		if agentName == "" {
			return failReport(report, "agent.name is required")
		}
		if _, exists := captures[agentName]; exists {
			return failReport(report, fmt.Sprintf("duplicate agent name %q", agentName))
		}

		prefix := strings.TrimSpace(agent.UsernamePrefix)
		if prefix == "" {
			prefix = agentName
		}
		username := fmt.Sprintf("%s-%s", sanitizeToken(prefix), shortRunToken(runID))
		agentUsers[agentName] = username
		captures[agentName] = map[string]any{"username": username}

		aReport := AgentReport{Name: agentName, Username: username, Mode: mode, Steps: make([]CommandResult, 0, 8)}

		register := Step{
			Name: "auth register",
			Args: []string{"auth", "register", "--username", username},
		}
		res, execErr := runStep(ctx, cfg, oarBinary, baseURL, username, agentName, register, captures)
		aReport.Steps = append(aReport.Steps, res)
		history[agentName] = append(history[agentName], res)
		if execErr != nil {
			report.Agents = append(report.Agents, aReport)
			return failReport(report, fmt.Sprintf("agent %s register failed: %v", agentName, execErr))
		}

		switch mode {
		case ModeDeterministic:
			for _, step := range agent.DeterministicSteps {
				res, stepErr := runStep(ctx, cfg, oarBinary, baseURL, username, agentName, step, captures)
				aReport.Steps = append(aReport.Steps, res)
				history[agentName] = append(history[agentName], res)
				if stepErr != nil {
					report.Agents = append(report.Agents, aReport)
					return failReport(report, fmt.Sprintf("agent %s step %q failed: %v", agentName, step.Name, stepErr))
				}
			}
		case ModeLLM:
			maxTurns := agent.LLM.MaxTurns
			if maxTurns <= 0 {
				maxTurns = 8
			}
			for turn := 1; turn <= maxTurns; turn++ {
				action, actionErr := nextLLMAction(ctx, cfg, scenario, agent, captures, history[agentName], turn, maxTurns, baseURL)
				if actionErr != nil {
					report.Agents = append(report.Agents, aReport)
					return failReport(report, fmt.Sprintf("agent %s turn %d driver failed: %v", agentName, turn, actionErr))
				}
				if strings.EqualFold(strings.TrimSpace(action.Action), "stop") {
					stopRes := CommandResult{
						Name:      fmt.Sprintf("llm stop (turn %d)", turn),
						Agent:     agentName,
						Args:      []string{},
						ExitCode:  0,
						Succeeded: true,
						Stdout:    strings.TrimSpace(action.Reason),
					}
					aReport.Steps = append(aReport.Steps, stopRes)
					history[agentName] = append(history[agentName], stopRes)
					break
				}

				step := Step{
					Name:         firstNonEmpty(strings.TrimSpace(action.Name), fmt.Sprintf("llm turn %d", turn)),
					Args:         action.Args,
					Stdin:        action.Stdin,
					AllowFailure: action.AllowFailure,
				}
				res, stepErr := runStep(ctx, cfg, oarBinary, baseURL, username, agentName, step, captures)
				aReport.Steps = append(aReport.Steps, res)
				history[agentName] = append(history[agentName], res)
				if stepErr != nil {
					report.Agents = append(report.Agents, aReport)
					return failReport(report, fmt.Sprintf("agent %s llm step %q failed: %v", agentName, step.Name, stepErr))
				}
			}
		}

		report.Agents = append(report.Agents, aReport)
	}

	assertions, assertErr := runAssertions(ctx, cfg, oarBinary, baseURL, scenario.Assertions, captures, agentUsers)
	report.Assertions = assertions
	if assertErr != nil {
		return failReport(report, assertErr.Error())
	}

	report.CompletedAt = time.Now().UTC()
	return report, nil
}

func runAssertions(ctx context.Context, cfg Config, oarBinary string, baseURL string, assertions []Assertion, captures map[string]map[string]any, agentUsers map[string]string) ([]AssertionResult, error) {
	results := make([]AssertionResult, 0, len(assertions))
	for _, assertion := range assertions {
		assertName := strings.TrimSpace(assertion.Name)
		if assertName == "" {
			assertName = "unnamed assertion"
		}
		agentName := strings.TrimSpace(assertion.Agent)
		username, exists := agentUsers[agentName]
		if !exists {
			result := AssertionResult{Name: assertName, Passed: false, Details: fmt.Sprintf("unknown assertion agent %q", agentName)}
			results = append(results, result)
			return results, errors.New(result.Details)
		}

		step := Step{Name: assertName, Args: assertion.Args, Stdin: assertion.Stdin}
		res, err := runStep(ctx, cfg, oarBinary, baseURL, username, agentName, step, captures)
		if err != nil {
			result := AssertionResult{Name: assertName, Passed: false, Details: err.Error(), Command: strings.Join(res.Args, " "), ExitCode: res.ExitCode}
			results = append(results, result)
			return results, fmt.Errorf("assertion %q failed: %w", assertName, err)
		}

		containsChecks := make([]string, 0, len(assertion.Contains))
		for _, entry := range assertion.Contains {
			resolved, resolveErr := interpolateString(entry, captures)
			if resolveErr != nil {
				result := AssertionResult{Name: assertName, Passed: false, Details: resolveErr.Error(), Command: strings.Join(res.Args, " "), ExitCode: res.ExitCode}
				results = append(results, result)
				return results, fmt.Errorf("assertion %q contains template: %w", assertName, resolveErr)
			}
			containsChecks = append(containsChecks, resolved)
		}
		for _, expected := range containsChecks {
			if !strings.Contains(res.Stdout, expected) {
				details := fmt.Sprintf("stdout does not contain %q", expected)
				result := AssertionResult{Name: assertName, Passed: false, Details: details, Command: strings.Join(res.Args, " "), ExitCode: res.ExitCode}
				results = append(results, result)
				return results, fmt.Errorf("assertion %q failed: %s", assertName, details)
			}
		}

		if len(assertion.JSONPaths) > 0 {
			var payload map[string]any
			if err := json.Unmarshal([]byte(strings.TrimSpace(res.Stdout)), &payload); err != nil {
				details := fmt.Sprintf("assertion requires JSON output but parse failed: %v", err)
				result := AssertionResult{Name: assertName, Passed: false, Details: details, Command: strings.Join(res.Args, " "), ExitCode: res.ExitCode}
				results = append(results, result)
				return results, fmt.Errorf("assertion %q failed: %s", assertName, details)
			}

			keys := make([]string, 0, len(assertion.JSONPaths))
			for key := range assertion.JSONPaths {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				expected := fmt.Sprint(assertion.JSONPaths[key])
				expectedResolved, resolveErr := interpolateString(expected, captures)
				if resolveErr != nil {
					result := AssertionResult{Name: assertName, Passed: false, Details: resolveErr.Error(), Command: strings.Join(res.Args, " "), ExitCode: res.ExitCode}
					results = append(results, result)
					return results, fmt.Errorf("assertion %q json path template: %w", assertName, resolveErr)
				}
				actual, ok := getPathValue(payload, key)
				if !ok {
					details := fmt.Sprintf("json path %q not found", key)
					result := AssertionResult{Name: assertName, Passed: false, Details: details, Command: strings.Join(res.Args, " "), ExitCode: res.ExitCode}
					results = append(results, result)
					return results, fmt.Errorf("assertion %q failed: %s", assertName, details)
				}
				if fmt.Sprint(actual) != expectedResolved {
					details := fmt.Sprintf("json path %q mismatch: got %q want %q", key, fmt.Sprint(actual), expectedResolved)
					result := AssertionResult{Name: assertName, Passed: false, Details: details, Command: strings.Join(res.Args, " "), ExitCode: res.ExitCode}
					results = append(results, result)
					return results, fmt.Errorf("assertion %q failed: %s", assertName, details)
				}
			}
		}

		results = append(results, AssertionResult{Name: assertName, Passed: true, Command: strings.Join(res.Args, " "), ExitCode: res.ExitCode})
	}
	return results, nil
}

func runStep(ctx context.Context, cfg Config, oarBinary string, baseURL string, username string, agentName string, step Step, captures map[string]map[string]any) (CommandResult, error) {
	name := strings.TrimSpace(step.Name)
	if name == "" {
		name = strings.Join(step.Args, " ")
	}

	args, stdin, err := materializeStep(step, captures)
	if err != nil {
		return CommandResult{Name: name, Agent: agentName, Args: append([]string(nil), args...), Stdin: stdin, ExitCode: 2, Succeeded: false}, err
	}
	if len(args) == 0 {
		return CommandResult{Name: name, Agent: agentName, Args: args, Stdin: stdin, ExitCode: 2, Succeeded: false}, fmt.Errorf("step %q has no args", name)
	}

	allArgs := make([]string, 0, len(args)+6)
	allArgs = append(allArgs, "--json", "--base-url", baseURL, "--agent", username)
	allArgs = append(allArgs, args...)

	var stdinBytes []byte
	if len(stdin) > 0 {
		stdinBytes, err = json.Marshal(stdin)
		if err != nil {
			return CommandResult{Name: name, Agent: agentName, Args: allArgs, Stdin: stdin, ExitCode: 2, Succeeded: false}, fmt.Errorf("encode step stdin: %w", err)
		}
	}

	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "[harness] %s (%s): %s\n", agentName, name, strings.Join(allArgs, " "))
	}

	cmd := exec.CommandContext(ctx, oarBinary, allArgs...)
	if strings.TrimSpace(cfg.WorkingDirectory) != "" {
		cmd.Dir = cfg.WorkingDirectory
	}
	cmd.Stderr = new(bytes.Buffer)
	cmd.Stdout = new(bytes.Buffer)
	if len(stdinBytes) > 0 {
		cmd.Stdin = bytes.NewReader(stdinBytes)
	}

	runErr := cmd.Run()
	stdout := cmd.Stdout.(*bytes.Buffer).String()
	stderr := cmd.Stderr.(*bytes.Buffer).String()
	exitCode := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	result := CommandResult{
		Name:      name,
		Agent:     agentName,
		Args:      allArgs,
		Stdin:     stdin,
		ExitCode:  exitCode,
		Succeeded: runErr == nil,
		Stdout:    stdout,
		Stderr:    stderr,
	}

	envelope, hasEnvelope := parseJSONEnvelope(stdout)
	reportedFailure := false
	if hasEnvelope {
		if okValue, ok := envelope["ok"].(bool); ok && !okValue {
			reportedFailure = true
		}
	}
	failed := runErr != nil || reportedFailure

	if step.ExpectError != nil {
		if err := verifyExpectedError(*step.ExpectError, result, failed, envelope, hasEnvelope); err != nil {
			return result, err
		}
	} else if runErr != nil && !step.AllowFailure {
		return result, fmt.Errorf("command failed (exit=%d): %s", exitCode, strings.TrimSpace(firstNonEmpty(stderr, stdout, runErr.Error())))
	} else if runErr == nil && reportedFailure && !step.AllowFailure {
		return result, fmt.Errorf("command returned ok=false")
	}

	if len(step.Capture) > 0 {
		if err := applyCaptures(step.Capture, result.Stdout, agentName, captures); err != nil {
			return result, err
		}
	}

	return result, nil
}

func parseJSONEnvelope(stdout string) (map[string]any, bool) {
	var envelope map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &envelope); err != nil {
		return nil, false
	}
	return envelope, true
}

func verifyExpectedError(expect ExpectedError, result CommandResult, failed bool, envelope map[string]any, hasEnvelope bool) error {
	if !failed {
		return fmt.Errorf("expected command failure, but command succeeded")
	}
	if expect.ExitCode != nil && result.ExitCode != *expect.ExitCode {
		return fmt.Errorf("expected error exit_code=%d, got %d", *expect.ExitCode, result.ExitCode)
	}
	if strings.TrimSpace(expect.Code) != "" {
		actualCode := ""
		if hasEnvelope {
			if value, ok := getPathValue(envelope, "error.code"); ok {
				actualCode = strings.TrimSpace(fmt.Sprint(value))
			}
		}
		if actualCode == "" {
			return fmt.Errorf("expected error code %q, but response had no error.code", expect.Code)
		}
		if actualCode != strings.TrimSpace(expect.Code) {
			return fmt.Errorf("expected error code %q, got %q", expect.Code, actualCode)
		}
	}
	if expect.Status != nil {
		actualStatus, ok := parseEnvelopeStatus(envelope, hasEnvelope)
		if !ok {
			return fmt.Errorf("expected error status=%d, but response had no error.details.status", *expect.Status)
		}
		if actualStatus != *expect.Status {
			return fmt.Errorf("expected error status=%d, got %d", *expect.Status, actualStatus)
		}
	}
	if strings.TrimSpace(expect.MessageContains) != "" {
		actualMessage := ""
		if hasEnvelope {
			if value, ok := getPathValue(envelope, "error.message"); ok {
				actualMessage = fmt.Sprint(value)
			}
		}
		if !strings.Contains(actualMessage, expect.MessageContains) {
			return fmt.Errorf("expected error.message to contain %q, got %q", expect.MessageContains, actualMessage)
		}
	}
	return nil
}

func parseEnvelopeStatus(envelope map[string]any, hasEnvelope bool) (int, bool) {
	if !hasEnvelope {
		return 0, false
	}
	value, ok := getPathValue(envelope, "error.details.status")
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case int:
		return typed, true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	default:
		return 0, false
	}
}

func validateScenario(scenario Scenario) error {
	seenAgents := make(map[string]struct{}, len(scenario.Agents))
	for agentIdx, agent := range scenario.Agents {
		agentName := strings.TrimSpace(agent.Name)
		if agentName == "" {
			return fmt.Errorf("agents[%d].name is required", agentIdx)
		}
		if _, exists := seenAgents[agentName]; exists {
			return fmt.Errorf("duplicate agent name %q", agentName)
		}
		seenAgents[agentName] = struct{}{}

		for stepIdx, step := range agent.DeterministicSteps {
			if len(step.Args) == 0 {
				return fmt.Errorf("agents[%d].deterministic_steps[%d]: args are required", agentIdx, stepIdx)
			}
			if step.AllowFailure && step.ExpectError != nil {
				return fmt.Errorf("agents[%d].deterministic_steps[%d]: allow_failure and expect_error are mutually exclusive", agentIdx, stepIdx)
			}
			if step.ExpectError != nil {
				if strings.TrimSpace(step.ExpectError.Code) == "" &&
					strings.TrimSpace(step.ExpectError.MessageContains) == "" &&
					step.ExpectError.Status == nil &&
					step.ExpectError.ExitCode == nil {
					return fmt.Errorf("agents[%d].deterministic_steps[%d]: expect_error must set at least one matcher", agentIdx, stepIdx)
				}
			}
			if err := validateKnownEventRefConstraints(agentIdx, stepIdx, step); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateKnownEventRefConstraints(agentIdx int, stepIdx int, step Step) error {
	if len(step.Args) < 2 {
		return nil
	}
	command := strings.ToLower(strings.TrimSpace(step.Args[0]))
	subcommand := strings.ToLower(strings.TrimSpace(step.Args[1]))
	if command != "events" || subcommand != "create" {
		return nil
	}
	eventValue, ok := step.Stdin["event"]
	if !ok {
		return nil
	}
	event, ok := eventValue.(map[string]any)
	if !ok {
		return nil
	}
	eventType := strings.TrimSpace(fmt.Sprint(event["type"]))
	if eventType != "review_completed" {
		return nil
	}
	refsValue, hasRefs := event["refs"]
	if !hasRefs {
		return fmt.Errorf("agents[%d].deterministic_steps[%d]: event.type=review_completed requires event.refs", agentIdx, stepIdx)
	}
	refs, ok := refsValue.([]any)
	if !ok {
		return fmt.Errorf("agents[%d].deterministic_steps[%d]: event.refs must be an array for review_completed", agentIdx, stepIdx)
	}
	artifactRefCount := 0
	hasDynamicRef := false
	for _, ref := range refs {
		refValue := strings.TrimSpace(fmt.Sprint(ref))
		if strings.HasPrefix(refValue, "artifact:") {
			artifactRefCount++
		}
		if strings.Contains(refValue, "{{") {
			hasDynamicRef = true
		}
	}
	if artifactRefCount < 3 && !hasDynamicRef {
		return fmt.Errorf("agents[%d].deterministic_steps[%d]: event.type=review_completed requires at least 3 artifact:* refs (found %d)", agentIdx, stepIdx, artifactRefCount)
	}
	return nil
}

func applyCaptures(capture map[string]any, stdout string, agentName string, captures map[string]map[string]any) error {
	var payload map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &payload); err != nil {
		return fmt.Errorf("capture requires JSON output: %w", err)
	}
	if captures[agentName] == nil {
		captures[agentName] = map[string]any{}
	}
	keys := make([]string, 0, len(capture))
	for key := range capture {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		path := strings.TrimSpace(fmt.Sprint(capture[key]))
		if path == "" {
			return fmt.Errorf("capture path for %q is empty", key)
		}
		value, ok := getPathValue(payload, path)
		if !ok {
			return fmt.Errorf("capture path %q not found", path)
		}
		captures[agentName][key] = value
	}
	return nil
}

func materializeStep(step Step, captures map[string]map[string]any) ([]string, map[string]any, error) {
	args := make([]string, 0, len(step.Args))
	for _, arg := range step.Args {
		resolved, err := interpolateString(arg, captures)
		if err != nil {
			return nil, nil, err
		}
		args = append(args, resolved)
	}
	stdin, err := materializeMap(step.Stdin, captures)
	if err != nil {
		return nil, nil, err
	}
	return args, stdin, nil
}

func materializeMap(input map[string]any, captures map[string]map[string]any) (map[string]any, error) {
	if len(input) == 0 {
		return map[string]any{}, nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		resolved, err := materializeValue(value, captures)
		if err != nil {
			return nil, err
		}
		out[key] = resolved
	}
	return out, nil
}

func materializeValue(input any, captures map[string]map[string]any) (any, error) {
	switch value := input.(type) {
	case string:
		return interpolateString(value, captures)
	case []any:
		out := make([]any, 0, len(value))
		for _, item := range value {
			resolved, err := materializeValue(item, captures)
			if err != nil {
				return nil, err
			}
			out = append(out, resolved)
		}
		return out, nil
	case map[string]any:
		return materializeMap(value, captures)
	default:
		return input, nil
	}
}

func interpolateString(input string, captures map[string]map[string]any) (string, error) {
	matches := templateTokenPattern.FindAllStringSubmatch(input, -1)
	if len(matches) == 0 {
		return input, nil
	}
	resolved := input
	for _, match := range matches {
		token := strings.TrimSpace(match[1])
		parts := strings.Split(token, ".")
		if len(parts) < 2 {
			return "", fmt.Errorf("template token %q is invalid", token)
		}
		agent := parts[0]
		path := strings.Join(parts[1:], ".")
		agentMap, exists := captures[agent]
		if !exists {
			return "", fmt.Errorf("template token %q references unknown scope", token)
		}
		value, ok := getPathValue(agentMap, path)
		if !ok {
			return "", fmt.Errorf("template token %q path not found", token)
		}
		resolved = strings.ReplaceAll(resolved, match[0], fmt.Sprint(value))
	}
	return resolved, nil
}

func getPathValue(root any, path string) (any, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return root, true
	}
	parts := strings.Split(path, ".")
	current := root
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, false
		}
		next, ok := navigatePath(current, part)
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func navigatePath(value any, key string) (any, bool) {
	switch typed := value.(type) {
	case map[string]any:
		next, ok := typed[key]
		return next, ok
	case map[string]string:
		next, ok := typed[key]
		if !ok {
			return nil, false
		}
		return next, true
	default:
		return nil, false
	}
}

func nextLLMAction(ctx context.Context, cfg Config, scenario Scenario, agent AgentSpec, captures map[string]map[string]any, history []CommandResult, turn int, maxTurns int, baseURL string) (DriverAction, error) {
	if strings.TrimSpace(cfg.LLMDriverBin) == "" {
		return DriverAction{}, fmt.Errorf("llm mode requires --llm-driver-bin")
	}
	profileText := ""
	if strings.TrimSpace(agent.LLM.ProfilePath) != "" {
		profilePath := strings.TrimSpace(agent.LLM.ProfilePath)
		if !filepath.IsAbs(profilePath) && strings.TrimSpace(cfg.WorkingDirectory) != "" {
			profilePath = filepath.Join(cfg.WorkingDirectory, profilePath)
		}
		if raw, err := os.ReadFile(profilePath); err == nil {
			profileText = string(raw)
		}
	}

	req := DriverRequest{
		Scenario:  scenario.Name,
		RunID:     fmt.Sprint(captures["run"]["id"]),
		Agent:     agent.Name,
		Objective: strings.TrimSpace(agent.LLM.Objective),
		Profile:   profileText,
		Turn:      turn,
		MaxTurns:  maxTurns,
		Captures:  captures,
		History:   history,
		BaseURL:   baseURL,
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return DriverAction{}, fmt.Errorf("encode driver request: %w", err)
	}

	cmdArgs := append([]string{}, cfg.LLMDriverArgs...)
	cmd := exec.CommandContext(ctx, cfg.LLMDriverBin, cmdArgs...)
	if strings.TrimSpace(cfg.WorkingDirectory) != "" {
		cmd.Dir = cfg.WorkingDirectory
	}
	cmd.Stdin = bytes.NewReader(payload)
	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return DriverAction{}, fmt.Errorf("driver failed: %s", strings.TrimSpace(firstNonEmpty(stderr.String(), err.Error())))
	}

	var action DriverAction
	if err := json.Unmarshal(stdout.Bytes(), &action); err != nil {
		return DriverAction{}, fmt.Errorf("decode driver action: %w", err)
	}
	action.Action = strings.ToLower(strings.TrimSpace(action.Action))
	if action.Action == "" {
		return DriverAction{}, fmt.Errorf("driver action is required")
	}
	if action.Action != "run" && action.Action != "stop" {
		return DriverAction{}, fmt.Errorf("unsupported driver action %q", action.Action)
	}
	if action.Action == "run" && len(action.Args) == 0 {
		return DriverAction{}, fmt.Errorf("driver action run requires args")
	}
	return action, nil
}

func failReport(report Report, reason string) (Report, error) {
	report.Failed = true
	report.FailureReason = strings.TrimSpace(reason)
	report.CompletedAt = time.Now().UTC()
	return report, errors.New(strings.TrimSpace(reason))
}

func shortRunToken(runID string) string {
	clean := strings.NewReplacer(".", "", ":", "", "-", "", "T", "", "Z", "").Replace(runID)
	if len(clean) > 12 {
		return clean[len(clean)-12:]
	}
	return clean
}

func sanitizeToken(input string) string {
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return "agent"
	}
	var b strings.Builder
	for _, ch := range input {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			b.WriteRune(ch)
			continue
		}
		b.WriteRune('-')
	}
	out := strings.Trim(strings.ReplaceAll(b.String(), "--", "-"), "-")
	if out == "" {
		return "agent"
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
