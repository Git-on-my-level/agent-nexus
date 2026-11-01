// Migration 12: moving topic summary extensions and typed refs into JSON
// columns and ref_edges.

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

func applyMigration12TopicsSummaryExtensionsJSON(ctx context.Context, tx *sql.Tx) error {
	ok, err := sqliteTableExists(ctx, tx, "topics")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	hasBody, err := sqliteTableHasColumn(ctx, tx, "topics", "body_json")
	if err != nil {
		return fmt.Errorf("migration 12 pragma topics.body_json: %w", err)
	}
	hasExt, err := sqliteTableHasColumn(ctx, tx, "topics", "extensions_json")
	if err != nil {
		return fmt.Errorf("migration 12 pragma topics.extensions_json: %w", err)
	}
	if hasBody && !hasExt {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE topics RENAME COLUMN body_json TO extensions_json`); err != nil {
			return fmt.Errorf("migration 12 rename body_json: %w", err)
		}
		hasExt = true
	} else if !hasExt {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE topics ADD COLUMN extensions_json TEXT NOT NULL DEFAULT '{}'`); err != nil {
			return fmt.Errorf("migration 12 add extensions_json: %w", err)
		}
		hasExt = true
	}
	hasSummary, err := sqliteTableHasColumn(ctx, tx, "topics", "summary")
	if err != nil {
		return fmt.Errorf("migration 12 pragma topics.summary: %w", err)
	}
	if !hasSummary {
		if _, err := tx.ExecContext(ctx, `ALTER TABLE topics ADD COLUMN summary TEXT`); err != nil {
			return fmt.Errorf("migration 12 add summary: %w", err)
		}
	}

	rows, err := tx.QueryContext(ctx, `SELECT id, type, title, thread_id, extensions_json FROM topics`)
	if err != nil {
		return fmt.Errorf("migration 12 scan topics: %w", err)
	}
	defer rows.Close()

	type row struct {
		id, ext string
		typ     sql.NullString
		title   sql.NullString
		thread  sql.NullString
	}
	var batch []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.typ, &r.title, &r.thread, &r.ext); err != nil {
			return fmt.Errorf("migration 12 scan row: %w", err)
		}
		batch = append(batch, r)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("migration 12 rows: %w", err)
	}

	for _, r := range batch {
		extObj := map[string]any{}
		if strings.TrimSpace(r.ext) != "" {
			if err := json.Unmarshal([]byte(r.ext), &extObj); err != nil || extObj == nil {
				extObj = map[string]any{}
			}
		}
		migration12CoerceLegacyTopicExt(extObj)

		nextType := strings.TrimSpace(r.typ.String)
		if nextType == "" {
			nextType = strings.TrimSpace(migration12StringFromAny(extObj["type"]))
		}
		nextTitle := strings.TrimSpace(r.title.String)
		if nextTitle == "" {
			nextTitle = strings.TrimSpace(migration12StringFromAny(extObj["title"]))
		}
		nextThread := strings.TrimSpace(r.thread.String)
		if nextThread == "" {
			nextThread = strings.TrimSpace(migration12StringFromAny(extObj["thread_id"]))
		}

		summary := strings.TrimSpace(migration12StringFromAny(extObj["summary"]))
		if summary == "" && nextTitle != "" {
			summary = nextTitle
		}

		hasRefLists := migration12ExtHasRefLists(extObj)
		if hasRefLists {
			targets := migration12CombineTopicRefTargets(extObj, nextThread)
			if err := migration12ReplaceTopicRefEdges(ctx, tx, r.id, targets); err != nil {
				return err
			}
		} else if err := migration12AnnotateTopicRefMetadata(ctx, tx, r.id, nextThread); err != nil {
			return err
		}

		for _, k := range migration12TopicKeysPromotedFromExt() {
			delete(extObj, k)
		}
		extBytes, err := json.Marshal(extObj)
		if err != nil {
			return fmt.Errorf("migration 12 marshal extensions for %s: %w", r.id, err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE topics SET type = ?, title = ?, thread_id = ?, summary = ?, extensions_json = ? WHERE id = ?`,
			nextType, nextTitle, nextThread, summary, string(extBytes), r.id,
		); err != nil {
			return fmt.Errorf("migration 12 update topic %s: %w", r.id, err)
		}
	}
	return nil
}

func migration12TopicKeysPromotedFromExt() []string {
	return []string{
		"id", "type", "title", "summary", "thread_id", "thread_ref", "primary_thread_ref", "primary_thread_id",
		"owner_refs", "document_refs", "board_refs", "related_refs",
		"status", "provenance",
		"created_at", "created_by", "updated_at", "updated_by", "state",
		"archived_at", "archived_by", "trashed_at", "trashed_by", "trash_reason",
	}
}

func migration12StringFromAny(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func migration12CoerceLegacyTopicExt(ext map[string]any) {
	legacyRef := "primary" + "_thread_ref"
	legacyID := "primary" + "_thread_id"
	if v, ok := ext[legacyRef]; ok && v != nil {
		refStr := migration12StringFromAny(v)
		if refStr != "" {
			if prefix, id, okRef := migration12SplitTypedRef(refStr); okRef && prefix == "thread" && strings.TrimSpace(id) != "" {
				if _, has := ext["thread_id"]; !has || migration12StringFromAny(ext["thread_id"]) == "" {
					ext["thread_id"] = strings.TrimSpace(id)
				}
			}
		}
		delete(ext, legacyRef)
	}
	if v, ok := ext[legacyID]; ok && v != nil {
		id := migration12StringFromAny(v)
		if id != "" {
			if _, has := ext["thread_id"]; !has || migration12StringFromAny(ext["thread_id"]) == "" {
				ext["thread_id"] = id
			}
		}
		delete(ext, legacyID)
	}
	if raw, exists := ext["thread_ref"]; exists && raw != nil {
		refStr := migration12StringFromAny(raw)
		if refStr != "" {
			if prefix, id, okRef := migration12SplitTypedRef(refStr); okRef && prefix == "thread" && strings.TrimSpace(id) != "" {
				if _, has := ext["thread_id"]; !has || migration12StringFromAny(ext["thread_id"]) == "" {
					ext["thread_id"] = strings.TrimSpace(id)
				}
			}
		}
		delete(ext, "thread_ref")
	}
}

func migration12SplitTypedRef(raw string) (prefix, id string, ok bool) {
	raw = strings.TrimSpace(raw)
	i := strings.IndexByte(raw, ':')
	if i <= 0 || i == len(raw)-1 {
		return "", "", false
	}
	return raw[:i], raw[i+1:], true
}

func migration12ExtHasRefLists(ext map[string]any) bool {
	for _, field := range []string{"owner_refs", "document_refs", "board_refs", "related_refs"} {
		raw, ok := ext[field]
		if !ok || raw == nil {
			continue
		}
		switch sl := raw.(type) {
		case []any:
			if len(sl) > 0 {
				return true
			}
		case []string:
			if len(sl) > 0 {
				return true
			}
		}
	}
	return false
}

type migration12RefTarget struct {
	TargetType   string
	TargetID     string
	EdgeType     string
	MetadataJSON string
}

func migration12TopicRefMetaJSON(field string) string {
	b, err := json.Marshal(map[string]string{"topic_ref_field": field})
	if err != nil {
		return `{"topic_ref_field":"related_refs"}`
	}
	return string(b)
}

func migration12CombineTopicRefTargets(ext map[string]any, primaryThreadID string) []migration12RefTarget {
	targets := make([]migration12RefTarget, 0, 8)
	for _, field := range []string{"owner_refs", "document_refs", "board_refs", "related_refs"} {
		for _, t := range migration12TypedRefEdgeTargets("ref", migration12StringSliceFromAny(ext[field])) {
			t.MetadataJSON = migration12TopicRefMetaJSON(field)
			targets = append(targets, t)
		}
	}
	if strings.TrimSpace(primaryThreadID) != "" {
		targets = append(targets, migration12RefTarget{
			TargetType:   "thread",
			TargetID:     strings.TrimSpace(primaryThreadID),
			EdgeType:     "ref",
			MetadataJSON: `{"topic_ref_field":"_primary_thread"}`,
		})
	}
	return targets
}

func migration12StringSliceFromAny(raw any) []string {
	if raw == nil {
		return nil
	}
	switch sl := raw.(type) {
	case []string:
		out := make([]string, 0, len(sl))
		for _, s := range sl {
			s = strings.TrimSpace(s)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(sl))
		for _, v := range sl {
			s := migration12StringFromAny(v)
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func migration12TypedRefEdgeTargets(edgeType string, refs []string) []migration12RefTarget {
	if len(refs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(refs))
	targets := make([]migration12RefTarget, 0, len(refs))
	for _, raw := range refs {
		prefix, id, ok := migration12SplitTypedRef(raw)
		if !ok {
			continue
		}
		key := edgeType + "\x00" + prefix + "\x00" + id
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		targets = append(targets, migration12RefTarget{
			TargetType: prefix,
			TargetID:   id,
			EdgeType:   edgeType,
		})
	}
	return targets
}

func migration12ReplaceTopicRefEdges(ctx context.Context, tx *sql.Tx, topicID string, targets []migration12RefTarget) error {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return fmt.Errorf("migration 12 ref edges: empty topic id")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM ref_edges WHERE source_type = ? AND source_id = ?`, "topic", topicID); err != nil {
		return fmt.Errorf("migration 12 clear ref edges for topic %s: %w", topicID, err)
	}
	if len(targets) == 0 {
		return nil
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	seen := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		targetType := strings.TrimSpace(target.TargetType)
		targetID := strings.TrimSpace(target.TargetID)
		edgeType := strings.TrimSpace(target.EdgeType)
		if targetType == "" || targetID == "" || edgeType == "" {
			continue
		}
		key := edgeType + "\x00" + targetType + "\x00" + targetID
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		meta := strings.TrimSpace(target.MetadataJSON)
		if meta == "" {
			meta = "{}"
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO ref_edges(id, source_type, source_id, target_type, target_id, edge_type, created_at, metadata_json)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			uuid.NewString(), "topic", topicID, targetType, targetID, edgeType, now, meta,
		); err != nil {
			return fmt.Errorf("migration 12 insert ref edge for topic %s: %w", topicID, err)
		}
	}
	return nil
}

func migration12AnnotateTopicRefMetadata(ctx context.Context, tx *sql.Tx, topicID, primaryThreadID string) error {
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return nil
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT target_type, target_id, metadata_json FROM ref_edges WHERE source_type = 'topic' AND source_id = ?`,
		topicID,
	)
	if err != nil {
		return fmt.Errorf("migration 12 list ref edges %s: %w", topicID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var targetType, targetID, meta string
		if err := rows.Scan(&targetType, &targetID, &meta); err != nil {
			return fmt.Errorf("migration 12 scan ref edge: %w", err)
		}
		field := migration12TopicRefFieldFromStoredMeta(meta, targetType, targetID, primaryThreadID)
		if field == "_primary_thread" {
			continue
		}
		newMeta := migration12TopicRefMetaJSON(field)
		if strings.TrimSpace(meta) == newMeta {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE ref_edges SET metadata_json = ? WHERE source_type = 'topic' AND source_id = ? AND target_type = ? AND target_id = ? AND edge_type = 'ref'`,
			newMeta, topicID, targetType, targetID,
		); err != nil {
			return fmt.Errorf("migration 12 annotate ref edge: %w", err)
		}
	}
	return rows.Err()
}

func migration12TopicRefFieldFromStoredMeta(metaJSON, targetType, targetID, primaryThreadID string) string {
	metaJSON = strings.TrimSpace(metaJSON)
	if metaJSON != "" && metaJSON != "{}" {
		var m map[string]any
		if json.Unmarshal([]byte(metaJSON), &m) == nil {
			if s, ok := m["topic_ref_field"].(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	tt := strings.TrimSpace(targetType)
	tid := strings.TrimSpace(targetID)
	if tt == "thread" && tid == strings.TrimSpace(primaryThreadID) {
		return "_primary_thread"
	}
	switch tt {
	case "actor":
		return "owner_refs"
	case "document":
		return "document_refs"
	case "board":
		return "board_refs"
	default:
		return "related_refs"
	}
}
