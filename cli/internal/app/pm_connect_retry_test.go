package app

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func TestPMServeRegistrationConflictUsesConfigurationBackoff(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	oldOS, oldSleep := pmServiceOS, sleepFn
	t.Cleanup(func() { pmServiceOS, sleepFn = oldOS, oldSleep })
	pmServiceOS = "linux"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/pm/connect" {
			t.Errorf("claimed before resolving registration conflict: %s %s", r.Method, r.URL.Path)
		}
		calls++
		w.WriteHeader(409)
		w.Write([]byte(`{"error":{"code":"conflict","message":"another PM actor is registered","hint":"https://user:secret@example.test/","recoverable":true}}`))
	}))
	defer srv.Close()
	cfg := config.Resolved{BaseURL: srv.URL, ConfigDir: filepath.Join(home, "credentials"), As: "pm", AccessToken: "test"}
	state := filepath.Join(home, "state", "anx", "pm", pmServiceID(cfg.BaseURL, cfg.ConfigDir, "pm"))
	a := &App{Stderr: &bytes.Buffer{}, UserHomeDir: func() (string, error) { return home, nil }}
	want := []time.Duration{time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 15 * time.Minute, 15 * time.Minute}
	sleeps := 0
	sleepFn = func(ctx context.Context, delay time.Duration) error {
		if sleeps >= len(want) || delay != want[sleeps] {
			t.Fatalf("conflict backoff %d: %s", sleeps, delay)
		}
		sleeps++
		result, err := a.runPMService(ctx, "status", nil, cfg)
		if err != nil {
			t.Fatal(err)
		}
		failure := result.Data.(pmServiceStatus).LastConnectFailure
		if failure == nil || failure.Code != "conflict" || failure.Status != 409 || failure.Retryable || !strings.Contains(result.Text, "ANX_PM_AGENT_ACTOR_ID") || !strings.Contains(result.Text, "restart anx-core") || strings.Contains(result.Text, "secret") {
			t.Fatalf("missing safe conflict repair: %+v %s", failure, result.Text)
		}
		if sleeps == len(want) {
			cancel()
			return ctx.Err()
		}
		return nil
	}
	result, err := a.runPMServe(ctx, []string{"--runner", "echo {prompt_file}", "--work-dir", state}, cfg)
	if err != nil || result == nil || !asBool(asMap(result.Data)["stopped"]) || calls != len(want) || sleeps != len(want) {
		t.Fatalf("serve stopped before cancellation: %+v %v calls=%d sleeps=%d", result, err, calls, sleeps)
	}
	if !claimErrorRetryable(errnorm.FromHTTPFailure(409, nil)) {
		t.Fatal("connect conflict handling changed claim lease retries")
	}
}

func TestPMConnectTransientBackoffRecovers(t *testing.T) {
	old := sleepFn
	t.Cleanup(func() { sleepFn = old })
	state := t.TempDir()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pm/connect" {
			t.Errorf("unexpected request before connection: %s", r.URL.Path)
		}
		calls++
		if calls <= 10 {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte(`{"state":"connected"}`))
	}))
	defer srv.Close()
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 32 * time.Second, 64 * time.Second, 2 * time.Minute, 2 * time.Minute, 2 * time.Minute}
	sleeps := 0
	sleepFn = func(_ context.Context, delay time.Duration) error {
		if sleeps >= len(want) || delay != want[sleeps] {
			t.Fatalf("sleep %d: %s", sleeps, delay)
		}
		sleeps++
		failure, err := readPMConnectFailure(state)
		if err != nil || failure == nil || failure.Status != 503 || !failure.Retryable {
			t.Fatalf("transient failure not recorded: %+v %v", failure, err)
		}
		return nil
	}
	a := &App{Stderr: &bytes.Buffer{}}
	if err := a.waitPMConnect(context.Background(), config.Resolved{BaseURL: srv.URL, AccessToken: "test"}, nil, state); err != nil {
		t.Fatal(err)
	}
	failure, err := readPMConnectFailure(state)
	if sleeps != len(want) || calls != 11 || err != nil || failure != nil {
		t.Fatalf("recovery: sleeps=%d calls=%d failure=%+v err=%v", sleeps, calls, failure, err)
	}
}

func TestPMServeTransientConnectNeverClaims(t *testing.T) {
	for _, network := range []bool{false, true} {
		t.Run(map[bool]string{false: "503", true: "network"}[network], func(t *testing.T) {
			old := sleepFn
			t.Cleanup(func() { sleepFn = old })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			slept := false
			sleepFn = func(ctx context.Context, delay time.Duration) error {
				slept = true
				if delay != time.Second {
					t.Fatalf("first retry: %s", delay)
				}
				cancel()
				return ctx.Err()
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/pm/connect" {
					t.Errorf("claimed before connection: %s", r.URL.Path)
				}
				w.WriteHeader(503)
			}))
			defer srv.Close()
			if network {
				srv.Close()
			}
			state := t.TempDir()
			a := &App{Stderr: &bytes.Buffer{}}
			result, err := a.runPMServe(ctx, []string{"--runner", "echo {prompt_file}", "--work-dir", state}, config.Resolved{BaseURL: srv.URL, AccessToken: "test", Agent: "pm"})
			failure, readErr := readPMConnectFailure(state)
			if err != nil || !slept || result == nil || !asBool(asMap(result.Data)["stopped"]) || readErr != nil || failure == nil || !failure.Retryable {
				t.Fatalf("serve: %+v %v slept=%t diagnostic=%+v %v", result, err, slept, failure, readErr)
			}
		})
	}
}

func TestPMConnectDiagnosticWriteFailureIsAdvisory(t *testing.T) {
	for _, status := range []int{200, 403, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			old := sleepFn
			t.Cleanup(func() { sleepFn = old })
			state := t.TempDir()
			// A directory at the destination reliably fails atomic replacement,
			// including for privileged test users and without filling the disk.
			if err := os.Mkdir(filepath.Join(state, "last-connect.json"), 0700); err != nil {
				t.Fatal(err)
			}
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(status)
				if status == 200 {
					w.Write([]byte(`{"state":"connected"}`))
				}
			}))
			defer srv.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			slept := false
			sleepFn = func(ctx context.Context, _ time.Duration) error { slept = true; cancel(); return ctx.Err() }
			var logs bytes.Buffer
			a := &App{Stderr: &logs}
			err := a.waitPMConnect(ctx, config.Resolved{BaseURL: srv.URL, AccessToken: "test"}, nil, state)
			if status == 200 && (err != nil || slept) || status != 200 && (!errors.Is(err, context.Canceled) || !slept) || calls != 1 || !strings.Contains(logs.String(), "cannot persist advisory connect status") {
				t.Fatalf("diagnostic failure stopped connect: err=%v slept=%t calls=%d logs=%s", err, slept, calls, &logs)
			}
		})
	}
}
