package primitives

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/storage"
)

// Compare the optimized projection predicate with the original complete
// spelling/OR matcher. Buckets must never become a visibility decision.
func TestProjectionReferenceProbeParity(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	for _, id := range []string{"private", "private-long", "legacy document", "[]", "Ångström", "k", "punct.id", "under_score"} {
		if _, err = ws.DB().Exec(`INSERT INTO resource_access_tombstones(kind,id,ref,owner) VALUES('document',?,?, 'owner')`, id, id); err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []string{"waKeup", "document_reviſion"} {
		if _, err = ws.DB().Exec(`INSERT INTO resource_access_tombstones(kind,id,ref,owner) VALUES(?,'private-kind','private-kind','owner')`, kind); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = ws.DB().Exec(`INSERT INTO resource_access_tombstones(kind,id,ref,owner) VALUES('document','deleted-id','historical-private','owner')`); err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"alternate:ref", "plain-key-alias"} {
		if _, err = ws.DB().Exec(`INSERT INTO resource_access_tombstones(kind,id,ref,owner) VALUES('external_key',?,?, 'owner')`, "id-"+ref, ref); err != nil {
			t.Fatal(err)
		}
	}
	scope := AccessScope{ActorID: "stranger"}
	old := `SELECT NOT EXISTS (SELECT 1 FROM json_each(anx_resource_json_refs(CAST(? AS BLOB))) j JOIN _anx_denied_atoms d ON j.value=d.ref COLLATE NOCASE OR d.typed AND ` + resourceaccess.TextReferenceMatchSQL("j.value", "d.ref") + `)`
	values := []string{"alternate:ref", "ALTERNATE:REF", "plain-key-alias", "PLAIN-KEY-ALIAS", "See alternate:ref!", "doc:historical-private", "See doc:historical-private!", "historical-private", "See wakeup:private-kind!", "See document_revision:private-kind!", "public", "PRIVATE", "private-extra", "doc:private", "DOCUMENT:private", "See document:private.", "See document:private-extra.", "document:legacy document", "See doc:legacy document!", "See doc:[]", "See doc:ÅNGSTRÖM.", "See doc:K!", "See doc:punct.id.", "See doc:_under_score_", "_doc:private_", "xdoc:private", "See doc:private\x00", "See doc:private\xff"}
	for i, value := range values {
		raw, _ := json.Marshal(map[string]any{"snapshot": []any{value}, value: "key references count too"})
		thread := fmt.Sprintf("projection-%d", i)
		if _, err = ws.DB().Exec(`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES(?,'now','fixture','{}')`, thread); err != nil {
			t.Fatal(err)
		}
		if _, err = ws.DB().Exec(`INSERT INTO derived_topic_views(thread_id,generated_at,data_json) VALUES(?,'now',?)`, thread, string(raw)); err != nil {
			t.Fatal(err)
		}
		var want bool
		if err = ws.DB().QueryRow(`WITH RECURSIVE `+accessCTEs(scope, old)+` `+old, string(raw)).Scan(&want); err != nil {
			t.Fatal(err)
		}
		if (value == "alternate:ref" || value == "plain-key-alias") && want {
			t.Fatal("alternate external-key tombstone ref must deny projection")
		}
		for mode, request := range []context.Context{WithAccessScope(ctx, scope), WithRequestAccessScope(ctx, scope)} {
			var got bool
			err = resourceaccess.NewDB(ws.DB()).QueryRowContext(request, `SELECT EXISTS(SELECT 1 FROM derived_topic_views WHERE thread_id=?)`, thread).Scan(&got)
			if err != nil || got != want {
				t.Fatalf("value=%q got=%v want=%v err=%v", value, got, want, err)
			}
			if mode == 1 {
				pin, close, err := s.BeginOverviewRead(request)
				if err != nil {
					t.Fatal(err)
				}
				if _, ok := pin.Value(pinnedDenialKey{}).(pinnedDenial); !ok {
					close()
					t.Fatal("projection read was not admitted")
				}
				err = s.db.QueryRowContext(pin, `SELECT EXISTS(SELECT 1 FROM derived_topic_views WHERE thread_id=?)`, thread).Scan(&got)
				close()
				if err != nil || got != want {
					t.Fatalf("admitted projection value=%q got=%v want=%v err=%v", value, got, want, err)
				}
			}
		}
	}
	query := `SELECT thread_id FROM derived_topic_views`
	rows, err := ws.DB().Query(`EXPLAIN QUERY PLAN ` + scopeRead(WithAccessScope(ctx, scope), query))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var equality, bucket bool
	for rows.Next() {
		var a, b, c int
		var detail string
		if err = rows.Scan(&a, &b, &c, &detail); err != nil {
			t.Fatal(err)
		}
		equality = equality || strings.Contains(detail, "SEARCH d USING AUTOMATIC") && strings.Contains(detail, "ref=?")
		bucket = bucket || strings.Contains(detail, "SEARCH d USING AUTOMATIC") && strings.Contains(detail, "bucket=?")
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if !equality || !bucket {
		t.Fatalf("projection probes must use equality indexes: equality=%v bucket=%v", equality, bucket)
	}
}
