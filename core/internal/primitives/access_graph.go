package primitives

import (
	"agent-nexus-core/internal/resourceaccess"
	"strings"
)

// Each recursive term probes indexed children of the current resource. Avoid a
// materialized union of every workspace record: unrelated public history must
// not increase authorization cost. Fence shared authorization sets within each
// statement so SQLite does not repeatedly flatten their recursive consumers.
func ownershipClosure(name, roots string, owner bool) string {
	columns, carry := "kind,id", ""
	if owner {
		columns += ",owner"
		carry = ",d.owner"
	}
	terms := []string{roots}
	edge := func(parent, child, table, parentCol, childCol string) {
		terms = append(terms, "SELECT '"+child+"',r."+childCol+carry+" FROM "+name+" d JOIN main."+table+" r ON d.kind='"+parent+"' AND r."+parentCol+"=d.id WHERE COALESCE(r."+childCol+",'')<>''")
	}
	for _, e := range [][5]string{
		{"thread", "board", "boards", "thread_id", "id"}, {"board", "thread", "boards", "id", "thread_id"},
		{"board", "card", "cards", "board_id", "id"}, {"thread", "card", "cards", "thread_id", "id"},
		{"thread", "card", "cards", "parent_thread_id", "id"}, {"card", "thread", "cards", "id", "thread_id"},
		{"thread", "topic", "topics", "thread_id", "id"}, {"topic", "thread", "topics", "id", "thread_id"},
		{"thread", "document", "documents", "thread_id", "id"}, {"document", "thread", "documents", "id", "thread_id"},
		{"thread", "event", "events", "thread_id", "id"}, {"thread", "artifact", "artifacts", "thread_id", "id"},
		{"document", "document_revision", "document_revisions", "document_id", "revision_id"},
		{"document", "artifact", "document_revisions", "document_id", "artifact_id"},
		{"card", "card_revision", "card_revisions", "card_id", "revision_id"},
		{"card", "artifact", "card_revisions", "card_id", "artifact_id"},
		{"thread", "inbox", "derived_inbox_items", "thread_id", "id"},
		{"card", "inbox", "derived_inbox_items", "source_card_id", "id"},
		{"event", "inbox", "derived_inbox_items", "source_event_id", "id"},
		{"thread", "wakeup", "agent_wakeups", "thread_id", "wakeup_id"},
		{"event", "wakeup", "agent_wakeups", "trigger_event_id", "wakeup_id"},
		{"card", "plan", "card_plans", "card_id", "card_id"},
		{"card", "participant", "work_participants", "card_id", "id"},
		{"work_metadata", "card", "work_metadata", "card_id", "card_id"},
		{"work_observation", "card", "work_observations", "id", "card_id"},
		{"card", "work_observation", "work_observations", "card_id", "id"},
		{"card", "work_evidence_record", "work_evidence_records", "card_id", "id"},
		{"work_evidence_record", "card", "work_evidence_records", "id", "card_id"},
		{"card", "work_evidence_alias", "work_evidence_index", "card_id", "id"},
		{"work_evidence_alias", "card", "work_evidence_index", "id", "card_id"},
		{"work_evidence_record", "work_evidence_alias", "work_evidence_index", "evidence_id", "id"},
		{"artifact", "document_revision", "document_revisions", "artifact_id", "revision_id"},
		{"artifact", "card_revision", "card_revisions", "artifact_id", "revision_id"},
		{"document_revision", "document", "document_revisions", "revision_id", "document_id"},
		{"card_revision", "card", "card_revisions", "revision_id", "card_id"},
	} {
		edge(e[0], e[1], e[2], e[3], e[4])
	}
	// Published keys are ownership identities, not numeric projection IDs.
	// Keeping a graph vertex for each key also lets purge retain it in the
	// existing ownership tombstone ledger. Native refs keep native resolution.
	terms = append(terms, "SELECT 'external_key',"+resourceaccess.TrimSpaceSQL("k.lookup_key")+carry+" FROM "+name+" d JOIN main.work_evidence_index k ON d.kind='card' AND k.card_id=d.id WHERE lower("+resourceaccess.TrimSpaceSQL("substr(k.lookup_key,1,instr(k.lookup_key,':')-1)")+") NOT IN ('card','doc','document','board','topic')")
	// Publishing the same key is not a reference to another publisher: it must
	// not let hidden evidence suppress visible resolution candidates. Other
	// inventory fields (including plan and work refs) inherit the key's owner.
	terms = append(terms, "SELECT e.source_kind,e.source_id"+carry+" FROM "+name+" d JOIN main.resource_access_exact_edges e ON d.kind='external_key' AND e.target_key="+resourceaccess.AtomKeySQL("d.id")+" WHERE e.source_kind NOT IN ('work_metadata','work_observation','work_evidence_record','work_evidence_alias')")
	terms = append(terms, "SELECT e.source_kind,e.source_id"+carry+" FROM "+name+" d JOIN main.resource_access_external_edges e ON d.kind='external_key' AND e.target_key="+resourceaccess.AtomKeySQL("d.id")+"")
	// Document full-text materialization includes comment text. A private
	// contributor must constrain the document before MATCH/rank/limit are applied.
	terms = append(terms, "SELECT 'document',r.id"+carry+" FROM "+name+" d JOIN main.events e ON d.kind='event' AND e.id=d.id JOIN main.documents r ON r.thread_id=e.thread_id WHERE e.type='message_posted' AND COALESCE(e.thread_id,'')<>''")
	refs := func(join, ref string) {
		terms = append(terms, "SELECT e.source_type,e.source_id"+carry+" FROM "+name+" d "+join+" JOIN main.ref_edges e INDEXED BY idx_ref_edges_access_cover ON e.target_type=d.kind AND e.target_id="+ref+" COLLATE NOCASE AND e.edge_type='ref' WHERE d.kind NOT LIKE 'filter/%' AND d.kind NOT IN ('work_evidence_record','work_evidence_alias','external_key')")
		terms = append(terms, "SELECT e.source_kind,e.source_id"+carry+" FROM "+name+" d "+join+" JOIN main.resource_access_exact_edges e ON e.target_key="+resourceaccess.AtomKeySQL("(d.kind||':'||"+ref+")")+" WHERE d.kind NOT LIKE 'filter/%' AND d.kind NOT IN ('work_evidence_record','work_evidence_alias','external_key')")
	}
	refs("", "d.id")
	refs("JOIN main.runs r ON d.kind='run' AND r.id=d.id", "r.handle")
	terms = append(terms, "SELECT e.source_kind,e.source_id"+carry+" FROM "+name+" d JOIN main.resource_access_exact_edges e ON e.target_key="+resourceaccess.AtomKeySQL("d.id")+" WHERE d.kind NOT IN ('plan','work_evidence_record','work_evidence_alias','external_key') AND d.kind NOT LIKE 'filter/%'")
	// Legacy source URLs may exceed the bounded evidence-key projection. Keep
	// their indexed ownership edges while separating publication from reference.
	for _, e := range []struct{ table, where string }{
		{"resource_access_exact_edges", " AND e.source_kind NOT IN ('work_metadata','work_observation','work_evidence_record','work_evidence_alias')"},
		{"resource_access_external_edges", ""},
	} {
		terms = append(terms, "SELECT e.source_kind,e.source_id"+carry+" FROM "+name+" d JOIN main.work_metadata m ON d.kind='card' AND m.card_id=d.id JOIN main."+e.table+" e ON e.target_key="+resourceaccess.AtomKeySQL("json_extract(m.metadata_json,'$.source.url')")+" WHERE m.authority<>'nexus'"+e.where)
		terms = append(terms, "SELECT e.source_kind,e.source_id"+carry+" FROM "+name+" d JOIN main.resource_access_tombstones r ON r.kind=d.kind AND r.id=d.id JOIN main."+e.table+" e ON e.target_key="+resourceaccess.AtomKeySQL("r.ref")+" WHERE (r.ref LIKE 'http://%' OR r.ref LIKE 'https://%')"+e.where)
	}
	for _, kind := range []string{"thread", "board", "card", "topic", "document", "event", "artifact"} {
		refs("JOIN main."+resourceTables[kind]+" r ON d.kind='"+kind+"' AND r.id=d.id", "r.handle")
	}
	for _, kind := range []string{"card", "document"} {
		refs("JOIN main."+kind+"_revisions r ON d.kind='"+kind+"_revision' AND r.revision_id=d.id JOIN main."+resourceTables[kind]+" p ON p.id=r."+kind+"_id", "COALESCE(NULLIF(p.handle,''),p.id)||'-r'||r.revision_number")
	}
	refs("JOIN main.resource_handle_aliases r ON r.resource_type=d.kind AND r.resource_id=d.id", "r.alias_handle")
	refs("JOIN main.resource_access_tombstones r ON r.kind=d.kind AND r.id=d.id", "r.ref")
	// Prose ownership is resolved atomically at writes, including new/renamed
	// identities matching older text. Reads never inspect stored mention text.
	terms = append(terms, "SELECT m.source_kind,m.source_id"+carry+" FROM "+name+" d JOIN main.resource_access_identities i ON i.kind=d.kind AND i.resource_id=d.id JOIN main.resource_access_mentions m ON m.identity_id=i.identity_id")
	for _, ref := range []string{"d.id", "r.handle"} {
		join := ""
		if ref == "r.handle" {
			join = "JOIN main.topics r ON r.id=d.id"
		}
		terms = append(terms, "SELECT 'card',m.card_id"+carry+" FROM "+name+" d "+join+" JOIN main.work_metadata m ON d.kind='topic' AND "+resourceaccess.ReferenceSQL("json_extract(m.metadata_json,'$.project_ref')")+"=('topic:'||"+ref+") COLLATE NOCASE")
	}
	for _, alias := range []struct{ table, condition, column string }{
		{"resource_handle_aliases", "r.resource_type=d.kind AND r.resource_id=d.id", "alias_handle"},
		{"resource_access_tombstones", "r.kind=d.kind AND r.id=d.id", "ref"},
	} {
		terms = append(terms, "SELECT 'card',m.card_id"+carry+" FROM "+name+" d JOIN main."+alias.table+" r ON "+alias.condition+" JOIN main.work_metadata m ON d.kind='topic' AND "+resourceaccess.ReferenceSQL("json_extract(m.metadata_json,'$.project_ref')")+"=('topic:'||r."+alias.column+") COLLATE NOCASE")
	}
	return name + "(" + columns + ") AS MATERIALIZED (" + strings.Join(terms, " UNION ") + ")"
}

// Resolve only the denied identities, including virtual revision handles and
// historical aliases. NULL/empty handles never become reference values.
func ownershipRefs(name, relation string) string {
	// SQLite deep-copies a CTE at every FROM reference during preparation,
	// even when MATERIALIZED. Resolve identities through one indexed join rather
	// than a UNION arm per kind (each arm used to duplicate the entire closure).
	// Keep raw IDs for missing/derived resources and external card URLs, which
	// are not navigable identities in the mention index.
	return name + `(kind,id,ref) AS MATERIALIZED (
 SELECT DISTINCT d.kind,d.id,j.value FROM ` + relation + ` d
 LEFT JOIN main.resource_access_identities i ON i.kind=d.kind AND i.resource_id=d.id
 LEFT JOIN main.work_metadata m ON d.kind='card' AND m.card_id=d.id AND m.authority<>'nexus'
 JOIN json_each(json_array(d.id,i.ref,json_extract(m.metadata_json,'$.source.url'))) j
 WHERE d.kind NOT LIKE 'filter/%' AND d.kind NOT IN ('work_evidence_record','work_evidence_alias') AND j.value IS NOT NULL AND j.value<>'')`
}

func privateOwnershipGraph() string {
	return ownershipClosure("_anx_private", `SELECT 'thread',id,json_extract(body_json,'$.pm_actor_id') FROM main.threads WHERE COALESCE(json_extract(body_json,'$.pm_actor_id'),'')<>'' UNION SELECT kind,id,owner FROM main.resource_access_tombstones`, true)
}
