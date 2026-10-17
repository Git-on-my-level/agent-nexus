package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"agent-nexus-cli/internal/errnorm"
)

func TestWorkspaceHintDoesNotProbeTypedAncestorOfUntypedLeaf(t *testing.T) {
	var probes atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		fmt.Fprint(w, `{"board":{"ref":"board:tasks"}}`)
	}))
	defer other.Close()
	a, cfg := hintFixture(t, other.URL, nil)
	for _, path := range []string{"/boards/board:tasks/cards/ship", "/boards/board:tasks/cards/internal-id/messages", "/docs/document:guide/comments/plain-comment"} {
		e := errnorm.New(errnorm.KindRemote, "not_found", "resource not found")
		annotateMissingRef(e, path)
		got := a.withWorkspaceHint(context.Background(), cfg, []string{"read", "card:ship"}, e)
		if got != e || asMap(e.Details)["requested_ref"] != nil || probes.Load() != 0 {
			t.Fatalf("false ancestor hint for %s: %#v probes=%d", path, e, probes.Load())
		}
	}
}

func TestWorkspaceHintRequiresMatchingIdentity(t *testing.T) {
	for _, tc := range []struct {
		body  string
		match bool
	}{
		{`{"card":{"id":"other"}}`, false},
		{`{"card":{"ref":"card:other","handle":"other","id":"other"}}`, false},
		{`{"card":{"ref":"topic:ship"}}`, false},
		{`{"card":{"ref":"topic:ship","handle":"ship","id":"ship"}}`, false},
		{`{"card":{"handle":"other"}}`, false},
		{`{"card":{"ref":"card:ship","id":"opaque-id"}}`, true},
		{`{"card":{"handle":"ship","id":"opaque-id"}}`, true},
		{`{"card":{"id":"ship"}}`, true},
	} {
		t.Run(tc.body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }))
			defer server.Close()
			a, cfg := hintFixture(t, server.URL, nil)
			e := errnorm.Normalize(a.withWorkspaceHint(context.Background(), cfg, []string{"context", "card:ship"}, missingCard()))
			matched := asMap(e.Details)["workspace_hint"] != nil
			if matched != tc.match {
				t.Fatalf("matched=%t err=%#v", matched, e)
			}
			if !tc.match && (strings.Contains(e.Message, "exists") || asMap(e.Details)["workspace_next_argv"] != nil) {
				t.Fatalf("false repair: %#v", e)
			}
		})
	}
}

func TestWorkspaceRetryArgvPreservesWhitespaceAndEmptyArguments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"card":{"ref":"card:ship"}}`) }))
	defer server.Close()
	for _, body := range []string{"", " \t ", "  padded note  ", "line one\nline two"} {
		a, cfg := hintFixture(t, server.URL, nil)
		original := []string{"note", "card:ship", body, "--next", "1d", "  checkpoint  "}
		e := errnorm.Normalize(a.withWorkspaceHint(context.Background(), cfg, original, missingCard()))
		want := append([]string{"anx", "--config-dir", cfg.ConfigDir, "--workspace", "b", "--as", "worker"}, original...)
		for _, jsonRoundTrip := range []bool{false, true} {
			current := e
			if jsonRoundTrip {
				raw, _ := json.Marshal(e.Details)
				var details map[string]any
				json.Unmarshal(raw, &details)
				current = errnorm.WithDetails(errnorm.New(errnorm.KindRemote, "not_found", e.Message), details)
			}
			actions := deriveErrorActions("note", current)
			if len(actions) != 1 || !reflect.DeepEqual(actions[0].Argv, want) {
				t.Fatalf("body=%q actions=%#v want=%#v", body, actions, want)
			}
		}
	}
}

func TestNoteTextPreservesSavedEvidence(t *testing.T) {
	rec := &noteRecorder{}
	server := noteServer(t, rec)
	defer server.Close()
	a, out := dailyTestApp(t, server.URL)
	if exit := a.Run([]string{"--as", "worker", "note", "card:x", "Progress", "--evidence", "event:proof", "--evidence", "https://example.test/verification"}); exit != 0 {
		t.Fatalf("exit=%d %s", exit, out)
	}
	for _, want := range []string{"event:proof", "https://example.test/verification"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("saved evidence %s missing: %s", want, out)
		}
	}
	if strings.Contains(out.String(), "Review the error") || a.Stderr.(*bytes.Buffer).Len() != 0 {
		t.Fatalf("unexpected error guidance: %s %s", out, a.Stderr)
	}
}
