package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

func TestTopicPatchAtomicOriginalReceiptAndAuthority(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	owner := seedNotificationTestAgent(t, env, "owner.topic-replay")
	ctx := context.Background()
	store := env.primitiveStore.(*primitives.Store)
	created, err := store.CreateTopic(ctx, owner.ActorID, map[string]any{"title": "Private original topic", "summary": "Original description", "related_refs": []string{}, "provenance": map[string]any{"sources": []string{"inferred"}, "notes": "Keep provenance notes", "custom_sequence": json.Number("9007199254740993")}})
	if err != nil {
		t.Fatal(err)
	}
	peer, err := store.CreateTopic(ctx, owner.ActorID, map[string]any{"title": "Other topic", "summary": "Other description"})
	if err != nil {
		t.Fatal(err)
	}
	id, thread := anyString(created.Topic["id"]), anyString(created.Topic["thread_id"])
	if _, err = store.PatchThread(ctx, owner.ActorID, thread, map[string]any{"pm_actor_id": owner.ActorID, "custom_extension": "Keep this metadata"}, nil); err != nil {
		t.Fatal(err)
	}
	contract, err := schema.Load("../../../contracts/anx-schema.yaml")
	if err != nil {
		t.Fatal(err)
	}
	opts := handlerOptions{primitiveStore: store, authStore: env.authStore, actorRegistry: env.registry, workspaceID: "ws_main", contract: contract}
	db := env.workspace.DB()
	db.SetMaxOpenConns(1)
	primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{MaxBlobBytes: 1 << 30, MaxArtifacts: 1000})(store)
	scoped := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: owner.ActorID})
	call := func(body map[string]any, target string) *httptest.ResponseRecorder {
		bounded, cancel := context.WithTimeout(scoped, 5*time.Second)
		defer cancel()
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPatch, "/topics/"+target, strings.NewReader(string(raw))).WithContext(bounded)
		cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: owner.ActorID})
		out := httptest.NewRecorder()
		handlePatchTopic(out, req, opts, target)
		return out
	}
	snapshot := func() string {
		t.Helper()
		var data []string
		for _, table := range []string{"topics", "threads", "ref_edges", "events", "derived_topic_dirty_queue", "blob_usage_ledger", "blob_usage_totals", "idempotency_replays", "resource_access_edges", "resource_access_epoch"} {
			rows, err := db.Query("SELECT * FROM " + table)
			if err != nil {
				t.Fatal(err)
			}
			columns, _ := rows.Columns()
			for rows.Next() {
				values := make([]any, len(columns))
				ptrs := make([]any, len(values))
				for i := range values {
					ptrs[i] = &values[i]
				}
				if err = rows.Scan(ptrs...); err != nil {
					t.Fatal(err)
				}
				raw, _ := json.Marshal(values)
				data = append(data, table+":"+string(raw))
			}
			if err = rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
		}
		sort.Strings(data)
		return strings.Join(data, "\n")
	}
	body := map[string]any{"actor_id": owner.ActorID, "request_key": "original-topic", "if_updated_at": created.Topic["updated_at"], "patch": map[string]any{"title": "  PRIVATE edited topic  ", "summary": "Keep the complete authored description.", "related_refs": []string{}, "provenance": map[string]any{"sources": []string{"inferred"}}}}
	body["patch"].(map[string]any)["workspace_move"] = map[string]any{"source_sequence": json.Number("9007199254740993")}
	clone := func(body map[string]any) map[string]any {
		raw, _ := json.Marshal(body)
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.UseNumber()
		var out map[string]any
		if err := decoder.Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	withoutBase := clone(body)
	delete(withoutBase, "if_updated_at")
	if out := call(withoutBase, id); out.Code != 400 {
		t.Fatal("keyed patch missing base accepted", out.Code, out.Body.String())
	}
	// Cold ledger initialization must join outer rollback too.
	if _, err = db.Exec(`DELETE FROM blob_usage_ledger; DELETE FROM blob_usage_totals`); err != nil {
		t.Fatal(err)
	}
	for _, trigger := range []string{
		`CREATE TRIGGER reject_topic BEFORE UPDATE ON topics BEGIN SELECT RAISE(ABORT,'topic failed'); END`,
		`CREATE TRIGGER reject_topic BEFORE UPDATE ON threads BEGIN SELECT RAISE(ABORT,'thread failed'); END`,
		`CREATE TRIGGER reject_topic BEFORE INSERT ON ref_edges BEGIN SELECT RAISE(ABORT,'refs failed'); END`,
		`CREATE TRIGGER reject_topic BEFORE INSERT ON events BEGIN INSERT INTO missing_topic_failure_table VALUES(1); END`,
		`CREATE TRIGGER reject_topic BEFORE INSERT ON idempotency_replays BEGIN SELECT RAISE(ABORT,'receipt failed'); END`,
	} {
		if _, err = db.Exec(trigger); err != nil {
			t.Fatal(err)
		}
		before := snapshot()
		if out := call(body, id); out.Code != 500 || snapshot() != before {
			t.Fatal("partial topic commit", trigger, out.Code, out.Body.String())
		}
		if _, err = db.Exec(`DROP TRIGGER reject_topic`); err != nil {
			t.Fatal(err)
		}
	}
	before := snapshot()
	primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{MaxBlobBytes: 1})(store)
	if out := call(body, id); out.Code != 409 || !strings.Contains(out.Body.String(), "quota_exceeded") || snapshot() != before {
		t.Fatal("quota bypass or partial commit", out.Code, out.Body.String())
	}
	primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{MaxBlobBytes: 1 << 30, MaxArtifacts: 1000})(store)
	var eventCount int
	if err = db.QueryRow(`SELECT count(*) FROM events`).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	results := make(chan *httptest.ResponseRecorder, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- call(body, id) }()
	}
	wg.Wait()
	close(results)
	original := ""
	for out := range results {
		if out.Code != 200 {
			t.Fatal(out.Code, out.Body.String())
		}
		if original == "" {
			original = out.Body.String()
		}
		if original != out.Body.String() {
			t.Fatal("receipt changed", original, out.Body.String())
		}
	}
	if !strings.Contains(original, "9007199254740993") {
		t.Fatal("receipt rounded integer metadata", original)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM events`).Scan(&count); err != nil || count != eventCount+1 {
		t.Fatal("duplicate event", count, eventCount, err)
	}
	var result struct {
		Topic map[string]any `json:"topic"`
	}
	if err = json.Unmarshal([]byte(original), &result); err != nil {
		t.Fatal(err)
	}
	if provenance := result.Topic["provenance"].(map[string]any); provenance["notes"] != "Keep provenance notes" {
		t.Fatal("partial provenance patch dropped notes", provenance)
	}
	loaded, err := store.GetThread(ctx, thread)
	if err != nil || loaded["pm_actor_id"] != owner.ActorID || loaded["custom_extension"] != "Keep this metadata" {
		t.Fatal("thread metadata or privacy boundary lost", loaded, err)
	}
	for _, mutate := range []func(map[string]any){
		func(body map[string]any) { body["patch"].(map[string]any)["title"] = "PRIVATE edited topic" },
		func(body map[string]any) {
			body["patch"].(map[string]any)["related_refs"] = []string{"topic:" + anyString(peer.Topic["id"])}
		},
		func(body map[string]any) { body["if_updated_at"] = result.Topic["updated_at"] },
		func(body map[string]any) { body["actor_id"] = "" },
		func(body map[string]any) {
			body["patch"].(map[string]any)["workspace_move"] = map[string]any{"source_sequence": json.Number("9007199254740992")}
		},
	} {
		changed := clone(body)
		mutate(changed)
		if out := call(changed, id); out.Code != 409 {
			t.Fatal("changed body accepted", out.Code, out.Body.String())
		}
	}
	if out := call(body, anyString(peer.Topic["id"])); out.Code != 409 {
		t.Fatal("changed target accepted", out.Code, out.Body.String())
	}
	otherActor := clone(body)
	otherActor["actor_id"] = "replacement-actor"
	if out := call(otherActor, id); out.Code != 403 {
		t.Fatal("actor substitution accepted", out.Code, out.Body.String())
	}
	for _, key := range []string{strings.Repeat("x", 257), "invalid\x00key"} {
		changed := map[string]any{"actor_id": owner.ActorID, "request_key": key, "if_updated_at": result.Topic["updated_at"], "patch": map[string]any{"summary": "Invalid key"}}
		if out := call(changed, id); out.Code != 400 {
			t.Fatal("invalid key accepted", out.Code, out.Body.String())
		}
	}
	later := map[string]any{"actor_id": owner.ActorID, "request_key": "later-topic", "if_updated_at": result.Topic["updated_at"], "patch": map[string]any{"summary": "Later description"}}
	if out := call(later, id); out.Code != 200 {
		t.Fatal(out.Code, out.Body.String())
	}
	var storedMetadata string
	if err = db.QueryRow(`SELECT extensions_json FROM topics WHERE id=?`, id).Scan(&storedMetadata); err != nil || !strings.Contains(storedMetadata, "9007199254740993") {
		t.Fatal("later edit rounded metadata", storedMetadata, err)
	}
	// Quota and metadata changes cannot replace the original receipt.
	primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{MaxBlobBytes: 1})(store)
	if out := call(body, id); out.Code != 200 || out.Body.String() != original {
		t.Fatal("original recovery failed", out.Code, out.Body.String())
	}
	primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{MaxBlobBytes: 1 << 30})(store)
	// A new key with the old base must conflict; unkeyed calls keep their guard.
	for _, key := range []string{"fresh-stale", ""} {
		changed := clone(body)
		changed["request_key"] = key
		if out := call(changed, id); out.Code != 409 {
			t.Fatal("stale base accepted", out.Code, out.Body.String())
		}
	}
	current, err := store.GetTopic(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if out := call(map[string]any{"actor_id": owner.ActorID, "if_updated_at": current["updated_at"], "patch": map[string]any{"summary": "", "provenance": map[string]any{"sources": []string{"inferred"}, "notes": ""}}}, id); out.Code != 200 {
		t.Fatal("legacy clearing failed", out.Code, out.Body.String())
	}
	cleared, err := store.GetTopic(ctx, id)
	if err != nil || cleared["provenance"].(map[string]any)["notes"] != "" {
		t.Fatal("explicit notes clear failed", cleared, err)
	}
	if _, err = store.PatchThread(ctx, owner.ActorID, thread, map[string]any{"pm_actor_id": "replacement-owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if out := call(body, id); (out.Code != 403 && out.Code != 404) || strings.Contains(out.Body.String(), "PRIVATE") {
		t.Fatal("revoked receipt leaked", out.Code, out.Body.String())
	}
}
