package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
)

// lifecycleBatchCall posts one batch straight to the handler on a real store,
// the way the route does, under a deadline so a self-deadlock fails the test
// instead of hanging it.
func lifecycleBatchCall(t *testing.T, ctx context.Context, opts handlerOptions, actorID, path string, body map[string]any) (int, map[string]any) {
	t.Helper()
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(raw))).WithContext(bounded)
	cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: actorID})
	out := httptest.NewRecorder()
	if path == "/events/lifecycle" {
		handleLifecycleBatch(out, req, opts, "/events/", true, handleEventLifecycle)
	} else {
		handleLifecycleBatch(out, req, opts, "/docs/", false, handleDocumentLifecycle)
	}
	if bounded.Err() != nil {
		t.Fatalf("batch did not finish before its deadline: %v", bounded.Err())
	}
	payload := map[string]any{}
	_ = json.Unmarshal(out.Body.Bytes(), &payload)
	return out.Code, payload
}

// lifecycleSingleCall posts one item through the single route's handler.
func lifecycleSingleCall(t *testing.T, ctx context.Context, opts handlerOptions, actorID, kind, id, action string, body map[string]any) (int, string) {
	t.Helper()
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/"+kind+"/"+id+"/"+action, strings.NewReader(string(raw))).WithContext(bounded)
	cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: actorID})
	out := httptest.NewRecorder()
	if kind == "events" {
		handleEventLifecycle(out, req, opts, id, action)
	} else {
		handleDocumentLifecycle(out, req, opts, id, action)
	}
	return out.Code, out.Body.String()
}

func lifecycleBatchResults(t *testing.T, payload map[string]any) []map[string]any {
	t.Helper()
	raw, _ := payload["results"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for i, item := range raw {
		result, _ := item.(map[string]any)
		if index, _ := result["index"].(float64); int(index) != i {
			t.Fatalf("result %d out of order: %#v", i, result)
		}
		out = append(out, result)
	}
	return out
}

func lifecycleItem(id, action string, body map[string]any) map[string]any {
	item := map[string]any{"id": id, "action": action}
	for k, v := range body {
		item[k] = v
	}
	return item
}

// One batch on the real scoped SQLite writer, with quotas enforced and a
// single connection: any read or write on the item path that left the batch
// transaction for another connection would block until the deadline.
func TestEventLifecycleBatchCommitsSuccessesAndReplaysThroughEitherRoute(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	owner := seedNotificationTestAgent(t, env, "owner.lifecycle-batch")
	store := env.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	topic, err := store.CreateTopic(ctx, owner.ActorID, map[string]any{"title": "Batch lifecycle", "summary": "Synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	thread := anyString(topic.Topic["thread_id"])
	for _, id := range []string{"batch-a", "batch-b", "batch-c", "batch-d"} {
		if _, err := store.AppendEvent(ctx, owner.ActorID, map[string]any{"id": id, "type": "message_posted", "thread_id": thread, "refs": []any{"thread:" + thread}, "payload": map[string]any{"text": id}}); err != nil {
			t.Fatal(err)
		}
	}
	env.workspace.DB().SetMaxOpenConns(1)
	primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{MaxBlobBytes: 1 << 30, MaxArtifacts: 1000})(store)
	scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: owner.ActorID})
	opts := handlerOptions{primitiveStore: store, authStore: env.authStore, actorRegistry: env.registry, workspaceID: "ws_main"}
	fresh := map[string]any{"archived_at": nil, "trashed_at": nil}
	keyed := func(key string) map[string]any {
		return map[string]any{"actor_id": owner.ActorID, "request_key": key, "reason": "Batch reason", "if_lifecycle": fresh}
	}
	// batch-b's write fails inside the batch; only its savepoint rolls back.
	if _, err := env.workspace.DB().Exec(`CREATE TRIGGER reject_b BEFORE UPDATE ON events WHEN NEW.id='batch-b' BEGIN SELECT RAISE(ABORT,'b rejected'); END`); err != nil {
		t.Fatal(err)
	}
	selected := map[string]any{"batch-a": fresh, "batch-b": fresh, "batch-c": fresh, "batch-d": fresh}
	batch := map[string]any{"selected_bases": selected, "items": []any{
		lifecycleItem("batch-a", "trash", keyed("key-a")),
		lifecycleItem("batch-b", "trash", keyed("key-b")),
		lifecycleItem("batch-missing", "trash", keyed("key-missing")),
		lifecycleItem("batch-c", "unarchive", keyed("key-c")),
		lifecycleItem("batch-d", "archive", keyed("key-d")),
	}}
	status, payload := lifecycleBatchCall(t, scope, opts, owner.ActorID, "/events/lifecycle", batch)
	if status != 200 {
		t.Fatalf("batch status %d %#v", status, payload)
	}
	results := lifecycleBatchResults(t, payload)
	want := []struct {
		status string
		code   float64
	}{{"done", 200}, {"failed", 500}, {"failed", 404}, {"failed", 409}, {"done", 200}}
	if len(results) != len(want) {
		t.Fatalf("results %#v", payload)
	}
	for i, w := range want {
		if results[i]["status"] != w.status || results[i]["http_status"] != w.code {
			t.Fatalf("item %d: want %s %v, got %#v", i, w.status, w.code, results[i])
		}
	}
	state := func(id string) (string, string) {
		event, err := store.GetEvent(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return anyString(event["archived_at"]), anyString(event["trashed_at"])
	}
	if _, trashed := state("batch-a"); trashed == "" {
		t.Fatal("successful item before a failure did not commit")
	}
	if _, trashed := state("batch-b"); trashed != "" {
		t.Fatal("failed item committed")
	}
	if archived, _ := state("batch-d"); archived == "" {
		t.Fatal("successful item after a failure did not commit")
	}
	var receipts int
	if err := env.workspace.DB().QueryRow(`SELECT count(*) FROM idempotency_replays WHERE request_key IN ('key-a','key-b','key-missing','key-c','key-d')`).Scan(&receipts); err != nil || receipts != 2 {
		t.Fatalf("want receipts for the two successes only, got %d (%v)", receipts, err)
	}

	// The single route, sent with the same body (the web UI adds the
	// selection's bases), replays the batch's original receipt.
	original, _ := json.Marshal(results[0]["response"])
	single := keyed("key-a")
	single["selected_bases"] = selected
	code, replay := lifecycleSingleCall(t, scope, opts, owner.ActorID, "events", "batch-a", "trash", single)
	if code != 200 || !jsonEqual(t, replay, string(original)) {
		t.Fatalf("single replay of a batch item %d %s, want %s", code, replay, original)
	}

	// The other way round: a single send, then the same item in a batch.
	if _, err := env.workspace.DB().Exec(`DROP TRIGGER reject_b`); err != nil {
		t.Fatal(err)
	}
	single = keyed("key-b")
	single["selected_bases"] = selected
	code, first := lifecycleSingleCall(t, scope, opts, owner.ActorID, "events", "batch-b", "trash", single)
	if code != 200 {
		t.Fatalf("single send %d %s", code, first)
	}
	status, payload = lifecycleBatchCall(t, scope, opts, owner.ActorID, "/events/lifecycle", map[string]any{"selected_bases": selected, "items": []any{lifecycleItem("batch-b", "trash", keyed("key-b")), lifecycleItem("batch-a", "trash", keyed("key-a"))}})
	results = lifecycleBatchResults(t, payload)
	if status != 200 || results[0]["status"] != "done" || results[1]["status"] != "done" {
		t.Fatalf("batch replay %d %#v", status, payload)
	}
	again, _ := json.Marshal(results[0]["response"])
	if !jsonEqual(t, first, string(again)) {
		t.Fatalf("batch replay of a single send changed the receipt: %s vs %s", again, first)
	}
	// A changed body under a used key conflicts, as it does on the single route.
	changed := keyed("key-b")
	changed["reason"] = "Changed intent"
	_, payload = lifecycleBatchCall(t, scope, opts, owner.ActorID, "/events/lifecycle", map[string]any{"selected_bases": selected, "items": []any{lifecycleItem("batch-b", "trash", changed)}})
	if results = lifecycleBatchResults(t, payload); results[0]["status"] != "failed" || results[0]["http_status"] != float64(409) {
		t.Fatalf("changed body accepted: %#v", results)
	}
}

func TestDocumentLifecycleBatchCommitsSuccessesAndReplaysThroughEitherRoute(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	owner := seedNotificationTestAgent(t, env, "owner.doc-batch")
	store := env.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	docs := map[string]map[string]any{}
	for _, name := range []string{"one", "two", "three"} {
		doc, _, err := store.CreateDocument(ctx, owner.ActorID, map[string]any{"title": "Batch " + name}, "Batch "+name+" bytes", "text", nil)
		if err != nil {
			t.Fatal(err)
		}
		docs[name] = doc
	}
	env.workspace.DB().SetMaxOpenConns(1)
	primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{MaxArtifacts: 1000, MaxDocuments: 1000, MaxBlobBytes: 1 << 30})(store)
	scope := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: owner.ActorID})
	opts := handlerOptions{primitiveStore: store, authStore: env.authStore, actorRegistry: env.registry, workspaceID: "ws_main"}
	keyed := func(name, key string) map[string]any {
		return map[string]any{"actor_id": owner.ActorID, "request_key": key, "if_updated_at": docs[name]["updated_at"], "reason": "Batch reason"}
	}
	stale := keyed("two", "key-two")
	stale["if_updated_at"] = "2000-01-01T00:00:00Z"
	batch := map[string]any{"items": []any{
		lifecycleItem(anyString(docs["one"]["id"]), "trash", keyed("one", "key-one")),
		lifecycleItem(anyString(docs["two"]["id"]), "archive", stale),
		lifecycleItem(anyString(docs["three"]["id"]), "archive", keyed("three", "key-three")),
	}}
	status, payload := lifecycleBatchCall(t, scope, opts, owner.ActorID, "/docs/lifecycle", batch)
	results := lifecycleBatchResults(t, payload)
	if status != 200 || len(results) != 3 || results[0]["status"] != "done" || results[1]["status"] != "failed" || results[1]["http_status"] != float64(409) || results[2]["status"] != "done" {
		t.Fatalf("batch %d %#v", status, payload)
	}
	for name, want := range map[string]string{"one": "trashed_at", "three": "archived_at"} {
		doc, _, err := store.GetDocument(ctx, anyString(docs[name]["id"]))
		if err != nil {
			t.Fatal(err)
		}
		if anyString(doc[want]) == "" {
			t.Fatalf("%s did not commit %s: %#v", name, want, doc)
		}
	}
	doc, _, err := store.GetDocument(ctx, anyString(docs["two"]["id"]))
	if err != nil || anyString(doc["archived_at"]) != "" {
		t.Fatalf("failed item changed: %#v %v", doc, err)
	}
	original, _ := json.Marshal(results[0]["response"])
	code, replay := lifecycleSingleCall(t, scope, opts, owner.ActorID, "docs", anyString(docs["one"]["id"]), "trash", keyed("one", "key-one"))
	if code != 200 || !jsonEqual(t, replay, string(original)) {
		t.Fatalf("single replay %d %s, want %s", code, replay, original)
	}
	// Sending the same batch again replays the successes.
	_, payload = lifecycleBatchCall(t, scope, opts, owner.ActorID, "/docs/lifecycle", batch)
	results = lifecycleBatchResults(t, payload)
	again, _ := json.Marshal(results[0]["response"])
	if results[0]["status"] != "done" || results[2]["status"] != "done" || string(again) != string(original) {
		t.Fatalf("batch replay %#v", payload)
	}
}

func TestLifecycleBatchRoutes(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	owner := seedNotificationTestAgent(t, env, "owner.batch-routes")
	store := env.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	topic, err := store.CreateTopic(ctx, owner.ActorID, map[string]any{"title": "Batch routes", "summary": "Synthetic"})
	if err != nil {
		t.Fatal(err)
	}
	thread := anyString(topic.Topic["thread_id"])
	for _, id := range []string{"route-a", "route-b", "route-c"} {
		if _, err := store.AppendEvent(ctx, owner.ActorID, map[string]any{"id": id, "type": "message_posted", "thread_id": thread, "refs": []any{"thread:" + thread}, "payload": map[string]any{"text": id}}); err != nil {
			t.Fatal(err)
		}
	}
	events := env.server.URL + "/events/lifecycle"
	item := func(id, action string) map[string]any { return map[string]any{"id": id, "action": action} }

	status, p := hostHTTP(t, "POST", events, "", map[string]any{"items": []any{item("route-a", "archive")}})
	hostStatus(t, status, 401, p)
	status, p = hostHTTP(t, "GET", events, owner.AccessToken, nil)
	if status != 404 {
		t.Fatalf("GET of a missing event named lifecycle: %d %#v", status, p)
	}
	// "lifecycle" stays a readable event id and document handle: only POST
	// is the batch.
	if _, err := store.AppendEvent(ctx, owner.ActorID, map[string]any{"id": "lifecycle", "type": "message_posted", "thread_id": thread, "refs": []any{"thread:" + thread}, "payload": map[string]any{"text": "named lifecycle"}}); err != nil {
		t.Fatal(err)
	}
	status, p = hostHTTP(t, "GET", events, owner.AccessToken, nil)
	hostStatus(t, status, 200, p)
	if doc, _, err := store.CreateDocument(ctx, owner.ActorID, map[string]any{"title": "Lifecycle"}, "Lifecycle bytes", "text", nil); err != nil {
		t.Fatal(err)
	} else if handle := anyString(doc["handle"]); handle == "lifecycle" || anyString(doc["id"]) == "lifecycle" {
		status, p = hostHTTP(t, "GET", env.server.URL+"/docs/lifecycle", owner.AccessToken, nil)
		hostStatus(t, status, 200, p)
	} else {
		t.Logf("document handle %q; GET /docs/lifecycle not exercised", handle)
	}
	tooMany := make([]any, lifecycleBatchMaxItems+1)
	for i := range tooMany {
		tooMany[i] = item("route-"+string(rune('a'+i%26))+strings.Repeat("x", i), "archive")
	}
	for _, bad := range []map[string]any{
		{"items": []any{}},
		{"items": tooMany},
		{"items": []any{item("route-a", "archive"), item("route-a", "trash")}},
		{"items": []any{item("route-a", "purge")}},
		{"items": []any{item("", "archive")}},
		{"items": []any{item("route-a/x", "archive")}},
		{"items": []any{map[string]any{"action": "archive"}}},
	} {
		status, p = hostHTTP(t, "POST", events, owner.AccessToken, bad)
		hostStatus(t, status, 400, p)
	}
	status, p = hostHTTP(t, "POST", env.server.URL+"/docs/lifecycle", owner.AccessToken, map[string]any{"selected_bases": map[string]any{}, "items": []any{item("doc", "archive")}})
	hostStatus(t, status, 400, p)

	status, p = hostHTTP(t, "POST", events, owner.AccessToken, map[string]any{"items": []any{item("route-a", "archive"), item("route-missing", "archive")}})
	hostStatus(t, status, 200, p)
	results := lifecycleBatchResults(t, p)
	if results[0]["status"] != "done" || results[1]["status"] != "failed" || results[1]["http_status"] != float64(404) {
		t.Fatalf("results %#v", p)
	}

	t.Run("time budget defers", func(t *testing.T) {
		saved := lifecycleBatchBudget
		lifecycleBatchBudget = 0
		t.Cleanup(func() { lifecycleBatchBudget = saved })
		// The first item always runs, so a request makes progress; the rest
		// wait for the next request once the lock budget is spent.
		status, p := hostHTTP(t, "POST", events, owner.AccessToken, map[string]any{"items": []any{item("route-c", "archive"), item("route-b", "archive")}})
		hostStatus(t, status, 200, p)
		if got := lifecycleBatchResults(t, p); got[0]["status"] != "done" || got[1]["status"] != "deferred" {
			t.Fatalf("want done then deferred, got %#v", got)
		}
		if event, _ := store.GetEvent(ctx, "route-b"); anyString(event["archived_at"]) != "" {
			t.Fatal("deferred item ran")
		}
	})

	t.Run("read-only workspace refuses unarchive and restore items only", func(t *testing.T) {
		readOnly := newReadOnlyAuthIntegrationServer(t, env)
		status, p := hostHTTP(t, "POST", readOnly.URL+"/events/lifecycle", owner.AccessToken, map[string]any{"items": []any{item("route-a", "unarchive"), item("route-b", "trash")}})
		hostStatus(t, status, 200, p)
		got := lifecycleBatchResults(t, p)
		errBody, _ := got[0]["error"].(map[string]any)
		if got[0]["status"] != "failed" || got[0]["http_status"] != float64(423) || errBody["code"] != "workspace_read_only" || got[1]["status"] != "done" {
			t.Fatalf("read-only results %#v", got)
		}
	})
}

func TestLifecycleBatchDefersPastTheWriteRate(t *testing.T) {
	requireIntegrationTest(t)
	// A burst of 2 covers the request and one more item; the third waits.
	env := newAuthIntegrationEnv(t, authIntegrationOptions{routeRateLimits: RouteRateLimits{WriteRequestsPerMinute: 1, WriteBurst: 2}})
	owner := seedNotificationTestAgent(t, env, "owner.batch-rate")
	store := env.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	var items []any
	for _, name := range []string{"a", "b", "c"} {
		doc, _, err := store.CreateDocument(ctx, owner.ActorID, map[string]any{"title": "Rate " + name}, "bytes "+name, "text", nil)
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, map[string]any{"id": doc["id"], "action": "archive"})
	}
	status, p := hostHTTP(t, "POST", env.server.URL+"/docs/lifecycle", owner.AccessToken, map[string]any{"items": items})
	hostStatus(t, status, 200, p)
	got := lifecycleBatchResults(t, p)
	if got[0]["status"] != "done" || got[1]["status"] != "done" || got[2]["status"] != "deferred" {
		t.Fatalf("want done, done, deferred; got %#v", got)
	}
}

func jsonEqual(t *testing.T, a, b string) bool {
	t.Helper()
	var left, right any
	if json.Unmarshal([]byte(a), &left) != nil || json.Unmarshal([]byte(b), &right) != nil {
		return false
	}
	l, _ := json.Marshal(left)
	r, _ := json.Marshal(right)
	return string(l) == string(r)
}
