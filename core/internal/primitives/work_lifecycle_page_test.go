package primitives

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestWorkLifecycleRoutingAndPagination(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	ids := []string{}
	for i := 0; i < 7; i++ {
		c, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": fmt.Sprintf("Lifecycle %d", i)})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, workString(c["id"]))
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := ws.DB().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`UPDATE cards SET archived_at='now' WHERE id IN (?,?,?)`, ids[0], ids[1], ids[2])
	exec(`INSERT INTO topics(id,handle,title,summary,extensions_json,provenance_json,created_at,created_by,updated_at,updated_by) VALUES('lifecycle-project','project','Project','','{}','{}','now','owner','now','owner')`)
	exec(`UPDATE work_metadata SET metadata_json=json_set(metadata_json,'$.project_ref','topic:project') WHERE card_id=?`, ids[3])
	check := func() {
		t.Helper()
		want := map[string]bool{}
		rows, err := ws.DB().Query(`SELECT c.id FROM cards c ` + cardVisibilityJoins + ` WHERE NOT ` + cardLifecycleWhere([]string{"active"}))
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			want[id] = true
		}
		rows.Close()
		got := map[string]bool{}
		rows, err = ws.DB().Query(`SELECT id FROM work_lifecycle_positions`)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			got[id] = true
		}
		rows.Close()
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("routing=%v canonical=%v", got, want)
		}
	}
	check()
	exec(`UPDATE topics SET archived_at='now' WHERE id='lifecycle-project'`)
	check()
	exec(`UPDATE topics SET handle='moved' WHERE id='lifecycle-project'`)
	check()
	exec(`UPDATE work_metadata SET metadata_json=json_set(metadata_json,'$.project_ref','topic:moved') WHERE card_id=?`, ids[3])
	check()
	exec(`UPDATE boards SET archived_at='now'`)
	check()
	exec(`UPDATE boards SET archived_at=NULL`)
	check()
	// A private archived row must not appear in refs or create a continuation.
	exec(`UPDATE threads SET body_json=json_set(body_json,'$.pm_actor_id','owner') WHERE id=(SELECT thread_id FROM cards WHERE id=?)`, ids[2])
	read := func(legacy bool) ([]string, map[string]bool) {
		t.Helper()
		cursor := ""
		work := []string{}
		refs := map[string]bool{}
		for pass := 0; pass < 20; pass++ {
			request := WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"})
			if legacy {
				request = canonicalRequestScope(ctx, AccessScope{ActorID: "reader"})
			}
			request, closeRead, err := s.BeginResourceRead(request)
			if err != nil {
				t.Fatal(err)
			}
			page, err := s.ListWork(request, WorkListFilter{Limit: 2, Cursor: cursor})
			closeRead()
			if err != nil {
				t.Fatal(err)
			}
			if len(page.Work) > 2 || len(page.ArchivedRefs) > 4 {
				t.Fatalf("unbounded page: %+v", page)
			}
			for _, c := range page.Work {
				work = append(work, workString(c["id"]))
			}
			for _, ref := range page.ArchivedRefs {
				refs[ref] = true
			}
			cursor = page.NextCursor
			if cursor == "" {
				return work, refs
			}
		}
		t.Fatal("pagination did not terminate")
		return nil, nil
	}
	work, refs := read(false)
	oldWork, oldRefs := read(true)
	if !reflect.DeepEqual(work, oldWork) || !reflect.DeepEqual(refs, oldRefs) {
		t.Fatalf("candidate and closure pages differ: %v/%v vs %v/%v", work, refs, oldWork, oldRefs)
	}
	for i, id := range ids {
		if i < 4 && i != 2 && !refs["card:"+id] {
			t.Fatalf("missing archived %s", id)
		}
		if i == 2 {
			for ref := range refs {
				if strings.Contains(ref, id) {
					t.Fatalf("private lifecycle ref leaked: %s", ref)
				}
			}
		}
	}
	exec(`UPDATE cards SET archived_at=NULL WHERE id=?`, ids[0])
	check()
	exec(`DELETE FROM cards WHERE id=?`, ids[1])
	check()
}
