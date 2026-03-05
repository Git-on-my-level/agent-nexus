package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"organization-autorunner-cli/scenarios/harness"
)

type repeatedStringFlag struct {
	values []string
}

func (f *repeatedStringFlag) String() string {
	return strings.Join(f.values, ",")
}

func (f *repeatedStringFlag) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	f.values = append(f.values, value)
	return nil
}

func main() {
	var (
		scenarioPath  string
		oarBinary     string
		baseURL       string
		mode          string
		outputPath    string
		llmDriverBin  string
		llmAPIBase    string
		llmAPIKey     string
		llmAPIKeyFile string
		llmModel      string
		llmTemp       float64
		llmMaxTokens  int
		llmTimeoutSec int
		llmRetries    int
		verbose       bool
	)
	var llmArgs repeatedStringFlag

	fs := flag.NewFlagSet("oar-scenario", flag.ExitOnError)
	fs.StringVar(&scenarioPath, "scenario", "", "Path to scenario JSON manifest")
	fs.StringVar(&oarBinary, "oar-bin", "", "Path to oar CLI binary (default: $OAR_BIN or ./oar)")
	fs.StringVar(&baseURL, "base-url", "", "Override scenario base URL")
	fs.StringVar(&mode, "mode", string(harness.ModeDeterministic), "Runner mode: deterministic or llm")
	fs.StringVar(&outputPath, "report", "", "Optional path to write JSON report")
	fs.StringVar(&llmDriverBin, "llm-driver-bin", "", "Path to external LLM driver program (optional in --mode llm)")
	fs.Var(&llmArgs, "llm-driver-arg", "Argument for external LLM driver (repeatable)")
	fs.StringVar(&llmAPIBase, "llm-api-base", firstNonEmpty(strings.TrimSpace(os.Getenv("OAR_LLM_API_BASE")), harness.DefaultOpenAICompatBaseURL), "OpenAI-compatible LLM API base URL used when --llm-driver-bin is unset")
	fs.StringVar(&llmAPIKey, "llm-api-key", firstNonEmpty(strings.TrimSpace(os.Getenv("OAR_LLM_API_KEY")), strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))), "API key for built-in OpenAI-compatible LLM mode")
	fs.StringVar(&llmAPIKeyFile, "llm-api-key-file", strings.TrimSpace(os.Getenv("OAR_LLM_API_KEY_FILE")), "Path to file containing API key for built-in OpenAI-compatible LLM mode")
	fs.StringVar(&llmModel, "llm-model", firstNonEmpty(strings.TrimSpace(os.Getenv("OAR_LLM_MODEL")), harness.DefaultOpenAICompatModel), "Model name for built-in OpenAI-compatible LLM mode")
	fs.Float64Var(&llmTemp, "llm-temperature", 0.0, "Sampling temperature for built-in OpenAI-compatible LLM mode")
	fs.IntVar(&llmMaxTokens, "llm-max-tokens", 2000, "Max completion tokens for built-in OpenAI-compatible LLM mode")
	fs.IntVar(&llmTimeoutSec, "llm-timeout-seconds", firstNonZeroInt(parseEnvInt("OAR_LLM_TIMEOUT_SECONDS"), 180), "HTTP timeout in seconds for built-in OpenAI-compatible LLM requests")
	fs.IntVar(&llmRetries, "llm-retries", firstNonZeroInt(parseEnvInt("OAR_LLM_RETRIES"), 3), "Max retry attempts for transient built-in LLM request failures")
	fs.BoolVar(&verbose, "verbose", false, "Print executed commands to stderr")

	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	scenarioPath = strings.TrimSpace(scenarioPath)
	if scenarioPath == "" {
		fmt.Fprintln(os.Stderr, "--scenario is required")
		os.Exit(2)
	}
	workingDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "resolve working directory:", err)
		os.Exit(1)
	}
	if !filepath.IsAbs(scenarioPath) {
		scenarioPath = filepath.Join(workingDir, scenarioPath)
	}

	oarBinary = strings.TrimSpace(oarBinary)
	if oarBinary == "" {
		oarBinary = strings.TrimSpace(os.Getenv("OAR_BIN"))
	}
	if oarBinary == "" {
		oarBinary = "./oar"
	}
	if !filepath.IsAbs(oarBinary) {
		oarBinary = filepath.Join(workingDir, oarBinary)
	}

	resolvedLLMAPIKey, err := resolveLLMAPIKey(strings.TrimSpace(llmAPIKey), strings.TrimSpace(llmAPIKeyFile), workingDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "resolve llm api key:", err)
		os.Exit(1)
	}

	cfg := harness.Config{
		ScenarioPath:      scenarioPath,
		OARBinary:         oarBinary,
		BaseURLOverride:   strings.TrimSpace(baseURL),
		Mode:              harness.Mode(strings.ToLower(strings.TrimSpace(mode))),
		LLMDriverBin:      strings.TrimSpace(llmDriverBin),
		LLMDriverArgs:     append([]string(nil), llmArgs.values...),
		LLMAPIBase:        strings.TrimSpace(llmAPIBase),
		LLMAPIKey:         resolvedLLMAPIKey,
		LLMModel:          strings.TrimSpace(llmModel),
		LLMTemperature:    llmTemp,
		LLMMaxTokens:      llmMaxTokens,
		LLMTimeoutSeconds: llmTimeoutSec,
		LLMRetryAttempts:  llmRetries,
		Verbose:           verbose,
		WorkingDirectory:  workingDir,
	}

	report, runErr := harness.Run(context.Background(), cfg)

	reportJSON, marshalErr := json.MarshalIndent(report, "", "  ")
	if marshalErr != nil {
		fmt.Fprintln(os.Stderr, "encode report:", marshalErr)
		os.Exit(1)
	}
	_, _ = os.Stdout.Write(reportJSON)
	_, _ = os.Stdout.Write([]byte("\n"))

	outputPath = strings.TrimSpace(outputPath)
	if outputPath != "" {
		if !filepath.IsAbs(outputPath) {
			outputPath = filepath.Join(workingDir, outputPath)
		}
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, "create report directory:", err)
			os.Exit(1)
		}
		if err := os.WriteFile(outputPath, reportJSON, 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "write report:", err)
			os.Exit(1)
		}
	}

	if runErr != nil {
		fmt.Fprintln(os.Stderr, "scenario failed:", runErr)
		os.Exit(1)
	}
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

func resolveLLMAPIKey(key string, keyFile string, workingDir string) (string, error) {
	if strings.TrimSpace(key) != "" {
		return strings.TrimSpace(key), nil
	}
	if strings.TrimSpace(keyFile) == "" {
		return "", nil
	}
	path := strings.TrimSpace(keyFile)
	if !filepath.IsAbs(path) {
		path = filepath.Join(workingDir, path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read key file %q: %w", path, err)
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return "", fmt.Errorf("key file %q is empty", path)
	}
	return trimmed, nil
}

func parseEnvInt(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return value
}

func firstNonZeroInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}
