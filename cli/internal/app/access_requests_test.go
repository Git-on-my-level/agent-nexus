package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestAccessRequestUsageErrorsBeforeHostEnrollment(t *testing.T) {
	for _, args := range [][]string{
		{"auth", "access-requests"},
		{"auth", "access-requests", "request", "--reason", "Need administration"},
		{"auth", "access-requests", "request", "--grant", "auth-admin"},
		{"auth", "access-requests", "request", "--grant", "auth-admin", "--reason", "Need administration", "--principal-id", "other"},
		{"auth", "access-requests", "approve"},
		{"auth", "access-requests", "list", "--grant", "auth-admin"},
		{"auth", "access-requests", "unknown"},
		{"inbox", "summary", "--limit", "nope"},
		{"inbox", "summary", "--limit", "51"},
		{"inbox", "summary", "extra"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			app := newTestApp(t)
			home := t.TempDir()
			app.UserHomeDir = func() (string, error) { return home, nil }
			app.Getenv = func(string) string { return "" }
			var out, stderr bytes.Buffer
			app.Stdout, app.Stderr = &out, &stderr
			if code := app.Run(append([]string{"--json"}, args...)); code != 2 {
				t.Fatalf("usage exit=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
			}
			var envelope map[string]any
			if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			if envelope["ok"] != false || envelope["schema_version"] != float64(2) {
				t.Fatalf("wrong usage envelope: %#v", envelope)
			}
		})
	}
}

func TestInboxSummaryCountOnlyUsage(t *testing.T) {
	name, err := preflightConfigIndependentUsage([]string{"inbox", "summary", "--limit", "0"})
	if err != nil || name != "inbox summary" {
		t.Fatalf("count-only preflight: name=%q err=%v", name, err)
	}
	limit, err := parseInboxSummary([]string{"--limit", "0"})
	if err != nil || limit != 0 {
		t.Fatalf("count-only parse: limit=%d err=%v", limit, err)
	}
}
