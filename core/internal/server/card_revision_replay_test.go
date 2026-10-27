package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/schema"
)

type revisionFailureBlob struct {
	blob.Backend
	fail    bool
	cleaned bool
}
type revisionFailureStage struct {
	blob.StagedWrite
	backend *revisionFailureBlob
}

func (b *revisionFailureBlob) Write(ctx context.Context, hash string, data []byte) (blob.StagedWrite, error) {
	stage, err := b.Backend.Write(ctx, hash, data)
	if err != nil {
		return nil, err
	}
	return &revisionFailureStage{stage, b}, nil
}
func (s *revisionFailureStage) Promote() error {
	if s.backend.fail {
		return errors.New("injected promotion failure")
	}
	return s.StagedWrite.Promote()
}
func (s *revisionFailureStage) Cleanup() error {
	s.backend.cleaned = true
	return s.StagedWrite.Cleanup()
}

func TestCardRevisionAtomicReplayOriginalBodyHeadBlobAndAuthority(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	owner := seedNotificationTestAgent(t, env, "owner.revisionhost")
	ctx := context.Background()
	originalStore := env.primitiveStore.(*primitives.Store)
	work, err := originalStore.CreateWork(ctx, owner.ActorID, "", map[string]any{"title": "Original card", "summary": "Original content"})
	if err != nil {
		t.Fatal(err)
	}
	peer, err := originalStore.CreateWork(ctx, owner.ActorID, "", map[string]any{"title": "Other card"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := originalStore.GetBoardCard(ctx, "", anyString(work["id"]))
	if err != nil {
		t.Fatal(err)
	}
	backend := &revisionFailureBlob{Backend: blob.NewFilesystemBackend(env.workspace.Layout().ArtifactContentDir)}
	store := primitives.NewStore(env.workspace.DB(), backend, env.workspace.Layout().ArtifactContentDir, primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{MaxArtifacts: 1000, MaxBlobBytes: 1 << 30}))
	env.workspace.DB().SetMaxOpenConns(1)
	contract, err := schema.Load("../../../contracts/anx-schema.yaml")
	if err != nil {
		t.Fatal(err)
	}
	scoped := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: owner.ActorID})
	opts := handlerOptions{primitiveStore: store, authStore: env.authStore, actorRegistry: env.registry, contract: contract, workspaceID: "ws_main"}
	write := func(body map[string]any, target string) *httptest.ResponseRecorder {
		callCtx, cancel := context.WithTimeout(scoped, 5*time.Second)
		defer cancel()
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/cards/"+target+"/revisions", strings.NewReader(string(raw))).WithContext(callCtx)
		cacheAuthenticatedPrincipal(req, &auth.Principal{ActorID: owner.ActorID})
		out := httptest.NewRecorder()
		handleCreateCardRevision(out, req, opts, target)
		return out
	}
	body := map[string]any{"actor_id": owner.ActorID, "request_key": "revision-original", "if_base_revision": card["head_revision_ref"], "revision": map[string]any{"title": "Authored title", "summary": "Keep original content", "definition_of_done": []string{"A", "B"}}}
	snapshot := func() string {
		t.Helper()
		var all []string
		for _, table := range []string{"cards", "boards", "card_revisions", "artifacts", "ref_edges", "blob_usage_ledger", "blob_usage_totals", "derived_topic_dirty_queue", "idempotency_replays"} {
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
	// Force first-use ledger rebuild inside the replay writer. Failure must also
	// roll back its initialization, and the next attempt must rebuild successfully.
	for _, table := range []string{"blob_usage_ledger", "blob_usage_totals"} {
		if _, err := env.workspace.DB().Exec("DELETE FROM " + table); err != nil {
			t.Fatal(err)
		}
	}
	for _, trigger := range []string{
		`CREATE TRIGGER reject_revision BEFORE INSERT ON card_revisions BEGIN SELECT RAISE(ABORT,'revision failed'); END`,
		`CREATE TRIGGER reject_revision BEFORE UPDATE OF head_revision_id ON cards BEGIN SELECT RAISE(ABORT,'head failed'); END`,
		`CREATE TRIGGER reject_revision BEFORE UPDATE ON boards BEGIN SELECT RAISE(ABORT,'board failed'); END`,
		`CREATE TRIGGER reject_revision BEFORE INSERT ON blob_usage_ledger BEGIN SELECT RAISE(ABORT,'ledger failed'); END`,
		`CREATE TRIGGER reject_revision BEFORE INSERT ON idempotency_replays WHEN NEW.scope='cards.revisions.create' BEGIN SELECT RAISE(ABORT,'receipt failed'); END`,
	} {
		before := snapshot()
		if _, err := env.workspace.DB().Exec(trigger); err != nil {
			t.Fatal(err)
		}
		out := write(body, anyString(work["ref"]))
		if out.Code != 500 {
			t.Fatal("failed revision confirmed", out.Body.String())
		}
		if after := snapshot(); after != before {
			t.Fatalf("partial revision survived %s: response %d %s\nbefore %s\nafter %s", trigger, out.Code, out.Body.String(), before, after)
		}
		if _, err := env.workspace.DB().Exec("DROP TRIGGER reject_revision"); err != nil {
			t.Fatal(err)
		}
	}
	before := snapshot()
	backend.cleaned = false
	backend.fail = true
	out := write(body, anyString(work["ref"]))
	backend.fail = false
	if out.Code != 500 || !backend.cleaned || snapshot() != before {
		t.Fatal("promotion failure left canonical state or staging", out.Code, out.Body.String())
	}
	responses := make(chan *httptest.ResponseRecorder, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); responses <- write(body, anyString(work["ref"])) }()
	}
	wg.Wait()
	close(responses)
	var first string
	for out := range responses {
		if out.Code != 201 {
			t.Fatal(out.Code, out.Body.String())
		}
		if first == "" {
			first = out.Body.String()
		} else if first != out.Body.String() {
			t.Fatal("concurrent receipts differ")
		}
	}
	var count int
	if err := env.workspace.DB().QueryRow("SELECT count(*) FROM card_revisions WHERE card_id=?", work["id"]).Scan(&count); err != nil || count != 2 {
		t.Fatalf("duplicate revision: %d %v", count, err)
	}
	for _, change := range []map[string]any{{"revision": map[string]any{"title": "Different"}}, {"if_base_revision": "different"}, {"title": "Ignored legacy field still binds the request"}, {"definition_of_done": []string{"legacy"}}} {
		changed := copyMutationBody(body)
		for key, value := range change {
			changed[key] = value
		}
		out := write(changed, anyString(work["ref"]))
		if out.Code != 409 {
			t.Fatal("changed body accepted", change, out.Code, out.Body.String())
		}
	}
	if out := write(body, anyString(peer["ref"])); out.Code != 409 {
		t.Fatal("changed target accepted", out.Code, out.Body.String())
	}
	var receipt map[string]any
	if err := json.Unmarshal([]byte(first), &receipt); err != nil {
		t.Fatal(err)
	}
	newer := copyMutationBody(body)
	newer["request_key"] = "revision-newer"
	newer["if_base_revision"] = receipt["card"].(map[string]any)["head_revision_ref"]
	newer["revision"] = map[string]any{"summary": "Later content"}
	if out := write(newer, anyString(work["ref"])); out.Code != 201 {
		t.Fatal(out.Code, out.Body.String())
	}
	if out := write(body, anyString(work["ref"])); out.Code != 201 || out.Body.String() != first {
		t.Fatal("original receipt advanced", out.Code, out.Body.String())
	}
	stale := copyMutationBody(body)
	stale["request_key"] = "fresh-key-stale-base"
	if out := write(stale, anyString(work["ref"])); out.Code != 409 {
		t.Fatal("fresh key bypassed stale base", out.Code, out.Body.String())
	}
	for _, key := range []string{strings.Repeat("x", 257), "bad\x00key"} {
		invalid := copyMutationBody(body)
		invalid["request_key"] = key
		if out := write(invalid, anyString(work["ref"])); out.Code != 400 {
			t.Fatal("invalid key accepted", out.Code, out.Body.String())
		}
	}
	missing := copyMutationBody(body)
	delete(missing, "if_base_revision")
	if out := write(missing, anyString(work["ref"])); out.Code != 400 {
		t.Fatal("unfenced keyed revision accepted", out.Code, out.Body.String())
	}
	currentCard, err := store.GetBoardCard(ctx, "", anyString(work["id"]))
	if err != nil {
		t.Fatal(err)
	}
	legacy := copyMutationBody(body)
	delete(legacy, "request_key")
	legacy["if_base_revision"] = currentCard["head_revision_ref"]
	legacy["revision"] = map[string]any{"summary": "Legacy unkeyed content"}
	for _, table := range []string{"blob_usage_ledger", "blob_usage_totals"} {
		if _, err := env.workspace.DB().Exec("DELETE FROM " + table); err != nil {
			t.Fatal(err)
		}
	}
	before = snapshot()
	backend.fail = true
	backend.cleaned = false
	legacyFailure := write(legacy, anyString(work["ref"]))
	backend.fail = false
	if legacyFailure.Code != 500 || !backend.cleaned || snapshot() != before {
		t.Fatal("legacy promotion escaped rollback", legacyFailure.Code, legacyFailure.Body.String())
	}
	if out := write(legacy, anyString(work["ref"])); out.Code != 201 {
		t.Fatal("legacy revision failed", out.Code, out.Body.String())
	}
	primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{MaxArtifacts: 1})(store)
	if out := write(body, anyString(work["ref"])); out.Code != 201 || out.Body.String() != first {
		t.Fatal("quota blocked original receipt", out.Code, out.Body.String())
	}
	primitives.WithWorkspaceQuota(primitives.WorkspaceQuota{MaxArtifacts: 1000, MaxBlobBytes: 1 << 30})(store)
	if _, err := store.PatchThread(ctx, owner.ActorID, anyString(work["thread_id"]), map[string]any{"pm_actor_id": "replacement"}, nil); err != nil {
		t.Fatal(err)
	}
	if out := write(body, anyString(work["ref"])); out.Code != 404 {
		t.Fatal("revoked receipt exposed", out.Code, out.Body.String())
	}
}
