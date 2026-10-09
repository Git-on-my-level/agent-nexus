package server

import (
	"agent-nexus-core/internal/commandcenter"
	"agent-nexus-core/internal/plans"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestResourceAccessPayloadHTTPAndPositiveAuthentication(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	s := env.primitiveStore.(*primitives.Store)
	owner := seedNotificationTestAgent(t, env, "owner.privatehost")
	stranger := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "payload-human", "payload-human-actor", "payload-human", "payload-human-token")
	agent := seedNotificationTestAgent(t, env, "other.otherhost")
	private, err := s.CreateWork(ctx, owner.ActorID, "", map[string]any{"title": "ReviewSecret child"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, owner.ActorID, anyString(private["thread_id"]), map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	public, err := s.CreateWork(ctx, owner.ActorID, "", map[string]any{"title": "public parent"})
	if err != nil {
		t.Fatal(err)
	}
	thread := anyString(public["thread_id"])
	topic, err := s.CreateTopic(ctx, owner.ActorID, map[string]any{"title": "public topic", "summary": "Public topic", "related_refs": []string{}})
	if err != nil {
		t.Fatal(err)
	}
	eventIDs := []string{}
	for _, payload := range []map[string]any{{"subject_ref": private["ref"], "text": "ReviewSecret subject"}, {"related_refs": []string{anyString(private["ref"])}, "text": "ReviewSecret related"}} {
		e, err := s.AppendEvent(ctx, owner.ActorID, map[string]any{"type": "message_posted", "thread_id": thread, "refs": []string{anyString(topic.Topic["ref"])}, "payload": payload})
		if err != nil {
			t.Fatal(err)
		}
		eventIDs = append(eventIDs, anyString(e["id"]))
	}
	if err = s.SetCardPlan(ctx, owner.ActorID, anyString(public["id"]), anyString(public["updated_at"]), plans.Plan{Steps: []plans.Step{{ID: "step", Title: "ReviewSecret stored step", Ref: anyString(private["ref"])}}}); err != nil {
		t.Fatal(err)
	}
	for _, w := range []primitives.AgentWakeup{
		{WakeupID: "review-trigger", TriggerEventID: eventIDs[0]},
		{WakeupID: "review-refs", Refs: []string{anyString(private["ref"])}},
	} {
		w.ThreadID = thread
		w.TargetActorID = agent.ActorID
		w.TargetHandle = agent.Username
		w.TriggerText = "ReviewSecret wakeup"
		w.Status = primitives.AgentWakeupStatusRequested
		seedReceiptStreamWakeup(t, s, w)
	}
	maintainer := NewProjectionMaintainer(ProjectionMaintainerConfig{PrimitiveStore: s})
	maintainer.recordError("rebuild", time.Now(), errors.New("ReviewSecret failure "+anyString(private["thread_id"])))
	contract, err := schema.Load("../../../contracts/anx-schema.yaml")
	if err != nil {
		t.Fatal(err)
	}
	env.server.Config.Handler = NewHandler("0.2.2", WithAuthStore(env.authStore), WithActorRegistry(env.registry), WithPrimitiveStore(s), WithSchemaContract(contract), WithRunStore(commandcenter.NewStore(env.workspace.DB(), commandcenter.SQLIdentities{DB: env.workspace.DB()})), WithProjectionMaintainer(maintainer), WithStreamPollInterval(10*time.Millisecond))
	request := func(method, path, token string) (int, string) {
		t.Helper()
		// Ordinary requests assert privacy, not latency; allow scheduler contention.
		// Streaming bodies still need the short deadline to terminate their reads.
		requestTimeout := 30 * time.Second
		if strings.HasPrefix(path, "/stream/") {
			requestTimeout = 5 * time.Second
		}
		c, cancel := context.WithTimeout(ctx, requestTimeout)
		defer cancel()
		r, err := http.NewRequestWithContext(c, method, env.server.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		if err != nil && !strings.HasPrefix(path, "/stream/") {
			t.Fatal(err)
		}
		return resp.StatusCode, string(b)
	}
	for _, token := range []string{stranger.AccessToken, agent.AccessToken} {
		for _, path := range []string{"/events", "/threads/" + thread + "/timeline", "/threads/" + thread + "/workspace", "/topics/" + anyString(topic.Topic["id"]) + "/timeline", "/stream/events", "/agent-notifications", "/stream/agent-notification-receipts?thread_id=" + thread, "/cards/" + anyString(public["id"]) + "/plan", "/cards/" + anyString(public["id"]), "/work", "/cards/" + anyString(public["id"]) + "/timeline", "/ops/health"} {
			status, body := request("GET", path, token)
			if status != 200 && !(status == 403 && path == "/agent-notifications") {
				t.Errorf("%s: %d %s", path, status, body)
			}
			for _, secret := range append(eventIDs, "ReviewSecret", anyString(private["id"]), anyString(private["ref"]), anyString(private["thread_id"]), "review-trigger", "review-refs") {
				if strings.Contains(body, secret) {
					t.Errorf("%s leaks %s: %s", path, secret, body)
				}
			}
		}
		for _, id := range eventIDs {
			for _, method := range []string{"GET", "POST"} {
				path := "/events/" + id
				if method == "POST" {
					path += "/archive"
				}
				status, body := request(method, path, token)
				if status != 404 {
					t.Errorf("event access: %d %s", status, body)
				}
			}
		}
	}
	for _, token := range []string{"", stranger.AccessToken} {
		for _, guess := range []string{anyString(private["ref"]), "card:nonexistent"} {
			for _, path := range []string{"/health?probe=", "/health?thread_id="} {
				status, body := request("GET", path+guess, token)
				if status != 200 {
					t.Errorf("existence probe: %d %s", status, body)
				}
			}
		}
	}
	var revision string
	if err = env.workspace.DB().QueryRow(`SELECT revision_id FROM card_revisions WHERE card_id=? LIMIT 1`, private["id"]).Scan(&revision); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/cards/" + anyString(private["id"]) + "/revisions", "/cards/" + anyString(private["id"]) + "/revisions/" + revision} {
		status, body := request("GET", path, owner.AccessToken)
		if status != 200 {
			t.Errorf("owner revision: %d %s", status, body)
		}
	}
	postJSONExpectStatusWithAuth(t, env.server.URL+"/cards/"+anyString(private["id"])+"/revisions", map[string]any{"revision": map[string]any{"summary": "owner revision"}}, owner.AccessToken, http.StatusCreated).Body.Close()
	if _, err = env.workspace.DB().Exec(`INSERT INTO agent_presence(agent_id,current_card_ref,note,observed_at) VALUES(?,?,?,?)`, hostAgentID(t, env, owner.ActorID), private["ref"], "ReviewSecret presence", time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	status, hostView := hostSignedHTTP(t, "PATCH", env.server.URL+"/hosts/"+owner.Host.ID, owner.Host.ID, owner.Host.KeyID, "patch", owner.Host.Private, map[string]any{"display_name": "Signed privacy host"})
	raw, _ := json.Marshal(hostView)
	if status != 200 || strings.Contains(string(raw), "ReviewSecret") || strings.Contains(string(raw), anyString(private["ref"])) {
		t.Errorf("signed host roster: %d %s", status, raw)
	}

	for _, kind := range []string{"complete", "fail"} {
		id := "owner-" + kind
		seedReceiptStreamWakeup(t, s, primitives.AgentWakeup{WakeupID: id, ThreadID: anyString(private["thread_id"]), TargetActorID: owner.ActorID, TargetHandle: owner.Username, Status: primitives.AgentWakeupStatusRequested})
		body := map[string]any{"wakeup_id": id, "bridge_instance_id": "private-bridge", "error": "test failure"}
		postNotificationWakeup(t, env.server.URL, "claim", body, owner).Body.Close()
		postNotificationWakeup(t, env.server.URL, kind, body, owner).Body.Close()
	}
}
