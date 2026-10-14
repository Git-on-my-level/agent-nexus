package server

import (
	"agent-nexus-core/internal/primitives"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAuthoringReplayCommitsNestedWritesAndReceiptTogether(t *testing.T) {
	for _, operation := range []string{"work.create", "docs.create"} {
		t.Run(operation, func(t *testing.T) {
			h := newProjectionMaintenanceTestServer(t)
			primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{MaxArtifacts: 100, MaxDocuments: 100, MaxRevisions: 100})(h.store.(*primitives.Store))
			postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-author","display_name":"Author","created_at":"2026-10-10T00:00:00Z"}}`, 201).Body.Close()
			route, body := "/work", `{"actor_id":"actor-author","request_key":"original","title":"Original outcome","source":{"authority":"nexus"},"definition_of_done":["Evidence reviewed"]}`
			if operation == "docs.create" {
				route = "/docs"
				body = `{"actor_id":"actor-author","request_key":"original","document":{"title":"Original document"},"content":"Original body","content_type":"text"}`
			}
			if _, err := h.workspace.DB().Exec(fmt.Sprintf(`CREATE TRIGGER reject_author_replay BEFORE INSERT ON idempotency_replays WHEN NEW.scope='%s' BEGIN SELECT RAISE(ABORT,'injected receipt failure'); END`, operation)); err != nil {
				t.Fatal(err)
			}
			postJSONExpectStatus(t, h.baseURL+route, body, 500).Body.Close()
			for _, table := range []string{"boards", "cards", "documents", "document_revisions", "artifacts", "threads"} {
				var count int
				if err := h.workspace.DB().QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 0 {
					t.Fatalf("partial %s %d %v", table, count, err)
				}
			}
			if _, err := h.workspace.DB().Exec(`DROP TRIGGER reject_author_replay`); err != nil {
				t.Fatal(err)
			}
			start := make(chan struct{})
			results := make(chan string, 8)
			for i := 0; i < 8; i++ {
				go func() {
					<-start
					resp, err := http.Post(h.baseURL+route, "application/json", strings.NewReader(body))
					if err != nil {
						results <- err.Error()
						return
					}
					defer resp.Body.Close()
					raw, _ := io.ReadAll(resp.Body)
					if resp.StatusCode != 201 {
						results <- fmt.Sprintf("%d %s", resp.StatusCode, raw)
						return
					}
					results <- string(raw)
				}()
			}
			close(start)
			first := <-results
			var response map[string]any
			if err := json.Unmarshal([]byte(first), &response); err != nil {
				t.Fatalf("response %s", first)
			}
			for i := 1; i < 8; i++ {
				if next := <-results; next != first {
					t.Fatalf("different receipt %s vs %s", first, next)
				}
			}
			postJSONExpectStatus(t, h.baseURL+route, strings.Replace(body, "Original", "Changed", 1), 409).Body.Close()
			table := "cards"
			if operation == "docs.create" {
				table = "documents"
			}
			var count int
			if err := h.workspace.DB().QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil || count != 1 {
				t.Fatalf("duplicate %d %v", count, err)
			}
		})
	}
}

func TestGeneralMutationReplayRetainsFreshResourceAuthorization(t *testing.T) {
	h := newProjectionMaintenanceTestServer(t)
	store := h.store.(*primitives.Store)
	ctx := context.Background()
	board, err := store.CreateBoard(ctx, "owner", map[string]any{"title": "Private authoring"})
	if err != nil {
		t.Fatal(err)
	}
	thread := board["thread_id"].(string)
	if _, err = store.PatchThread(ctx, "owner", thread, map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	owner := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "owner"})
	run := func(ctx context.Context) (map[string]any, error) {
		work, err := store.CreateWork(ctx, "owner", board["id"].(string), map[string]any{"title": "Private authored outcome", "source": map[string]any{"authority": "nexus"}})
		return map[string]any{"work": work}, err
	}
	if _, err = store.ExecuteWithReplay(owner, "work.create", "owner", "private-key", "body-hash", 201, run); err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, "owner", thread, map[string]any{"pm_actor_id": "replacement"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = store.GetIdempotencyReplay(owner, "work.create", "owner", "private-key"); !errors.Is(err, primitives.ErrNotFound) {
		t.Fatalf("receipt bypassed revoked ownership %v", err)
	}
	if receipt, err := store.ExecuteWithReplay(owner, "work.create", "owner", "private-key", "body-hash", 201, run); err == nil || receipt.Response != nil {
		t.Fatalf("mutation bypassed revoked ownership %+v %v", receipt, err)
	}
}

func TestDocumentMetadataAndRevisionReplayRetainOriginalConcurrencyTokens(t *testing.T) {
	h := newProjectionMaintenanceTestServer(t)
	postJSONExpectStatus(t, h.baseURL+"/actors", `{"actor":{"id":"actor-author","display_name":"Author","created_at":"2026-10-10T00:00:00Z"}}`, 201).Body.Close()
	created := postJSONExpectStatus(t, h.baseURL+"/docs", `{"actor_id":"actor-author","document":{"title":"Document"},"content":"Original body","content_type":"text"}`, 201)
	var doc map[string]any
	json.NewDecoder(created.Body).Decode(&doc)
	created.Body.Close()
	d := doc["document"].(map[string]any)
	rev := doc["revision"].(map[string]any)
	for _, tc := range []struct {
		method, route, body string
		status              int
	}{
		{"PATCH", "/docs/" + d["id"].(string), fmt.Sprintf(`{"actor_id":"actor-author","request_key":"metadata","patch":{"summary":"Authored summary"},"if_updated_at":%q}`, d["updated_at"]), 200},
		{"POST", "/docs/" + d["id"].(string) + "/revisions", fmt.Sprintf(`{"actor_id":"actor-author","request_key":"revision","content":"Authored body","content_type":"text","if_base_revision":%q}`, rev["revision_id"]), 201},
	} {
		call := func(body string) (int, string) {
			req, _ := http.NewRequest(tc.method, h.baseURL+tc.route, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return 0, err.Error()
			}
			defer resp.Body.Close()
			raw, _ := io.ReadAll(resp.Body)
			return resp.StatusCode, string(raw)
		}
		status, first := call(tc.body)
		if status != tc.status {
			t.Fatalf("first %d %s", status, first)
		}
		status, again := call(tc.body)
		if status != tc.status || again != first {
			t.Fatalf("replay %d %s vs %s", status, again, first)
		}
		if status, raw := call(strings.Replace(tc.body, "Authored", "Changed", 1)); status != 409 {
			t.Fatalf("changed %d %s", status, raw)
		}
	}
}
