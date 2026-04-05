package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	refEdgeTypeRef                  = "ref"
	refEdgeTypeBoardCard            = "board_card"
	refEdgeTypeBoardPrimaryDocument = "primary_document"
	refEdgeTypeBoardPinnedRef       = "pinned_ref"
	refEdgeTypeCardParentThread     = "parent_thread"
	refEdgeTypeCardPinnedDocument   = "pinned_document"
	refEdgeTypeDocumentThread       = "thread"
)

type refEdgeTarget struct {
	TargetType string
	TargetID   string
	EdgeType   string
}

type lifecycleFields struct {
	ArchivedAt  sql.NullString
	ArchivedBy  sql.NullString
	TrashedAt   sql.NullString
	TrashedBy   sql.NullString
	TrashReason sql.NullString
}

func normalizeTypedRef(raw string) (string, string, bool) {
	prefix, value, ok := splitTypedRef(strings.TrimSpace(raw))
	if !ok {
		return "", "", false
	}
	return prefix, value, true
}

func typedRefEdgeTargets(edgeType string, refs []string) []refEdgeTarget {
	if len(refs) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(refs))
	targets := make([]refEdgeTarget, 0, len(refs))
	for _, raw := range refs {
		targetType, targetID, ok := normalizeTypedRef(raw)
		if !ok {
			continue
		}
		key := edgeType + "\x00" + targetType + "\x00" + targetID
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		targets = append(targets, refEdgeTarget{
			TargetType: targetType,
			TargetID:   targetID,
			EdgeType:   edgeType,
		})
	}
	return targets
}

func uniqueSortedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func appendRefEdgeTarget(targets []refEdgeTarget, edgeType, targetType, targetID string) []refEdgeTarget {
	targetType = strings.TrimSpace(targetType)
	targetID = strings.TrimSpace(targetID)
	if targetType == "" || targetID == "" {
		return targets
	}
	return append(targets, refEdgeTarget{
		TargetType: targetType,
		TargetID:   targetID,
		EdgeType:   strings.TrimSpace(edgeType),
	})
}

func replaceRefEdges(ctx context.Context, exec eventExec, sourceType, sourceID string, targets []refEdgeTarget) error {
	sourceType = strings.TrimSpace(sourceType)
	sourceID = strings.TrimSpace(sourceID)
	if sourceType == "" || sourceID == "" {
		return fmt.Errorf("ref edge source is required")
	}

	if _, err := exec.ExecContext(
		ctx,
		`DELETE FROM ref_edges WHERE source_type = ? AND source_id = ?`,
		sourceType,
		sourceID,
	); err != nil {
		return fmt.Errorf("clear ref edges for %s %s: %w", sourceType, sourceID, err)
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

		if _, err := exec.ExecContext(
			ctx,
			`INSERT INTO ref_edges(id, source_type, source_id, target_type, target_id, edge_type, created_at, metadata_json)
			 VALUES (?, ?, ?, ?, ?, ?, ?, '{}')`,
			uuid.NewString(),
			sourceType,
			sourceID,
			targetType,
			targetID,
			edgeType,
			now,
		); err != nil {
			if isUniqueViolation(err) {
				continue
			}
			return fmt.Errorf("insert ref edge for %s %s -> %s %s (%s): %w", sourceType, sourceID, targetType, targetID, edgeType, err)
		}
	}
	return nil
}

func lifecycleFieldsFromSQLColumns(archivedAt, archivedBy, trashedAt, trashedBy, trashReason sql.NullString) lifecycleFields {
	return lifecycleFields{
		ArchivedAt:  archivedAt,
		ArchivedBy:  archivedBy,
		TrashedAt:   trashedAt,
		TrashedBy:   trashedBy,
		TrashReason: trashReason,
	}
}

func (fields lifecycleFields) apply(out map[string]any) {
	delete(out, "archived_at")
	delete(out, "archived_by")
	delete(out, "trashed_at")
	delete(out, "trashed_by")
	delete(out, "trash_reason")
	if fields.TrashedAt.Valid && strings.TrimSpace(fields.TrashedAt.String) != "" {
		out["trashed_at"] = fields.TrashedAt.String
		if fields.TrashedBy.Valid && strings.TrimSpace(fields.TrashedBy.String) != "" {
			out["trashed_by"] = fields.TrashedBy.String
		}
		if fields.TrashReason.Valid && strings.TrimSpace(fields.TrashReason.String) != "" {
			out["trash_reason"] = fields.TrashReason.String
		}
		return
	}
	if fields.ArchivedAt.Valid && strings.TrimSpace(fields.ArchivedAt.String) != "" {
		out["archived_at"] = fields.ArchivedAt.String
		if fields.ArchivedBy.Valid && strings.TrimSpace(fields.ArchivedBy.String) != "" {
			out["archived_by"] = fields.ArchivedBy.String
		}
	}
}

func applyArchivedLifecycle(out map[string]any, archivedAt, archivedBy string) {
	lifecycleFields{
		ArchivedAt: nullableString(archivedAt),
		ArchivedBy: nullableString(archivedBy),
	}.apply(out)
}

func clearArchivedLifecycle(out map[string]any) {
	lifecycleFields{}.apply(out)
}

func applyTrashedLifecycle(out map[string]any, trashedAt, trashedBy, trashReason string) {
	lifecycleFields{
		TrashedAt:   nullableString(trashedAt),
		TrashedBy:   nullableString(trashedBy),
		TrashReason: nullableString(trashReason),
	}.apply(out)
}

func clearTrashedLifecycle(out map[string]any, archivedAt, archivedBy string) {
	lifecycleFields{
		ArchivedAt: nullableString(archivedAt),
		ArchivedBy: nullableString(archivedBy),
	}.apply(out)
}

func normalizeIfUpdatedAt(ifUpdatedAt *string) *string {
	if ifUpdatedAt == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*ifUpdatedAt)
	return &trimmed
}

func ensureUpdatedAtMatches(currentUpdatedAt string, ifUpdatedAt *string) error {
	normalized := normalizeIfUpdatedAt(ifUpdatedAt)
	if normalized == nil {
		return nil
	}
	if strings.TrimSpace(currentUpdatedAt) != *normalized {
		return ErrConflict
	}
	return nil
}

func appendIfUpdatedAtClause(query string, args []any, ifUpdatedAt *string) (string, []any) {
	normalized := normalizeIfUpdatedAt(ifUpdatedAt)
	if normalized == nil {
		return query, args
	}
	return query + ` AND updated_at = ?`, append(args, *normalized)
}

func requireIfUpdatedAtRowsAffected(result sql.Result, ifUpdatedAt *string, operation string) error {
	if normalizeIfUpdatedAt(ifUpdatedAt) == nil {
		return nil
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read %s rows affected: %w", strings.TrimSpace(operation), err)
	}
	if rowsAffected == 0 {
		return ErrConflict
	}
	return nil
}

func cloneProvenance(raw any) map[string]any {
	provenance, ok := raw.(map[string]any)
	if !ok || provenance == nil {
		return map[string]any{}
	}
	return cloneMap(provenance)
}

func marshalProvenance(raw any, operation string) (map[string]any, string, error) {
	provenance := cloneProvenance(raw)
	provenanceJSON, err := json.Marshal(provenance)
	if err != nil {
		return nil, "", fmt.Errorf("%s provenance: %w", strings.TrimSpace(operation), err)
	}
	return provenance, string(provenanceJSON), nil
}

func setProvenanceFieldLabels(provenance map[string]any, field string, labels []string) map[string]any {
	if len(labels) == 0 {
		return provenance
	}
	if provenance == nil {
		provenance = map[string]any{}
	}

	byField := map[string]any{}
	if rawByField, ok := provenance["by_field"].(map[string]any); ok {
		byField = cloneMap(rawByField)
	}
	byField[strings.TrimSpace(field)] = labels
	provenance["by_field"] = byField
	return provenance
}

func inferredProvenanceJSON() string {
	return `{"sources":["inferred"]}`
}
