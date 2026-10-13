package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/commandcenter"
	"agent-nexus-core/internal/primitives"
)

func TestResourcePageSnapshotAuthenticationAndSingleConnection(t *testing.T) {
	requireIntegrationTest(t)
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	db := env.workspace.DB()
	if err := env.workspace.InitializeCompactAccessShadow(ctx); err != nil {
		t.Fatal(err)
	}
	owner := seedHumanPrincipalForLockoutTest(t, ctx, db, "page-owner", "page-owner-actor", "page.owner", "page-owner-token")
	stranger := seedMachinePrincipalForLockoutTest(t, ctx, db, "page-stranger", "page-stranger-actor", "page.stranger", "page-stranger-token")
	s := env.primitiveStore.(*primitives.Store)
	board, err := s.CreateBoard(ctx, owner.ActorID, map[string]any{"title": "PrivatePageSentinel"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.CreateBoardCard(ctx, owner.ActorID, anyString(board["id"]), primitives.AddBoardCardInput{Title: "PrivatePageSentinel", ColumnKey: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, owner.ActorID, anyString(board["thread_id"]), map[string]any{"pm_actor_id": owner.ActorID}, nil); err != nil {
		t.Fatal(err)
	}
	thread := anyString(card.Card["thread_id"])
	seedStreamPrivacyInbox(t, s, thread, streamPrivacyInboxItem(thread, "page-private-inbox", "PrivatePageSentinel"))
	for done := false; !done; {
		var err error
		done, err = env.workspace.MaintainCompactAccessBatch(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	for done := false; !done; {
		done, err = env.workspace.MaintainStableAccessBatch(ctx)
		if err != nil {
			t.Fatal(err)
		}
	}
	proof, err := env.workspace.VerifyCompactAccessCutover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = env.workspace.CutoverCompactAccess(ctx, proof); err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	paths := []string{"/cards/" + anyString(card.Card["id"]), "/boards/" + anyString(board["id"]) + "/cards/" + anyString(card.Card["id"]), "/inbox?limit=50", "/overview", "/overview?summary=1&work_view=summary", "/overview/changes"}
	for _, live := range []bool{false, true} {
		t.Run(fmt.Sprintf("live=%v", live), func(t *testing.T) {
			store := primitives.NewStore(db, blob.NewFilesystemBackend(env.workspace.Layout().ArtifactContentDir), env.workspace.Layout().ArtifactContentDir, primitives.WithScopedInboxReader(live))
			h := NewHandler("page-test", WithPrimitiveStore(store), WithAuthStore(auth.NewStore(db)), WithActorRegistry(actors.NewStore(db)), WithRunStore(commandcenter.NewStore(db, commandcenter.SQLIdentities{DB: db})))
			request := func(token, path string, want int) {
				t.Helper()
				c, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				r := httptest.NewRequest(http.MethodGet, path, nil).WithContext(c)
				r.Header.Set("Authorization", "Bearer "+token)
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != want {
					t.Fatalf("path=%s got=%d want=%d body=%s", path, w.Code, want, w.Body.String())
				}
				if token == owner.AccessToken && want == http.StatusOK && path != "/overview/changes" && !strings.Contains(w.Body.String(), "PrivatePageSentinel") {
					t.Fatal("authorized page lost the private positive control")
				}
				if token == stranger.AccessToken && strings.Contains(w.Body.String(), "PrivatePageSentinel") {
					t.Fatal("private page disclosed")
				}
			}
			for _, path := range paths {
				request(owner.AccessToken, path, 200)
				status := 404
				if strings.HasPrefix(path, "/inbox") || strings.HasPrefix(path, "/overview") {
					status = 200
				}
				request(stranger.AccessToken, path, status)
			}
			if _, err := env.workspace.VerifyCompactAccessShadow(ctx); err != nil {
				t.Fatal(err)
			}
			// Warm closure caches never extend credential lifetime or bypass revocation.
			for _, column := range []string{"expires_at", "revoked_at"} {
				if _, err = db.Exec(`UPDATE auth_access_tokens SET `+column+`='2000-01-01T00:00:00Z' WHERE agent_id=?`, owner.AgentID); err != nil {
					t.Fatal(err)
				}
				for _, path := range paths {
					request(owner.AccessToken, path, 401)
				}
				reset := `NULL`
				if column == "expires_at" {
					reset = `'2099-01-01T00:00:00Z'`
				}
				if _, err = db.Exec(`UPDATE auth_access_tokens SET `+column+`=`+reset+` WHERE agent_id=?`, owner.AgentID); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
