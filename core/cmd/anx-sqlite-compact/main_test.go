package main

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/storage"
)

func clearCompactEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{timeoutEnv, verifyEnv, timingsEnv, onlineDryRunEnv} {
		t.Setenv(name, "")
	}
}

func TestParseConfigDefaults(t *testing.T) {
	clearCompactEnv(t)
	cfg, err := parseConfig([]string{"--source", "state.sqlite", "--destination", "state.compact.sqlite"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.timeout != 2*time.Hour {
		t.Fatalf("timeout = %v, want 2h", cfg.timeout)
	}
	if cfg.verify != storage.CompactVerifyFull {
		t.Fatalf("verify = %q, want full", cfg.verify)
	}
	if cfg.timings || cfg.onlineDryRun {
		t.Fatalf("unexpected boolean defaults: %+v", cfg)
	}
}

func TestParseConfigEnvOverrides(t *testing.T) {
	clearCompactEnv(t)
	t.Setenv(timeoutEnv, "45m")
	t.Setenv(verifyEnv, "quick")
	t.Setenv(timingsEnv, "1")
	t.Setenv(onlineDryRunEnv, "1")
	cfg, err := parseConfig([]string{"--source", "state.sqlite", "--destination", "state.compact.sqlite"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.timeout != 45*time.Minute {
		t.Fatalf("timeout = %v, want 45m", cfg.timeout)
	}
	if cfg.verify != storage.CompactVerifyQuick {
		t.Fatalf("verify = %q, want quick", cfg.verify)
	}
	if !cfg.timings || !cfg.onlineDryRun {
		t.Fatalf("environment booleans not applied: %+v", cfg)
	}
}

func TestParseConfigFlagPrecedence(t *testing.T) {
	clearCompactEnv(t)
	t.Setenv(timeoutEnv, "45m")
	t.Setenv(verifyEnv, "quick")
	t.Setenv(timingsEnv, "1")
	t.Setenv(onlineDryRunEnv, "1")
	cfg, err := parseConfig([]string{
		"--source", "state.sqlite",
		"--destination", "state.compact.sqlite",
		"--timeout", "3h",
		"--verify", "full",
		"--timings=false",
		"--online-dry-run=false",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.timeout != 3*time.Hour {
		t.Fatalf("timeout = %v, want 3h", cfg.timeout)
	}
	if cfg.verify != storage.CompactVerifyFull {
		t.Fatalf("verify = %q, want full", cfg.verify)
	}
	if cfg.timings || cfg.onlineDryRun {
		t.Fatalf("flags did not override environment: %+v", cfg)
	}
}

func TestParseConfigInvalidValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		args []string
	}{
		{name: "zero timeout flag", args: []string{"--timeout=0"}},
		{name: "negative timeout flag", args: []string{"--timeout=-1s"}},
		{name: "invalid timeout flag", args: []string{"--timeout=bogus"}},
		{name: "zero timeout env", env: map[string]string{timeoutEnv: "0"}},
		{name: "negative timeout env", env: map[string]string{timeoutEnv: "-5m"}},
		{name: "invalid timeout env", env: map[string]string{timeoutEnv: "soon"}},
		{name: "invalid verify flag", args: []string{"--verify=bogus"}},
		{name: "invalid verify env", env: map[string]string{verifyEnv: "bogus"}},
		{name: "invalid timings env", env: map[string]string{timingsEnv: "maybe"}},
		{name: "invalid dry-run env", env: map[string]string{onlineDryRunEnv: "maybe"}},
		{name: "dry run with full flag", args: []string{"--online-dry-run", "--verify=full"}},
		{name: "dry run with full env", env: map[string]string{onlineDryRunEnv: "1", verifyEnv: "full"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearCompactEnv(t)
			for name, value := range tc.env {
				t.Setenv(name, value)
			}
			args := append([]string{"--source", "state.sqlite", "--destination", "state.compact.sqlite"}, tc.args...)
			if _, err := parseConfig(args); err == nil {
				t.Fatalf("expected an error for %v", args)
			}
		})
	}
}

func TestParseConfigInvalidEnvIgnoredWhenFlagSet(t *testing.T) {
	clearCompactEnv(t)
	t.Setenv(timeoutEnv, "bogus")
	t.Setenv(verifyEnv, "bogus")
	t.Setenv(timingsEnv, "bogus")
	t.Setenv(onlineDryRunEnv, "bogus")
	cfg, err := parseConfig([]string{
		"--source", "state.sqlite",
		"--destination", "state.compact.sqlite",
		"--timeout", "1h",
		"--verify", "quick",
		"--timings=false",
		"--online-dry-run=false",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.timeout != time.Hour || cfg.verify != storage.CompactVerifyQuick || cfg.timings || cfg.onlineDryRun {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestRunOnlineDryRun(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "state.sqlite")
	destination := filepath.Join(dir, "state.compact.sqlite")
	db, err := sql.Open("sqlite", "file:"+source)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	for _, q := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA page_size=4096`,
		`PRAGMA auto_vacuum=NONE`,
		`CREATE TABLE items(id INTEGER PRIMARY KEY, body BLOB)`,
		`INSERT INTO items(body) VALUES('committed')`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{
		"--source", source,
		"--destination", destination,
		"--online-dry-run",
		"--verify", "quick",
		"--timings",
		"--reserve-bytes", "1",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run exit code = %d, stderr = %s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "DRY_RUN_NOT_PUBLISHABLE") {
		t.Fatalf("missing dry-run marker: %s", stdout.String())
	}
	if strings.Contains(stdout.String(), "Verified copy") {
		t.Fatalf("dry run printed the verified-success line: %s", stdout.String())
	}
	if !strings.Contains(stdout.String(), "phase=vacuum_into") {
		t.Fatalf("missing vacuum_into timing: %s", stdout.String())
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run output was not removed: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO items(body) VALUES('after')`); err != nil {
		t.Fatalf("source was not writable after the dry run: %v", err)
	}
}
