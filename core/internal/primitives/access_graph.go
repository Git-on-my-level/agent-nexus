package primitives

import "strings"

// Each recursive term probes indexed children of the current resource. Avoid a
// materialized union of every workspace record: unrelated public history must
// not increase authorization cost.
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
		{"card", "participant", "work_participants", "card_id", "id"},
	} {
		edge(e[0], e[1], e[2], e[3], e[4])
	}
	refs := func(join, ref string) {
		terms = append(terms, "SELECT e.source_type,e.source_id"+carry+" FROM "+name+" d "+join+" JOIN main.ref_edges e ON e.target_type=d.kind AND e.target_id="+ref+" COLLATE NOCASE AND e.edge_type='ref'")
	}
	refs("", "d.id")
	for _, kind := range []string{"thread", "board", "card", "topic", "document", "event", "artifact"} {
		refs("JOIN main."+resourceTables[kind]+" r ON d.kind='"+kind+"' AND r.id=d.id", "r.handle")
	}
	for _, kind := range []string{"card", "document"} {
		refs("JOIN main."+kind+"_revisions r ON d.kind='"+kind+"_revision' AND r.revision_id=d.id JOIN main."+resourceTables[kind]+" p ON p.id=r."+kind+"_id", "COALESCE(NULLIF(p.handle,''),p.id)||'-r'||r.revision_number")
	}
	refs("JOIN main.resource_handle_aliases r ON r.resource_type=d.kind AND r.resource_id=d.id", "r.alias_handle")
	refs("JOIN main.resource_access_tombstones r ON r.kind=d.kind AND r.id=d.id", "r.ref")
	for _, ref := range []string{"d.id", "r.handle"} {
		join := ""
		if ref == "r.handle" {
			join = "JOIN main.topics r ON r.id=d.id"
		}
		terms = append(terms, "SELECT 'card',m.card_id"+carry+" FROM "+name+" d "+join+" JOIN main.work_metadata m ON d.kind='topic' AND json_extract(m.metadata_json,'$.project_ref')=('topic:'||"+ref+") COLLATE NOCASE")
	}
	for _, alias := range []struct{ table, condition, column string }{
		{"resource_handle_aliases", "r.resource_type=d.kind AND r.resource_id=d.id", "alias_handle"},
		{"resource_access_tombstones", "r.kind=d.kind AND r.id=d.id", "ref"},
	} {
		terms = append(terms, "SELECT 'card',m.card_id"+carry+" FROM "+name+" d JOIN main."+alias.table+" r ON "+alias.condition+" JOIN main.work_metadata m ON d.kind='topic' AND json_extract(m.metadata_json,'$.project_ref')=('topic:'||r."+alias.column+") COLLATE NOCASE")
	}
	return name + "(" + columns + ") AS (" + strings.Join(terms, " UNION ") + ")"
}

// Resolve only the denied identities, including virtual revision handles and
// historical aliases. NULL/empty handles never become reference values.
func ownershipRefs(name, relation string) string {
	terms := []string{"SELECT d.kind,d.id,d.id AS ref FROM " + relation + " d"}
	for _, kind := range []string{"thread", "board", "card", "topic", "document", "event", "artifact"} {
		terms = append(terms, "SELECT d.kind,d.id,r.handle FROM "+relation+" d JOIN main."+resourceTables[kind]+" r ON d.kind='"+kind+"' AND r.id=d.id WHERE COALESCE(r.handle,'')<>''")
	}
	for _, kind := range []string{"card", "document"} {
		terms = append(terms, "SELECT d.kind,d.id,COALESCE(NULLIF(p.handle,''),p.id)||'-r'||r.revision_number FROM "+relation+" d JOIN main."+kind+"_revisions r ON d.kind='"+kind+"_revision' AND r.revision_id=d.id JOIN main."+resourceTables[kind]+" p ON p.id=r."+kind+"_id")
	}
	terms = append(terms, "SELECT d.kind,d.id,r.alias_handle FROM "+relation+" d JOIN main.resource_handle_aliases r ON r.resource_type=d.kind AND r.resource_id=d.id WHERE COALESCE(r.alias_handle,'')<>''", "SELECT d.kind,d.id,r.ref FROM "+relation+" d JOIN main.resource_access_tombstones r ON r.kind=d.kind AND r.id=d.id")
	return name + "(kind,id,ref) AS (" + strings.Join(terms, " UNION ") + ")"
}

func privateOwnershipGraph() string {
	return ownershipClosure("_anx_private", `SELECT 'thread',id,json_extract(body_json,'$.pm_actor_id') FROM main.threads WHERE COALESCE(json_extract(body_json,'$.pm_actor_id'),'')<>'' UNION SELECT kind,id,owner FROM main.resource_access_tombstones`, true)
}
