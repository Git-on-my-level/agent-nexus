package server

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
	"encoding/json"
)

func TestResourceAccessObservationEvidenceHTTP(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	s := env.primitiveStore.(*primitives.Store)
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "obs-owner", "obs-owner-actor", "obs-owner", "obs-owner-token")
	stranger := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "obs-stranger", "obs-stranger-actor", "obs-stranger", "obs-stranger-token")
	agent := seedMachinePrincipalForLockoutTest(t, ctx, env.workspace.DB(), "obs-agent", "obs-agent-actor", "obs.agent", "obs-agent-token")
	private, err := s.CreateWork(ctx, owner.ActorID, "", map[string]any{"title": "private observation source"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, owner.ActorID, anyString(private["thread_id"]), map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	// Enrollment metadata is another arbitrary structured storage surface. Both
	// its GET and transaction-backed decision response must retain the scope.
	adoptions, _ := json.Marshal([]any{map[string]any{"ref": private["ref"], "title": "EnrollmentSecret"}})
	if _, err = env.workspace.DB().Exec(`INSERT INTO host_enrollments(id,user_code,poll_token_hash,public_key,requested_slug,os_user,hostname,discovered_adapters_json,request_nonce,adoptions_json,requesting_ip,status,created_at,expires_at) VALUES('private-enrollment','private-code','hash','key','private-host','test','host','[]','nonce',?,'','pending','2026-01-01T00:00:00Z','2999-01-01T00:00:00Z')`, string(adoptions)); err != nil {
		t.Fatal(err)
	}
	for _, actor := range []string{stranger.ActorID, agent.ActorID} {
		rows, count, err := env.authStore.PendingHostEnrollmentsPage(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: actor}), 10)
		if err != nil || count != 0 || len(rows) != 0 {
			t.Fatalf("private enrollment leaked: %#v %d %v", rows, count, err)
		}
	}

	postJSONExpectStatusWithAuth(t, env.server.URL+"/auth/hosts/enrollments/private-enrollment/approve", nil, stranger.AccessToken, 404).Body.Close()
	if _, err = env.authStore.DecideHostEnrollment(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: owner.ActorID}), "private-enrollment", false, auth.Principal{AgentID: owner.AgentID, ActorID: owner.ActorID, PrincipalKind: "human"}); err != nil {
		t.Fatalf("owner cannot decide private enrollment: %v", err)
	}
	public, err := s.CreateWork(ctx, owner.ActorID, "", map[string]any{"title": "public observed work", "source": map[string]any{"authority": "github", "connection_id": "fixture", "native_id": "public-observed"}})
	if err != nil {
		t.Fatal(err)
	}
	control, err := s.CreateWork(ctx, owner.ActorID, "", map[string]any{"title": "public observation control"})
	if err != nil {
		t.Fatal(err)
	}
	path := "/work/" + anyString(public["ref"])
	getJSONExpectStatusWithAuth(t, env.server.URL+path, stranger.AccessToken, 200).Body.Close()
	body := map[string]any{"observation": map[string]any{"idempotency_key": "private-evidence", "reader_id": "fixture", "reader_revision": "1", "observed_at": time.Now().UTC().Format(time.RFC3339Nano), "status": "reported", "facts": map[string]any{"title": "ObservationSecret derived title"}, "evidence": []any{map[string]any{"ref": private["ref"], "title": "ObservationSecret evidence title"}}}}
	postJSONExpectStatusWithAuth(t, env.server.URL+path+"/observations", body, owner.AccessToken, http.StatusOK).Body.Close()
	for _, suffix := range []string{"", "/observations"} {
		resp := getJSONExpectStatusWithAuth(t, env.server.URL+path+suffix, owner.AccessToken, 200)
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if !strings.Contains(string(data), "ObservationSecret") {
			t.Fatalf("owner evidence missing: %s", data)
		}
	}
	for _, token := range []string{stranger.AccessToken, agent.AccessToken} {
		for _, p := range []string{path, path + "/observations", "/cards/" + anyString(public["ref"]), "/cards/" + anyString(private["ref"])} {
			getJSONExpectStatusWithAuth(t, env.server.URL+p, token, 404).Body.Close()
		}
		for _, p := range []string{"/work", "/work?q=", "/work?q=ObservationSecret", "/work?limit=1", "/events"} {
			resp := getJSONExpectStatusWithAuth(t, env.server.URL+p, token, 200)
			data, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			for _, secret := range []string{"ObservationSecret", anyString(private["ref"]), anyString(public["ref"])} {
				if strings.Contains(string(data), secret) {
					t.Errorf("%s leaked %s: %s", p, secret, data)
				}
			}
			if p == "/work" && !strings.Contains(string(data), anyString(control["ref"])) {
				t.Errorf("public control missing: %s", data)
			}
		}
	}
}
