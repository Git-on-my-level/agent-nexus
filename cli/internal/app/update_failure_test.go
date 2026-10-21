package app

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"agent-nexus-cli/internal/errnorm"
)

func TestUpdateFailureCategories(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "HTTP status", err: errnorm.FromHTTPFailure(503, []byte("private response body")), want: "http_503"},
		{name: "timeout", err: fmt.Errorf("request: %w", context.DeadlineExceeded), want: "timeout"},
		{name: "network", err: errors.New("https://user:secret@example.invalid/path"), want: "network"},
		{name: "TLS record header", err: tls.RecordHeaderError{Msg: "private TLS detail"}, want: "tls"},
		{name: "TLS certificate", err: &tls.CertificateVerificationError{Err: errors.New("private certificate detail")}, want: "tls"},
		{name: "invalid JSON", err: &json.SyntaxError{Offset: 1}, want: "invalid_response"},
		{name: "normalized invalid response", err: errnorm.Wrap(errnorm.KindRemote, "invalid_response", "release response invalid", &json.SyntaxError{Offset: 1}), want: "invalid_response"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := updateFailureCategory(tt.err)
			if got != tt.want {
				t.Fatalf("updateFailureCategory() = %q, want %q", got, tt.want)
			}
			if !validUpdateFailureCategory(got) {
				t.Fatalf("category %q is outside the allowlist", got)
			}
		})
	}
}

func TestValidUpdateFailureCategoryIsBounded(t *testing.T) {
	for _, category := range []string{"network", "timeout", "tls", "invalid_response", "http_100", "http_503", "http_599"} {
		if !validUpdateFailureCategory(category) {
			t.Errorf("valid category %q rejected", category)
		}
	}
	for _, category := range []string{"http_99", "http_600", "http_050", "http_503x", "https://secret.invalid", "response body"} {
		if validUpdateFailureCategory(category) {
			t.Errorf("unbounded category %q accepted", category)
		}
	}
}

func TestReadUpdateStateDropsUnknownFailureCategories(t *testing.T) {
	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	if err := os.WriteFile(statePath, []byte(`{"failure_category":"https://secret.invalid","server_trigger_failure_category":"http_600"}`), 0600); err != nil {
		t.Fatal(err)
	}
	state, err := readUpdateState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if state.FailureCategory != "" || state.ServerTriggerFailureCategory != "" {
		t.Fatalf("unrecognized categories survived state read: %+v", state)
	}
}
