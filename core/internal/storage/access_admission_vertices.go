package storage

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"agent-nexus-core/internal/resourceaccess"
)

// Canonical identity belongs to a graph vertex, not to a live handle row or an
// outgoing atom. In particular, metadata/revision/plan/structural rows can make
// absent resources private. Every endpoint and root needs independent ID keys.
// Share one immutable ID mapping across canonical declarations. A key carries
// no authority; only current roots/reachability do. Retain the mapping when a
// row disappears, because the forward oracle resolves a denied vertex's ID
// independently of resource existence. Owner/count-only updates do no work.
// Untouched historical wakeups retain their lazy key path.
func installAdmissionCanonicalVertices(ctx context.Context, tx *sql.Tx) error {
	type source struct {
		table  string
		fields [][2]string
	}
	// Derive structural declarations from the same inventory as the forward
	// oracle. Do not discover identities by following key-generated graph rows:
	// that would create keys inside their own resolver trigger (SQLite suppresses
	// recursive invocations), making partial key insertion order significant.
	byTable := map[string][][2]string{
		"resource_access_edges":           {{"@source_kind", "@source_id"}},
		"resource_access_exact_edges":     {{"@source_kind", "@source_id"}},
		"resource_access_external_edges":  {{"@source_kind", "@source_id"}},
		"ref_edges":                       {{"@source_type", "@source_id"}},
		"resource_access_admission_roots": {{"@kind", "@id"}},
	}
	for _, edge := range resourceaccess.StructuralOwnershipEdges {
		for _, f := range [][2]string{{"'" + edge[0] + "'", "@" + edge[3]}, {"'" + edge[1] + "'", "@" + edge[4]}} {
			seen := false
			for _, prior := range byTable[edge[2]] {
				seen = seen || prior == f
			}
			if !seen {
				byTable[edge[2]] = append(byTable[edge[2]], f)
			}
		}
	}
	var tables []string
	for table := range byTable {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	var sources []source
	for _, table := range tables {
		sources = append(sources, source{table, byTable[table]})
	}
	expr := func(s, p string) string { return strings.ReplaceAll(s, "@", p) }

	valid := func(k string) string {
		return k + ` NOT LIKE 'filter/%' AND ` + k + ` NOT IN ('work_evidence_record','work_evidence_alias','external_key','document_history')`
	}
	key := func(k, id, originID, from, condition string) string {
		prefix := `INSERT INTO resource_access_admission_keys(origin,origin_id,kind,id,ref,navigation,atom,published) `
		typed := prefix + `SELECT 'vertex',` + originID + `,` + k + `,` + id + `,` + id + `,1,unhex(` + resourceaccess.AtomKeySQL(k+`||':'||`+id) + `),0 FROM ` + from + ` WHERE ` + id + ` IS NOT NULL AND ` + valid(k) + ` AND (` + condition + `) ON CONFLICT DO NOTHING;`
		raw := prefix + `SELECT 'vertex',` + originID + `,` + k + `,` + id + `,'',0,unhex(` + resourceaccess.AtomKeySQL(id) + `),0 FROM ` + from + ` WHERE ` + id + ` IS NOT NULL AND ` + valid(k) + ` AND ` + k + `<>'plan' AND (` + condition + `) ON CONFLICT DO NOTHING;`
		external := prefix + `SELECT 'vertex',` + originID + `,'external_key',` + id + `,'',0,unhex(` + resourceaccess.AtomKeySQL(id) + `),1 FROM ` + from + ` WHERE ` + id + ` IS NOT NULL AND ` + k + `='external_key' AND (` + condition + `) ON CONFLICT DO NOTHING;`
		return typed + raw + external
	}
	add := func(k, id, from string) string {
		originID := `json_array(` + k + `,` + id + `)`
		return key(k, id, originID, from, "1") + key("'document'", id, originID, from, k+`='document_history'`)
	}

	var backfill []string
	for _, s := range sources {
		var insert string
		var changed []string
		for _, f := range s.fields {
			insert += add(expr(f[0], "NEW."), expr(f[1], "NEW."), "(SELECT 1)")
			changed = append(changed, expr(f[0], "OLD.")+" IS NOT "+expr(f[0], "NEW."), expr(f[1], "OLD.")+" IS NOT "+expr(f[1], "NEW."))
			if s.table != "agent_wakeups" {
				backfill = append(backfill, `SELECT `+expr(f[0], "")+` kind,`+expr(f[1], "")+` id FROM `+s.table+` WHERE `+expr(f[1], "")+` IS NOT NULL`)
			}
		}
		for _, q := range []string{
			`CREATE TRIGGER admission_vertex_` + s.table + `_insert AFTER INSERT ON ` + s.table + ` BEGIN ` + insert + ` END`,
			`CREATE TRIGGER admission_vertex_` + s.table + `_update AFTER UPDATE ON ` + s.table + ` WHEN ` + strings.Join(changed, " OR ") + ` BEGIN ` + insert + ` END`,
		} {
			if _, err := tx.ExecContext(ctx, q); err != nil {
				return fmt.Errorf("canonical admission vertices: %w", err)
			}
		}
	}
	if _, err := tx.ExecContext(ctx, add("v.kind", "v.id", "("+strings.Join(backfill, " UNION ")+") v")); err != nil {
		return fmt.Errorf("canonical admission vertex backfill: %w", err)
	}
	return nil
}
