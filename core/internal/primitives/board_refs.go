// Board typed refs: parsing and normalizing the refs collections carried on
// boards and cards, loading them from ref_edges, projecting them into API
// maps, and the ref-edge upsert that keeps card placement edges current.

package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// sqlRowsQuerier is implemented by *sql.DB and *sql.Tx for loading ref_edges sets.
type sqlRowsQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func loadBoardTypedRefStringsByBoardIDs(ctx context.Context, q sqlRowsQuerier, boardIDs []string) (map[string][]string, error) {
	out := make(map[string][]string)
	if len(boardIDs) == 0 {
		return out, nil
	}
	placeholders := strings.Repeat("?,", len(boardIDs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, 0, 1+len(boardIDs))
	args = append(args, refEdgeTypeRef)
	for _, id := range boardIDs {
		args = append(args, strings.TrimSpace(id))
	}
	query := `SELECT source_id, target_type, target_id FROM ref_edges
		WHERE source_type = 'board' AND edge_type = ? AND source_id IN (` + placeholders + `)
		ORDER BY source_id, target_type, target_id`
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query board typed ref edges: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var sourceID, targetType, targetID string
		if err := rows.Scan(&sourceID, &targetType, &targetID); err != nil {
			return nil, fmt.Errorf("scan board ref edge: %w", err)
		}
		targetType = strings.TrimSpace(targetType)
		targetID = strings.TrimSpace(targetID)
		if targetType == "" || targetID == "" {
			continue
		}
		out[sourceID] = append(out[sourceID], makeTypedRef(targetType, targetID))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for id, refs := range out {
		out[id] = uniqueSortedStrings(refs)
	}
	return out, nil
}

func loadBoardCardRefStringsByBoardIDs(ctx context.Context, q sqlRowsQuerier, boardIDs []string) (map[string][]string, error) {
	out := make(map[string][]string)
	if len(boardIDs) == 0 {
		return out, nil
	}
	placeholders := strings.Repeat("?,", len(boardIDs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, 0, 1+len(boardIDs))
	args = append(args, refEdgeTypeBoardCard)
	for _, id := range boardIDs {
		args = append(args, strings.TrimSpace(id))
	}
	query := `SELECT source_id, target_id FROM ref_edges
		WHERE source_type = 'board' AND edge_type = ? AND source_id IN (` + placeholders + `)
		ORDER BY source_id, target_id`
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query board card ref edges: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var sourceID, cardID string
		if err := rows.Scan(&sourceID, &cardID); err != nil {
			return nil, fmt.Errorf("scan board card edge: %w", err)
		}
		cardID = strings.TrimSpace(cardID)
		if cardID == "" {
			continue
		}
		out[sourceID] = append(out[sourceID], makeTypedRef("card", cardID))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for id, refs := range out {
		out[id] = uniqueSortedStrings(refs)
	}
	return out, nil
}

// boardRowToAPI maps a boards row to the HTTP shape. Typed associations are canonical in
// ref_edges (edge_type=ref for document/topic/thread/…); boards.refs_json is a denormalized copy
// kept in sync on write. Responses prefer ref_edges and fall back to refs_json for pre-migration rows.
func boardRowToAPI(ctx context.Context, q sqlRowsQuerier, row boardRow) (map[string]any, error) {
	typedMap, err := loadBoardTypedRefStringsByBoardIDs(ctx, q, []string{row.ID})
	if err != nil {
		return nil, err
	}
	cardMap, err := loadBoardCardRefStringsByBoardIDs(ctx, q, []string{row.ID})
	if err != nil {
		return nil, err
	}
	typedRefs := typedMap[row.ID]
	if len(typedRefs) == 0 {
		typedRefs, err = decodeStoredJSONList(row.RefsJSON, "board.refs")
		if err != nil {
			return nil, err
		}
	}
	cardRefs := cardMap[row.ID]
	if cardRefs == nil {
		cardRefs = []string{}
	}
	return row.boardToMapWithRefData(typedRefs, cardRefs)
}

func boardEffectiveTypedRefsForPatch(ctx context.Context, q sqlRowsQuerier, row boardRow) ([]string, error) {
	typedMap, err := loadBoardTypedRefStringsByBoardIDs(ctx, q, []string{row.ID})
	if err != nil {
		return nil, err
	}
	refs := typedMap[row.ID]
	if len(refs) == 0 {
		refs, err = decodeStoredJSONList(row.RefsJSON, "board.refs")
		if err != nil {
			return nil, err
		}
	}
	return refs, nil
}

func normalizeBoardRefs(board map[string]any) ([]string, error) {
	refs := make([]string, 0)
	if raw, exists := board["refs"]; exists {
		values, err := normalizeBoardTypedRefs(raw)
		if err != nil {
			return nil, err
		}
		refs = append(refs, values...)
	}
	if raw, exists := board["document_refs"]; exists {
		values, err := normalizeBoardTypedRefs(raw)
		if err != nil {
			return nil, err
		}
		refs = append(refs, replaceTypedRefs(nil, "document", values)...)
	}
	if raw, exists := board["pinned_refs"]; exists {
		values, err := normalizeBoardTypedRefs(raw)
		if err != nil {
			return nil, err
		}
		refs = append(refs, values...)
	}
	if raw, exists := board["primary"+"_document_id"]; exists {
		documentID := strings.TrimSpace(anyStringValue(raw))
		if documentID != "" {
			if err := validateDocumentID(documentID); err != nil {
				return nil, err
			}
			refs = append(refs, "document:"+documentID)
		}
	}
	refs = uniqueSortedStrings(refs)
	return refs, nil
}

func normalizeBoardRefsFromValue(raw any) ([]string, error) {
	values, err := normalizeStringSlice(raw)
	if err != nil {
		return nil, err
	}
	return uniqueSortedStrings(values), nil
}

func normalizeBoardTypedRefs(raw any) ([]string, error) {
	values, err := normalizeStringSlice(raw)
	if err != nil {
		return nil, err
	}
	for _, ref := range values {
		if _, _, ok := normalizeTypedRef(ref); !ok {
			return nil, fmt.Errorf("invalid typed ref %q", strings.TrimSpace(ref))
		}
	}
	return uniqueSortedStrings(values), nil
}

func replaceTypedRefs(refs []string, targetType string, values []string) []string {
	targetType = strings.TrimSpace(targetType)
	out := make([]string, 0, len(refs)+len(values))
	for _, ref := range refs {
		if _, refTargetType, ok := normalizeTypedRef(ref); ok && refTargetType == targetType {
			continue
		}
		out = append(out, strings.TrimSpace(ref))
	}
	out = append(out, values...)
	return uniqueSortedStrings(out)
}

func replaceBoardPinnedRefs(refs []string, values []string) []string {
	return append(refs, values...)
}

func removeTypedRefPrefix(refs []string, targetType string) []string {
	targetType = strings.TrimSpace(targetType)
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if _, refTargetType, ok := normalizeTypedRef(ref); ok && refTargetType == targetType {
			continue
		}
		out = append(out, strings.TrimSpace(ref))
	}
	return uniqueSortedStrings(out)
}

func boardDocumentRefsFromRefs(refs []string) []string {
	out := make([]string, 0)
	for _, ref := range refs {
		if prefix, value, ok := normalizeTypedRef(ref); ok && prefix == "document" {
			out = append(out, "document:"+value)
		}
	}
	return uniqueSortedStrings(out)
}

func boardPinnedRefsFromRefs(refs []string) []string {
	out := make([]string, 0)
	for _, ref := range refs {
		if _, targetType, ok := normalizeTypedRef(ref); ok && targetType != "document" && targetType != "topic" && targetType != "thread" {
			out = append(out, ref)
		}
	}
	return uniqueSortedStrings(out)
}

func boardTopicRefsFromRefs(refs []string) []string {
	out := make([]string, 0)
	for _, ref := range refs {
		if prefix, value, ok := normalizeTypedRef(ref); ok && prefix == "topic" {
			out = append(out, "topic:"+value)
		}
	}
	return uniqueSortedStrings(out)
}

func boardTypedRefOrNil(prefix, raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return strings.TrimSpace(prefix) + ":" + raw
}

// boardToMapWithRefData builds the public board map using canonical typed refs and board→card edges.
func (r boardRow) boardToMapWithRefData(typedRefs, cardRefs []string) (map[string]any, error) {
	columnSchema, err := decodeBoardColumnSchema(r.ColumnSchemaJSON)
	if err != nil {
		return nil, err
	}
	typedRefs = uniqueSortedStrings(typedRefs)
	if typedRefs == nil {
		typedRefs = []string{}
	}
	cardRefs = uniqueSortedStrings(cardRefs)
	if cardRefs == nil {
		cardRefs = []string{}
	}
	handle := firstNonEmpty(strings.TrimSpace(r.Handle.String), r.ID)
	m := map[string]any{
		"id":                r.ID,
		"ref":               "board:" + handle,
		"handle":            handle,
		"title":             r.Title,
		"summary":           strings.TrimSpace(r.Summary),
		"role":              r.Role,
		"initiative_target": nullableInitiativeTarget(r.InitiativeTarget),
		"state":             canonicalLifecycleState(r.ArchivedAt, r.TrashedAt),
		"thread_id":         r.ThreadID,
		"refs":              typedRefs,
		"card_refs":         cardRefs,
		"column_schema":     columnSchema,
		"created_at":        r.CreatedAt,
		"created_by":        r.CreatedBy,
		"updated_at":        r.UpdatedAt,
		"updated_by":        r.UpdatedBy,
	}
	owners, err := decodeStoredJSONList(r.OwnersJSON, "board.owners")
	if err != nil {
		return nil, err
	}
	m["owners"] = owners
	if documentRefs := boardDocumentRefsFromRefs(typedRefs); len(documentRefs) > 0 {
		m["document_refs"] = documentRefs
	}
	if pinnedRefs := boardPinnedRefsFromRefs(typedRefs); len(pinnedRefs) > 0 {
		m["pinned_refs"] = pinnedRefs
	}
	if topicRefs := boardTopicRefsFromRefs(typedRefs); len(topicRefs) > 0 {
		m["primary_topic_ref"] = topicRefs[0]
	}
	mergeBoardArchiveTrashFields(m, r)
	return m, nil
}

func containsTypedRefPrefix(refs []string, prefix string) bool {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return false
	}
	for _, ref := range refs {
		refPrefix, _, ok := normalizeTypedRef(ref)
		if ok && refPrefix == prefix {
			return true
		}
	}
	return false
}

func upsertBoardCardRefEdge(ctx context.Context, tx *accessTx, boardID, cardID, columnKey, rank string) error {
	boardID = strings.TrimSpace(boardID)
	cardID = strings.TrimSpace(cardID)
	if boardID == "" || cardID == "" {
		return invalidBoardRequest("board card membership requires board and card ids")
	}
	metadataJSON, err := json.Marshal(map[string]any{
		"column_key": strings.TrimSpace(columnKey),
		"rank":       strings.TrimSpace(rank),
	})
	if err != nil {
		return fmt.Errorf("marshal board card edge metadata: %w", err)
	}
	_, err = tx.ExecContext(
		ctx,
		`INSERT INTO ref_edges(id, source_type, source_id, target_type, target_id, edge_type, created_at, metadata_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(source_type, source_id, target_type, target_id, edge_type)
		 DO UPDATE SET metadata_json = excluded.metadata_json`,
		uuid.NewString(),
		"board",
		boardID,
		"card",
		cardID,
		refEdgeTypeBoardCard,
		time.Now().UTC().Format(time.RFC3339Nano),
		string(metadataJSON),
	)
	if err != nil {
		return fmt.Errorf("upsert board card ref edge: %w", err)
	}
	// Keep the typed primary phase in the same canonical write transaction.
	// Secondary board placement remains independent. Do this in the runtime
	// writer, so unrelated ref-edge writes on plain SQLite connections do not
	// acquire dependencies on the card timestamp indexes' SQL functions.
	_, err = tx.ExecContext(ctx, `UPDATE cards SET column_key=? WHERE id=? AND board_id=? AND column_key<>?`, strings.TrimSpace(columnKey), cardID, boardID, strings.TrimSpace(columnKey))
	return err
}
