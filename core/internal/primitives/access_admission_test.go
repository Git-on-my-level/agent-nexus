package primitives

import (
	"context"
	"database/sql"
	"fmt"
	"math/rand"
	"reflect"
	"testing"

	"agent-nexus-core/internal/resourceaccess"
)

// Differentially compare the complete old closure to the new direct-edge
// ledger, including synthetic identities that are not rows in resource tables.
func TestAdmissionGraphMatchesCanonical(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	for i := 0; i < 16; i++ {
		card, err := s.CreateWork(ctx, "owner", "", map[string]any{"title": fmt.Sprintf("Candidate %d", i)})
		if err != nil {
			t.Fatal(err)
		}
		if i%3 == 0 {
			if _, err := s.PatchThread(ctx, "owner", card["thread_id"].(string), map[string]any{"pm_actor_id": "owner"}, nil); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, scope := range []AccessScope{{ActorID: "owner"}, {ActorID: "stranger"}, {ActorID: "owner", PMActorID: "owner"}, {}} {
		compareAdmissionSpellings(t, ctx, ws.DB(), scope)
		t.Run(fmt.Sprintf("%+v", scope), func(t *testing.T) {
			compareAdmissionGraph(t, ctx, ws.DB(), scope)
		})
	}
}

func compareAdmissionSpellings(t *testing.T, ctx context.Context, db admissionQueryer, scope AccessScope) {
	t.Helper()
	read := func(q string) map[[2]string]bool {
		t.Helper()
		rows, err := db.QueryContext(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := map[[2]string]bool{}
		for rows.Next() {
			var ref, typed string
			if err := rows.Scan(&ref, &typed); err != nil {
				t.Fatal(err)
			}
			out[[2]string{ref, typed}] = true
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return out
	}
	want := read(scopeRead(WithAccessScope(ctx, scope), `SELECT ref,CAST(typed AS TEXT) FROM _anx_denied_atoms`))
	roots := `SELECT kind,id FROM main.resource_access_admission_roots r WHERE ` + candidateRootConstraintSQL(scope, "r")
	got := read(`WITH RECURSIVE ` + admissionLedgerCTE(scope) + `, _d(kind,id) AS (` + roots + ` UNION SELECT e.child_kind,e.child_id FROM _d d JOIN main.resource_access_admission_graph e ON e.parent_kind=d.kind AND e.parent_id=d.id WHERE e.ledger=0 OR e.ledger=(SELECT ledger FROM _anx_admission_ledger) UNION SELECT 'document',id FROM _d WHERE kind='document_history') SELECT s.ref,CAST(s.is_ref AS TEXT) FROM _d d JOIN main.resource_access_admission_spellings s ON s.kind=d.kind AND s.id=d.id`)
	if !reflect.DeepEqual(got, want) {
		for ref := range want {
			if !got[ref] {
				t.Errorf("missing projection spelling: %q typed=%s", ref[0], ref[1])
			}
		}
		for ref := range got {
			if !want[ref] {
				t.Errorf("extra projection spelling: %q typed=%s", ref[0], ref[1])
			}
		}
	}
}

func TestAdmissionGraphRandomizedParity(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	tx, err := ws.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("random-%d", i)
		exec(`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES(?,'now','owner','{}')`, id)
		exec(`INSERT INTO cards(id,handle,thread_id,title,summary,column_key,created_at,created_by,updated_at,updated_by) VALUES(?,?,?,'Candidate','','todo','now','owner','now','owner')`, id, id, id)
		exec(`INSERT INTO documents(id,handle,thread_id,title,head_revision_id,head_revision_number,created_at,created_by,updated_at,updated_by) VALUES(?,?,?,'Candidate','',0,'now','owner','now','owner')`, id, id, id)
		exec(`INSERT INTO events(id,handle,type,ts,actor_id,thread_id,refs_json,payload_json) VALUES(?,?,'message_posted','now','owner',?,'[]','{}')`, id, id, id)
	}
	rng := rand.New(rand.NewSource(20261025))
	for step := 0; step < 48; step++ {
		id, target := fmt.Sprintf("random-%d", rng.Intn(12)), fmt.Sprintf("random-%d", rng.Intn(12))
		switch step % 8 {
		case 0:
			exec(`UPDATE threads SET body_json=json_object('pm_actor_id',?) WHERE id=?`, []any{"", "owner", "other", 17}[rng.Intn(4)], id)
		case 1:
			exec(`UPDATE cards SET refs_json=json_array(?) WHERE id=?`, "document:"+target, id)
		case 2:
			exec(`UPDATE cards SET summary=? WHERE id=?`, "Copied from **document:"+target+"** yesterday", id)
		case 3:
			exec(`UPDATE documents SET handle=? WHERE id=?`, fmt.Sprintf("renamed-%d-%s", step, target), id)
		case 4:
			exec(`UPDATE events SET payload_json=json_object('text',?) WHERE id=?`, "card:"+target, id)
		case 5:
			exec(`UPDATE events SET type=?,thread_id=? WHERE id=?`, []string{"message_posted", "custom"}[rng.Intn(2)], target, id)
		case 6:
			exec(`INSERT INTO resource_access_tombstones(kind,id,ref,owner) VALUES('document',?,?,'other') ON CONFLICT DO NOTHING`, id, "old-"+target)
		case 7:
			exec(`DELETE FROM resource_access_tombstones WHERE kind='document' AND id=?`, id)
		}
		for _, scope := range []AccessScope{{ActorID: "owner"}, {ActorID: "stranger"}, {ActorID: "17"}, {ActorID: "owner", PMActorID: "owner"}, {}} {
			t.Run(fmt.Sprintf("step-%d/%+v", step, scope), func(t *testing.T) { compareAdmissionGraph(t, ctx, tx, scope) })
		}
	}
	// Metadata and alias text are privacy filters even when the endpoint
	// identities themselves remain public. Cover native and legacy fallback.
	exec(`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('safe','now','owner','{}')`)
	exec(`INSERT INTO cards(id,handle,thread_id,title,summary,column_key,created_at,created_by,updated_at,updated_by) VALUES('safe','safe','safe','Public','','todo','now','owner','now','owner')`)
	exec(`UPDATE threads SET body_json='{"pm_actor_id":"owner"}' WHERE id='random-0'`)
	exec(`INSERT INTO work_metadata(card_id,authority,metadata_json,updated_at,updated_by) VALUES('random-0','external','{"source":{"url":"legacy-source"}}','now','owner') ON CONFLICT(card_id) DO UPDATE SET authority=excluded.authority,metadata_json=excluded.metadata_json`)
	for i, value := range []string{"ready", "card:random-0", "random-0", "Copied from **card:random-0** yesterday", "card:legacy-source", "legacy-source", "document:random-0"} {
		exec(`INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at,metadata_json) VALUES(?,'card','safe','card','safe',?,'now',json_object('reason',?))`, fmt.Sprintf("display-%d", i), fmt.Sprintf("tag-%d", i), value)
		exec(`INSERT INTO resource_handle_aliases(resource_type,alias_handle,resource_id,canonical_handle,created_at,reason) VALUES('card',?,'safe','safe','now',?)`, fmt.Sprintf("display-%d", i), value)
	}
	for _, scope := range []AccessScope{{ActorID: "owner"}, {ActorID: "stranger"}, {ActorID: "owner", PMActorID: "owner"}, {}} {
		for _, value := range []string{"legacy-source", "card:legacy-source", "safe", "card:safe", "random-0", "card:random-0", "document:random-0", "plan:random-0", "ready"} {
			want := requireAccessibleValues(WithAccessScope(ctx, scope), tx, []string{value})
			got := requireAccessibleValues(WithRequestAccessScope(ctx, scope), tx, []string{value})
			if got != want {
				t.Fatalf("scope=%+v value=%q candidate=%v canonical=%v", scope, value, got, want)
			}
		}
		compareAdmissionSpellings(t, ctx, tx, scope)
		for _, query := range []string{`SELECT id FROM cards ORDER BY id`, `SELECT id FROM cards ORDER BY id LIMIT 3 OFFSET 1`, `SELECT json_array(count(*),EXISTS(SELECT 1 FROM cards ORDER BY id LIMIT 1 OFFSET 3)) FROM cards`, `SELECT id FROM ref_edges ORDER BY id`, `SELECT alias_handle FROM resource_handle_aliases ORDER BY alias_handle`} {
			read := func(c context.Context) []string {
				t.Helper()
				var values []string
				policy, _ := resourceaccess.PolicyFrom(c)
				q, args := query, []any(nil)
				if policy.ReadOnDB != nil {
					q, args = policy.ReadOnDB(c, tx, q, args)
				} else {
					q = policy.Read(q)
				}
				rows, err := tx.QueryContext(c, q, args...)
				if err != nil {
					t.Fatal(err)
				}
				for rows.Next() {
					var value string
					if err := rows.Scan(&value); err != nil {
						t.Fatal(err)
					}
					values = append(values, value)
				}
				if err := rows.Err(); err != nil {
					t.Fatal(err)
				}
				rows.Close()
				return values
			}
			want := read(WithAccessScope(ctx, scope))
			got := read(WithRequestAccessScope(ctx, scope))
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("scope=%+v query=%s candidate=%v canonical=%v", scope, query, got, want)
			}
		}
	}
	// Direct compact-ledger writes and operator rollback must select precisely
	// the same edges as the canonical reader, including an incomplete ledger.
	exec(`INSERT INTO resource_access_compact_edges SELECT edge_id,unhex(target_key) FROM resource_access_exact_edges WHERE true ON CONFLICT(edge_id) DO UPDATE SET target_key=excluded.target_key`)
	for _, control := range []string{`UPDATE resource_access_compact_control SET reader=1,force_legacy=0`, `DELETE FROM resource_access_compact_edges WHERE edge_id=(SELECT min(edge_id) FROM resource_access_compact_edges)`, `UPDATE resource_access_compact_control SET force_legacy=1`, `DELETE FROM resource_access_compact_control`} {
		exec(control)
		for _, scope := range []AccessScope{{ActorID: "owner"}, {ActorID: "stranger"}, {}} {
			compareAdmissionGraph(t, ctx, tx, scope)
		}
	}
}

type admissionQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func compareAdmissionGraph(t *testing.T, ctx context.Context, db admissionQueryer, scope AccessScope) {
	t.Helper()
	old := ownershipClosure("_anx_denied", deniedRootSQL(scope), false)
	graph := admissionLedgerCTE(scope) + ", " + old + `, _indexed(kind,id) AS (SELECT kind,id FROM main.resource_access_admission_roots r WHERE ` + candidateRootConstraintSQL(scope, "r") + `), _reachable(kind,id) AS (SELECT r.kind,r.id FROM main.resource_access_admission_owners r WHERE r.ledger=(SELECT ledger FROM _anx_admission_ledger) AND (` + candidateRootConstraintSQL(scope, "r") + `)), _old_only AS (SELECT kind,CAST(id AS TEXT) id FROM _anx_denied EXCEPT SELECT kind,id FROM _reachable), _new_only AS (SELECT kind,id FROM _reachable EXCEPT SELECT kind,CAST(id AS TEXT) FROM _anx_denied) SELECT 'missing',kind,id FROM _old_only UNION ALL SELECT 'extra',kind,id FROM _new_only UNION ALL SELECT 'root-counts','','' WHERE (SELECT active FROM _anx_admission_ledger)<>EXISTS(` + deniedRootSQL(scope) + `) LIMIT 20`
	rows, err := db.QueryContext(ctx, `WITH RECURSIVE `+graph)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var side, kind, id string
		if err := rows.Scan(&side, &kind, &id); err != nil {
			t.Fatal(err)
		}
		t.Errorf("%s dependency closure: %s:%s", side, kind, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}
