package server

import (
	"context"
	"io"
	"strings"
	"testing"

	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
)

func TestExternalEvidenceKeysProtectStoredPlansAcrossHTTPReaders(t *testing.T) {
	if testing.Short() {
		t.Skip("HTTP/storage integration")
	}
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "key-owner", "key-owner", "Owner", "key-owner-token")
	reader := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "key-reader", "key-reader", "Reader", "key-reader-token")
	s := env.primitiveStore.(*primitives.Store)
	private, err := s.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Private evidence board"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, owner.ActorID, anyString(private["thread_id"]), map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	public, err := s.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Public plans"})
	if err != nil {
		t.Fatal(err)
	}
	report := `{"kind":"anx.visual-report","schema_version":1,"title":"Public report","summary":"Work","generated_at":"2026-10-04T12:00:00Z","projects":[{"id":"work","title":"Work","summary":"Ship","outcome":"Delivery"}],"sources":[],"panels":[{"id":"progress","project_id":"work","type":"live-initiatives","title":"Progress","author":"Test","provenance":"reported","observed_at":null,"freshness":"unknown","source_ids":[],"data":{}}]}`
	doc, _, err := s.CreateDocument(ctx, owner.ActorID, map[string]any{"title": "Public report"}, report, "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	secrets := []string{}
	parents := []map[string]any{}
	for _, key := range []string{"opaque-private-step", "https://source.test/private/42", "github:team/repo#42"} {
		evidence := map[string]any{"authority": "tracker", "connection_id": "private", "native_id": key, "status": "in_progress"}
		switch {
		case strings.HasPrefix(key, "https:"):
			evidence["native_id"], evidence["url"] = "url-source", key
		case strings.HasPrefix(key, "github:"):
			evidence["authority"], evidence["native_id"] = "github", "team/repo#42"
		default:
			evidence["native_id"], evidence["aliases"] = "opaque-source", []string{key}
		}
		child, err := s.CreateWork(ctx, owner.ActorID, anyString(private["id"]), map[string]any{"title": "Confidential evidence " + key, "source_refs": []any{evidence}})
		if err != nil {
			t.Fatal(err)
		}
		resp := getJSONExpectStatusWithAuth(t, env.server.URL+"/cards/"+anyString(child["id"]), reader.AccessToken, 404)
		resp.Body.Close()
		parent, err := s.CreateWork(ctx, owner.ActorID, anyString(public["id"]), map[string]any{"title": "Public initiative " + anyString(evidence["native_id"])})
		if err != nil {
			t.Fatal(err)
		}
		title := "Confidential stored step " + key
		if err = s.SetCardPlan(ctx, owner.ActorID, anyString(parent["id"]), anyString(parent["updated_at"]), plans.Plan{Steps: []plans.Step{{ID: "secret", Title: title, Ref: key}}}); err != nil {
			t.Fatal(err)
		}
		parents = append(parents, parent)
		secrets = append(secrets, title, "\"ref\":\""+key+"\"")
		// Work-level references inherit the same ownership even if this work
		// also publishes the key. Publication fields cannot mask a real ref.
		linked, err := s.CreateWork(ctx, owner.ActorID, anyString(public["id"]), map[string]any{"title": "Confidential linked work", "source_refs": []any{map[string]any{"authority": "tracker", "connection_id": "public", "native_id": "public-" + key, "aliases": []string{key}, "extension": map[string]any{"context_ref": key}}}})
		if err != nil {
			t.Fatal(err)
		}
		resp = getJSONExpectStatusWithAuth(t, env.server.URL+"/work/"+anyString(linked["id"]), reader.AccessToken, 404)
		resp.Body.Close()
		secrets = append(secrets, "Confidential linked work")
	}
	paths := []string{"/work?limit=100", "/overview", "/docs/" + anyString(doc["id"]) + "/report"}
	for _, parent := range parents {
		paths = append(paths, "/cards/"+anyString(parent["id"])+"/plan")
	}
	for _, path := range paths {
		resp := getJSONExpectStatusWithAuth(t, env.server.URL+path, reader.AccessToken, 200)
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		round4NoSecrets(t, path, string(raw), secrets)
		resp = getJSONExpectStatusWithAuth(t, env.server.URL+path, owner.AccessToken, 200)
		raw, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		if !strings.Contains(string(raw), "Confidential stored step") {
			t.Fatalf("owner lost stored plan on %s: %s", path, raw)
		}
	}
	refs := []string{}
	for _, parent := range parents {
		refs = append(refs, anyString(parent["ref"]))
	}
	resp := postJSONExpectStatusWithAuth(t, env.server.URL+"/refs/resolve", map[string]any{"refs": refs}, reader.AccessToken, 200)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	round4NoSecrets(t, "ref preview next step", string(raw), secrets)
	if strings.Contains(string(raw), "\"next_step\"") {
		t.Fatalf("private step projected into preview: %s", raw)
	}
}
