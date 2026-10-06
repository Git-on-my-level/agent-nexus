package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
)

func TestBatchResolutionKeepsVisibleSharedEvidenceAndInputEntries(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "shared-owner", "shared-owner", "Owner", "shared-owner-token")
	reader := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "shared-reader", "shared-reader", "Reader", "shared-reader-token")
	agent := seedNotificationTestAgent(t, env, "shared.otherhost")
	s := env.primitiveStore.(*primitives.Store)
	private, err := s.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Confidential shared board"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, owner.ActorID, anyString(private["thread_id"]), map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	public, err := s.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Public shared board"})
	if err != nil {
		t.Fatal(err)
	}
	const shared = "SHARED-ROUND6"
	var privateCard, publicCard map[string]any
	for _, seed := range []struct {
		board, identity, title, status string
		aliases                        []string
		dst                            *map[string]any
	}{
		{anyString(private["id"]), "confidential-source", "Confidential evidence", "done", []string{shared, "PRIVATE-ONLY"}, &privateCard},
		{anyString(public["id"]), "public-source", "Visible evidence", "in_progress", []string{shared}, &publicCard},
	} {
		*seed.dst, err = s.CreateWork(ctx, owner.ActorID, seed.board, map[string]any{"title": seed.title, "source_refs": []any{map[string]any{"authority": "tracker", "connection_id": seed.identity, "native_id": seed.identity, "title": seed.title, "status": seed.status, "aliases": seed.aliases}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	refs := []string{shared, "UNKNOWN-ROUND6", shared, "PRIVATE-ONLY", anyString(privateCard["ref"]), anyString(publicCard["ref"])}
	for _, principal := range []struct{ ActorID, AccessToken string }{{reader.ActorID, reader.AccessToken}, {agent.ActorID, agent.AccessToken}} {
		t.Run(principal.ActorID, func(t *testing.T) {
			getJSONExpectStatusWithAuth(t, env.server.URL+"/cards/"+anyString(privateCard["id"]), principal.AccessToken, 404).Body.Close()
			resp := postJSONExpectStatusWithAuth(t, env.server.URL+"/refs/resolve", map[string]any{"refs": refs}, principal.AccessToken, 200)
			raw, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			var result struct {
				Items []map[string]any `json:"items"`
			}
			if err = json.Unmarshal(raw, &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Items) != len(refs) {
				t.Fatalf("batch entries lost: %s", raw)
			}
			for i, ref := range refs {
				if result.Items[i]["ref"] != ref {
					t.Fatalf("input order changed at %d: %s", i, raw)
				}
			}
			item := result.Items[0]
			if item["resolvable"] != true || item["authority"] != "tracker" || item["native_id"] != "public-source" || item["connection_id"] != "public-source" || item["title"] != "Visible evidence" || item["status"] != "in_progress" {
				t.Fatalf("visible shared evidence missing: %s", raw)
			}
			if !reflect.DeepEqual(item, result.Items[2]) || result.Items[5]["resolvable"] != true {
				t.Fatalf("duplicate or visible native entry lost: %s", raw)
			}
			for _, i := range []int{1, 3, 4} {
				if !reflect.DeepEqual(result.Items[i], map[string]any{"ref": refs[i], "resolvable": false}) {
					t.Fatalf("unknown/private entry %d exposed metadata: %s", i, raw)
				}
			}
			round4NoSecrets(t, "shared evidence batch", string(raw), []string{"Confidential", "confidential-source", anyString(privateCard["thread_id"])})
			// Resolution is a read; publishing a plan reference still requires
			// every inherited owner, even when the same key has public evidence.
			patch := map[string]any{"actor_id": principal.ActorID, "if_updated_at": publicCard["updated_at"], "plan": map[string]any{"steps": []any{map[string]any{"id": "private", "title": "attempt", "ref": shared}}}}
			body, _ := json.Marshal(patch)
			req, _ := http.NewRequest("PUT", env.server.URL+"/cards/"+anyString(publicCard["id"])+"/plan", strings.NewReader(string(body)))
			req.Header.Set("Authorization", "Bearer "+principal.AccessToken)
			req.Header.Set("Content-Type", "application/json")
			write, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			write.Body.Close()
			if write.StatusCode != 404 {
				t.Fatalf("shared private ownership lost on write: %d", write.StatusCode)
			}
			postJSONExpectStatusWithAuth(t, env.server.URL+"/refs/resolve", map[string]any{"refs": []string{shared + "\x00"}}, principal.AccessToken, 400).Body.Close()
		})
	}
	getJSONExpectStatusWithAuth(t, env.server.URL+"/cards/"+anyString(privateCard["id"]), owner.AccessToken, 200).Body.Close()
	var plans int
	if err = env.workspace.DB().QueryRow(`SELECT count(*) FROM card_plans WHERE card_id=?`, publicCard["id"]).Scan(&plans); err != nil || plans != 0 {
		t.Fatalf("denied plan write persisted: plans=%d err=%v", plans, err)
	}
}

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
