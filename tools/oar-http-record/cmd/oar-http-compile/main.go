package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"organization-autorunner-tools-oar-http-record/internal/compiler"
)

func main() {
	var inputPath string
	var outputPath string

	flag.StringVar(&inputPath, "input", "", "required input JSONL recording path")
	flag.StringVar(&outputPath, "output", "", "required output compiled JSON path")
	flag.Parse()

	if inputPath == "" || outputPath == "" {
		fmt.Fprintln(os.Stderr, "--input and --output are required")
		os.Exit(2)
	}

	inputFile, err := os.Open(inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "open input: %v\n", err)
		os.Exit(1)
	}
	defer inputFile.Close()

	run, err := compiler.CompileJSONL(inputFile, compiler.Options{SourceRecording: inputPath})
	if err != nil {
		fmt.Fprintf(os.Stderr, "compile recording: %v\n", err)
		os.Exit(1)
	}

	raw, err := json.MarshalIndent(run, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "encode output: %v\n", err)
		os.Exit(1)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(outputPath, raw, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write output: %v\n", err)
		os.Exit(1)
	}
}
