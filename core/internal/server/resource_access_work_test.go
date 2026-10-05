package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"agent-nexus-core/internal/primitives"
)

func TestResourceAccessWorkMetadataReferences(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	s := env.primitiveStore.(*primitives.Store)
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "work-owner", "work-owner-actor", "work-owner", "work-owner-token")
	stranger := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "work-stranger", "work-stranger-actor", "work-stranger", "work-stranger-token")
	agent := seedMachinePrincipalForLockoutTest(t, ctx, env.workspace.DB(), "work-agent", "work-agent-actor", "work.agent", "work-agent-token")
	publicBoard, err := s.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Public work"})
	if err != nil {
		t.Fatal(err)
	}
	privateBoard, err := s.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Private work"})
	if err != nil {
		t.Fatal(err)
	}
	private, err := s.CreateWork(ctx, owner.ActorID, anyString(privateBoard["id"]), map[string]any{"title": "Private child"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, owner.ActorID, anyString(privateBoard["thread_id"]), map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	control, err := s.CreateWork(ctx, owner.ActorID, anyString(publicBoard["id"]), map[string]any{"title": "Public control"})
	if err != nil {
		t.Fatal(err)
	}
	read := func(path, token string, status int) string {
		t.Helper()
		resp := getJSONExpectStatusWithAuth(t, env.server.URL+path, token, status)
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	for _, field := range []string{"plan", "relations"} {
		t.Run(field, func(t *testing.T) {
			title := "MetadataSecret" + field
			work, err := s.CreateWork(ctx, owner.ActorID, anyString(publicBoard["id"]), map[string]any{"title": title})
			if err != nil {
				t.Fatal(err)
			}
			path := "/work/" + anyString(work["ref"])
			read(path, stranger.AccessToken, http.StatusOK)
			patch := map[string]any{}
			clear := map[string]any{}
			if field == "plan" {
				patch[field] = map[string]any{"steps": []any{map[string]any{"id": "step", "title": "StoredSecretStep", "ref": private["ref"]}}}
				clear[field] = map[string]any{"steps": []any{}}
			} else {
				patch[field] = []any{map[string]any{"kind": "related", "ref": private["ref"], "note": "StoredSecretStep"}}
				clear[field] = []any{}
			}
			resp := patchJSONExpectStatusWithAuth(t, env.server.URL+path, map[string]any{"if_version": work["version"], "patch": patch}, owner.AccessToken, http.StatusOK)
			var updated struct {
				Work map[string]any `json:"work"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&updated); err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			ownerBody := read(path, owner.AccessToken, http.StatusOK)
			if !strings.Contains(ownerBody, anyString(private["ref"])) || !strings.Contains(ownerBody, "StoredSecretStep") {
				t.Fatalf("owner lost metadata: %s", ownerBody)
			}
			for _, token := range []string{stranger.AccessToken, agent.AccessToken} {
				read(path, token, http.StatusNotFound)
				read("/cards/"+anyString(work["ref"]), token, http.StatusNotFound)
				read("/cards/"+anyString(private["ref"]), token, http.StatusNotFound)
				for _, collection := range []string{"/work", "/work?limit=1", "/work?q=", "/work?q=" + title, "/work?q=StoredSecretStep", "/work?q=" + url.QueryEscape(anyString(private["ref"])), "/cards", "/events"} {
					body := read(collection, token, http.StatusOK)
					for _, secret := range []string{anyString(work["ref"]), anyString(private["ref"]), title, "StoredSecretStep"} {
						if strings.Contains(body, secret) {
							t.Errorf("%s leaks %s: %s", collection, secret, body)
						}
					}
					if collection == "/work" && !strings.Contains(body, anyString(control["ref"])) {
						t.Errorf("public control missing: %s", body)
					}
					if collection == "/work?limit=1" {
						var page struct {
							Work []any `json:"work"`
						}
						if err := json.Unmarshal([]byte(body), &page); err != nil || len(page.Work) != 1 {
							t.Errorf("private row consumed public page limit: %s (%v)", body, err)
						}
					}
				}
				body := read("/work?q=StoredSecretStep", token, http.StatusOK)
				var result struct {
					Work []any `json:"work"`
				}
				if err := json.Unmarshal([]byte(body), &result); err != nil || len(result.Work) != 0 {
					t.Errorf("search matched hidden step title: %s (%v)", body, err)
				}
			}
			// Replacing metadata removes the live ownership edge. Old audit events
			// retain their own private payload edges.
			patchJSONExpectStatusWithAuth(t, env.server.URL+path, map[string]any{"if_version": updated.Work["version"], "patch": clear}, owner.AccessToken, http.StatusOK).Body.Close()
			for _, token := range []string{stranger.AccessToken, agent.AccessToken} {
				read(path, token, http.StatusOK)
			}
		})
	}
}
