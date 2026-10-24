package server

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/storage"
	"agent-nexus-core/internal/testutil/perfguard"
)

func TestCandidateDenseAncestryAndBucketFanout(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	tx, err := ws.DB().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO resource_access_tombstones(kind,id,ref,owner) VALUES('card','dense-0','dense-0','reader'),('card','unrelated-private','unrelated-private','other')`)
	for i := 0; i <= 128; i++ {
		id := fmt.Sprintf("dense-%d", i)
		exec(`INSERT INTO cards(id,handle,title,summary,column_key,created_at,created_by,updated_at,updated_by) VALUES(?,?,'Dense','','todo','now','owner','now','owner')`, id, id)
	}
	// Shared DAG with 1,016 incoming dependencies, plus cycles and duplicate
	// canonical spellings. Admission should seek roots, not replay this ancestry.
	for i := 1; i <= 128; i++ {
		for j := max(0, i-8); j < i; j++ {
			exec(`INSERT INTO resource_access_edges(source_kind,source_id,target_ref) VALUES('card',?,?)`, fmt.Sprintf("dense-%d", i), fmt.Sprintf("card:dense-%d", j))
		}
	}
	exec(`INSERT INTO resource_access_edges(source_kind,source_id,target_ref) VALUES('card','dense-4','card:dense-128')`)
	for i := 0; i < 128; i++ {
		id := fmt.Sprintf("fanroot-%d", i)
		exec(`INSERT INTO resource_access_tombstones(kind,id,ref,owner) VALUES('card',?,?,'reader')`, id, id)
		exec(`INSERT INTO resource_access_edges(source_kind,source_id,target_ref) VALUES('card','dense-128',?)`, "card:"+id)
	}
	for i := 0; i < 512; i++ {
		id := fmt.Sprintf("sharedprefix-%04d", i)
		exec(`INSERT INTO resource_access_tombstones(kind,id,ref,owner) VALUES('document',?,?,'reader')`, id, id)
	}
	texts := []string{}
	for i := 0; i < 20; i++ {
		texts = append(texts, fmt.Sprintf("See document:sharedprefix-%04d.", i))
	}
	encodedTexts, _ := json.Marshal(texts)
	exec(`INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,metadata_json,created_at) VALUES('dense-prose','card','dense-128','card','dense-127','test',?,'now')`, string(encodedTexts))
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	db, capture, err := perfguard.Open("file:" + ws.Layout().DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := primitives.NewTestStore(db, ws.Layout().ArtifactContentDir)
	scoped := resourceaccess.NewDB(db)
	scope := primitives.AccessScope{ActorID: "reader"}
	ids := []string{}

	for i := 0; i < 20; i++ {
		ids = append(ids, fmt.Sprintf("dense-%d", 128-i))
	}
	encodedIDs, _ := json.Marshal(ids)
	for _, test := range []struct {
		name, sql string
		args      []any
		want      int
	}{
		{"dense ancestry", `SELECT count(*) FROM cards WHERE id IN (SELECT value FROM json_each(?))`, []any{string(encodedIDs)}, 20},
		{"dense legacy bucket", `SELECT count(*) FROM ref_edges WHERE id='dense-prose'`, nil, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var canonical int
			if err := scoped.QueryRowContext(primitives.WithAccessScope(ctx, scope), test.sql, test.args...).Scan(&canonical); err != nil || canonical != test.want {
				t.Fatalf("canonical=%d want=%d err=%v", canonical, test.want, err)
			}
			for _, phase := range []string{"cold", "warm", "invalidated"} {
				if phase == "invalidated" {
					if _, err := ws.DB().Exec(`UPDATE resource_access_tombstones SET owner=owner WHERE id='unrelated-private'`); err != nil {
						t.Fatal(err)
					}
				}
				capture.Start()
				request, closeRead, err := store.BeginResourceRead(primitives.WithRequestAccessScope(ctx, scope))
				if err != nil {
					t.Fatal(err)
				}
				var got int
				err = scoped.QueryRowContext(request, test.sql, test.args...).Scan(&got)
				capture.Stop()
				closeRead()
				work := capture.Work()
				t.Logf("%s work=%+v", phase, work)
				if err != nil || capture.WorkError() != nil || got != test.want {
					t.Fatalf("got=%d want=%d err=%v instrumentation=%v", got, test.want, err, capture.WorkError())
				}
				if work.VMSteps > 50000 {
					t.Errorf("%s exceeds unchanged 50,000 VM ceiling: %+v", phase, work)
				}
			}
		})
	}

	if _, err := ws.DB().Exec(`UPDATE resource_access_tombstones SET owner='other' WHERE kind='document' AND id='sharedprefix-0000'`); err != nil {
		t.Fatal(err)
	}
	var denied int
	if err := scoped.QueryRowContext(primitives.WithRequestAccessScope(ctx, scope), `SELECT count(*) FROM ref_edges WHERE id='dense-prose'`).Scan(&denied); err != nil || denied != 0 {
		t.Fatalf("bucket revocation=%d err=%v", denied, err)
	}
}
