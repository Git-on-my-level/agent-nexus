package primitives

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"sort"
	"strings"
)

func dumpAccessPolicy(ctx context.Context, tx *sql.Tx, writer *accessEquivalenceWriter, tables map[string]bool) error {
	ledgerTable := "schema_migrations"
	if tables["scope_schema_migrations"] {
		ledgerTable = "scope_schema_migrations"
	}
	if tables[ledgerTable] {
		rows, err := tx.QueryContext(ctx, `SELECT version FROM main.`+ledgerTable+` ORDER BY version`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var version int64
			if err = rows.Scan(&version); err != nil {
				rows.Close()
				return err
			}
			if err = writer.write(accessPolicyRecord{
				Section: "p", Key: fmt.Sprintf("1|schema_migrations|%020d", version), Name: "schema_migrations",
				Value: map[string]any{"version": version}, Compare: "x",
			}); err != nil {
				rows.Close()
				return err
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	for _, table := range []string{"scope_format_state", "resource_access_compact_control", "resource_access_epoch"} {
		if !tables[table] {
			continue
		}
		if err := dumpAccessPolicyTable(ctx, tx, writer, table); err != nil {
			return err
		}
	}
	var userVersion int64
	if err := tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&userVersion); err != nil {
		return err
	}
	if err := writer.write(accessPolicyRecord{
		Section: "p", Key: "1|user_version", Name: "user_version",
		Value: map[string]any{"version": userVersion}, Compare: "x",
	}); err != nil {
		return err
	}
	var ledger, active int64
	query := `WITH RECURSIVE ` + admissionLedgerCTE(AccessScope{}) + ` SELECT ledger,active FROM _anx_admission_ledger`
	if err := tx.QueryRowContext(ctx, query).Scan(&ledger, &active); err != nil {
		return err
	}
	return writer.write(accessPolicyRecord{
		Section: "p", Key: "1|admission_ledger", Name: "admission_ledger",
		Value: map[string]any{"ledger": ledger, "active": active},
	})
}

func dumpAccessPolicyTable(ctx context.Context, tx *sql.Tx, writer *accessEquivalenceWriter, table string) error {
	columns, err := accessTableColumns(ctx, tx, table)
	if err != nil {
		return err
	}
	selects := make([]string, 0, len(columns)*2)
	for _, column := range columns {
		selects = append(selects, "typeof("+column+")", column)
	}
	rows, err := tx.QueryContext(ctx, `SELECT `+strings.Join(selects, ",")+` FROM main.`+table+` ORDER BY `+strings.Join(columns, ","))
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		values, err := scanAccessTypedRow(rows, len(columns))
		if err != nil {
			return err
		}
		encoded, _ := json.Marshal(values)
		if err = writer.write(accessPolicyRecord{
			Section: "p", Key: "1|" + table + "|" + string(encoded), Name: table,
			Value: values, Compare: "x",
		}); err != nil {
			return err
		}
	}
	return rows.Err()
}

func accessTableColumns(ctx context.Context, tx *sql.Tx, table string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(`+table+`)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var columns []string
	for rows.Next() {
		var cid int
		var name, kind string
		var notNull, primaryKey int
		var defaultValue any
		if err = rows.Scan(&cid, &name, &kind, &notNull, &defaultValue, &primaryKey); err != nil {
			return nil, err
		}
		columns = append(columns, name)
	}
	return columns, rows.Err()
}

func scanAccessTypedRow(rows *sql.Rows, columns int) ([]accessTypedValue, error) {
	raw := make([]any, columns*2)
	dest := make([]any, columns*2)
	for i := range raw {
		dest[i] = &raw[i]
	}
	if err := rows.Scan(dest...); err != nil {
		return nil, err
	}
	values := make([]accessTypedValue, columns)
	for i := range values {
		values[i] = accessEncodeValue(raw[i*2+1])
	}
	return values, nil
}

func dumpAccessInputs(ctx context.Context, tx *sql.Tx, writer *accessEquivalenceWriter, tables map[string]bool) error {
	ledger := admissionLedgerCTE(AccessScope{})
	if tables["resource_access_admission_owners"] {
		countQuery := `WITH RECURSIVE ` + ledger + ` SELECT count(*) FROM main.resource_access_admission_owners WHERE ledger=(SELECT ledger FROM _anx_admission_ledger)`
		rowQuery := `WITH RECURSIVE ` + ledger + ` SELECT typeof(kind),kind,typeof(id),id,typeof(unconditional),unconditional,typeof(owner),owner
 FROM main.resource_access_admission_owners WHERE ledger=(SELECT ledger FROM _anx_admission_ledger)
 ORDER BY kind,id,unconditional,owner`
		scan := func(rows *sql.Rows) (string, any, error) {
			var kindType, kind, idType, id, unconditionalType, ownerType string
			var unconditional int64
			var owner any
			if err := rows.Scan(&kindType, &kind, &idType, &id, &unconditionalType, &unconditional, &ownerType, &owner); err != nil {
				return "", nil, err
			}
			ownerValue := accessEncodeValue(owner)
			key := fmt.Sprintf("2|owners|%s|%s|%d|%s", kind, id, unconditional, fmt.Sprint(ownerValue.Value))
			return key, map[string]any{"kind": kind, "id": id, "unconditional": unconditional, "owner": ownerValue}, nil
		}
		if err := emitAccessInputSection(ctx, tx, writer, "owners", countQuery, rowQuery, scan); err != nil {
			return err
		}
	}
	if tables["resource_access_admission_prose_prefixes"] {
		countQuery := `SELECT count(*) FROM main.resource_access_admission_prose_prefixes`
		rowQuery := `SELECT hex(atom),spellings FROM main.resource_access_admission_prose_prefixes ORDER BY atom`
		scan := func(rows *sql.Rows) (string, any, error) {
			var atom string
			var spellings int64
			if err := rows.Scan(&atom, &spellings); err != nil {
				return "", nil, err
			}
			return "2|prose_prefixes|" + atom, map[string]any{"atom": accessHexBlob(atom), "spellings": spellings}, nil
		}
		if err := emitAccessInputSection(ctx, tx, writer, "prose_prefixes", countQuery, rowQuery, scan); err != nil {
			return err
		}
	}
	if tables["resource_access_admission_root_owners"] {
		countQuery := `SELECT count(*) FROM main.resource_access_admission_root_owners`
		rowQuery := `SELECT typeof(unconditional),unconditional,typeof(owner),owner FROM main.resource_access_admission_root_owners ORDER BY unconditional,owner`
		scan := func(rows *sql.Rows) (string, any, error) {
			var unconditionalType, ownerType string
			var unconditional int64
			var owner any
			if err := rows.Scan(&unconditionalType, &unconditional, &ownerType, &owner); err != nil {
				return "", nil, err
			}
			ownerValue := accessEncodeValue(owner)
			key := fmt.Sprintf("2|root_owners|%d|%s", unconditional, fmt.Sprint(ownerValue.Value))
			return key, map[string]any{"unconditional": unconditional, "owner": ownerValue}, nil
		}
		if err := emitAccessInputSection(ctx, tx, writer, "root_owners", countQuery, rowQuery, scan); err != nil {
			return err
		}
	}
	if tables["resource_access_admission_spellings"] {
		countQuery := `SELECT count(*) FROM main.resource_access_admission_spellings`
		rowQuery := `SELECT origin,origin_id,kind,id,ref,is_ref,hex(atom),hex(prose_atom) FROM main.resource_access_admission_spellings ORDER BY origin,origin_id,kind,id,ref,is_ref`
		scan := func(rows *sql.Rows) (string, any, error) {
			var origin, originID, kind, id, ref, atom, proseAtom string
			var isRef int64
			if err := rows.Scan(&origin, &originID, &kind, &id, &ref, &isRef, &atom, &proseAtom); err != nil {
				return "", nil, err
			}
			key := fmt.Sprintf("2|spellings|%s|%s|%s|%s|%s|%d", origin, originID, kind, id, ref, isRef)
			return key, map[string]any{
				"origin": origin, "origin_id": originID, "kind": kind, "id": id, "ref": ref,
				"is_ref": isRef, "atom": accessHexBlob(atom), "prose_atom": accessHexBlob(proseAtom),
			}, nil
		}
		if err := emitAccessInputSection(ctx, tx, writer, "spellings", countQuery, rowQuery, scan); err != nil {
			return err
		}
	}
	if tables["work_lifecycle_positions"] {
		countQuery := `WITH RECURSIVE ` + ledger + ` SELECT count(*) FROM main.work_lifecycle_positions WHERE ledger=(SELECT ledger FROM _anx_admission_ledger)`
		rowQuery := `WITH RECURSIVE ` + ledger + ` SELECT typeof(id),id,typeof(availability),availability,typeof(owner),owner
 FROM main.work_lifecycle_positions WHERE ledger=(SELECT ledger FROM _anx_admission_ledger) ORDER BY id`
		scan := func(rows *sql.Rows) (string, any, error) {
			var idType, id, availabilityType, ownerType string
			var availability int64
			var owner any
			if err := rows.Scan(&idType, &id, &availabilityType, &availability, &ownerType, &owner); err != nil {
				return "", nil, err
			}
			return "2|work_lifecycle|" + id, map[string]any{"id": id, "availability": availability, "owner": accessEncodeValue(owner)}, nil
		}
		if err := emitAccessInputSection(ctx, tx, writer, "work_lifecycle", countQuery, rowQuery, scan); err != nil {
			return err
		}
	}
	return nil
}

// emitAccessInputSection writes a persisted-input section row by row, or a
// sorted digest plus row count when the section is above the row limit. The
// digest is over the same key and JSON value that a row record would contain,
// so a digest mismatch is a hard compare failure.
func emitAccessInputSection(ctx context.Context, tx *sql.Tx, writer *accessEquivalenceWriter, name, countQuery, rowQuery string, scan func(*sql.Rows) (string, any, error)) error {
	var count int64
	if err := tx.QueryRowContext(ctx, countQuery).Scan(&count); err != nil {
		return err
	}
	if count > accessEquivalenceInputRowLimit {
		rows, err := tx.QueryContext(ctx, rowQuery)
		if err != nil {
			return err
		}
		defer rows.Close()
		digest := sha256.New()
		var seen int64
		for rows.Next() {
			key, value, err := scan(rows)
			if err != nil {
				return err
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				return err
			}
			digest.Write([]byte(key))
			digest.Write([]byte{0})
			digest.Write(encoded)
			digest.Write([]byte{0xff})
			seen++
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if seen != count {
			return fmt.Errorf("input section %s changed during dump: %d != %d", name, seen, count)
		}
		return writer.write(accessInputRecord{
			Section: "i", Key: "2|~digest|" + name, Name: name,
			Value: map[string]any{"rows": count, "sha256": hex.EncodeToString(digest.Sum(nil))},
		})
	}
	rows, err := tx.QueryContext(ctx, rowQuery)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		key, value, err := scan(rows)
		if err != nil {
			return err
		}
		if err := writer.write(accessInputRecord{Section: "i", Key: key, Name: name, Value: value}); err != nil {
			return err
		}
	}
	return rows.Err()
}

type accessDiagnosticQuery struct {
	name  string
	query string
}

func dumpAccessDiagnostics(ctx context.Context, tx *sql.Tx, writer *accessEquivalenceWriter, tables map[string]bool) error {
	queries := []accessDiagnosticQuery{
		{"admission_graph", `SELECT child_kind,child_id,ledger,parent_kind,parent_id,contributions FROM main.resource_access_admission_graph ORDER BY child_kind,child_id,ledger,parent_kind,parent_id`},
		{"admission_links", `SELECT origin,origin_id,via_id,parent_kind,parent_id,child_kind,child_id,ledger FROM main.resource_access_admission_links ORDER BY origin,origin_id,via_id,parent_kind,parent_id,child_kind,child_id,ledger`},
		{"admission_owners", `SELECT ledger,kind,id,unconditional,typeof(owner),owner FROM main.resource_access_admission_owners ORDER BY ledger,kind,id,unconditional,owner`},
		{"admission_reach", `SELECT ledger,kind,id,root_kind,root_id FROM main.resource_access_admission_reach ORDER BY ledger,kind,id,root_kind,root_id`},
		{"admission_roots", `SELECT kind,id,unconditional,typeof(owner),owner,contributions FROM main.resource_access_admission_roots ORDER BY kind,id,unconditional,owner`},
		{"admission_spellings", `SELECT origin,origin_id,kind,id,ref,is_ref,hex(atom),hex(prose_atom) FROM main.resource_access_admission_spellings ORDER BY origin,origin_id,kind,id,ref,is_ref`},
		{"compact_edges", `SELECT edge_id,hex(target_key) FROM main.resource_access_compact_edges ORDER BY edge_id`},
		{"exact_edges", `SELECT edge_id,source_kind,source_id,hex(target_key) FROM main.resource_access_exact_edges ORDER BY edge_id`},
		{"identities", `SELECT identity_id,origin,origin_id,kind,resource_id,ref,bucket FROM main.resource_access_identities ORDER BY identity_id`},
		{"prose_prefixes", `SELECT hex(atom),spellings FROM main.resource_access_admission_prose_prefixes ORDER BY atom`},
		{"raw_edges", `SELECT rowid,source_kind,source_id,target_ref FROM main.resource_access_edges ORDER BY rowid`},
		{"schema_objects", `SELECT type,name,COALESCE(sql,'') FROM sqlite_schema WHERE type IN ('table','index','view','trigger') ORDER BY type,name`},
		{"schema_triggers", `SELECT name,COALESCE(sql,'') FROM sqlite_schema WHERE type='trigger' ORDER BY name`},
	}
	sort.Slice(queries, func(i, j int) bool { return queries[i].name < queries[j].name })
	for _, diagnostic := range queries {
		table := diagnosticTable(diagnostic.name)
		if table != "" && !tables[table] {
			continue
		}
		count, digest, err := hashAccessQuery(ctx, tx, diagnostic.query)
		if err != nil {
			return fmt.Errorf("diagnostic %s: %w", diagnostic.name, err)
		}
		if err = writer.write(accessDiagnosticRecord{
			Section: "g", Name: diagnostic.name,
			Value: map[string]any{"rows": count, "sha256": digest},
		}); err != nil {
			return err
		}
	}
	if tables["resource_access_compact_control"] {
		var generation int64
		if err := tx.QueryRowContext(ctx, `SELECT generation FROM main.resource_access_compact_control WHERE singleton=1`).Scan(&generation); err != nil {
			return err
		}
		if err := writer.write(accessDiagnosticRecord{Section: "g", Name: "compact_generation", Value: map[string]any{"generation": generation}}); err != nil {
			return err
		}
	}
	return nil
}

func diagnosticTable(name string) string {
	switch name {
	case "admission_graph":
		return "resource_access_admission_graph"
	case "admission_links":
		return "resource_access_admission_links"
	case "admission_owners":
		return "resource_access_admission_owners"
	case "admission_reach":
		return "resource_access_admission_reach"
	case "admission_roots":
		return "resource_access_admission_roots"
	case "admission_spellings":
		return "resource_access_admission_spellings"
	case "compact_edges":
		return "resource_access_compact_edges"
	case "exact_edges":
		return "resource_access_exact_edges"
	case "identities":
		return "resource_access_identities"
	case "prose_prefixes":
		return "resource_access_admission_prose_prefixes"
	case "raw_edges":
		return "resource_access_edges"
	}
	return ""
}

func hashAccessQuery(ctx context.Context, tx *sql.Tx, query string) (int64, string, error) {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return 0, "", err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return 0, "", err
	}
	digest := sha256.New()
	var count int64
	for rows.Next() {
		raw := make([]any, len(columns))
		dest := make([]any, len(columns))
		for i := range raw {
			dest[i] = &raw[i]
		}
		if err = rows.Scan(dest...); err != nil {
			return count, "", err
		}
		for _, value := range raw {
			writeAccessHashValue(digest, value)
		}
		digest.Write([]byte{0})
		count++
	}
	if err = rows.Err(); err != nil {
		return count, "", err
	}
	return count, hex.EncodeToString(digest.Sum(nil)), nil
}

func writeAccessHashValue(digest hash.Hash, value any) {
	switch typed := value.(type) {
	case nil:
		digest.Write([]byte{'n'})
	case int64:
		digest.Write([]byte{'i'})
		digest.Write([]byte(fmt.Sprintf("%d", typed)))
	case float64:
		digest.Write([]byte{'r'})
		digest.Write([]byte(fmt.Sprintf("%v", typed)))
	case string:
		digest.Write([]byte{'s'})
		digest.Write([]byte(typed))
	case []byte:
		digest.Write([]byte{'b'})
		digest.Write(typed)
	default:
		digest.Write([]byte{'?'})
		digest.Write([]byte(fmt.Sprint(typed)))
	}
	digest.Write([]byte{0xff})
}
