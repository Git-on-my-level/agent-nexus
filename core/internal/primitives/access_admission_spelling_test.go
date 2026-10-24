package primitives

import (
	"context"
	"reflect"
	"testing"

	"agent-nexus-core/internal/resourceaccess"
)

func TestCandidateReferenceSpellingParity(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for _, q := range []string{
		`INSERT INTO resource_access_tombstones(kind,id,ref,owner) VALUES('document','card:public','card:public','owner'),('card','Secret','Secret','owner'),('document','hidden','hidden','owner'),('document_history','purged','purged','owner'),('external_key','external-owner','thread:external-alias','owner')`,
		`INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at,metadata_json) VALUES('raw-id','card','public','card','public','label','now','{}'),('case','card','secret','card','secret','label','now','{}'),('doc-alias','doc','hidden','card','public','label','now','{}'),('json-raw','card','public','card','public','json-label','now','{"reason":"card:public"}'),('purged','document','purged','card','public','label','now','{}'),('external','thread','external-alias','card','public','label','now','{}'),('prose','card','public','card','public','prose-label','now','{"reason":"See **document:purged** yesterday"}')`,
	} {
		if _, err := ws.DB().ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	read := func(c context.Context) []string {
		t.Helper()
		rows, err := resourceaccess.NewDB(ws.DB()).QueryContext(c, `SELECT id FROM ref_edges ORDER BY id`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, id)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return ids
	}
	scope := AccessScope{ActorID: "reader"}
	compareAdmissionGraph(t, ctx, ws.DB(), scope)
	compareAdmissionSpellings(t, ctx, ws.DB(), scope)
	want := read(WithAccessScope(ctx, scope))
	got := read(WithRequestAccessScope(ctx, scope))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidate=%v canonical=%v", got, want)
	}
	if !reflect.DeepEqual(want, []string{"case", "raw-id"}) {
		t.Fatalf("unexpected canonical control: %v", want)
	}
}

func TestCandidateVirtualHistoryReferenceParity(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	if _, err := ws.DB().Exec(`INSERT INTO resource_access_tombstones(kind,id,ref,owner) VALUES('document_history','purged','purged','owner')`); err != nil {
		t.Fatal(err)
	}
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	if _, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": "document:purged"}); err != nil {
		t.Fatal(err)
	}
	compareAdmissionGraph(t, ctx, ws.DB(), AccessScope{ActorID: "reader"})
}
