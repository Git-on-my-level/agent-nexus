package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Hour)
	defer cancel()
	switch os.Args[1] {
	case "dump":
		runDump(ctx, os.Args[2:])
	case "compare":
		runCompare(ctx, os.Args[2:])
	case "migrate-and-drain":
		runMigrateAndDrain(ctx, os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: anx-access-equivalence <dump|compare|migrate-and-drain> [flags]")
	fmt.Fprintln(os.Stderr, "  dump --db <copy> --out <file.jsonl>")
	fmt.Fprintln(os.Stderr, "  compare <before.jsonl> <after.jsonl> [--allow-card-history-union --before-db <copy>]")
	fmt.Fprintln(os.Stderr, "  migrate-and-drain --db <writable copy>")
}

func runDump(ctx context.Context, args []string) {
	flags := flag.NewFlagSet("dump", flag.ExitOnError)
	db := flags.String("db", "", "read-only database copy")
	out := flags.String("out", "", "JSONL dump output")
	_ = flags.Parse(args)
	if *db == "" || *out == "" {
		flags.Usage()
		os.Exit(2)
	}
	stats, err := primitives.DumpAccessEquivalence(ctx, *db, *out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dump failed:", err)
		os.Exit(1)
	}
	fmt.Printf("dump: records=%d decisions=%d denied=%d scopes=%d resources=%d bytes=%d duration=%s\n",
		stats.Records, stats.Decisions, stats.Denied, stats.Scopes, stats.Resources, stats.Bytes, stats.Duration.Round(time.Millisecond))
}

func runCompare(ctx context.Context, args []string) {
	flags := flag.NewFlagSet("compare", flag.ExitOnError)
	allow := flags.Bool("allow-card-history-union", false, "permit computed older card-revision visible->hidden changes")
	beforeDB := flags.String("before-db", "", "read-only before database copy used to compute the card-history allowlist")
	_ = flags.Parse(args)
	if flags.NArg() != 2 {
		flags.Usage()
		os.Exit(2)
	}
	result, err := primitives.CompareAccessEquivalence(ctx, flags.Arg(0), flags.Arg(1), *allow, *beforeDB)
	if err != nil {
		fmt.Fprintln(os.Stderr, "compare failed:", err)
		os.Exit(1)
	}
	fmt.Printf("compare: compared=%d allowed_card_history=%d mismatches=%d\n", result.Compared, result.Allowed, result.Mismatches)
}

func runMigrateAndDrain(ctx context.Context, args []string) {
	flags := flag.NewFlagSet("migrate-and-drain", flag.ExitOnError)
	db := flags.String("db", "", "writable database copy")
	_ = flags.Parse(args)
	if *db == "" {
		flags.Usage()
		os.Exit(2)
	}
	logf := func(message string) { fmt.Fprintln(os.Stderr, message) }
	if err := storage.MigrateAndDrainAccess(ctx, *db, logf); err != nil {
		fmt.Fprintln(os.Stderr, "migrate-and-drain failed:", err)
		os.Exit(1)
	}
	fmt.Println("migrate-and-drain: all access maintenance jobs idle; full verification passed")
}
