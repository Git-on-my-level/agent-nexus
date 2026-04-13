package primitives

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"
)

type DerivedInboxListFilter struct {
	ThreadID string
}

type DerivedInboxItem struct {
	ID            string
	ThreadID      string
	Category      string
	TriggerAt     string
	DueAt         string
	HasDueAt      bool
	SourceEventID string
	SourceCardID  string
	GeneratedAt   string
	Data          map[string]any
	SourceHash    string
}

type DerivedTopicProjection struct {
	ThreadID               string
	Stale                  bool
	LastActivityAt         string
	LatestStaleExceptionAt string
	InboxCount             int
	PendingDecisionCount   int
	RecommendationCount    int
	DecisionRequestCount   int
	DecisionCount          int
	ArtifactCount          int
	OpenCardCount          int
	DocumentCount          int
	GeneratedAt            string
	Data                   map[string]any
	SourceHash             string
}

type DerivedTopicProjectionDirtyEntry struct {
	ThreadID string
	DirtyAt  string
}

type DerivedTopicProjectionQueueStats struct {
	PendingCount  int
	OldestDirtyAt string
}

type TopicProjectionRefreshStatus struct {
	ThreadID               string
	DesiredGeneration      int64
	MaterializedGeneration int64
	InProgressGeneration   *int64
	QueuedAt               string
	StartedAt              string
	LastCompletedAt        string
	LastErrorAt            string
	LastErrorMessage       string
}

func (s TopicProjectionRefreshStatus) IsDirty() bool {
	return s.DesiredGeneration > s.MaterializedGeneration
}

func (s TopicProjectionRefreshStatus) InProgress() bool {
	return s.InProgressGeneration != nil
}

func (s *Store) ReplaceDerivedInboxItems(ctx context.Context, threadID string, items []DerivedInboxItem) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("primitives store database is not initialized")
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin derived inbox transaction: %w", err)
	}
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
	}()

	if _, err := tx.ExecContext(ctx, `DELETE FROM derived_inbox_items WHERE thread_id = ?`, threadID); err != nil {
		return fmt.Errorf("delete derived inbox items: %w", err)
	}

	for _, item := range items {
		if err := insertDerivedInboxItem(ctx, tx, threadID, item); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit derived inbox transaction: %w", err)
	}
	return nil
}

func insertDerivedInboxItem(ctx context.Context, exec eventExec, threadID string, item DerivedInboxItem) error {
	item.ID = strings.TrimSpace(item.ID)
	if item.ID == "" {
		return fmt.Errorf("derived inbox item id is required")
	}
	item.ThreadID = firstNonEmptyDerivedString(item.ThreadID, threadID)
	if item.ThreadID == "" {
		return fmt.Errorf("derived inbox item thread_id is required")
	}
	item.Category = strings.TrimSpace(item.Category)
	if item.Category == "" {
		return fmt.Errorf("derived inbox item category is required")
	}
	item.TriggerAt = strings.TrimSpace(item.TriggerAt)
	if item.TriggerAt == "" {
		return fmt.Errorf("derived inbox item trigger_at is required")
	}
	dataJSON, sourceHash, err := marshalDerivedJSON(item.Data, item.SourceHash)
	if err != nil {
		return fmt.Errorf("marshal derived inbox item %s: %w", item.ID, err)
	}

	if _, err := exec.ExecContext(
		ctx,
		`INSERT INTO derived_inbox_items(id, thread_id, category, trigger_at, due_at, has_due_at, source_event_id, source_card_id, generated_at, data_json, source_hash)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID,
		item.ThreadID,
		item.Category,
		item.TriggerAt,
		nullableString(item.DueAt),
		boolToInt(item.HasDueAt),
		nullableString(item.SourceEventID),
		nullableString(item.SourceCardID),
		firstNonEmptyDerivedString(item.GeneratedAt, time.Now().UTC().Format(time.RFC3339Nano)),
		dataJSON,
		sourceHash,
	); err != nil {
		return fmt.Errorf("insert derived inbox item %s: %w", item.ID, err)
	}
	return nil
}

func (s *Store) ListDerivedInboxItems(ctx context.Context, filter DerivedInboxListFilter) ([]DerivedInboxItem, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}

	query := `SELECT id, thread_id, category, trigger_at, due_at, has_due_at, source_event_id, source_card_id, generated_at, data_json, source_hash
		FROM derived_inbox_items`
	args := make([]any, 0, 1)
	clauses := make([]string, 0, 1)
	if threadID := strings.TrimSpace(filter.ThreadID); threadID != "" {
		clauses = append(clauses, "thread_id = ?")
		args = append(args, threadID)
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY trigger_at DESC, id ASC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query derived inbox items: %w", err)
	}
	defer rows.Close()

	items := make([]DerivedInboxItem, 0)
	for rows.Next() {
		item, err := scanDerivedInboxItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate derived inbox items: %w", err)
	}

	sort.SliceStable(items, func(i int, j int) bool {
		left := items[i]
		right := items[j]
		leftOrder := derivedInboxCategoryOrder(left.Category)
		rightOrder := derivedInboxCategoryOrder(right.Category)
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		if (left.Category == "risk_exception" || left.Category == "work_item_risk") &&
			(right.Category == "risk_exception" || right.Category == "work_item_risk") &&
			left.HasDueAt && right.HasDueAt && left.DueAt != right.DueAt {
			return left.DueAt < right.DueAt
		}
		if left.TriggerAt != right.TriggerAt {
			return left.TriggerAt > right.TriggerAt
		}
		return left.ID < right.ID
	})

	return items, nil
}

func (s *Store) GetDerivedInboxItem(ctx context.Context, id string) (DerivedInboxItem, error) {
	if s == nil || s.db == nil {
		return DerivedInboxItem{}, fmt.Errorf("primitives store database is not initialized")
	}

	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, thread_id, category, trigger_at, due_at, has_due_at, source_event_id, source_card_id, generated_at, data_json, source_hash
		 FROM derived_inbox_items WHERE id = ?`,
		strings.TrimSpace(id),
	)
	item, err := scanDerivedInboxItem(row)
	if err == sql.ErrNoRows {
		return DerivedInboxItem{}, ErrNotFound
	}
	if err != nil {
		return DerivedInboxItem{}, err
	}
	return item, nil
}

type scanDerivedInboxItemRower interface {
	Scan(dest ...any) error
}

func scanDerivedInboxItem(row scanDerivedInboxItemRower) (DerivedInboxItem, error) {
	var (
		item          DerivedInboxItem
		dueAt         sql.NullString
		hasDueAt      int
		sourceEventID sql.NullString
		sourceCard    sql.NullString
		dataJSON      string
		sourceHash    sql.NullString
	)
	if err := row.Scan(
		&item.ID,
		&item.ThreadID,
		&item.Category,
		&item.TriggerAt,
		&dueAt,
		&hasDueAt,
		&sourceEventID,
		&sourceCard,
		&item.GeneratedAt,
		&dataJSON,
		&sourceHash,
	); err != nil {
		return DerivedInboxItem{}, err
	}
	item.HasDueAt = hasDueAt != 0
	item.DueAt = dueAt.String
	item.SourceEventID = sourceEventID.String
	item.SourceCardID = sourceCard.String
	item.SourceHash = sourceHash.String
	if err := json.Unmarshal([]byte(dataJSON), &item.Data); err != nil {
		return DerivedInboxItem{}, fmt.Errorf("decode derived inbox item %s: %w", item.ID, err)
	}
	return item, nil
}

func (s *Store) PutDerivedTopicProjection(ctx context.Context, projection DerivedTopicProjection) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("primitives store database is not initialized")
	}
	projection.ThreadID = strings.TrimSpace(projection.ThreadID)
	if projection.ThreadID == "" {
		return fmt.Errorf("derived thread projection thread_id is required")
	}

	dataJSON, sourceHash, err := marshalDerivedJSON(projection.Data, projection.SourceHash)
	if err != nil {
		return fmt.Errorf("marshal derived thread projection %s: %w", projection.ThreadID, err)
	}

	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO derived_topic_views(
			thread_id, stale, last_activity_at, latest_stale_exception_at,
			inbox_count, pending_decision_count, recommendation_count, decision_request_count, decision_count,
			artifact_count, open_card_count, document_count, generated_at, data_json, source_hash
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(thread_id) DO UPDATE SET
			stale = excluded.stale,
			last_activity_at = excluded.last_activity_at,
			latest_stale_exception_at = excluded.latest_stale_exception_at,
			inbox_count = excluded.inbox_count,
			pending_decision_count = excluded.pending_decision_count,
			recommendation_count = excluded.recommendation_count,
			decision_request_count = excluded.decision_request_count,
			decision_count = excluded.decision_count,
			artifact_count = excluded.artifact_count,
			open_card_count = excluded.open_card_count,
			document_count = excluded.document_count,
			generated_at = excluded.generated_at,
			data_json = excluded.data_json,
			source_hash = excluded.source_hash`,
		projection.ThreadID,
		boolToInt(projection.Stale),
		nullableString(projection.LastActivityAt),
		nullableString(projection.LatestStaleExceptionAt),
		projection.InboxCount,
		projection.PendingDecisionCount,
		projection.RecommendationCount,
		projection.DecisionRequestCount,
		projection.DecisionCount,
		projection.ArtifactCount,
		projection.OpenCardCount,
		projection.DocumentCount,
		firstNonEmptyDerivedString(projection.GeneratedAt, time.Now().UTC().Format(time.RFC3339Nano)),
		dataJSON,
		sourceHash,
	)
	if err != nil {
		return fmt.Errorf("upsert derived thread projection %s: %w", projection.ThreadID, err)
	}
	return nil
}

func (s *Store) MarkDerivedTopicProjectionDirty(ctx context.Context, threadID string, dirtyAt string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("primitives store database is not initialized")
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	dirtyAt = firstNonEmptyDerivedString(dirtyAt, time.Now().UTC().Format(time.RFC3339Nano))

	_, err := s.db.ExecContext(
		ctx,
		`INSERT INTO derived_topic_dirty_queue(thread_id, dirty_at)
		 VALUES (?, ?)
		ON CONFLICT(thread_id) DO UPDATE SET
			dirty_at = CASE
				WHEN derived_topic_dirty_queue.dirty_at <= excluded.dirty_at THEN derived_topic_dirty_queue.dirty_at
				ELSE excluded.dirty_at
			END`,
		threadID,
		dirtyAt,
	)
	if err != nil {
		return fmt.Errorf("upsert derived thread dirty queue %s: %w", threadID, err)
	}
	return nil
}

func (s *Store) ClearDerivedTopicProjectionDirty(ctx context.Context, threadID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("primitives store database is not initialized")
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM derived_topic_dirty_queue WHERE thread_id = ?`, threadID); err != nil {
		return fmt.Errorf("delete derived thread dirty queue %s: %w", threadID, err)
	}
	return nil
}

func (s *Store) ListDerivedTopicProjectionDirtyEntries(ctx context.Context, limit int) ([]DerivedTopicProjectionDirtyEntry, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	if limit <= 0 {
		return []DerivedTopicProjectionDirtyEntry{}, nil
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT thread_id, dirty_at
		   FROM (
				SELECT q.thread_id, q.dirty_at
				  FROM derived_topic_dirty_queue q
				UNION ALL
				SELECT s.thread_id,
				       COALESCE(NULLIF(TRIM(s.queued_at), ''), NULLIF(TRIM(s.started_at), ''), NULLIF(TRIM(s.last_error_at), ''), s.updated_at) AS dirty_at
				  FROM topic_projection_refresh_status s
				 WHERE s.desired_generation > s.materialized_generation
				   AND NOT EXISTS (
						SELECT 1
						  FROM derived_topic_dirty_queue q
						 WHERE q.thread_id = s.thread_id
				   )
		   )
		  ORDER BY dirty_at ASC, thread_id ASC
		  LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query derived thread dirty queue: %w", err)
	}
	defer rows.Close()

	entries := make([]DerivedTopicProjectionDirtyEntry, 0, limit)
	for rows.Next() {
		var entry DerivedTopicProjectionDirtyEntry
		if err := rows.Scan(&entry.ThreadID, &entry.DirtyAt); err != nil {
			return nil, fmt.Errorf("scan derived thread dirty queue: %w", err)
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate derived thread dirty queue: %w", err)
	}
	return entries, nil
}

func (s *Store) GetDerivedTopicProjectionQueueStats(ctx context.Context) (DerivedTopicProjectionQueueStats, error) {
	if s == nil || s.db == nil {
		return DerivedTopicProjectionQueueStats{}, fmt.Errorf("primitives store database is not initialized")
	}

	var (
		stats         DerivedTopicProjectionQueueStats
		oldestDirtyAt sql.NullString
	)
	if err := s.db.QueryRowContext(
		ctx,
		`SELECT COUNT(*), MIN(dirty_at)
		   FROM (
				SELECT q.thread_id, q.dirty_at
				  FROM derived_topic_dirty_queue q
				UNION ALL
				SELECT s.thread_id,
				       COALESCE(NULLIF(TRIM(s.queued_at), ''), NULLIF(TRIM(s.started_at), ''), NULLIF(TRIM(s.last_error_at), ''), s.updated_at) AS dirty_at
				  FROM topic_projection_refresh_status s
				 WHERE s.desired_generation > s.materialized_generation
				   AND NOT EXISTS (
						SELECT 1
						  FROM derived_topic_dirty_queue q
						 WHERE q.thread_id = s.thread_id
				   )
		   ) pending`,
	).Scan(&stats.PendingCount, &oldestDirtyAt); err != nil {
		return DerivedTopicProjectionQueueStats{}, fmt.Errorf("query derived thread dirty queue stats: %w", err)
	}
	stats.OldestDirtyAt = oldestDirtyAt.String
	return stats, nil
}

func (s *Store) GetDerivedTopicProjection(ctx context.Context, threadID string) (DerivedTopicProjection, error) {
	if s == nil || s.db == nil {
		return DerivedTopicProjection{}, fmt.Errorf("primitives store database is not initialized")
	}
	row := s.db.QueryRowContext(
		ctx,
		`SELECT thread_id, stale, last_activity_at, latest_stale_exception_at,
		        inbox_count, pending_decision_count, recommendation_count, decision_request_count, decision_count,
		        artifact_count, open_card_count, document_count, generated_at, data_json, source_hash
		   FROM derived_topic_views
		  WHERE thread_id = ?`,
		strings.TrimSpace(threadID),
	)
	projection, err := scanDerivedTopicProjection(row)
	if err == sql.ErrNoRows {
		return DerivedTopicProjection{}, ErrNotFound
	}
	if err != nil {
		return DerivedTopicProjection{}, err
	}
	return projection, nil
}

func (s *Store) ListDerivedTopicProjections(ctx context.Context, threadIDs []string) (map[string]DerivedTopicProjection, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}

	threadIDs = uniqueNormalizedStrings(threadIDs)
	if len(threadIDs) == 0 {
		return map[string]DerivedTopicProjection{}, nil
	}

	placeholders := make([]string, 0, len(threadIDs))
	args := make([]any, 0, len(threadIDs))
	for _, threadID := range threadIDs {
		placeholders = append(placeholders, "?")
		args = append(args, threadID)
	}

	query := `SELECT thread_id, stale, last_activity_at, latest_stale_exception_at,
		        inbox_count, pending_decision_count, recommendation_count, decision_request_count, decision_count,
		        artifact_count, open_card_count, document_count, generated_at, data_json, source_hash
		   FROM derived_topic_views
		  WHERE thread_id IN (` + strings.Join(placeholders, ", ") + `)`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query derived thread projections: %w", err)
	}
	defer rows.Close()

	out := make(map[string]DerivedTopicProjection, len(threadIDs))
	for rows.Next() {
		projection, err := scanDerivedTopicProjection(rows)
		if err != nil {
			return nil, err
		}
		out[projection.ThreadID] = projection
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate derived thread projections: %w", err)
	}
	return out, nil
}

func scanDerivedTopicProjection(row scanDerivedInboxItemRower) (DerivedTopicProjection, error) {
	var (
		projection             DerivedTopicProjection
		stale                  int
		lastActivityAt         sql.NullString
		latestStaleExceptionAt sql.NullString
		dataJSON               string
		sourceHash             sql.NullString
	)
	if err := row.Scan(
		&projection.ThreadID,
		&stale,
		&lastActivityAt,
		&latestStaleExceptionAt,
		&projection.InboxCount,
		&projection.PendingDecisionCount,
		&projection.RecommendationCount,
		&projection.DecisionRequestCount,
		&projection.DecisionCount,
		&projection.ArtifactCount,
		&projection.OpenCardCount,
		&projection.DocumentCount,
		&projection.GeneratedAt,
		&dataJSON,
		&sourceHash,
	); err != nil {
		return DerivedTopicProjection{}, err
	}
	projection.Stale = stale != 0
	projection.LastActivityAt = lastActivityAt.String
	projection.LatestStaleExceptionAt = latestStaleExceptionAt.String
	projection.SourceHash = sourceHash.String
	if err := json.Unmarshal([]byte(dataJSON), &projection.Data); err != nil {
		return DerivedTopicProjection{}, fmt.Errorf("decode derived thread projection %s: %w", projection.ThreadID, err)
	}
	return projection, nil
}

func marshalDerivedJSON(data map[string]any, explicitHash string) (string, string, error) {
	if data == nil {
		data = map[string]any{}
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return "", "", err
	}
	sourceHash := strings.TrimSpace(explicitHash)
	if sourceHash == "" {
		sum := sha256.Sum256(encoded)
		sourceHash = hex.EncodeToString(sum[:])
	}
	return string(encoded), sourceHash, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func derivedInboxCategoryOrder(category string) int {
	switch strings.TrimSpace(category) {
	case "action_needed", "decision_needed":
		return 0
	case "risk_exception", "intervention_needed", "stale_topic", "work_item_risk":
		return 1
	case "attention", "document_attention":
		return 2
	default:
		return 99
	}
}

func firstNonEmptyDerivedString(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func (s *Store) MarkTopicProjectionsDirty(ctx context.Context, threadIDs []string, queuedAt time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("primitives store database is not initialized")
	}

	threadIDs = uniqueNormalizedStrings(threadIDs)
	if len(threadIDs) == 0 {
		return nil
	}

	queuedAtText := queuedAt.UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin projection refresh dirty transaction: %w", err)
	}
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
	}()

	for _, threadID := range threadIDs {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO derived_topic_dirty_queue(thread_id, dirty_at)
			 VALUES (?, ?)
			ON CONFLICT(thread_id) DO UPDATE SET
				dirty_at = CASE
					WHEN derived_topic_dirty_queue.dirty_at <= excluded.dirty_at THEN derived_topic_dirty_queue.dirty_at
					ELSE excluded.dirty_at
				END`,
			threadID,
			queuedAtText,
		); err != nil {
			return fmt.Errorf("mark derived thread projection %s dirty: %w", threadID, err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO topic_projection_refresh_status(
				thread_id, desired_generation, materialized_generation, queued_at, updated_at
			) VALUES (?, 1, 0, ?, ?)
			ON CONFLICT(thread_id) DO UPDATE SET
				desired_generation = topic_projection_refresh_status.desired_generation + 1,
				queued_at = CASE
					WHEN topic_projection_refresh_status.queued_at IS NULL OR topic_projection_refresh_status.queued_at > excluded.queued_at THEN excluded.queued_at
					ELSE topic_projection_refresh_status.queued_at
				END,
				updated_at = excluded.updated_at`,
			threadID,
			queuedAtText,
			queuedAtText,
		); err != nil {
			return fmt.Errorf("mark thread projection %s dirty: %w", threadID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit projection refresh dirty transaction: %w", err)
	}
	return nil
}

func (s *Store) RequeueTopicProjectionRefresh(ctx context.Context, threadID string, queuedAt time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("primitives store database is not initialized")
	}

	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}

	queuedAtText := queuedAt.UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin projection refresh requeue transaction: %w", err)
	}
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
	}()

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO derived_topic_dirty_queue(thread_id, dirty_at)
		 VALUES (?, ?)
		ON CONFLICT(thread_id) DO UPDATE SET
			dirty_at = CASE
				WHEN derived_topic_dirty_queue.dirty_at <= excluded.dirty_at THEN derived_topic_dirty_queue.dirty_at
				ELSE excluded.dirty_at
			END`,
		threadID,
		queuedAtText,
	); err != nil {
		return fmt.Errorf("requeue derived thread projection %s: %w", threadID, err)
	}

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO topic_projection_refresh_status(
			thread_id, desired_generation, materialized_generation, queued_at, updated_at
		) VALUES (?, 1, 0, ?, ?)
		ON CONFLICT(thread_id) DO UPDATE SET
			queued_at = CASE
				WHEN topic_projection_refresh_status.queued_at IS NULL OR topic_projection_refresh_status.queued_at > excluded.queued_at THEN excluded.queued_at
				ELSE topic_projection_refresh_status.queued_at
			END,
			updated_at = excluded.updated_at`,
		threadID,
		queuedAtText,
		queuedAtText,
	); err != nil {
		return fmt.Errorf("requeue thread projection %s: %w", threadID, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit projection refresh requeue transaction: %w", err)
	}
	return nil
}

func (s *Store) MarkTopicProjectionRefreshStarted(ctx context.Context, threadID string, startedAt time.Time) (int64, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("primitives store database is not initialized")
	}

	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return 0, nil
	}

	startedAtText := startedAt.UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin projection refresh started transaction: %w", err)
	}
	defer func() {
		if rbErr := tx.Rollback(); rbErr != nil {
			log.Printf("tx rollback failed: %v", rbErr)
		}
	}()

	if _, err := tx.ExecContext(
		ctx,
		`INSERT INTO topic_projection_refresh_status(
			thread_id, desired_generation, materialized_generation, updated_at
		) VALUES (?, 0, 0, ?)
		ON CONFLICT(thread_id) DO NOTHING`,
		threadID,
		startedAtText,
	); err != nil {
		return 0, fmt.Errorf("ensure thread projection %s refresh status: %w", threadID, err)
	}

	var (
		desiredGeneration      int64
		materializedGeneration int64
	)
	if err := tx.QueryRowContext(
		ctx,
		`SELECT desired_generation, materialized_generation
		   FROM topic_projection_refresh_status
		  WHERE thread_id = ?`,
		threadID,
	).Scan(&desiredGeneration, &materializedGeneration); err != nil {
		return 0, fmt.Errorf("load thread projection %s generations: %w", threadID, err)
	}
	if desiredGeneration <= materializedGeneration {
		if _, err := tx.ExecContext(
			ctx,
			`UPDATE topic_projection_refresh_status
			    SET in_progress_generation = NULL,
			        updated_at = ?
			  WHERE thread_id = ?`,
			startedAtText,
			threadID,
		); err != nil {
			return 0, fmt.Errorf("clear redundant thread projection %s refresh start: %w", threadID, err)
		}
		if err := tx.Commit(); err != nil {
			return 0, fmt.Errorf("commit redundant projection refresh started transaction: %w", err)
		}
		return 0, nil
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE topic_projection_refresh_status
		    SET in_progress_generation = ?,
		        started_at = ?,
		        last_error = NULL,
		        last_error_at = NULL,
		        updated_at = ?
		  WHERE thread_id = ?`,
		desiredGeneration,
		startedAtText,
		startedAtText,
		threadID,
	); err != nil {
		return 0, fmt.Errorf("mark thread projection %s refresh started: %w", threadID, err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit projection refresh started transaction: %w", err)
	}
	return desiredGeneration, nil
}

func (s *Store) GetTopicProjectionRefreshStatuses(ctx context.Context, threadIDs []string) (map[string]TopicProjectionRefreshStatus, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}

	threadIDs = uniqueNormalizedStrings(threadIDs)
	if len(threadIDs) == 0 {
		return map[string]TopicProjectionRefreshStatus{}, nil
	}

	placeholders := make([]string, 0, len(threadIDs))
	args := make([]any, 0, len(threadIDs))
	for _, threadID := range threadIDs {
		placeholders = append(placeholders, "?")
		args = append(args, threadID)
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT thread_id, desired_generation, materialized_generation, in_progress_generation, queued_at, started_at, completed_at, last_error_at, last_error
		   FROM topic_projection_refresh_status
		  WHERE thread_id IN (`+strings.Join(placeholders, ", ")+`)`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("query thread projection refresh status: %w", err)
	}
	defer rows.Close()

	out := make(map[string]TopicProjectionRefreshStatus, len(threadIDs))
	for rows.Next() {
		status, err := scanTopicProjectionRefreshStatus(rows)
		if err != nil {
			return nil, err
		}
		out[status.ThreadID] = status
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate thread projection refresh status: %w", err)
	}
	return out, nil
}

func (s *Store) MarkTopicProjectionRefreshSucceeded(ctx context.Context, threadID string, completedGeneration int64, completedAt time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("primitives store database is not initialized")
	}

	threadID = strings.TrimSpace(threadID)
	if threadID == "" || completedGeneration <= 0 {
		return nil
	}

	completedAtText := completedAt.UTC().Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(
		ctx,
		`INSERT INTO topic_projection_refresh_status(
			thread_id, desired_generation, materialized_generation, completed_at, updated_at
		) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(thread_id) DO UPDATE SET
			materialized_generation = CASE
				WHEN topic_projection_refresh_status.materialized_generation < excluded.materialized_generation THEN excluded.materialized_generation
				ELSE topic_projection_refresh_status.materialized_generation
			END,
			in_progress_generation = CASE
				WHEN COALESCE(topic_projection_refresh_status.in_progress_generation, 0) <= excluded.materialized_generation THEN NULL
				ELSE topic_projection_refresh_status.in_progress_generation
			END,
			queued_at = CASE
				WHEN topic_projection_refresh_status.desired_generation <= CASE
					WHEN topic_projection_refresh_status.materialized_generation < excluded.materialized_generation THEN excluded.materialized_generation
					ELSE topic_projection_refresh_status.materialized_generation
				END THEN NULL
				ELSE topic_projection_refresh_status.queued_at
			END,
			completed_at = excluded.completed_at,
			last_error = NULL,
			last_error_at = NULL,
			updated_at = excluded.updated_at`,
		threadID,
		completedGeneration,
		completedGeneration,
		completedAtText,
		completedAtText,
	); err != nil {
		return fmt.Errorf("mark thread projection %s refresh succeeded: %w", threadID, err)
	}
	return nil
}

func (s *Store) MarkTopicProjectionRefreshFailed(ctx context.Context, threadID string, failedGeneration int64, failedAt time.Time, message string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("primitives store database is not initialized")
	}

	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}

	failedAtText := failedAt.UTC().Format(time.RFC3339Nano)
	message = strings.TrimSpace(message)
	if _, err := s.db.ExecContext(
		ctx,
		`INSERT INTO topic_projection_refresh_status(
			thread_id, desired_generation, materialized_generation, last_error_at, last_error, updated_at
		) VALUES (?, 1, 0, ?, ?, ?)
		ON CONFLICT(thread_id) DO UPDATE SET
			in_progress_generation = CASE
				WHEN ? > 0 AND COALESCE(topic_projection_refresh_status.in_progress_generation, 0) <= ? THEN NULL
				ELSE topic_projection_refresh_status.in_progress_generation
			END,
			last_error_at = excluded.last_error_at,
			last_error = excluded.last_error,
			updated_at = excluded.updated_at`,
		threadID,
		failedAtText,
		message,
		failedAtText,
		failedGeneration,
		failedGeneration,
	); err != nil {
		return fmt.Errorf("mark thread projection %s refresh failed: %w", threadID, err)
	}
	return nil
}

func scanTopicProjectionRefreshStatus(row scanDerivedInboxItemRower) (TopicProjectionRefreshStatus, error) {
	var (
		status               TopicProjectionRefreshStatus
		inProgressGeneration sql.NullInt64
		queuedAt             sql.NullString
		startedAt            sql.NullString
		lastCompletedAt      sql.NullString
		lastErrorAt          sql.NullString
		lastError            sql.NullString
	)
	if err := row.Scan(
		&status.ThreadID,
		&status.DesiredGeneration,
		&status.MaterializedGeneration,
		&inProgressGeneration,
		&queuedAt,
		&startedAt,
		&lastCompletedAt,
		&lastErrorAt,
		&lastError,
	); err != nil {
		return TopicProjectionRefreshStatus{}, err
	}
	if inProgressGeneration.Valid {
		value := inProgressGeneration.Int64
		status.InProgressGeneration = &value
	}
	status.QueuedAt = queuedAt.String
	status.StartedAt = startedAt.String
	status.LastCompletedAt = lastCompletedAt.String
	status.LastErrorAt = lastErrorAt.String
	status.LastErrorMessage = lastError.String
	return status, nil
}
