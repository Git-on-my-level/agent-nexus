package storage

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var admissionGenericUpdate = regexp.MustCompile(`(?i)\bAFTER UPDATE ON\b`)
var admissionRowIDInput = regexp.MustCompile(`(?i)\b(?:OLD|NEW)\.(?:rowid|_rowid_|oid)\b`)

// WHEN prevents execution, but SQLite still prepares the complete trigger
// program, including every nested cascade. UPDATE OF also excludes that program
// when the outer statement cannot change any input. Retain the existing WHEN
// and explicit UPDATE OF lists; this changes preparation, not matching policy.
func admissionNarrowUpdateProgram(name, original string, aliases ...string) string {
	// These canonical refresh signals deliberately reindex unchanged refs after
	// revision membership changes. Preserve their original event declarations.
	if name == "access_artifacts_update" || name == "access_document_revisions_update" {
		return original
	}
	at := strings.Index(original, " BEGIN ")
	if at < 0 || !strings.Contains(strings.ToUpper(original[:at]), " WHEN ") || !admissionGenericUpdate.MatchString(original[:at]) {
		return original
	}
	columns := map[string]bool{}
	for _, match := range admissionUpdateColumn.FindAllStringSubmatch(original, -1) {
		columns[strings.ToLower(match[1])] = true
	}
	if len(columns) == 0 {
		return original
	}
	// UPDATE OF compares the submitted spelling. INTEGER PRIMARY KEY and
	// rowid spellings can name the same input even when only one appears in
	// the trigger body. Keep those import/update spellings independently.
	for _, alias := range append([]string{"rowid", "_rowid_", "oid"}, aliases...) {
		columns[alias] = true
	}
	names := make([]string, 0, len(columns))
	for column := range columns {
		names = append(names, column)
	}
	sort.Strings(names)
	return admissionGenericUpdate.ReplaceAllString(original[:at], "AFTER UPDATE OF "+strings.Join(names, ",")+" ON") + original[at:]
}

func admissionUpdateProgramChanges(ctx context.Context, tx *sql.Tx) (map[string]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT name,tbl_name,sql FROM sqlite_schema WHERE type='trigger' AND (name GLOB 'admission_*' OR name GLOB 'mention_identity_*_update' OR name GLOB 'access_*_update') ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type trigger struct{ name, table, original string }
	var triggers []trigger
	for rows.Next() {
		var t trigger
		if err := rows.Scan(&t.name, &t.table, &t.original); err != nil {
			return nil, err
		}
		triggers = append(triggers, t)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	changes := map[string]string{}
	primaryAliases := map[string]string{}
	for _, t := range triggers {
		narrowed := admissionNarrowUpdateProgram(t.name, t.original)
		if narrowed == t.original {
			continue
		}
		if admissionRowIDInput.MatchString(t.original) {
			alias, known := primaryAliases[t.table]
			if !known {
				err := tx.QueryRowContext(ctx, `SELECT name FROM pragma_table_info(?) WHERE pk=1 AND upper(type)='INTEGER' AND (SELECT count(*) FROM pragma_table_info(?) WHERE pk<>0)=1`, t.table, t.table).Scan(&alias)
				if err != nil && err != sql.ErrNoRows {
					return nil, err
				}
				primaryAliases[t.table] = alias
			}
			if alias != "" {
				narrowed = admissionNarrowUpdateProgram(t.name, t.original, alias)
			}
		}
		changes[t.name] = narrowed
	}
	return changes, nil
}

func restrictAdmissionUpdatePrograms(ctx context.Context, tx *sql.Tx) error {
	changes, err := admissionUpdateProgramChanges(ctx, tx)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(changes))
	for name := range changes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, err := tx.ExecContext(ctx, `DROP TRIGGER "`+strings.ReplaceAll(name, `"`, `""`)+`"`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, changes[name]); err != nil {
			return fmt.Errorf("admission update program %s: %w", name, err)
		}
	}
	return nil
}

func verifyAdmissionUpdatePrograms(ctx context.Context, tx *sql.Tx) error {
	if err := verifyAdmissionMigration(ctx, tx); err != nil {
		return err
	}
	changes, err := admissionUpdateProgramChanges(ctx, tx)
	if err != nil {
		return err
	}
	if len(changes) != 0 {
		return fmt.Errorf("migration 117 admission update programs not narrowed: %d", len(changes))
	}
	return nil
}
