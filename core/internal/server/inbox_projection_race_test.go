package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

type snapshotBlockedInboxStore struct {
	*primitives.Store
	snapshotted chan struct{}
	release     chan struct{}
	once        sync.Once
}

func (s *snapshotBlockedInboxStore) ListEventsByThread(ctx context.Context, thread string) ([]map[string]any, error) {
	events, err := s.Store.ListEventsByThread(ctx, thread)
	if err != nil {
		return nil, err
	}
	block := false
	s.once.Do(func() { block = true; close(s.snapshotted) })
	if block {
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return events, nil
}

func TestInboxProjectionSnapshotBeforeAnswer(t *testing.T) {
	requireIntegrationTest(t)
	for _, scoped := range []bool{false, true} {
		for _, newerFirst := range []bool{false, true} {
			t.Run(fmt.Sprintf("scoped=%t/newerFirst=%t", scoped, newerFirst), func(t *testing.T) {
				env := newAuthIntegrationEnv(t, authIntegrationOptions{scopedInboxReader: scoped})
				ctx := context.Background()
				owner := seedHumanPrincipalForLockoutTest(t, ctx, env.workspace.DB(), "race-owner", "race-owner-actor", "race-owner", "race-owner-token")
				store := env.primitiveStore.(*primitives.Store)
				ask, _ := seedInboxResponsePrivacyAsk(t, store, owner.ActorID, "card")
				if scoped {
					for done := false; !done; {
						var err error
						done, err = env.workspace.MaintainScopeInboxBatch(ctx)
						if err != nil {
							t.Fatal(err)
						}
					}
				}
				oldGeneration, err := store.MarkTopicProjectionRefreshStarted(ctx, ask.ThreadID, time.Now().UTC())
				if err != nil {
					t.Fatal(err)
				}
				blocked := &snapshotBlockedInboxStore{Store: store, snapshotted: make(chan struct{}), release: make(chan struct{})}
				finished := make(chan error, 1)
				refreshCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				defer cancel()
				// Always join the refresh, including on a failed HTTP assertion.
				released := false
				defer func() {
					if !released {
						close(blocked.release)
						<-finished
					}
				}()
				go func() {
					finished <- refreshDerivedTopicProjection(refreshCtx, handlerOptions{primitiveStore: blocked}, ask.ThreadID, time.Now().UTC(), owner.ActorID, oldGeneration)
				}()
				select {
				case <-blocked.snapshotted:
				case <-refreshCtx.Done():
					t.Fatal("refresh did not snapshot")
				}
				status, body := hostHTTP(t, http.MethodPost, env.server.URL+"/inbox/"+url.PathEscape(ask.ID)+"/respond", owner.AccessToken, map[string]any{"response_text": "Approved", "outcome": "answered", "notify_mode": "none"})
				if status != http.StatusCreated {
					t.Fatalf("answer: %d %#v", status, body)
				}
				if newerFirst {
					generation, err := store.MarkTopicProjectionRefreshStarted(ctx, ask.ThreadID, time.Now().UTC())
					if err != nil {
						t.Fatal(err)
					}
					if err := refreshDerivedTopicProjection(ctx, handlerOptions{primitiveStore: store}, ask.ThreadID, time.Now().UTC(), owner.ActorID, generation); err != nil {
						t.Fatal(err)
					}
					if err := store.MarkTopicProjectionRefreshSucceeded(ctx, ask.ThreadID, generation, time.Now().UTC()); err != nil {
						t.Fatal(err)
					}
				}
				close(blocked.release)
				released = true
				if err := <-finished; err != nil {
					t.Fatal(err)
				}
				if err := store.MarkTopicProjectionRefreshSucceeded(ctx, ask.ThreadID, oldGeneration, time.Now().UTC()); err != nil {
					t.Fatal(err)
				}
				for _, table := range []string{"derived_inbox_items", "scope_inbox_live"} {
					var count int
					if err := env.workspace.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE id=?", ask.ID).Scan(&count); err != nil {
						t.Fatal(err)
					}
					if count != 0 {
						t.Fatalf("answered item resurrected in %s", table)
					}
				}
				inbox := scopeInboxHTTP(t, env, owner.AccessToken, "/inbox?limit=50")
				if len(inbox["items"].([]any)) != 0 {
					t.Fatalf("answered item reopened: %#v", inbox)
				}
				if err := store.ReplaceDerivedInboxItems(ctx, ask.ThreadID, []primitives.DerivedInboxItem{ask}, oldGeneration); err != primitives.ErrProjectionGenerationChanged {
					t.Fatalf("stale generation was not rejected: %v", err)
				}
				// The predicate must use the same normalized source ID as storage.
				ask.SourceEventID = " " + ask.SourceEventID + " "
				// Also exercise the resolution guard without a generation constraint.
				if err := store.ReplaceDerivedInboxItems(ctx, ask.ThreadID, []primitives.DerivedInboxItem{ask}); err != nil {
					t.Fatal(err)
				}
				if _, err := store.GetDerivedInboxItem(ctx, ask.ID); err != primitives.ErrNotFound {
					t.Fatalf("resolution guard reinserted answered source: %v", err)
				}
				statuses, err := store.GetTopicProjectionRefreshStatuses(ctx, []string{ask.ThreadID})
				if err != nil {
					t.Fatal(err)
				}
				if statuses[ask.ThreadID].IsDirty() == newerFirst {
					t.Fatalf("wrong dirty state after superseded refresh: %#v", statuses)
				}
			})
		}
	}
}
