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
)

// Exercise the real scoped SQLite writer with quotas and a single connection.
func TestDocumentLifecycleAtomicOriginalReceipt(t *testing.T) {
	requireIntegrationTest(t)
	for _, action := range []string{"archive", "unarchive", "trash", "restore"} {
		t.Run(action, func(t *testing.T) {
			env := newAuthIntegrationEnv(t, authIntegrationOptions{})
			owner := seedNotificationTestAgent(t, env, "owner.lifecycle")
			store := env.primitiveStore.(*primitives.Store)
			ctx := context.Background()
			doc, _, err := store.CreateDocument(ctx, owner.ActorID, map[string]any{"title": "Private original"}, "Private original bytes", "text", nil)
			if err != nil {
				t.Fatal(err)
			}
			peer, _, err := store.CreateDocument(ctx, owner.ActorID, map[string]any{"title": "Other"}, "Other bytes", "text", nil)
			if err != nil {
				t.Fatal(err)
			}
			id := anyString(doc["id"])
			thread := anyString(doc["thread_id"])
			if _, err := store.PatchThread(ctx, owner.ActorID, thread, map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
				t.Fatal(err)
			}
			if action == "unarchive" {
				doc, _, err = store.ArchiveDocument(ctx, owner.ActorID, id)
			}
			if action == "restore" {
				doc, _, err = store.TrashDocument(ctx, owner.ActorID, id, "prepare")
			}
			if err != nil {
				t.Fatal(err)
			}
			primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{MaxArtifacts: 1000, MaxDocuments: 1000, MaxBlobBytes: 1 << 30})(store)
			env.workspace.DB().SetMaxOpenConns(1)
			scoped := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: owner.ActorID})
			opts := handlerOptions{primitiveStore: store, authStore: env.authStore, actorRegistry: env.registry, workspaceID: "ws_main"}
			write := func(body map[string]any, target string) *httptest.ResponseRecorder {
				callCtx, cancel := context.WithTimeout(scoped, 5*time.Second)
				defer cancel()
				raw, _ := json.Marshal(body)
				req := httptest.NewRequest(http.MethodPost, "/docs/"+target+"/"+action, strings.NewReader(string(raw))).WithContext(callCtx)
				cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: owner.ActorID})
				out := httptest.NewRecorder()
				handleDocumentLifecycle(out, req, opts, target, action)
				return out
			}
			body := map[string]any{"actor_id": owner.ActorID, "request_key": "original-lifecycle", "if_updated_at": doc["updated_at"], "reason": "Original reason"}
			snapshot := func() string {
				t.Helper()
				var all []string
				for _, table := range []string{"documents", "document_revisions", "artifacts", "events", "ref_edges", "derived_topic_dirty_queue", "blob_usage_ledger", "blob_usage_totals", "idempotency_replays"} {
					rows, err := env.workspace.DB().Query("SELECT * FROM " + table)
					if err != nil {
						t.Fatal(err)
					}
					columns, _ := rows.Columns()
					var data []string
					for rows.Next() {
						values := make([]any, len(columns))
						ptr := make([]any, len(columns))
						for i := range values {
							ptr[i] = &values[i]
						}
						if err := rows.Scan(ptr...); err != nil {
							t.Fatal(err)
						}
						raw, _ := json.Marshal(values)
						data = append(data, string(raw))
					}
					if err := rows.Err(); err != nil {
						t.Fatal(err)
					}
					rows.Close()
					sort.Strings(data)
					all = append(all, table+strings.Join(data, ";"))
				}
				return strings.Join(all, "\n")
			}
			if action == "trash" || action == "restore" {
				before := snapshot()
				primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{MaxBlobBytes: 1})(store)
				out := write(body, id)
				if out.Code != 409 || !strings.Contains(out.Body.String(), "quota_exceeded") || snapshot() != before {
					t.Fatal("quota failure left lifecycle state", out.Code, out.Body.String())
				}
				primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{MaxArtifacts: 1000, MaxDocuments: 1000, MaxBlobBytes: 1 << 30})(store)
			}
			// A cold quota ledger rebuild must join the lifecycle writer and
			// roll back with a failed event or receipt, without another connection.
			if action == "trash" || action == "restore" {
				for _, table := range []string{"blob_usage_ledger", "blob_usage_totals"} {
					if _, err := env.workspace.DB().Exec("DELETE FROM " + table); err != nil {
						t.Fatal(err)
					}
				}
			}
			triggers := []string{
				`CREATE TRIGGER reject_lifecycle BEFORE UPDATE ON documents BEGIN SELECT RAISE(ABORT,'document failed'); END`,
				`CREATE TRIGGER reject_lifecycle BEFORE INSERT ON idempotency_replays BEGIN SELECT RAISE(ABORT,'receipt failed'); END`,
			}
			if action == "trash" || action == "restore" {
				triggers = append(triggers, `CREATE TRIGGER reject_lifecycle BEFORE INSERT ON events BEGIN INSERT INTO missing_lifecycle_failure_table VALUES (1); END`)
			} else {
				triggers = append(triggers, `CREATE TRIGGER reject_lifecycle BEFORE INSERT ON derived_topic_dirty_queue BEGIN SELECT RAISE(ABORT,'projection failed'); END`)
			}
			for _, trigger := range triggers {
				before := snapshot()
				if _, err := env.workspace.DB().Exec(trigger); err != nil {
					t.Fatal(err)
				}
				out := write(body, id)
				if out.Code != 500 || snapshot() != before {
					t.Fatalf("partial lifecycle survived: %s: %d %s", trigger, out.Code, out.Body.String())
				}
				if _, err := env.workspace.DB().Exec("DROP TRIGGER reject_lifecycle"); err != nil {
					t.Fatal(err)
				}
			}
			var beforeEvents int
			if err := env.workspace.DB().QueryRow("SELECT count(*) FROM events").Scan(&beforeEvents); err != nil {
				t.Fatal(err)
			}
			results := make(chan *httptest.ResponseRecorder, 8)
			var group sync.WaitGroup
			for i := 0; i < 8; i++ {
				group.Add(1)
				go func() { defer group.Done(); results <- write(body, id) }()
			}
			group.Wait()
			close(results)
			original := ""
			for out := range results {
				if out.Code != 200 {
					t.Fatal(out.Code, out.Body.String())
				}
				if original == "" {
					original = out.Body.String()
				} else if original != out.Body.String() {
					t.Fatal("different concurrent receipts")
				}
			}
			var receipt map[string]any
			if err := json.Unmarshal([]byte(original), &receipt); err != nil {
				t.Fatal(err)
			}
			confirmed := receipt["document"].(map[string]any)
			if confirmed["updated_at"] == doc["updated_at"] || confirmed["updated_by"] != owner.ActorID {
				t.Fatal("lifecycle did not advance concurrency token", confirmed)
			}
			var afterEvents int
			if err := env.workspace.DB().QueryRow("SELECT count(*) FROM events").Scan(&afterEvents); err != nil {
				t.Fatal(err)
			}
			expectedEvents := 0
			if action == "trash" || action == "restore" {
				expectedEvents = 1
			}
			if afterEvents-beforeEvents != expectedEvents {
				t.Fatal("duplicate lifecycle events", afterEvents-beforeEvents)
			}
			for field, value := range map[string]any{"reason": "Changed", "if_updated_at": confirmed["updated_at"]} {
				changed := copyMutationBody(body)
				changed[field] = value
				out := write(changed, id)
				if out.Code != 409 {
					t.Fatal("changed request accepted", field, out.Code, out.Body.String())
				}
			}
			equivalentBase := copyMutationBody(body)
			equivalentBase["if_updated_at"] = strings.TrimSuffix(anyString(doc["updated_at"]), "Z") + "+00:00"
			if out := write(equivalentBase, id); out.Code != 409 {
				t.Fatal("changed base representation accepted", out.Code, out.Body.String())
			}
			if out := write(body, anyString(peer["id"])); out.Code != 409 {
				t.Fatal("changed target accepted", out.Code, out.Body.String())
			}
			missing := copyMutationBody(body)
			delete(missing, "if_updated_at")
			if out := write(missing, id); out.Code != 400 {
				t.Fatal("missing base accepted", out.Code, out.Body.String())
			}
			for _, key := range []string{strings.Repeat("x", 257), "invalid\x00key"} {
				invalid := copyMutationBody(body)
				invalid["request_key"] = key
				if out := write(invalid, id); out.Code != 400 {
					t.Fatal("invalid key accepted", out.Code, out.Body.String())
				}
			}
			impersonated := copyMutationBody(body)
			impersonated["actor_id"] = "replacement"
			if out := write(impersonated, id); out.Code != 403 {
				t.Fatal("actor replacement accepted", out.Code, out.Body.String())
			}
			stale := copyMutationBody(body)
			stale["request_key"] = "fresh-stale"
			if out := write(stale, id); out.Code != 409 {
				t.Fatal("stale new intent accepted", out.Code, out.Body.String())
			}
			unkeyedStale := copyMutationBody(body)
			delete(unkeyedStale, "request_key")
			if out := write(unkeyedStale, id); out.Code != 409 {
				t.Fatal("unkeyed stale base accepted", out.Code, out.Body.String())
			}
			if action == "archive" || action == "trash" {
				noOp, _, err := store.ApplyDocumentLifecycle(ctx, owner.ActorID, id, action, "ignored later reason", nil)
				if err != nil || noOp["updated_at"] != confirmed["updated_at"] {
					t.Fatal("legacy no-op changed base", noOp, err)
				}
			}
			if action == "trash" || action == "restore" {
				primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{MaxBlobBytes: 1})(store)
				if out := write(body, id); out.Code != 200 || out.Body.String() != original {
					t.Fatal("quota change blocked confirmed recovery", out.Code, out.Body.String())
				}
				primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{MaxArtifacts: 1000, MaxDocuments: 1000, MaxBlobBytes: 1 << 30})(store)
			}
			// Later lifecycle and metadata edits must not replace the original receipt.
			switch action {
			case "archive":
				_, _, err = store.UnarchiveDocument(ctx, owner.ActorID, id)
			case "unarchive":
				_, _, err = store.ArchiveDocument(ctx, owner.ActorID, id)
			case "trash":
				_, _, err = store.RestoreDocument(ctx, owner.ActorID, id, "later")
			case "restore":
				_, _, err = store.TrashDocument(ctx, owner.ActorID, id, "later")
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = store.PatchDocument(ctx, owner.ActorID, id, map[string]any{"title": "Later title"}, nil); err != nil {
				t.Fatal(err)
			}
			if out := write(body, id); out.Code != 200 || out.Body.String() != original {
				t.Fatal("receipt advanced", out.Code, out.Body.String())
			}
			if _, err = store.PatchThread(ctx, owner.ActorID, thread, map[string]any{"pm_actor_id": "replacement"}, nil); err != nil {
				t.Fatal(err)
			}
			if out := write(body, id); out.Code != 404 || strings.Contains(out.Body.String(), "Private original") {
				t.Fatal("revoked receipt leaked", out.Code, out.Body.String())
			}
		})
	}
}

func TestDocumentKnowledgePatchAtomicOriginalReceipt(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	owner := seedNotificationTestAgent(t, env, "owner.knowledge")
	store := env.primitiveStore.(*primitives.Store)
	ctx := context.Background()
	doc, _, err := store.CreateDocument(ctx, owner.ActorID, map[string]any{"title": "Knowledge"}, "Keep original bytes", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	id := anyString(doc["id"])
	thread := anyString(doc["thread_id"])
	if _, err = store.PatchThread(ctx, owner.ActorID, thread, map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{MaxArtifacts: 1000, MaxBlobBytes: 1 << 30})(store)
	env.workspace.DB().SetMaxOpenConns(1)
	scoped := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: owner.ActorID})
	opts := handlerOptions{primitiveStore: store, authStore: env.authStore, actorRegistry: env.registry, workspaceID: "ws_main"}
	write := func(body map[string]any) *httptest.ResponseRecorder {
		callCtx, cancel := context.WithTimeout(scoped, 5*time.Second)
		defer cancel()
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPatch, "/docs/"+id, strings.NewReader(string(raw))).WithContext(callCtx)
		cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: owner.ActorID})
		out := httptest.NewRecorder()
		handlePatchDocument(out, req, opts, id)
		return out
	}
	patch := map[string]any{"source": "runbook source", "summary": "Original knowledge", "tags": []string{"knowledge", "operations"}, "hosts": []string{"laptop"}, "verified_at": "2026-10-13T12:00:00Z"}
	body := map[string]any{"actor_id": owner.ActorID, "request_key": "knowledge-original", "if_updated_at": doc["updated_at"], "patch": patch}
	if _, err = env.workspace.DB().Exec(`CREATE TRIGGER reject_knowledge BEFORE INSERT ON idempotency_replays WHEN NEW.scope='docs.patch' BEGIN SELECT RAISE(ABORT,'receipt failed'); END`); err != nil {
		t.Fatal(err)
	}
	if out := write(body); out.Code != 500 {
		t.Fatal("failed receipt confirmed", out.Code, out.Body.String())
	}
	current, _, err := store.GetDocument(ctx, id)
	if err != nil || current["updated_at"] != doc["updated_at"] || current["source"] != doc["source"] {
		t.Fatal("metadata survived failed receipt", current, err)
	}
	if _, err = env.workspace.DB().Exec("DROP TRIGGER reject_knowledge"); err != nil {
		t.Fatal(err)
	}
	results := make(chan *httptest.ResponseRecorder, 8)
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() { defer group.Done(); results <- write(body) }()
	}
	group.Wait()
	close(results)
	original := ""
	for out := range results {
		if out.Code != 200 {
			t.Fatal(out.Code, out.Body.String())
		}
		if original == "" {
			original = out.Body.String()
		} else if original != out.Body.String() {
			t.Fatal("different knowledge receipts")
		}
	}
	var receipt map[string]any
	if err = json.Unmarshal([]byte(original), &receipt); err != nil {
		t.Fatal(err)
	}
	confirmed := receipt["document"].(map[string]any)
	for key, value := range patch {
		actual, _ := json.Marshal(confirmed[key])
		expected, _ := json.Marshal(value)
		if string(actual) != string(expected) {
			t.Fatal("lost knowledge metadata", key, string(actual), string(expected))
		}
	}
	for key := range patch {
		changed := copyMutationBody(body)
		changedPatch := copyMutationBody(patch)
		if key == "tags" || key == "hosts" {
			changedPatch[key] = []string{}
		} else {
			changedPatch[key] = ""
		}
		changed["patch"] = changedPatch
		if out := write(changed); out.Code != 409 {
			t.Fatal("changed knowledge accepted", key, out.Code, out.Body.String())
		}
	}
	if _, _, err = store.PatchDocument(ctx, owner.ActorID, id, map[string]any{"source": "", "summary": "", "tags": []string{}, "hosts": []string{}, "verified_at": ""}, nil); err != nil {
		t.Fatal(err)
	}
	if out := write(body); out.Code != 200 || out.Body.String() != original {
		t.Fatal("knowledge receipt advanced", out.Code, out.Body.String())
	}
	if _, err = store.PatchThread(ctx, owner.ActorID, thread, map[string]any{"pm_actor_id": "replacement"}, nil); err != nil {
		t.Fatal(err)
	}
	if out := write(body); out.Code != 404 || strings.Contains(out.Body.String(), "Original knowledge") {
		t.Fatal("revoked knowledge leaked", out.Code, out.Body.String())
	}
}
