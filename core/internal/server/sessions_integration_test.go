package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

func sessionJSON(t *testing.T, r *http.Response) map[string]any {
	t.Helper()
	defer r.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(r.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}
func sessionInput(native string, seq int64) map[string]any {
	return map[string]any{"provider": "generic", "host_scope": "private-host", "native_session_id": native, "native_session_id_kind": "opaque", "sequence": seq, "activity": "active", "capabilities": map[string]any{"resume": "unsupported", "history": "unknown", "logs": "unsupported"}}
}
func TestGenericSessionsAuthenticationScopeAndOrdering(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{enableDevActorMode: true, allowUnauthenticatedWrites: true})
	ctx := context.Background()
	db := env.workspace.DB()
	owner := seedMachinePrincipalForLockoutTest(t, ctx, db, "session-agent", "session-actor", "session-agent", "session-token")
	other := seedMachinePrincipalForLockoutTest(t, ctx, db, "other-agent", "other-actor", "other-agent", "other-token")
	human := seedHumanPrincipalForLockoutTest(t, ctx, db, "session-human", "session-human-actor", "session-human", "session-human-token")
	post := func(body map[string]any, token string, status int) map[string]any {
		return sessionJSON(t, postJSONExpectStatusWithAuth(t, env.server.URL+"/sessions", body, token, status))
	}
	post(sessionInput("one", 0), "", 401)
	post(sessionInput("one", 0), human.AccessToken, 403)
	first := post(sessionInput("one", 0), owner.AccessToken, 200)["session"].(map[string]any)
	if first["agent_id"] != owner.AgentID || first["actor_id"] != owner.ActorID || first["active"] != true {
		t.Fatalf("binding %#v", first)
	}
	if !reflect.DeepEqual(first, post(sessionInput("one", 0), owner.AccessToken, 200)["session"]) {
		t.Fatal("exact replay changed timestamps or identity")
	}
	duplicate := sessionInput("one", 0)
	duplicate["activity"] = "idle"
	post(duplicate, owner.AccessToken, 409)
	newer := sessionInput("one", 2)
	newer["activity"] = "idle"
	second := post(newer, owner.AccessToken, 200)["session"].(map[string]any)
	if second["activity"] != "idle" || second["sequence"] != float64(2) {
		t.Fatal(second)
	}
	post(sessionInput("one", 1), owner.AccessToken, 409)
	sessionJSON(t, getJSONExpectStatusWithAuth(t, env.server.URL+"/sessions/"+first["session_id"].(string), other.AccessToken, 404))
	sessionJSON(t, getJSONExpectStatusWithAuth(t, env.server.URL+"/sessions/"+first["session_id"].(string), human.AccessToken, 404))
	otherSession := post(sessionInput("one", 0), other.AccessToken, 200)["session"].(map[string]any)
	if otherSession["session_id"] == first["session_id"] {
		t.Fatal("agents share private session identity")
	}
	for _, change := range []map[string]any{{"provider": "custom-runtime"}, {"host_scope": "second-host"}} {
		in := sessionInput("one", 0)
		for k, v := range change {
			in[k] = v
		}
		if post(in, owner.AccessToken, 200)["session"].(map[string]any)["session_id"] == first["session_id"] {
			t.Fatal("scope collision")
		}
	}
	for _, change := range []map[string]any{{"agent_id": other.AgentID}, {"actor_id": other.ActorID}, {"transcript": "private content"}, {"sequence": nil}, {"sequence": -1}, {"sequence": 1.5}, {"sequence": 9007199254740992}, {"provider": ""}, {"native_session_id_kind": "provider_session_sha256"}, {"capabilities": map[string]any{"resume": "yes", "history": "unknown", "logs": "unsupported"}}, {"capabilities": map[string]any{"resume": "unknown", "history": "unknown"}}, {"capabilities": map[string]any{"resume": "unknown", "history": "unknown", "logs": "unsupported", "transcript": "private"}}} {
		in := sessionInput("invalid", 0)
		for k, v := range change {
			in[k] = v
		}
		post(in, owner.AccessToken, 400)
	}
	hashed := sessionInput("sha256:"+strings.Repeat("a", 64), 0)
	hashed["native_session_id_kind"] = "provider_session_sha256"
	post(hashed, owner.AccessToken, 200)
	changedKind := sessionInput("one", 3)
	changedKind["native_session_id_kind"] = "provider_session_sha256"
	post(changedKind, owner.AccessToken, 400)
	closed := sessionInput("one", 3)
	closed["activity"] = "closed"
	post(closed, owner.AccessToken, 200)
	post(sessionInput("one", 4), owner.AccessToken, 409)
	// A lease expiring on read never changes the durable ordered report.
	old := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`UPDATE agent_sessions SET expires_at=? WHERE id=?`, old, otherSession["session_id"]); err != nil {
		t.Fatal(err)
	}
	stale := post(sessionInput("one", 0), other.AccessToken, 200)["session"].(map[string]any)
	if stale["activity"] != "stale" || stale["active"] != false || stale["expires_at"] != old {
		t.Fatal("replay renewed expired lease", stale)
	}
	// Authenticated enrollment is the authority for host namespace.
	_, err := db.Exec(`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) VALUES('actual-host','actual-slug','Host','test','test','[]',?)`, old)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO host_agents(host_id,name,agent_id,identity_kind) VALUES('actual-host','generic',?,'derived')`, owner.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	bound := sessionInput("enrolled", 0)
	delete(bound, "host_scope")
	registered := post(bound, owner.AccessToken, 200)["session"].(map[string]any)
	if registered["host_scope"] != "actual-host" {
		t.Fatal(registered)
	}
	bound["host_scope"] = "actual-slug"
	if !reflect.DeepEqual(registered, post(bound, owner.AccessToken, 200)["session"]) {
		t.Fatal("canonical host alias replay failed")
	}
	bound["host_scope"] = "forged-host"
	post(bound, owner.AccessToken, 403)
	ro := newReadOnlyAuthIntegrationServer(t, env)
	defer ro.Close()
	postJSONExpectStatusWithAuth(t, ro.URL+"/sessions", bound, owner.AccessToken, http.StatusLocked).Body.Close()
}

func TestWorkParticipantsNonlockingPrivateAndExpiring(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	store := env.primitiveStore.(*primitives.Store)
	owner := seedMachinePrincipalForLockoutTest(t, ctx, db, "participant-agent", "participant-actor", "participant-agent", "participant-token")
	other := seedMachinePrincipalForLockoutTest(t, ctx, db, "reader-agent", "reader-actor", "reader-agent", "reader-token")
	register := func(native string) string {
		return sessionJSON(t, postJSONExpectStatusWithAuth(t, env.server.URL+"/sessions", sessionInput(native, 0), owner.AccessToken, 200))["session"].(map[string]any)["session_id"].(string)
	}
	s1, s2 := register("session-one"), register("session-two")
	native, err := store.CreateWork(ctx, owner.ActorID, "", map[string]any{"title": "Native work"})
	if err != nil {
		t.Fatal(err)
	}
	external, err := store.CreateWork(ctx, owner.ActorID, "", map[string]any{"title": "Source work", "owner": "source-user", "phase": "review", "source": map[string]any{"authority": "github", "connection_id": "fixture", "native_id": "repo/issues/7"}})
	if err != nil {
		t.Fatal(err)
	}
	original := map[string]map[string]any{}
	for _, work := range []map[string]any{native, external} {
		v, e := store.GetWork(ctx, work["id"].(string))
		if e != nil {
			t.Fatal(e)
		}
		original[work["id"].(string)] = v
	}
	post := func(work map[string]any, sid, activity string, seq int64, token string, status int) map[string]any {
		return sessionJSON(t, postJSONExpectStatusWithAuth(t, fmt.Sprintf("%s/work/%s/participants", env.server.URL, work["ref"]), map[string]any{"session_id": sid, "activity": activity, "sequence": seq}, token, status))
	}
	list := func(work map[string]any, token string, status int, suffix string) map[string]any {
		return sessionJSON(t, getJSONExpectStatusWithAuth(t, fmt.Sprintf("%s/work/%s/participants%s", env.server.URL, work["ref"], suffix), token, status))
	}
	for _, work := range []map[string]any{native, external} {
		for _, sid := range []string{s1, s2} {
			post(work, sid, "active", 0, owner.AccessToken, 200)
		}
	}
	first := post(native, s1, "active", 0, owner.AccessToken, 200)["participant"].(map[string]any)
	if first["active"] != true || first["session_id"] != s1 {
		t.Fatal(first)
	}
	post(native, s1, "idle", 0, owner.AccessToken, 409)
	post(native, s1, "active", 0, other.AccessToken, 404)
	list(native, "", 401, "")
	for _, work := range []map[string]any{native, external} {
		view := list(work, other.AccessToken, 200, "")
		items := view["participants"].([]any)
		if len(items) != 2 {
			t.Fatal(view)
		}
		for _, item := range items {
			p := item.(map[string]any)
			for _, forbidden := range []string{"session_id", "native_session_id", "host_scope", "capabilities", "provider", "other_tasks"} {
				if _, exists := p[forbidden]; exists {
					t.Fatalf("private field %s leaked: %#v", forbidden, p)
				}
			}
		}
	}
	paged := list(native, owner.AccessToken, 200, "?limit=1")
	cursor := paged["next_cursor"].(string)
	if cursor == "" {
		t.Fatal("missing next cursor")
	}
	second := list(native, owner.AccessToken, 200, "?limit=1&cursor="+cursor)
	if second["next_cursor"] != "" || second["participants"].([]any)[0].(map[string]any)["participant_id"] == paged["participants"].([]any)[0].(map[string]any)["participant_id"] {
		t.Fatal(second)
	}
	list(external, owner.AccessToken, 400, "?cursor="+cursor)
	old := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`UPDATE work_participants SET expires_at=? WHERE card_id=? AND session_id=?`, old, native["id"], s1); err != nil {
		t.Fatal(err)
	}
	replay := post(native, s1, "active", 0, owner.AccessToken, 200)["participant"].(map[string]any)
	if replay["active"] != false || replay["activity"] != "stale" {
		t.Fatal(replay)
	}
	if got := post(external, s1, "active", 0, owner.AccessToken, 200)["participant"].(map[string]any); got["active"] != true {
		t.Fatal("other task lease overwritten", got)
	}
	heartbeat := sessionInput("session-one", 1)
	sessionJSON(t, postJSONExpectStatusWithAuth(t, env.server.URL+"/sessions", heartbeat, owner.AccessToken, 200))
	if got := post(native, s1, "active", 0, owner.AccessToken, 200)["participant"].(map[string]any); got["active"] != false {
		t.Fatal("session heartbeat renewed stale task", got)
	}
	post(native, s1, "active", 1, owner.AccessToken, 200)
	heartbeat["sequence"] = 2
	heartbeat["activity"] = "closed"
	sessionJSON(t, postJSONExpectStatusWithAuth(t, env.server.URL+"/sessions", heartbeat, owner.AccessToken, 200))
	if got := post(native, s1, "active", 1, owner.AccessToken, 200)["participant"].(map[string]any); got["activity"] != "closed" || got["active"] != false {
		t.Fatal(got)
	}
	post(native, s1, "active", 2, owner.AccessToken, 409)
	post(native, s1, "left", 2, owner.AccessToken, 200)
	for _, work := range []map[string]any{native, external} {
		v, e := store.GetWork(ctx, work["id"].(string))
		if e != nil {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(original[work["id"].(string)], v) {
			t.Fatalf("participation mutated work: before %#v after %#v", original[work["id"].(string)], v)
		}
	}

	// Session inactivity/expiry bounds every task, while unrelated sessions remain active.
	idle := sessionInput("session-two", 1)
	idle["activity"] = "idle"
	sessionJSON(t, postJSONExpectStatusWithAuth(t, env.server.URL+"/sessions", idle, owner.AccessToken, 200))
	if got := post(external, s2, "active", 0, owner.AccessToken, 200)["participant"].(map[string]any); got["activity"] != "idle" || got["active"] != false {
		t.Fatal("idle session still active", got)
	}
	active := sessionInput("session-two", 2)
	sessionJSON(t, postJSONExpectStatusWithAuth(t, env.server.URL+"/sessions", active, owner.AccessToken, 200))
	if _, err := db.Exec(`UPDATE agent_sessions SET expires_at=? WHERE id=?`, old, s2); err != nil {
		t.Fatal(err)
	}
	if got := post(external, s2, "active", 1, owner.AccessToken, 200)["participant"].(map[string]any); got["activity"] != "stale" || got["active"] != false {
		t.Fatal("task update renewed stale session", got)
	}
	// Task privacy is checked before exposing a participant list or allowing writes.
	if _, err := db.Exec(`UPDATE threads SET body_json=json_set(body_json,'$.pm_actor_id',?) WHERE id=?`, owner.ActorID, native["thread_id"]); err != nil {
		t.Fatal(err)
	}
	list(native, other.AccessToken, 404, "")
	list(native, owner.AccessToken, 200, "")
	post(native, s2, "active", 1, other.AccessToken, 404)
	// Board scope is independently checked, even for an otherwise public task.
	board, err := store.GetBoard(ctx, external["board_id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE threads SET body_json=json_set(body_json,'$.pm_actor_id',?) WHERE id=?`, owner.ActorID, board["thread_id"]); err != nil {
		t.Fatal(err)
	}
	list(external, other.AccessToken, 404, "")
}

func TestSessionConcurrentRegistrationAndParticipants(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	store := env.primitiveStore.(*primitives.Store)
	agent := seedMachinePrincipalForLockoutTest(t, ctx, db, "concurrent-agent", "concurrent-actor", "concurrent-agent", "concurrent-token")
	zero := int64(0)
	in := primitives.SessionRegistration{Provider: "generic", HostScope: "host", NativeSessionID: "native", Capabilities: primitives.SessionCapabilities{Resume: "unknown", History: "unknown", Logs: "unknown"}, Activity: "active", Sequence: &zero}
	work, err := store.CreateWork(ctx, agent.ActorID, "", map[string]any{"title": "Concurrent participation"})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	ids := make(chan string, 12)
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			session, e := store.UpsertSession(ctx, agent.AgentID, agent.ActorID, in)
			if e != nil {
				errs <- e
				return
			}
			p, e := store.UpsertWorkParticipant(ctx, agent.AgentID, work["id"].(string), primitives.WorkParticipantRegistration{SessionID: session.SessionID, Activity: "active", Sequence: &zero})
			if e != nil {
				errs <- e
				return
			}
			ids <- session.SessionID + ":" + p.ParticipantID
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		} else if first != id {
			t.Fatalf("concurrent upsert duplicated rows %s %s", first, id)
		}
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM work_participants`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("count %d error %v", count, err)
	}
}

func TestWorkParticipantProjectScope(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	store := env.primitiveStore.(*primitives.Store)
	owner := seedMachinePrincipalForLockoutTest(t, ctx, db, "project-owner", "project-owner-actor", "project-owner", "project-owner-token")
	other := seedMachinePrincipalForLockoutTest(t, ctx, db, "project-other", "project-other-actor", "project-other", "project-other-token")
	project, err := store.CreateTopic(ctx, owner.ActorID, map[string]any{"title": "Private project", "summary": "Private scope fixture"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateWork(ctx, owner.ActorID, "", map[string]any{"title": "Project task", "project_ref": project.Topic["ref"]})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE threads SET body_json=json_set(body_json,'$.pm_actor_id',?) WHERE id=?`, owner.ActorID, project.Topic["thread_id"]); err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("%s/work/%s/participants", env.server.URL, work["ref"])
	getJSONExpectStatusWithAuth(t, path, other.AccessToken, 404).Body.Close()
	getJSONExpectStatusWithAuth(t, path, owner.AccessToken, 200).Body.Close()
	// Missing/corrupt scope must not degrade to workspace-visible participation.
	if _, err = db.Exec(`DELETE FROM threads WHERE id=?`, project.Topic["thread_id"]); err != nil {
		t.Fatal(err)
	}
	getJSONExpectStatusWithAuth(t, path, owner.AccessToken, 404).Body.Close()
}

func TestSessionParticipationPurgeCleanup(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	store := env.primitiveStore.(*primitives.Store)
	owner := seedMachinePrincipalForLockoutTest(t, ctx, db, "purge-owner", "purge-owner-actor", "purge-owner", "purge-owner-token")
	response := sessionJSON(t, postJSONExpectStatusWithAuth(t, env.server.URL+"/sessions", sessionInput("native-purge", 0), owner.AccessToken, 200))
	sessionID := response["session"].(map[string]any)["session_id"].(string)
	work, err := store.CreateWork(ctx, owner.ActorID, "", map[string]any{"title": "Purge fixture"})
	if err != nil {
		t.Fatal(err)
	}
	seq := int64(0)
	_, err = store.UpsertWorkParticipant(ctx, owner.AgentID, work["id"].(string), primitives.WorkParticipantRegistration{SessionID: sessionID, Activity: "active", Sequence: &seq})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`UPDATE cards SET archived_at='2026-01-01T00:00:00Z' WHERE id=?`, work["id"]); err != nil {
		t.Fatal(err)
	}
	if err = store.PurgeArchivedBoardCard(ctx, work["board_id"].(string), work["id"].(string)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM work_participants WHERE card_id=?`, work["id"]).Scan(&count); err != nil || count != 0 {
		t.Fatalf("purged participation retained %d: %v", count, err)
	}
	if _, err = store.GetSession(ctx, owner.AgentID, sessionID); err != nil {
		t.Fatalf("purging a task deleted private session: %v", err)
	}
}

func TestSessionRoutesRejectUnsupportedMethodsAndPaths(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	store := env.primitiveStore.(*primitives.Store)
	agent := seedMachinePrincipalForLockoutTest(t, ctx, db, "route-agent", "route-actor", "route-agent", "route-token")
	// An enrolled agent makes X-ANX-Run-Id eligible to create provisional runs,
	// so the matrix also detects side effects before the handler rejects a route.
	if _, err := db.Exec(`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) VALUES('route-host','private-host','Route host','test','test','[]',?)`, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO host_agents(host_id,name,agent_id,identity_kind) VALUES('route-host','generic',?,'derived')`, agent.AgentID); err != nil {
		t.Fatal(err)
	}
	created := sessionJSON(t, postJSONExpectStatusWithAuth(t, env.server.URL+"/sessions", sessionInput("route-native", 0), agent.AccessToken, http.StatusOK))
	sessionID := created["session"].(map[string]any)["session_id"].(string)
	work, err := store.CreateWork(ctx, agent.ActorID, "", map[string]any{"title": "Route rejection fixture"})
	if err != nil {
		t.Fatal(err)
	}
	participantPath := "/work/" + work["ref"].(string) + "/participants"
	initialParticipant := map[string]any{"session_id": sessionID, "activity": "active", "sequence": 0}
	sessionJSON(t, postJSONExpectStatusWithAuth(t, env.server.URL+participantPath, initialParticipant, agent.AccessToken, http.StatusOK))
	beforeSession, err := store.GetSession(ctx, agent.AgentID, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	beforeParticipants, err := store.ListWorkParticipants(ctx, agent.AgentID, work["id"].(string), 50, "")
	if err != nil {
		t.Fatal(err)
	}
	beforeWork, err := store.GetWork(ctx, work["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	readOnly := newReadOnlyAuthIntegrationServer(t, env)
	defer readOnly.Close()
	for _, mode := range []struct{ name, url string }{{"read_write", env.server.URL}, {"read_only", readOnly.URL}} {
		t.Run(mode.name, func(t *testing.T) {
			requestNumber := 0
			request := func(method, path string, body map[string]any, status int, allow string) {
				t.Helper()
				encoded, err := json.Marshal(body)
				if err != nil {
					t.Fatal(err)
				}
				req, err := http.NewRequest(method, mode.url+path, strings.NewReader(string(encoded)))
				if err != nil {
					t.Fatal(err)
				}
				req.Header.Set("Authorization", "Bearer "+agent.AccessToken)
				req.Header.Set("Content-Type", "application/json")
				requestNumber++
				req.Header.Set("X-ANX-Run-Id", fmt.Sprintf("agentctl/rejected-%s-%d", mode.name, requestNumber))
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if resp.StatusCode != status || resp.Header.Get("Allow") != allow {
					t.Errorf("%s %s: status=%d Allow=%q; want %d %q", method, path, resp.StatusCode, resp.Header.Get("Allow"), status, allow)
				}
			}
			// Every request carries a valid newer report so a method/path fallthrough
			// would modify state rather than being masked by ordinary input validation.
			sessionReport := sessionInput("route-native", 1)
			participantReport := map[string]any{"session_id": sessionID, "activity": "idle", "sequence": 1}
			for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
				request(method, "/sessions", sessionReport, http.StatusMethodNotAllowed, "POST")
			}
			for _, method := range []string{http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
				request(method, "/sessions/"+sessionID, sessionReport, http.StatusMethodNotAllowed, "GET")
			}
			for _, method := range []string{http.MethodHead, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
				request(method, participantPath, participantReport, http.StatusMethodNotAllowed, "GET, POST")
			}
			for _, path := range []string{"/sessions/", "/sessions/" + sessionID + "/", "/sessions/arbitrary/path", "/sessions/" + sessionID + "/extra/deeper"} {
				for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
					request(method, path, sessionInput("must-not-register", 0), http.StatusNotFound, "")
				}
			}
			for _, path := range []string{participantPath + "/", participantPath + "/extra", participantPath + "/extra/deeper"} {
				for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
					request(method, path, participantReport, http.StatusNotFound, "")
				}
			}
			if mode.name == "read_only" {
				request(http.MethodPost, "/sessions", sessionReport, http.StatusLocked, "")
				request(http.MethodPost, participantPath, participantReport, http.StatusLocked, "")
			}
			// Valid reads retain their normal behavior in both workspace modes.
			request(http.MethodGet, "/sessions/"+sessionID, nil, http.StatusOK, "")
			request(http.MethodGet, participantPath, nil, http.StatusOK, "")
		})
	}
	afterSession, err := store.GetSession(ctx, agent.AgentID, sessionID)
	if err != nil || !reflect.DeepEqual(beforeSession, afterSession) {
		t.Fatalf("unsupported request mutated session: before=%#v after=%#v err=%v", beforeSession, afterSession, err)
	}
	afterParticipants, err := store.ListWorkParticipants(ctx, agent.AgentID, work["id"].(string), 50, "")
	if err != nil || !reflect.DeepEqual(beforeParticipants, afterParticipants) {
		t.Fatalf("unsupported request mutated participants: before=%#v after=%#v err=%v", beforeParticipants, afterParticipants, err)
	}
	afterWork, err := store.GetWork(ctx, work["id"].(string))
	if err != nil || !reflect.DeepEqual(beforeWork, afterWork) {
		t.Fatalf("unsupported request mutated work: before=%#v after=%#v err=%v", beforeWork, afterWork, err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM agent_sessions`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("unsupported subpath created session: count=%d err=%v", count, err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM runs`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("unsupported route created provisional run: count=%d err=%v", count, err)
	}
}
