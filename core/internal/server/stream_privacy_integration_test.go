package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

func TestInboxStreamAppliesListVisibilityOnEveryPoll(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()
	for _, role := range []string{"owner", "stranger", "agent"} {
		t.Run(role, func(t *testing.T) {
			t.Parallel()
			env := newAuthIntegrationEnv(t, authIntegrationOptions{})
			ctx := context.Background()
			db := env.workspace.DB()
			owner := seedHumanPrincipalForLockoutTest(t, ctx, db, "stream-owner", "stream-owner-actor", "stream-owner", "stream-owner-token")
			stranger := seedHumanPrincipalForLockoutTest(t, ctx, db, "stream-stranger", "stream-stranger-actor", "stream-stranger", "stream-stranger-token")
			agent := seedMachinePrincipalForLockoutTest(t, ctx, db, "stream-agent", "stream-agent-actor", "stream.agent", "stream-agent-token")
			token := map[string]string{"owner": owner.AccessToken, "stranger": stranger.AccessToken, "agent": agent.AccessToken}[role]
			store := env.primitiveStore.(*primitives.Store)
			private := seedStreamPrivacyThread(t, store, owner.ActorID, true)
			public := seedStreamPrivacyThread(t, store, owner.ActorID, false)
			hidden := seedStreamPrivacyThread(t, store, owner.ActorID, false)
			if _, err := store.ArchiveThread(ctx, owner.ActorID, hidden); err != nil {
				t.Fatal(err)
			}
			privateItem := streamPrivacyInboxItem(private, "private-ask", "private body")
			publicItem := streamPrivacyInboxItem(public, "public-ask", "public body")
			archivedAsk := streamPrivacyInboxItem(public, "archived-subject-ask", "archived subject body")
			archivedAsk.Data["subject_ref"] = "thread:" + hidden
			archivedAsk.Data["related_refs"] = []any{"thread:" + hidden}
			hiddenItem := streamPrivacyInboxItem(public, "hidden-subject-notification", "hidden subject body")
			hiddenItem.Category = "agent_wake"
			hiddenItem.Data["kind"] = "agent_wake"
			hiddenItem.Data["subject_ref"] = "thread:" + hidden
			hiddenItem.Data["related_refs"] = []any{"thread:" + hidden}
			publicNotification := streamPrivacyInboxItem(public, "public-notification", "public notification body")
			publicNotification.Category = "agent_wake"
			publicNotification.Data["kind"] = "agent_wake"
			seedStreamPrivacyInbox(t, store, private, privateItem)
			seedStreamPrivacyInbox(t, store, public, publicItem, archivedAsk, publicNotification, hiddenItem)

			// An unknown resume cursor must not bypass snapshot authorization.
			resp := openAuthenticatedPrivacyStream(t, env.server.URL+"/stream/inbox", token, "unknown-cursor")
			reader, stop := startSSEReader(resp.Body)
			defer stop()
			want := map[string]string{
				"public-ask":           "public body",
				"archived-subject-ask": "archived subject body",
				"public-notification":  "public notification body",
			}
			if role == "owner" {
				want["private-ask"] = "private body"
			}
			assertPrivacyInboxEvents(t, reader, want)
			assertPrivacyInboxList(t, env.server.URL, token, want)

			// Both a new private ask and an update to an existing ask must be
			// filtered, while a public update proves the stream is still polling.
			privateItem.Data["body"] = "updated private body"
			publicItem.Data["body"] = "updated public body"
			archivedAsk.Data["body"] = "updated archived subject body"
			publicNotification.Data["body"] = "updated public notification body"
			hiddenItem.Data["body"] = "updated hidden subject body"
			seedStreamPrivacyInbox(t, store, private, privateItem, streamPrivacyInboxItem(private, "new-private-ask", "new private body"))
			seedStreamPrivacyInbox(t, store, public, publicItem, archivedAsk, publicNotification, hiddenItem)
			want = map[string]string{
				"public-ask":           "updated public body",
				"archived-subject-ask": "updated archived subject body",
				"public-notification":  "updated public notification body",
			}
			if role == "owner" {
				want["private-ask"] = "updated private body"
				want["new-private-ask"] = "new private body"
			}
			assertPrivacyInboxEvents(t, reader, want)
			assertPrivacyInboxList(t, env.server.URL, token, want)

			// Open asks survive archive, but ordinary notifications disappear.
			// Privacy still applies to archived asks on every poll.
			if _, err := store.ArchiveThread(ctx, owner.ActorID, public); err != nil {
				t.Fatal(err)
			}
			if _, err := store.ArchiveThread(ctx, owner.ActorID, private); err != nil {
				t.Fatal(err)
			}
			publicItem.Data["body"] = "open ask after archive"
			privateItem.Data["body"] = "private ask after archive"
			publicNotification.Data["body"] = "hidden notification after archive"
			seedStreamPrivacyInbox(t, store, private, privateItem, streamPrivacyInboxItem(private, "new-private-ask", "new private body"))
			seedStreamPrivacyInbox(t, store, public, publicItem, archivedAsk, publicNotification, hiddenItem)
			visible := seedStreamPrivacyThread(t, store, owner.ActorID, false)
			seedStreamPrivacyInbox(t, store, visible, streamPrivacyInboxItem(visible, "visible-after-archive", "still visible"))
			updates := map[string]string{"public-ask": "open ask after archive", "visible-after-archive": "still visible"}
			if role == "owner" {
				updates["private-ask"] = "private ask after archive"
				want["private-ask"] = "private ask after archive"
			}
			assertPrivacyInboxEvents(t, reader, updates)
			delete(want, "public-notification")
			want["public-ask"] = "open ask after archive"
			want["visible-after-archive"] = "still visible"
			assertPrivacyInboxList(t, env.server.URL, token, want)
		})
	}
}

func TestReceiptStreamEnforcesThreadPrivacy(t *testing.T) {
	requireIntegrationTest(t)
	t.Parallel()
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, db, "receipt-owner", "receipt-owner-actor", "receipt-owner", "receipt-owner-token")
	stranger := seedHumanPrincipalForLockoutTest(t, ctx, db, "receipt-stranger", "receipt-stranger-actor", "receipt-stranger", "receipt-stranger-token")
	agent := seedMachinePrincipalForLockoutTest(t, ctx, db, "receipt-agent", "receipt-agent-actor", "receipt.agent", "receipt-agent-token")
	store := env.primitiveStore.(*primitives.Store)
	thread := seedStreamPrivacyThread(t, store, owner.ActorID, true)
	seedReceiptStreamWakeup(t, store, primitives.AgentWakeup{
		WakeupID: "private-wakeup", ThreadID: thread,
		TargetActorID: agent.ActorID, TargetHandle: agent.Username,
		Status: primitives.AgentWakeupStatusRequested, TriggerText: "private receipt body",
		Refs: []string{"thread:" + thread},
	})
	url := env.server.URL + "/stream/agent-notification-receipts?thread_id=" + thread
	for _, token := range []string{stranger.AccessToken, agent.AccessToken} {
		resp := getJSONExpectStatusWithAuth(t, url, token, http.StatusNotFound)
		resp.Body.Close()
	}
	resp := openAuthenticatedPrivacyStream(t, url, owner.AccessToken, "")
	reader, stop := startSSEReader(resp.Body)
	defer stop()
	first := awaitSSEEvent(t, reader, 3*time.Second)
	receipt, _ := first.Data["receipt"].(map[string]any)
	if first.Event != "notification_receipt" || anyString(receipt["trigger_text"]) != "private receipt body" {
		t.Fatalf("owner did not receive private receipt: %#v", first)
	}
	if _, err := store.PatchThread(ctx, owner.ActorID, thread, map[string]any{"pm_actor_id": stranger.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	// The owner loses access while connected; no further receipt is emitted.
	next := awaitSSEEvent(t, reader, 3*time.Second)
	errorData, _ := next.Data["error"].(map[string]any)
	if next.Event != "error" || anyString(errorData["code"]) != "not_found" {
		t.Fatalf("expected access loss to close receipt stream, got %#v", next)
	}
}

func seedStreamPrivacyThread(t *testing.T, store *primitives.Store, owner string, private bool) string {
	t.Helper()
	data := map[string]any{"title": "Stream privacy test"}
	if private {
		// Exercise inherited privacy: the card thread itself deliberately stays public.
		ctx := context.Background()
		board, err := store.CreateBoard(ctx, owner, map[string]any{"title": "Private stream board"})
		if err != nil {
			t.Fatal(err)
		}
		card, err := store.CreateBoardCard(ctx, owner, anyString(board["id"]), primitives.AddBoardCardInput{Title: "Private stream card", ColumnKey: "ready"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = store.PatchThread(ctx, owner, anyString(board["thread_id"]), map[string]any{"pm_actor_id": owner}, nil); err != nil {
			t.Fatal(err)
		}
		return anyString(card.Card["thread_id"])
	}
	created, err := store.CreateThread(context.Background(), owner, data)
	if err != nil {
		t.Fatal(err)
	}
	return anyString(created.Thread["id"])
}

func streamPrivacyInboxItem(thread, id, body string) primitives.DerivedInboxItem {
	return primitives.DerivedInboxItem{
		ID: id, ThreadID: thread, Category: "ask", TriggerAt: "2026-10-05T12:00:00Z", GeneratedAt: "2026-10-05T12:00:00Z",
		Data: map[string]any{"id": id, "kind": "ask", "title": id + " title", "body": body, "subject_ref": "thread:" + thread, "related_refs": []any{"thread:" + thread}},
	}
}

func seedStreamPrivacyInbox(t *testing.T, store *primitives.Store, thread string, items ...primitives.DerivedInboxItem) {
	t.Helper()
	if err := store.ReplaceDerivedInboxItems(context.Background(), thread, items); err != nil {
		t.Fatal(err)
	}
}

func openAuthenticatedPrivacyStream(t *testing.T, url, token, cursor string) *http.Response {
	t.Helper()
	// SSE spans multiple mutations and list checks. Bound connection setup,
	// not the entire response lifetime; each expected event has its own budget.
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Last-Event-ID", cursor)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 10 * time.Second
	t.Cleanup(transport.CloseIdleConnections)
	resp, err := (&http.Client{Transport: transport}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		t.Fatalf("stream returned status %d", resp.StatusCode)
	}
	return resp
}

func assertPrivacyInboxEvents(t *testing.T, reader <-chan sseEvent, want map[string]string, titles ...map[string]string) {
	t.Helper()
	seen := map[string]bool{}
	for range want {
		// An eight-subject archived snapshot traverses the ownership graph for
		// each item. Keep a bounded delivery check without timing the whole test.
		event := awaitSSEEvent(t, reader, 10*time.Second)
		item, _ := event.Data["item"].(map[string]any)
		id := anyString(item["id"])
		body, ok := want[id]
		if event.Event != "inbox_item" || !ok || seen[id] || anyString(item["body"]) != body {
			t.Fatalf("unexpected inbox event: %#v; expected %#v", event, want)
		}
		title := id + " title"
		if len(titles) > 0 {
			title = titles[0][id]
		}
		if anyString(item["title"]) != title || anyString(item["subject_ref"]) == "" || len(stringSliceAny(item["related_refs"])) == 0 {
			t.Fatalf("authorized inbox payload lost fields: %#v", item)
		}
		seen[id] = true
	}
	select {
	case event, ok := <-reader:
		if !ok {
			t.Fatal("sse stream closed while validating continued polling")
		}
		t.Fatalf("unexpected extra inbox event: %#v", event)
	case <-time.After(150 * time.Millisecond):
	}
}

func assertPrivacyInboxList(t *testing.T, baseURL, token string, want map[string]string) {
	t.Helper()
	resp := getJSONExpectStatusWithAuth(t, baseURL+"/inbox", token, http.StatusOK)
	defer resp.Body.Close()
	var payload struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Items) != len(want) {
		t.Fatalf("list visibility differs from stream: %#v; want %#v", payload.Items, want)
	}
	for _, item := range payload.Items {
		body, ok := want[anyString(item["id"])]
		if !ok || anyString(item["body"]) != body {
			t.Fatalf("unexpected listed item: %#v", item)
		}
	}
}
