package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	hostbridge "agent-nexus-cli/internal/bridge"
)

func TestHostBridgeReconnectLogging(t *testing.T) {
	a := newTestApp(t)
	var out bytes.Buffer
	a.Stderr = &out
	a.now = func() time.Time { return time.Date(2026, 10, 18, 10, 20, 30, 0, time.FixedZone("fixture", 7*3600)) }
	w, agent := hostbridge.Workspace{ID: "ws"}, hostbridge.Agent{ActorID: "actor"}
	for _, job := range []string{"answers", "wake"} {
		for _, err := range []error{nil, context.DeadlineExceeded, fmt.Errorf("read sse line: %w", context.DeadlineExceeded)} {
			if err = bridgeWakeResult(err); err != nil {
				a.logBridgeJobError(job, w, agent, err)
			}
		}
	}
	if out.Len() != 0 {
		t.Fatal("normal reconnect logged", out.String())
	}
	for _, err := range []error{fmt.Errorf("read sse line: %w", io.ErrUnexpectedEOF), fmt.Errorf("wake stream status 503")} {
		if err = bridgeWakeResult(err); err == nil {
			t.Fatal("real stream error suppressed")
		}
		a.logBridgeJobError("answers", w, agent, err)
	}
	// A non-SSE job's deadline is still logged.
	a.logBridgeJobError("git", w, agent, context.DeadlineExceeded)
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatal(out.String())
	}
	for _, line := range lines {
		stamp, _, ok := strings.Cut(line, " ")
		if _, err := time.Parse(time.RFC3339, stamp); !ok || err != nil || stamp != "2026-10-18T03:20:30Z" {
			t.Fatal(line, err)
		}
		if !strings.Contains(line, "code=internal_error") || !strings.Contains(line, "detail=") {
			t.Fatal(line)
		}
	}
}

func TestHostBridgeDoctorErrorSanitization(t *testing.T) {
	const secret = "secret callback command"
	for _, err := range []error{
		&json.SyntaxError{Offset: 42},
		&json.UnmarshalTypeError{Value: secret, Field: secret, Type: reflect.TypeOf("")},
		&os.PathError{Op: "open", Path: secret, Err: os.ErrPermission},
		fmt.Errorf("refusing non-regular file %s", secret),
	} {
		text := safeBridgeInspectionError(err)
		if text == "" || strings.Contains(text, secret) {
			t.Fatal(text)
		}
	}
}
