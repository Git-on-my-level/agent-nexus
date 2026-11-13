//go:build integration

package integration

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Dogfood compiled clients against a real core and disposable SQLite workspace.
// Neither a repository nor an installed provider runtime is needed.
func TestPortableWorkContextRecovery(t *testing.T) {
	h := newLiveCoreHarness(t)
	h.enrollHost(t, "reporter")
	for _, authority := range []string{"nexus", "github", "remote-tracker"} {
		t.Run(authority, func(t *testing.T) {
			source := map[string]any{"authority": authority}
			if authority != "nexus" {
				source["connection_id"] = "fixture"
				source["native_id"] = "research-1"
			}
			created := h.runCLIExpectOK(t, "reporter", map[string]any{"title": "Recover research " + authority, "source": source}, "work", "create", "--from-file", "-")
			ref := mustStringPath(t, created.Payload, "result.work.ref")
			before := h.runCLIExpectOK(t, "reporter", nil, "context", ref, "--brief")
			beforeVersion, _ := getPathValue(before.Payload, "result.work.version")
			beforeDecision, _ := getPathValue(before.Payload, "result.work.decision_revision")
			checkpoint := map[string]any{"summary": "Draft preserved; provider unavailable", "next_action": "Read the draft and check the existing run before continuing", "execution": map[string]any{"authority": "runner", "run_id": "run-17"}, "artifacts": []any{map[string]any{"ref": "file:draft.txt", "portability": "local_only", "host": "author-laptop"}, map[string]any{"ref": "https://example.test/research/draft", "portability": "portable"}}}
			body := map[string]any{"if_version": 0, "checkpoint": checkpoint}
			h.runCLIExpectOK(t, "reporter", body, "work", "checkpoint", ref, "--from-file", "-")
			for i := 0; i < 3; i++ {
				retry := h.runCLIExpectOK(t, "reporter", body, "work", "checkpoint", ref, "--from-file", "-")
				if v, _ := getPathValue(retry.Payload, "result.unchanged"); v != true {
					t.Fatal("retry was not a no-op", retry.Stdout)
				}
			}
			brief := h.runCLIExpectOK(t, "reporter", nil, "work", "briefing", ref)
			if v, _ := getPathValue(brief.Payload, "result.work.version"); v != beforeVersion {
				t.Fatal("checkpoint altered work version")
			}
			if v, _ := getPathValue(brief.Payload, "result.work.decision_revision"); v != beforeDecision {
				t.Fatal("checkpoint altered decision revision")
			}
			if v := mustStringPath(t, brief.Payload, "result.checkpoint.content.next_action"); v != checkpoint["next_action"] {
				t.Fatal("lost recovery instructions")
			}
			checkpoint["next_action"] = "Check reviewer objections"
			stale := h.runCLI(t, "reporter", body, "work", "checkpoint", ref, "--from-file", "-")
			if stale.ExitCode == 0 || !strings.Contains(stale.Stdout, "conflict") {
				t.Fatal("stale checkpoint overwrote current report", stale.Stdout)
			}
			body["if_version"] = 1
			h.runCLIExpectOK(t, "reporter", body, "work", "checkpoint", ref, "--from-file", "-")
			if authority != "nexus" {
				for i, status := range []string{"reported", "error"} {
					report := map[string]any{"observation": map[string]any{"idempotency_key": fmt.Sprint(i), "reader_id": "fixture", "reader_revision": "v1", "source_sequence": i + 1, "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "status": status, "facts": map[string]any{"phase": "review", "native_status": "custom-provider-state"}}}
					h.runCLIExpectOK(t, "reporter", report, "work", "observations", "submit", ref, "--from-file", "-")
				}
				brief = h.runCLIExpectOK(t, "reporter", nil, "context", ref, "--brief")
				if got := mustStringPath(t, brief.Payload, "result.work.freshness.status"); got != "error" {
					t.Fatal("outage hidden", brief.Stdout)
				}
				if got := mustStringPath(t, brief.Payload, "result.work.phase"); got != "review" {
					t.Fatal("outage regressed source state", brief.Stdout)
				}
				if got := mustStringPath(t, brief.Payload, "result.observation.facts.native_status"); got != "custom-provider-state" {
					t.Fatal("native source detail lost", brief.Stdout)
				}
			}
			t.Logf("%s: recoverable intent, attribution, artifact locality, retries, CAS and independent task revision verified", authority)
		})
	}
}
