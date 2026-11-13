package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	sqlite3 "modernc.org/sqlite/lib"

	"agent-nexus-core/internal/storage"
)

const (
	defaultCompactTimeout = 2 * time.Hour
	timeoutEnv            = "ANX_SQLITE_COMPACT_TIMEOUT"
	verifyEnv             = "ANX_SQLITE_COMPACT_VERIFY"
	timingsEnv            = "ANX_SQLITE_COMPACT_TIMINGS"
	onlineDryRunEnv       = "ANX_SQLITE_COMPACT_ONLINE_DRY_RUN"
)

type config struct {
	source              string
	destination         string
	reserve             int64
	requireStableAccess bool
	timeout             time.Duration
	verify              storage.CompactVerifyMode
	timings             bool
	onlineDryRun        bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	cfg, err := parseConfig(args)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if cfg.source == "" || cfg.destination == "" {
		if cfg.onlineDryRun {
			fmt.Fprintln(stderr, "--source and --destination are required")
		} else {
			fmt.Fprintln(stderr, "--source and --destination are required; stop the core first")
		}
		return 2
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()
	if cfg.onlineDryRun {
		// A live core owns the workspace temp-directory lease. Use a dedicated
		// directory on the same filesystem so the dry run can run alongside it.
		previousTempDir, hadTempDir := os.LookupEnv("SQLITE_TMPDIR")
		tempDir, err := os.MkdirTemp(filepath.Dir(cfg.source), ".anx-compact-tmp-")
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		defer os.RemoveAll(tempDir)
		defer func() {
			if hadTempDir {
				_ = os.Setenv("SQLITE_TMPDIR", previousTempDir)
			} else {
				_ = os.Unsetenv("SQLITE_TMPDIR")
			}
		}()
		if err := os.Setenv("SQLITE_TMPDIR", tempDir); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
	}
	tempLease, err := storage.PrepareSQLiteTempDirectory(cfg.source)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer tempLease.Close()
	sqlite3.Xsqlite3_temp_directory = tempLease.NativePath
	var observer storage.CompactPhaseObserver
	if cfg.timings {
		observer = func(phase storage.CompactPhase, elapsed time.Duration) {
			fmt.Fprintf(stdout, "phase=%s elapsed_ms=%d\n", phase, elapsed.Milliseconds())
		}
	}
	result, err := storage.CompactSQLiteOffline(ctx, cfg.source, cfg.destination, storage.CompactOptions{
		RequireStableAccess: cfg.requireStableAccess,
		ReserveBytes:        cfg.reserve,
		Verify:              cfg.verify,
		OnlineDryRun:        cfg.onlineDryRun,
		PhaseObserver:       observer,
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if result.DryRun {
		fmt.Fprintf(
			stdout,
			"DRY_RUN_NOT_PUBLISHABLE: before=%d after=%d saved=%d bytes auto_vacuum=%d page_size=%d live_pages=%d wal=%d free_before=%d reserve=%d. The output is a stale snapshot and must not be published.\n",
			result.SourceBytes, result.DestinationBytes, result.SavedBytes, result.AutoVacuum,
			result.PageSize, result.LivePageBytes, result.WALBytes, result.FreeBytes, result.ReserveBytes,
		)
		if err := os.Remove(cfg.destination); err != nil {
			fmt.Fprintf(stderr, "remove dry-run output %s: %v\n", cfg.destination, err)
			return 1
		}
		fmt.Fprintf(stdout, "Removed dry-run output %s.\n", cfg.destination)
		return 0
	}
	fmt.Fprintf(
		stdout,
		"Verified copy: before=%d after=%d saved=%d bytes auto_vacuum=%d page_size=%d live_pages=%d wal=%d free_before=%d reserve=%d. Keep the core stopped until replacement; source was retained.\n",
		result.SourceBytes, result.DestinationBytes, result.SavedBytes, result.AutoVacuum,
		result.PageSize, result.LivePageBytes, result.WALBytes, result.FreeBytes, result.ReserveBytes,
	)
	return 0
}

func parseConfig(args []string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("anx-sqlite-compact", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	source := fs.String("source", "", "stopped workspace state.sqlite")
	destination := fs.String("destination", "", "new verified database file (must not exist)")
	reserve := fs.Int64("reserve-bytes", storage.DefaultCompactReserveBytes, "free space to leave on the destination filesystem after live pages")
	requireStableAccess := fs.Bool("require-stable-access", false, "require certified stable compact-access raw edge identities")

	timeoutDefault := defaultCompactTimeout
	timeoutEnvValue, timeoutEnvSet, timeoutEnvErr := envDuration(timeoutEnv)
	if timeoutEnvSet && timeoutEnvErr == nil {
		timeoutDefault = timeoutEnvValue
	}
	timeout := fs.Duration("timeout", timeoutDefault, "overall compaction deadline (default 2h)")

	verifyDefault := string(storage.CompactVerifyFull)
	if raw := os.Getenv(verifyEnv); raw != "" {
		verifyDefault = raw
	}
	verify := fs.String("verify", verifyDefault, "verification level: full or quick")

	timingsDefault, timingsEnvErr := envBool(timingsEnv)
	timings := fs.Bool("timings", timingsDefault, "print one line per compaction phase")

	onlineDryRunDefault, onlineDryRunEnvErr := envBool(onlineDryRunEnv)
	onlineDryRun := fs.Bool("online-dry-run", onlineDryRunDefault, "measure a read-only copy of a live database; requires --verify=quick")

	if err := fs.Parse(args); err != nil {
		return cfg, err
	}
	set := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	if !set["timeout"] {
		if timeoutEnvErr != nil {
			return cfg, timeoutEnvErr
		}
		if timeoutEnvSet {
			*timeout = timeoutEnvValue
		}
	}
	if *timeout <= 0 {
		return cfg, errors.New("--timeout must be positive")
	}
	if !set["verify"] {
		if raw := os.Getenv(verifyEnv); raw != "" {
			*verify = raw
		}
	}
	switch storage.CompactVerifyMode(*verify) {
	case storage.CompactVerifyFull, storage.CompactVerifyQuick:
	default:
		return cfg, fmt.Errorf("--verify must be full or quick, got %q", *verify)
	}
	if !set["timings"] && timingsEnvErr != nil {
		return cfg, timingsEnvErr
	}
	if !set["online-dry-run"] && onlineDryRunEnvErr != nil {
		return cfg, onlineDryRunEnvErr
	}
	cfg = config{
		source:              *source,
		destination:         *destination,
		reserve:             *reserve,
		requireStableAccess: *requireStableAccess,
		timeout:             *timeout,
		verify:              storage.CompactVerifyMode(*verify),
		timings:             *timings,
		onlineDryRun:        *onlineDryRun,
	}
	if cfg.onlineDryRun && cfg.verify != storage.CompactVerifyQuick {
		return cfg, errors.New("--online-dry-run requires --verify=quick")
	}
	return cfg, nil
}

func envDuration(name string) (time.Duration, bool, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return 0, false, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, true, fmt.Errorf("%s: %w", name, err)
	}
	if value <= 0 {
		return 0, true, fmt.Errorf("%s must be positive", name)
	}
	return value, true, nil
}

func envBool(name string) (bool, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return false, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s: %w", name, err)
	}
	return value, nil
}
