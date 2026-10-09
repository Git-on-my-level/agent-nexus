package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/readmodel"
	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/scopes"
)

func scopeInboxHTTP(t *testing.T, env authIntegrationEnv, token, path string) map[string]any {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, env.server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || resp.StatusCode != http.StatusOK {
		t.Fatal(resp.StatusCode, err, body)
	}
	return body
}

func TestScopeInboxHTTPAccessRequestAndRevokedTargetEnrichment(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, db, "enrichment-owner", "enrichment-owner-actor", "enrichment.owner", "enrichment-owner-token")
	agent := seedMachinePrincipalForLockoutTest(t, ctx, db, "enrichment-agent", "enrichment-agent-actor", "enrichment.agent", "enrichment-agent-token")
	store := env.primitiveStore.(*primitives.Store)
	request, err := store.CreateAccessRequest(ctx, auth.Principal{AgentID: agent.AgentID, ActorID: agent.ActorID, Username: "enrichment.agent", PrincipalKind: "agent"}, "auth-admin", "Need access for review")
	if err != nil {
		t.Fatal(err)
	}
	eventID := strings.TrimPrefix(request.RequestEventRef, "event:")
	event, err := store.GetEvent(ctx, eventID)
	if err != nil {
		t.Fatal(err)
	}
	thread := anyString(event["thread_id"])
	item := streamPrivacyInboxItem(thread, request.InboxItemID, "Grant auth-admin")
	item.Category = "review"
	item.SourceEventID = eventID
	item.Data["requester_agent_id"] = agent.AgentID
	item.Data["requester_actor_id"] = agent.ActorID
	if err := store.ReplaceDerivedInboxItems(ctx, thread, []primitives.DerivedInboxItem{item}); err != nil {
		t.Fatal(err)
	}
	item, err = store.GetDerivedInboxItem(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	identity := scopes.ResourceIdentity{ScopeID: "fixture", Kind: "inbox", ResourceID: "private-fixture-key", CanonicalID: item.ID, RID: 1, CanonicalVersion: 1}
	raw, err := primitives.EncodeScopeInbox(identity, item)
	if err != nil {
		t.Fatal(err)
	}
	for _, revoked := range []bool{false, true} {
		t.Run(fmt.Sprintf("revoked=%v", revoked), func(t *testing.T) {
			if revoked {
				if _, err := db.Exec(`UPDATE agents SET revoked_at='2026-10-07T00:00:00Z' WHERE id=?`, agent.AgentID); err != nil {
					t.Fatal(err)
				}
			}
			captured, err := primitives.DecodeScopeInbox(identity, raw)
			if err != nil {
				t.Fatal(err)
			}
			payload := payloadFromDerivedInboxItem(captured)
			targets, err := env.authStore.NotificationTargets(ctx, nil, []string{agent.AgentID})
			if err != nil {
				t.Fatal(err)
			}
			target, found := targets["agent:"+agent.AgentID]
			if found == revoked {
				t.Fatal("revocation retained a live notification target")
			}
			applyNotificationTargetStatus(payload, humanAttentionResponseTarget{ActorID: target.ActorID, AgentID: target.AgentID, Handle: target.Username}, found, nil)
			requests, err := store.AccessRequestsForEvents(ctx, []string{eventID})
			if err != nil {
				t.Fatal(err)
			}
			applyAccessRequestInboxMetadata(payload, requests[request.RequestEventRef])
			if err := store.EnrichInboxAskStaleness(ctx, []map[string]any{payload}, time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
			body := scopeInboxHTTP(t, env, owner.AccessToken, "/inbox?limit=100")
			items := body["items"].([]any)
			if len(items) != 1 || !reflect.DeepEqual(items[0], payload) || payload["access_request_id"] != request.ID || payload["requested_grant"] != "auth-admin" {
				t.Fatal("mounted HTTP lost current enrichment parity", items, payload)
			}
		})
	}
}

// An admitted selected-stream snapshot is not a discovery-completeness proof.
// This runs the actual HTTP oracle and actual repository refusal boundaries;
// no production dispatcher exists yet, so it does not claim fallback wiring.
func TestScopeInboxHTTPDirectoryCompletenessAndRefusalMatrix(t *testing.T) {
	t.Parallel()
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	owner := seedHumanPrincipalForLockoutTest(t, ctx, db, "directory-owner", "directory-actor", "directory.owner", "directory-token")
	store := env.primitiveStore.(*primitives.Store)
	repo := scopedrepo.New(db)
	if err := repo.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.InitializeFeedSchema(ctx); err != nil {
		t.Fatal(err)
	}
	if err := repo.InitializeInboxDispatcherSchema(ctx); err != nil {
		t.Fatal(err)
	}
	codec, err := readmodel.NewCursorCodec(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	dispatcher := scopedrepo.NewInboxDispatcher(repo, codec)
	var ids []scopes.ID
	var streams []scopes.Stream
	var newlyEligibleThread string
	for n := 0; n < 65; n++ {
		board, err := store.CreateBoard(ctx, owner.ActorID, map[string]any{"title": fmt.Sprintf("directory-%02d", n)})
		if err != nil {
			t.Fatal(err)
		}
		thread := anyString(board["thread_id"])
		item := streamPrivacyInboxItem(thread, fmt.Sprintf("directory-item-%02d", n), "Directory ask")
		if err := store.ReplaceDerivedInboxItems(ctx, thread, []primitives.DerivedInboxItem{item}); err != nil {
			t.Fatal(err)
		}
		if n == 64 {
			newlyEligibleThread = thread
			continue // canonical source exists, but the entire shadow directory omits it
		}
		id := scopes.ID(fmt.Sprintf("directory-scope-%02d", n))
		ids = append(ids, id)
		streams = append(streams, scopes.Stream{Scope: id, Family: "inbox", Audience: "all"})
		if _, err := db.Exec(`INSERT INTO scope_domains VALUES(?,'active',1)`, id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO scope_memberships VALUES(?,?,'reader',1)`, owner.AgentID, id); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO scope_feed_bindings VALUES(?,?,1,'inbox','all',1,1)`, owner.AgentID, id); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO scope_feed_generations SELECT id,1,1,1,1,1,(SELECT version FROM resource_access_epoch WHERE singleton=1) FROM scope_domains`); err != nil {
		t.Fatal(err)
	}
	request := scopes.RequestSelection{Principal: owner.AgentID, ScopeIDs: ids}
	if err := repo.ReadFeed(ctx, request, streams, func(r scopedrepo.FeedReader) error {
		s, err := r.Snapshot()
		if err != nil {
			return err
		}
		if len(s.Scopes) != 64 || s.MoreScopes {
			t.Fatal("fixture no longer exposes selected-only directory gap")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Canonical HTTP must retain all 65 sources across pages, counts and order.
	var got []string
	cursor := ""
	for page := 0; page < 4; page++ {
		body := scopeInboxHTTP(t, env, owner.AccessToken, "/inbox?limit=23&cursor="+url.QueryEscape(cursor))
		for _, row := range body["items"].([]any) {
			got = append(got, anyString(row.(map[string]any)["id"]))
		}
		cursor = anyString(body["next_cursor"])
		if cursor == "" {
			if body["has_more"] != false {
				t.Fatal("terminal page claims more")
			}
			break
		}
	}
	whole := scopeInboxHTTP(t, env, owner.AccessToken, "/inbox?limit=100")
	var want []string
	for _, row := range whole["items"].([]any) {
		want = append(want, anyString(row.(map[string]any)["id"]))
	}
	if len(got) != 65 || cursor != "" || !reflect.DeepEqual(got, want) {
		t.Fatalf("canonical directory/page gap: paged=%d whole=%d", len(got), len(want))
	}
	assertWholeOracle := func(t *testing.T) {
		t.Helper()
		body := scopeInboxHTTP(t, env, owner.AccessToken, "/inbox?limit=100")
		if len(body["items"].([]any)) != 65 {
			t.Fatal("refused shadow selection lost canonical sources")
		}
		summary := scopeInboxHTTP(t, env, owner.AccessToken, "/inbox/summary?limit=0")
		if summary["open_ask_count"] != float64(65) || len(summary["asks"].([]any)) != 0 {
			t.Fatal("canonical count included only selected scopes", summary)
		}
	}
	assertDispatchFallback := func(t *testing.T, reason scopedrepo.InboxFallback) {
		t.Helper()
		before := dispatcher.Diagnostics()
		result, err := dispatcher.Read(ctx, owner.AgentID, 23, "")
		if err != nil || result.Fallback != reason || len(result.Page.Items) != 0 || len(result.Counts) != 0 {
			t.Fatal("dispatcher exposed partial output before proof", result, err)
		}
		after := dispatcher.Diagnostics()
		if after.Requests != before.Requests+1 || after.FallbackRequests != before.FallbackRequests+1 {
			t.Fatal("whole-response fallback was not accounted")
		}
		assertWholeOracle(t)
	}
	refuse := func(t *testing.T, req scopes.RequestSelection, ss []scopes.Stream, want error) {
		t.Helper()
		called := false
		err := repo.ReadBatchFeed(ctx, req, ss, func(scopedrepo.BatchFeedReader) error { called = true; return nil })
		if called || !errors.Is(err, want) {
			t.Fatalf("unsafe admission: callback=%v error=%v want=%v", called, err, want)
		}
		assertDispatchFallback(t, scopedrepo.InboxProofUnavailable)
	}
	t.Run("missing proof", func(t *testing.T) { refuse(t, request, streams, scopes.ErrUpdating) })
	t.Run("missing directory", func(t *testing.T) {
		r := request
		r.ScopeIDs = append(append([]scopes.ID{}, ids[:63]...), "omitted-canonical-scope")
		s := append(append([]scopes.Stream{}, streams[:63]...), scopes.Stream{Scope: "omitted-canonical-scope", Family: "inbox", Audience: "all"})
		refuse(t, r, s, scopes.ErrDenied)
	})
	t.Run("more than 64", func(t *testing.T) {
		r := request
		r.ScopeIDs = append(append([]scopes.ID{}, ids...), "65th")
		refuse(t, r, streams, scopes.ErrBudget)
	})
	t.Run("mixed updating selection", func(t *testing.T) {
		if _, err := db.Exec(`UPDATE scope_domains SET state='transitioning' WHERE id=?`, ids[0]); err != nil {
			t.Fatal(err)
		}
		refuse(t, request, streams, scopes.ErrUpdating)
		if _, err := db.Exec(`UPDATE scope_domains SET state='active' WHERE id=?`, ids[0]); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("revoked membership", func(t *testing.T) {
		if _, err := db.Exec(`DELETE FROM scope_memberships WHERE principal=? AND scope_id=?`, owner.AgentID, ids[0]); err != nil {
			t.Fatal(err)
		}
		refuse(t, request, streams, scopes.ErrDenied)
		if _, err := db.Exec(`INSERT INTO scope_memberships VALUES(?,?,'reader',1)`, owner.AgentID, ids[0]); err != nil {
			t.Fatal(err)
		}
		// Revocation deletes the exact audience binding as well. Restoring only
		// membership is deliberately insufficient to read the old generation.
		if _, err := db.Exec(`INSERT INTO scope_feed_bindings VALUES(?,?,1,'inbox','all',1,1)`, owner.AgentID, ids[0]); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("new canonical mutation stales generations", func(t *testing.T) {
		if _, err := store.PatchThread(ctx, owner.ActorID, newlyEligibleThread, map[string]any{"title": "New canonical source revision"}, nil); err != nil {
			t.Fatal(err)
		}
		if err := repo.ReadFeed(ctx, request, streams, func(r scopedrepo.FeedReader) error {
			_, err := r.Candidates(0, nil, 1)
			return err
		}); !errors.Is(err, scopes.ErrUpdating) {
			t.Fatal("source outside selected scopes failed to stale generation", err)
		}
		refuse(t, request, streams, scopes.ErrUpdating)
	})
	t.Run("freshness includes dirty sources without inbox items", func(t *testing.T) {
		thread, err := store.CreateThread(ctx, owner.ActorID, map[string]any{"title": "Unprojected source"})
		if err != nil {
			t.Fatal(err)
		}
		id := anyString(thread.Thread["id"])
		if err := store.RequeueTopicProjectionRefresh(ctx, id, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		assertFreshness := func(status string) {
			t.Helper()
			body := scopeInboxHTTP(t, env, owner.AccessToken, "/inbox?limit=1")
			freshness := body["projection_freshness"].(map[string]any)
			if freshness["thread_count"] != float64(66) || freshness["status"] != status || len(body["items"].([]any)) != 1 {
				t.Fatal("page-local freshness lost canonical source", freshness)
			}
		}
		assertFreshness("pending")
		generation, err := store.MarkTopicProjectionRefreshStarted(ctx, id, time.Now().UTC())
		if err != nil {
			t.Fatal(err)
		}
		if err := store.MarkTopicProjectionRefreshFailed(ctx, id, generation, time.Now().UTC(), "fixture error"); err != nil {
			t.Fatal(err)
		}
		assertFreshness("error")
		assertWholeOracle(t)
	})
	t.Run("actual directory over 64 retains complete legacy response", func(t *testing.T) {
		if _, err := db.Exec(`INSERT INTO scope_domains VALUES('directory-scope-64','active',1)`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO scope_memberships VALUES(?,'directory-scope-64','reader',1)`, owner.AgentID); err != nil {
			t.Fatal(err)
		}
		assertDispatchFallback(t, scopedrepo.InboxDirectoryBudget)
	})
	t.Run("no directory retains complete legacy response", func(t *testing.T) {
		if _, err := db.Exec(`DELETE FROM scope_memberships WHERE principal=?`, owner.AgentID); err != nil {
			t.Fatal(err)
		}
		assertDispatchFallback(t, scopedrepo.InboxNoDirectory)
	})
}
