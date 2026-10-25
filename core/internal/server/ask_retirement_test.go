package server

import (
	"context"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

func TestAskRetirementAuthorizationAndIntegrity(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{bootstrapToken: testBootstrapToken})
	ctx := context.Background()
	store := env.primitiveStore.(*primitives.Store)
	requester := seedNotificationTestAgent(t, env, "retire.requester")
	other := seedNotificationTestAgent(t, env, "retire.other")
	admin := seedNotificationTestAgent(t, env, "retire.admin")
	human := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "retire-human", "retire-human-actor", "retire-human", "retire-human-token")
	hp, err := env.authStore.AuthenticateAccessToken(ctx, human.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	ap, err := env.authStore.AuthenticateAccessToken(ctx, admin.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = env.authStore.SetAuthAdmin(ctx, ap.AgentID, true, hp); err != nil {
		t.Fatal(err)
	}
	work, err := store.CreateWork(ctx, requester.ActorID, "", map[string]any{"title": "Retirement subject", "phase": "in_progress"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.GetBoardCard(ctx, "", anyString(work["id"]))
	if err != nil {
		t.Fatal(err)
	}
	fileAsk := func() map[string]any {
		t.Helper()
		ask, e := store.AppendTaskAttentionEvent(ctx, requester.ActorID, map[string]any{"type": "human_attention_requested", "thread_id": card["thread_id"], "refs": []string{anyString(card["ref"]), "thread:" + anyString(card["thread_id"])}, "summary": "Original?", "payload": map[string]any{"kind": "ask", "title": "Original?", "requester_actor_id": requester.ActorID, "subject_ref": card["ref"], "response_proposals": []any{"Ship original", "Wait original"}, "recommended_response_index": 1, "revision": 7, "by": "2099-01-01T00:00:00Z", "delivery": "batch"}})
		if e != nil {
			t.Fatal(e)
		}
		return ask
	}
	request := func(key string) map[string]any {
		return map[string]any{"request_key": key, "reason": "The old rollout is no longer relevant.", "evidence": []string{anyString(card["ref"]), "https://github.com/Git-on-my-level/agent-nexus/pull/484"}}
	}
	retire := func(ask map[string]any, token string, body map[string]any) (int, map[string]any) {
		return hostHTTP(t, "POST", env.server.URL+"/asks/"+url.PathEscape("event:"+anyString(ask["id"]))+"/retire", token, body)
	}
	for _, tc := range []struct {
		name, token string
		status      int
	}{{"requester", requester.AccessToken, 200}, {"human", human.AccessToken, 200}, {"admin", admin.AccessToken, 200}, {"other", other.AccessToken, 403}, {"anonymous", "", 401}} {
		t.Run(tc.name, func(t *testing.T) {
			ask := fileAsk()
			status, p := retire(ask, tc.token, request(tc.name))
			hostStatus(t, status, tc.status, p)
			if status != 200 {
				return
			}
			status, p = retire(ask, tc.token, map[string]any{"request_key": "undo-" + tc.name, "undo": true, "if_retirement": p["retirement_event_ref"]})
			hostStatus(t, status, 200, p)
		})
	}
	ask := fileAsk()
	ref := "event:" + anyString(ask["id"])
	var original string
	if err = env.workspace.DB().QueryRow(`SELECT payload_json FROM events WHERE id=?`, ask["id"]).Scan(&original); err != nil {
		t.Fatal(err)
	}
	refresh := func() {
		t.Helper()
		if e := refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: store}, anyString(card["thread_id"]), time.Now().UTC(), requester.ActorID); e != nil {
			t.Fatal(e)
		}
	}
	status, revision := hostHTTP(t, "POST", env.server.URL+"/asks/"+url.PathEscape(ref)+"/revise", human.AccessToken, map[string]any{"if_revision": 0, "request_key": "retire-rewritten", "presentation": map[string]any{"title": "Clearer original question?"}})
	hostStatus(t, status, 200, revision)
	refresh()
	item, ok := deriveHumanAttentionInboxItem(ask)
	if !ok {
		t.Fatal("missing item")
	}
	itemPath := env.server.URL + "/inbox/" + url.PathEscape(item.ID)
	status, before := hostHTTP(t, "GET", itemPath, human.AccessToken, nil)
	hostStatus(t, status, 200, before)
	status, first := retire(ask, admin.AccessToken, request("roundtrip"))
	hostStatus(t, status, 200, first)
	if first["status"] != "withdrawn_as_stale" || first["notified_actor_id"] != requester.ActorID {
		t.Fatal(first)
	}
	status, replay := retire(ask, admin.AccessToken, request("roundtrip"))
	hostStatus(t, status, 200, replay)
	if !reflect.DeepEqual(first, replay) {
		t.Fatal("replay changed")
	}
	changed := request("roundtrip")
	changed["reason"] = "different"
	status, p := retire(ask, admin.AccessToken, changed)
	hostStatus(t, status, 409, p)
	status, p = hostHTTP(t, "GET", env.server.URL+"/asks/"+url.PathEscape(ref), human.AccessToken, nil)
	hostStatus(t, status, 200, p)
	if p["status"] != "withdrawn_as_stale" || p["retirement_event_ref"] != first["retirement_event_ref"] {
		t.Fatal(p)
	}
	// Both a channel's cached inbox link and a captured answer fail closed,
	// including before the asynchronous projection maintainer has run.
	status, p = hostHTTP(t, "GET", itemPath, human.AccessToken, nil)
	hostStatus(t, status, 404, p)
	status, p = hostHTTP(t, "POST", itemPath+"/respond", human.AccessToken, map[string]any{"response_text": "Ship original", "outcome": "answered", "idempotency_key": "retired-button"})
	if status != 404 && status != 409 {
		t.Fatalf("retired answer %d %#v", status, p)
	}
	if !strings.Contains(strings.ToLower(anyString(asMap(p["error"])["message"])), "no longer open") {
		t.Fatal(p)
	}
	scoped := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: human.ActorID})
	brief, e := store.Brief(scoped, primitives.BriefReader{ActorID: human.ActorID, Kind: "human"}, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	for _, d := range brief["decisions"].([]map[string]any) {
		if d["event_ref"] == ref {
			t.Fatal("retired ask remains in Decide")
		}
	}
	var answers int
	_ = env.workspace.DB().QueryRow(`SELECT count(*) FROM human_attention_response_claims WHERE request_event_id=?`, ask["id"]).Scan(&answers)
	if answers != 0 {
		t.Fatal("retirement counted as human answer")
	}
	wakes, e := store.ListAgentWakeups(primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: requester.ActorID}), primitives.AgentWakeupListFilter{TargetActorID: requester.ActorID})
	if e != nil {
		t.Fatal(e)
	}
	found := false
	event := asMap(first["event"])
	for _, w := range wakes {
		if w.TriggerEventID == event["id"] {
			found = true
			if !strings.Contains(w.TriggerText, "no longer relevant") || w.TargetActorID != requester.ActorID {
				t.Fatal(w)
			}
		}
	}
	if !found {
		t.Fatal("requester not notified")
	}
	// Rebuild while retired, then undo and rebuild: canonical audit replay must
	// also restore an item that has already been pruned from the projection.
	refresh()
	undoBody := map[string]any{"request_key": "undo", "undo": true, "if_retirement": first["retirement_event_ref"]}
	status, p = retire(ask, other.AccessToken, undoBody)
	hostStatus(t, status, 403, p)
	status, undone := retire(ask, human.AccessToken, undoBody)
	hostStatus(t, status, 200, undone)
	if undone["status"] != "open" {
		t.Fatal(undone)
	}
	refresh()
	status, after := hostHTTP(t, "GET", itemPath, human.AccessToken, nil)
	hostStatus(t, status, 200, after)
	for _, field := range []string{"title", "presentation_revision", "presentation_rewrite", "id", "request_event_ref", "subject_ref", "requester_actor_id", "response_proposals", "recommended_response_index", "revision", "by", "delivery"} {
		if !reflect.DeepEqual(asMap(before["item"])[field], asMap(after["item"])[field]) {
			t.Fatalf("binding %s changed", field)
		}
	}
	status, second := retire(ask, human.AccessToken, request("again"))
	hostStatus(t, status, 200, second)
	status, p = retire(ask, human.AccessToken, map[string]any{"request_key": "stale-undo", "undo": true, "if_retirement": first["retirement_event_ref"]})
	hostStatus(t, status, 409, p)
	status, p = retire(ask, human.AccessToken, map[string]any{"request_key": "undo-again", "undo": true, "if_retirement": second["retirement_event_ref"]})
	hostStatus(t, status, 200, p)
	refresh()
	status, answer := hostHTTP(t, "POST", itemPath+"/respond", human.AccessToken, map[string]any{"response_text": "Ship original", "outcome": "answered", "idempotency_key": "after-undo"})
	hostStatus(t, status, 201, answer)
	if asMap(answer["notify"])["target_actor_id"] != requester.ActorID {
		t.Fatal(answer)
	}
	status, p = retire(ask, human.AccessToken, map[string]any{"request_key": "cannot-undo-answer", "undo": true, "if_retirement": second["retirement_event_ref"]})
	hostStatus(t, status, 409, p)
	var final string
	_ = env.workspace.DB().QueryRow(`SELECT payload_json FROM events WHERE id=?`, ask["id"]).Scan(&final)
	if final != original {
		t.Fatal("original ask changed")
	}

	// Reject semantic fields and malformed retirement requests rather than
	// silently accepting an attempted change to the original ask.
	invalidAsk := fileAsk()
	for _, bad := range []map[string]any{
		{"request_key": "bad", "reason": "stale", "evidence": []string{anyString(card["ref"])}, "requester_actor_id": other.ActorID},
		{"request_key": "bad", "reason": "stale", "evidence": []string{anyString(card["ref"])}, "response_proposals": []string{"changed"}},
		{"request_key": "bad", "reason": "stale"},
		{"request_key": "bad", "reason": 42, "evidence": []string{anyString(card["ref"])}},
		{"request_key": "bad", "undo": true},
		{"request_key": "bad", "reason": "stale", "evidence": []string{anyString(card["ref"])}, "undo": true, "if_retirement": first["retirement_event_ref"]},
	} {
		status, p = retire(invalidAsk, human.AccessToken, bad)
		hostStatus(t, status, 400, p)
	}
	for _, eventType := range []string{primitives.AskRetiredEventType, primitives.AskReopenedEventType} {
		status, p = hostHTTP(t, "POST", env.server.URL+"/events", human.AccessToken, map[string]any{"event": map[string]any{"type": eventType, "refs": []string{anyString(card["ref"])}, "payload": map[string]any{"request_event_id": invalidAsk["id"]}}})
		hostStatus(t, status, 400, p)
	}
	// Undo cannot revive a closed subject, even if the retirement was valid.
	closedAsk := fileAsk()
	status, closedRetirement := retire(closedAsk, human.AccessToken, request("closed-subject"))
	hostStatus(t, status, 200, closedRetirement)
	if _, err = env.workspace.DB().Exec(`UPDATE cards SET archived_at='2026-10-26T00:00:00Z' WHERE id=?`, card["id"]); err != nil {
		t.Fatal(err)
	}
	status, p = retire(closedAsk, human.AccessToken, map[string]any{"request_key": "closed-undo", "undo": true, "if_retirement": closedRetirement["retirement_event_ref"]})
	hostStatus(t, status, 409, p)
	if _, err = env.workspace.DB().Exec(`UPDATE cards SET archived_at=NULL WHERE id=?`, card["id"]); err != nil {
		t.Fatal(err)
	}
	// Authority is read inside the mutation transaction, after body upload.
	pending := fileAsk()
	blocked := &blockedAdminBody{entered: make(chan struct{}), release: make(chan struct{}), Reader: strings.NewReader(`{"request_key":"revoked","reason":"stale","evidence":["` + anyString(card["ref"]) + `"]}`)}
	requestHTTP := httptest.NewRequest("POST", "/asks/"+anyString(pending["id"])+"/retire", blocked)
	requestHTTP.Header.Set("Authorization", "Bearer "+admin.AccessToken)
	requestHTTP.Header.Set("Content-Type", "application/json")
	var release sync.Once
	unblock := func() { release.Do(func() { close(blocked.release) }) }
	defer unblock()
	recorder := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { env.server.Config.Handler.ServeHTTP(recorder, requestHTTP); close(done) }()
	select {
	case <-blocked.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("body not read")
	}
	if _, err = env.authStore.SetAuthAdmin(ctx, ap.AgentID, false, hp); err != nil {
		t.Fatal(err)
	}
	unblock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("retirement stuck")
	}
	if recorder.Code != 403 {
		t.Fatalf("revoked admin %d %s", recorder.Code, recorder.Body.String())
	}
	if _, err = env.authStore.SetAuthAdmin(ctx, ap.AgentID, true, hp); err != nil {
		t.Fatal(err)
	}

	// A concurrent retire/answer must have exactly one canonical winner.
	racing := fileAsk()
	refresh()
	raceItem, _ := deriveHumanAttentionInboxItem(racing)
	var wg sync.WaitGroup
	codes := make(chan int, 2)
	wg.Add(2)
	go func() { defer wg.Done(); s, _ := retire(racing, admin.AccessToken, request("race")); codes <- s }()
	go func() {
		defer wg.Done()
		s, _ := hostHTTP(t, "POST", env.server.URL+"/inbox/"+url.PathEscape(raceItem.ID)+"/respond", human.AccessToken, map[string]any{"response_text": "Ship original", "outcome": "answered", "idempotency_key": "race-answer"})
		codes <- s
	}()
	wg.Wait()
	close(codes)
	wins := 0
	for s := range codes {
		if s == 200 || s == 201 {
			wins++
		} else if s != 404 && s != 409 {
			t.Fatalf("race status %d", s)
		}
	}
	if wins != 1 {
		t.Fatalf("race winners %d", wins)
	}
}
