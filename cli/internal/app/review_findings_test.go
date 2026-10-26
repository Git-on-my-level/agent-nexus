package app

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
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
		got := a.withWorkspaceHint(cfg, e)
		if got != e || asMap(e.Details)["requested_ref"] != nil || probes.Load() != 0 {
			t.Fatalf("false ancestor hint for %s: %#v probes=%d", path, e, probes.Load())
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
