package server

import (
	"agent-nexus-core/internal/commandcenter"
	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/schema"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

type privacyRoutePolicy struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Policy string `json:"policy"`
	Reason string `json:"reason,omitempty"`
}

func loadPrivacyRouteMatrix(t *testing.T) []privacyRoutePolicy {
	t.Helper()
	raw, err := os.ReadFile("testdata/resource_access_routes.json")
	if err != nil {
		t.Fatal(err)
	}
	var policies []privacyRoutePolicy
	if err = json.Unmarshal(raw, &policies); err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile("../../../contracts/gen/meta/routes.json")
	if err != nil {
		t.Fatal(err)
	}
	var inventory struct {
		Routes []privacyRoutePolicy `json:"routes"`
	}
	if err = json.Unmarshal(raw, &inventory); err != nil {
		t.Fatal(err)
	}
	covered := map[string]privacyRoutePolicy{}
	for _, p := range policies {
		key := p.Method + " " + p.Path
		if _, ok := covered[key]; ok {
			t.Fatalf("duplicate privacy policy: %s", key)
		}
		switch p.Policy {
		case "record", "collection", "reference-read", "reference-write", "stream", "independent", "maintenance":
		default:
			t.Errorf("unimplemented privacy policy %q: %s", p.Policy, key)
		}
		covered[key] = p
		if p.Policy == "independent" && !independentPrivacyRoute(p) {
			t.Errorf("resource route cannot be exempt: %s", key)
		}
	}
	for _, r := range inventory.Routes {
		key := r.Method + " " + r.Path
		p, ok := covered[key]
		if !ok {
			t.Errorf("route needs privacy fixture/classification: %s", key)
		} else if p.Policy == "independent" && p.Reason == "" {
			t.Errorf("exemption needs rationale: %s", key)
		}
		delete(covered, key)
	}
	for key := range covered {
		t.Errorf("stale privacy route: %s", key)
	}
	return policies
}
func TestResourceAccessRouteInventory(t *testing.T) {
	policies := loadPrivacyRouteMatrix(t)
	if err := validatePrivacyMounts(policies, privacyMountedRoutes()); err != nil {
		t.Fatal(err)
	}
	checkResourceAccessRegistration(t)
}

func TestResourceAccessRouteMatrix(t *testing.T) {
	requireIntegrationTest(t)
	policies := loadPrivacyRouteMatrix(t)
	// Rebuild is a successful canonical maintenance action; run it after the
	// seeded projection fixtures have been exercised.
	sort.SliceStable(policies, func(i, j int) bool { return policies[i].Policy != "maintenance" && policies[j].Policy == "maintenance" })
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	store := env.primitiveStore.(*primitives.Store)
	owner := seedHumanPrincipalForLockoutTest(t, ctx, db, "matrix-owner", "matrix-owner-actor", "matrix-owner", "matrix-owner-token")
	stranger := seedHumanPrincipalForLockoutTest(t, ctx, db, "matrix-stranger", "matrix-stranger-actor", "matrix-stranger", "matrix-stranger-token")
	agent := seedMachinePrincipalForLockoutTest(t, ctx, db, "matrix-agent", "matrix-agent-actor", "matrix.agent", "matrix-agent-token")
	// Resume provenance must be visible to every tested principal.
	if _, err := db.Exec(`INSERT INTO events(id,type,ts,actor_id,refs_json,payload_json) VALUES('matrix-stream-anchor','message_posted','2000-01-01T00:00:00Z','owner','[]','{}')`); err != nil {
		t.Fatal(err)
	}
	board, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Confidential board sentinel"})
	if err != nil {
		t.Fatal(err)
	}
	boardID := anyString(board["id"])
	card, err := store.CreateBoardCard(ctx, owner.ActorID, boardID, primitives.AddBoardCardInput{Title: "Confidential card sentinel", Body: "Confidential body sentinel", ColumnKey: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	cardID := anyString(card.Card["id"])
	threadID := anyString(card.Card["thread_id"])
	boardThread := anyString(board["thread_id"])
	if _, err = store.PatchThread(ctx, owner.ActorID, boardThread, map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	event, err := store.AppendEvent(ctx, owner.ActorID, map[string]any{"type": "message_posted", "thread_id": threadID, "refs": []string{"card:" + cardID}, "payload": map[string]any{"text": "Confidential evidence sentinel"}})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.CreateArtifact(ctx, owner.ActorID, map[string]any{"kind": "note", "thread_id": threadID, "refs": []string{"card:" + cardID}}, map[string]any{"text": "Confidential artifact sentinel"}, "structured")
	if err != nil {
		t.Fatal(err)
	}
	document, _, err := store.CreateDocument(ctx, owner.ActorID, map[string]any{"title": "Confidential document sentinel"}, "Confidential document body sentinel", "text", []string{"card:" + cardID})
	if err != nil {
		t.Fatal(err)
	}
	topic, err := store.CreateTopic(ctx, owner.ActorID, map[string]any{"title": "Confidential topic sentinel", "summary": "Confidential topic body sentinel", "related_refs": []string{"card:" + cardID}})
	if err != nil {
		t.Fatal(err)
	}
	seedReceiptStreamWakeup(t, store, primitives.AgentWakeup{WakeupID: "matrix-private-wakeup", ThreadID: threadID, TargetActorID: agent.ActorID, TargetHandle: agent.Username, Status: primitives.AgentWakeupStatusRequested, TriggerText: "Confidential wakeup", Refs: []string{"card:" + cardID}})
	askID := "matrix-private-ask"
	item := streamPrivacyInboxItem(threadID, askID, "Confidential ask sentinel")
	item.SourceCardID = cardID
	item.SourceEventID = anyString(event["id"])
	seedStreamPrivacyInbox(t, store, threadID, item)
	// A public row guarantees collections cannot satisfy the test by denying all reads.
	public, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Public board control"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.CreateBoardCard(ctx, owner.ActorID, anyString(public["id"]), primitives.AddBoardCardInput{Title: "Public card control", ColumnKey: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	publicThread := anyString(public["thread_id"])
	if _, err = store.AppendEvent(ctx, owner.ActorID, map[string]any{"type": "message_posted", "thread_id": publicThread, "refs": []string{}, "payload": map[string]any{"text": "Visible stream control"}}); err != nil {
		t.Fatal(err)
	}
	seedStreamPrivacyInbox(t, store, publicThread, streamPrivacyInboxItem(publicThread, "public-control-ask", "Visible inbox control"))
	selected := seedMachinePrincipalForLockoutTest(t, ctx, db, "matrix-selected", "matrix-selected-actor", "matrix.selected", "matrix-selected-token")
	runtime, err := NewPMRuntime(db, store, env.authStore, PMRuntimeConfig{PM: pm.Config{WorkspaceID: "ws_main", AgentActorID: selected.ActorID}})
	if err != nil {
		t.Fatal(err)
	}
	conversation, err := runtime.Service.CreateConversation(ctx, pm.Principal{WorkspaceID: "ws_main", ActorID: owner.ActorID, Human: true}, pm.CreateConversation{RequestKey: "private", Title: "Confidential conversation sentinel", WorkRef: anyString(card.Card["ref"])})
	if err != nil {
		t.Fatal(err)
	}
	rosterAgent := seedNotificationTestAgent(t, env, "roster.matrixhost")
	now := time.Now().UTC()
	for kind, value := range map[string]any{
		"decision": pm.Decision{ID: "matrix-private-decision", WorkspaceID: "ws_main", ActorID: owner.ActorID, WorkRef: anyString(card.Card["ref"]), Instruction: "Confidential decision sentinel", Status: pm.AwaitingAnswer, Revision: 1, CreatedAt: now},
		"action":   pm.Action{ID: "matrix-private-action", DecisionID: "matrix-private-decision", WorkspaceID: "ws_main", ActorID: owner.ActorID, WorkRef: anyString(card.Card["ref"]), Status: pm.Pending},
		"turn":     pm.Turn{ID: "matrix-private-turn", ConversationID: conversation.ID, WorkspaceID: "ws_main", ActorID: owner.ActorID, Text: "Confidential turn sentinel", Status: pm.Pending},
	} {
		raw, _ := json.Marshal(value)
		var item map[string]any
		_ = json.Unmarshal(raw, &item)
		if _, err = db.ExecContext(ctx, `INSERT INTO pm_records(kind,id,workspace_id,actor_id,parent_id,revision,body) VALUES(?,?,?,?,?,1,?)`, kind, item["id"], "ws_main", owner.ActorID, conversation.ID, raw); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO runs(id,handle,launcher,external_id,host_id,agent_id,adapter,state,liveness,result_collected,labels_json,card_ref,last_observed_at) VALUES('matrix-private-run','matrix-private-run','test','private','matrix-host',?,'test','running','alive',0,'[]',?,?)`, hostAgentID(t, env, rosterAgent.ActorID), card.Card["ref"], now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO agent_presence(agent_id,current_card_ref,note,observed_at) VALUES(?,?,?,?)`, hostAgentID(t, env, rosterAgent.ActorID), card.Card["ref"], "Confidential presence sentinel", now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	contract, err := schema.Load("../../../contracts/anx-schema.yaml")
	if err != nil {
		t.Fatal(err)
	}
	env.server.Config.Handler = NewHandler("0.2.2", WithAuthStore(env.authStore), WithActorRegistry(env.registry), WithPrimitiveStore(store), WithSchemaContract(contract), WithRunStore(commandcenter.NewStore(db, commandcenter.SQLIdentities{DB: db})), WithPMRuntime(runtime))
	if _, err = db.ExecContext(ctx, `INSERT INTO access_requests(id,principal_id,actor_id,username,grant_name,reason,created_at,request_event_id,inbox_item_id) VALUES('matrix-private-access',?,?,?,'auth-admin','Confidential access reason','now',?,'matrix-access-inbox')`, agent.AgentID, agent.ActorID, "matrix.agent", event["id"]); err != nil {
		t.Fatal(err)
	}
	replacements := strings.NewReplacer("{request_id}", "matrix-private-access", "{host_id}", rosterAgent.Host.ID, "{run_id}", "matrix-private-run", "{agent_id}", agent.AgentID, "{conversation_id}", conversation.ID, "{decision_id}", "matrix-private-decision", "{action_id}", "matrix-private-action", "{turn_id}", "matrix-private-turn", "{document_id}", anyString(document["id"]), "{topic_id}", anyString(topic.Topic["id"]), "{comment_id}", anyString(event["id"]), "{board_id}", boardID, "{card_id}", cardID, "{card_ref}", "card:"+cardID, "{thread_id}", threadID, "{artifact_id}", anyString(artifact["id"]), "{event_id}", anyString(event["id"]), "{inbox_id}", askID, "{revision_id}", anyString(card.Card["head_revision_id"]))
	hidden := []string{boardID, cardID, threadID, boardThread, anyString(event["id"]), anyString(artifact["id"]), askID, conversation.ID, "matrix-private-decision", "matrix-private-action", "matrix-private-turn", "matrix-private-run", "Confidential"}
	snapshot := func() string {
		var v string
		if err := db.QueryRowContext(ctx, `SELECT json_object('cards',(SELECT json_group_array(json_object('id',id,'updated_at',updated_at,'due_at',due_at,'summary',summary)) FROM cards),'events',(SELECT COUNT(*) FROM events),'wakeups',(SELECT COUNT(*) FROM agent_wakeups))`).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	for _, principal := range []lockoutPrincipalSeed{stranger, agent} {
		seedStreamPrivacyInbox(t, store, threadID, item)
		seedStreamPrivacyInbox(t, store, publicThread, streamPrivacyInboxItem(publicThread, "public-control-ask", "Visible inbox control"))
		t.Run(principal.ActorID, func(t *testing.T) {
			for _, p := range policies {
				if p.Policy != "record" && p.Policy != "collection" && p.Policy != "reference-read" && p.Policy != "reference-write" && p.Policy != "stream" && p.Policy != "independent" && p.Policy != "maintenance" {
					t.Fatalf("unimplemented policy %q for %s %s", p.Policy, p.Method, p.Path)
				}
				t.Run(p.Method+" "+p.Path, func(t *testing.T) {
					path := replacements.Replace(p.Path)
					if p.Policy == "independent" {
						path = regexp.MustCompile(`\{[^/{}]+\}`).ReplaceAllString(path, "privacy-fixture")
					}
					if strings.Contains(path, "{") {
						t.Fatalf("missing private fixture for %s", path)
					}
					// Point routes use their actual private path selector, without
					// unrelated private IDs injected into the request body.
					payload := map[string]any{}
					if p.Method == "PATCH" && p.Path == "/hosts/{host_id}" {
						payload["display_name"] = "Privacy roster fixture"
					}
					if p.Policy == "reference-write" {
						payload = privacyWritePayload(t, p.Path, principal.ActorID, boardID, cardID, threadID, anyString(document["id"]), anyString(event["id"]), agent.AgentID)
					}
					if p.Policy == "reference-read" {
						if p.Path != "/refs/resolve" {
							t.Fatalf("missing reference-read fixture: %s", p.Path)
						}
						payload["refs"] = []string{"card:" + cardID}
					}
					if p.Path == "/stream/agent-notification-receipts" {
						path += "?thread_id=" + threadID
					}
					if path == "/docs/search" {
						path += "?q=Confidential"
					}
					if path == "/ref-edges" {
						path += "?source_ref=board:" + anyString(public["id"])
					}
					body, _ := json.Marshal(payload)
					var reader io.Reader
					if p.Method != "GET" {
						reader = bytes.NewReader(body)
					}
					before := snapshot()
					req, err := http.NewRequest(p.Method, env.server.URL+path, reader)
					if err != nil {
						t.Fatal(err)
					}
					req.Header.Set("Authorization", "Bearer "+principal.AccessToken)
					req.Header.Set("Content-Type", "application/json")
					if p.Path == "/stream/events" {
						// Resume the seeded fixture: a fresh connection starts at
						// head, while this matrix must exercise visible replay too.
						req.Header.Set("Last-Event-ID", "matrix-stream-anchor")
					}
					if p.Path == "/artifacts/attachments" {
						var multipartBody bytes.Buffer
						mw := multipart.NewWriter(&multipartBody)
						_ = mw.WriteField("actor_id", principal.ActorID)
						refs, _ := json.Marshal([]string{"card:" + cardID})
						_ = mw.WriteField("refs", string(refs))
						file, e := mw.CreateFormFile("file", "privacy.txt")
						if e != nil {
							t.Fatal(e)
						}
						_, _ = file.Write([]byte("attempt attachment"))
						_ = mw.Close()
						req.Body = io.NopCloser(&multipartBody)
						req.ContentLength = int64(multipartBody.Len())
						req.Header.Set("Content-Type", mw.FormDataContentType())
					}
					if p.Policy == "stream" && p.Path != "/stream/agent-notification-receipts" {
						streamCtx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
						defer cancel()
						req = req.WithContext(streamCtx)
					}
					resp, err := http.DefaultClient.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					defer resp.Body.Close()
					out, err := io.ReadAll(resp.Body)
					if err != nil && p.Policy != "stream" {
						t.Fatal(err)
					}
					if p.Policy == "stream" && p.Path != "/stream/agent-notification-receipts" {
						if resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
							t.Errorf("stream failed: %d %s", resp.StatusCode, out)
						}
					} else if p.Policy == "independent" {
						// Exercise identity/transport exemptions too. Absent optional
						// services may return 503; no response may serialize secrets.
						if resp.StatusCode >= 500 && resp.StatusCode != 503 {
							t.Errorf("exempt route failed: %d %s", resp.StatusCode, out)
						}
					} else if p.Policy == "reference-read" {
						var result map[string]any
						want := map[string]any{"items": []any{map[string]any{"ref": "card:" + cardID, "resolvable": false}}}
						if json.Unmarshal(out, &result) != nil || resp.StatusCode != 200 || !reflect.DeepEqual(result, want) {
							t.Errorf("private reference read: %d %s", resp.StatusCode, out)
						}
					} else if p.Policy == "collection" || p.Policy == "maintenance" {
						if resp.StatusCode != 200 && !(resp.StatusCode == 403 && (strings.HasPrefix(p.Path, "/agent-") || p.Path == "/agents/me" || p.Path == "/pm/bindings" || p.Path == "/hosts/{host_id}" || strings.HasPrefix(p.Path, "/auth/access"))) {
							t.Errorf("collection status %d: %s", resp.StatusCode, out)
						}
					} else if resp.StatusCode != 404 && resp.StatusCode != 403 && !(resp.StatusCode == 401 && strings.HasPrefix(p.Path, "/agent-wakeups/")) {
						t.Errorf("private resource status %d: %s", resp.StatusCode, out)
					}
					if p.Path == "/stream/events" && !bytes.Contains(out, []byte("Visible stream control")) {
						t.Errorf("public stream control absent: %s", out)
					}
					if p.Path == "/stream/inbox" && !bytes.Contains(out, []byte("Visible inbox control")) {
						t.Errorf("public inbox control absent: %s", out)
					}
					// Reference reads echo the requested identity only; the exact
					// response comparison above rejects every metadata field.
					for _, needle := range hidden {
						if p.Policy == "reference-read" {
							continue
						}
						if needle != "" && bytes.Contains(out, []byte(needle)) {
							t.Errorf("private data %q exposed: %s", needle, out)
						}
					}
					if p.Method != "GET" {
						if after := snapshot(); after != before {
							t.Errorf("rejected mutation changed canonical state: before=%s after=%s", before, after)
						}
					}
				})
			}
		})
	}
	publicPreview := privacyWritePayload(t, "/reports/preview", stranger.ActorID, anyString(public["id"]), cardID, threadID, anyString(document["id"]), anyString(event["id"]), agent.AgentID)
	body, _ := json.Marshal(publicPreview)
	req, _ := http.NewRequest("POST", env.server.URL+"/reports/preview", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+stranger.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	control, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	out, _ := io.ReadAll(control.Body)
	control.Body.Close()
	if control.StatusCode != 200 {
		t.Fatalf("public report control: %d %s", control.StatusCode, out)
	}
	var validRun commandcenter.Run
	runBody, _ := json.Marshal(privacyWritePayload(t, "/runs", agent.ActorID, boardID, cardID, threadID, "", "", agent.AgentID))
	if e = json.Unmarshal(runBody, &validRun); e != nil {
		t.Fatal(e)
	}
	if e = commandcenter.ValidateRun(&validRun); e != nil {
		t.Fatalf("invalid run fixture: %v", e)
	}
	resp := getJSONExpectStatusWithAuth(t, env.server.URL+"/cards/"+cardID, owner.AccessToken, 200)
	resp.Body.Close()
}

func privacyWritePayload(t *testing.T, path, actor, board, card, thread, doc, event, agent string) map[string]any {
	t.Helper()
	refs := []string{"card:" + card}
	switch path {
	case "/auth/access-requests":
		return map[string]any{"grant": "auth-admin", "reason": "card:" + card}
	case "/boards":
		return map[string]any{"actor_id": actor, "board": map[string]any{"title": "attempt", "refs": refs}}
	case "/cards":
		return map[string]any{"actor_id": actor, "board_id": board, "title": "attempt", "column_key": "ready"}
	case "/work":
		return map[string]any{"actor_id": actor, "board_ref": "board:" + board, "title": "attempt"}
	case "/events":
		return map[string]any{"actor_id": actor, "event": map[string]any{"type": "message_posted", "thread_id": thread, "refs": refs, "payload": map[string]any{"text": "attempt"}}}
	case "/artifacts", "/artifacts/attachments":
		return map[string]any{"actor_id": actor, "artifact": map[string]any{"kind": "note", "refs": refs}, "content": "attempt", "content_type": "text"}
	case "/docs":
		return map[string]any{"actor_id": actor, "document": map[string]any{"title": "attempt"}, "content": "attempt", "content_type": "text", "refs": refs}
	case "/topics":
		return map[string]any{"actor_id": actor, "topic": map[string]any{"title": "attempt", "summary": "attempt", "related_refs": refs}}
	case "/home/read":
		return map[string]any{"actor_id": actor, "group_ref": "thread:" + thread}
	case "/workspace/dashboard":
		return map[string]any{"actor_id": actor, "document_ref": "document:" + doc}
	case "/agent-inbox/answers/read":
		return map[string]any{"answer_event_id": event}
	case "/agent-notifications/read", "/agent-notifications/dismiss":
		return map[string]any{"wakeup_id": "matrix-private-wakeup"}
	case "/agent-wakeups/claim", "/agent-wakeups/complete", "/agent-wakeups/fail":
		return map[string]any{"wakeup_id": "matrix-private-wakeup", "bridge_instance_id": "test", "error": "failed"}
	case "/agents/me/presence":
		return map[string]any{"current_card_ref": "card:" + card, "note": "attempt"}
	case "/runs":
		return map[string]any{"launcher": "agentctl", "last_observed_at": time.Now().UTC().Format(time.RFC3339Nano), "labels": []string{}, "external_id": "attempt", "host_id": "matrix-host", "agent_id": agent, "adapter": "test", "state": "running", "liveness": "alive", "card_ref": "card:" + card}
	case "/pm/conversations":
		return map[string]any{"request_key": "attempt", "title": "attempt", "work_ref": "card:" + card}
	case "/pm/decisions":
		return map[string]any{"request_key": "attempt", "work_ref": "card:" + card, "scope": "work.phase", "target_revision": "1.1", "instruction": "attempt"}
	case "/pm/turns/claim":
		return map[string]any{"runner_id": "attempt"}
	case "/reports/preview":
		return map[string]any{"report": map[string]any{"kind": "anx.visual-report", "schema_version": 1, "title": "Privacy control", "summary": "Workspace report", "generated_at": "2026-10-04T10:00:00Z", "sources": []any{}, "projects": []any{map[string]any{"id": "workspace", "title": "Workspace", "summary": "Current work", "outcome": "Ship"}}, "panels": []any{map[string]any{"id": "private", "type": "live-initiatives", "data": map[string]any{"board_refs": []string{"board:" + board}}, "project_id": "workspace", "title": "Initiatives", "author": "ANX", "provenance": "reported", "observed_at": nil, "freshness": "unavailable", "source_ids": []any{}}}}}
	default:
		t.Fatalf("missing reference-write fixture: %s", path)
		return nil
	}
}

func hostAgentID(t *testing.T, env authIntegrationEnv, actorID string) string {
	t.Helper()
	var id string
	if err := env.workspace.DB().QueryRow(`SELECT id FROM agents WHERE actor_id=?`, actorID).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}
