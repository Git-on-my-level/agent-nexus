package primitives

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"agent-nexus-core/internal/resourceaccess"
)

func TestResourceAccessSnapshotParameterBindings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
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
	for _, prefix := range []string{"document:", "doc:"} {
		for _, identity := range []string{doc["id"].(string), doc["handle"].(string)} {
			ref := prefix + identity
			if err = s.CheckResourceValues(scope, []string{ref}); !errors.Is(err, ErrNotFound) {
				t.Fatalf("stale selector snapshot exposed %s: %v", ref, err)
			}
			if err = s.CheckResourceValues(WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"}), []string{ref}); !errors.Is(err, ErrNotFound) {
				t.Fatalf("fresh selector snapshot exposed %s: %v", ref, err)
			}
			var denied bool
			if err = s.db.QueryRowContext(scope, `SELECT EXISTS(SELECT 1 FROM _anx_denied_refs WHERE ref=?)`, ref).Scan(&denied); err != nil || !denied {
				t.Fatalf("explicit denied-ref selector lost %s: denied=%v err=%v", ref, denied, err)
			}
		}
	}
	var id string
	if err = ws.DB().QueryRowContext(ctx, q, args...).Scan(&id); err != sql.ErrNoRows {
		t.Fatalf("stale bound snapshot exposed document: %v", err)
	}
}
