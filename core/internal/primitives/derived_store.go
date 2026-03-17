package primitives

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type DerivedInboxListFilter struct {
	ThreadID string
}

type DerivedInboxItem struct {
	ID                 string
	ThreadID           string
	Category           string
	TriggerAt          string
	DueAt              string
	HasDueAt           bool
	SourceEventID      string
	SourceCommitmentID string
	GeneratedAt        string
	Data               map[string]any
	SourceHash         string
}

type DerivedThreadProjection struct {
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
	OpenCommitmentCount    int
	DocumentCount          int
	GeneratedAt            string
	Data                   map[string]any
	SourceHash             string
}

type ThreadProjectionRefreshStatus struct {
	ThreadID         string
	IsDirty          bool
	InProgress       bool
	QueuedAt         string
	StartedAt        string
	LastCompletedAt  string
	LastErrorAt      string
	LastErrorMessage string
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
		_ = tx.Rollback()
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
		`INSERT INTO derived_inbox_items(id, thread_id, category, trigger_at, due_at, has_due_at, source_event_id, source_commitment_id, generated_at, data_json, source_hash)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID,
		item.ThreadID,
		item.Category,
		item.TriggerAt,
		nullableString(item.DueAt),
		boolToInt(item.HasDueAt),
		nullableString(item.SourceEventID),
		nullableString(item.SourceCommitmentID),
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

	query := `SELECT id, thread_id, category, trigger_at, due_at, has_due_at, source_event_id, source_commitment_id, generated_at, data_json, source_hash
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
		if left.Category == "commitment_risk" && right.Category == "commitment_risk" && left.HasDueAt && right.HasDueAt && left.DueAt != right.DueAt {
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
		`SELECT id, thread_id, category, trigger_at, due_at, has_due_at, source_event_id, source_commitment_id, generated_at, data_json, source_hash
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
		item             DerivedInboxItem
		dueAt            sql.NullString
		hasDueAt         int
		sourceEventID    sql.NullString
		sourceCommitment sql.NullString
		dataJSON         string
		sourceHash       sql.NullString
	)
	if err := row.Scan(
		&item.ID,
		&item.ThreadID,
		&item.Category,
		&item.TriggerAt,
		&dueAt,
		&hasDueAt,
		&sourceEventID,
		&sourceCommitment,
		&item.GeneratedAt,
		&dataJSON,
		&sourceHash,
	); err != nil {
		return DerivedInboxItem{}, err
	}
	item.HasDueAt = hasDueAt != 0
	item.DueAt = dueAt.String
	item.SourceEventID = sourceEventID.String
	item.SourceCommitmentID = sourceCommitment.String
	item.SourceHash = sourceHash.String
	if err := json.Unmarshal([]byte(dataJSON), &item.Data); err != nil {
		return DerivedInboxItem{}, fmt.Errorf("decode derived inbox item %s: %w", item.ID, err)
	}
	return item, nil
}

func (s *Store) PutDerivedThreadProjection(ctx context.Context, projection DerivedThreadProjection) error {
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
		`INSERT INTO derived_thread_views(
			thread_id, stale, last_activity_at, latest_stale_exception_at,
			inbox_count, pending_decision_count, recommendation_count, decision_request_count, decision_count,
			artifact_count, open_commitment_count, document_count, generated_at, data_json, source_hash
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
			open_commitment_count = excluded.open_commitment_count,
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
		projection.OpenCommitmentCount,
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

func (s *Store) GetDerivedThreadProjection(ctx context.Context, threadID string) (DerivedThreadProjection, error) {
	if s == nil || s.db == nil {
		return DerivedThreadProjection{}, fmt.Errorf("primitives store database is not initialized")
	}
	row := s.db.QueryRowContext(
		ctx,
		`SELECT thread_id, stale, last_activity_at, latest_stale_exception_at,
		        inbox_count, pending_decision_count, recommendation_count, decision_request_count, decision_count,
		        artifact_count, open_commitment_count, document_count, generated_at, data_json, source_hash
		   FROM derived_thread_views
		  WHERE thread_id = ?`,
		strings.TrimSpace(threadID),
	)
	projection, err := scanDerivedThreadProjection(row)
	if err == sql.ErrNoRows {
		return DerivedThreadProjection{}, ErrNotFound
	}
	if err != nil {
		return DerivedThreadProjection{}, err
	}
	return projection, nil
}

func (s *Store) ListDerivedThreadProjections(ctx context.Context, threadIDs []string) (map[string]DerivedThreadProjection, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}

	threadIDs = uniqueNormalizedStrings(threadIDs)
	if len(threadIDs) == 0 {
		return map[string]DerivedThreadProjection{}, nil
	}

	placeholders := make([]string, 0, len(threadIDs))
	args := make([]any, 0, len(threadIDs))
	for _, threadID := range threadIDs {
		placeholders = append(placeholders, "?")
		args = append(args, threadID)
	}

	query := `SELECT thread_id, stale, last_activity_at, latest_stale_exception_at,
		        inbox_count, pending_decision_count, recommendation_count, decision_request_count, decision_count,
		        artifact_count, open_commitment_count, document_count, generated_at, data_json, source_hash
		   FROM derived_thread_views
		  WHERE thread_id IN (` + strings.Join(placeholders, ", ") + `)`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query derived thread projections: %w", err)
	}
	defer rows.Close()

	out := make(map[string]DerivedThreadProjection, len(threadIDs))
	for rows.Next() {
		projection, err := scanDerivedThreadProjection(rows)
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

func scanDerivedThreadProjection(row scanDerivedInboxItemRower) (DerivedThreadProjection, error) {
	var (
		projection             DerivedThreadProjection
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
		&projection.OpenCommitmentCount,
		&projection.DocumentCount,
		&projection.GeneratedAt,
		&dataJSON,
		&sourceHash,
	); err != nil {
		return DerivedThreadProjection{}, err
	}
	projection.Stale = stale != 0
	projection.LastActivityAt = lastActivityAt.String
	projection.LatestStaleExceptionAt = latestStaleExceptionAt.String
	projection.SourceHash = sourceHash.String
	if err := json.Unmarshal([]byte(dataJSON), &projection.Data); err != nil {
		return DerivedThreadProjection{}, fmt.Errorf("decode derived thread projection %s: %w", projection.ThreadID, err)
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
	case "decision_needed":
		return 0
	case "exception":
		return 1
	case "commitment_risk":
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

func (s *Store) MarkThreadProjectionsDirty(ctx context.Context, threadIDs []string, queuedAt time.Time) error {
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
		_ = tx.Rollback()
	}()

	for _, threadID := range threadIDs {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO thread_projection_refresh_status(
				thread_id, is_dirty, in_progress, queued_at, updated_at
			) VALUES (?, 1, 0, ?, ?)
			ON CONFLICT(thread_id) DO UPDATE SET
				is_dirty = 1,
				queued_at = excluded.queued_at,
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

func (s *Store) GetThreadProjectionRefreshStatuses(ctx context.Context, threadIDs []string) (map[string]ThreadProjectionRefreshStatus, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}

	threadIDs = uniqueNormalizedStrings(threadIDs)
	if len(threadIDs) == 0 {
		return map[string]ThreadProjectionRefreshStatus{}, nil
	}

	placeholders := make([]string, 0, len(threadIDs))
	args := make([]any, 0, len(threadIDs))
	for _, threadID := range threadIDs {
		placeholders = append(placeholders, "?")
		args = append(args, threadID)
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT thread_id, is_dirty, in_progress, queued_at, started_at, completed_at, last_error_at, last_error
		   FROM thread_projection_refresh_status
		  WHERE thread_id IN (`+strings.Join(placeholders, ", ")+`)`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("query thread projection refresh status: %w", err)
	}
	defer rows.Close()

	out := make(map[string]ThreadProjectionRefreshStatus, len(threadIDs))
	for rows.Next() {
		status, err := scanThreadProjectionRefreshStatus(rows)
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

func (s *Store) ClaimNextDirtyThreadProjection(ctx context.Context, startedAt time.Time) (ThreadProjectionRefreshStatus, bool, error) {
	if s == nil || s.db == nil {
		return ThreadProjectionRefreshStatus{}, false, fmt.Errorf("primitives store database is not initialized")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ThreadProjectionRefreshStatus{}, false, fmt.Errorf("begin claim thread projection refresh transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	row := tx.QueryRowContext(
		ctx,
		`SELECT thread_id, is_dirty, in_progress, queued_at, started_at, completed_at, last_error_at, last_error
		   FROM thread_projection_refresh_status
		  WHERE is_dirty = 1 AND in_progress = 0
		  ORDER BY queued_at ASC, thread_id ASC
		  LIMIT 1`,
	)
	status, err := scanThreadProjectionRefreshStatus(row)
	if err == sql.ErrNoRows {
		return ThreadProjectionRefreshStatus{}, false, nil
	}
	if err != nil {
		return ThreadProjectionRefreshStatus{}, false, err
	}

	startedAtText := startedAt.UTC().Format(time.RFC3339Nano)
	result, err := tx.ExecContext(
		ctx,
		`UPDATE thread_projection_refresh_status
		    SET is_dirty = 0,
		        in_progress = 1,
		        started_at = ?,
		        updated_at = ?
		  WHERE thread_id = ? AND is_dirty = 1 AND in_progress = 0`,
		startedAtText,
		startedAtText,
		status.ThreadID,
	)
	if err != nil {
		return ThreadProjectionRefreshStatus{}, false, fmt.Errorf("claim thread projection refresh %s: %w", status.ThreadID, err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return ThreadProjectionRefreshStatus{}, false, fmt.Errorf("read thread projection refresh rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return ThreadProjectionRefreshStatus{}, false, nil
	}
	if err := tx.Commit(); err != nil {
		return ThreadProjectionRefreshStatus{}, false, fmt.Errorf("commit claim thread projection refresh transaction: %w", err)
	}

	status.IsDirty = false
	status.InProgress = true
	status.StartedAt = startedAtText
	return status, true, nil
}

func (s *Store) MarkThreadProjectionRefreshSucceeded(ctx context.Context, threadID string, completedAt time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("primitives store database is not initialized")
	}

	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return nil
	}

	completedAtText := completedAt.UTC().Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(
		ctx,
		`INSERT INTO thread_projection_refresh_status(
			thread_id, is_dirty, in_progress, completed_at, updated_at
		) VALUES (?, 0, 0, ?, ?)
		ON CONFLICT(thread_id) DO UPDATE SET
			in_progress = 0,
			completed_at = excluded.completed_at,
			last_error = NULL,
			last_error_at = NULL,
			updated_at = excluded.updated_at`,
		threadID,
		completedAtText,
		completedAtText,
	); err != nil {
		return fmt.Errorf("mark thread projection %s refresh succeeded: %w", threadID, err)
	}
	return nil
}

func (s *Store) MarkThreadProjectionRefreshFailed(ctx context.Context, threadID string, failedAt time.Time, message string) error {
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
		`INSERT INTO thread_projection_refresh_status(
			thread_id, is_dirty, in_progress, last_error_at, last_error, updated_at
		) VALUES (?, 0, 0, ?, ?, ?)
		ON CONFLICT(thread_id) DO UPDATE SET
			in_progress = 0,
			last_error_at = excluded.last_error_at,
			last_error = excluded.last_error,
			updated_at = excluded.updated_at`,
		threadID,
		failedAtText,
		message,
		failedAtText,
	); err != nil {
		return fmt.Errorf("mark thread projection %s refresh failed: %w", threadID, err)
	}
	return nil
}

func scanThreadProjectionRefreshStatus(row scanDerivedInboxItemRower) (ThreadProjectionRefreshStatus, error) {
	var (
		status          ThreadProjectionRefreshStatus
		isDirty         int
		inProgress      int
		queuedAt        sql.NullString
		startedAt       sql.NullString
		lastCompletedAt sql.NullString
		lastErrorAt     sql.NullString
		lastError       sql.NullString
	)
	if err := row.Scan(
		&status.ThreadID,
		&isDirty,
		&inProgress,
		&queuedAt,
		&startedAt,
		&lastCompletedAt,
		&lastErrorAt,
		&lastError,
	); err != nil {
		return ThreadProjectionRefreshStatus{}, err
	}
	status.IsDirty = isDirty != 0
	status.InProgress = inProgress != 0
	status.QueuedAt = queuedAt.String
	status.StartedAt = startedAt.String
	status.LastCompletedAt = lastCompletedAt.String
	status.LastErrorAt = lastErrorAt.String
	status.LastErrorMessage = lastError.String
	return status, nil
}
