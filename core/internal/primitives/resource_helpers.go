package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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
	// MetadataJSON is optional JSON object for the ref_edges.metadata_json column; empty means "{}".
	MetadataJSON string
}

type lifecycleFields struct {
	ArchivedAt  sql.NullString
	ArchivedBy  sql.NullString
	TrashedAt   sql.NullString
	TrashedBy   sql.NullString
	TrashReason sql.NullString
}

func makeTypedRef(prefix, id string) string {
	return prefix + ":" + id
}

func makePublicTypedRef(ctx context.Context, q queryRower, prefix, id string) string {
	prefix = strings.TrimSpace(prefix)
	id = strings.TrimSpace(id)
	if prefix == "" || id == "" {
		return ""
	}
	if q == nil {
		return makeTypedRef(prefix, id)
	}
	resolved, err := resolvePublicResourceRef(ctx, q, prefix, id)
	if err != nil {
		return makeTypedRef(prefix, id)
	}
	return resolved.CanonicalRef
}

func resolvePublicResourceRef(ctx context.Context, q queryRower, typ, id string) (ResolvedResourceRef, error) {
	typ = strings.TrimSpace(typ)
	id = strings.TrimSpace(id)
	if typ == "" || id == "" {
		return ResolvedResourceRef{}, ErrInvalidResourceRef
	}
	if typ == "document_revision" || typ == "card_revision" {
		return resolveRevisionResourceRef(ctx, q, typ, id)
	}
	table := resourceTables[typ]
	if table == "" {
		return ResolvedResourceRef{Type: typ, ID: id, CanonicalRef: makeTypedRef(typ, id)}, nil
	}
	var handle sql.NullString
	err := q.QueryRowContext(ctx, `SELECT handle FROM `+table+` WHERE id = ?`, id).Scan(&handle)
	if errors.Is(err, sql.ErrNoRows) {
		return ResolvedResourceRef{Type: typ, ID: id, CanonicalRef: makeTypedRef(typ, id)}, nil
	}
	if err != nil {
		return ResolvedResourceRef{}, err
	}
	h := strings.TrimSpace(handle.String)
	if h == "" {
		h = id
	}
	return ResolvedResourceRef{Type: typ, ID: id, Handle: h, CanonicalRef: makeTypedRef(typ, h)}, nil
}

func publicTypedRefs(ctx context.Context, q queryRower, refs []string) []string {
	if len(refs) == 0 {
		return refs
	}
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		prefix, value, ok := normalizeTypedRef(ref)
		if !ok {
			out = append(out, ref)
			continue
		}
		out = append(out, makePublicTypedRef(ctx, q, prefix, value))
	}
	return uniqueSortedStrings(out)
}

func publicTypedRefsAny(ctx context.Context, q queryRower, refs []any) []any {
	if len(refs) == 0 {
		return refs
	}
	out := make([]any, 0, len(refs))
	stringsOnly := make([]string, 0, len(refs))
	for _, ref := range refs {
		text, ok := ref.(string)
		if !ok {
			out = append(out, ref)
			continue
		}
		stringsOnly = append(stringsOnly, text)
	}
	if len(stringsOnly) == len(refs) {
		public := publicTypedRefs(ctx, q, stringsOnly)
		out = make([]any, len(public))
		for i, ref := range public {
			out[i] = ref
		}
		return out
	}
	for _, ref := range stringsOnly {
		out = append(out, publicTypedRefs(ctx, q, []string{ref})[0])
	}
	return out
}

func publicRefsInValue(ctx context.Context, q queryRower, value any) any {
	return publicRefsInField(ctx, q, "", value)
}

func publicRefsInField(ctx context.Context, q queryRower, key string, value any) any {
	if isPublicRefFieldKey(key) {
		return publicRefsInRefValue(ctx, q, value)
	}
	switch v := value.(type) {
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = publicRefsInValue(ctx, q, item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = publicRefsInField(ctx, q, key, item)
		}
		return out
	default:
		return value
	}
}

func publicRefsInRefValue(ctx context.Context, q queryRower, value any) any {
	switch v := value.(type) {
	case string:
		prefix, refValue, ok := normalizeTypedRef(v)
		if !ok {
			return v
		}
		return makePublicTypedRef(ctx, q, prefix, refValue)
	case []string:
		return publicTypedRefs(ctx, q, v)
	case []any:
		return publicTypedRefsAny(ctx, q, v)
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = publicRefsInField(ctx, q, key, item)
		}
		return out
	default:
		return value
	}
}

func isPublicRefFieldKey(key string) bool {
	key = strings.TrimSpace(key)
	return key == "ref" || key == "refs" || strings.HasSuffix(key, "_ref") || strings.HasSuffix(key, "_refs")
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

func resolvedTypedRefEdgeTargets(ctx context.Context, q queryRower, edgeType string, refs []string) []refEdgeTarget {
	if len(refs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(refs))
	targets := make([]refEdgeTarget, 0, len(refs))
	for _, raw := range refs {
		targetType, targetValue, ok := normalizeTypedRef(raw)
		if !ok {
			continue
		}
		targetID := targetValue
		publicRef := strings.TrimSpace(raw)
		if resourceTables[targetType] != "" || targetType == "document_revision" || targetType == "card_revision" {
			if resolved, err := resolveResourceByTypedValue(ctx, q, targetType, targetValue); err == nil {
				targetID = resolved.ID
				publicRef = resolved.CanonicalRef
			}
		}
		key := edgeType + "\x00" + targetType + "\x00" + targetID
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		meta := map[string]any{
			"target_ref":         publicRef,
			"resolved_target_id": targetID,
		}
		metaJSON, _ := json.Marshal(meta)
		targets = append(targets, refEdgeTarget{
			TargetType:   targetType,
			TargetID:     targetID,
			EdgeType:     edgeType,
			MetadataJSON: string(metaJSON),
		})
	}
	return targets
}

func resolveResourceByTypedValue(ctx context.Context, q queryRower, typ, value string) (ResolvedResourceRef, error) {
	if typ == "document_revision" || typ == "card_revision" {
		return resolveRevisionResourceRef(ctx, q, typ, value)
	}
	table := resourceTables[typ]
	if table == "" {
		return ResolvedResourceRef{Type: typ, ID: strings.TrimSpace(value), CanonicalRef: makeTypedRef(typ, strings.TrimSpace(value))}, nil
	}
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return ResolvedResourceRef{}, ErrInvalidResourceRef
	}
	var id string
	var handle sql.NullString
	err := q.QueryRowContext(ctx, `SELECT id, handle FROM `+table+` WHERE handle = ?`, normalized).Scan(&id, &handle)
	if err == nil {
		h := strings.TrimSpace(handle.String)
		return ResolvedResourceRef{Type: typ, ID: id, Handle: h, CanonicalRef: makeTypedRef(typ, h)}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return ResolvedResourceRef{}, err
	}
	err = q.QueryRowContext(ctx, `SELECT id, handle FROM `+table+` WHERE id = ?`, normalized).Scan(&id, &handle)
	if errors.Is(err, sql.ErrNoRows) {
		return ResolvedResourceRef{}, ErrNotFound
	}
	if err != nil {
		return ResolvedResourceRef{}, err
	}
	h := strings.TrimSpace(handle.String)
	canonical := makeTypedRef(typ, id)
	if h != "" {
		canonical = makeTypedRef(typ, h)
	}
	return ResolvedResourceRef{Type: typ, ID: id, Handle: h, CanonicalRef: canonical}, nil
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
	return replaceRefEdgesSelective(ctx, exec, sourceType, sourceID, nil, targets)
}

// replaceRefEdgesSelective deletes ref_edges for source matching deleteEdgeTypes, then inserts targets.
// When deleteEdgeTypes is nil or empty, all edges for that source are removed (same as legacy replaceRefEdges).
func replaceRefEdgesSelective(ctx context.Context, exec eventExec, sourceType, sourceID string, deleteEdgeTypes []string, targets []refEdgeTarget) error {
	sourceType = strings.TrimSpace(sourceType)
	sourceID = strings.TrimSpace(sourceID)
	if sourceType == "" || sourceID == "" {
		return fmt.Errorf("ref edge source is required")
	}

	if len(deleteEdgeTypes) == 0 {
		if _, err := exec.ExecContext(
			ctx,
			`DELETE FROM ref_edges WHERE source_type = ? AND source_id = ?`,
			sourceType,
			sourceID,
		); err != nil {
			return fmt.Errorf("clear ref edges for %s %s: %w", sourceType, sourceID, err)
		}
	} else {
		placeholders := strings.Repeat("?,", len(deleteEdgeTypes))
		placeholders = placeholders[:len(placeholders)-1]
		args := make([]any, 0, 2+len(deleteEdgeTypes))
		args = append(args, sourceType, sourceID)
		for _, et := range deleteEdgeTypes {
			args = append(args, strings.TrimSpace(et))
		}
		q := `DELETE FROM ref_edges WHERE source_type = ? AND source_id = ? AND edge_type IN (` + placeholders + `)`
		if _, err := exec.ExecContext(ctx, q, args...); err != nil {
			return fmt.Errorf("clear ref edges for %s %s (types %v): %w", sourceType, sourceID, deleteEdgeTypes, err)
		}
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
		publicTargetRef := makeTypedRef(targetType, targetID)
		if resolved, err := resolveResourceByTypedValue(ctx, exec, targetType, targetID); err == nil {
			targetID = resolved.ID
			publicTargetRef = resolved.CanonicalRef
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
		meta = mergeRefEdgeMetadata(meta, map[string]any{
			"target_ref":         publicTargetRef,
			"resolved_target_id": targetID,
		})
		if _, err := exec.ExecContext(
			ctx,
			`INSERT INTO ref_edges(id, source_type, source_id, target_type, target_id, edge_type, created_at, metadata_json)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			uuid.NewString(),
			sourceType,
			sourceID,
			targetType,
			targetID,
			edgeType,
			now,
			meta,
		); err != nil {
			if isUniqueViolation(err) {
				continue
			}
			return fmt.Errorf("insert ref edge for %s %s -> %s %s (%s): %w", sourceType, sourceID, targetType, targetID, edgeType, err)
		}
	}
	return nil
}

func mergeRefEdgeMetadata(raw string, additions map[string]any) string {
	meta := map[string]any{}
	if strings.TrimSpace(raw) != "" {
		_ = json.Unmarshal([]byte(raw), &meta)
	}
	if meta == nil {
		meta = map[string]any{}
	}
	for key, value := range additions {
		if _, exists := meta[key]; !exists {
			meta[key] = value
		}
	}
	encoded, err := json.Marshal(meta)
	if err != nil {
		return raw
	}
	return string(encoded)
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
