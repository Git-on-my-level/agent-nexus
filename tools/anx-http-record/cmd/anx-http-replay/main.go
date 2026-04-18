package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"agent-nexus-tools-anx-http-record/internal/compiled"
	"agent-nexus-tools-anx-http-record/internal/replay"
)

func main() {
	var inputPath string
	var baseURL string
	var bindingsPath string

	flag.StringVar(&inputPath, "input", "", "required compiled replay JSON path")
	flag.StringVar(&baseURL, "base-url", "", "required target core base URL")
	flag.StringVar(&bindingsPath, "bindings-output", "", "optional output path for resolved runtime bindings JSON")
	flag.Parse()

	if inputPath == "" || baseURL == "" {
		fmt.Fprintln(os.Stderr, "--input and --base-url are required")
		os.Exit(2)
	}

	raw, err := os.ReadFile(inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read input: %v\n", err)
		os.Exit(1)
	}

	var run compiled.Run
	if err := json.Unmarshal(raw, &run); err != nil {
		fmt.Fprintf(os.Stderr, "decode input: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	bindings, err := replay.Replay(ctx, run, replay.Options{BaseURL: baseURL})
	if err != nil {
		fmt.Fprintf(os.Stderr, "replay compiled seed: %v\n", err)
		os.Exit(1)
	}

	if bindingsPath != "" {
		payload, err := json.MarshalIndent(bindings, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "encode bindings: %v\n", err)
			os.Exit(1)
		}
		payload = append(payload, '\n')
		if err := os.WriteFile(bindingsPath, payload, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "write bindings: %v\n", err)
			os.Exit(1)
		}
	}
}
