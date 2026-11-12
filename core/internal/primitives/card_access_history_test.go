package primitives

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"agent-nexus-core/internal/storage"
)

type cardHistoryFixture struct {
	ws           *storage.Workspace
	s            *Store
	ctx          context.Context
	boardID      string
	secretRef    string
	secretThread string
}

func newCardHistoryFixture(t *testing.T) *cardHistoryFixture {
	t.Helper()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	store := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := store.CreateBoard(ctx, "actor-1", map[string]any{"title": "History"})
	if err != nil {
		t.Fatal(err)
	}
	private, err := store.CreateWork(ctx, "owner", "", map[string]any{"title": "private card history target"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, "owner", anyStringValue(private["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	return &cardHistoryFixture{ws: ws, s: store, ctx: ctx, boardID: anyStringValue(board["id"]), secretRef: anyStringValue(private["ref"]), secretThread: anyStringValue(private["thread_id"])}
}

type cardRevisionRow struct{ revision, artifact string }

func (f *cardHistoryFixture) revisions(t *testing.T, cardID string) []cardRevisionRow {
	t.Helper()
	rows, err := f.ws.DB().Query(`SELECT revision_id,artifact_id FROM card_revisions WHERE card_id=? ORDER BY revision_number`, cardID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []cardRevisionRow
	for rows.Next() {
		var r cardRevisionRow
		if err = rows.Scan(&r.revision, &r.artifact); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func (f *cardHistoryFixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := f.ws.DB().QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *cardHistoryFixture) cardEdges(t *testing.T, cardID string) int {
	return f.count(t, `SELECT count(*) FROM resource_access_edges WHERE source_kind='card_history' AND source_id=?`, cardID)
}

// perRevisionEdges counts edges still owned by a card's revisions or revision
// artifacts rather than by the card's deduplicated set.
func (f *cardHistoryFixture) perRevisionEdges(t *testing.T, cardID string) int {
	return f.count(t, `SELECT count(*) FROM resource_access_edges e JOIN card_revisions r ON (e.source_kind='artifact' AND e.source_id=r.artifact_id) OR (e.source_kind='card_revision' AND e.source_id=r.revision_id) WHERE r.card_id=?`, cardID)
}

func (f *cardHistoryFixture) visible(t *testing.T, actor, kind, id string) bool {
	t.Helper()
	return f.s.CanAccessResource(WithAccessScope(f.ctx, AccessScope{ActorID: actor}), kind, id)
}

func (f *cardHistoryFixture) addCard(t *testing.T, title string, refs ...string) string {
	t.Helper()
	added, err := f.s.AddBoardCard(f.ctx, "actor-1", f.boardID, AddBoardCardInput{Title: title, Refs: refs})
	if err != nil {
		t.Fatal(err)
	}
	return anyStringValue(added.Card["id"])
}

func (f *cardHistoryFixture) revise(t *testing.T, cardID string, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		summary := fmt.Sprintf("revision %d", i)
		if _, _, err := f.s.CreateCardRevision(f.ctx, "actor-1", cardID, CreateCardRevisionInput{Summary: &summary}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCardRevisionAccessEdgesStayPerCard(t *testing.T) {
	t.Parallel()
	f := newCardHistoryFixture(t)
	tainted := f.addCard(t, "tainted card", f.secretRef)
	f.revise(t, tainted, 30)
	public := f.addCard(t, "public card")
	f.revise(t, public, 30)

	for _, cardID := range []string{tainted, public} {
		if n := f.perRevisionEdges(t, cardID); n != 0 {
			t.Fatalf("%s kept %d per-revision edges", cardID, n)
		}
		if f.cardEdges(t, cardID) == 0 {
			t.Fatalf("%s has no card history edges", cardID)
		}
	}
	// The admission ledger must carry the same card_history -> card edge and
	// reachability as the canonical graph.
	if n := f.count(t, `SELECT count(*) FROM resource_access_admission_links WHERE origin='card_history' AND parent_kind='card_history' AND parent_id=? AND child_kind='card' AND child_id=?`, tainted, tainted); n == 0 {
		t.Fatal("card history admission link missing")
	}
	if n := f.count(t, `SELECT count(*) FROM resource_access_admission_reach WHERE kind='card' AND id=? AND root_kind='thread' AND root_id=?`, tainted, f.secretThread); n == 0 {
		t.Fatal("card history denial did not reach the card")
	}
	// Each revision adds only its few new atoms, not another copy of the stable
	// title, thread and card identities.
	if got := f.cardEdges(t, public); got > 250 {
		t.Fatalf("public card history grew with revisions: %d edges", got)
	}

	for _, actor := range []string{"stranger", "unauthorized-agent", "owner"} {
		for _, r := range f.revisions(t, tainted) {
			for _, target := range []struct{ kind, id string }{{"card", tainted}, {"card_revision", r.revision}, {"artifact", r.artifact}} {
				if got := f.visible(t, actor, target.kind, target.id); got != (actor == "owner") {
					t.Errorf("%s %s %s visible=%v", actor, target.kind, target.id, got)
				}
			}
		}
	}
	for _, r := range f.revisions(t, public) {
		for _, actor := range []string{"stranger", "owner"} {
			for _, target := range []struct{ kind, id string }{{"card", public}, {"card_revision", r.revision}, {"artifact", r.artifact}} {
				if !f.visible(t, actor, target.kind, target.id) {
					t.Errorf("%s cannot see public %s %s", actor, target.kind, target.id)
				}
			}
		}
	}
}

func TestCardRevisionAccessKeepsLateProseTargets(t *testing.T) {
	t.Parallel()
	f := newCardHistoryFixture(t)
	added, err := f.s.AddBoardCard(f.ctx, "actor-1", f.boardID, AddBoardCardInput{Title: "notes", Body: "See card:late-secret for context."})
	if err != nil {
		t.Fatal(err)
	}
	cardID := anyStringValue(added.Card["id"])
	f.revise(t, cardID, 5)
	if !f.visible(t, "stranger", "card", cardID) {
		t.Fatal("public notes hidden before target exists")
	}
	late, err := f.s.CreateWork(f.ctx, "owner", "", map[string]any{"title": "late", "handle": "late-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.PatchThread(f.ctx, "owner", anyStringValue(late["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	if f.visible(t, "stranger", "card", cardID) {
		t.Fatal("late private prose target escaped")
	}
	if !f.visible(t, "owner", "card", cardID) {
		t.Fatal("owner lost notes")
	}
}

func TestCardRevisionAccessRebuildAndPurge(t *testing.T) {
	t.Parallel()
	f := newCardHistoryFixture(t)
	cardID := f.addCard(t, "rebuilt card", f.secretRef)
	f.revise(t, cardID, 3)
	revisions := f.revisions(t, cardID)
	if f.visible(t, "stranger", "card", cardID) {
		t.Fatal("tainted card visible")
	}
	before := f.cardEdges(t, cardID)
	// A direct write that removes the private target from every artifact
	// rebuilds the card's set from canonical rows.
	for _, r := range revisions {
		if _, err := f.ws.DB().Exec(`UPDATE artifacts SET content_refs_json=json_array(?),metadata_json='{}',refs_json='[]' WHERE id=?`, "card:"+cardID, r.artifact); err != nil {
			t.Fatal(err)
		}
	}
	// Remove the private target from the canonical revision and card rows too;
	// otherwise their own reference edges still constrain the card directly.
	for _, q := range []string{
		`UPDATE card_revisions SET refs_json='[]' WHERE card_id=?`,
		`UPDATE cards SET refs_json='[]' WHERE id=?`,
		`DELETE FROM ref_edges WHERE source_type='card' AND source_id=?`,
		`DELETE FROM ref_edges WHERE source_type IN ('card_revision','artifact') AND source_id IN (SELECT revision_id FROM card_revisions WHERE card_id=? UNION SELECT artifact_id FROM card_revisions WHERE card_id=?)`,
	} {
		args := []any{cardID}
		if strings.Count(q, "?") == 2 {
			args = append(args, cardID)
		}
		if _, err := f.ws.DB().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	if !f.visible(t, "stranger", "card", cardID) {
		rows, _ := f.ws.DB().Query(`SELECT source_kind||':'||source_id||' -> '||target_ref FROM resource_access_edges WHERE target_ref=? OR source_id=? OR source_id IN (SELECT artifact_id FROM card_revisions WHERE card_id=?) ORDER BY 1`, f.secretRef, cardID, cardID)
		var left []string
		if rows != nil {
			for rows.Next() {
				var row string
				_ = rows.Scan(&row)
				left = append(left, row)
			}
			rows.Close()
		}
		t.Fatalf("removed target still constrains (edges %d -> %d): %v", before, f.cardEdges(t, cardID), left)
	}
	if n := f.perRevisionEdges(t, cardID); n != 0 {
		t.Fatalf("rebuild left %d per-revision edges", n)
	}
	if _, err := f.s.ArchiveBoardCard(f.ctx, "actor-1", f.boardID, cardID, RemoveBoardCardInput{}); err != nil {
		t.Fatal(err)
	}
	if err := f.s.PurgeArchivedBoardCard(f.ctx, f.boardID, cardID); err != nil {
		t.Fatal(err)
	}
	if n := f.cardEdges(t, cardID); n != 0 {
		t.Fatalf("purge kept %d card history edges", n)
	}
	for _, r := range revisions {
		if n := f.count(t, `SELECT count(*) FROM resource_access_edges WHERE source_kind='artifact' AND source_id=?`, r.artifact); n != 0 {
			t.Fatalf("purge restored %d artifact edges", n)
		}
	}
}
