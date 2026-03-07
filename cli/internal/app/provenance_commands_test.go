package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProvenanceWalkBuildsGraphAndReportsMissingRefs(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/events/event_root":
			_, _ = w.Write([]byte(`{"event":{"id":"event_root","refs":["snapshot:snapshot_1","artifact:artifact_missing"],"provenance":{"sources":["inferred"]}}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/snapshots/snapshot_1":
			_, _ = w.Write([]byte(`{"snapshot":{"id":"snapshot_1","refs":["artifact:artifact_1"],"provenance":{"sources":["inferred"]}}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/artifacts/artifact_1":
			_, _ = w.Write([]byte(`{"artifact":{"id":"artifact_1","kind":"evidence","refs":[]}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/artifacts/artifact_missing":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"artifact not found"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"provenance", "walk",
		"--from", "event:event_root",
		"--depth", "2",
	})
	payload := assertEnvelopeOK(t, raw)
	if got := anyStringValue(payload["command"]); got != "provenance walk" {
		t.Fatalf("unexpected command label: %#v", payload)
	}
	data, _ := payload["data"].(map[string]any)
	if got := anyStringValue(data["from"]); got != "event:event_root" {
		t.Fatalf("expected canonical start ref, got %#v", data)
	}
	if got := int(data["depth"].(float64)); got != 2 {
		t.Fatalf("expected depth=2, got %#v", data)
	}

	nodesByRef := provenanceNodesByRef(asSlice(data["nodes"]))
	if len(nodesByRef) != 3 {
		t.Fatalf("expected 3 resolved nodes, got %d payload=%#v", len(nodesByRef), payload)
	}
	if _, ok := nodesByRef["event:event_root"]; !ok {
		t.Fatalf("missing event root node payload=%#v", payload)
	}
	if _, ok := nodesByRef["snapshot:snapshot_1"]; !ok {
		t.Fatalf("missing snapshot node payload=%#v", payload)
	}
	artifactNode, ok := nodesByRef["artifact:artifact_1"]
	if !ok {
		t.Fatalf("missing artifact node payload=%#v", payload)
	}
	source, _ := artifactNode["source"].(map[string]any)
	if got := anyStringValue(source["command_id"]); got != "artifacts.get" {
		t.Fatalf("expected source command_id artifacts.get, got %#v", artifactNode)
	}
	if got := anyStringValue(source["path"]); got != "/artifacts/artifact_1" {
		t.Fatalf("expected source path /artifacts/artifact_1, got %#v", artifactNode)
	}

	edges := asSlice(data["edges"])
	if !provenanceHasEdge(edges, "event:event_root", "snapshot:snapshot_1", "refs") {
		t.Fatalf("expected event->snapshot edge, got %#v", edges)
	}
	if !provenanceHasEdge(edges, "snapshot:snapshot_1", "artifact:artifact_1", "refs") {
		t.Fatalf("expected snapshot->artifact edge, got %#v", edges)
	}
	if !provenanceHasEdge(edges, "event:event_root", "artifact:artifact_missing", "refs") {
		t.Fatalf("expected edge to unresolved artifact ref, got %#v", edges)
	}

	missingByRef := provenanceMissingByRef(asSlice(data["missing_refs"]))
	missing, ok := missingByRef["artifact:artifact_missing"]
	if !ok {
		t.Fatalf("expected missing ref artifact:artifact_missing, got %#v", data["missing_refs"])
	}
	if got := anyStringValue(missing["reason"]); got != "not_found" {
		t.Fatalf("expected missing reason not_found, got %#v", missing)
	}
}

func TestProvenanceWalkDepthOneStopsAtSingleHop(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/events/event_root":
			_, _ = w.Write([]byte(`{"event":{"id":"event_root","refs":["snapshot:snapshot_1"],"provenance":{"sources":["inferred"]}}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/snapshots/snapshot_1":
			_, _ = w.Write([]byte(`{"snapshot":{"id":"snapshot_1","refs":["artifact:artifact_1"],"provenance":{"sources":["inferred"]}}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/artifacts/artifact_1":
			_, _ = w.Write([]byte(`{"artifact":{"id":"artifact_1","kind":"evidence","refs":[]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"provenance", "walk",
		"--from", "event:event_root",
		"--depth", "1",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	nodesByRef := provenanceNodesByRef(asSlice(data["nodes"]))

	if _, ok := nodesByRef["event:event_root"]; !ok {
		t.Fatalf("missing root event node payload=%#v", payload)
	}
	if _, ok := nodesByRef["snapshot:snapshot_1"]; !ok {
		t.Fatalf("missing first-hop snapshot node payload=%#v", payload)
	}
	if _, ok := nodesByRef["artifact:artifact_1"]; ok {
		t.Fatalf("did not expect second-hop artifact node at depth=1 payload=%#v", payload)
	}
}

func TestProvenanceWalkIncludeEventChainAddsThreadEdge(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/events/event_root":
			_, _ = w.Write([]byte(`{"event":{"id":"event_root","thread_id":"thread_1","refs":[],"provenance":{"sources":["inferred"]}}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/threads/thread_1":
			_, _ = w.Write([]byte(`{"thread":{"id":"thread_1","status":"active","refs":[],"provenance":{"sources":["inferred"]}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"provenance", "walk",
		"--from", "event:event_root",
		"--depth", "1",
		"--include-event-chain",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)
	edges := asSlice(data["edges"])
	if !provenanceHasEdge(edges, "event:event_root", "thread:thread_1", "event.thread_id") {
		t.Fatalf("expected event.thread_id edge, got %#v", edges)
	}
	nodesByRef := provenanceNodesByRef(asSlice(data["nodes"]))
	if _, ok := nodesByRef["thread:thread_1"]; !ok {
		t.Fatalf("expected resolved thread node, got %#v", payload)
	}
}

func TestProvenanceWalkPreservesAllMissingRefContexts(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/events/event_root":
			_, _ = w.Write([]byte(`{"event":{"id":"event_root","refs":["artifact:artifact_missing"],"links":["artifact:artifact_missing"]}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/artifacts/artifact_missing":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"not_found","message":"artifact not found"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"provenance", "walk",
		"--from", "event:event_root",
		"--depth", "1",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)

	edges := asSlice(data["edges"])
	if !provenanceHasEdge(edges, "event:event_root", "artifact:artifact_missing", "refs") {
		t.Fatalf("expected refs edge to missing artifact, got %#v", edges)
	}
	if !provenanceHasEdge(edges, "event:event_root", "artifact:artifact_missing", "links") {
		t.Fatalf("expected links edge to missing artifact, got %#v", edges)
	}

	missing := asSlice(data["missing_refs"])
	if got := provenanceMissingCount(missing, "artifact:artifact_missing", "event:event_root", "refs", "not_found"); got != 1 {
		t.Fatalf("expected one refs missing context, got %d payload=%#v", got, payload)
	}
	if got := provenanceMissingCount(missing, "artifact:artifact_missing", "event:event_root", "links", "not_found"); got != 1 {
		t.Fatalf("expected one links missing context, got %d payload=%#v", got, payload)
	}
}

func TestProvenanceWalkTreatsInvalidDescendantIDAsMissingRef(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/events/event_root":
			_, _ = w.Write([]byte(`{"event":{"id":"event_root","refs":["artifact:bad id"]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--json",
		"--base-url", server.URL,
		"provenance", "walk",
		"--from", "event:event_root",
		"--depth", "1",
	})
	payload := assertEnvelopeOK(t, raw)
	data, _ := payload["data"].(map[string]any)

	edges := asSlice(data["edges"])
	if !provenanceHasEdge(edges, "event:event_root", "artifact:bad id", "refs") {
		t.Fatalf("expected edge to invalid descendant id, got %#v", edges)
	}
	missing := asSlice(data["missing_refs"])
	if got := provenanceMissingCount(missing, "artifact:bad id", "event:event_root", "refs", "invalid_ref_id"); got != 1 {
		t.Fatalf("expected invalid_ref_id missing context, got %d payload=%#v", got, payload)
	}
}

func TestProvenanceWalkRequiresFromFlag(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "provenance", "walk"})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "invalid_request" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	if got := anyStringValue(errObj["message"]); !strings.Contains(got, "--from is required") {
		t.Fatalf("expected --from guidance, got %#v", payload)
	}
}

func TestProvenanceWalkRejectsUnsupportedRootType(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	raw := runCLIForTest(t, home, map[string]string{}, nil, []string{"--json", "provenance", "walk", "--from", "inbox:item_1"})
	payload := assertEnvelopeError(t, raw)
	errObj, _ := payload["error"].(map[string]any)
	if errObj == nil || anyStringValue(errObj["code"]) != "invalid_request" {
		t.Fatalf("unexpected error payload: %#v", payload)
	}
	if got := anyStringValue(errObj["message"]); !strings.Contains(got, `unsupported provenance ref type "inbox"`) {
		t.Fatalf("expected unsupported root-type guidance, got %#v", payload)
	}
}

func TestProvenanceWalkHonorsVerboseAndHeadersFlags(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/events/event_root" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"event":{"id":"event_root","refs":[],"provenance":{"sources":["inferred"]}}}`))
	}))
	defer server.Close()

	home := t.TempDir()

	summary := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"provenance", "walk",
		"--from", "event:event_root",
		"--depth", "0",
	})
	if !strings.Contains(summary, "Provenance walk event:event_root") || strings.Contains(summary, `"nodes"`) {
		t.Fatalf("expected summary output in default mode, got:\n%s", summary)
	}

	verbose := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"--verbose",
		"provenance", "walk",
		"--from", "event:event_root",
		"--depth", "0",
	})
	if !strings.Contains(verbose, `"nodes":`) || !strings.Contains(verbose, `"event:event_root"`) {
		t.Fatalf("expected JSON body in verbose mode, got:\n%s", verbose)
	}

	withHeaders := runCLIForTest(t, home, map[string]string{}, nil, []string{
		"--base-url", server.URL,
		"--headers",
		"provenance", "walk",
		"--from", "event:event_root",
		"--depth", "0",
	})
	if !strings.Contains(withHeaders, "status: 200") || !strings.Contains(withHeaders, "header Content-Type: application/json") {
		t.Fatalf("expected header framing in --headers mode, got:\n%s", withHeaders)
	}
}

func provenanceNodesByRef(nodes []any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(nodes))
	for _, raw := range nodes {
		node, _ := raw.(map[string]any)
		if node == nil {
			continue
		}
		ref := strings.TrimSpace(anyStringValue(node["ref"]))
		if ref == "" {
			continue
		}
		out[ref] = node
	}
	return out
}

func provenanceMissingByRef(items []any) map[string]map[string]any {
	out := make(map[string]map[string]any, len(items))
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item == nil {
			continue
		}
		ref := strings.TrimSpace(anyStringValue(item["ref"]))
		if ref == "" {
			continue
		}
		out[ref] = item
	}
	return out
}

func provenanceHasEdge(edges []any, from string, to string, relation string) bool {
	for _, raw := range edges {
		edge, _ := raw.(map[string]any)
		if edge == nil {
			continue
		}
		if strings.TrimSpace(anyStringValue(edge["from"])) != from {
			continue
		}
		if strings.TrimSpace(anyStringValue(edge["to"])) != to {
			continue
		}
		if strings.TrimSpace(anyStringValue(edge["relation"])) != relation {
			continue
		}
		return true
	}
	return false
}

func provenanceMissingCount(items []any, ref string, from string, relation string, reason string) int {
	count := 0
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item == nil {
			continue
		}
		if strings.TrimSpace(anyStringValue(item["ref"])) != ref {
			continue
		}
		if strings.TrimSpace(anyStringValue(item["from"])) != from {
			continue
		}
		if strings.TrimSpace(anyStringValue(item["relation"])) != relation {
			continue
		}
		if strings.TrimSpace(anyStringValue(item["reason"])) != reason {
			continue
		}
		count++
	}
	return count
}
