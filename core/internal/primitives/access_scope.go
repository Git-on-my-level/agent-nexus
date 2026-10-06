package primitives

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"agent-nexus-core/internal/resourceaccess"
)

// AccessScope identifies the authenticated reader. Empty ActorID is anonymous,
// not privileged. Trusted internal operations use contexts without this scope.
type AccessScope struct{ ActorID, PMActorID string }
type accessScopeKey struct{}

func WithAccessScope(ctx context.Context, scope AccessScope) context.Context {
	ctx = context.WithValue(ctx, accessScopeKey{}, scope)
	// Rebinding either principal or selected PM starts a new policy boundary.
	ctx = context.WithValue(ctx, denialRequestKey{}, struct{}{})
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

func deniedRootSQL(scope AccessScope) string {
	unknown := "SELECT 'artifact',id FROM main.artifacts WHERE content_refs_json IS NULL"
	if scope.ActorID != "" && scope.ActorID == scope.PMActorID {
		return unknown
	}
	actor := "'" + strings.ReplaceAll(scope.ActorID, "'", "''") + "'"
	return "SELECT 'thread',id FROM main.threads WHERE COALESCE(json_extract(body_json,'$.pm_actor_id'),'')<>'' AND json_extract(body_json,'$.pm_actor_id')<>" + actor + " UNION SELECT kind,id FROM main.resource_access_tombstones WHERE owner<>" + actor + " UNION " + unknown
}

var sqlIdentifiers = regexp.MustCompile(`[A-Za-z_][A-Za-z_0-9]*`)

// Include relation shadows actually named by this SQL statement. Matching all
// tokens (including quoted identifiers and literals) may include extra shadows,
// but never omits a named relation. Ownership recursion still uses canonical
// main tables and is evaluated within every statement's snapshot.
func accessCTEs(scope AccessScope, query string) string {
	return accessCTEsWithSnapshot(scope, query, nil)
}
func accessCTEsWithSnapshot(scope AccessScope, query string, snapshot *denialSnapshot) string {
	needed := map[string]bool{}
	for _, token := range sqlIdentifiers.FindAllString(query, -1) {
		needed[strings.ToLower(token)] = true
	}
	graph := ""

	denied := func(kind, id string) string {
		return "NOT EXISTS (SELECT 1 FROM _anx_denied WHERE kind='" + kind + "' AND id=" + id + ")"
	}
	cleanJSON := func(column string) string {
		return "NOT EXISTS (SELECT 1 FROM json_each(" + resourceaccess.ReferenceSQLAtoms(column, strings.HasSuffix(column, "_json") || column == "_row.body" || column == "_row.labels") + ") j JOIN _anx_denied_atoms d ON j.value=d.ref COLLATE NOCASE OR d.typed AND " + resourceaccess.TextReferenceMatchSQL("j.value", "d.ref") + ")"
	}
	add := func(table, where string) {
		if !needed[table] {
			return
		}
		graph += ", " + table + " AS (SELECT * FROM main." + table + " AS _row WHERE " + where + ")"
	}
	for _, kind := range []string{"thread", "board", "card", "topic", "document", "event", "artifact"} {
		add(resourceTables[kind], denied(kind, "_row.id"))
	}
	add("document_revisions", denied("document", "_row.document_id")+" AND "+denied("document_revision", "_row.revision_id"))
	add("card_revisions", denied("card", "_row.card_id")+" AND "+denied("card_revision", "_row.revision_id"))
	add("resource_handle_aliases", cleanJSON("_row.reason")+" AND NOT EXISTS (SELECT 1 FROM _anx_denied WHERE kind=_row.resource_type AND id=_row.resource_id)")
	add("ref_edges", cleanJSON("_row.metadata_json")+" AND NOT EXISTS (SELECT 1 FROM _anx_denied_refs WHERE ref=_row.source_type || ':' || _row.source_id OR ref=_row.target_type || ':' || _row.target_id)")
	add("resource_access_external_edges", "NOT EXISTS (SELECT 1 FROM _anx_denied WHERE kind=_row.source_kind AND id=_row.source_id)")
	add("work_metadata", denied("card", "_row.card_id"))
	add("work_observations", denied("card", "_row.card_id")+" AND "+denied("work_observation", "_row.id"))
	add("work_evidence_records", denied("card", "_row.card_id")+" AND "+denied("work_evidence_record", "_row.id"))
	add("work_evidence_index", denied("card", "_row.card_id")+" AND "+denied("work_evidence_alias", "_row.id")+" AND "+denied("work_evidence_record", "_row.evidence_id"))
	add("work_evidence_entries", denied("card", "_row.card_id"))
	add("work_evidence_keys", denied("card", "_row.card_id")+" AND "+denied("work_evidence_record", "_row.evidence_id"))
	add("work_participants", denied("card", "_row.card_id")+" AND "+denied("participant", "_row.id"))
	add("card_plans", denied("plan", "_row.card_id"))
	add("agent_wakeups", denied("wakeup", "_row.wakeup_id"))
	add("access_requests", denied("event", "_row.request_event_id")+" AND "+cleanJSON("_row.reason"))
	add("human_attention_response_claims", denied("event", "_row.request_event_id")+" AND "+denied("event", "_row.response_event_id")+" AND "+cleanJSON("_row.response_json"))
	for _, table := range []string{"derived_topic_dirty_queue"} {
		add(table, denied("thread", "_row.thread_id"))
	}
	add("topic_projection_refresh_status", denied("thread", "_row.thread_id")+" AND "+cleanJSON("_row.last_error"))
	add("derived_topic_views", cleanJSON("_row.data_json")+" AND "+denied("thread", "_row.thread_id")+` AND NOT EXISTS (SELECT 1 FROM main.events e JOIN _anx_denied d ON d.kind='event' AND d.id=e.id WHERE e.thread_id=_row.thread_id) AND NOT EXISTS (SELECT 1 FROM main.cards c JOIN _anx_denied d ON d.kind='card' AND d.id=c.id WHERE c.parent_thread_id=_row.thread_id) AND NOT EXISTS (SELECT 1 FROM main.artifacts a JOIN _anx_denied d ON d.kind='artifact' AND d.id=a.id WHERE a.thread_id=_row.thread_id) AND NOT EXISTS (SELECT 1 FROM main.documents doc JOIN _anx_denied d ON d.kind='document' AND d.id=doc.id WHERE doc.thread_id=_row.thread_id)`)
	add("derived_board_views", cleanJSON("_row.data_json")+" AND "+denied("board", "_row.board_id")+` AND NOT EXISTS (SELECT 1 FROM main.cards c JOIN _anx_denied d ON d.kind='card' AND d.id=c.id WHERE c.board_id=_row.board_id)`)
	add("runs", denied("run", "_row.id"))
	add("agent_presence", cleanJSON("_row.note")+" AND NOT EXISTS (SELECT 1 FROM _anx_denied_refs WHERE ref="+resourceaccess.ReferenceSQL("_row.current_card_ref")+" COLLATE NOCASE)")
	add("agent_progress_notes", cleanJSON("_row.text")+" AND NOT EXISTS (SELECT 1 FROM _anx_denied_refs WHERE ref="+resourceaccess.ReferenceSQL("_row.card_ref")+" COLLATE NOCASE)")
	if needed["pm_records"] {
		graph += ", pm_records AS (SELECT rowid,* FROM main.pm_records AS _row WHERE " + denied("pm", "_row.id") + ")"
	}
	add("derived_inbox_items", denied("inbox", "_row.id"))
	add("workspace_dashboard", denied("document", "_row.document_id"))
	add("idempotency_replays", cleanJSON("_row.response_json"))
	for _, source := range resourceaccess.FilterOwnershipSources() {
		if source.Table == "series_adapters" {
			continue // Custom projection below also filters shared freshness.
		}
		add(source.Table, denied(source.Kind, "_row."+source.ID))
	}
	// A rollup must not expose a private contributor through counts or last
	// values. Remove the whole label stream before admission, buckets or limits.
	if needed["series_adapters"] || needed["series_labels"] || needed["series_points"] || needed["series_daily"] || needed["series_live_daily"] {
		graph += ", _anx_private_series(series,labels) AS MATERIALIZED (SELECT json_extract(id,'$[0]'),json_extract(id,'$[1]') FROM _anx_denied WHERE kind='series' UNION SELECT series,labels FROM main.resource_access_series_unknown WHERE EXISTS (SELECT 1 FROM _anx_denied))"
		if needed["series_adapters"] {
			graph += ", series_adapters AS (SELECT name,description,agent_id,host_id,expected_interval,created_at,revoked_at,deleted_at,CASE WHEN EXISTS (SELECT 1 FROM main.series_definitions def LEFT JOIN _anx_private_series p ON p.series=def.name WHERE def.adapter=_row.name AND (p.series IS NOT NULL OR NOT (" + denied("filter/series_definitions", "def.name") + "))) THEN NULL ELSE last_push END AS last_push FROM main.series_adapters _row WHERE " + denied("filter/series_adapters", "_row.name") + ")"
		}
		for _, table := range []string{"series_labels", "series_points", "series_daily", "series_live_daily"} {
			add(table, cleanJSON("_row.labels")+" AND NOT EXISTS (SELECT 1 FROM _anx_private_series p WHERE p.series=_row.series AND p.labels=_row.labels)")
		}
	}
	// An unused ownership graph does not filter any row, but SQLite still has
	// to parse it. Keep graph-only callers and explicit internal dependencies.
	needsGraph := graph != "" || query == ""
	for name := range needed {
		needsGraph = needsGraph || strings.HasPrefix(name, "_anx_")
	}
	if !needsGraph {
		return ""
	}
	deniedGraph := ownershipClosure("_anx_denied", deniedRootSQL(scope), false)
	if snapshot != nil {
		epoch := fmt.Sprint(snapshot.epoch)
		current := "COALESCE((SELECT version FROM main.resource_access_epoch WHERE singleton=1),-1)"
		roots := "SELECT * FROM (" + deniedRootSQL(scope) + ") WHERE " + current + "<>" + epoch
		deniedGraph = ownershipClosure("_anx_fresh_denied", roots, false) + ", _anx_denied(kind,id) AS MATERIALIZED (SELECT kind,id FROM _anx_fresh_denied UNION SELECT json_extract(value,'$[0]'),json_extract(value,'$[1]') FROM json_each(?) WHERE " + current + "=" + epoch + ")"
	}
	// Most indexed resource reads only need denied (kind,id) rows. Unused ref
	// and atom CTEs still duplicate the closure during SQLite preparation.
	needsRefs := query == "" || needed["_anx_resource_refs"] || needed["_anx_denied_refs"] || needed["_anx_denied_atoms"] || strings.Contains(graph, "_anx_denied_refs") || strings.Contains(graph, "_anx_denied_atoms")
	if !needsRefs {
		return deniedGraph + graph
	}
	return deniedGraph +
		", " + ownershipRefs("_anx_resource_refs", "_anx_denied") +
		// One reference to the identities CTE avoids copying the recursive graph
		// again during SQLite preparation just to emit the document synonym.
		", _anx_denied_refs(ref) AS MATERIALIZED (SELECT DISTINCT j.value FROM _anx_resource_refs r JOIN json_each(json_array(CASE WHEN r.kind='external_key' OR r.kind='card' AND (r.ref LIKE 'http://%' OR r.ref LIKE 'https://%') THEN r.ref ELSE r.kind||':'||r.ref END,CASE WHEN r.kind='document' THEN 'doc:'||r.ref END)) j WHERE j.value IS NOT NULL), _anx_denied_atoms(ref,typed) AS MATERIALIZED (SELECT ref,1 FROM _anx_denied_refs UNION SELECT id,0 FROM _anx_denied WHERE kind NOT IN ('plan','work_evidence_record','work_evidence_alias','external_key') AND kind NOT LIKE 'filter/%')" + graph
}

// Apply relation visibility before limits, aggregates and cursors. SQLite
// evaluates ownership in every statement snapshot, including each SSE poll.
// Mutation SQL is never rewritten: mutations must load/authorize existing
// targets and destinations through these same relations before writing.
func scopeRead(ctx context.Context, query string) string {
	return scopeReadWithSnapshot(ctx, query, nil)
}

func scopeReadWithSnapshot(ctx context.Context, query string, snapshot *denialSnapshot) string {
	scope, scoped := accessScopeFrom(ctx)
	if !scoped {
		return query
	}
	q := strings.TrimSpace(query)
	// Scoped relation shadows have no named physical indexes. The optimizer
	// still pushes candidate keys into the indexed canonical relations.
	for _, index := range []string{"idx_work_metadata_project", "idx_work_source_url", "idx_work_evidence_lookup", "idx_work_evidence_public_lookup", "idx_cards_handle_unique", "sqlite_autoindex_cards_1"} {
		q = strings.ReplaceAll(q, " INDEXED BY "+index, "")
	}
	upper := strings.ToUpper(q)
	graph := accessCTEsWithSnapshot(scope, q, snapshot)
	prefix := "WITH RECURSIVE " + graph
	if fields := strings.Fields(upper); len(fields) > 0 {
		switch fields[0] {
		case "WITH":
			if graph == "" {
				return q
			}
			rest := strings.TrimSpace(q[len("WITH"):])
			if strings.HasPrefix(strings.ToUpper(rest), "RECURSIVE") {
				rest = strings.TrimSpace(rest[len("RECURSIVE"):])
			}
			return prefix + ", " + rest
		case "SELECT":
			if graph == "" {
				return q
			}
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
	// A structured SQL column contributes JSON atoms, while a scalar identity
	// contributes its original spelling even when it happens to be valid JSON.
	_, mutation := values.(resourceaccess.SQLValues)
	if sqlValues, ok := values.(resourceaccess.SQLValues); ok {
		structured := resourceaccess.StructuredSQLArguments(sqlValues.Query)
		decoded := make([]any, len(sqlValues.Args))
		for i, arg := range sqlValues.Args {
			decoded[i] = arg
			var raw []byte
			switch v := arg.(type) {
			case string:
				raw = []byte(v)
			case resourceaccess.ReferenceManifest:
				raw = []byte(v)
			case []byte:
				raw = v
				decoded[i] = string(v)
			}
			if structured[i] && len(raw) > 0 {
				var nested any
				if json.Unmarshal(raw, &nested) == nil {
					decoded[i] = nested
				}
			}
		}
		values = decoded
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return err
	}
	// Requests with no reference atoms (health, identity and collection reads)
	// cannot reference private resources. Avoid even opening the denial graph.
	if resourceaccess.ContentReferenceAtomsJSON(string(encoded), "structured") == "[]" {
		return nil
	}
	// This check uses the mutation's own transaction snapshot. Without denied
	// roots there can be no denied descendants; skip constructing the expensive
	// closure, never cache this answer across statements or requests.
	var hasDeniedRoot bool
	if err := q.QueryRowContext(ctx, "SELECT EXISTS ("+deniedRootSQL(scope)+")").Scan(&hasDeniedRoot); err != nil {
		return err
	}
	if !hasDeniedRoot {
		return nil
	}
	var denied bool
	query := `SELECT EXISTS (
 SELECT 1 FROM json_each(anx_resource_json_refs(CAST(? AS BLOB))) j JOIN _anx_denied_atoms d
 ON j.value=d.ref COLLATE NOCASE OR d.typed AND ` + resourceaccess.TextReferenceMatchSQL("j.value", "d.ref") + `)`
	args := []any{string(encoded)}
	_, visitSnapshot := ctx.Value(overviewVisitWriteKey{}).(struct{})
	if policy, ok := resourceaccess.PolicyFrom(ctx); (!mutation || visitSnapshot) && ok && policy.ReadOnDB != nil {
		// Read selector checks share the request closure, still validating its
		// epoch inside this statement. A visit check runs on the write transaction
		// connection, so an epoch mismatch uses that transaction's current graph.
		query, args = policy.ReadOnDB(ctx, q, query, args)
	} else {
		query = `WITH RECURSIVE ` + accessCTEs(scope, "") + ` ` + query
	}
	if err = q.QueryRowContext(ctx, query, args...).Scan(&denied); err != nil {
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
