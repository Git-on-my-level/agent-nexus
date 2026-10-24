package storage

import (
	"agent-nexus-core/internal/resourceaccess"
	"context"
	"database/sql"
	"regexp"
	"sort"
	"strings"
)

var admissionUpdateColumn = regexp.MustCompile(`(?i)\b(?:OLD|NEW)\.([a-z_][a-z_0-9]*)`)
var admissionSourceReplacement = regexp.MustCompile(`(?s)DELETE FROM (resource_access_edges|resource_access_external_edges) WHERE source_kind='([^']*)' AND source_id=OLD\.([a-z_0-9]+);(INSERT INTO [a-z_]+\(source_kind,source_id,(target_ref|target_key)\) )(.*) ON CONFLICT\(source_kind,source_id,[a-z_]+\) DO NOTHING;`)
var admissionUpdateOf = regexp.MustCompile(`(?i)AFTER UPDATE OF ([a-z_0-9, ]+) ON`)

// Canonical identity updates used to delete/reinsert identical identities on
// every source UPDATE. Avoid that churn, and restrict our own trigger families
// to changed inputs. This changes no matching or ownership semantics.
func restrictAdmissionUpdates(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT name,sql FROM sqlite_schema WHERE type='trigger' AND (name GLOB 'admission_*_update' OR name GLOB 'mention_identity_*_update' OR name GLOB 'access_*_update')`)
	if err != nil {
		return err
	}
	type trigger struct{ name, sql string }
	updates := []trigger{}
	for rows.Next() {
		var t trigger
		if err := rows.Scan(&t.name, &t.sql); err != nil {
			rows.Close()
			return err
		}
		updates = append(updates, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, t := range updates {
		// Insert new atoms first and delete only removed atoms. Unchanged rowids
		// keep exact/compact keys, mentions and reachability contributions intact.
		t.sql = admissionSourceReplacement.ReplaceAllStringFunc(t.sql, func(body string) string {
			m := admissionSourceReplacement.FindStringSubmatch(body)
			return m[4] + m[6] + ` ON CONFLICT(source_kind,source_id,` + m[5] + `) DO NOTHING; DELETE FROM ` + m[1] + ` WHERE source_kind='` + m[2] + `' AND source_id=OLD.` + m[3] + ` AND (OLD.` + m[3] + ` IS NOT NEW.` + m[3] + ` OR ` + m[5] + ` NOT IN (WITH proposed(kind,id,ref) AS (` + m[6] + `) SELECT ref FROM proposed));`
		})
		at := strings.Index(t.sql, " BEGIN ")
		if at < 0 || strings.Contains(t.sql[:at], " WHEN ") {
			continue
		}
		cols := map[string]bool{}
		for _, m := range admissionUpdateColumn.FindAllStringSubmatch(t.sql, -1) {
			cols[m[1]] = true
		}
		if m := admissionUpdateOf.FindStringSubmatch(t.sql); len(m) > 1 {
			for _, c := range strings.Split(m[1], ",") {
				cols[strings.TrimSpace(c)] = true
			}
		}
		names := []string{}
		for c := range cols {
			names = append(names, c)
		}
		sort.Strings(names)
		terms := []string{}
		for _, c := range names {
			terms = append(terms, "OLD."+c+" IS NOT NEW."+c)
		}
		if len(terms) == 0 {
			continue
		}
		condition := strings.Join(terms, " OR ")
		switch t.name {
		case "access_artifacts_update", "access_document_revisions_update":
			// History transfer/detach explicitly reindexes unchanged refs after
			// revision membership changes. Keep that canonical refresh signal.
			condition = "1"
		case "admission_source_url_update", "admission_spelling_work_metadata_update":
			condition = `OLD.card_id IS NOT NEW.card_id OR (OLD.authority<>'nexus') IS NOT (NEW.authority<>'nexus') OR json_extract(OLD.metadata_json,'$.source.url') IS NOT json_extract(NEW.metadata_json,'$.source.url')`
		case "admission_project_update":
			condition = `OLD.card_id IS NOT NEW.card_id OR ` + resourceaccess.ReferenceSQL(`json_extract(OLD.metadata_json,'$.project_ref')`) + ` IS NOT ` + resourceaccess.ReferenceSQL(`json_extract(NEW.metadata_json,'$.project_ref')`)
		}
		sql := t.sql[:at] + " WHEN " + condition + t.sql[at:]
		if _, err := tx.ExecContext(ctx, `DROP TRIGGER "`+t.name+`"`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, sql); err != nil {
			return err
		}
	}
	return nil
}

// Keep stable evidence IDs and publication keys when unrelated metadata or an
// evidence description changes. Replacing the entire card's evidence used to
// churn identical authorization contributions, including their reachability.
func restrictAdmissionEvidenceWrites(ctx context.Context, tx *sql.Tx) error {
	for _, op := range []string{"insert", "update"} {
		event, old := "INSERT", ""
		if op == "update" {
			event = "UPDATE OF card_id,metadata_json,latest_observation_id,authority,connection_id,native_id,updated_at"
			old = `DELETE FROM work_evidence_records WHERE card_id=OLD.card_id AND OLD.card_id IS NOT NEW.card_id;`
		}
		if _, err := tx.ExecContext(ctx, `DROP TRIGGER IF EXISTS work_evidence_metadata_`+op); err != nil {
			return err
		}
		q := `CREATE TRIGGER work_evidence_metadata_` + op + ` AFTER ` + event + ` ON work_metadata BEGIN ` + old + `
 DELETE FROM work_evidence_records WHERE card_id=NEW.card_id AND slot NOT IN (SELECT slot FROM work_evidence_entries WHERE card_id=NEW.card_id);
 INSERT INTO work_evidence_records(card_id,slot,evidence_json) SELECT card_id,slot,evidence_json FROM work_evidence_entries WHERE card_id=NEW.card_id ON CONFLICT(card_id,slot) DO UPDATE SET evidence_json=excluded.evidence_json WHERE work_evidence_records.evidence_json IS NOT excluded.evidence_json;
 INSERT INTO work_evidence_index(card_id,lookup_key,evidence_id) SELECT card_id,lookup_key,evidence_id FROM work_evidence_keys WHERE card_id=NEW.card_id ON CONFLICT(evidence_id,lookup_key) DO NOTHING;
 DELETE FROM work_evidence_index WHERE card_id=NEW.card_id AND (evidence_id,lookup_key) NOT IN (SELECT evidence_id,lookup_key FROM work_evidence_keys WHERE card_id=NEW.card_id); END`
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return nil
}
