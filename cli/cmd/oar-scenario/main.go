package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
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
		scenarioPath string
		oarBinary    string
		baseURL      string
		mode         string
		outputPath   string
		llmDriverBin string
		verbose      bool
	)
	var llmArgs repeatedStringFlag

	fs := flag.NewFlagSet("oar-scenario", flag.ExitOnError)
	fs.StringVar(&scenarioPath, "scenario", "", "Path to scenario JSON manifest")
	fs.StringVar(&oarBinary, "oar-bin", "", "Path to oar CLI binary (default: $OAR_BIN or ./oar)")
	fs.StringVar(&baseURL, "base-url", "", "Override scenario base URL")
	fs.StringVar(&mode, "mode", string(harness.ModeDeterministic), "Runner mode: deterministic or llm")
	fs.StringVar(&outputPath, "report", "", "Optional path to write JSON report")
	fs.StringVar(&llmDriverBin, "llm-driver-bin", "", "Path to external LLM driver program (required for --mode llm)")
	fs.Var(&llmArgs, "llm-driver-arg", "Argument for external LLM driver (repeatable)")
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

	cfg := harness.Config{
		ScenarioPath:     scenarioPath,
		OARBinary:        oarBinary,
		BaseURLOverride:  strings.TrimSpace(baseURL),
		Mode:             harness.Mode(strings.ToLower(strings.TrimSpace(mode))),
		LLMDriverBin:     strings.TrimSpace(llmDriverBin),
		LLMDriverArgs:    append([]string(nil), llmArgs.values...),
		Verbose:          verbose,
		WorkingDirectory: workingDir,
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
