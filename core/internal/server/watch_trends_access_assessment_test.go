package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

// Discovery remains scoped; the Trends eligibility status intentionally exposes
// the empty-versus-nonempty denial set, as documented in the round-4 assessment.
func TestWatchTrendsFirstHiddenResourceExistence(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	store := env.primitiveStore.(*primitives.Store)
	viewer := seedHumanPrincipalForLockoutTest(t, ctx, db, "existence-viewer", "existence-viewer", "existence-viewer", "existence-viewer-token")
	owner := seedHumanPrincipalForLockoutTest(t, ctx, db, "existence-owner", "existence-owner", "existence-owner", "existence-owner-token")

	// Independently check the root-index fact alongside the real HTTP outcome.
	denied := func(want bool) {
		t.Helper()
		var got bool
		err := db.QueryRow(`SELECT EXISTS(SELECT 1 FROM resource_access_admission_root_owners WHERE unconditional=1 LIMIT 1)
			OR EXISTS(SELECT 1 FROM resource_access_admission_root_owners WHERE unconditional=0 AND owner<? LIMIT 1)
			OR EXISTS(SELECT 1 FROM resource_access_admission_root_owners WHERE unconditional=0 AND owner>? LIMIT 1)`, viewer.ActorID, viewer.ActorID).Scan(&got)
		if err != nil || got != want {
			t.Fatalf("denied-root bit=%v want=%v: %v", got, want, err)
		}
	}
	paths := []string{"/boards", "/threads", "/work", "/docs", "/events", "/series"}
	snapshot := func() map[string]any {
		t.Helper()
		out := map[string]any{}
		for _, path := range paths {
			status, body := hostHTTP(t, http.MethodGet, env.server.URL+path, viewer.AccessToken, nil)
			if status != http.StatusOK {
				t.Fatalf("%s: %d %v", path, status, body)
			}
			out[path] = body
		}
		return out
	}
	denied(false)
	before := snapshot()
	trendsResponseBody(t, env.server.URL+"/watch/trends?group=decisions", viewer.AccessToken, http.StatusOK)
	board, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Undiscoverable board"})
	if err != nil {
		t.Fatal(err)
	}
	thread := anyString(board["thread_id"])
	if _, err = store.PatchThread(ctx, owner.ActorID, thread, map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	denied(true)
	if after := snapshot(); !reflect.DeepEqual(before, after) {
		oldJSON, _ := json.Marshal(before)
		newJSON, _ := json.Marshal(after)
		t.Fatalf("discovery exposed first hidden resource:\nbefore %s\nafter %s", oldJSON, newJSON)
	}
	trendsBody(t, env.server.URL+"/watch/trends?group=decisions", viewer.AccessToken)
	var missingBody map[string]any
	for _, ref := range []string{anyString(board["ref"]), "board:existence-missing"} {
		status, body := hostHTTP(t, http.MethodGet, env.server.URL+"/boards/"+ref, viewer.AccessToken, nil)
		if status != http.StatusNotFound || body["error"].(map[string]any)["code"] != "not_found" {
			t.Fatalf("hidden/missing lookup %s: %d %v", ref, status, body)
		}
		if missingBody != nil {
			// Record the existing disclosure without requiring its error wording
			// to remain a product contract. The assessment names this limitation.
			t.Logf("existing hidden/missing board responses: %v / %v", missingBody, body)
		}
		missingBody = body
		status, body = hostHTTP(t, http.MethodPost, env.server.URL+"/refs/resolve", viewer.AccessToken, map[string]any{"refs": []string{ref}})
		want := map[string]any{"items": []any{map[string]any{"ref": ref, "resolvable": false}}}
		if status != http.StatusOK || !reflect.DeepEqual(body, want) {
			t.Fatalf("hidden/missing ref %s: %d %v", ref, status, body)
		}
	}
	// A transfer grants full visibility on the next request.
	if _, err = store.PatchThread(ctx, owner.ActorID, thread, map[string]any{"pm_actor_id": viewer.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	denied(false)
	trendsResponseBody(t, env.server.URL+"/watch/trends?group=decisions", viewer.AccessToken, http.StatusOK)
	// Removing privacy also restores access to the former owner immediately.
	if _, err = store.PatchThread(ctx, viewer.ActorID, thread, map[string]any{"pm_actor_id": ""}, nil); err != nil {
		t.Fatal(err)
	}
	trendsResponseBody(t, env.server.URL+"/watch/trends?group=decisions", owner.AccessToken, http.StatusOK)
}

func TestWatchTrendsDenialRootsRefreshEveryRequest(t *testing.T) {
	requireIntegrationTest(t)
	for _, root := range []struct{ name, insert, remove string }{
		{"unknown-artifact", `INSERT INTO artifacts(id,kind,created_at,created_by,content_type,content_hash,content_refs_json) VALUES('unknown','text','2026-01-01','owner','text/plain','unknown',NULL)`, `DELETE FROM artifacts WHERE id='unknown'`},
		{"retained-tombstone", `INSERT INTO resource_access_tombstones(kind,id,ref,owner) VALUES('card','retained','card:retained','another-owner')`, `DELETE FROM resource_access_tombstones WHERE id='retained'`},
	} {
		t.Run(root.name, func(t *testing.T) {
			env := newAuthIntegrationEnv(t, authIntegrationOptions{})
			viewer := seedHumanPrincipalForLockoutTest(t, context.Background(), env.workspace.DB(), "root-viewer", "root-viewer", "root-viewer", "root-viewer-token")
			read := func(status int) {
				for _, group := range primitives.WatchTrendGroups {
					trendsResponseBody(t, env.server.URL+"/watch/trends?group="+group, viewer.AccessToken, status)
				}
			}
			read(http.StatusOK)
			if _, err := env.workspace.DB().Exec(root.insert); err != nil {
				t.Fatal(err)
			}
			read(http.StatusForbidden)
			if _, err := env.workspace.DB().Exec(root.remove); err != nil {
				t.Fatal(err)
			}
			read(http.StatusOK)
		})
	}
}

func TestWatchTrendsMissingHTTPScopeFailsClosed(t *testing.T) {
	out := httptest.NewRecorder()
	handleGetWatchTrends(out, httptest.NewRequest(http.MethodGet, "/watch/trends?group=decisions", nil), handlerOptions{})
	if out.Code != http.StatusForbidden {
		t.Fatalf("missing scope: %d %s", out.Code, out.Body.String())
	}
}

// The preexisting legacy provenance fallback exposes the same global bit in
// workspaces with uncertain series, without requiring a guessed private ref.
func TestWatchTrendsExistingLegacySeriesExistenceSignal(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	seedSeriesIdentities(t, env)
	token := declareSeriesHTTP(t, env)
	status, body := hostHTTP(t, http.MethodPost, env.server.URL+"/series/prs/points", token, map[string]any{"value": 42})
	hostStatus(t, status, http.StatusOK, body)
	if _, err := env.workspace.DB().Exec(`INSERT INTO resource_access_series_unknown(series,labels) VALUES('prs','{}')`); err != nil {
		t.Fatal(err)
	}
	read := func(present bool) {
		t.Helper()
		status, body := hostHTTP(t, http.MethodGet, env.server.URL+"/series/prs/query?range=1h&step=1m", "admin-token", nil)
		hostStatus(t, status, http.StatusOK, body)
		if (body["series"].(map[string]any)["last_push"] != nil) != present {
			t.Fatalf("existing legacy freshness disclosure: %v", body)
		}
	}
	read(true)
	if _, err := env.workspace.DB().Exec(`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('hidden-root',?,'another-owner','{"pm_actor_id":"another-owner"}')`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	read(false)
	if _, err := env.workspace.DB().Exec(`DELETE FROM threads WHERE id='hidden-root'`); err != nil {
		t.Fatal(err)
	}
	read(true)
}
