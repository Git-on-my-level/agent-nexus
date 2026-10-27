package primitives

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"agent-nexus-core/internal/testsql"
)

func TestPublicRefProjectionBatchesAndRetainsHiddenIDs(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	defer db.Close()
	db.SetMaxOpenConns(1)
	s := NewTestStore(db, ws.Layout().ArtifactContentDir)
	for _, query := range []string{
		`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('secret-thread','now','owner','{"pm_actor_id":"owner"}')`,
		`INSERT INTO documents(id,handle,title,thread_id,head_revision_id,head_revision_number,created_at,created_by,updated_at,updated_by) VALUES('secret','hidden-handle','Secret','secret-thread','head',1,'now','owner','now','owner')`,
	} {
		if _, err = db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	refs := []string{"document:secret", "document:missing"}
	seedTx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer seedTx.Rollback()
	for i := 0; i < 450; i++ {
		id := fmt.Sprintf("public-%d", i)
		if _, err = seedTx.Exec(`INSERT INTO documents(id,handle,title,head_revision_id,head_revision_number,created_at,created_by,updated_at,updated_by) VALUES(?,?,'Public','head',1,'now','owner','now','owner')`, id, "handle-"+id); err != nil {
			t.Fatal(err)
		}
		refs = append(refs, "document:"+id)
	}
	if err = seedTx.Commit(); err != nil {
		t.Fatal(err)
	}
	scope := WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"})
	scope, finish, err := s.BeginResourceRead(scope)
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	counter.Reset()
	input := map[string]any{"related_refs": refs, "nested": map[string]any{"subject_ref": "document:public-1"}, "body": "document:public-2"}
	out := publicRefsInValue(scope, s.db, input).(map[string]any)
	if count := counter.Count(); count != 3 {
		t.Fatalf("450 refs need 3 bounded batches, got %d", count)
	}
	joined := strings.Join(out["related_refs"].([]string), ",")
	if strings.Contains(joined, "hidden-handle") || !strings.Contains(joined, "document:secret") || !strings.Contains(joined, "document:handle-public-449") || !strings.Contains(joined, "document:missing") {
		t.Fatal(joined)
	}
	if out["body"] != "document:public-2" || out["nested"].(map[string]any)["subject_ref"] != "document:handle-public-1" {
		t.Fatalf("wrong field traversal: %v", out)
	}
}

func TestPublicRefCacheIsClearedOnPrincipalRebind(t *testing.T) {
	ctx := context.WithValue(context.Background(), publicRefCacheKey{}, map[string]string{"document:secret": "document:hidden-handle"})
	rebound := WithAccessScope(ctx, AccessScope{ActorID: "stranger"})
	if _, ok := rebound.Value(publicRefCacheKey{}).(map[string]string); ok {
		t.Fatal("presentation cache survived principal rebind")
	}
}

func TestPublicRevisionBatchPreservesNormalizedHandles(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	defer db.Close()
	s := NewTestStore(db, ws.Layout().ArtifactContentDir)
	doc, revision, err := s.CreateDocument(ctx, "owner", map[string]any{"title": "Batch revision control"}, "body", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	expected := doc["head_revision_ref"].(string)
	scope, closeRead, err := s.BeginResourceRead(WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"}))
	if err != nil {
		t.Fatal(err)
	}
	defer closeRead()
	counter.Reset()
	refs := []string{expected, "document_revision:" + strings.ToUpper(strings.TrimPrefix(expected, "document_revision:")), "document_revision:" + revision["revision_id"].(string)}
	got := publicTypedRefs(scope, s.db, refs)
	if len(got) != 1 || got[0] != expected || counter.Count() != 1 {
		t.Fatalf("revision aliases: got=%v expected=%s queries=%d", got, expected, counter.Count())
	}
	counter.Reset()
	handleOnly := []string{"document_revision:" + strings.ToUpper(strings.TrimPrefix(expected, "document_revision:"))}
	got = publicTypedRefs(scope, s.db, handleOnly)
	if len(got) != 1 || got[0] != expected || counter.Count() != 1 {
		t.Fatalf("handle-only revision: got=%v expected=%s queries=%d", got, expected, counter.Count())
	}
}

func TestPublicRevisionBatchResolvesHandleWhenIdentityMisses(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	defer db.Close()
	s := NewTestStore(db, ws.Layout().ArtifactContentDir)
	for _, query := range []string{
		`INSERT INTO documents(id,handle,title,head_revision_id,head_revision_number,created_at,created_by,updated_at,updated_by) VALUES('doc','Mixed-Handle','Doc','rev',1,'now','owner','now','owner')`,
		`INSERT INTO document_revisions(revision_id,document_id,revision_number,artifact_id,created_at,created_by) VALUES('rev','doc',1,'blob','now','owner')`,
	} {
		if _, err = db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec(`DELETE FROM resource_access_identities WHERE kind='document_revision'`); err != nil {
		t.Fatal(err)
	}
	expected := "document_revision:Mixed-Handle-r1"
	scope, closeRead, err := s.BeginResourceRead(WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"}))
	if err != nil {
		t.Fatal(err)
	}
	defer closeRead()
	counter.Reset()
	got := publicTypedRefs(scope, s.db, []string{"document_revision:MIXED-HANDLE-R1"})
	if len(got) != 1 || got[0] != expected || counter.Count() != 1 {
		t.Fatalf("identity-miss handle: got=%v expected=%s queries=%d", got, expected, counter.Count())
	}
	statement := counter.Statements()[0]
	rows, err := ws.DB().QueryContext(ctx, "EXPLAIN QUERY PLAN "+statement.SQL, statement.Args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan.WriteString(detail + "\n")
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	text := plan.String()
	if strings.Contains(text, "SCAN _row") || strings.Contains(text, "SCAN r") || strings.Contains(text, "SCAN route") || !strings.Contains(text, "idx_access_identity_key") || !strings.Contains(text, "(document_id=? AND revision_number=?)") {
		t.Fatalf("revision routing lost bounded indexes: %s", text)
	}

}

func TestEventRefBatchUsesIndexedIDAndHandleSelectors(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	db, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	defer db.Close()
	s := NewTestStore(db, ws.Layout().ArtifactContentDir)
	if _, err = db.Exec(`INSERT INTO events(id,handle,type,ts,actor_id,refs_json,payload_json) VALUES('event-id','event-handle','message_posted','now','owner','[]','{}')`); err != nil {
		t.Fatal(err)
	}
	scope, finish, err := s.BeginResourceRead(WithRequestAccessScope(ctx, AccessScope{ActorID: "reader"}))
	if err != nil {
		t.Fatal(err)
	}
	defer finish()
	counter.Reset()
	events, err := s.GetEventsByRefs(scope, []string{"event:event-id", "event:event-handle", "event:unknown"})
	if err != nil || len(events) != 2 || events["event:event-handle"]["id"] != "event-id" {
		t.Fatalf("batch hydration=%v err=%v", events, err)
	}
	if counter.Count() != 1 {
		t.Fatalf("batch did %d queries", counter.Count())
	}
	statement := counter.Statements()[0]
	rows, err := ws.DB().QueryContext(ctx, "EXPLAIN QUERY PLAN "+statement.SQL, statement.Args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	plans := []string{}
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err = rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plans = append(plans, detail)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(plans, "\n")
	if !strings.Contains(plan, "sqlite_autoindex_events_1") || !strings.Contains(plan, "idx_events_handle_unique") || strings.Contains(plan, "SCAN _row") {
		t.Fatalf("event selector lost indexes: %s", plan)
	}
}
