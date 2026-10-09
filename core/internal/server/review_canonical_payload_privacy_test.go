package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

func TestCanonicalPayloadPrivacyAcrossEventSurfaces(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("HTTP/storage integration")
	}
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "payload-owner", "payload-owner", "Owner", "payload-owner-token")
	reader := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "payload-reader", "payload-reader", "Reader", "payload-reader-token")
	store := env.primitiveStore.(*primitives.Store)
	topic, err := store.CreateTopic(ctx, owner.ActorID, map[string]any{"title": "Public payload topic", "summary": "Public context"})
	if err != nil {
		t.Fatal(err)
	}
	tid := anyString(topic.Topic["thread_id"])
	card := round4PrivateCard(t, store, owner.ActorID)
	appendAsk := func(title, subject string, related []string) map[string]any {
		t.Helper()
		event, err := store.AppendEvent(ctx, owner.ActorID, map[string]any{"type": "human_attention_requested", "thread_id": tid, "refs": []string{"thread:" + tid}, "payload": map[string]any{"kind": "ask", "requester_actor_id": owner.ActorID, "request_id": title, "title": title, "body": title + " body", "subject_ref": subject, "related_refs": related, "response_proposals": []string{"Proceed", "Wait"}}})
		if err != nil {
			t.Fatal(err)
		}
		return event
	}
	visible := appendAsk("Visible payload ask", "thread:"+tid, nil)
	hidden := []map[string]any{appendAsk("Confidential subject ask", anyString(card["ref"]), nil), appendAsk("Confidential related ask", "thread:"+tid, []string{anyString(card["ref"])})}
	// Rebuild from canonical events, rather than inserting prefiltered inbox rows.
	if err = refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: store}, tid, time.Now().UTC(), owner.ActorID); err != nil {
		t.Fatal(err)
	}
	canonical, err := store.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: tid})
	if err != nil || len(canonical) != 3 {
		t.Fatalf("canonical projection lost asks: %d %v", len(canonical), err)
	}
	secrets := []string{"Confidential", anyString(card["ref"]), anyString(card["id"])}
	for _, event := range hidden {
		secrets = append(secrets, anyString(event["id"]))
		resp := getJSONExpectStatusWithAuth(t, env.server.URL+"/events/"+anyString(event["id"]), reader.AccessToken, 404)
		resp.Body.Close()
		resp = getJSONExpectStatusWithAuth(t, env.server.URL+"/events/"+anyString(event["id"]), owner.AccessToken, 200)
		resp.Body.Close()
	}
	for _, path := range []string{"/events?thread_id=" + tid + "&type=human_attention_requested&limit=1", "/threads/" + tid + "/context", "/threads/" + tid + "/workspace", "/threads/" + tid + "/timeline", "/topics/" + anyString(topic.Topic["id"]) + "/timeline"} {
		resp := getJSONExpectStatusWithAuth(t, env.server.URL+path, reader.AccessToken, 200)
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		round4NoSecrets(t, path, string(raw), secrets)
		if !strings.Contains(string(raw), "Visible payload ask") {
			t.Fatalf("%s lost visible event: %s", path, raw)
		}
		if strings.HasPrefix(path, "/events?") {
			var body map[string]any
			if err = json.Unmarshal(raw, &body); err != nil {
				t.Fatal(err)
			}
			if body["page_info"].(map[string]any)["has_more"] != false {
				t.Fatalf("private payload affected event pagination: %s", raw)
			}
		}
		resp = getJSONExpectStatusWithAuth(t, env.server.URL+path, owner.AccessToken, 200)
		raw, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		if !strings.Contains(string(raw), "Confidential") {
			t.Fatalf("owner lost private event on %s: %s", path, raw)
		}
	}
	streamCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(streamCtx, "GET", env.server.URL+"/stream/events?thread_id="+tid+"&type=human_attention_requested", nil)
	req.Header.Set("Authorization", "Bearer "+reader.AccessToken)
	req.Header.Set("Last-Event-ID", anyString(visible["id"]))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	frames := bufio.NewReader(resp.Body)
	first := round4SSEFrame(t, frames)
	round4NoSecrets(t, "initial SSE", first, secrets)
	if strings.Contains(first, "data:") {
		t.Fatalf("resume replayed history: %s", first)
	}
	appendAsk("Confidential incremental ask", anyString(card["ref"]), nil)
	next := round4SSEFrame(t, frames)
	round4NoSecrets(t, "incremental SSE", next, secrets)
	if strings.Contains(next, "data:") {
		t.Fatalf("private event serialized: %s", next)
	}
	appendAsk("Incremental visible ask", "thread:"+tid, nil)
	for {
		next = round4SSEFrame(t, frames)
		round4NoSecrets(t, "incremental visible SSE", next, secrets)
		if strings.Contains(next, "Incremental visible ask") {
			break
		}
	}
}

func TestBoardWorkspaceAuthorizesCanonicalInboxBeforeCounting(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("HTTP/storage integration")
	}
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "board-inbox-owner", "board-inbox-owner", "Owner", "board-inbox-owner-token")
	reader := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "board-inbox-reader", "board-inbox-reader", "Reader", "board-inbox-reader-token")
	store := env.primitiveStore.(*primitives.Store)
	public, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "Public inbox board"})
	if err != nil {
		t.Fatal(err)
	}
	card := round4PrivateCard(t, store, owner.ActorID)
	tid := anyString(public["thread_id"])
	for _, ask := range []struct {
		title, subject string
		related        []string
	}{{"Visible board ask", "thread:" + tid, nil}, {"Confidential board subject", anyString(card["ref"]), nil}, {"Confidential board related", "thread:" + tid, []string{anyString(card["ref"])}}} {
		if _, err = store.AppendEvent(ctx, owner.ActorID, map[string]any{"type": "human_attention_requested", "thread_id": tid, "refs": []string{"thread:" + tid}, "payload": map[string]any{"kind": "ask", "requester_actor_id": owner.ActorID, "request_id": ask.title, "title": ask.title, "body": ask.title + " body", "subject_ref": ask.subject, "related_refs": ask.related, "response_proposals": []string{"Proceed", "Wait"}}}); err != nil {
			t.Fatal(err)
		}
	}
	if err = refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: store}, tid, time.Now().UTC(), owner.ActorID); err != nil {
		t.Fatal(err)
	}
	for _, who := range []struct {
		token string
		count int
	}{{reader.AccessToken, 1}, {owner.AccessToken, 3}} {
		resp := getJSONExpectStatusWithAuth(t, env.server.URL+"/boards/"+anyString(public["id"])+"/workspace", who.token, 200)
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var body map[string]any
		if err = json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		inbox := body["inbox"].(map[string]any)
		if inbox["count"] != float64(who.count) || len(inbox["items"].([]any)) != who.count {
			t.Fatalf("unauthorized inbox counts: %s", raw)
		}
		if who.count == 1 {
			round4NoSecrets(t, "board workspace", string(raw), []string{"Confidential", anyString(card["ref"]), anyString(card["id"])})
		}
	}
	canonical, err := store.ListDerivedInboxItems(ctx, primitives.DerivedInboxListFilter{ThreadID: tid})
	if err != nil || len(canonical) != 3 {
		t.Fatalf("canonical projection changed: %d %v", len(canonical), err)
	}
}

func TestCanonicalPayloadReferenceNormalizationAndEventChains(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("HTTP/storage integration")
	}
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "chain-owner", "chain-owner", "Owner", "chain-owner-token")
	reader := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "chain-reader", "chain-reader", "Reader", "chain-reader-token")
	store := env.primitiveStore.(*primitives.Store)
	topic, err := store.CreateTopic(ctx, owner.ActorID, map[string]any{"title": "Public chain topic", "summary": "Public context"})
	if err != nil {
		t.Fatal(err)
	}
	tid := anyString(topic.Topic["thread_id"])
	card := round4PrivateCard(t, store, owner.ActorID)
	appendAsk := func(title, subject string, related []string) map[string]any {
		t.Helper()
		event, err := store.AppendEvent(ctx, owner.ActorID, map[string]any{"type": "human_attention_requested", "thread_id": tid, "refs": []string{"thread:" + tid}, "payload": map[string]any{"kind": "ask", "requester_actor_id": owner.ActorID, "request_id": title, "title": title, "subject_ref": subject, "related_refs": related, "response_proposals": []string{"Proceed", "Wait"}}})
		if err != nil {
			t.Fatal(err)
		}
		return event
	}
	private := appendAsk("Confidential direct subject", anyString(card["ref"]), nil)
	hidden := []map[string]any{private}
	var threadHandle string
	if err = env.workspace.DB().QueryRowContext(ctx, "SELECT handle FROM threads WHERE id=?", anyString(card["thread_id"])).Scan(&threadHandle); err != nil {
		t.Fatal(err)
	}
	hidden = append(hidden, appendAsk("Confidential thread handle", "thread:"+threadHandle, nil), appendAsk("Confidential normalized card handle", "card:"+strings.ReplaceAll(strings.ToUpper(anyString(card["handle"])), "-", "._/"), nil))
	var revisionID string
	var revisionNumber int
	if err = env.workspace.DB().QueryRowContext(ctx, "SELECT revision_id,revision_number FROM card_revisions WHERE card_id=? ORDER BY revision_number LIMIT 1", anyString(card["id"])).Scan(&revisionID, &revisionNumber); err != nil {
		t.Fatal(err)
	}
	hidden = append(hidden, appendAsk("Confidential revision ID", "card_revision:"+revisionID, nil), appendAsk("Confidential revision handle", "card_revision:"+strings.ReplaceAll(strings.ToUpper(anyString(card["handle"]))+"-r"+fmt.Sprint(revisionNumber), "-", "._/"), nil))
	if _, err = env.workspace.DB().ExecContext(ctx, "INSERT INTO resource_handle_aliases(resource_type,resource_id,alias_handle,canonical_handle,created_at) VALUES('card',?,?,?,?)", anyString(card["id"]), "old-private-card", anyString(card["handle"]), time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	hidden = append(hidden, appendAsk("Confidential old card alias", "card:OLD_PRIVATE_CARD", nil))
	longCard, err := store.CreateWork(ctx, owner.ActorID, anyString(card["board_id"]), map[string]any{"title": strings.Repeat("z", 64)})
	if err != nil {
		t.Fatal(err)
	}
	hidden = append(hidden, appendAsk("Confidential long revision handle", "card_revision:"+anyString(longCard["handle"])+"-r1", nil))
	for _, padding := range []string{" ", "\t\n", "\u00a0\u2003"} {
		padded := padding + "card" + padding + ":" + padding + anyString(card["id"]) + padding
		hidden = append(hidden, appendAsk("Confidential padded subject "+padding, padded, nil), appendAsk("Confidential padded related "+padding, "thread:"+tid, []string{padded}))
	}
	chain := appendAsk("Confidential event subject", "event:"+anyString(private["id"]), nil)
	hidden = append(hidden, chain, appendAsk("Confidential event related", "thread:"+tid, []string{anyString(private["ref"])}), appendAsk("Confidential chained event", "\u00a0event \t: \n"+anyString(chain["id"])+"\u2003", nil))
	// Legacy flat payloads remain subject to the same historical read policy.
	legacy := appendAsk("Confidential legacy subject", "thread:"+tid, nil)
	legacyPayload, _ := json.Marshal(map[string]any{"title": "Confidential legacy subject", "subject_ref": anyString(card["ref"]), "related_refs": []string{}})
	if _, err = env.workspace.DB().ExecContext(ctx, "UPDATE events SET payload_json=? WHERE id=?", string(legacyPayload), anyString(legacy["id"])); err != nil {
		t.Fatal(err)
	}
	hidden = append(hidden, legacy)
	legacyRelated := appendAsk("Confidential legacy related", "thread:"+tid, nil)
	legacyPayload, _ = json.Marshal(map[string]any{"title": "Confidential legacy related", "subject_ref": "thread:" + tid, "related_refs": anyString(card["ref"])})
	if _, err = env.workspace.DB().ExecContext(ctx, "UPDATE events SET payload_json=? WHERE id=?", string(legacyPayload), anyString(legacyRelated["id"])); err != nil {
		t.Fatal(err)
	}
	hidden = append(hidden, legacyRelated)
	// Open payload objects historically permit scalar related_refs. Preserve
	// readable strings while authorizing a scalar typed reference.
	for _, value := range []string{"ordinary text", "thread:" + tid, anyString(card["ref"]), "event:" + anyString(private["id"])} {
		event := appendAsk("Scalar related "+value, "thread:"+tid, nil)
		if _, err = env.workspace.DB().ExecContext(ctx, "UPDATE events SET payload_json=json_set(payload_json,'$.payload.related_refs',?) WHERE id=?", value, anyString(event["id"])); err != nil {
			t.Fatal(err)
		}
		if value == anyString(card["ref"]) || strings.HasPrefix(value, "event:") {
			hidden = append(hidden, event)
		} else {
			resp := getJSONExpectStatusWithAuth(t, env.server.URL+"/events/"+anyString(event["id"]), reader.AccessToken, 200)
			resp.Body.Close()
		}
	}
	// UNION must terminate public cycles, and a hidden reachable subject must
	// still deny a cycle rather than being lost at an arbitrary depth cap.
	visibleA := appendAsk("Visible cycle A", "thread:"+tid, nil)
	visibleB := appendAsk("Visible cycle B", "event:"+anyString(visibleA["id"]), nil)
	hiddenA := appendAsk("Confidential cycle A", "event:"+anyString(private["id"]), nil)
	hiddenB := appendAsk("Confidential cycle B", "event:"+anyString(hiddenA["id"]), nil)
	for _, edge := range []struct{ event, target map[string]any }{{visibleA, visibleB}, {hiddenA, hiddenB}} {
		refs, _ := json.Marshal([]string{"thread:" + tid, "event:" + anyString(edge.target["id"])})
		if _, err = env.workspace.DB().ExecContext(ctx, "UPDATE events SET refs_json=? WHERE id=?", string(refs), anyString(edge.event["id"])); err != nil {
			t.Fatal(err)
		}
	}
	hidden = append(hidden, hiddenA, hiddenB)
	for _, event := range hidden {
		resp := getJSONExpectStatusWithAuth(t, env.server.URL+"/events/"+anyString(event["id"]), reader.AccessToken, 404)
		resp.Body.Close()
		resp = getJSONExpectStatusWithAuth(t, env.server.URL+"/events/"+anyString(event["id"]), owner.AccessToken, 200)
		resp.Body.Close()
	}
	for _, event := range []map[string]any{visibleA, visibleB} {
		resp := getJSONExpectStatusWithAuth(t, env.server.URL+"/events/"+anyString(event["id"]), reader.AccessToken, 200)
		resp.Body.Close()
	}
	resp := getJSONExpectStatusWithAuth(t, env.server.URL+"/events?thread_id="+tid+"&type=human_attention_requested&limit=2", reader.AccessToken, 200)
	raw, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	round4NoSecrets(t, "event chains", string(raw), []string{"Confidential", anyString(card["ref"]), anyString(card["id"])})
	if !strings.Contains(string(raw), "Visible cycle A") || !strings.Contains(string(raw), "Visible cycle B") {
		t.Fatalf("hidden chains changed accessible pagination: %s", raw)
	}
	// Historical event access is independent of the subject's active lifecycle.
	if _, err = env.workspace.DB().ExecContext(ctx, "UPDATE cards SET archived_at=? WHERE id=?", time.Now().UTC().Format(time.RFC3339Nano), anyString(card["id"])); err != nil {
		t.Fatal(err)
	}
	resp = getJSONExpectStatusWithAuth(t, env.server.URL+"/events/"+anyString(private["id"]), owner.AccessToken, 200)
	resp.Body.Close()
	resp = getJSONExpectStatusWithAuth(t, env.server.URL+"/events/"+anyString(private["id"]), reader.AccessToken, 404)
	resp.Body.Close()
}

func round4PrivateCard(t *testing.T, store *primitives.Store, actor string) map[string]any {
	t.Helper()
	ctx := context.Background()
	board, err := store.CreateBoard(ctx, actor, map[string]any{"title": "Confidential payload board"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateWork(ctx, actor, anyString(board["id"]), map[string]any{"title": "Confidential payload card"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, actor, anyString(board["thread_id"]), map[string]any{"pm_actor_id": actor}, nil); err != nil {
		t.Fatal(err)
	}
	return card
}
func round4NoSecrets(t *testing.T, path, raw string, secrets []string) {
	t.Helper()
	for _, secret := range secrets {
		if strings.Contains(raw, secret) {
			t.Fatalf("%s leaked %s: %s", path, secret, raw)
		}
	}
}
func round4SSEFrame(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	data := ""
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		data += line
		if line == "\n" {
			return data
		}
	}
}
