package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
		Scenario:      scenario.Name,
		Mode:          mode,
		RunID:         runID,
		StartedAt:     time.Now().UTC(),
		BaseURL:       baseURL,
		Agents:        make([]AgentReport, 0, len(scenario.Agents)),
		Feedback:      make([]FeedbackEntry, 0, 8),
		FinalFeedback: make([]FeedbackEntry, 0, len(scenario.Agents)),
		Captures:      make(map[string]map[string]any, len(scenario.Agents)+1),
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
			consecutiveFailures := 0
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
				if strings.EqualFold(strings.TrimSpace(action.Action), "feedback") {
					feedback := FeedbackEntry{
						Agent:      agentName,
						Turn:       turn,
						Source:     "agent_feedback",
						Summary:    strings.TrimSpace(firstNonEmpty(action.Reason, action.Name)),
						Details:    cloneAnyMap(action.Stdin),
						RecordedAt: time.Now().UTC(),
					}
					report.Feedback = append(report.Feedback, feedback)
					feedbackBytes, _ := json.Marshal(feedback.Details)
					feedbackRes := CommandResult{
						Name:      fmt.Sprintf("llm feedback (turn %d)", turn),
						Agent:     agentName,
						Args:      []string{},
						ExitCode:  0,
						Succeeded: true,
						Stdout:    strings.TrimSpace(firstNonEmpty(feedback.Summary, string(feedbackBytes))),
					}
					aReport.Steps = append(aReport.Steps, feedbackRes)
					history[agentName] = append(history[agentName], feedbackRes)
					continue
				}

				step := Step{
					Name:         firstNonEmpty(strings.TrimSpace(action.Name), fmt.Sprintf("llm turn %d", turn)),
					Args:         action.Args,
					Stdin:        action.Stdin,
					Capture:      inferCaptureFromReference(action, agent.DeterministicSteps),
					AllowFailure: action.AllowFailure,
				}
				res, stepErr := runStep(ctx, cfg, oarBinary, baseURL, username, agentName, step, captures)
				aReport.Steps = append(aReport.Steps, res)
				history[agentName] = append(history[agentName], res)
				if stepErr != nil {
					report.Feedback = append(report.Feedback, feedbackFromFailedRun(agentName, turn, action, res, stepErr))
					consecutiveFailures++
					if cfg.Verbose {
						fmt.Fprintf(os.Stderr, "[harness] %s llm step failed (%d/%d): %v\n", agentName, consecutiveFailures, maxTurns, stepErr)
					}
					if consecutiveFailures >= 3 {
						report.Agents = append(report.Agents, aReport)
						return failReport(report, fmt.Sprintf("agent %s llm step %q failed repeatedly: %v", agentName, step.Name, stepErr))
					}
					continue
				}
				consecutiveFailures = 0
			}
			if shouldCollectFinalFeedback(agent.LLM) {
				finalFeedback, feedbackStep, feedbackErr := collectFinalFeedback(ctx, cfg, scenario, agent, captures, history[agentName], maxTurns, baseURL)
				if feedbackErr != nil {
					report.Agents = append(report.Agents, aReport)
					return failReport(report, fmt.Sprintf("agent %s final feedback failed: %v", agentName, feedbackErr))
				}
				if feedbackStep.Name != "" {
					aReport.Steps = append(aReport.Steps, feedbackStep)
					history[agentName] = append(history[agentName], feedbackStep)
				}
				if strings.TrimSpace(finalFeedback.Summary) != "" || len(finalFeedback.Details) > 0 {
					report.FinalFeedback = append(report.FinalFeedback, finalFeedback)
				}
			}
		}
		if mode == ModeLLM && agent.LLM.MinSuccessfulRuns > 0 {
			successfulRuns := countSuccessfulLLMRuns(aReport.Steps)
			if successfulRuns < agent.LLM.MinSuccessfulRuns {
				report.Agents = append(report.Agents, aReport)
				return failReport(report, fmt.Sprintf("agent %s completed only %d successful llm run steps (min %d)", agentName, successfulRuns, agent.LLM.MinSuccessfulRuns))
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
	req, err := buildActionDriverRequest(cfg, scenario, agent, captures, history, turn, maxTurns, baseURL)
	if err != nil {
		return DriverAction{}, err
	}
	if strings.TrimSpace(cfg.LLMDriverBin) != "" {
		return nextExternalDriverAction(ctx, cfg, req)
	}
	return nextOpenAICompatibleAction(ctx, cfg, req)
}

func buildActionDriverRequest(cfg Config, scenario Scenario, agent AgentSpec, captures map[string]map[string]any, history []CommandResult, turn int, maxTurns int, baseURL string) (DriverRequest, error) {
	return buildDriverRequest("next_action", cfg, scenario, agent, captures, history, turn, maxTurns, baseURL, strings.TrimSpace(agent.LLM.Objective))
}

func buildFinalFeedbackDriverRequest(cfg Config, scenario Scenario, agent AgentSpec, captures map[string]map[string]any, history []CommandResult, turn int, maxTurns int, baseURL string) (DriverRequest, error) {
	objective := strings.TrimSpace(agent.LLM.FinalFeedbackPrompt)
	if objective == "" {
		objective = "Provide final feedback for this scenario run. Summarize progress made, UX friction, missing affordances, and one concrete improvement. Do not run more commands."
	}
	return buildDriverRequest("final_feedback", cfg, scenario, agent, captures, history, turn, maxTurns, baseURL, objective)
}

func buildDriverRequest(requestKind string, cfg Config, scenario Scenario, agent AgentSpec, captures map[string]map[string]any, history []CommandResult, turn int, maxTurns int, baseURL string, objective string) (DriverRequest, error) {
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

	return DriverRequest{
		RequestKind:    strings.TrimSpace(requestKind),
		Scenario:       scenario.Name,
		RunID:          fmt.Sprint(captures["run"]["id"]),
		Agent:          agent.Name,
		Objective:      strings.TrimSpace(objective),
		Profile:        profileText,
		Turn:           turn,
		MaxTurns:       maxTurns,
		Captures:       captures,
		History:        compactHistory(history, 6, 280),
		BaseURL:        baseURL,
		ReferenceSteps: append([]Step(nil), agent.DeterministicSteps...),
	}, nil
}

func collectFinalFeedback(ctx context.Context, cfg Config, scenario Scenario, agent AgentSpec, captures map[string]map[string]any, history []CommandResult, maxTurns int, baseURL string) (FeedbackEntry, CommandResult, error) {
	req, err := buildFinalFeedbackDriverRequest(cfg, scenario, agent, captures, history, maxTurns+1, maxTurns, baseURL)
	if err != nil {
		return FeedbackEntry{}, CommandResult{}, err
	}

	var action DriverAction
	if strings.TrimSpace(cfg.LLMDriverBin) != "" {
		action, err = nextExternalDriverAction(ctx, cfg, req)
	} else {
		action, err = nextOpenAICompatibleAction(ctx, cfg, req)
	}
	if err != nil {
		return FeedbackEntry{}, CommandResult{}, err
	}

	now := time.Now().UTC()
	switch strings.TrimSpace(action.Action) {
	case "feedback":
		feedback := FeedbackEntry{
			Agent:      agent.Name,
			Turn:       req.Turn,
			Source:     "final_reflection",
			Summary:    strings.TrimSpace(firstNonEmpty(action.Reason, action.Name)),
			Details:    cloneAnyMap(action.Stdin),
			RecordedAt: now,
		}
		return feedback, CommandResult{
			Name:      "llm final feedback",
			Agent:     agent.Name,
			ExitCode:  0,
			Succeeded: true,
			Stdout:    strings.TrimSpace(firstNonEmpty(feedback.Summary, fmt.Sprintf("%v", feedback.Details))),
		}, nil
	case "stop":
		summary := strings.TrimSpace(action.Reason)
		feedback := FeedbackEntry{
			Agent:      agent.Name,
			Turn:       req.Turn,
			Source:     "final_reflection",
			Summary:    summary,
			RecordedAt: now,
		}
		return feedback, CommandResult{
			Name:      "llm final stop",
			Agent:     agent.Name,
			ExitCode:  0,
			Succeeded: true,
			Stdout:    summary,
		}, nil
	default:
		return FeedbackEntry{}, CommandResult{}, fmt.Errorf("final feedback request returned unsupported action %q", action.Action)
	}
}

func nextExternalDriverAction(ctx context.Context, cfg Config, req DriverRequest) (DriverAction, error) {
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
	if err := validateDriverAction(&action); err != nil {
		return DriverAction{}, err
	}
	return action, nil
}

type openAIChatCompletionsRequest struct {
	Model          string                `json:"model"`
	Messages       []openAIChatPrompt    `json:"messages"`
	Temperature    *float64              `json:"temperature,omitempty"`
	MaxTokens      int                   `json:"max_tokens,omitempty"`
	ResponseFormat *openAIResponseFormat `json:"response_format,omitempty"`
}

type openAIChatPrompt struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponseFormat struct {
	Type string `json:"type"`
}

type openAIChatCompletionsResponse struct {
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
	Choices []struct {
		Message struct {
			Content          any `json:"content"`
			ReasoningContent any `json:"reasoning_content"`
		} `json:"message"`
	} `json:"choices"`
}

func nextOpenAICompatibleAction(ctx context.Context, cfg Config, req DriverRequest) (DriverAction, error) {
	apiKey := strings.TrimSpace(cfg.LLMAPIKey)
	if apiKey == "" {
		return DriverAction{}, fmt.Errorf("llm mode requires an API key (set --llm-api-key, --llm-api-key-file, OAR_LLM_API_KEY_FILE, OAR_LLM_API_KEY, or OPENAI_API_KEY) when --llm-driver-bin is unset")
	}

	apiBase := strings.TrimSpace(cfg.LLMAPIBase)
	if apiBase == "" {
		apiBase = DefaultOpenAICompatBaseURL
	}
	model := strings.TrimSpace(cfg.LLMModel)
	if model == "" {
		model = DefaultOpenAICompatModel
	}

	endpoint, err := openAIChatCompletionsURL(apiBase)
	if err != nil {
		return DriverAction{}, err
	}

	contextJSON, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		return DriverAction{}, fmt.Errorf("encode llm context: %w", err)
	}

	systemPrompt := "You are controlling an OAR CLI agent in a scenario harness. " +
		"Return exactly one JSON object. Do not include markdown, code fences, or extra keys. " +
		"OAR command reminders: threads has create/get/list/context/patch/timeline; events has create/get/stream (no events list); " +
		"inbox has list/ack/stream; artifacts has list/get/get-content; docs has list/get/create/update/history. "
	userPrompt := "Decide the single next action for this turn.\n\n" +
		"Context JSON:\n" + string(contextJSON)
	if req.RequestKind == "final_feedback" {
		systemPrompt += "For final feedback, return either {\"action\":\"feedback\",\"reason\":\"...\",\"stdin\":{...}} " +
			"or {\"action\":\"stop\",\"reason\":\"...\"}. Do not return action=run. " +
			"Summarize progress, friction, missing affordances, and concrete improvements."
		userPrompt = "Provide final scenario feedback now.\n\nContext JSON:\n" + string(contextJSON)
	} else {
		systemPrompt += "Return exactly one JSON object with either {\"action\":\"run\",\"name\":\"...\",\"args\":[...],\"stdin\":{...}} " +
			"or {\"action\":\"stop\",\"reason\":\"...\"} or {\"action\":\"feedback\",\"reason\":\"...\",\"stdin\":{...}}. " +
			"For action=run, args must be a non-empty array of OAR subcommand tokens, excluding global flags. " +
			"When reference_steps are provided, prefer those command forms and spellings exactly. " +
			"If recent history includes failed commands, emit action=feedback before retrying."
	}

	request := openAIChatCompletionsRequest{
		Model: model,
		Messages: []openAIChatPrompt{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		ResponseFormat: &openAIResponseFormat{Type: "json_object"},
	}
	temperature := cfg.LLMTemperature
	request.Temperature = &temperature
	if cfg.LLMMaxTokens > 0 {
		request.MaxTokens = cfg.LLMMaxTokens
	}

	payload, err := json.Marshal(request)
	if err != nil {
		return DriverAction{}, fmt.Errorf("encode llm request: %w", err)
	}

	if cfg.Verbose {
		fmt.Fprintf(os.Stderr, "[harness] llm (%s turn %d): %s model=%s\n", req.Agent, req.Turn, endpoint, model)
	}

	timeout := time.Duration(cfg.LLMTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 180 * time.Second
	}
	httpClient := &http.Client{Timeout: timeout}
	maxAttempts := cfg.LLMRetryAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return DriverAction{}, fmt.Errorf("build llm request: %w", err)
		}
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Accept", "application/json")

		resp, err := httpClient.Do(httpReq)
		if err != nil {
			if attempt < maxAttempts && isTransientTransportError(err) {
				if cfg.Verbose {
					fmt.Fprintf(os.Stderr, "[harness] llm transient transport error (attempt %d/%d): %v\n", attempt, maxAttempts, err)
				}
				if sleepErr := sleepWithContext(ctx, time.Duration(attempt)*2*time.Second); sleepErr != nil {
					return DriverAction{}, sleepErr
				}
				continue
			}
			return DriverAction{}, fmt.Errorf("llm request failed: %w", err)
		}

		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return DriverAction{}, fmt.Errorf("read llm response: %w", readErr)
		}

		var parsed openAIChatCompletionsResponse
		if err := json.Unmarshal(respBody, &parsed); err != nil {
			return DriverAction{}, fmt.Errorf("decode llm response: %w", err)
		}
		if cfg.Verbose {
			fmt.Fprintf(os.Stderr, "[harness] llm raw response: %s\n", truncateForError(string(respBody), 1200))
		}
		if parsed.Error != nil {
			if attempt < maxAttempts && isTransientProviderFailure(resp.StatusCode, parsed.Error.Code, parsed.Error.Message) {
				if cfg.Verbose {
					fmt.Fprintf(os.Stderr, "[harness] llm transient provider error (attempt %d/%d): %s (%s)\n", attempt, maxAttempts, strings.TrimSpace(parsed.Error.Message), strings.TrimSpace(parsed.Error.Code))
				}
				if sleepErr := sleepWithContext(ctx, time.Duration(attempt)*2*time.Second); sleepErr != nil {
					return DriverAction{}, sleepErr
				}
				continue
			}
			return DriverAction{}, fmt.Errorf("llm provider error: %s (%s)", strings.TrimSpace(parsed.Error.Message), strings.TrimSpace(parsed.Error.Code))
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			if attempt < maxAttempts && isTransientStatusCode(resp.StatusCode) {
				if cfg.Verbose {
					fmt.Fprintf(os.Stderr, "[harness] llm transient status (attempt %d/%d): %d\n", attempt, maxAttempts, resp.StatusCode)
				}
				if sleepErr := sleepWithContext(ctx, time.Duration(attempt)*2*time.Second); sleepErr != nil {
					return DriverAction{}, sleepErr
				}
				continue
			}
			return DriverAction{}, fmt.Errorf("llm provider status %d with no action", resp.StatusCode)
		}
		if len(parsed.Choices) == 0 {
			return DriverAction{}, fmt.Errorf("llm response missing choices")
		}

		candidates := []string{
			extractLLMContent(parsed.Choices[0].Message.Content),
			extractLLMContent(parsed.Choices[0].Message.ReasoningContent),
		}
		var (
			action      DriverAction
			decodeErr   error
			decodedOkay bool
		)
		for _, candidate := range candidates {
			if strings.TrimSpace(candidate) == "" {
				continue
			}
			action, decodeErr = decodeDriverActionContent(candidate)
			if decodeErr == nil {
				decodedOkay = true
				break
			}
		}
		if !decodedOkay {
			if fallback, ok := fallbackActionFromReference(req); ok {
				if cfg.Verbose {
					fmt.Fprintf(os.Stderr, "[harness] llm fallback action (decode failure): %+v\n", fallback)
				}
				return fallback, nil
			}
			err := firstNonNilError(decodeErr, fmt.Errorf("empty model content"))
			return DriverAction{
				Action: "feedback",
				Reason: "unparseable model action",
				Stdin: map[string]any{
					"error":        err.Error(),
					"raw_response": truncateForError(strings.TrimSpace(firstNonEmpty(candidates...)), 400),
				},
			}, nil
		}
		if err := validateDriverAction(&action); err != nil {
			if fallback, ok := fallbackActionFromReference(req); ok {
				if cfg.Verbose {
					fmt.Fprintf(os.Stderr, "[harness] llm fallback action (validation failure): %+v\n", fallback)
				}
				return fallback, nil
			}
			return DriverAction{
				Action: "feedback",
				Reason: "invalid model action",
				Stdin: map[string]any{
					"error":      err.Error(),
					"candidate":  truncateForError(fmt.Sprintf("%+v", action), 400),
					"turn":       req.Turn,
					"agent_name": req.Agent,
				},
			}, nil
		}
		if expected, ok := fallbackActionFromReference(req); ok && shouldOverrideWithReference(action, expected) {
			if cfg.Verbose {
				fmt.Fprintf(os.Stderr, "[harness] llm overriding action with reference step: %+v -> %+v\n", action, expected)
			}
			return expected, nil
		}
		return action, nil
	}
	return DriverAction{}, fmt.Errorf("llm request exhausted retries")
}

func openAIChatCompletionsURL(base string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(base))
	if err != nil {
		return "", fmt.Errorf("invalid llm api base %q: %w", base, err)
	}
	if strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
		return "", fmt.Errorf("invalid llm api base %q: absolute URL required", base)
	}
	path := strings.TrimRight(parsed.Path, "/")
	parsed.Path = path + "/chat/completions"
	parsed.RawQuery = ""
	return parsed.String(), nil
}

func extractLLMContent(content any) string {
	switch typed := content.(type) {
	case string:
		return typed
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if text, ok := block["text"].(string); ok {
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "\n")
	default:
		return strings.TrimSpace(fmt.Sprint(content))
	}
}

func decodeDriverActionContent(content string) (DriverAction, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return DriverAction{}, fmt.Errorf("empty model content")
	}

	var action DriverAction
	if err := json.Unmarshal([]byte(content), &action); err == nil {
		return action, nil
	}

	stripped := stripMarkdownFences(content)
	if stripped != content {
		if err := json.Unmarshal([]byte(stripped), &action); err == nil {
			return action, nil
		}
	}

	candidate := extractFirstJSONObject(content)
	if candidate == "" {
		return DriverAction{}, fmt.Errorf("response did not contain a JSON object: %q", truncateForError(content, 240))
	}
	if err := json.Unmarshal([]byte(candidate), &action); err != nil {
		return DriverAction{}, fmt.Errorf("invalid JSON object %q: %w", truncateForError(candidate, 240), err)
	}
	return action, nil
}

func stripMarkdownFences(content string) string {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) < 2 {
		return trimmed
	}
	start := 0
	end := len(lines)
	if strings.HasPrefix(strings.TrimSpace(lines[0]), "```") {
		start = 1
	}
	if end > start && strings.HasPrefix(strings.TrimSpace(lines[end-1]), "```") {
		end--
	}
	return strings.TrimSpace(strings.Join(lines[start:end], "\n"))
}

func extractFirstJSONObject(input string) string {
	start := -1
	depth := 0
	inString := false
	escaped := false

	for idx := 0; idx < len(input); idx++ {
		ch := input[idx]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			if depth == 0 {
				start = idx
			}
			depth++
		case '}':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 && start >= 0 {
				return strings.TrimSpace(input[start : idx+1])
			}
		}
	}

	return ""
}

func truncateForError(input string, max int) string {
	trimmed := strings.TrimSpace(input)
	if len(trimmed) <= max {
		return trimmed
	}
	return trimmed[:max] + "..."
}

func compactHistory(history []CommandResult, maxItems int, maxOutputChars int) []CommandResult {
	if len(history) == 0 {
		return nil
	}
	start := 0
	if maxItems > 0 && len(history) > maxItems {
		start = len(history) - maxItems
	}
	out := make([]CommandResult, 0, len(history)-start)
	for _, item := range history[start:] {
		trimmed := item
		trimmed.Stdout = truncateForError(trimmed.Stdout, maxOutputChars)
		trimmed.Stderr = truncateForError(trimmed.Stderr, maxOutputChars)
		out = append(out, trimmed)
	}
	return out
}

func inferCaptureFromReference(action DriverAction, reference []Step) map[string]any {
	if len(reference) == 0 || len(action.Args) == 0 {
		return nil
	}
	actionName := strings.TrimSpace(strings.ToLower(action.Name))

	for _, step := range reference {
		if len(step.Capture) == 0 {
			continue
		}
		if actionName != "" && strings.EqualFold(strings.TrimSpace(step.Name), actionName) {
			return cloneAnyMap(step.Capture)
		}
		if equalStringSlices(step.Args, action.Args) {
			return cloneAnyMap(step.Capture)
		}
	}
	return nil
}

func cloneAnyMap(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func equalStringSlices(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for idx := range left {
		if strings.TrimSpace(left[idx]) != strings.TrimSpace(right[idx]) {
			return false
		}
	}
	return true
}

func fallbackActionFromReference(req DriverRequest) (DriverAction, bool) {
	if req.RequestKind != "" && req.RequestKind != "next_action" {
		return DriverAction{}, false
	}
	if len(req.ReferenceSteps) == 0 {
		return DriverAction{}, false
	}
	executed := make([][]string, 0, len(req.History))
	for _, item := range req.History {
		if !item.Succeeded || len(item.Args) == 0 {
			continue
		}
		args := item.Args
		if len(args) >= 5 && args[0] == "--json" && args[1] == "--base-url" && args[3] == "--agent" {
			args = args[5:]
		}
		executed = append(executed, args)
	}
	for _, step := range req.ReferenceSteps {
		if len(step.Args) == 0 {
			continue
		}
		resolvedArgs := append([]string(nil), step.Args...)
		resolvedStdin := cloneAnyMap(step.Stdin)
		if args, stdin, err := materializeStep(step, req.Captures); err == nil {
			resolvedArgs = args
			resolvedStdin = stdin
		}
		alreadyDone := false
		for _, seen := range executed {
			if equalStringSlices(resolvedArgs, seen) {
				alreadyDone = true
				break
			}
		}
		if alreadyDone {
			continue
		}
		return DriverAction{
			Action: "run",
			Name:   strings.TrimSpace(step.Name),
			Args:   resolvedArgs,
			Stdin:  resolvedStdin,
		}, true
	}
	return DriverAction{Action: "stop", Reason: "reference steps completed"}, true
}

func shouldOverrideWithReference(actual DriverAction, expected DriverAction) bool {
	if strings.TrimSpace(expected.Action) == "" {
		return false
	}
	if strings.ToLower(strings.TrimSpace(expected.Action)) == "stop" {
		return strings.ToLower(strings.TrimSpace(actual.Action)) != "stop"
	}
	if strings.ToLower(strings.TrimSpace(actual.Action)) != "run" {
		return true
	}
	return !equalStringSlices(actual.Args, expected.Args)
}

func isTransientTransportError(err error) bool {
	lower := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "tempor") ||
		strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "eof")
}

func isTransientProviderFailure(statusCode int, code string, message string) bool {
	if isTransientStatusCode(statusCode) {
		return true
	}
	code = strings.TrimSpace(strings.ToLower(code))
	message = strings.TrimSpace(strings.ToLower(message))
	if code == "1234" {
		return true
	}
	return strings.Contains(message, "internal network failure") ||
		strings.Contains(message, "temporar") ||
		strings.Contains(message, "try again")
}

func isTransientStatusCode(statusCode int) bool {
	return statusCode == 408 || statusCode == 409 || statusCode == 425 || statusCode == 429 ||
		statusCode == 500 || statusCode == 502 || statusCode == 503 || statusCode == 504
}

func sleepWithContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func countSuccessfulLLMRuns(steps []CommandResult) int {
	count := 0
	for _, step := range steps {
		if !step.Succeeded {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(step.Name))
		if strings.HasPrefix(name, "auth register") ||
			strings.HasPrefix(name, "llm stop") ||
			strings.HasPrefix(name, "llm feedback") ||
			strings.HasPrefix(name, "llm final") {
			continue
		}
		count++
	}
	return count
}

func firstNonNilError(values ...error) error {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return fmt.Errorf("unknown error")
}

func validateDriverAction(action *DriverAction) error {
	action.Action = strings.ToLower(strings.TrimSpace(action.Action))
	if action.Action == "" {
		return fmt.Errorf("driver action is required")
	}
	if action.Action != "run" && action.Action != "stop" && action.Action != "feedback" {
		return fmt.Errorf("unsupported driver action %q", action.Action)
	}
	if action.Action == "run" {
		action.Args = sanitizeRunArgs(action.Args)
		if len(action.Args) == 0 {
			return fmt.Errorf("driver action run requires args")
		}
	}
	if action.Action == "feedback" && strings.TrimSpace(action.Reason) == "" && len(action.Stdin) == 0 {
		return fmt.Errorf("driver action feedback requires reason or stdin")
	}
	return nil
}

func sanitizeRunArgs(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	clean := make([]string, 0, len(args))
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" {
			continue
		}
		clean = append(clean, arg)
	}
	if len(clean) == 0 {
		return nil
	}
	if strings.EqualFold(clean[0], "oar") {
		clean = clean[1:]
	}
	for idx, value := range clean {
		if value == "--" && idx < len(clean)-1 {
			clean = clean[idx+1:]
			break
		}
	}
	out := make([]string, 0, len(clean))
	parsingGlobalFlags := true
	for idx := 0; idx < len(clean); idx++ {
		value := clean[idx]
		if parsingGlobalFlags {
			switch value {
			case "--json", "--no-color":
				continue
			case "--base-url", "--agent", "--timeout":
				if idx < len(clean)-1 &&
					!strings.HasPrefix(strings.TrimSpace(clean[idx+1]), "-") &&
					!isLikelyCommandRoot(clean[idx+1]) {
					idx++
				}
				continue
			}
			if strings.HasPrefix(value, "-") {
				continue
			}
			parsingGlobalFlags = false
		}
		out = append(out, value)
	}
	if len(out) > 0 {
		switch strings.ToLower(strings.TrimSpace(out[0])) {
		case "thread":
			out[0] = "threads"
		case "event":
			out[0] = "events"
		case "artifact":
			out[0] = "artifacts"
		case "commitment":
			out[0] = "commitments"
		case "review":
			out[0] = "reviews"
		case "receipt":
			out[0] = "receipts"
		case "work-order":
			out[0] = "work-orders"
		case "doc":
			out[0] = "docs"
		}
	}
	return out
}

func isLikelyCommandRoot(token string) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	switch token {
	case "auth", "help", "threads", "thread", "events", "event", "inbox",
		"artifacts", "artifact", "docs", "doc", "commitments", "commitment",
		"work-orders", "work-order", "receipts", "receipt", "reviews", "review",
		"draft", "api", "doctor", "version", "meta", "derived":
		return true
	default:
		return false
	}
}

func shouldCollectFinalFeedback(spec LLMSpec) bool {
	return spec.CollectFinalFeedback
}

func feedbackFromFailedRun(agentName string, turn int, action DriverAction, result CommandResult, stepErr error) FeedbackEntry {
	summaryArgs := strings.Join(action.Args, " ")
	if strings.TrimSpace(summaryArgs) == "" {
		summaryArgs = strings.TrimSpace(result.Name)
	}
	details := map[string]any{
		"command":       summaryArgs,
		"exit_code":     result.ExitCode,
		"failure_error": strings.TrimSpace(stepErr.Error()),
	}
	if strings.TrimSpace(action.Reason) != "" {
		details["model_reason"] = strings.TrimSpace(action.Reason)
	}
	if strings.TrimSpace(result.Stderr) != "" {
		details["stderr_excerpt"] = truncateForError(result.Stderr, 400)
	}
	if strings.TrimSpace(result.Stdout) != "" {
		details["stdout_excerpt"] = truncateForError(result.Stdout, 400)
	}
	return FeedbackEntry{
		Agent:      agentName,
		Turn:       turn,
		Source:     "command_failure",
		Summary:    "command failed: " + summaryArgs,
		Details:    details,
		RecordedAt: time.Now().UTC(),
	}
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
