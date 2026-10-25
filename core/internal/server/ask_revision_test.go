package server

import (
	"context"
	"fmt"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

func TestAskRevisionAuthorizationAndIntegrity(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	ctx := context.Background()
	store := env.primitiveStore.(*primitives.Store)
	requester := seedNotificationTestAgent(t, env, "revision.requester")
	other := seedNotificationTestAgent(t, env, "revision.other")
	admin := seedNotificationTestAgent(t, env, "revision.admin")
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "revision-human", "revision-human-actor", "revision-human", "revision-human-token")
	humanPrincipal, err := env.authStore.AuthenticateAccessToken(ctx, human.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	adminPrincipal, err := env.authStore.AuthenticateAccessToken(ctx, admin.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = env.authStore.SetAuthAdmin(ctx, adminPrincipal.AgentID, true, humanPrincipal); err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateWork(ctx, requester.ActorID, "", map[string]any{"title": "Revision subject", "phase": "in_progress"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := env.primitiveStore.GetBoardCard(ctx, "", anyString(work["id"]))
	if err != nil {
		t.Fatal(err)
	}
	fileAsk := func() map[string]any {
		t.Helper()
		ask, err := store.AppendTaskAttentionEvent(ctx, requester.ActorID, map[string]any{"type": "human_attention_requested", "thread_id": card["thread_id"], "refs": []string{anyString(card["ref"]), "thread:" + anyString(card["thread_id"])}, "summary": "Original?", "payload": map[string]any{"kind": "ask", "title": "Original?", "body": "Original technical body.", "situation": "Original situation.", "requester_actor_id": requester.ActorID, "subject_ref": card["ref"], "response_proposals": []any{"Ship exact option", "Wait exact option"}, "recommended_response_index": 1, "revision": 7, "by": "2099-01-01T00:00:00Z", "delivery": "batch"}})
		if err != nil {
			t.Fatal(err)
		}
		return ask
	}
	req := func(revision int, key string) map[string]any {
		return map[string]any{"if_revision": revision, "request_key": key, "presentation": map[string]any{"title": "Plain question?", "situation": "Clear situation.", "recommendation": map[string]any{"action": "Ship exact option", "why": "This helps customers."}, "details": "Explanatory text only."}}
	}
	revise := func(ask map[string]any, token string, body map[string]any) (int, map[string]any) {
		return hostHTTP(t, "POST", env.server.URL+"/asks/"+url.PathEscape("event:"+anyString(ask["id"]))+"/revise", token, body)
	}
	for _, tc := range []struct {
		name, token string
		status      int
	}{{"requester", requester.AccessToken, 200}, {"human", human.AccessToken, 200}, {"admin", admin.AccessToken, 200}, {"other", other.AccessToken, 403}, {"anonymous", "", 401}} {
		t.Run(tc.name, func(t *testing.T) {
			ask := fileAsk()
			status, p := revise(ask, tc.token, req(0, "matrix-"+tc.name))
			if status != tc.status {
				t.Fatalf("status %d want %d: %#v", status, tc.status, p)
			}
		})
	}
	ask := fileAsk()
	ref := "event:" + anyString(ask["id"])
	var originalRaw string
	if err = env.workspace.DB().QueryRow(`SELECT payload_json FROM events WHERE id=?`, ask["id"]).Scan(&originalRaw); err != nil {
		t.Fatal(err)
	}
	if err = refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: env.primitiveStore}, anyString(card["thread_id"]), time.Now().UTC(), requester.ActorID); err != nil {
		t.Fatal(err)
	}
	originalItem, ok := deriveHumanAttentionInboxItem(ask)
	if !ok {
		t.Fatal("missing item")
	}
	status, before := hostHTTP(t, "GET", env.server.URL+"/inbox/"+url.PathEscape(originalItem.ID), human.AccessToken, nil)
	hostStatus(t, status, 200, before)
	status, state := hostHTTP(t, "GET", env.server.URL+"/asks/"+url.PathEscape(ref)+"/presentation", human.AccessToken, nil)
	hostStatus(t, status, 200, state)
	if state["presentation_revision"] != float64(0) {
		t.Fatal(state)
	}
	body := req(0, "round-trip")
	status, first := revise(ask, admin.AccessToken, body)
	hostStatus(t, status, 200, first)
	status, replay := revise(ask, admin.AccessToken, body)
	hostStatus(t, status, 200, replay)
	if !reflect.DeepEqual(first, replay) {
		t.Fatalf("replay changed %#v vs %#v", first, replay)
	}
	status, p := revise(ask, admin.AccessToken, req(0, "stale"))
	hostStatus(t, status, 409, p)
	changed := req(1, "round-trip")
	status, p = revise(ask, admin.AccessToken, changed)
	hostStatus(t, status, 409, p)
	for _, field := range []string{"requester_actor_id", "requester_agent_id", "subject_ref", "kind", "response_options", "response_proposals", "proposals", "recommended_response_index", "revision", "decision_revision", "by", "expires_at", "delivery", "blocks", "evidence", "body"} {
		for _, nested := range []bool{false, true} {
			bad := req(1, "immutable-"+field+fmt.Sprint(nested))
			if nested {
				bad["presentation"].(map[string]any)[field] = "changed"
			} else {
				bad[field] = "changed"
			}
			status, p = revise(ask, human.AccessToken, bad)
			if status != 400 {
				t.Fatalf("immutable %s nested=%v: %d %#v", field, nested, status, p)
			}
		}
	}
	for _, bad := range []map[string]any{{"recommendation": map[string]any{"action": "Ship", "why": "Helps", "recommended_response_index": 1}}, {"alternatives": []any{map[string]any{"action": "Wait", "consequence": "Slower", "option_index": 0}}}, {"title": nil}, {"impact": strings.Repeat("界", 4001)}} {
		status, p = revise(ask, human.AccessToken, map[string]any{"if_revision": 1, "request_key": "bad-shape", "presentation": bad})
		hostStatus(t, status, 400, p)
	}
	status, after := hostHTTP(t, "GET", env.server.URL+"/inbox/"+url.PathEscape(originalItem.ID), human.AccessToken, nil)
	hostStatus(t, status, 200, after)
	oldItem := before["item"].(map[string]any)
	newItem := after["item"].(map[string]any)
	for _, field := range []string{"id", "request_event_ref", "subject_ref", "requester_actor_id", "response_proposals", "recommended_response_index", "revision", "by", "delivery"} {
		if !reflect.DeepEqual(oldItem[field], newItem[field]) {
			t.Fatalf("answer binding %s changed: %#v -> %#v", field, oldItem[field], newItem[field])
		}
	}
	if newItem["title"] != "Plain question?" || newItem["presentation_rewrite"] == nil {
		t.Fatal(newItem)
	}
	brief, err := store.Brief(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: human.ActorID}), primitives.BriefReader{ActorID: human.ActorID, Kind: "human"}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range brief["decisions"].([]map[string]any) {
		if item["event_ref"] == ref {
			found = true
			if item["title"] != "Plain question?" || item["presentation_rewrite"] == nil {
				t.Fatal(item)
			}
		}
	}
	if !found {
		t.Fatal("rewritten ask absent from Decide")
	}
	status, undo := revise(ask, human.AccessToken, map[string]any{"if_revision": 1, "request_key": "undo", "undo": true})
	hostStatus(t, status, 200, undo)
	if !reflect.DeepEqual(state["presentation"], undo["presentation"]) {
		t.Fatalf("undo lost original: %#v", undo)
	}
	status, p = revise(ask, human.AccessToken, req(2, "rewrite-again"))
	hostStatus(t, status, 200, p)
	var currentRaw string
	_ = env.workspace.DB().QueryRow(`SELECT payload_json FROM events WHERE id=?`, ask["id"]).Scan(&currentRaw)
	if currentRaw != originalRaw {
		t.Fatal("original canonical event was modified")
	}
	var count int
	_ = env.workspace.DB().QueryRow(`SELECT count(*) FROM events WHERE type='human_attention_revised' AND json_extract(payload_json,'$.payload.request_event_id')=?`, ask["id"]).Scan(&count)
	if count != 3 {
		t.Fatalf("want 3 audit events, got %d", count)
	}
	// A button captured before any rewrite still submits its exact original text
	// to the same inbox and releases the answer to the original requester.
	status, answer := hostHTTP(t, "POST", env.server.URL+"/inbox/"+url.PathEscape(anyString(oldItem["id"]))+"/respond", human.AccessToken, map[string]any{"response_text": oldItem["response_proposals"].([]any)[0], "outcome": "answered", "idempotency_key": "captured-button"})
	hostStatus(t, status, 201, answer)
	notify := answer["notify"].(map[string]any)
	if notify["target_actor_id"] != requester.ActorID {
		t.Fatalf("answer rerouted: %#v", answer)
	}
	status, p = revise(ask, human.AccessToken, req(3, "closed"))
	hostStatus(t, status, 409, p)
	// Audit events cannot be forged through the ordinary event writer.
	status, p = hostHTTP(t, "POST", env.server.URL+"/events", other.AccessToken, map[string]any{"event": map[string]any{"type": primitives.AskRevisedEventType, "summary": "forged", "refs": []string{ref}, "provenance": map[string]any{"sources": []string{"inferred"}}, "payload": map[string]any{"request_event_id": ask["id"], "presentation_revision": 99, "previous": map[string]any{}, "presentation": map[string]any{"title": "forged"}}}})
	hostStatus(t, status, 400, p)
	// Two simultaneous writers based on one presentation: one wins, one conflicts.
	concurrent := fileAsk()
	start := make(chan struct{})
	statuses := make(chan int, 2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			<-start
			status, _ := revise(concurrent, human.AccessToken, req(0, fmt.Sprintf("concurrent-%d", i)))
			statuses <- status
		}(i)
	}
	close(start)
	a, b := <-statuses, <-statuses
	if !((a == 200 && b == 409) || (a == 409 && b == 200)) {
		t.Fatalf("concurrent writes: %d %d", a, b)
	}
	// Admission snapshots cannot retain a revoked auth-admin grant.
	pending := fileAsk()
	blocked := &blockedAdminBody{Reader: strings.NewReader(`{"if_revision":0,"request_key":"revoked-during-upload","presentation":{"title":"Denied?"}}`), entered: make(chan struct{}), release: make(chan struct{})}
	var release sync.Once
	unblock := func() { release.Do(func() { close(blocked.release) }) }
	defer unblock()
	request := httptest.NewRequest("POST", "/asks/"+anyString(pending["id"])+"/revise", blocked)
	request.Header.Set("Authorization", "Bearer "+admin.AccessToken)
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); env.server.Config.Handler.ServeHTTP(recorder, request) }()
	select {
	case <-blocked.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("body not read")
	}
	if _, err = env.authStore.SetAuthAdmin(ctx, adminPrincipal.AgentID, false, humanPrincipal); err != nil {
		t.Fatal(err)
	}
	unblock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("write stuck")
	}
	if recorder.Code != 403 {
		t.Fatalf("revoked admin: %d %s", recorder.Code, recorder.Body.String())
	}
	// Humans and explicitly granted admin agents still cannot revise a private
	// card inherited through its board, even when the ask thread was public.
	if _, err = env.authStore.SetAuthAdmin(ctx, adminPrincipal.AgentID, true, humanPrincipal); err != nil {
		t.Fatal(err)
	}
	board, err := store.GetBoard(ctx, anyString(card["board_id"]))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, requester.ActorID, anyString(board["thread_id"]), map[string]any{"pm_actor_id": requester.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{human.AccessToken, admin.AccessToken, other.AccessToken} {
		status, p = revise(pending, token, req(0, "private-card"))
		hostStatus(t, status, 404, p)
		status, p = hostHTTP(t, "GET", env.server.URL+"/asks/"+anyString(pending["id"])+"/presentation", token, nil)
		hostStatus(t, status, 404, p)
	}
}
