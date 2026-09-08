//go:build integration

package integration

import (
	"fmt"
	"testing"
	"time"
)

// These scenarios execute the compiled CLI against an actual isolated core and
// SQLite workspace. All resources and evidence are synthetic test fixtures.
func TestUnifiedWorkObservationReplayScenario(t *testing.T) {
	h := newLiveCoreHarness(t)
	h.registerAgentBootstrap(t, "reporter", "reporter."+runToken())
	board := h.runCLIExpectOK(t, "reporter", map[string]any{"board": map[string]any{"title": "Synthetic unified work", "document_refs": []any{}, "pinned_refs": []any{}, "provenance": map[string]any{"sources": []any{"inferred"}}}}, "boards", "create")
	boardRef := mustStringPath(t, board.Payload, "data.board.ref")
	create := map[string]any{"board_ref": boardRef, "title": "Synthetic externally owned commitment", "source": map[string]any{"authority": "github", "connection_id": "synthetic", "native_id": "fixture/repository/issues/1"}}
	work := h.runCLIExpectOK(t, "reporter", create, "work", "create", "--from-file", "-")
	ref := mustStringPath(t, work.Payload, "data.work.ref")
	duplicate := h.runCLIExpectOK(t, "reporter", create, "work", "create", "--from-file", "-")
	if got := mustStringPath(t, duplicate.Payload, "data.work.ref"); got != ref {
		t.Fatalf("registration duplicated work: %s != %s", got, ref)
	}
	// Native card identity survives the projection.
	h.runCLIExpectOK(t, "reporter", nil, "cards", "get", ref)
	h.runCLIExpectOK(t, "reporter", nil, "work", "capabilities")
	report := func(key string, sequence int, phase, status string) map[string]any {
		return map[string]any{"observation": map[string]any{"idempotency_key": key, "reader_id": "synthetic-reader", "reader_revision": "v1", "source_sequence": sequence, "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "status": status, "facts": map[string]any{"phase": phase, "native_status": "custom-source-state"}, "evidence": []any{map[string]any{"url": "https://example.test/fixture", "summary": "Synthetic fixture"}}}}
	}
	newest := report("newest", 20, "review", "verified")
	first := h.runCLIExpectOK(t, "reporter", newest, "work", "observations", "submit", ref, "--from-file", "-")
	if got := mustStringPath(t, first.Payload, "data.observation.verification"); got != "reported" {
		t.Fatalf("remote claim self-certified: %s", got)
	}
	second := h.runCLIExpectOK(t, "reporter", newest, "work", "observations", "submit", ref, "--from-file", "-")
	if got, _ := getPathValue(second.Payload, "data.duplicate"); got != true {
		t.Fatalf("duplicate not identified: %s", second.Stdout)
	}
	if got := mustStringPath(t, second.Payload, "data.observation.id"); got != mustStringPath(t, first.Payload, "data.observation.id") {
		t.Fatalf("duplicate created evidence: %s", second.Stdout)
	}
	h.runCLIExpectOK(t, "reporter", report("older", 10, "in_progress", "reported"), "work", "observations", "submit", ref, "--from-file", "-")
	current := h.runCLIExpectOK(t, "reporter", nil, "work", "get", ref)
	if got := mustStringPath(t, current.Payload, "data.work.phase"); got != "review" {
		t.Fatalf("out-of-order report regressed state: %s", current.Stdout)
	}
	conflict := h.runCLI(t, "reporter", report("newest", 30, "done", "reported"), "work", "observations", "submit", ref, "--from-file", "-")
	if conflict.ExitCode == 0 {
		t.Fatalf("same key with different report accepted: %s", conflict.Stdout)
	}
	h.runCLIExpectOK(t, "reporter", report("failed", 30, "done", "error"), "work", "observations", "submit", ref, "--from-file", "-")
	freshness := h.runCLIExpectOK(t, "reporter", nil, "work", "freshness", ref)
	if got := mustStringPath(t, freshness.Payload, "data.freshness.status"); got != "error" {
		t.Fatalf("failed refresh not visible: %s", freshness.Stdout)
	}
	current = h.runCLIExpectOK(t, "reporter", nil, "work", "get", ref)
	if got := mustStringPath(t, current.Payload, "data.work.phase"); got != "review" {
		t.Fatalf("failed read regressed last good work: %s", current.Stdout)
	}
	observations := h.runCLIExpectOK(t, "reporter", nil, "work", "observations", "list", ref, "--limit", "1")
	cursor := mustStringPath(t, observations.Payload, "data.next_cursor")
	next := h.runCLIExpectOK(t, "reporter", nil, "work", "observations", "list", ref, "--limit", "1", "--cursor", cursor)
	a, _ := getPathValue(observations.Payload, "data.observations")
	b, _ := getPathValue(next.Payload, "data.observations")
	if fmt.Sprint(a) == fmt.Sprint(b) {
		t.Fatal("observation cursor repeated page")
	}
	h.runCLIExpectOK(t, "reporter", nil, "work", "context", ref, "--limit", "2")
	queued := h.runCLIExpectOK(t, "reporter", nil, "work", "refresh", "request", ref)
	again := h.runCLIExpectOK(t, "reporter", nil, "work", "refresh", "request", ref)
	if mustStringPath(t, queued.Payload, "data.refresh.requested_at") != mustStringPath(t, again.Payload, "data.refresh.requested_at") {
		t.Fatal("refresh requests did not coalesce")
	}
	h.runCLIExpectOK(t, "reporter", nil, "work", "refresh", "get", ref)
	// Source-owned fields cannot bypass observation/PM boundaries through metadata.
	denied := h.runCLI(t, "reporter", map[string]any{"if_version": mustIntPath(t, current.Payload, "data.work.version"), "patch": map[string]any{"phase": "done"}}, "work", "patch", ref, "--from-file", "-")
	if denied.ExitCode == 0 {
		t.Fatalf("external status mutation bypass: %s", denied.Stdout)
	}
}

func TestUnifiedWorkPaginationAndWorkspaceAuthorization(t *testing.T) {
	h := newLiveCoreHarness(t)
	h.registerAgentBootstrap(t, "owner", "owner."+runToken())
	board := h.runCLIExpectOK(t, "owner", map[string]any{"board": map[string]any{"title": "Synthetic pagination", "document_refs": []any{}, "pinned_refs": []any{}, "provenance": map[string]any{"sources": []any{"inferred"}}}}, "boards", "create")
	boardRef := mustStringPath(t, board.Payload, "data.board.ref")
	refs := map[string]bool{}
	for i := 0; i < 3; i++ {
		w := h.runCLIExpectOK(t, "owner", map[string]any{"board_ref": boardRef, "title": fmt.Sprintf("Synthetic pagination %d", i)}, "work", "create", "--from-file", "-")
		refs[mustStringPath(t, w.Payload, "data.work.ref")] = true
	}
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < 4; page++ {
		args := []string{"work", "list", "--source", "nexus", "--limit", "1"}
		if cursor != "" {
			args = append(args, "--cursor", cursor)
		}
		result := h.runCLIExpectOK(t, "owner", nil, args...)
		raw, _ := getPathValue(result.Payload, "data.work")
		rows, ok := raw.([]any)
		if !ok {
			t.Fatalf("missing work page: %s", result.Stdout)
		}
		for _, row := range rows {
			ref := fmt.Sprint(row.(map[string]any)["ref"])
			if seen[ref] {
				t.Fatalf("duplicate page item %s", ref)
			}
			seen[ref] = true
		}
		raw, _ = getPathValue(result.Payload, "data.next_cursor")
		cursor, _ = raw.(string)
		if cursor == "" {
			break
		}
	}
	if len(seen) != len(refs) {
		t.Fatalf("pagination omitted work: seen=%v expected=%v", seen, refs)
	}
	// A key authenticated in another central workspace must not read or report here.
	other := newLiveCoreHarness(t)
	other.registerAgentBootstrap(t, "foreign", "foreign."+runToken())
	foreign := *other
	foreign.baseURL = h.baseURL
	var ref string
	for id := range refs {
		ref = id
		break
	}
	for _, args := range [][]string{{"work", "list"}, {"work", "get", ref}, {"work", "observations", "submit", ref, "--from-file", "-"}, {"work", "refresh", "request", ref}} {
		result := foreign.runCLI(t, "foreign", map[string]any{"observation": map[string]any{}}, args...)
		if result.ExitCode == 0 {
			t.Fatalf("cross-workspace access succeeded: %s", result.Stdout)
		}
		code := mustStringPath(t, result.Payload, "error.code")
		if code != "invalid_token" && code != "auth_required" && code != "unauthorized" && code != "key_mismatch" {
			t.Fatalf("expected auth rejection before resource handling, got %s: %s", code, result.Stdout)
		}
	}
}

func TestSecondMachineCLIObservationDedupSurvivesRestart(t *testing.T) {
	h := newLiveCoreHarness(t)
	h.registerAgentBootstrap(t, "machine-a", "machine-a."+runToken())
	h.registerAgentBootstrap(t, "machine-b", "machine-b."+runToken())
	board := h.runCLIExpectOK(t, "machine-a", map[string]any{"board": map[string]any{"title": "Synthetic second machine", "document_refs": []any{}, "pinned_refs": []any{}, "provenance": map[string]any{"sources": []any{"inferred"}}}}, "boards", "create")
	boardRef := mustStringPath(t, board.Payload, "data.board.ref")
	work := h.runCLIExpectOK(t, "machine-a", map[string]any{"board_ref": boardRef, "title": "Remote CLI commitment", "source": map[string]any{"authority": "github", "connection_id": "synthetic", "native_id": "fixture/repository/issues/second-machine"}}, "work", "create", "--from-file", "-")
	ref := mustStringPath(t, work.Payload, "data.work.ref")
	obs := map[string]any{"observation": map[string]any{"idempotency_key": "second-machine-cli", "reader_id": "synthetic-remote-cli", "reader_revision": "v1", "source_sequence": 4, "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "status": "reported", "facts": map[string]any{"phase": "in_progress", "native_status": "OPEN"}, "evidence": []any{map[string]any{"url": "https://example.test/fixture", "summary": "Synthetic remote CLI report"}}}}
	first := h.runCLIExpectOK(t, "machine-b", obs, "work", "observations", "submit", ref, "--from-file", "-")
	if got := mustStringPath(t, first.Payload, "data.observation.verification"); got != "reported" {
		t.Fatalf("second-machine CLI self-certified: %s", first.Stdout)
	}
	restartCoreForWorkTest(t, h)
	dup := h.runCLIExpectOK(t, "machine-b", obs, "work", "observations", "submit", ref, "--from-file", "-")
	if got, _ := getPathValue(dup.Payload, "data.duplicate"); got != true {
		t.Fatalf("restart lost second-machine CLI idempotency: %s", dup.Stdout)
	}
	listed := h.runCLIExpectOK(t, "machine-a", nil, "work", "observations", "list", ref, "--limit", "5")
	raw, ok := getPathValue(listed.Payload, "data.observations")
	rows, _ := raw.([]any)
	if !ok || len(rows) == 0 {
		t.Fatalf("central work lost remote CLI evidence: %s", listed.Stdout)
	}
	if fmt.Sprint(rows[0].(map[string]any)["id"]) != mustStringPath(t, first.Payload, "data.observation.id") {
		t.Fatalf("central work lost remote CLI evidence: %s", listed.Stdout)
	}
}
