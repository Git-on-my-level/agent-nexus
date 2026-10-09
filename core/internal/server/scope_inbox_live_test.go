package server

import (
	"agent-nexus-core/internal/primitives"
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestScopedInboxMountedReaderRetainsEnrichmentAndPrivacy(t *testing.T) {
	t.Parallel()
	env := newAuthIntegrationEnv(t, authIntegrationOptions{scopedInboxReader: true})
	ctx := context.Background()
	db := env.workspace.DB()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, db, "live-owner", "live-owner-actor", "live.owner", "live-owner-token")
	stranger := seedHumanPrincipalForLockoutTest(t, ctx, db, "live-stranger", "live-stranger-actor", "live.stranger", "live-stranger-token")
	s := env.primitiveStore.(*primitives.Store)
	public, err := s.CreateThread(ctx, owner.ActorID, map[string]any{"title": "Public"})
	if err != nil {
		t.Fatal(err)
	}
	private, err := s.CreateThread(ctx, owner.ActorID, map[string]any{"title": "Secret", "pm_actor_id": owner.ActorID})
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range []struct{ thread, id, title string }{{anyString(public.Thread["id"]), "live-public", "Visible request"}, {anyString(private.Thread["id"]), "live-private", "Private request"}} {
		item := streamPrivacyInboxItem(source.thread, source.id, source.title)
		item.Data["requester_actor_id"] = owner.ActorID
		if err = s.ReplaceDerivedInboxItems(ctx, source.thread, []primitives.DerivedInboxItem{item}); err != nil {
			t.Fatal(err)
		}
	}
	paths := []string{"/inbox?limit=1", "/inbox/summary?limit=1"}
	baseline := []map[string]any{}
	for _, path := range paths {
		baseline = append(baseline, scopeInboxHTTP(t, env, "live-stranger-token", path))
	}
	done := false
	for !done {
		done, err = env.workspace.MaintainScopeInboxBatch(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	for i, path := range paths {
		actual := scopeInboxHTTP(t, env, "live-stranger-token", path)
		delete(actual, "generated_at")
		delete(baseline[i], "generated_at")
		if !reflect.DeepEqual(actual, baseline[i]) {
			t.Fatalf("mounted parity %s: %#v != %#v", path, actual, baseline[i])
		}
		raw, _ := json.Marshal(actual)
		if strings.Contains(string(raw), "Private request") || strings.Contains(string(raw), "live-private") {
			t.Fatalf("private data exposed: %s", raw)
		}
	}
	// Owner can read the private ask; next request by another principal cannot.
	body := scopeInboxHTTP(t, env, "live-owner-token", "/inbox?limit=10")
	raw, _ := json.Marshal(body)
	if !strings.Contains(string(raw), "live-private") {
		t.Fatalf("owner missing private ask: %s", raw)
	}
	if d := s.ScopeInboxDiagnostics(); d.Served < 3 || d.NotBuilt != 2 {
		t.Fatalf("dispatcher was not exercised: %+v", d)
	}
	_ = stranger
}
