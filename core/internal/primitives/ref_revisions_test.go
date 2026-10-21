package primitives

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestResolveRefsRevisionsOpenTheirParent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)

	doc, first, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "Runbook"}, "v1", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	docID, docHandle := anyStringValue(doc["id"]), anyStringValue(doc["handle"])
	_, second, err := s.UpdateDocument(ctx, "owner", docID, nil, anyStringValue(first["revision_id"]), "v2", "text", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	board, err := s.CreateBoard(ctx, "owner", map[string]any{"title": "Board"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := s.CreateBoardCard(ctx, "owner", anyStringValue(board["id"]), AddBoardCardInput{Title: "Launch", Body: "body", ColumnKey: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	cardID, cardHandle := anyStringValue(card.Card["id"]), anyStringValue(card.Card["handle"])
	var cardRevisionID string
	if err = ws.DB().QueryRowContext(ctx, `SELECT revision_id FROM card_revisions WHERE card_id=? LIMIT 1`, cardID).Scan(&cardRevisionID); err != nil {
		t.Fatal(err)
	}

	docURL := "/docs/" + docHandle + "?revision=" + docHandle + "-r2"
	cases := []struct {
		ref, kind, parent, url string
	}{
		{"document_revision:" + anyStringValue(second["revision_id"]), "document_revision", "document:" + docHandle, docURL},
		{"document_revision:" + docHandle + "-r2", "document_revision", "document:" + docHandle, docURL},
		{"document_revision:" + docID + "-r1", "document_revision", "document:" + docHandle, "/docs/" + docHandle + "?revision=" + docHandle + "-r1"},
		{"card_revision:" + cardRevisionID, "card_revision", "card:" + cardHandle, "/tasks/" + cardHandle},
		{"card_revision:" + cardHandle + "-r1", "card_revision", "card:" + cardHandle, "/tasks/" + cardHandle},
	}
	missing := []string{"document_revision:" + docHandle + "-r9", "document_revision:nope", "card_revision:" + cardHandle + "-r0", "card_revision:"}
	refs := []string{}
	for _, tc := range cases {
		refs = append(refs, tc.ref)
	}
	refs = append(refs, missing...)

	items, err := s.ResolveRefs(ctx, refs, nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for i, tc := range cases {
		got := items[i]
		if got.Ref != tc.ref || !got.Resolvable || got.Kind != tc.kind || got.ParentRef != tc.parent || got.URL != tc.url || got.Title == "" {
			t.Errorf("%s: got %+v", tc.ref, got)
		}
	}
	for i := range missing {
		if got := items[len(cases)+i]; got.Resolvable || got.Ref != missing[i] {
			t.Errorf("%s: expected unresolvable, got %+v", missing[i], got)
		}
	}

	// A revision is visible only when its parent is.
	hidden, err := s.ResolveRefs(WithAccessScope(ctx, AccessScope{ActorID: "stranger"}), refs[:len(cases)], func(string, string) bool { return false }, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range hidden {
		if got.Resolvable {
			t.Errorf("hidden parent leaked through revision: %+v", got)
		}
	}
}

// Revision resolution must never scan a revision or parent table.
func TestResolveRevisionQueriesAreIndexed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for prefix, spec := range revisionRefKinds {
		for name, query := range map[string]string{"id": revisionByIDQuery(spec), "handle": revisionByHandleQuery(spec)} {
			arg := `["x"]`
			if name == "handle" {
				arg = `[{"k":"a-r1","p":"a","h":"a","n":1}]`
			}
			rows, err := ws.DB().QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, arg)
			if err != nil {
				t.Fatal(err)
			}
			for rows.Next() {
				var id, parent, notused int
				var detail string
				if err := rows.Scan(&id, &parent, &notused, &detail); err != nil {
					t.Fatal(err)
				}
				// Only the bounded json_each input may be scanned; every
				// table read must be an index search.
				if strings.HasPrefix(detail, "SCAN ") && !strings.HasPrefix(detail, "SCAN j ") {
					t.Errorf("%s %s: unindexed plan %q", prefix, name, detail)
				}
				if strings.Contains(detail, "TEMP B-TREE") || strings.Contains(detail, "AUTOMATIC") {
					t.Errorf("%s %s: temporary structure %q", prefix, name, detail)
				}
			}
			rows.Close()
		}
	}
}
