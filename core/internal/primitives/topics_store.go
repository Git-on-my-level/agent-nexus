package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrInvalidTopicRequest = errors.New("invalid topic request")

type TopicPatchResult struct {
	Topic map[string]any
	Event map[string]any
}

type topicRow struct {
	ID             string
	Title          sql.NullString
	Summary        sql.NullString
	ThreadID       sql.NullString
	ExtensionsJSON string
	ProvenanceJSON string
	CreatedAt      string
	CreatedBy      string
	UpdatedAt      string
	UpdatedBy      string
	ArchivedAt     sql.NullString
	ArchivedBy     sql.NullString
	TrashedAt      sql.NullString
	TrashedBy      sql.NullString
	TrashReason    sql.NullString
}

// topicRefBuckets holds topic typed-ref lists reconstructed from ref_edges (not from extensions_json).
type topicRefBuckets struct {
	OwnerRefs    []string
	DocumentRefs []string
	BoardRefs    []string
	RelatedRefs  []string
}

func emptyTopicRefBuckets() topicRefBuckets {
	return topicRefBuckets{
		OwnerRefs:    []string{},
		DocumentRefs: []string{},
		BoardRefs:    []string{},
		RelatedRefs:  []string{},
	}
}

func topicRefFieldMetaJSON(field string) string {
	b, err := json.Marshal(map[string]string{"topic_ref_field": field})
	if err != nil {
		return `{"topic_ref_field":"related_refs"}`
	}
	return string(b)
}

func topicRefFieldFromEdgeMeta(metaJSON, targetType, targetID, primaryThreadID string) string {
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

func appendUniqueString(dst []string, v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return dst
	}
	for _, x := range dst {
		if x == v {
			return dst
		}
	}
	return append(dst, v)
}

func topicRefBucketsFromEdgeRows(rows []topicRefEdgeScan, primaryThreadID string) topicRefBuckets {
	b := emptyTopicRefBuckets()
	for _, row := range rows {
		field := topicRefFieldFromEdgeMeta(row.MetadataJSON, row.TargetType, row.TargetID, primaryThreadID)
		if field == "_primary_thread" {
			continue
		}
		ref := makeTypedRef(row.TargetType, row.TargetID)
		switch field {
		case "owner_refs":
			b.OwnerRefs = appendUniqueString(b.OwnerRefs, ref)
		case "document_refs":
			b.DocumentRefs = appendUniqueString(b.DocumentRefs, ref)
		case "board_refs":
			b.BoardRefs = appendUniqueString(b.BoardRefs, ref)
		default:
			b.RelatedRefs = appendUniqueString(b.RelatedRefs, ref)
		}
	}
	sort.Strings(b.OwnerRefs)
	sort.Strings(b.DocumentRefs)
	sort.Strings(b.BoardRefs)
	sort.Strings(b.RelatedRefs)
	return b
}

type topicRefEdgeScan struct {
	TargetType   string
	TargetID     string
	MetadataJSON string
}

func (s *Store) loadTopicRefBuckets(ctx context.Context, topicID, primaryThreadID string) (topicRefBuckets, error) {
	if s == nil || s.db == nil {
		return emptyTopicRefBuckets(), fmt.Errorf("primitives store database is not initialized")
	}
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return emptyTopicRefBuckets(), nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT target_type, target_id, metadata_json FROM ref_edges WHERE source_type = 'topic' AND source_id = ? AND edge_type = ?`,
		topicID, refEdgeTypeRef,
	)
	if err != nil {
		return emptyTopicRefBuckets(), fmt.Errorf("query topic ref_edges: %w", err)
	}
	defer rows.Close()
	var scanned []topicRefEdgeScan
	for rows.Next() {
		var r topicRefEdgeScan
		if err := rows.Scan(&r.TargetType, &r.TargetID, &r.MetadataJSON); err != nil {
			return emptyTopicRefBuckets(), fmt.Errorf("scan topic ref_edge: %w", err)
		}
		scanned = append(scanned, r)
	}
	if err := rows.Err(); err != nil {
		return emptyTopicRefBuckets(), fmt.Errorf("iterate topic ref_edges: %w", err)
	}
	return topicRefBucketsFromEdgeRows(scanned, primaryThreadID), nil
}

func (s *Store) loadTopicRefBucketsBatch(ctx context.Context, topicIDs []string, threadIDByTopic map[string]string) (map[string]topicRefBuckets, error) {
	if len(topicIDs) == 0 {
		return map[string]topicRefBuckets{}, nil
	}
	out := make(map[string]topicRefBuckets, len(topicIDs))
	for _, id := range topicIDs {
		out[strings.TrimSpace(id)] = emptyTopicRefBuckets()
	}
	placeholders := strings.Repeat("?,", len(topicIDs))
	placeholders = placeholders[:len(placeholders)-1]
	args := make([]any, 0, len(topicIDs)+2)
	args = append(args, "topic", refEdgeTypeRef)
	for _, id := range topicIDs {
		args = append(args, strings.TrimSpace(id))
	}
	q := fmt.Sprintf(
		`SELECT source_id, target_type, target_id, metadata_json FROM ref_edges WHERE source_type = ? AND edge_type = ? AND source_id IN (%s)`,
		placeholders,
	)
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query topic ref_edges batch: %w", err)
	}
	defer rows.Close()
	byTopic := make(map[string][]topicRefEdgeScan)
	for rows.Next() {
		var sourceID string
		var r topicRefEdgeScan
		if err := rows.Scan(&sourceID, &r.TargetType, &r.TargetID, &r.MetadataJSON); err != nil {
			return nil, fmt.Errorf("scan topic ref_edge batch: %w", err)
		}
		sourceID = strings.TrimSpace(sourceID)
		byTopic[sourceID] = append(byTopic[sourceID], r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate topic ref_edges batch: %w", err)
	}
	for id := range out {
		primary := ""
		if threadIDByTopic != nil {
			primary = strings.TrimSpace(threadIDByTopic[id])
		}
		out[id] = topicRefBucketsFromEdgeRows(byTopic[id], primary)
	}
	return out, nil
}

var topicExtensionsMergeDenylist = map[string]struct{}{
	"id": {}, "type": {}, "title": {}, "summary": {}, "thread_id": {},
	"owner_refs": {}, "document_refs": {}, "board_refs": {}, "related_refs": {},
	"thread_ref": {}, "primary_thread_ref": {}, "primary_thread_id": {},
	"status": {}, "state": {},
	"created_at": {}, "created_by": {}, "updated_at": {}, "updated_by": {},
	"provenance":  {},
	"archived_at": {}, "archived_by": {}, "trashed_at": {}, "trashed_by": {}, "trash_reason": {},
}

func marshalTopicExtensionsJSON(m map[string]any) (string, error) {
	ext := cloneMap(m)
	for k := range topicExtensionsMergeDenylist {
		delete(ext, k)
	}
	if len(ext) == 0 {
		return "{}", nil
	}
	b, err := json.Marshal(ext)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (r topicRow) toMap(buckets topicRefBuckets) (map[string]any, error) {
	ext := map[string]any{}
	if strings.TrimSpace(r.ExtensionsJSON) != "" {
		if err := json.Unmarshal([]byte(r.ExtensionsJSON), &ext); err != nil {
			ext = map[string]any{}
		}
	}
	if ext == nil {
		ext = map[string]any{}
	}

	provenance := map[string]any{}
	if strings.TrimSpace(r.ProvenanceJSON) != "" {
		if err := json.Unmarshal([]byte(r.ProvenanceJSON), &provenance); err != nil {
			provenance = map[string]any{}
		}
	}
	if provenance == nil {
		provenance = map[string]any{}
	}

	out := map[string]any{}
	out["id"] = r.ID
	title := ""
	if r.Title.Valid {
		title = r.Title.String
	}
	out["title"] = title
	summary := ""
	if r.Summary.Valid {
		summary = strings.TrimSpace(r.Summary.String)
	}
	out["summary"] = summary
	if r.ThreadID.Valid && strings.TrimSpace(r.ThreadID.String) != "" {
		out["thread_id"] = strings.TrimSpace(r.ThreadID.String)
	}
	out["owner_refs"] = append([]string(nil), buckets.OwnerRefs...)
	out["document_refs"] = append([]string(nil), buckets.DocumentRefs...)
	out["board_refs"] = append([]string(nil), buckets.BoardRefs...)
	out["related_refs"] = append([]string(nil), buckets.RelatedRefs...)
	out["created_at"] = r.CreatedAt
	out["created_by"] = r.CreatedBy
	out["updated_at"] = r.UpdatedAt
	out["updated_by"] = r.UpdatedBy
	out["provenance"] = provenance

	for k, v := range ext {
		if _, skip := topicExtensionsMergeDenylist[k]; skip {
			continue
		}
		out[k] = v
	}

	if r.ArchivedAt.Valid && strings.TrimSpace(r.ArchivedAt.String) != "" {
		out["archived_at"] = r.ArchivedAt.String
		if r.ArchivedBy.Valid && strings.TrimSpace(r.ArchivedBy.String) != "" {
			out["archived_by"] = r.ArchivedBy.String
		}
	}
	if r.TrashedAt.Valid && strings.TrimSpace(r.TrashedAt.String) != "" {
		out["trashed_at"] = r.TrashedAt.String
		if r.TrashedBy.Valid && strings.TrimSpace(r.TrashedBy.String) != "" {
			out["trashed_by"] = r.TrashedBy.String
		}
		if r.TrashReason.Valid && strings.TrimSpace(r.TrashReason.String) != "" {
			out["trash_reason"] = r.TrashReason.String
		}
	}

	out["state"] = canonicalLifecycleState(r.ArchivedAt, r.TrashedAt)

	return out, nil
}

func (s *Store) ListTopics(ctx context.Context, filter TopicListFilter) ([]map[string]any, string, error) {
	if s == nil || s.db == nil {
		return nil, "", fmt.Errorf("primitives store database is not initialized")
	}
	if filter.Cursor != "" {
		if _, err := decodeCursor(filter.Cursor); err != nil {
			return nil, "", fmt.Errorf("%w: %v", ErrInvalidCursor, err)
		}
	}

	query, args := buildListTopicsQuery(filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("query topics: %w", err)
	}
	defer rows.Close()

	rowBuf := make([]topicRow, 0)
	for rows.Next() {
		var row topicRow
		if err := rows.Scan(
			&row.ID,
			&row.Title,
			&row.Summary,
			&row.ThreadID,
			&row.ExtensionsJSON,
			&row.ProvenanceJSON,
			&row.CreatedAt,
			&row.CreatedBy,
			&row.UpdatedAt,
			&row.UpdatedBy,
			&row.ArchivedAt,
			&row.ArchivedBy,
			&row.TrashedAt,
			&row.TrashedBy,
			&row.TrashReason,
		); err != nil {
			return nil, "", fmt.Errorf("scan topic row: %w", err)
		}
		rowBuf = append(rowBuf, row)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate topic rows: %w", err)
	}

	topicIDs := make([]string, 0, len(rowBuf))
	threadByTopic := make(map[string]string, len(rowBuf))
	for _, row := range rowBuf {
		topicIDs = append(topicIDs, row.ID)
		if row.ThreadID.Valid {
			threadByTopic[row.ID] = strings.TrimSpace(row.ThreadID.String)
		}
	}
	bucketMap, err := s.loadTopicRefBucketsBatch(ctx, topicIDs, threadByTopic)
	if err != nil {
		return nil, "", err
	}

	topics := make([]map[string]any, 0, len(rowBuf))
	for _, row := range rowBuf {
		topic, err := row.toMap(bucketMap[row.ID])
		if err != nil {
			return nil, "", err
		}
		topics = append(topics, topic)
	}

	var nextCursor string
	if filter.Limit != nil && len(topics) > *filter.Limit {
		topics = topics[:*filter.Limit]
		offset := 0
		if filter.Cursor != "" {
			offset, _ = decodeCursor(filter.Cursor)
		}
		nextCursor = encodeCursor(offset + *filter.Limit)
	}

	return topics, nextCursor, nil
}

func (s *Store) CreateTopic(ctx context.Context, actorID string, topic map[string]any) (TopicPatchResult, error) {
	if s == nil || s.db == nil {
		return TopicPatchResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return TopicPatchResult{}, ErrInvalidTopicRequest
	}
	if topic == nil {
		return TopicPatchResult{}, ErrInvalidTopicRequest
	}

	normalized, err := normalizeTopicInput(topic, true)
	if err != nil {
		return TopicPatchResult{}, err
	}

	topicID := strings.TrimSpace(anyStringValue(normalized["id"]))
	if topicID == "" {
		topicID = uuid.NewString()
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)

	primaryThreadID := strings.TrimSpace(anyStringValue(normalized["thread_id"]))
	if primaryThreadID == "" {
		primaryThreadID = uuid.NewString()
	}

	topicBody := cloneMap(normalized)
	delete(topicBody, "id")
	delete(topicBody, "created_at")
	delete(topicBody, "created_by")
	delete(topicBody, "updated_at")
	delete(topicBody, "updated_by")
	delete(topicBody, "thread_id")

	threadBody := buildTopicBackingThreadBody(topicID, normalized, primaryThreadID, actorID, now)

	topicSummaryCol := strings.TrimSpace(anyStringValue(topicBody["summary"]))
	topicExtensionsJSON, err := marshalTopicExtensionsJSON(topicBody)
	if err != nil {
		return TopicPatchResult{}, fmt.Errorf("marshal topic extensions: %w", err)
	}
	topicProvenance, topicProvenanceJSON, err := marshalProvenance(topicBody["provenance"], "marshal topic")
	if err != nil {
		return TopicPatchResult{}, err
	}
	_ = topicProvenance
	threadBodyJSON, err := json.Marshal(threadBody)
	if err != nil {
		return TopicPatchResult{}, fmt.Errorf("marshal topic backing thread body: %w", err)
	}
	threadProvenance, threadProvenanceJSON, err := marshalProvenance(threadBody["provenance"], "marshal topic backing thread")
	if err != nil {
		return TopicPatchResult{}, err
	}
	_ = threadProvenance

	topicTargets := combineTopicRefTargets(topicBody, primaryThreadID)
	threadTargets := typedRefEdgeTargets(refEdgeTypeRef, []string{"topic:" + topicID})

	title := strings.TrimSpace(anyStringValue(topicBody["title"]))

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TopicPatchResult{}, fmt.Errorf("begin topic create transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO topics(
			id, title, thread_id, summary, extensions_json, provenance_json,
			created_at, created_by, updated_at, updated_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		topicID,
		title,
		primaryThreadID,
		topicSummaryCol,
		topicExtensionsJSON,
		topicProvenanceJSON,
		now,
		actorID,
		now,
		actorID,
	); err != nil {
		if isUniqueViolation(err) {
			return TopicPatchResult{}, ErrConflict
		}
		return TopicPatchResult{}, fmt.Errorf("insert topic: %w", err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO threads(id, kind, thread_id, updated_at, updated_by, body_json, provenance_json)
		 VALUES (?, 'thread', ?, ?, ?, ?, ?)`,
		primaryThreadID,
		primaryThreadID,
		now,
		actorID,
		string(threadBodyJSON),
		threadProvenanceJSON,
	); err != nil {
		if isUniqueViolation(err) {
			return TopicPatchResult{}, ErrConflict
		}
		return TopicPatchResult{}, fmt.Errorf("insert topic backing thread: %w", err)
	}

	if err := replaceRefEdges(ctx, tx, "topic", topicID, topicTargets); err != nil {
		return TopicPatchResult{}, err
	}
	if err := replaceRefEdges(ctx, tx, "thread", primaryThreadID, threadTargets); err != nil {
		return TopicPatchResult{}, err
	}

	changedFields := sortedKeys(topicBody)
	changedFields = append(changedFields, "thread_id")
	changedFields = append(changedFields, "provenance")
	sort.Strings(changedFields)

	createEvent := map[string]any{
		"type":       "topic_created",
		"thread_id":  primaryThreadID,
		"refs":       []string{"topic:" + topicID},
		"summary":    "topic created",
		"payload":    map[string]any{"changed_fields": changedFields},
		"provenance": actorStatementProvenance(),
	}
	preparedEvent, err := prepareEventForInsert(actorID, createEvent)
	if err != nil {
		return TopicPatchResult{}, fmt.Errorf("prepare topic_created event: %w", err)
	}
	if err := insertPreparedEvent(ctx, tx, preparedEvent); err != nil {
		return TopicPatchResult{}, fmt.Errorf("emit topic_created event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return TopicPatchResult{}, fmt.Errorf("commit topic create transaction: %w", err)
	}

	topicOut := cloneMap(topicBody)
	topicOut["id"] = topicID
	topicOut["thread_id"] = primaryThreadID
	topicOut["state"] = "active"
	topicOut["created_at"] = now
	topicOut["created_by"] = actorID
	topicOut["updated_at"] = now
	topicOut["updated_by"] = actorID
	topicOut["provenance"] = topicProvenance

	return TopicPatchResult{
		Topic: topicOut,
		Event: preparedEvent.Body,
	}, nil
}

func (s *Store) GetTopic(ctx context.Context, topicID string) (map[string]any, error) {
	row, err := s.getTopicRow(ctx, topicID)
	if err != nil {
		return nil, err
	}
	primary := ""
	if row.ThreadID.Valid {
		primary = strings.TrimSpace(row.ThreadID.String)
	}
	buckets, err := s.loadTopicRefBuckets(ctx, topicID, primary)
	if err != nil {
		return nil, err
	}
	return row.toMap(buckets)
}

func (s *Store) PatchTopic(ctx context.Context, actorID string, topicID string, patch map[string]any, ifUpdatedAt *string) (TopicPatchResult, error) {
	if s == nil || s.db == nil {
		return TopicPatchResult{}, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return TopicPatchResult{}, ErrInvalidTopicRequest
	}
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return TopicPatchResult{}, ErrInvalidTopicRequest
	}
	if patch == nil || len(patch) == 0 {
		return TopicPatchResult{}, ErrInvalidTopicRequest
	}

	row, err := s.getTopicRow(ctx, topicID)
	if err != nil {
		return TopicPatchResult{}, err
	}
	if err := ensureUpdatedAtMatches(row.UpdatedAt, ifUpdatedAt); err != nil {
		return TopicPatchResult{}, err
	}

	primaryThreadForRefs := ""
	if row.ThreadID.Valid {
		primaryThreadForRefs = strings.TrimSpace(row.ThreadID.String)
	}
	refBuckets, err := s.loadTopicRefBuckets(ctx, topicID, primaryThreadForRefs)
	if err != nil {
		return TopicPatchResult{}, err
	}

	current, err := row.toMap(refBuckets)
	if err != nil {
		return TopicPatchResult{}, err
	}
	currentProvenance := cloneProvenance(current["provenance"])

	normalizedPatch, err := normalizeTopicInput(patch, false)
	if err != nil {
		return TopicPatchResult{}, err
	}

	bodyPatch := cloneMap(normalizedPatch)
	nextProvenance := cloneProvenance(currentProvenance)
	provenanceChanged := false
	if rawProvenance, hasProvenance := bodyPatch["provenance"]; hasProvenance {
		provenancePatch, ok := rawProvenance.(map[string]any)
		if !ok {
			return TopicPatchResult{}, fmt.Errorf("topic.provenance must be an object")
		}
		nextProvenance = cloneMap(provenancePatch)
		delete(bodyPatch, "provenance")
		provenanceChanged = !reflectDeepEqual(currentProvenance, nextProvenance)
	}

	if rawThreadID, exists := bodyPatch["thread_id"]; exists {
		if strings.TrimSpace(anyStringValue(rawThreadID)) == "" {
			return TopicPatchResult{}, ErrInvalidTopicRequest
		}
		currentThreadID := strings.TrimSpace(anyStringValue(current["thread_id"]))
		if currentThreadID != "" && currentThreadID != strings.TrimSpace(anyStringValue(rawThreadID)) {
			return TopicPatchResult{}, ErrInvalidTopicRequest
		}
	}

	changedFields := make([]string, 0, len(bodyPatch)+1)
	nextBody := cloneMap(current)
	delete(nextBody, "created_at")
	delete(nextBody, "created_by")
	delete(nextBody, "updated_at")
	delete(nextBody, "updated_by")
	for key, incoming := range bodyPatch {
		existing, exists := nextBody[key]
		if !exists || !reflectDeepEqual(existing, incoming) {
			changedFields = append(changedFields, key)
		}
		nextBody[key] = incoming
	}
	if provenanceChanged {
		changedFields = append(changedFields, "provenance")
	}
	sort.Strings(changedFields)

	nextTitle := strings.TrimSpace(anyStringValue(nextBody["title"]))
	nextSummary := strings.TrimSpace(anyStringValue(nextBody["summary"]))
	nextBackingThreadID := strings.TrimSpace(anyStringValue(nextBody["thread_id"]))

	nextTopicBody := cloneMap(nextBody)
	nextTopicBody["provenance"] = nextProvenance
	if nextBackingThreadID == "" {
		nextBackingThreadID = strings.TrimSpace(row.ThreadID.String)
	}
	nextTopicBody["thread_id"] = nextBackingThreadID
	delete(nextTopicBody, "thread_ref")

	now := time.Now().UTC().Format(time.RFC3339Nano)
	nextThreadBody := buildTopicBackingThreadBody(topicID, nextTopicBody, nextBackingThreadID, actorID, now)

	topicTargets := combineTopicRefTargets(nextTopicBody, nextBackingThreadID)
	threadTargets := typedRefEdgeTargets(refEdgeTypeRef, []string{"topic:" + topicID})

	topicExtensionsJSON, err := marshalTopicExtensionsJSON(stripTopicWriteOnlyFields(nextTopicBody))
	if err != nil {
		return TopicPatchResult{}, fmt.Errorf("marshal patched topic extensions: %w", err)
	}
	updatedProvenanceJSON, err := json.Marshal(nextProvenance)
	if err != nil {
		return TopicPatchResult{}, fmt.Errorf("marshal patched topic provenance: %w", err)
	}
	threadBodyJSON, err := json.Marshal(nextThreadBody)
	if err != nil {
		return TopicPatchResult{}, fmt.Errorf("marshal patched topic backing thread body: %w", err)
	}
	threadProvenanceJSON, err := json.Marshal(nextThreadBody["provenance"])
	if err != nil {
		return TopicPatchResult{}, fmt.Errorf("marshal patched topic backing thread provenance: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TopicPatchResult{}, fmt.Errorf("begin topic patch transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	updateQuery := `UPDATE topics
			SET title = ?, thread_id = ?, summary = ?, extensions_json = ?, provenance_json = ?, updated_at = ?, updated_by = ?
		  WHERE id = ?`
	updateArgs := []any{
		nextTitle,
		nextBackingThreadID,
		nextSummary,
		topicExtensionsJSON,
		string(updatedProvenanceJSON),
		now,
		actorID,
		topicID,
	}
	updateQuery, updateArgs = appendIfUpdatedAtClause(updateQuery, updateArgs, ifUpdatedAt)
	updateTopicResult, err := tx.ExecContext(ctx, updateQuery, updateArgs...)
	if err != nil {
		return TopicPatchResult{}, fmt.Errorf("update topic: %w", err)
	}
	if err := requireIfUpdatedAtRowsAffected(updateTopicResult, ifUpdatedAt, "patch topic"); err != nil {
		return TopicPatchResult{}, err
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE threads
			SET body_json = ?, provenance_json = ?, updated_at = ?, updated_by = ?
		  WHERE id = ?`,
		string(threadBodyJSON),
		string(threadProvenanceJSON),
		now,
		actorID,
		nextBackingThreadID,
	); err != nil {
		return TopicPatchResult{}, fmt.Errorf("update topic backing thread: %w", err)
	}

	if err := replaceRefEdges(ctx, tx, "topic", topicID, topicTargets); err != nil {
		return TopicPatchResult{}, err
	}
	if err := replaceRefEdges(ctx, tx, "thread", nextBackingThreadID, threadTargets); err != nil {
		return TopicPatchResult{}, err
	}

	eventType := "topic_updated"
	eventPayload := map[string]any{"changed_fields": changedFields}
	event := map[string]any{
		"type":       eventType,
		"thread_id":  nextBackingThreadID,
		"refs":       []string{"topic:" + topicID},
		"summary":    "topic updated",
		"payload":    eventPayload,
		"provenance": actorStatementProvenance(),
	}
	preparedEvent, err := prepareEventForInsert(actorID, event)
	if err != nil {
		return TopicPatchResult{}, fmt.Errorf("prepare topic event: %w", err)
	}
	if err := insertPreparedEvent(ctx, tx, preparedEvent); err != nil {
		return TopicPatchResult{}, fmt.Errorf("emit topic event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return TopicPatchResult{}, fmt.Errorf("commit topic patch transaction: %w", err)
	}

	nextBody["id"] = topicID
	delete(nextBody, "status")
	nextBody["title"] = nextTitle
	nextBody["summary"] = nextSummary
	nextBody["thread_id"] = nextBackingThreadID
	delete(nextBody, "thread_ref")
	nextBody["created_at"] = row.CreatedAt
	nextBody["created_by"] = row.CreatedBy
	nextBody["updated_at"] = now
	nextBody["updated_by"] = actorID
	nextBody["provenance"] = nextProvenance
	nextBody["state"] = canonicalLifecycleState(row.ArchivedAt, row.TrashedAt)

	return TopicPatchResult{
		Topic: nextBody,
		Event: preparedEvent.Body,
	}, nil
}

func (s *Store) ArchiveTopic(ctx context.Context, actorID, topicID string) (map[string]any, error) {
	return s.applyTopicLifecycle(ctx, actorID, topicID, "archive")
}

func (s *Store) UnarchiveTopic(ctx context.Context, actorID, topicID string) (map[string]any, error) {
	return s.applyTopicLifecycle(ctx, actorID, topicID, "unarchive")
}

func (s *Store) TrashTopic(ctx context.Context, actorID, topicID, reason string) (map[string]any, error) {
	return s.applyTopicLifecycleWithReason(ctx, actorID, topicID, "trash", reason)
}

func (s *Store) RestoreTopic(ctx context.Context, actorID, topicID string) (map[string]any, error) {
	return s.applyTopicLifecycle(ctx, actorID, topicID, "restore")
}

func (s *Store) getTopicRow(ctx context.Context, topicID string) (topicRow, error) {
	if s == nil || s.db == nil {
		return topicRow{}, fmt.Errorf("primitives store database is not initialized")
	}
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return topicRow{}, ErrNotFound
	}
	row := topicRow{}
	err := s.db.QueryRowContext(
		ctx,
		`SELECT id, title, summary, thread_id, extensions_json, provenance_json,
			created_at, created_by, updated_at, updated_by, archived_at, archived_by, trashed_at, trashed_by, trash_reason
		 FROM topics WHERE id = ?`,
		topicID,
	).Scan(&row.ID, &row.Title, &row.Summary, &row.ThreadID, &row.ExtensionsJSON, &row.ProvenanceJSON,
		&row.CreatedAt, &row.CreatedBy, &row.UpdatedAt, &row.UpdatedBy, &row.ArchivedAt, &row.ArchivedBy, &row.TrashedAt, &row.TrashedBy, &row.TrashReason)
	if errors.Is(err, sql.ErrNoRows) {
		return topicRow{}, ErrNotFound
	}
	if err != nil {
		return topicRow{}, fmt.Errorf("query topic row: %w", err)
	}
	return row, nil
}

func (s *Store) applyTopicLifecycle(ctx context.Context, actorID, topicID, action string) (map[string]any, error) {
	return s.applyTopicLifecycleWithReason(ctx, actorID, topicID, action, "")
}

func (s *Store) applyTopicLifecycleWithReason(ctx context.Context, actorID, topicID, action, reason string) (map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	actorID = strings.TrimSpace(actorID)
	if actorID == "" {
		return nil, ErrInvalidTopicRequest
	}
	topicID = strings.TrimSpace(topicID)
	if topicID == "" {
		return nil, ErrInvalidTopicRequest
	}

	row, err := s.getTopicRow(ctx, topicID)
	if err != nil {
		return nil, err
	}
	if row.TrashedAt.Valid && strings.TrimSpace(row.TrashedAt.String) != "" && action != "restore" {
		return nil, ErrAlreadyTrashed
	}

	primaryForRefs := ""
	if row.ThreadID.Valid {
		primaryForRefs = strings.TrimSpace(row.ThreadID.String)
	}
	lifeBuckets, err := s.loadTopicRefBuckets(ctx, topicID, primaryForRefs)
	if err != nil {
		return nil, err
	}

	current, err := row.toMap(lifeBuckets)
	if err != nil {
		return nil, err
	}
	primaryThreadID := strings.TrimSpace(row.ThreadID.String)
	now := time.Now().UTC().Format(time.RFC3339Nano)

	if row.TrashedAt.Valid && strings.TrimSpace(row.TrashedAt.String) != "" {
		switch action {
		case "restore":
			// handled below
		case "trash":
			return current, nil
		default:
			return nil, ErrAlreadyTrashed
		}
	}
	if row.ArchivedAt.Valid && strings.TrimSpace(row.ArchivedAt.String) != "" {
		switch action {
		case "archive":
			return current, nil
		case "unarchive":
			// handled below
		case "restore":
			// handled below
		case "trash":
			// handled below
		default:
			return nil, ErrInvalidTopicRequest
		}
	}
	if action == "unarchive" && (!row.ArchivedAt.Valid || strings.TrimSpace(row.ArchivedAt.String) == "") {
		return nil, ErrNotArchived
	}
	if action == "restore" && (!row.TrashedAt.Valid || strings.TrimSpace(row.TrashedAt.String) == "") {
		return nil, ErrNotTrashed
	}
	if action != "archive" && action != "unarchive" && action != "trash" && action != "restore" {
		return nil, ErrInvalidTopicRequest
	}

	updatedTopic := cloneMap(current)
	updatedTopic["updated_at"] = now
	updatedTopic["updated_by"] = actorID
	updatedTopic["provenance"] = cloneProvenance(current["provenance"])
	delete(updatedTopic, "created_at")
	delete(updatedTopic, "created_by")

	delete(updatedTopic, "status")
	if action == "trash" {
		delete(updatedTopic, "archived_at")
		delete(updatedTopic, "archived_by")
	}

	topicExtensionsJSON, err := marshalTopicExtensionsJSON(stripTopicWriteOnlyFields(updatedTopic))
	if err != nil {
		return nil, fmt.Errorf("marshal topic lifecycle extensions: %w", err)
	}
	summaryCol := strings.TrimSpace(anyStringValue(updatedTopic["summary"]))

	threadBody := buildTopicBackingThreadBody(topicID, updatedTopic, primaryThreadID, actorID, now)
	threadBody["updated_at"] = now
	threadBody["updated_by"] = actorID
	threadBodyJSON, err := json.Marshal(threadBody)
	if err != nil {
		return nil, fmt.Errorf("marshal topic lifecycle thread body: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin topic lifecycle transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	switch action {
	case "archive":
		if _, err := tx.ExecContext(ctx,
			`UPDATE topics SET archived_at = ?, archived_by = ?, trashed_at = NULL, trashed_by = NULL, trash_reason = NULL, summary = ?, extensions_json = ?, updated_at = ?, updated_by = ? WHERE id = ?`,
			now, actorID, summaryCol, topicExtensionsJSON, now, actorID, topicID,
		); err != nil {
			return nil, fmt.Errorf("archive topic: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE threads SET archived_at = ?, archived_by = ?, body_json = ?, provenance_json = ?, updated_at = ?, updated_by = ?
			  WHERE id = ?`,
			now, actorID, string(threadBodyJSON), inferredProvenanceJSON(), now, actorID,
			primaryThreadID,
		); err != nil {
			return nil, fmt.Errorf("archive topic backing thread: %w", err)
		}
	case "unarchive":
		if _, err := tx.ExecContext(ctx,
			`UPDATE topics SET archived_at = NULL, archived_by = NULL, summary = ?, extensions_json = ?, updated_at = ?, updated_by = ? WHERE id = ?`,
			summaryCol, topicExtensionsJSON, now, actorID, topicID,
		); err != nil {
			return nil, fmt.Errorf("unarchive topic: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE threads SET archived_at = NULL, archived_by = NULL, body_json = ?, provenance_json = ?, updated_at = ?, updated_by = ?
			  WHERE id = ?`,
			string(threadBodyJSON), inferredProvenanceJSON(), now, actorID,
			primaryThreadID,
		); err != nil {
			return nil, fmt.Errorf("unarchive topic backing thread: %w", err)
		}
	case "trash":
		if _, err := tx.ExecContext(ctx,
			`UPDATE topics SET trashed_at = ?, trashed_by = ?, trash_reason = ?, archived_at = NULL, archived_by = NULL, summary = ?, extensions_json = ?, updated_at = ?, updated_by = ? WHERE id = ?`,
			now, actorID, strings.TrimSpace(reason), summaryCol, topicExtensionsJSON, now, actorID, topicID,
		); err != nil {
			return nil, fmt.Errorf("trash topic: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE threads SET trashed_at = ?, trashed_by = ?, trash_reason = ?, archived_at = NULL, archived_by = NULL, body_json = ?, provenance_json = ?, updated_at = ?, updated_by = ?
			  WHERE id = ?`,
			now, actorID, strings.TrimSpace(reason), string(threadBodyJSON), inferredProvenanceJSON(), now, actorID,
			primaryThreadID,
		); err != nil {
			return nil, fmt.Errorf("trash topic backing thread: %w", err)
		}
	case "restore":
		if _, err := tx.ExecContext(ctx,
			`UPDATE topics SET trashed_at = NULL, trashed_by = NULL, trash_reason = NULL, summary = ?, extensions_json = ?, updated_at = ?, updated_by = ? WHERE id = ?`,
			summaryCol, topicExtensionsJSON, now, actorID, topicID,
		); err != nil {
			return nil, fmt.Errorf("restore topic: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE threads SET trashed_at = NULL, trashed_by = NULL, trash_reason = NULL, body_json = ?, provenance_json = ?, updated_at = ?, updated_by = ?
			  WHERE id = ?`,
			string(threadBodyJSON), inferredProvenanceJSON(), now, actorID,
			primaryThreadID,
		); err != nil {
			return nil, fmt.Errorf("restore topic backing thread: %w", err)
		}
	default:
		return nil, ErrInvalidTopicRequest
	}

	topicTargets := combineTopicRefTargets(updatedTopic, primaryThreadID)
	if err := replaceRefEdges(ctx, tx, "topic", topicID, topicTargets); err != nil {
		return nil, err
	}
	if err := replaceRefEdges(ctx, tx, "thread", primaryThreadID, typedRefEdgeTargets(refEdgeTypeRef, []string{"topic:" + topicID})); err != nil {
		return nil, err
	}

	event := map[string]any{
		"type":       topicLifecycleEventType(action),
		"thread_id":  primaryThreadID,
		"refs":       []string{"topic:" + topicID},
		"summary":    "topic " + action,
		"payload":    map[string]any{"action": action, "reason": strings.TrimSpace(reason)},
		"provenance": actorStatementProvenance(),
	}
	preparedEvent, err := prepareEventForInsert(actorID, event)
	if err != nil {
		return nil, fmt.Errorf("prepare topic lifecycle event: %w", err)
	}
	if err := insertPreparedEvent(ctx, tx, preparedEvent); err != nil {
		return nil, fmt.Errorf("emit topic lifecycle event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit topic lifecycle transaction: %w", err)
	}

	updatedTopic["updated_at"] = now
	updatedTopic["updated_by"] = actorID
	if action == "archive" {
		updatedTopic["archived_at"] = now
		updatedTopic["archived_by"] = actorID
	}
	if action == "unarchive" {
		delete(updatedTopic, "archived_at")
		delete(updatedTopic, "archived_by")
	}
	if action == "trash" {
		delete(updatedTopic, "archived_at")
		delete(updatedTopic, "archived_by")
		updatedTopic["trashed_at"] = now
		updatedTopic["trashed_by"] = actorID
		if strings.TrimSpace(reason) != "" {
			updatedTopic["trash_reason"] = strings.TrimSpace(reason)
		}
	}
	if action == "restore" {
		delete(updatedTopic, "trashed_at")
		delete(updatedTopic, "trashed_by")
		delete(updatedTopic, "trash_reason")
		delete(updatedTopic, "archived_at")
		delete(updatedTopic, "archived_by")
	}

	updatedTopic["state"] = LifecycleStateFromTimestampStrings(
		strings.TrimSpace(anyStringValue(updatedTopic["archived_at"])),
		strings.TrimSpace(anyStringValue(updatedTopic["trashed_at"])),
	)

	return updatedTopic, nil
}

func topicLifecycleEventType(action string) string {
	switch strings.TrimSpace(action) {
	case "archive":
		return "topic_archived"
	case "trash":
		return "topic_trashed"
	case "unarchive", "restore":
		return "topic_restored"
	default:
		return "topic_updated"
	}
}

func buildListTopicsQuery(filter TopicListFilter) (string, []any) {
	query := `SELECT id, title, summary, thread_id, extensions_json, provenance_json, created_at, created_by, updated_at, updated_by, archived_at, archived_by, trashed_at, trashed_by, trash_reason
		FROM topics
		WHERE 1=1`
	args := make([]any, 0, 8)
	state := strings.TrimSpace(filter.State)
	if state != "" {
		switch state {
		case "active":
			query += ` AND archived_at IS NULL AND trashed_at IS NULL`
		case "archived":
			query += ` AND archived_at IS NOT NULL AND trashed_at IS NULL`
		case "trashed":
			query += ` AND trashed_at IS NOT NULL`
		default:
			query += ` AND 1=0`
		}
	} else {
		if filter.TrashedOnly {
			query += ` AND trashed_at IS NOT NULL`
		} else if !filter.IncludeTrashed {
			query += ` AND trashed_at IS NULL`
		}
		if filter.ArchivedOnly {
			query += ` AND archived_at IS NOT NULL AND trashed_at IS NULL`
		} else if !filter.IncludeArchived {
			query += ` AND archived_at IS NULL`
		}
	}
	if q := strings.TrimSpace(filter.Query); q != "" {
		pattern := "%" + strings.ToLower(q) + "%"
		query += ` AND (LOWER(id) LIKE ? OR LOWER(COALESCE(title, '')) LIKE ? OR LOWER(COALESCE(summary, '')) LIKE ?)`
		args = append(args, pattern, pattern, pattern)
	}
	query += ` ORDER BY updated_at DESC, id ASC`
	if filter.Limit != nil && *filter.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, *filter.Limit+1)
		if filter.Cursor != "" {
			if offset, err := decodeCursor(filter.Cursor); err == nil && offset > 0 {
				query += ` OFFSET ?`
				args = append(args, offset)
			}
		}
	}
	return query, args
}

func normalizeTopicInput(topic map[string]any, createMode bool) (map[string]any, error) {
	out := cloneMap(topic)
	if _, ok := out["thread_ref"]; ok {
		return nil, ErrInvalidTopicRequest
	}
	if _, ok := out["primary_thread_ref"]; ok {
		return nil, ErrInvalidTopicRequest
	}

	if raw, exists := out["thread_id"]; exists && raw != nil {
		id := strings.TrimSpace(anyStringValue(raw))
		if id != "" && strings.Contains(id, "/") {
			return nil, ErrInvalidTopicRequest
		}
	}

	if id, exists := out["id"]; exists {
		if strings.TrimSpace(anyStringValue(id)) == "" {
			return nil, ErrInvalidTopicRequest
		}
	}
	if createMode && strings.TrimSpace(anyStringValue(out["title"])) == "" {
		return nil, ErrInvalidTopicRequest
	}
	if createMode && strings.TrimSpace(anyStringValue(out["summary"])) == "" {
		return nil, ErrInvalidTopicRequest
	}

	// Legacy clients may still send topic.type; it is not stored.
	delete(out, "type")
	delete(out, "status")

	for _, field := range []string{"owner_refs", "document_refs", "board_refs", "related_refs"} {
		if raw, exists := out[field]; exists && raw != nil {
			refs, err := normalizeStringSlice(raw)
			if err != nil {
				return nil, ErrInvalidTopicRequest
			}
			for _, ref := range refs {
				if _, _, ok := normalizeTypedRef(ref); !ok {
					return nil, ErrInvalidTopicRequest
				}
			}
			out[field] = refs
		} else if createMode {
			out[field] = []string{}
		}
	}

	if raw, exists := out["provenance"]; exists && raw != nil {
		if _, ok := raw.(map[string]any); !ok {
			return nil, ErrInvalidTopicRequest
		}
	} else if createMode {
		out["provenance"] = map[string]any{"sources": []string{"inferred"}}
	}

	return out, nil
}

func buildTopicBackingThreadBody(topicID string, topic map[string]any, threadID, actorID, now string) map[string]any {
	_ = actorID
	_ = now
	_ = topic
	body := map[string]any{
		"id":          strings.TrimSpace(threadID),
		"subject_ref": "topic:" + strings.TrimSpace(topicID),
		"provenance":  map[string]any{"sources": []string{"inferred"}},
	}
	return body
}

func stripTopicWriteOnlyFields(topic map[string]any) map[string]any {
	out := cloneMap(topic)
	delete(out, "created_at")
	delete(out, "created_by")
	delete(out, "updated_at")
	delete(out, "updated_by")
	return out
}

func combineTopicRefTargets(topic map[string]any, primaryThreadID string) []refEdgeTarget {
	targets := make([]refEdgeTarget, 0, 8)
	for _, field := range []string{"owner_refs", "document_refs", "board_refs", "related_refs"} {
		refs, _ := extractTopicRefs(topic[field])
		for _, t := range typedRefEdgeTargets(refEdgeTypeRef, refs) {
			t.MetadataJSON = topicRefFieldMetaJSON(field)
			targets = append(targets, t)
		}
	}
	if strings.TrimSpace(primaryThreadID) != "" {
		targets = append(targets, refEdgeTarget{
			TargetType:   "thread",
			TargetID:     strings.TrimSpace(primaryThreadID),
			EdgeType:     refEdgeTypeRef,
			MetadataJSON: `{"topic_ref_field":"_primary_thread"}`,
		})
	}
	return targets
}

func extractTopicRefs(raw any) ([]string, error) {
	if raw == nil {
		return nil, nil
	}
	refs, err := normalizeStringSlice(raw)
	if err != nil {
		return nil, err
	}
	return uniqueNormalizedStrings(refs), nil
}

func reflectDeepEqual(left, right any) bool {
	return reflect.DeepEqual(left, right)
}
