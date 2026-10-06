package server

import (
	"agent-nexus-core/internal/primitives"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

func TestPrivateBoardEvidenceHiddenFromEventsAndThreadTimeline(t *testing.T) {
	if testing.Short() {
		t.Skip("HTTP/storage integration")
	}
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "owner-event", "owner-event", "Owner", "owner-event-token")
	stranger := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "reader-event", "reader-event", "Reader", "reader-event-token")
	agent := seedMachinePrincipalForLockoutTest(t, ctx, env.workspace.DB(), "agent-event", "agent-event", "Agent", "agent-event-token")
	store := env.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Confidential board"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateWork(ctx, owner.ActorID, anyString(board["id"]), map[string]any{"title": "Confidential card"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, owner.ActorID, anyString(board["thread_id"]), map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	secrets := []string{"secret-connection", "secret-repository", "Secret evidence title"}
	if _, err = store.PatchWork(ctx, owner.ActorID, anyString(card["id"]), card["version"].(int64), map[string]any{"source_refs": []any{map[string]any{"authority": "generic", "connection_id": secrets[0], "native_id": secrets[1], "title": secrets[2]}}}); err != nil {
		t.Fatal(err)
	}
	events, err := store.ListEventsByThread(ctx, anyString(card["thread_id"]))
	if err != nil || len(events) == 0 {
		t.Fatalf("missing canonical events: %v", err)
	}
	eventID := ""
	for _, event := range events {
		raw, _ := json.Marshal(event)
		if strings.Contains(string(raw), secrets[0]) {
			eventID = anyString(event["id"])
		}
	}
	if eventID == "" {
		t.Fatal("fixture did not produce an evidence audit event")
	}
	if _, err = store.UpsertAgentWakeup(ctx, primitives.AgentWakeup{WakeupID: "private-event-receipt", Status: primitives.AgentWakeupStatusRequested, TargetHandle: agent.Username, TargetActorID: agent.ActorID, ThreadID: anyString(card["thread_id"]), ThreadTitle: "Secret evidence title", TriggerEventID: eventID, TriggerText: "secret-connection"}); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{stranger.AccessToken, agent.AccessToken} {
		for _, path := range []string{"/events?thread_id=" + anyString(card["thread_id"]), "/threads/" + anyString(card["thread_id"]) + "/timeline", "/threads/" + anyString(card["thread_id"]) + "/workspace"} {
			resp := getJSONExpectStatusWithAuth(t, env.server.URL+path, token, 404)
			raw, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			for _, secret := range append(secrets, eventID) {
				if strings.Contains(string(raw), secret) {
					t.Fatalf("%s leaked %s: %s", path, secret, raw)
				}
			}
		}
		resp := getJSONExpectStatusWithAuth(t, env.server.URL+"/events/"+eventID, token, 404)
		resp.Body.Close()
		// Inherited board ownership now denies the backing-thread selector at
		// the shared route boundary, including SSE, before any frame is sent.
		resp = getJSONExpectStatusWithAuth(t, env.server.URL+"/stream/events?thread_id="+anyString(card["thread_id"]), token, 404)
		resp.Body.Close()
	}

	resp := getJSONExpectStatusWithAuth(t, env.server.URL+"/events/"+eventID, owner.AccessToken, 200)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	for _, secret := range secrets {
		if !strings.Contains(string(raw), secret) {
			t.Fatalf("owner lost %s", secret)
		}
	}
}

func TestInboxSubjectCardAndBoardAccessBeforeCountsAndOverview(t *testing.T) {
	if testing.Short() {
		t.Skip("HTTP/storage integration")
	}
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "owner-ask", "owner-ask", "Owner", "owner-ask-token")
	reader := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "reader-ask", "reader-ask", "Reader", "reader-ask-token")
	store := env.primitiveStore.(*primitives.Store)
	publicThread, err := store.CreateThread(ctx, owner.ActorID, map[string]any{"title": "Shared asks"})
	if err != nil {
		t.Fatal(err)
	}
	public, err := store.CreateWork(ctx, owner.ActorID, "", map[string]any{"title": "Public work"})
	if err != nil {
		t.Fatal(err)
	}
	projected := []primitives.DerivedInboxItem{{ID: "public-ask", ThreadID: anyString(publicThread.Thread["id"]), Category: "ask", TriggerAt: time.Now().UTC().Format(time.RFC3339Nano), Data: map[string]any{"kind": "ask", "title": "Visible ask", "subject_ref": public["ref"]}}}
	for _, scope := range []string{"card", "board"} {
		board, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Private " + scope})
		if err != nil {
			t.Fatal(err)
		}
		card, err := store.CreateWork(ctx, owner.ActorID, anyString(board["id"]), map[string]any{"title": "Confidential " + scope})
		if err != nil {
			t.Fatal(err)
		}
		thread := anyString(card["thread_id"])
		if scope == "board" {
			thread = anyString(board["thread_id"])
		}
		if _, err = store.PatchThread(ctx, owner.ActorID, thread, map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
			t.Fatal(err)
		}
		projected = append(projected, primitives.DerivedInboxItem{ID: "hidden-" + scope, ThreadID: anyString(publicThread.Thread["id"]), Category: "ask", TriggerAt: time.Now().UTC().Format(time.RFC3339Nano), Data: map[string]any{"kind": "ask", "title": "Confidential ask " + scope, "subject_ref": card["ref"]}})
	}
	if err = store.ReplaceDerivedInboxItems(ctx, anyString(publicThread.Thread["id"]), projected); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{reader.AccessToken, owner.AccessToken} {
		for _, limit := range []string{"0", "1", "50"} {
			resp := getJSONExpectStatusWithAuth(t, env.server.URL+"/inbox/summary?limit="+limit, token, 200)
			var body struct {
				Count int              `json:"open_ask_count"`
				Asks  []map[string]any `json:"asks"`
			}
			if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			expected := 1
			if token == owner.AccessToken {
				expected = 3
			}
			if body.Count != expected {
				t.Fatalf("subject access affected counts incorrectly: %+v", body)
			}
			if token == reader.AccessToken {
				raw, _ := json.Marshal(body)
				if strings.Contains(string(raw), "Confidential") {
					t.Fatal(string(raw))
				}
			}
		}
	}
	resp := getJSONExpectStatusWithAuth(t, env.server.URL+"/overview", reader.AccessToken, 200)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if strings.Contains(string(raw), "Confidential ask") || !strings.Contains(string(raw), "Visible ask") {
		t.Fatalf("Overview has different inbox access: %s", raw)
	}
}

func TestBoardCursorUsesOnlyAccessibleTitleMatches(t *testing.T) {
	if testing.Short() {
		t.Skip("HTTP/storage integration")
	}
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "owner-board-page", "owner-board-page", "Owner", "owner-board-page-token")
	reader := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "reader-board-page", "reader-board-page", "Reader", "reader-board-page-token")
	store := env.primitiveStore.(*primitives.Store)
	for i := 0; i < 2; i++ {
		board, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Confidential-acquisition"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.PatchThread(ctx, owner.ActorID, anyString(board["thread_id"]), map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
			t.Fatal(err)
		}
	}
	for visible := 0; visible < 3; visible++ {
		resp := getJSONExpectStatusWithAuth(t, env.server.URL+"/boards?q=Confidential-acquisition&limit=1", reader.AccessToken, 200)
		var body struct {
			Boards []map[string]any `json:"boards"`
			Cursor string           `json:"next_cursor"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		expected := visible
		if expected > 1 {
			expected = 1
		}
		if len(body.Boards) != expected || (body.Cursor != "") != (visible > 1) {
			t.Fatalf("private title cursor oracle: %+v", body)
		}
		if visible < 2 {
			if _, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Confidential-acquisition public"}); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestLegacyBackingThreadAndInboxRefsRespectPrivateBoard(t *testing.T) {
	if testing.Short() {
		t.Skip("HTTP/storage integration")
	}
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "owner-legacy", "owner-legacy", "Owner", "owner-legacy-token")
	reader := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "reader-legacy", "reader-legacy", "Reader", "reader-legacy-token")
	store := env.primitiveStore.(*primitives.Store)
	board, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Private legacy board"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateWork(ctx, owner.ActorID, anyString(board["id"]), map[string]any{"title": "Private legacy card"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, owner.ActorID, anyString(board["thread_id"]), map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	cardThread := anyString(card["thread_id"])
	if _, err = env.workspace.DB().Exec(`UPDATE cards SET parent_thread_id=thread_id,thread_id=NULL WHERE id=?`, card["id"]); err != nil {
		t.Fatal(err)
	}
	event, err := store.AppendEvent(ctx, owner.ActorID, map[string]any{"type": "message", "thread_id": cardThread, "refs": []string{}, "payload": map[string]any{"text": "Private legacy evidence"}})
	if err != nil {
		t.Fatal(err)
	}
	other, err := store.CreateThread(ctx, owner.ActorID, map[string]any{"title": "Public inbox thread"})
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := store.CreateArtifact(ctx, owner.ActorID, map[string]any{"kind": "note", "thread_id": cardThread, "refs": []string{}}, "Private legacy evidence", "text/plain")
	if err != nil {
		t.Fatal(err)
	}
	items := []primitives.DerivedInboxItem{
		{ID: "legacy-backing", ThreadID: cardThread, Category: "ask", TriggerAt: time.Now().UTC().Format(time.RFC3339Nano), Data: map[string]any{"kind": "ask", "title": "Private legacy evidence", "subject_ref": "thread:" + cardThread}},
		{ID: "legacy-refs", ThreadID: anyString(other.Thread["id"]), Category: "ask", TriggerAt: time.Now().UTC().Format(time.RFC3339Nano), Data: map[string]any{"kind": "ask", "title": "Private legacy evidence", "subject_ref": "thread:" + anyString(other.Thread["id"]), "related_refs": []string{}, "refs": []string{anyString(card["ref"])}}},
		{ID: "event-subject", ThreadID: anyString(other.Thread["id"]), Category: "ask", TriggerAt: time.Now().UTC().Format(time.RFC3339Nano), Data: map[string]any{"kind": "ask", "title": "Private legacy evidence", "subject_ref": "event:" + anyString(event["id"])}},
	}
	items = append(items, primitives.DerivedInboxItem{ID: "artifact-subject", ThreadID: anyString(other.Thread["id"]), Category: "ask", TriggerAt: time.Now().UTC().Format(time.RFC3339Nano), Data: map[string]any{"kind": "ask", "title": "Private legacy evidence", "subject_ref": anyString(artifact["ref"])}})
	grouped := map[string][]primitives.DerivedInboxItem{}
	for _, item := range items {
		grouped[item.ThreadID] = append(grouped[item.ThreadID], item)
	}
	for tid, group := range grouped {
		if err = store.ReplaceDerivedInboxItems(ctx, tid, group); err != nil {
			t.Fatal(err)
		}
	}
	for _, token := range []string{owner.AccessToken, reader.AccessToken} {
		resp := getJSONExpectStatusWithAuth(t, env.server.URL+"/inbox/summary?limit=50", token, 200)
		var summary struct {
			Count int `json:"open_ask_count"`
		}
		json.NewDecoder(resp.Body).Decode(&summary)
		resp.Body.Close()
		expected := 0
		if token == owner.AccessToken {
			expected = 4
		}
		if summary.Count != expected {
			t.Fatalf("legacy subjects count=%d expected=%d", summary.Count, expected)
		}
	}
	for _, path := range []string{"/events?thread_id=" + cardThread, "/threads/" + cardThread + "/timeline", "/inbox/summary?limit=50", "/overview"} {
		status := 200
		if strings.HasPrefix(path, "/events?") || strings.HasPrefix(path, "/threads/") {
			status = 404
		}
		resp := getJSONExpectStatusWithAuth(t, env.server.URL+path, reader.AccessToken, status)
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if strings.Contains(string(raw), "Private legacy evidence") || strings.Contains(string(raw), anyString(event["id"])) {
			t.Fatalf("legacy privacy %s: %s", path, raw)
		}
	}
	resp := getJSONExpectStatusWithAuth(t, env.server.URL+"/events/"+anyString(event["id"]), reader.AccessToken, 404)
	resp.Body.Close()
}

func TestProjectionRebuildRemainsCanonicalUnderScopedRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("HTTP/storage integration")
	}
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "owner-projection", "owner-projection", "Owner", "owner-projection-token")
	reader := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "reader-projection", "reader-projection", "Reader", "reader-projection-token")
	store := env.primitiveStore.(*primitives.Store)
	thread, err := store.CreateThread(ctx, owner.ActorID, map[string]any{"title": "Public projection"})
	if err != nil {
		t.Fatal(err)
	}
	board, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Private projection board"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateWork(ctx, owner.ActorID, anyString(board["id"]), map[string]any{"title": "Private projection card"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, owner.ActorID, anyString(board["thread_id"]), map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	threadID := anyString(thread.Thread["id"])
	for _, ref := range []string{"thread:" + threadID, anyString(card["ref"])} {
		if _, err = store.AppendEvent(ctx, owner.ActorID, map[string]any{"type": "human_attention_requested", "thread_id": threadID, "refs": []string{ref}, "payload": map[string]any{"kind": "ask", "title": "Projection ask", "subject_ref": ref, "request_id": ref, "response_proposals": []string{"Proceed", "Wait"}}}); err != nil {
			t.Fatal(err)
		}
	}
	scoped := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: reader.ActorID})
	if err = refreshDerivedTopicProjection(scoped, handlerOptions{primitiveStore: store}, threadID, time.Now().UTC(), reader.ActorID); err != nil {
		t.Fatal(err)
	}
	canonical, err := store.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: threadID})
	if err != nil || len(canonical) != 2 {
		t.Fatalf("request policy corrupted shared projection: %d %v", len(canonical), err)
	}
	visible, count, err := store.ReadInbox(scoped, primitives.InboxReadOptions{})
	if err != nil || count != 1 || len(visible) != 1 {
		t.Fatalf("projection read policy: %d %d %v", count, len(visible), err)
	}
}
