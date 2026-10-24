package storage

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"agent-nexus-core/internal/resourceaccess"
)

// Persist direct ownership edges, not principal-specific transitive closures.
// The reverse covering index lets admission start at the requested identity.
// Origins retain independent contributions so removing one spelling or source
// cannot remove a dependency still contributed by another canonical row.
func installAccessAdmission(ctx context.Context, tx *sql.Tx) error {
	// Historical schema-repair migrations can be replayed while their derived
	// admission tables remain. Rebuild only these derivable indexes atomically.
	rows, err := tx.QueryContext(ctx, `SELECT name FROM sqlite_schema WHERE type='trigger' AND name GLOB 'admission_*'`)
	if err != nil {
		return err
	}
	var triggers []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		triggers = append(triggers, name)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	for _, name := range triggers {
		if _, err := tx.ExecContext(ctx, `DROP TRIGGER "`+strings.ReplaceAll(name, `"`, `""`)+`"`); err != nil {
			return err
		}
	}
	for _, q := range []string{`DROP TABLE IF EXISTS resource_access_admission_compact_atoms`, `DROP TABLE IF EXISTS resource_access_admission_keys`, `DROP TABLE IF EXISTS resource_access_admission_links`} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	exec := func(q string) error {
		// An outer canonical UPSERT can override OR IGNORE inside a trigger.
		// Give each generated INSERT its own explicit conflict target behavior.
		parts := strings.Split(q, ";")
		for i, part := range parts {
			if strings.Contains(part, "INSERT OR IGNORE INTO resource_access_admission_") {
				parts[i] = strings.Replace(part, "INSERT OR IGNORE INTO", "INSERT INTO", 1) + " ON CONFLICT DO NOTHING"
			}
		}
		q = strings.Join(parts, ";")
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("authorization admission: %w", err)
		}
		return nil
	}
	for _, q := range []string{
		// Older workspaces install this empty capability after migrations.
		`CREATE TABLE IF NOT EXISTS resource_access_compact_edges(edge_id INTEGER PRIMARY KEY,target_key BLOB NOT NULL CHECK(typeof(target_key)='blob' AND length(target_key)=32))`,
		`CREATE INDEX IF NOT EXISTS idx_access_compact_target ON resource_access_compact_edges(target_key)`,
		`CREATE TABLE resource_access_admission_compact_atoms(edge_id INTEGER PRIMARY KEY,target_key BLOB NOT NULL)`,
		`CREATE INDEX idx_admission_compact_atom ON resource_access_admission_compact_atoms(target_key)`,
		`CREATE TABLE resource_access_admission_links(origin TEXT NOT NULL,origin_id TEXT NOT NULL,via_id INTEGER NOT NULL DEFAULT 0,parent_kind TEXT NOT NULL,parent_id TEXT NOT NULL,child_kind TEXT NOT NULL,child_id TEXT NOT NULL,ledger INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(origin,origin_id,via_id,parent_kind,parent_id,child_kind,child_id,ledger)) WITHOUT ROWID`,
		`CREATE INDEX idx_access_admission_child ON resource_access_admission_links(child_kind,child_id,ledger,parent_kind,parent_id)`,
		`CREATE INDEX idx_access_admission_parent ON resource_access_admission_links(parent_kind,parent_id,child_kind,child_id)`,
		`CREATE INDEX idx_access_admission_via ON resource_access_admission_links(origin,via_id)`,
		`CREATE TABLE resource_access_admission_keys(key_id INTEGER PRIMARY KEY,origin TEXT NOT NULL,origin_id TEXT NOT NULL,kind TEXT NOT NULL,id TEXT NOT NULL,ref TEXT NOT NULL,navigation INTEGER NOT NULL,atom BLOB NOT NULL,published INTEGER NOT NULL,UNIQUE(origin,origin_id,kind,id,ref,navigation,atom,published))`,
		`CREATE INDEX idx_access_admission_atom ON resource_access_admission_keys(atom,kind,id,published)`,
		`CREATE INDEX idx_access_admission_name ON resource_access_admission_keys(kind,ref COLLATE NOCASE,navigation,id)`,
	} {
		if err := exec(q); err != nil {
			return err
		}
	}
	links := `INSERT OR IGNORE INTO resource_access_admission_links(origin,origin_id,via_id,parent_kind,parent_id,child_kind,child_id,ledger) `
	keys := `INSERT OR IGNORE INTO resource_access_admission_keys(origin,origin_id,kind,id,ref,navigation,atom,published) `
	virtualHistoryKeys := func(origin, originID, kind, id, from, condition string) string {
		// The canonical document_history -> document edge exists even after a
		// purge removed the live document. Its virtual document must still
		// resolve raw/typed exact references and navigational ref edges.
		where := kind + `='document_history' AND (` + condition + `)`
		return keys + `SELECT '` + origin + `',` + originID + `,'document',` + id + `,` + id + `,1,unhex(` + resourceaccess.AtomKeySQL(`'document:'||`+id) + `),0 FROM ` + from + `(SELECT 1) WHERE ` + where + `; ` +
			keys + `SELECT '` + origin + `',` + originID + `,'document',` + id + `,'',0,unhex(` + resourceaccess.AtomKeySQL(id) + `),0 FROM ` + from + `(SELECT 1) WHERE ` + where + `;`
	}
	valid := func(kind string) string {
		return kind + ` NOT LIKE 'filter/%' AND ` + kind + ` NOT IN ('work_evidence_record','work_evidence_alias','external_key','document_history')`
	}
	publication := `('work_metadata','work_observation','work_evidence_record','work_evidence_alias')`
	refOrigin := func(p string) string {
		return `CASE WHEN ` + p + `id IS NULL THEN 'tuple:'||json_array(` + p + `source_type,` + p + `source_id,` + p + `target_type,` + p + `target_id,` + p + `edge_type) ELSE 'id:'||` + p + `id END`
	}
	keyLinks := links + `SELECT 'exact',e.edge_id,NEW.key_id,NEW.kind,NEW.id,e.source_kind,e.source_id,1 FROM resource_access_exact_edges e WHERE e.target_key=lower(hex(NEW.atom)) AND (NEW.published=0 OR e.source_kind NOT IN ` + publication + `); ` +
		links + `SELECT 'compact',e.rowid,NEW.key_id,NEW.kind,NEW.id,e.source_kind,e.source_id,2 FROM resource_access_admission_compact_atoms c JOIN resource_access_edges e ON e.rowid=c.edge_id WHERE c.target_key=NEW.atom AND (NEW.published=0 OR e.source_kind NOT IN ` + publication + `); ` +
		links + `SELECT 'external',e.rowid,NEW.key_id,NEW.kind,NEW.id,e.source_kind,e.source_id,0 FROM resource_access_external_edges e WHERE NEW.published=1 AND e.target_key=lower(hex(NEW.atom)); ` +
		links + `SELECT 'ref',` + refOrigin("e.") + `,NEW.key_id,NEW.kind,NEW.id,e.source_type,e.source_id,0 FROM ref_edges e INDEXED BY idx_ref_edges_access_cover WHERE NEW.navigation=1 AND e.target_type=NEW.kind AND e.target_id=NEW.ref COLLATE NOCASE AND e.edge_type='ref'; ` +
		links + `SELECT 'project',m.card_id,NEW.key_id,NEW.kind,NEW.id,'card',m.card_id,0 FROM work_metadata m WHERE NEW.kind='topic' AND NEW.navigation=1 AND ` + resourceaccess.ReferenceSQL(`json_extract(m.metadata_json,'$.project_ref')`) + `=('topic:'||NEW.ref) COLLATE NOCASE;`
	if err := exec(`CREATE TRIGGER admission_key_insert AFTER INSERT ON resource_access_admission_keys BEGIN ` + keyLinks + ` END`); err != nil {
		return err
	}
	if err := exec(`CREATE TRIGGER admission_key_delete AFTER DELETE ON resource_access_admission_keys BEGIN DELETE FROM resource_access_admission_links WHERE origin IN ('exact','compact','external','ref','project') AND via_id=OLD.key_id; END`); err != nil {
		return err
	}
	// Install one origin-scoped replacement trigger family and backfill through
	// precisely the same SELECT. All helpers receive only schema-owned SQL.
	install := func(name, table, id string, insert func(prefix, from string) string, clear string) error {
		if clear == "" {
			clear = `DELETE FROM resource_access_admission_links WHERE origin='` + name + `' AND origin_id=OLD.` + id + `;`
		}
		for _, q := range []string{
			`CREATE TRIGGER admission_` + name + `_insert AFTER INSERT ON ` + table + ` BEGIN ` + insert("NEW.", "") + ` END`,
			`CREATE TRIGGER admission_` + name + `_update AFTER UPDATE ON ` + table + ` BEGIN ` + clear + insert("NEW.", "") + ` END`,
			`CREATE TRIGGER admission_` + name + `_delete AFTER DELETE ON ` + table + ` BEGIN ` + clear + ` END`,
			insert("r.", table+" r, "),
		} {
			if err := exec(q); err != nil {
				return err
			}
		}
		return nil
	}
	// Structural edges are shared with the canonical forward oracle.
	byTable := map[string][][5]string{}
	for _, edge := range resourceaccess.StructuralOwnershipEdges {
		byTable[edge[2]] = append(byTable[edge[2]], edge)
	}
	var tables []string
	for table := range byTable {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	for _, table := range tables {
		id := "id"
		switch table {
		case "document_revisions", "card_revisions":
			id = "revision_id"
		case "card_plans", "work_metadata":
			id = "card_id"
		case "agent_wakeups":
			id = "wakeup_id"
		}
		edges := byTable[table]
		insert := func(p, from string) string {
			var out string
			for _, e := range edges {
				out += links + `SELECT 'struct_` + table + `',` + p + id + `,0,'` + e[0] + `',` + p + e[3] + `,'` + e[1] + `',` + p + e[4] + `,0 FROM ` + from + `(SELECT 1) WHERE ` + p + e[3] + ` IS NOT NULL AND COALESCE(` + p + e[4] + `,'')<>'';`
			}
			return out
		}
		if err := install("struct_"+table, table, id, insert, ""); err != nil {
			return err
		}
	}
	// Synthetic history vertices and comment contributors constrain documents.
	for _, spec := range []struct {
		name, table, id string
		insert          func(string, string) string
		clear           string
	}{
		{"history", "documents", "id", func(p, f string) string {
			return links + `SELECT 'history',` + p + `id,0,'document_history',` + p + `id,'document',` + p + `id,0 FROM ` + f + `(SELECT 1) WHERE true;`
		}, ""},
		{"comment", "events", "id", func(p, f string) string {
			return links + `SELECT 'comment',` + p + `id,0,'event',` + p + `id,'document',d.id,0 FROM ` + f + `documents d WHERE ` + p + `type='message_posted' AND d.thread_id=` + p + `thread_id AND COALESCE(` + p + `thread_id,'')<>'';`
		}, ""},
		{"document_comment", "documents", "id", func(p, f string) string {
			return links + `SELECT 'document_comment',` + p + `id,0,'event',e.id,'document',` + p + `id,0 FROM ` + f + `events e WHERE e.type='message_posted' AND e.thread_id=` + p + `thread_id AND COALESCE(` + p + `thread_id,'')<>'';`
		}, ""},
	} {
		if err := install(spec.name, spec.table, spec.id, spec.insert, spec.clear); err != nil {
			return err
		}
	}
	// A comment/document update also removes the contribution inserted when the
	// other side was created. Keep the surviving side's current relationship.
	for _, q := range []string{
		`CREATE TRIGGER admission_comment_other_update AFTER UPDATE OF thread_id,type,id ON events BEGIN DELETE FROM resource_access_admission_links WHERE origin='document_comment' AND parent_kind='event' AND parent_id=OLD.id; END`,
		`CREATE TRIGGER admission_comment_other_delete AFTER DELETE ON events BEGIN DELETE FROM resource_access_admission_links WHERE origin='document_comment' AND parent_kind='event' AND parent_id=OLD.id; END`,
		`CREATE TRIGGER admission_document_other_update AFTER UPDATE OF thread_id,id ON documents BEGIN DELETE FROM resource_access_admission_links WHERE origin='comment' AND child_kind='document' AND child_id=OLD.id; END`,
		`CREATE TRIGGER admission_document_other_delete AFTER DELETE ON documents BEGIN DELETE FROM resource_access_admission_links WHERE origin='comment' AND child_kind='document' AND child_id=OLD.id; END`,
	} {
		if err := exec(q); err != nil {
			return err
		}
	}
	// Live IDs, handles, historical aliases, virtual revision handles, tombstones.
	identityKeys := func(p, f string) string {
		return keys + `SELECT 'identity',` + p + `identity_id,` + p + `kind,` + p + `resource_id,` + p + `ref,1,unhex(` + resourceaccess.AtomKeySQL(p+`kind||':'||`+p+`ref`) + `),0 FROM ` + f + `(SELECT 1) WHERE ` + valid(p+"kind") + `; ` +
			keys + `SELECT 'identity',` + p + `identity_id,` + p + `kind,` + p + `resource_id,'',0,unhex(` + resourceaccess.AtomKeySQL(p+`resource_id`) + `),0 FROM ` + f + `(SELECT 1) WHERE ` + valid(p+"kind") + ` AND ` + p + `kind<>'plan' AND ` + p + `ref=` + p + `resource_id;` + virtualHistoryKeys("identity", p+"identity_id", p+"kind", p+"resource_id", f, p+"ref="+p+"resource_id")
	}
	if err := install("identity", "resource_access_identities", "identity_id", identityKeys, `DELETE FROM resource_access_admission_keys WHERE origin='identity' AND origin_id=OLD.identity_id;`); err != nil {
		return err
	}
	// Every graph vertex has raw/typed ID spellings even when it is synthetic
	// (series streams, PM envelopes, retained brief folds) or has no handle row.
	// Keep one contribution per source vertex, independently of atom count.
	for _, spec := range []struct{ table, kind, id string }{{"resource_access_edges", "source_kind", "source_id"}, {"ref_edges", "source_type", "source_id"}} {
		vertexID := func(p string) string { return `json_array(` + p + spec.kind + `,` + p + spec.id + `)` }
		insert := func(p, f string) string {
			return keys + `SELECT 'vertex',` + vertexID(p) + `,` + p + spec.kind + `,` + p + spec.id + `,'',0,unhex(` + resourceaccess.AtomKeySQL(p+spec.kind+`||':'||`+p+spec.id) + `),0 FROM ` + f + `(SELECT 1) WHERE ` + valid(p+spec.kind) + `; ` +
				keys + `SELECT 'vertex',` + vertexID(p) + `,` + p + spec.kind + `,` + p + spec.id + `,'',0,unhex(` + resourceaccess.AtomKeySQL(p+spec.id) + `),0 FROM ` + f + `(SELECT 1) WHERE ` + valid(p+spec.kind) + ` AND ` + p + spec.kind + `<>'plan';` + virtualHistoryKeys("vertex", vertexID(p), p+spec.kind, p+spec.id, f, "1")
		}
		clear := `DELETE FROM resource_access_admission_keys WHERE origin='vertex' AND origin_id=` + vertexID("OLD.") + ` AND NOT EXISTS(SELECT 1 FROM resource_access_edges WHERE source_kind=OLD.` + spec.kind + ` AND source_id=OLD.` + spec.id + `) AND NOT EXISTS(SELECT 1 FROM ref_edges WHERE source_type=OLD.` + spec.kind + ` AND source_id=OLD.` + spec.id + `);`
		for _, q := range []string{
			`CREATE TRIGGER admission_vertex_` + spec.table + `_insert AFTER INSERT ON ` + spec.table + ` BEGIN ` + insert("NEW.", "") + ` END`,
			`CREATE TRIGGER admission_vertex_` + spec.table + `_update AFTER UPDATE ON ` + spec.table + ` BEGIN ` + clear + insert("NEW.", "") + ` END`,
			`CREATE TRIGGER admission_vertex_` + spec.table + `_delete AFTER DELETE ON ` + spec.table + ` BEGIN ` + clear + ` END`,
			insert("r.", `(SELECT DISTINCT `+spec.kind+`,`+spec.id+` FROM `+spec.table+`) r, `),
		} {
			if err := exec(q); err != nil {
				return err
			}
		}
	}
	// Empty legacy handles are omitted from the mention identity table but are
	// still navigable by the canonical conservative empty-reference rule.
	for _, kind := range []string{"thread", "board", "card", "topic", "document", "event", "artifact"} {
		insert := func(p, f string) string {
			return keys + `SELECT 'empty_` + kind + `',` + p + `id,'` + kind + `',` + p + `id,'',1,unhex(` + resourceaccess.AtomKeySQL(`'`+kind+`:'`) + `),0 FROM ` + f + `(SELECT 1) WHERE ` + p + `handle='';`
		}
		if err := install("empty_"+kind, kind+"s", "id", insert, `DELETE FROM resource_access_admission_keys WHERE origin='empty_`+kind+`' AND origin_id=OLD.id;`); err != nil {
			return err
		}
	}
	// Index resolved prose directly; matching remains owned by the existing
	// atomic mention ledger (including identities introduced after the prose).
	mentionInsert := func(p, f string) string {
		return links + `SELECT 'mention',` + p + `edge_id,` + p + `identity_id,i.kind,i.resource_id,` + p + `source_kind,` + p + `source_id,0 FROM ` + f + `resource_access_identities i WHERE i.identity_id=` + p + `identity_id;`
	}
	if err := install("mention", "resource_access_mentions", "edge_id", mentionInsert, `DELETE FROM resource_access_admission_links WHERE origin='mention' AND origin_id=OLD.edge_id AND via_id=OLD.identity_id;`); err != nil {
		return err
	}
	// Exact ledgers stay independently selectable. A cutover or force-legacy
	// switch changes which indexed edges admission traverses in that statement.
	for _, compact := range []bool{false, true} {
		name, table, id, source, key, ledger := "exact", "resource_access_exact_edges", "edge_id", "", "unhex("+"$target_key)", "1"
		if compact {
			name, table, source, key, ledger = "compact", "resource_access_admission_compact_atoms", ` JOIN resource_access_edges e ON e.rowid=$edge_id `, "$target_key", "2"
		}
		insert := func(p, f string) string {
			replace := func(s string) string { return strings.ReplaceAll(s, "$", p) }
			kind, resourceID := p+"source_kind", p+"source_id"
			if compact {
				kind, resourceID = "e.source_kind", "e.source_id"
			}
			// FROM must have a left side for the compact ledger's canonical join.
			return links + `SELECT '` + name + `',` + p + id + `,k.key_id,k.kind,k.id,` + kind + `,` + resourceID + `,` + ledger + ` FROM ` + f + `(SELECT 1)` + replace(source) + ` JOIN resource_access_admission_keys k ON k.atom=` + replace(key) + ` WHERE k.published=0 OR ` + kind + ` NOT IN ` + publication + `;`
		}
		if err := install(name, table, id, insert, ""); err != nil {
			return err
		}
	}
	// The optional compact capability can be removed independently. Canonical
	// writes consult only our required atom index, never the optional table.
	if err := installAdmissionCompactSync(ctx, tx); err != nil {
		return err
	}
	// Raw source moves need not update the independently imported compact key.
	compactSource := links + `SELECT 'compact',NEW.rowid,k.key_id,k.kind,k.id,NEW.source_kind,NEW.source_id,2 FROM resource_access_admission_compact_atoms c JOIN resource_access_admission_keys k ON k.atom=c.target_key WHERE c.edge_id=NEW.rowid AND (k.published=0 OR NEW.source_kind NOT IN ` + publication + `);`
	for _, q := range []string{
		`CREATE TRIGGER admission_compact_source_insert AFTER INSERT ON resource_access_edges BEGIN DELETE FROM resource_access_admission_links WHERE origin='compact' AND origin_id=NEW.rowid; ` + compactSource + ` END`,
		`CREATE TRIGGER admission_compact_source_update AFTER UPDATE OF rowid,source_kind,source_id ON resource_access_edges BEGIN DELETE FROM resource_access_admission_links WHERE origin='compact' AND origin_id=OLD.rowid; ` + compactSource + ` END`,
		`CREATE TRIGGER admission_compact_source_delete AFTER DELETE ON resource_access_edges BEGIN DELETE FROM resource_access_admission_links WHERE origin='compact' AND origin_id=OLD.rowid; END`,
	} {
		if err := exec(q); err != nil {
			return err
		}
	}
	refInsert := func(p, f string) string {
		clear := ""
		if f == "" {
			clear = `DELETE FROM resource_access_admission_links WHERE origin='ref' AND origin_id=` + refOrigin(p) + `;`
		}
		return clear + links + `SELECT 'ref',` + refOrigin(p) + `,k.key_id,k.kind,k.id,` + p + `source_type,` + p + `source_id,0 FROM ` + f + `resource_access_admission_keys k WHERE k.navigation=1 AND k.kind=` + p + `target_type AND k.ref=` + p + `target_id COLLATE NOCASE AND ` + p + `edge_type='ref';`
	}
	if err := install("ref", "ref_edges", "id", refInsert, `DELETE FROM resource_access_admission_links WHERE origin='ref' AND origin_id=`+refOrigin("OLD.")+`;`); err != nil {
		return err
	}
	externalInsert := func(p, f string) string {
		return links + `SELECT 'external',` + p + `rowid,k.key_id,k.kind,k.id,` + p + `source_kind,` + p + `source_id,0 FROM ` + f + `resource_access_admission_keys k WHERE k.published=1 AND k.atom=unhex(` + p + `target_key);`
	}
	if err := install("external", "resource_access_external_edges", "rowid", externalInsert, ""); err != nil {
		return err
	}
	// Publication is an ownership identity, not a reference to another publisher.
	// Both current source URLs and retained purge URLs must remain resolvable.
	for _, spec := range []struct {
		name, table, id string
		insert          func(string, string) string
		clear           string
	}{
		{"evidence_key", "work_evidence_index", "id", func(p, f string) string {
			value := resourceaccess.TrimSpaceSQL(p + "lookup_key")
			return links + `SELECT 'evidence_key',` + p + `id,0,'card',` + p + `card_id,'external_key',` + value + `,0 FROM ` + f + `(SELECT 1) WHERE lower(` + resourceaccess.TrimSpaceSQL(`substr(`+p+`lookup_key,1,instr(`+p+`lookup_key,':')-1)`) + `) NOT IN ('card','doc','document','board','topic'); ` +
				keys + `SELECT 'evidence_key',` + p + `id,'external_key',` + value + `,'',0,unhex(` + resourceaccess.AtomKeySQL(value) + `),1 FROM ` + f + `(SELECT 1) WHERE lower(` + resourceaccess.TrimSpaceSQL(`substr(`+p+`lookup_key,1,instr(`+p+`lookup_key,':')-1)`) + `) NOT IN ('card','doc','document','board','topic');`
		}, `DELETE FROM resource_access_admission_links WHERE origin='evidence_key' AND origin_id=OLD.id; DELETE FROM resource_access_admission_keys WHERE origin='evidence_key' AND origin_id=OLD.id;`},
		{"source_url", "work_metadata", "card_id", func(p, f string) string {
			return keys + `SELECT 'source_url',` + p + `card_id,'card',` + p + `card_id,'',0,unhex(` + resourceaccess.AtomKeySQL(`json_extract(`+p+`metadata_json,'$.source.url')`) + `),1 FROM ` + f + `(SELECT 1) WHERE ` + p + `authority<>'nexus';`
		}, `DELETE FROM resource_access_admission_keys WHERE origin='source_url' AND origin_id=OLD.card_id;`},
		{"project", "work_metadata", "card_id", func(p, f string) string {
			return links + `SELECT 'project',` + p + `card_id,k.key_id,k.kind,k.id,'card',` + p + `card_id,0 FROM ` + f + `resource_access_admission_keys k WHERE k.kind='topic' AND k.navigation=1 AND ` + resourceaccess.ReferenceSQL(`json_extract(`+p+`metadata_json,'$.project_ref')`) + `=('topic:'||k.ref) COLLATE NOCASE;`
		}, ""},
	} {
		if err := install(spec.name, spec.table, spec.id, spec.insert, spec.clear); err != nil {
			return err
		}
	}
	// A tombstone's origin is its full composite key, independently of other
	// owners retaining the same spelling after a purge.
	for _, table := range []string{"resource_access_tombstones", "resource_handle_aliases"} {
		kind, id, ref, originID := "kind", "id", "ref", `json_array($kind,$id,$ref,$owner)`
		if table == "resource_handle_aliases" {
			kind, id, ref, originID = "resource_type", "resource_id", "alias_handle", `json_array($resource_type,$alias_handle)`
		}
		replace := func(s, p string) string { return strings.ReplaceAll(s, "$", p) }
		insert := func(p, f string) string {
			out := keys + `SELECT '` + table + `',` + replace(originID, p) + `,` + p + kind + `,` + p + id + `,'',1,unhex(` + resourceaccess.AtomKeySQL(p+kind+`||':'`) + `),0 FROM ` + f + `(SELECT 1) WHERE ` + p + ref + `='' AND ` + valid(p+kind) + `;`
			if table == "resource_access_tombstones" {
				out += keys + `SELECT '` + table + `',` + replace(originID, p) + `,` + p + kind + `,` + p + id + `,'',0,unhex(` + resourceaccess.AtomKeySQL(p+ref) + `),1 FROM ` + f + `(SELECT 1) WHERE ` + p + ref + ` LIKE 'http://%' OR ` + p + ref + ` LIKE 'https://%' OR ` + p + kind + `='external_key';`
			}
			return out
		}
		if err := install(table, table, "", insert, `DELETE FROM resource_access_admission_keys WHERE origin='`+table+`' AND origin_id=`+replace(originID, "OLD.")+`;`); err != nil {
			return err
		}
	}
	for _, install := range []func(context.Context, *sql.Tx) error{installAccessAdmissionRootCounts, installAccessAdmissionRoots, installAccessAdmissionGraph, installAccessAdmissionSpellings, installAccessAdmissionReach, installAccessAdmissionOwners, installWorkLifecycle, restrictAdmissionEvidenceWrites, restrictAdmissionUpdates} {
		if err := install(ctx, tx); err != nil {
			return err
		}
	}
	return nil
}

// These hooks belong to the optional table and disappear on operator teardown.
// Reinstalling the capability refreshes stale atoms once; normal startup does
// constant schema probes and never recopies the ledger.
func installAdmissionCompactSync(ctx context.Context, tx *sql.Tx) error {
	var installed, required bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM sqlite_schema WHERE name='resource_access_admission_compact_atoms'),EXISTS(SELECT 1 FROM sqlite_schema WHERE name='admission_compact_sync_insert')`).Scan(&required, &installed); err != nil {
		return err
	}
	if !required || installed {
		return nil
	}
	for _, q := range []string{
		`DELETE FROM resource_access_admission_compact_atoms`,
		`INSERT INTO resource_access_admission_compact_atoms SELECT edge_id,target_key FROM resource_access_compact_edges`,
		`CREATE TRIGGER admission_compact_sync_insert AFTER INSERT ON resource_access_compact_edges BEGIN INSERT INTO resource_access_admission_compact_atoms VALUES(NEW.edge_id,NEW.target_key) ON CONFLICT(edge_id) DO UPDATE SET target_key=excluded.target_key; END`,
		`CREATE TRIGGER admission_compact_sync_update AFTER UPDATE ON resource_access_compact_edges BEGIN DELETE FROM resource_access_admission_compact_atoms WHERE edge_id=OLD.edge_id; INSERT INTO resource_access_admission_compact_atoms VALUES(NEW.edge_id,NEW.target_key) ON CONFLICT(edge_id) DO UPDATE SET target_key=excluded.target_key; END`,
		`CREATE TRIGGER admission_compact_sync_delete AFTER DELETE ON resource_access_compact_edges BEGIN DELETE FROM resource_access_admission_compact_atoms WHERE edge_id=OLD.edge_id; END`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("compact admission sync: %w", err)
		}
	}
	return nil
}
