package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"agent-nexus-core/internal/resourceaccess"
)

// AccessScope identifies the authenticated reader. Empty ActorID is anonymous,
// not privileged. Trusted internal operations use contexts without this scope.
type AccessScope struct {
	ActorID, PMActorID string
	AuthAdmin          bool
}
type accessScopeKey struct{}

func WithAccessScope(ctx context.Context, scope AccessScope) context.Context {
	ctx = context.WithValue(ctx, accessScopeKey{}, scope)
	// Rebinding either principal or selected PM starts a new policy boundary.
	ctx = context.WithValue(ctx, denialRequestKey{}, struct{}{})
	ctx = context.WithValue(ctx, publicRefCacheKey{}, struct{}{})
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

var briefIndexHint = regexp.MustCompile(`(?i)\s+INDEXED\s+BY\s+idx_brief_[A-Za-z0-9_]+\b`)

var sqlIdentifiers = regexp.MustCompile(`[A-Za-z_][A-Za-z_0-9]*`)

// Include relation shadows actually named by this SQL statement. Matching all
// tokens (including quoted identifiers and literals) may include extra shadows,
// but never omits a named relation. Ownership recursion still uses canonical
// main tables and is evaluated within every statement's snapshot.
func accessCTEs(scope AccessScope, query string) string {
	return accessCTEsWithSnapshot(scope, query, nil)
}
func accessCTEsWithSnapshot(scope AccessScope, query string, snapshot *denialSnapshot) string {
	return accessSnapshotCTEs(scope, query, snapshot, false)
}
func accessSnapshotCTEs(scope AccessScope, query string, snapshot *denialSnapshot, pinned bool, pointReads ...bool) string {
	needed := map[string]bool{}
	for _, token := range sqlIdentifiers.FindAllString(query, -1) {
		needed[strings.ToLower(token)] = true
	}
	// Bounded internal edge probes name main.ref_edges deliberately; their
	// candidate event rows still use the scoped event relation. An unused edge
	// shadow would force workspace-sized denial/ref materialization unnecessarily.
	if needed["ref_edges"] && !resourceaccess.UsesShadowedSQLIdentifier(query, "ref_edges") {
		delete(needed, "ref_edges")
	}
	if needed["ask_delivery_attempts"] {
		needed["ask_deliveries"] = true
	}
	graph := ""
	pointPredicates := map[string]string{}

	denied := func(kind, id string) string {
		if snapshot != nil && snapshot.candidates {
			return candidateVisibleSQL(scope, "'"+kind+"'", id, snapshot)
		}
		predicate := "NOT EXISTS (SELECT 1 FROM _anx_denied WHERE kind='" + kind + "' AND id=" + id + ")"
		pointPredicates[predicate] = "anx_point_snapshot_denied((SELECT token FROM _anx_point_snapshot),'" + kind + "',CAST(" + id + " AS BLOB))=0"
		return predicate
	}
	cleanJSON := func(column string) string {
		// Keep equality and prose probes separate: an OR join scans every denied
		// spelling for every JSON atom, even when the atom contains no prose ref.
		// Buckets are conservative candidates; the full boundary matcher remains
		// authoritative for legacy punctuation, whitespace and Unicode identities.
		atoms := resourceaccess.ReferenceSQLAtoms(column, strings.HasSuffix(column, "_json") || column == "_row.body" || column == "_row.labels")
		// CASE skips atom extraction when the consuming statement's epoch-checked
		// denial relation is empty, including a cached empty request snapshot.
		canonical := "CASE WHEN EXISTS (SELECT 1 FROM _anx_denied) THEN NOT EXISTS (SELECT 1 FROM json_each(" + atoms + ") j WHERE EXISTS (SELECT 1 FROM _anx_denied_atoms d WHERE d.ref=j.value COLLATE NOCASE) OR EXISTS (SELECT 1 FROM json_each(anx_resource_mention_buckets(CAST(j.value AS BLOB))) b JOIN _anx_denied_prose d ON d.bucket=" + resourceaccess.MentionRefBucketSQL("b.value") + " WHERE " + resourceaccess.TextReferenceMatchSQL("j.value", "d.ref") + ")) ELSE 1 END"
		if snapshot != nil && snapshot.candidates {
			return "CASE WHEN (SELECT active FROM _anx_admission_ledger)=0 THEN 1 ELSE NOT EXISTS(SELECT 1 FROM json_each(" + atoms + ") j WHERE NOT (" + candidateCleanAtomSQL(scope, "j.value", snapshot) + ")) END"
		}
		return canonical
	}
	cleanRef := func(value string) string {
		canonical := `NOT EXISTS(SELECT 1 FROM _anx_denied_refs WHERE ref=` + value + `)`
		if snapshot != nil && snapshot.candidates {
			return `CASE WHEN (SELECT active FROM _anx_admission_ledger)=0 OR ` + value + ` IS NULL OR ` + value + `='' THEN 1 ELSE ` + candidateCleanRefSQL(scope, value, snapshot) + ` END`
		}
		return canonical
	}
	addColumns := func(table, columns, where string) {
		if snapshot != nil && snapshot.mutation {
			// A mutable transaction admits each operation afresh. If a caller's
			// concurrent operation changes it during preparation, hide the entire
			// relation rather than treating an empty denial set as a read grant.
			where = "COALESCE((SELECT version FROM main.resource_access_epoch WHERE singleton=1),-1)=" + fmt.Sprint(snapshot.epoch) + " AND (" + where + ")"
		}
		if !needed[table] {
			return
		}
		materialization := ""
		if snapshot != nil && snapshot.candidates || table == "document_search_postings" || table == "series_points" || strings.HasPrefix(table, "brief_") || table == "attention_placements" {
			// Repeated endpoint and projection probes must retain indexed seeks
			// through the privacy predicate. Materializing these shadows would
			// copy workspace relations before candidate selection.
			materialization = "NOT MATERIALIZED "
		}
		graph += ", " + table + " AS " + materialization + "(SELECT " + columns + " FROM main." + table + " AS _row WHERE " + where + ")"
	}
	add := func(table, where string) { addColumns(table, "*", where) }
	for _, kind := range []string{"thread", "board", "card", "topic", "document", "event", "artifact"} {
		add(resourceTables[kind], denied(kind, "_row.id"))
	}
	add("document_revisions", denied("document", "_row.document_id")+" AND "+denied("document_revision", "_row.revision_id"))
	add("card_revisions", denied("card", "_row.card_id")+" AND "+denied("card_revision", "_row.revision_id"))
	aliasVisible := `NOT EXISTS(SELECT 1 FROM _anx_denied WHERE kind=_row.resource_type AND id=_row.resource_id)`
	if snapshot != nil && snapshot.candidates {
		aliasVisible = candidateVisibleSQL(scope, "_row.resource_type", "_row.resource_id", snapshot)
	}
	add("resource_handle_aliases", cleanJSON("_row.reason")+" AND "+aliasVisible)
	// Separate equality probes let SQLite index the materialized reference set.
	// An OR here scans every denied spelling for every candidate placement.
	add("ref_edges", cleanJSON("_row.metadata_json")+" AND "+cleanRef("(_row.source_type||':'||_row.source_id)")+" AND "+cleanRef("(_row.target_type||':'||_row.target_id)"))
	add("resource_access_external_edges", "NOT EXISTS (SELECT 1 FROM _anx_denied WHERE kind=_row.source_kind AND id=_row.source_id)")
	// Filter the whole projection by card visibility, including routing hints,
	// promises and evidence times. A caller need not remember a cards join.
	// Receipt aggregates additionally inherit every contributing event.
	if needed["brief_initiatives"] {
		visible := denied("brief_loop", "'loop-facts/'||_row.card_id")
		columns := "plan_provenance_id,plan_provenance_json,progress_done,progress_total,card_id,open,promise_at,promise_what,promise_source,promise_source_ref,promise_author,promise_moves,last_evidence_at,step_status_sig,step_change_at,computed_at,loop_annotated,loop_annotated_at,loop_last_run_at,CASE WHEN " + visible + " THEN loop_facts_json END loop_facts_json,CASE WHEN " + visible + " THEN loop_valid_from END loop_valid_from,CASE WHEN " + visible + " THEN loop_valid_until END loop_valid_until"
		addColumns("brief_initiatives", columns, denied("card", "_row.card_id")+" AND "+denied("plan", "_row.card_id")+" AND "+denied("brief_progress", "'plan-facts/'||_row.card_id"))
	}
	// Placements are principal-scoped canonical records. Dedicated admin reads
	// may inspect another recipient, while authored
	// reason/subject references still obey the ordinary resource policy.
	if needed["attention_placements"] {
		recipient := "_row.for_actor='" + strings.ReplaceAll(scope.ActorID, "'", "''") + "'"
		if scope.AuthAdmin {
			recipient = "1=1"
		}
		add("attention_placements", "("+recipient+") AND "+denied("filter/attention_placement", "_row.record_id"))
	}
	add("brief_notes", denied("card", "_row.card_id")+" AND "+denied("event", "_row.event_id"))
	add("brief_asks", denied("event", "_row.ask_id"))
	add("brief_ask_blocks", denied("event", "_row.ask_id"))
	add("brief_dirty", denied("card", "_row.card_id"))
	add("brief_plan_refs", denied("card", "_row.card_id")+" AND "+denied("plan", "_row.card_id"))
	add("brief_loop_facts", denied("card", "_row.card_id")+" AND "+denied("brief_loop", "'loop-facts/'||_row.card_id"))

	add("work_metadata", denied("card", "_row.card_id"))
	add("work_observations", denied("card", "_row.card_id")+" AND "+denied("work_observation", "_row.id"))
	add("work_evidence_records", denied("card", "_row.card_id")+" AND "+denied("work_evidence_record", "_row.id"))
	add("work_evidence_index", denied("card", "_row.card_id")+" AND "+denied("work_evidence_alias", "_row.id")+" AND "+denied("work_evidence_record", "_row.evidence_id"))
	add("work_evidence_entries", denied("card", "_row.card_id"))
	add("work_evidence_keys", denied("card", "_row.card_id")+" AND "+denied("work_evidence_record", "_row.evidence_id"))
	add("work_participants", denied("card", "_row.card_id")+" AND "+denied("participant", "_row.id"))
	add("card_plans", denied("plan", "_row.card_id"))
	add("agent_wakeups", denied("wakeup", "_row.wakeup_id"))
	add("ask_subscriptions", "(_row.ask_id='' OR "+denied("event", "_row.ask_id")+") AND "+denied("filter/ask_subscriptions", "_row.id"))
	add("ask_deliveries", denied("event", "_row.ask_id")+" AND "+denied("filter/ask_deliveries", "_row.id"))
	add("ask_delivery_attempts", "EXISTS (SELECT 1 FROM ask_deliveries d WHERE d.id=_row.delivery_id)")
	add("ask_subject_close_queue", denied("card", "_row.card_id"))
	add("ask_subjects", denied("event", "_row.ask_id")+" AND "+denied("card", "_row.card_id"))
	add("access_requests", denied("event", "_row.request_event_id")+" AND "+cleanJSON("_row.reason"))
	add("human_attention_response_claims", denied("event", "_row.request_event_id")+" AND "+denied("event", "_row.response_event_id")+" AND "+cleanJSON("_row.response_json"))
	// Batch claims carry their member ask ids inside response_json
	// ($.answers[*].ask_ref, "event:<id>"), so the member probe walks the JSON
	// array: a batch answered behind a private thread must look absent, not
	// merely redacted, to a principal who cannot see its members.
	add("human_attention_batch_response_claims", denied("inbox", "_row.inbox_item_id")+" AND "+cleanJSON("_row.response_json")+` AND NOT EXISTS (
		SELECT 1 FROM json_each(_row.response_json,'$.answers') AS ans
		WHERE EXISTS (
			SELECT 1 FROM _anx_denied d
			WHERE d.kind='event'
			  AND d.id=substr(COALESCE(json_extract(ans.value,'$.ask_ref'),''), 7)
		)
	)`)
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
	add("scope_inbox_live", denied("inbox", "_row.id"))
	// Search copies never confer authority. Bind every head and term to a live
	// canonical event and re-evaluate its inherited privacy in this snapshot.
	for _, table := range []string{"comment_search_heads", "comment_search_postings", "comment_search_pending_heads"} {
		add(table, denied("event", "_row.event_id")+` AND EXISTS (SELECT 1 FROM main.events e WHERE e.id=_row.event_id AND e.thread_id=_row.thread_id AND e.type='message_posted' AND COALESCE(trim(e.trashed_at),'')='')`)
	}
	add("comment_search_threads", denied("thread", "_row.thread_id"))
	add("document_search_heads", denied("document", "_row.document_id")+` AND EXISTS(SELECT 1 FROM main.documents d WHERE d.id=_row.document_id AND d.head_revision_id=_row.body_revision) AND _row.ready=1`)
	resourceAuthority := func(alias string) string {
		return `EXISTS(SELECT 1 FROM main.document_search_heads h JOIN main.documents d ON d.id=h.document_id WHERE h.n=` + alias + `.doc_n AND ` + denied("document", "d.id") + ` AND ((` + alias + `.kind='document' AND h.ready=1 AND h.body_revision=d.head_revision_id) OR (` + alias + `.kind='comment' AND ` + denied("event", alias+".resource_id") + ` AND EXISTS(SELECT 1 FROM main.events e WHERE e.id=` + alias + `.resource_id AND e.thread_id=d.thread_id AND e.type='message_posted' AND COALESCE(trim(e.trashed_at),'')=''))))`
	}
	add("document_search_resources", resourceAuthority("_row"))
	add("document_search_postings", `EXISTS(SELECT 1 FROM main.document_search_resources _resource WHERE _resource.n=_row.res_n AND `+resourceAuthority("_resource")+`)`)
	add("dashboard_pins", "_row.principal_id="+("'"+strings.ReplaceAll(scope.ActorID, "'", "''")+"'"))
	add("workspace_dashboard", denied("document", "_row.document_id"))
	add("idempotency_replays", cleanJSON("_row.response_json"))
	for _, source := range resourceaccess.FilterOwnershipSources() {
		if source.Table == "series_adapters" || source.Table == "ask_subscriptions" || source.Table == "ask_deliveries" {
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
			add(table, "CASE WHEN _row.labels='{}' THEN 1 ELSE ("+cleanJSON("_row.labels")+") END"+" AND NOT EXISTS (SELECT 1 FROM _anx_private_series p WHERE p.series=_row.series AND p.labels=_row.labels)")
		}
	}
	// An unused ownership graph does not filter any row, but SQLite still has
	// to parse it. Keep graph-only callers and explicit internal dependencies.
	if snapshot != nil && snapshot.candidates {
		needsCanonical := query == "" || strings.Contains(graph, "_anx_denied") || needed["_anx_denied"] || needed["_anx_denied_refs"] || needed["_anx_denied_atoms"] || needed["_anx_denied_prose"] || needed["_anx_resource_refs"]
		if graph == "" && !needsCanonical {
			return ""
		}
		if snapshot.candidateAllPublic && !snapshot.inventory && snapshot.pointToken > 0 && snapshot.epoch >= 0 {
			// One statement snapshot validates the empty-root proof for every
			// admitted identity. Keep both guards, without repeating their work
			// for each row in a hierarchy or summary join.
			graph = ", _anx_candidate_public_guard(valid) AS MATERIALIZED (SELECT CASE WHEN " + candidateProofGuardSQL(snapshot) + " THEN anx_candidate_lookup_public(" + fmt.Sprint(snapshot.pointToken) + ") END)" + graph
		}
		if !needsCanonical {
			return admissionLedgerCTE(scope) + graph
		}
		// Retain arbitrary text/ref projection checks lazily. Exact resource
		// shadows above do not depend on this closure, so an unmatched alias
		// branch cannot capture workspace authority before candidate admission.
		snapshot = nil
	}
	needsGraph := graph != "" || query == ""
	for name := range needed {
		needsGraph = needsGraph || strings.HasPrefix(name, "_anx_")
	}
	if !needsGraph {
		return ""
	}
	deniedGraph := ""
	if snapshot == nil {
		deniedGraph = ownershipClosure("_anx_denied", deniedRootSQL(scope), false)
		if strings.Contains(graph, "_anx_admission_ledger") {
			deniedGraph = admissionLedgerCTE(scope) + ", " + deniedGraph
		}
	}
	if snapshot != nil {
		epoch := fmt.Sprint(snapshot.epoch)
		current := "COALESCE((SELECT version FROM main.resource_access_epoch WHERE singleton=1),-1)"
		if snapshot.epochTable == "receipt_access_epoch" {
			current = "COALESCE((SELECT version FROM main.receipt_access_epoch WHERE singleton=1),-1)"
		}
		if pinned {
			// Admission established this epoch in the SAME read-only transaction.
			// SQLite cannot change it until rollback; ordinary reads retain fallback.
			deniedGraph = "_anx_denied(kind,id) AS MATERIALIZED (SELECT json_extract(value,'$[0]'),json_extract(value,'$[1]') FROM json_each(?) WHERE " + current + "=" + epoch + ")"
		} else {
			roots := "SELECT * FROM (" + deniedRootSQL(scope) + ") WHERE " + current + "<>" + epoch
			deniedGraph = ownershipClosure("_anx_fresh_denied", roots, false) + ", _anx_denied(kind,id) AS MATERIALIZED (SELECT kind,id FROM _anx_fresh_denied UNION SELECT json_extract(value,'$[0]'),json_extract(value,'$[1]') FROM json_each(?) WHERE " + current + "=" + epoch + ")"
		}
	}
	// Most indexed resource reads only need denied (kind,id) rows. Unused ref
	// and atom CTEs still duplicate the closure during SQLite preparation.
	needsRefs := query == "" || needed["_anx_resource_refs"] || needed["_anx_denied_refs"] || needed["_anx_denied_atoms"] || needed["_anx_denied_prose"] || strings.Contains(graph, "_anx_denied_refs") || strings.Contains(graph, "_anx_denied_atoms")
	if !needsRefs {
		if pinned && snapshot != nil && snapshot.pointToken > 0 && len(pointReads) > 0 && pointReads[0] {
			pointGraph := graph
			for predicate, lookup := range pointPredicates {
				pointGraph = strings.ReplaceAll(pointGraph, predicate, lookup)
			}
			// Arbitrary text/ref projections retain their complete SQL policy;
			// only exact identity membership shadows use the admitted map.
			if !strings.Contains(pointGraph, "_anx_denied") && !strings.Contains(query, "_anx_denied") {
				current := "COALESCE((SELECT version FROM main.resource_access_epoch WHERE singleton=1),-1)"
				return "_anx_point_snapshot(token) AS MATERIALIZED (SELECT ? WHERE " + current + "=" + fmt.Sprint(snapshot.epoch) + ")" + pointGraph
			}
		}
		return deniedGraph + graph
	}
	prose := ""
	if needed["_anx_denied_prose"] || strings.Contains(graph, "_anx_denied_prose") {
		prose = ", _anx_denied_prose(ref,bucket) AS MATERIALIZED (SELECT ref," + resourceaccess.MentionRefBucketSQL("ref") + " FROM _anx_denied_refs WHERE instr(ref,':')>0)"
	}
	// Pinned series reads use the same proof without decoding unrelated event
	// history. Arbitrary label JSON still falls back to the complete canonical
	// atom/prose predicates, lazily; empty labels need no reference traversal.
	// The unknown-ledger empty check is an unscoped physical first-row seek,
	// never a scoped existence probe that could walk hidden business rows.
	if pinned && snapshot != nil && snapshot.pointToken > 0 && len(pointReads) > 0 && pointReads[0] &&
		(needed["series_labels"] || needed["series_points"] || needed["series_daily"] || needed["series_live_daily"]) {
		current := "COALESCE((SELECT version FROM main.resource_access_epoch WHERE singleton=1),-1)"
		token := "(SELECT token FROM _anx_point_snapshot)"
		deniedGraph = "_anx_point_snapshot(token) AS MATERIALIZED (SELECT ? WHERE " + current + "=" + fmt.Sprint(snapshot.epoch) + "), _anx_denied(kind,id) AS MATERIALIZED (SELECT json_extract(value,'$[0]'),json_extract(value,'$[1]') FROM json_each(anx_point_snapshot_rows(" + token + ",'all')))"
		graph = strings.ReplaceAll(graph, "NOT EXISTS (SELECT 1 FROM _anx_private_series p WHERE p.series=_row.series AND p.labels=_row.labels)", "((SELECT anx_point_snapshot_rows("+token+",'series-count'))=0 OR anx_point_snapshot_denied("+token+",'series',CAST(json_array(_row.series,_row.labels) AS BLOB))=0) AND CASE WHEN NOT EXISTS (SELECT 1 FROM main.resource_access_series_unknown LIMIT 1) OR (SELECT anx_point_snapshot_rows("+token+",'any'))=0 THEN 1 ELSE NOT EXISTS (SELECT 1 FROM main.resource_access_series_unknown u WHERE u.series=_row.series AND u.labels=_row.labels) END")
		graph = strings.ReplaceAll(graph, "EXISTS (SELECT 1 FROM _anx_denied)", "(SELECT anx_point_snapshot_rows("+token+",'any'))>0")
		for predicate, lookup := range pointPredicates {
			graph = strings.ReplaceAll(graph, predicate, lookup)
		}
	}
	return deniedGraph +
		", " + ownershipRefs("_anx_resource_refs", "_anx_denied") +
		// One reference to the identities CTE avoids copying the recursive graph
		// again during SQLite preparation just to emit the document synonym.
		", _anx_denied_refs(ref) AS MATERIALIZED (SELECT DISTINCT j.value FROM _anx_resource_refs r JOIN json_each(json_array(CASE WHEN r.kind='external_key' OR r.kind='card' AND (r.ref LIKE 'http://%' OR r.ref LIKE 'https://%') THEN r.ref ELSE r.kind||':'||r.ref END,CASE WHEN r.kind='document' THEN 'doc:'||r.ref END)) j WHERE j.value IS NOT NULL), _anx_denied_atoms(ref,typed) AS MATERIALIZED (SELECT ref,1 FROM _anx_denied_refs UNION SELECT id,0 FROM _anx_denied WHERE kind NOT IN ('brief_loop','plan','work_evidence_record','work_evidence_alias','external_key') AND kind NOT LIKE 'filter/%')" + prose + graph
}

// Apply relation visibility before limits, aggregates and cursors. SQLite
// evaluates ownership in every statement snapshot, including each SSE poll.
// Mutation SQL is never rewritten: mutations must load/authorize existing
// targets and destinations through these same relations before writing.
func scopeRead(ctx context.Context, query string) string {
	return scopeReadWithSnapshot(ctx, query, nil)
}

func scopeReadWithSnapshot(ctx context.Context, query string, snapshot *denialSnapshot) string {
	return scopeReadSnapshot(ctx, query, snapshot, false)
}
func scopeReadSnapshot(ctx context.Context, query string, snapshot *denialSnapshot, pinned bool) string {
	q, _ := scopedReadSnapshotQuery(ctx, query, snapshot, pinned)
	return q
}

// Return whether the rewrite needs the snapshot binding along with the SQL.
// Callers must not build and discard a second full ownership CTE just to find
// out whether the first rewrite scoped a relation.
func scopedReadSnapshotQuery(ctx context.Context, query string, snapshot *denialSnapshot, pinned bool) (string, bool) {
	scope, scoped := accessScopeFrom(ctx)
	if !scoped {
		return query, false
	}
	q := briefIndexHint.ReplaceAllString(strings.TrimSpace(query), "")
	// Scoped relation shadows have no named physical indexes. The optimizer
	// still pushes candidate keys into the indexed canonical relations.
	for _, index := range []string{"idx_work_metadata_project", "idx_work_source_url", "idx_work_evidence_lookup", "idx_work_evidence_public_lookup", "idx_cards_handle_unique", "sqlite_autoindex_cards_1"} {
		q = strings.ReplaceAll(q, " INDEXED BY "+index, "")
	}
	upper := strings.ToUpper(q)
	pointReads, _ := ctx.Value(summaryPointReadKey{}).(bool)
	graph := accessSnapshotCTEs(scope, q, snapshot, pinned, pointReads)
	prefix := "WITH RECURSIVE " + graph
	if fields := strings.Fields(upper); len(fields) > 0 {
		switch fields[0] {
		case "WITH":
			if graph == "" {
				return q, false
			}
			rest := strings.TrimSpace(q[len("WITH"):])
			if strings.HasPrefix(strings.ToUpper(rest), "RECURSIVE") {
				rest = strings.TrimSpace(rest[len("RECURSIVE"):])
			}
			return prefix + ", " + rest, true
		case "SELECT":
			if graph == "" {
				return q, false
			}
			return prefix + " " + q, true
		}
	}
	// Scoped reads must be SELECTs; new query forms require deliberate review.
	return "SELECT * FROM _anx_unsupported_scoped_read", graph != ""
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
	atoms := string(resourceaccess.ContentReferenceAtomsJSON(string(encoded), "structured"))
	if atoms == "[]" {
		return nil
	}
	if state, requested := ctx.Value(denialRequestKey{}).(*denialRequestState); !mutation && requested && state.candidateMode {
		var denied sql.NullBool
		if err := q.QueryRowContext(ctx, candidateValueCheckSQL(scope), atoms).Scan(&denied); err != nil {
			return err
		}
		if denied.Valid && denied.Bool {
			return ErrNotFound
		}
		if denied.Valid {
			return nil
		}
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
 SELECT 1 FROM json_each(anx_resource_json_refs(CAST(? AS BLOB))) j CROSS JOIN _anx_denied_atoms d
 WHERE j.value=d.ref COLLATE NOCASE
 UNION ALL
 SELECT 1 FROM json_each(anx_resource_json_refs(CAST(? AS BLOB))) j CROSS JOIN _anx_denied_atoms d
 WHERE ` + resourceaccess.TextReferenceCandidateSQL("j.value") + ` AND d.typed AND ` + resourceaccess.TextReferenceMatchSQL("j.value", "d.ref") + `)`
	// Separate exact equality from prose matching so SQLite can build an
	// automatic index on the materialized denied atoms instead of scanning
	// every denied identity for every scalar in the submitted snapshot.
	args := []any{string(encoded), string(encoded)}
	if !strings.Contains(atoms, `"$anx-ref-text$`) {
		query = exactValueCheckSQL()
		args = []any{atoms, atoms, atoms, atoms, atoms, atoms}
	}
	if mutation {
		if query, args, ok := responseMutationRead(ctx, q, query, args); ok {
			if err = q.QueryRowContext(ctx, query, args...).Scan(&denied); err != nil {
				return err
			}
			if denied {
				return ErrNotFound
			}
			return nil
		}
	}
	visit, _ := ctx.Value(overviewVisitValidationKey{}).(bool)
	useSnapshot := !mutation || visit && denialSnapshotFrom(ctx) != nil
	if policy, ok := resourceaccess.PolicyFrom(ctx); useSnapshot && ok && policy.ReadOnDB != nil {
		// Read selector checks share the request closure, still validating its
		// epoch inside this statement. The visit ledger also checks that epoch
		// in its own write transaction, falling back on any change or missing
		// epoch. Ordinary business writes use their current graph directly.
		query, args = policy.ReadOnDB(ctx, q, query, args)
	} else {
		query = `WITH RECURSIVE ` + accessCTEs(scope, query) + ` ` + query
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
