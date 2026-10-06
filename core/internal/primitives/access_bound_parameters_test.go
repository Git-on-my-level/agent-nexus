package primitives

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/storage"
)

func TestResourceAccessSnapshotParameterBindings(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	doc, _, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "quoted ' ? : ref"}, "public", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	scope := WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"})
	for _, tc := range []struct {
		q    string
		args []any
	}{
		{`SELECT id FROM documents WHERE id=? AND title=?`, []any{doc["id"], doc["title"]}},
		{`WITH ids(v) AS (SELECT ?) SELECT id FROM documents WHERE id=(SELECT v FROM ids) AND '?'='?' /* :not_a_binding */`, []any{doc["id"]}},
		{`SELECT id FROM documents WHERE id=?1 OR id=?1`, []any{doc["id"]}},
		{`SELECT id FROM documents WHERE id=:id`, []any{sql.Named("id", doc["id"])}},
		{`SELECT id FROM documents WHERE id=$1`, []any{doc["id"]}},
	} {
		var id string
		if err = s.db.QueryRowContext(scope, tc.q, tc.args...).Scan(&id); err != nil || id != doc["id"] {
			t.Fatalf("%s: %q %v", tc.q, id, err)
		}
	}
	policy, _ := resourceaccess.PolicyFrom(scope)
	q, args := policy.ReadOnDB(scope, ws.DB(), `SELECT id FROM documents WHERE id=?`, []any{doc["id"]})
	if len(args) != 2 || !strings.Contains(q, "json_each(?)") {
		t.Fatal("snapshot was not bound once before existing arguments")
	}
	// The public row becomes private after the snapshot. Bound snapshots must
	// retain the canonical fallback in the same consuming statement snapshot.
	if _, err = s.PatchThread(ctx, "owner", doc["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.CheckResourceValues(scope, []string{"document:" + doc["id"].(string)}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale selector snapshot exposed document: %v", err)
	}
	var id string
	if err = ws.DB().QueryRowContext(ctx, q, args...).Scan(&id); err != sql.ErrNoRows {
		t.Fatalf("stale bound snapshot exposed document: %v", err)
	}
}
