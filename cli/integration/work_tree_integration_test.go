//go:build integration

package integration

import (
	"strings"
	"testing"
)

// The ordinary agent loop on the work tree: file work under an initiative,
// find a home for loose work with where, confirm it with mv, and read the tree
// and paths back. Nothing becomes an initiative without an explicit act.
func TestWorkTreeAgentLoopAgainstCore(t *testing.T) {
	h := newLiveCoreHarness(t)
	h.enrollHost(t, "worker")
	root := h.runCLIExpectOK(t, "worker", nil, "work", "create", "--title", "Webhook delivery", "--handle", "webhooks", "--initiative")
	rootRef := mustStringPath(t, root.Payload, "result.work.ref")
	child := h.runCLIExpectOK(t, "worker", nil, "work", "create", "--title", "Retry policy", "--handle", "retry-policy", "--under", rootRef)
	if got := mustStringPath(t, child.Payload, "result.work.parent_ref"); got != rootRef {
		t.Fatalf("create --under parent=%s: %s", got, child.Stdout)
	}
	loose := h.runCLIExpectOK(t, "worker", nil, "work", "create", "--title", "Webhook retries flaky", "--handle", "flaky-retries")
	looseRef := mustStringPath(t, loose.Payload, "result.work.ref")
	if v, _ := getPathValue(loose.Payload, "result.work.needs_home"); v != true {
		t.Fatalf("loose work should wait in Unsorted: %s", loose.Stdout)
	}

	where := h.runCLIExpectOK(t, "worker", nil, "where", looseRef)
	suggestions, ok := getPathValue(where.Payload, "result.suggestions")
	if !ok || len(suggestions.([]any)) == 0 || suggestions.([]any)[0].(map[string]any)["ref"] != rootRef {
		t.Fatalf("where should suggest the initiative: %s", where.Stdout)
	}

	// Paths resolve anywhere a card ref is accepted.
	h.runCLIExpectOK(t, "worker", nil, "mv", "unsorted/flaky-retries", "--under", "initiatives/webhooks")
	tree := h.runCLIExpectOK(t, "worker", nil, "tree")
	initiatives, ok := getPathValue(tree.Payload, "result.initiatives")
	if !ok || len(initiatives.([]any)) != 1 {
		t.Fatalf("tree initiatives: %s", tree.Stdout)
	}
	children := initiatives.([]any)[0].(map[string]any)["children"].([]any)
	paths := []string{}
	for _, c := range children {
		paths = append(paths, c.(map[string]any)["path"].(string))
	}
	if strings.Join(paths, ",") != "initiatives/webhooks/retry-policy,initiatives/webhooks/flaky-retries" {
		t.Fatalf("children paths=%v: %s", paths, tree.Stdout)
	}

	// Promotion is explicit and needs a reason.
	refused := h.runCLI(t, "worker", nil, "mv", looseRef, "--root")
	if refused.ExitCode == 0 {
		t.Fatalf("promotion without a reason succeeded: %s", refused.Stdout)
	}
	h.runCLIExpectOK(t, "worker", nil, "mv", looseRef, "--root", "--reason", "Separate reliability outcome")
	orient := h.runCLIExpectOK(t, "worker", nil, "orient")
	if !strings.Contains(orient.Stdout, "Webhook retries flaky") || !strings.Contains(orient.Stdout, "Webhook delivery") {
		t.Fatalf("orient should list both initiatives: %s", orient.Stdout)
	}
	// A top_level patch is no longer a silent no-op.
	patched := h.runCLI(t, "worker", map[string]any{"if_version": 1, "patch": map[string]any{"top_level": true}}, "work", "patch", rootRef, "--from-file", "-")
	if patched.ExitCode == 0 || !strings.Contains(patched.Stdout+patched.Stderr, "work move") {
		t.Fatalf("top_level patch should point at work move: %s %s", patched.Stdout, patched.Stderr)
	}
}
