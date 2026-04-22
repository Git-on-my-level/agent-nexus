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
	Type           sql.NullString
	Status         sql.NullString
	Title          sql.NullString
	Summary        sql.NullString
	ThreadID       sql.NullString
	BodyJSON       string
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

func (r topicRow) toMap() (map[string]any, error) {
	body := map[string]any{}
	if strings.TrimSpace(r.BodyJSON) != "" {
		if err := json.Unmarshal([]byte(r.BodyJSON), &body); err != nil {
			return nil, fmt.Errorf("decode topic body: %w", err)
		}
	}

	provenance := map[string]any{}
	if strings.TrimSpace(r.ProvenanceJSON) != "" {
		if err := json.Unmarshal([]byte(r.ProvenanceJSON), &provenance); err != nil {
			return nil, fmt.Errorf("decode topic provenance: %w", err)
		}
	}

	body["id"] = r.ID
	if _, has := body["type"]; !has {
		body["type"] = r.Type.String
	}
	if _, has := body["status"]; !has {
		body["status"] = r.Status.String
	}
	if _, has := body["title"]; !has {
		body["title"] = r.Title.String
	}
	if _, has := body["summary"]; !has {
		body["summary"] = r.Summary.String
	}
	coerceLegacyTopicPersistedBody(body)
	if r.ThreadID.Valid && strings.TrimSpace(r.ThreadID.String) != "" {
		body["thread_id"] = strings.TrimSpace(r.ThreadID.String)
	}
	delete(body, "thread_ref")
	for _, field := range []string{"owner_refs", "document_refs", "board_refs", "related_refs"} {
		if _, has := body[field]; !has {
			body[field] = []string{}
		}
	}
	body["created_at"] = r.CreatedAt
	body["created_by"] = r.CreatedBy
	body["updated_at"] = r.UpdatedAt
	body["updated_by"] = r.UpdatedBy
	body["provenance"] = provenance

	if r.ArchivedAt.Valid && strings.TrimSpace(r.ArchivedAt.String) != "" {
		body["archived_at"] = r.ArchivedAt.String
		if r.ArchivedBy.Valid && strings.TrimSpace(r.ArchivedBy.String) != "" {
			body["archived_by"] = r.ArchivedBy.String
		}
	}
	if r.TrashedAt.Valid && strings.TrimSpace(r.TrashedAt.String) != "" {
		body["trashed_at"] = r.TrashedAt.String
		if r.TrashedBy.Valid && strings.TrimSpace(r.TrashedBy.String) != "" {
			body["trashed_by"] = r.TrashedBy.String
		}
		if r.TrashReason.Valid && strings.TrimSpace(r.TrashReason.String) != "" {
			body["trash_reason"] = r.TrashReason.String
		}
	}

	return body, nil
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

	topics := make([]map[string]any, 0)
	for rows.Next() {
		var row topicRow
		if err := rows.Scan(
			&row.ID,
			&row.Type,
			&row.Status,
			&row.Title,
			&row.ThreadID,
			&row.BodyJSON,
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
		topic, err := row.toMap()
		if err != nil {
			return nil, "", err
		}
		topics = append(topics, topic)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate topic rows: %w", err)
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

	topicBodyJSON, err := json.Marshal(stripTopicBodyJSONFields(topicBody))
	if err != nil {
		return TopicPatchResult{}, fmt.Errorf("marshal topic body: %w", err)
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

	topicType := strings.TrimSpace(anyStringValue(topicBody["type"]))
	topicStatus := strings.TrimSpace(anyStringValue(topicBody["status"]))
	title := strings.TrimSpace(anyStringValue(topicBody["title"]))
	threadColumns := threadFilterColumnsForKind("thread", threadBody)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TopicPatchResult{}, fmt.Errorf("begin topic create transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO topics(
			id, title, status, type, thread_id, body_json, provenance_json,
			created_at, created_by, updated_at, updated_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		topicID,
		title,
		topicStatus,
		topicType,
		primaryThreadID,
		string(topicBodyJSON),
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
		`INSERT INTO threads(id, kind, thread_id, updated_at, updated_by, body_json, provenance_json, filter_status, filter_priority, filter_owner, filter_due_at, filter_cadence, filter_cadence_preset, filter_tags_json)
		 VALUES (?, 'thread', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		primaryThreadID,
		primaryThreadID,
		now,
		actorID,
		string(threadBodyJSON),
		threadProvenanceJSON,
		nullableString(threadColumns.Status),
		nullableString(threadColumns.Priority),
		nil,
		nil,
		nullableString(threadColumns.Cadence),
		nullableString(threadColumns.CadencePreset),
		threadColumns.TagsJSON,
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
	return row.toMap()
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

	current, err := row.toMap()
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

	nextTopicType := strings.TrimSpace(anyStringValue(nextBody["type"]))
	nextTopicStatus := strings.TrimSpace(anyStringValue(nextBody["status"]))
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

	topicBodyJSON, err := json.Marshal(stripTopicBodyJSONFields(stripTopicWriteOnlyFields(nextTopicBody)))
	if err != nil {
		return TopicPatchResult{}, fmt.Errorf("marshal patched topic body: %w", err)
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
	threadColumns := threadFilterColumnsForKind("thread", nextThreadBody)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return TopicPatchResult{}, fmt.Errorf("begin topic patch transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	updateQuery := `UPDATE topics
			SET title = ?, status = ?, type = ?, thread_id = ?, body_json = ?, provenance_json = ?, updated_at = ?, updated_by = ?
		  WHERE id = ?`
	updateArgs := []any{
		nextTitle,
		nextTopicStatus,
		nextTopicType,
		nextBackingThreadID,
		string(topicBodyJSON),
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
			SET body_json = ?, provenance_json = ?, updated_at = ?, updated_by = ?,
			    filter_status = ?, filter_priority = ?, filter_owner = ?, filter_due_at = ?,
			    filter_cadence = ?, filter_cadence_preset = ?, filter_tags_json = ?
		  WHERE id = ?`,
		string(threadBodyJSON),
		string(threadProvenanceJSON),
		now,
		actorID,
		nullableString(threadColumns.Status),
		nullableString(threadColumns.Priority),
		nil,
		nil,
		nullableString(threadColumns.Cadence),
		nullableString(threadColumns.CadencePreset),
		threadColumns.TagsJSON,
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
	if oldStatus := strings.TrimSpace(anyStringValue(current["status"])); oldStatus != nextTopicStatus {
		eventType = "topic_status_changed"
		eventPayload["from_status"] = oldStatus
		eventPayload["to_status"] = nextTopicStatus
	}
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
	nextBody["type"] = nextTopicType
	nextBody["status"] = nextTopicStatus
	nextBody["title"] = nextTitle
	nextBody["summary"] = nextSummary
	nextBody["thread_id"] = nextBackingThreadID
	delete(nextBody, "thread_ref")
	nextBody["created_at"] = row.CreatedAt
	nextBody["created_by"] = row.CreatedBy
	nextBody["updated_at"] = now
	nextBody["updated_by"] = actorID
	nextBody["provenance"] = nextProvenance

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
		`SELECT id, type, status, title, thread_id, body_json, provenance_json,
			created_at, created_by, updated_at, updated_by, archived_at, archived_by, trashed_at, trashed_by, trash_reason
		 FROM topics WHERE id = ?`,
		topicID,
	).Scan(&row.ID, &row.Type, &row.Status, &row.Title, &row.ThreadID, &row.BodyJSON, &row.ProvenanceJSON,
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

	current, err := row.toMap()
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

	switch action {
	case "archive", "trash":
		updatedTopic["status"] = "archived"
	case "unarchive", "restore":
		updatedTopic["status"] = "active"
	}
	if action == "trash" {
		delete(updatedTopic, "archived_at")
		delete(updatedTopic, "archived_by")
	}

	topicBodyJSON, err := json.Marshal(stripTopicBodyJSONFields(stripTopicWriteOnlyFields(updatedTopic)))
	if err != nil {
		return nil, fmt.Errorf("marshal topic lifecycle body: %w", err)
	}

	threadBody := buildTopicBackingThreadBody(topicID, updatedTopic, primaryThreadID, actorID, now)
	threadBody["updated_at"] = now
	threadBody["updated_by"] = actorID
	threadBodyJSON, err := json.Marshal(threadBody)
	if err != nil {
		return nil, fmt.Errorf("marshal topic lifecycle thread body: %w", err)
	}
	threadColumns := threadFilterColumnsForKind("thread", threadBody)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin topic lifecycle transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	switch action {
	case "archive":
		if _, err := tx.ExecContext(ctx,
			`UPDATE topics SET status = ?, archived_at = ?, archived_by = ?, trashed_at = NULL, trashed_by = NULL, trash_reason = NULL, body_json = ?, updated_at = ?, updated_by = ? WHERE id = ?`,
			"archived", now, actorID, string(topicBodyJSON), now, actorID, topicID,
		); err != nil {
			return nil, fmt.Errorf("archive topic: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE threads SET archived_at = ?, archived_by = ?, body_json = ?, provenance_json = ?, updated_at = ?, updated_by = ?,
			    filter_status = ?, filter_priority = ?, filter_owner = ?, filter_due_at = ?, filter_cadence = ?, filter_cadence_preset = ?, filter_tags_json = ?
			  WHERE id = ?`,
			now, actorID, string(threadBodyJSON), inferredProvenanceJSON(), now, actorID,
			nullableString(threadColumns.Status), nullableString(threadColumns.Priority), nil, nil, nullableString(threadColumns.Cadence), nullableString(threadColumns.CadencePreset), threadColumns.TagsJSON,
			primaryThreadID,
		); err != nil {
			return nil, fmt.Errorf("archive topic backing thread: %w", err)
		}
	case "unarchive":
		if _, err := tx.ExecContext(ctx,
			`UPDATE topics SET status = ?, archived_at = NULL, archived_by = NULL, body_json = ?, updated_at = ?, updated_by = ? WHERE id = ?`,
			"active", string(topicBodyJSON), now, actorID, topicID,
		); err != nil {
			return nil, fmt.Errorf("unarchive topic: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE threads SET archived_at = NULL, archived_by = NULL, body_json = ?, provenance_json = ?, updated_at = ?, updated_by = ?,
			    filter_status = ?, filter_priority = ?, filter_owner = ?, filter_due_at = ?, filter_cadence = ?, filter_cadence_preset = ?, filter_tags_json = ?
			  WHERE id = ?`,
			string(threadBodyJSON), inferredProvenanceJSON(), now, actorID,
			nullableString(threadColumns.Status), nullableString(threadColumns.Priority), nil, nil, nullableString(threadColumns.Cadence), nullableString(threadColumns.CadencePreset), threadColumns.TagsJSON,
			primaryThreadID,
		); err != nil {
			return nil, fmt.Errorf("unarchive topic backing thread: %w", err)
		}
	case "trash":
		if _, err := tx.ExecContext(ctx,
			`UPDATE topics SET status = ?, trashed_at = ?, trashed_by = ?, trash_reason = ?, archived_at = NULL, archived_by = NULL, body_json = ?, updated_at = ?, updated_by = ? WHERE id = ?`,
			"archived", now, actorID, strings.TrimSpace(reason), string(topicBodyJSON), now, actorID, topicID,
		); err != nil {
			return nil, fmt.Errorf("trash topic: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE threads SET trashed_at = ?, trashed_by = ?, trash_reason = ?, archived_at = NULL, archived_by = NULL, body_json = ?, provenance_json = ?, updated_at = ?, updated_by = ?,
			    filter_status = ?, filter_priority = ?, filter_owner = ?, filter_due_at = ?, filter_cadence = ?, filter_cadence_preset = ?, filter_tags_json = ?
			  WHERE id = ?`,
			now, actorID, strings.TrimSpace(reason), string(threadBodyJSON), inferredProvenanceJSON(), now, actorID,
			nullableString(threadColumns.Status), nullableString(threadColumns.Priority), nil, nil, nullableString(threadColumns.Cadence), nullableString(threadColumns.CadencePreset), threadColumns.TagsJSON,
			primaryThreadID,
		); err != nil {
			return nil, fmt.Errorf("trash topic backing thread: %w", err)
		}
	case "restore":
		if _, err := tx.ExecContext(ctx,
			`UPDATE topics SET status = ?, trashed_at = NULL, trashed_by = NULL, trash_reason = NULL, body_json = ?, updated_at = ?, updated_by = ? WHERE id = ?`,
			"active", string(topicBodyJSON), now, actorID, topicID,
		); err != nil {
			return nil, fmt.Errorf("restore topic: %w", err)
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE threads SET trashed_at = NULL, trashed_by = NULL, trash_reason = NULL, body_json = ?, provenance_json = ?, updated_at = ?, updated_by = ?,
			    filter_status = ?, filter_priority = ?, filter_owner = ?, filter_due_at = ?, filter_cadence = ?, filter_cadence_preset = ?, filter_tags_json = ?
			  WHERE id = ?`,
			string(threadBodyJSON), inferredProvenanceJSON(), now, actorID,
			nullableString(threadColumns.Status), nullableString(threadColumns.Priority), nil, nil, nullableString(threadColumns.Cadence), nullableString(threadColumns.CadencePreset), threadColumns.TagsJSON,
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
		"type":       "topic_updated",
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

	return updatedTopic, nil
}

func buildListTopicsQuery(filter TopicListFilter) (string, []any) {
	query := `SELECT id, type, status, title, thread_id, body_json, provenance_json, created_at, created_by, updated_at, updated_by, archived_at, archived_by, trashed_at, trashed_by, trash_reason
		FROM topics
		WHERE 1=1`
	args := make([]any, 0, 8)
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
	if topicType := strings.TrimSpace(filter.Type); topicType != "" {
		query += ` AND type = ?`
		args = append(args, topicType)
	}
	if status := strings.TrimSpace(filter.Status); status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}
	if q := strings.TrimSpace(filter.Query); q != "" {
		// summary is stored in body_json, not a topics table column
		pattern := "%" + strings.ToLower(q) + "%"
		query += ` AND (LOWER(id) LIKE ? OR LOWER(COALESCE(title, '')) LIKE ? OR LOWER(COALESCE(json_extract(body_json, '$.summary'), '')) LIKE ?)`
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

// coerceLegacyTopicPersistedBody upgrades historical topic JSON bodies to thread_id and drops legacy keys.
func coerceLegacyTopicPersistedBody(out map[string]any) {
	legacyRef := "primary" + "_thread_ref"
	legacyID := "primary" + "_thread_id"
	if v, ok := out[legacyRef]; ok && v != nil {
		refStr := strings.TrimSpace(anyStringValue(v))
		if refStr != "" {
			if prefix, id, okRef := splitTypedRef(refStr); okRef && prefix == "thread" && strings.TrimSpace(id) != "" {
				if _, has := out["thread_id"]; !has || strings.TrimSpace(anyStringValue(out["thread_id"])) == "" {
					out["thread_id"] = strings.TrimSpace(id)
				}
			}
		}
		delete(out, legacyRef)
	}
	if v, ok := out[legacyID]; ok && v != nil {
		id := strings.TrimSpace(anyStringValue(v))
		if id != "" {
			if _, has := out["thread_id"]; !has || strings.TrimSpace(anyStringValue(out["thread_id"])) == "" {
				out["thread_id"] = id
			}
		}
		delete(out, legacyID)
	}
	if raw, exists := out["thread_ref"]; exists && raw != nil {
		refStr := strings.TrimSpace(anyStringValue(raw))
		if refStr != "" {
			if prefix, id, okRef := splitTypedRef(refStr); okRef && prefix == "thread" && strings.TrimSpace(id) != "" {
				if _, has := out["thread_id"]; !has || strings.TrimSpace(anyStringValue(out["thread_id"])) == "" {
					out["thread_id"] = strings.TrimSpace(id)
				}
			}
		}
		delete(out, "thread_ref")
	}
}

func stripTopicBodyJSONFields(body map[string]any) map[string]any {
	out := cloneMap(body)
	delete(out, "thread_ref")
	delete(out, "thread_id")
	return out
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

	topicType := strings.TrimSpace(anyStringValue(out["type"]))
	if topicType != "" && !isTopicType(topicType) {
		return nil, ErrInvalidTopicRequest
	}
	topicStatus := strings.TrimSpace(anyStringValue(out["status"]))
	if topicStatus != "" && !isTopicStatus(topicStatus) {
		return nil, ErrInvalidTopicRequest
	}

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
	title := strings.TrimSpace(anyStringValue(topic["title"]))
	status := "active"
	if strings.TrimSpace(anyStringValue(topic["archived_at"])) != "" || strings.TrimSpace(anyStringValue(topic["trashed_at"])) != "" || strings.EqualFold(strings.TrimSpace(anyStringValue(topic["status"])), "archived") {
		status = "archived"
	}
	body := map[string]any{
		"id":          strings.TrimSpace(threadID),
		"subject_ref": "topic:" + strings.TrimSpace(topicID),
		"title":       title,
		"status":      status,
		"priority":    "p2",
		"tags":        []string{},
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
		targets = append(targets, typedRefEdgeTargets(refEdgeTypeRef, refs)...)
	}
	if strings.TrimSpace(primaryThreadID) != "" {
		targets = append(targets, refEdgeTarget{
			TargetType: "thread",
			TargetID:   strings.TrimSpace(primaryThreadID),
			EdgeType:   refEdgeTypeRef,
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

func isTopicType(value string) bool {
	switch strings.TrimSpace(value) {
	case "case", "process", "relationship", "initiative", "objective", "decision", "incident", "risk", "request", "note", "other":
		return true
	default:
		return false
	}
}

func isTopicStatus(value string) bool {
	switch strings.TrimSpace(value) {
	case "proposed", "active", "paused", "blocked", "resolved", "closed", "archived":
		return true
	default:
		return false
	}
}

func reflectDeepEqual(left, right any) bool {
	return reflect.DeepEqual(left, right)
}
