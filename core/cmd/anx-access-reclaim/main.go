package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"agent-nexus-core/internal/storage"
)

func main() {
	source := flag.String("source", "", "stopped workspace state.sqlite")
	destination := flag.String("destination", "", "new verified database file (must not exist)")
	flag.Parse()
	if *source == "" || *destination == "" {
		fmt.Fprintln(os.Stderr, "--source and --destination are required; stop the core first")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	if err := storage.ReclaimCompactAccessOffline(ctx, *source, *destination); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	before, err := os.Stat(*source)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	after, err := os.Stat(*destination)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("Verified copy: before=%d after=%d saved=%d bytes. Keep the core stopped until replacement; source was retained.\n", before.Size(), after.Size(), before.Size()-after.Size())
}
