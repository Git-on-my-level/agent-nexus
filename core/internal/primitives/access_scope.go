package primitives

import (
	"context"
	"encoding/json"
	"strings"

	"agent-nexus-core/internal/resourceaccess"
)

// AccessScope identifies the authenticated reader. Empty ActorID is anonymous,
// not privileged. Trusted internal operations use contexts without this scope.
type AccessScope struct{ ActorID, PMActorID string }
type accessScopeKey struct{}

func WithAccessScope(ctx context.Context, scope AccessScope) context.Context {
	ctx = context.WithValue(ctx, accessScopeKey{}, scope)
	return resourceaccess.WithPolicy(ctx, resourceaccess.Policy{
		Read: func(q string) string { return scopeRead(ctx, q) },
		Check: func(c context.Context, q resourceaccess.QueryRower, v any) error {
			return requireAccessibleValues(c, q, v)
		},
	})
}
func accessScopeFrom(ctx context.Context) (AccessScope, bool) {
	scope, ok := ctx.Value(accessScopeKey{}).(AccessScope)
	return scope, ok
}

// Narrow database handles prevent accidental contextless reads.
type accessDB = resourceaccess.DB
type accessTx = resourceaccess.Tx

func accessCTEs(scope AccessScope) string {
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	graph := ownershipClosure("_anx_denied", "SELECT 'thread',id FROM main.threads WHERE COALESCE(json_extract(body_json,'$.pm_actor_id'),'')<>'' AND json_extract(body_json,'$.pm_actor_id')<>"+quote(scope.ActorID)+" AND NOT ("+quote(scope.ActorID)+"<>'' AND "+quote(scope.ActorID)+"="+quote(scope.PMActorID)+") UNION SELECT kind,id FROM main.resource_access_tombstones WHERE owner<>"+quote(scope.ActorID)+" AND NOT ("+quote(scope.ActorID)+"<>'' AND "+quote(scope.ActorID)+"="+quote(scope.PMActorID)+")", false)
	graph += ", " + ownershipRefs("_anx_resource_refs", "_anx_denied") + ", _anx_denied_refs(ref) AS (SELECT CASE WHEN kind='card' AND (ref LIKE 'http://%' OR ref LIKE 'https://%') THEN ref ELSE kind||':'||ref END FROM _anx_resource_refs UNION SELECT 'doc:'||ref FROM _anx_resource_refs WHERE kind='document')"

	denied := func(kind, id string) string {
		return "NOT EXISTS (SELECT 1 FROM _anx_denied WHERE kind='" + kind + "' AND id=" + id + ")"
	}
	cleanJSON := func(column string) string {
		return "NOT EXISTS (SELECT 1 FROM json_tree(" + column + ") j WHERE " + resourceaccess.ReferenceSQL("j.atom") + " COLLATE NOCASE IN (SELECT ref FROM _anx_denied_refs) OR " + resourceaccess.TrimSpaceSQL("j.atom") + " COLLATE NOCASE IN (SELECT id FROM _anx_denied))"
	}
	add := func(table, where string) {
		graph += ", " + table + " AS (SELECT * FROM main." + table + " AS _row WHERE " + where + ")"
	}
	for _, kind := range []string{"thread", "board", "card", "topic", "document", "event", "artifact"} {
		add(resourceTables[kind], denied(kind, "_row.id"))
	}
	add("document_revisions", denied("document", "_row.document_id"))
	add("card_revisions", denied("card", "_row.card_id"))
	add("resource_handle_aliases", "NOT EXISTS (SELECT 1 FROM _anx_denied WHERE kind=_row.resource_type AND id=_row.resource_id)")
	add("ref_edges", "NOT EXISTS (SELECT 1 FROM _anx_denied_refs WHERE ref=_row.source_type || ':' || _row.source_id OR ref=_row.target_type || ':' || _row.target_id)")
	for _, table := range []string{"work_metadata", "work_observations", "work_participants"} {
		add(table, denied("card", "_row.card_id"))
	}
	add("card_plans", denied("plan", "_row.card_id"))
	add("agent_wakeups", denied("wakeup", "_row.wakeup_id"))
	add("access_requests", denied("event", "_row.request_event_id"))
	for _, table := range []string{"topic_projection_refresh_status", "derived_topic_dirty_queue", "home_topic_read_cursors"} {
		add(table, denied("thread", "_row.thread_id"))
	}
	add("derived_topic_views", denied("thread", "_row.thread_id")+` AND NOT EXISTS (SELECT 1 FROM main.events e JOIN _anx_denied d ON d.kind='event' AND d.id=e.id WHERE e.thread_id=_row.thread_id) AND NOT EXISTS (SELECT 1 FROM main.cards c JOIN _anx_denied d ON d.kind='card' AND d.id=c.id WHERE c.parent_thread_id=_row.thread_id)`)
	add("derived_board_views", denied("board", "_row.board_id")+` AND NOT EXISTS (SELECT 1 FROM main.cards c JOIN _anx_denied d ON d.kind='card' AND d.id=c.id WHERE c.board_id=_row.board_id)`)
	add("runs", denied("run", "_row.id"))
	add("agent_presence", "NOT EXISTS (SELECT 1 FROM _anx_denied_refs WHERE ref="+resourceaccess.ReferenceSQL("_row.current_card_ref")+" COLLATE NOCASE)")
	add("agent_progress_notes", "NOT EXISTS (SELECT 1 FROM _anx_denied_refs WHERE ref="+resourceaccess.ReferenceSQL("_row.card_ref")+" COLLATE NOCASE)")
	graph += ", _anx_private_pm(id) AS (SELECT id FROM main.pm_records AS _row WHERE NOT (" + cleanJSON("_row.body") + `) UNION SELECT r.id FROM main.pm_records r JOIN json_tree(r.body) j JOIN _anx_private_pm p ON j.atom=p.id), pm_records AS (SELECT rowid,* FROM main.pm_records WHERE id NOT IN (SELECT id FROM _anx_private_pm))`
	add("derived_inbox_items", denied("thread", "_row.thread_id")+" AND "+denied("card", "_row.source_card_id")+" AND "+denied("event", "_row.source_event_id")+" AND "+cleanJSON("_row.data_json"))
	add("workspace_dashboard", denied("document", "_row.document_id"))
	add("idempotency_replays", cleanJSON("_row.response_json"))
	return graph
}

// Apply relation visibility before limits, aggregates and cursors. SQLite
// evaluates ownership in every statement snapshot, including each SSE poll.
// Mutation SQL is never rewritten: mutations must load/authorize existing
// targets and destinations through these same relations before writing.
func scopeRead(ctx context.Context, query string) string {
	scope, scoped := accessScopeFrom(ctx)
	if !scoped {
		return query
	}
	q := strings.ReplaceAll(strings.TrimSpace(query), " INDEXED BY idx_work_metadata_project", "")
	upper := strings.ToUpper(q)
	prefix := "WITH RECURSIVE " + accessCTEs(scope)
	if fields := strings.Fields(upper); len(fields) > 0 {
		switch fields[0] {
		case "WITH":
			rest := strings.TrimSpace(q[len("WITH"):])
			if strings.HasPrefix(strings.ToUpper(rest), "RECURSIVE") {
				rest = strings.TrimSpace(rest[len("RECURSIVE"):])
			}
			return prefix + ", " + rest
		case "SELECT":
			return prefix + " " + q
		}
	}
	// Scoped reads must be SELECTs; new query forms require deliberate review.
	return "SELECT * FROM _anx_unsupported_scoped_read"
}

// CanAccessResource is the principal-can-access predicate. Point resolution,
// lists and mutation loaders all evaluate this same relational policy.
func (s *Store) CanAccessResource(ctx context.Context, typ, ref string) bool {
	_, err := s.ResolveResourceRef(ctx, ResourceRefInput{Type: typ, Ref: ref})
	return err == nil
}

// requireAccessibleValues rejects inaccessible existing resource identities in
// mutation arguments, including JSON refs and aliases. New identities are not
// rejected. This runs on the mutation connection before Exec, so a missed
// handler check cannot write through a known private ID or parent reference.
func requireAccessibleValues(ctx context.Context, q queryRower, values any) error {
	scope, ok := accessScopeFrom(ctx)
	if !ok {
		return nil
	}
	// SQL JSON columns arrive as encoded strings/bytes. Decode before walking,
	// otherwise json_tree would see just an opaque string (or base64 bytes).
	if args, ok := values.([]any); ok {
		decoded := make([]any, len(args))
		for i, arg := range args {
			decoded[i] = arg
			var raw []byte
			switch v := arg.(type) {
			case string:
				raw = []byte(v)
			case []byte:
				raw = v
			}
			var nested any
			if len(raw) > 0 && json.Unmarshal(raw, &nested) == nil {
				decoded[i] = nested
			}
		}
		values = decoded
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return err
	}
	var denied bool
	query := `WITH RECURSIVE ` + accessCTEs(scope) + ` SELECT EXISTS (
 SELECT 1 FROM json_tree(?) j WHERE j.type='text' AND (
 ` + resourceaccess.TrimSpaceSQL("j.atom") + ` COLLATE NOCASE IN (SELECT id FROM _anx_denied) OR
 ` + resourceaccess.ReferenceSQL("j.atom") + ` COLLATE NOCASE IN (SELECT ref FROM _anx_denied_refs)))`
	if err = q.QueryRowContext(ctx, query, string(encoded)).Scan(&denied); err != nil {
		return err
	}
	if denied {
		return ErrNotFound
	}
	return nil
}

// CheckResourceValues applies the same policy to HTTP selectors and decoded
// bodies before idempotency replay, conflict details, attribution or side effects.
func (s *Store) CheckResourceValues(ctx context.Context, values any) error {
	return s.db.CheckValues(ctx, values)
}

// CanonicalMaintenanceContext is only for canonical projection/ledger maintenance.
// Only canonical maintenance may remove reader scope. It must not return raw
// records to a client, and must not be used for ordinary business mutations.
func CanonicalMaintenanceContext(ctx context.Context) context.Context {
	return resourceaccess.WithoutPolicy(context.WithValue(ctx, accessScopeKey{}, struct{}{}))
}

// Preserve the authorization of orphaned evidence before deleting ownership or
// ref edges. Purge is irreversible; retained identities contain no content.
func preservePurgedAccess(ctx context.Context, tx *accessTx, kind, id string) error {
	maintenance := CanonicalMaintenanceContext(ctx)
	graph := privateOwnershipGraph() + ", " + ownershipClosure("_anx_desc", "SELECT ?,?", false)
	graph += ", _anx_orphans AS (SELECT p.* FROM _anx_private p JOIN _anx_desc d ON d.kind=p.kind AND d.id=p.id), " + ownershipRefs("_anx_orphan_refs", "_anx_orphans")
	_, err := tx.ExecContext(maintenance, "WITH RECURSIVE "+graph+" INSERT OR IGNORE INTO resource_access_tombstones(kind,id,ref,owner) SELECT p.kind,p.id,r.ref,p.owner FROM _anx_orphans p JOIN _anx_orphan_refs r ON r.kind=p.kind AND r.id=p.id", kind, id)
	return err
}
