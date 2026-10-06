package primitives_test

import (
	"agent-nexus-core/internal/primitives"
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestSourceRefsGenericAnnotationsAndMarkdownIsolation(t *testing.T) {
	ctx := context.Background()
	s, board := newWorkTestStore(t)
	evidence := []any{map[string]any{"authority": "github", "connection_id": "any-adapter", "native_id": "org/repo#42", "url": "https://github.com/org/repo/pull/42", "status": "merged", "observed_at": "2026-10-05T10:00:00Z", "custom": map[string]any{"retain": true}}}
	raw, _ := json.Marshal(evidence)
	// Core must ignore even a well-formed adapter markdown block.
	w, err := s.CreateWork(ctx, "actor-1", board, map[string]any{"title": "Generic evidence", "summary": "<!-- external-adapter:evidence:v1 -->\n<!-- external-adapter:data\n" + string(raw) + "\n-->\n<!-- /external-adapter:evidence -->"})
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(source string) {
		t.Helper()
		items, err := s.ResolveRefs(ctx, []string{"github:org/repo#42"}, nil, time.Now(), 0)
		if err != nil || items[0].Source != source {
			t.Fatalf("items=%v err=%v", items, err)
		}
	}
	resolve("parsed")
	updated, err := s.PatchWork(ctx, "actor-1", w["id"].(string), w["version"].(int64), map[string]any{"source_refs": evidence})
	if err != nil {
		t.Fatal(err)
	}
	resolve("evidence")
	if _, err = s.PatchWork(ctx, "actor-1", w["id"].(string), w["version"].(int64), map[string]any{"source_refs": []any{}}); err != primitives.ErrConflict {
		t.Fatalf("stale fence: %v", err)
	}
	updated, err = s.PatchWork(ctx, "actor-1", w["id"].(string), updated["version"].(int64), map[string]any{"next_action": "Preserve refs"})
	if err != nil || updated["source_refs"] == nil {
		t.Fatalf("work=%v err=%v", updated, err)
	}
	refs := updated["source_refs"].([]any)
	if refs[0].(map[string]any)["custom"].(map[string]any)["retain"] != true {
		t.Fatal(updated)
	}
	card, err := s.GetBoardCard(ctx, "", w["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.EnrichCardPlans(ctx, []map[string]any{card}, nil, time.Now(), 0); err != nil || len(card["source_refs"].([]any)) != 1 {
		t.Fatalf("card=%v err=%v", card, err)
	}
	if _, err = s.PatchWork(ctx, "actor-1", w["id"].(string), updated["version"].(int64), map[string]any{"source_refs": []any{}}); err != nil {
		t.Fatal(err)
	}
	resolve("parsed")
	for _, bad := range []any{nil, "wrong", []any{map[string]any{"authority": "any"}}, append(evidence, evidence[0]), []any{map[string]any{"authority": "any", "connection_id": "connection", "native_id": "opaque", "url": "file:///private"}}} {
		if err := primitives.ValidateWorkAnnotations(map[string]any{"source_refs": bad}); err == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
	// Authority and status remain open; the primitive is not a provider registry.
	if err := primitives.ValidateWorkAnnotations(map[string]any{"source_refs": []any{map[string]any{"authority": "other-provider", "connection_id": "connection", "native_id": "opaque", "status": "future-status"}}}); err != nil {
		t.Fatal(err)
	}
}
