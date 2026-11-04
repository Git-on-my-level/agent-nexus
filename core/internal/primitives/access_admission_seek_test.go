package primitives

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// admissionSeekQueries are the scoped relations the document search reader
// joins row by row, plus the readiness probes it runs in the pinned snapshot.
var admissionSeekQueries = []string{
	`SELECT id FROM documents ORDER BY id`,
	`SELECT document_id FROM document_search_heads ORDER BY document_id`,
	`SELECT n||':'||kind||':'||resource_id FROM document_search_resources ORDER BY n`,
	`SELECT term||':'||doc_n||':'||res_n FROM document_search_postings ORDER BY term,doc_n,res_n`,
	`SELECT event_id FROM comment_search_heads ORDER BY event_id`,
	`SELECT thread_id FROM comment_search_threads ORDER BY thread_id`,
	`SELECT event_id FROM comment_search_pending_heads ORDER BY event_id`,
	`SELECT id FROM events ORDER BY id`,
}

// TestSearchSeekAdmissionMatchesProofAndLegacy compares the seek-only candidate
// admission used by document search with the request-local proof (memo) path
// and with the legacy denial closure, for every scope and ledger control.
func TestSearchSeekAdmissionMatchesProofAndLegacy(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	admissionSnapshotCorpus(t, ctx, s)
	public, _, err := s.CreateDocument(ctx, "owner", map[string]any{"id": "public-search", "title": "public search"}, "searchable anchor public", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	private, _, err := s.CreateDocument(ctx, "owner", map[string]any{"id": "private-search", "title": "private search"}, "searchable anchor private", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PatchThread(ctx, "owner", anyStringValue(private["thread_id"]), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
		t.Fatal(err)
	}
	for _, doc := range []map[string]any{public, private} {
		if _, err = s.CreateDocumentComment(ctx, "owner", anyStringValue(doc["id"]), "searchable anchor comment", ""); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; ; i++ {
		if i > 1000 {
			t.Fatal("search fixture did not certify")
		}
		comments, err := ws.MaintainCommentSearchBatch(ctx)
		if err != nil {
			t.Fatal(err)
		}
		documents, err := s.MaintainDocumentSearchBatch(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !comments || !documents {
			continue
		}
		if _, _, err = s.SearchDocuments(ctx, DocumentSearchFilter{Query: "searchable"}); errors.Is(err, ErrSearchRepairPending) {
			continue
		} else if err != nil {
			t.Fatal(err)
		}
		break
	}

	read := func(c context.Context, q string) []string {
		t.Helper()
		rows, err := s.db.QueryContext(c, q)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		defer rows.Close()
		out := []string{}
		for rows.Next() {
			var v string
			if err := rows.Scan(&v); err != nil {
				t.Fatal(err)
			}
			out = append(out, v)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	controls := []struct{ name, write string }{
		{"compact-reader", `UPDATE resource_access_compact_control SET reader=1,force_legacy=0`},
		{"legacy-forced", `UPDATE resource_access_compact_control SET reader=1,force_legacy=1`},
		{"reader-off", `UPDATE resource_access_compact_control SET reader=0,force_legacy=0`},
	}
	hidden, memo := 0, 0
	for _, control := range controls {
		if _, err := ws.DB().ExecContext(ctx, control.write); err != nil {
			t.Fatal(err)
		}
		for _, scope := range admissionSnapshotScopes {
			legacy := WithAccessScope(ctx, scope)
			want := map[string][]string{}
			for _, q := range admissionSeekQueries {
				want[q] = read(legacy, q)
			}
			if !slices.Contains(want[admissionSeekQueries[0]], "private-search") {
				hidden++
			}
			for _, seek := range []bool{true, false} {
				request := WithRequestAccessScope(ctx, scope)
				if seek {
					request = withSeekAdmission(request)
				}
				pin, closeRead, err := s.BeginResourceRead(request)
				if err != nil {
					t.Fatal(err)
				}
				snapshot := pin.Value(pinnedDenialKey{}).(pinnedDenial).snapshot
				if !snapshot.candidates || snapshot.candidateSeek != seek {
					closeRead()
					t.Fatalf("%s %+v: pin candidate=%v seek=%v, want seek=%v", control.name, scope, snapshot.candidates, snapshot.candidateSeek, seek)
				}
				for _, q := range admissionSeekQueries {
					scoped := scopeReadWithSnapshot(pin, q, snapshot)
					usesMemo := strings.Contains(scoped, "anx_candidate_lookup(") || strings.Contains(scoped, "anx_candidate_remember(") || strings.Contains(scoped, "anx_candidate_text_")
					if seek && usesMemo {
						closeRead()
						t.Fatalf("%s %+v: seek admission kept a WHERE memo function: %s", control.name, scope, q)
					}
					if !seek && usesMemo {
						memo++
					}
					if got := read(pin, q); !slices.Equal(got, want[q]) {
						closeRead()
						t.Fatalf("%s %+v seek=%v %s: got=%v legacy=%v", control.name, scope, seek, q, got, want[q])
					}
				}
				closeRead()
			}
			// The production reader must return exactly the legacy-visible parents.
			got, _, err := s.SearchDocuments(WithRequestAccessScope(ctx, scope), DocumentSearchFilter{Query: "searchable anchor"})
			if err != nil {
				t.Fatal(err)
			}
			ids := []string{}
			for _, doc := range got {
				ids = append(ids, anyStringValue(doc["id"]))
			}
			slices.Sort(ids)
			visible := read(legacy, `SELECT id FROM documents WHERE id IN ('public-search','private-search') ORDER BY id`)
			if !slices.Equal(ids, visible) {
				t.Fatalf("%s %+v: search=%v legacy-visible=%v", control.name, scope, ids, visible)
			}
		}
	}
	if hidden == 0 || memo == 0 {
		t.Fatalf("corpus must exercise hidden documents and the memo path: hidden=%d memo=%d", hidden, memo)
	}
}
