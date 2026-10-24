package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestBoardInventoryAlwaysUsesOneAdmissionBatch(t *testing.T) {
	if testing.Short() {
		t.Skip("full HTTP/storage fixture")
	}
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	ctx := context.Background()
	raw := env.workspace.DB()
	reader := seedMachinePrincipalForLockoutTest(t, ctx, raw, "inventory-reader", "inventory-reader", "inventory.reader", "inventory-token")
	seed := primitives.NewTestStore(raw, env.workspace.Layout().ArtifactContentDir)
	if _, err := seed.CreateBoard(ctx, "owner", map[string]any{"title": "Public inventory control"}); err != nil {
		t.Fatal(err)
	}
	private, err := seed.CreateBoard(ctx, "owner", map[string]any{"title": "Private inventory control"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seed.PatchThread(ctx, "owner", private["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/boards", "/boards?summary=1"} {
		t.Run(path, func(t *testing.T) {
			db, capture, err := perfguard.Open("file:" + env.workspace.Layout().DatabasePath)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			store := primitives.NewTestStore(db, env.workspace.Layout().ArtifactContentDir)
			h := NewHandler("inventory-mode", WithPrimitiveStore(store), WithAuthStore(auth.NewStore(db)), WithActorRegistry(actors.NewStore(db)))
			r := httptest.NewRequest(http.MethodGet, path, nil)
			r.Header.Set("Authorization", "Bearer "+reader.AccessToken)
			w := httptest.NewRecorder()
			capture.Start()
			h.ServeHTTP(w, r)
			statements, _, _ := capture.Stop()
			if w.Code != 200 || strings.Contains(w.Body.String(), "Private inventory control") {
				t.Fatalf("status=%d body=%.300s", w.Code, w.Body.String())
			}
			batches := 0
			for _, stmt := range statements {
				if strings.Contains(stmt.SQL, "SELECT DISTINCT r.kind,r.id FROM main.resource_access_admission_owners") {
					batches++
				}
			}
			if batches != 1 {
				t.Fatalf("indexed inventory batches=%d want=1; summary is not a board collection paging option", batches)
			}
		})
	}
}
