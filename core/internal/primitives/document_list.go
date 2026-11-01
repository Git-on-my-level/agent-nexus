// Document list path: query construction, list enrichment with head revision
// character counts, and the list payload content bounds.

package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"unicode/utf8"

	"golang.org/x/sync/errgroup"

	"agent-nexus-core/internal/blob"
)

func buildListDocumentsQuery(filter DocumentListFilter) (string, []any) {
	inner := `SELECT d.id, d.handle, d.thread_id, d.title, d.summary, d.source, d.tags_json, d.hosts_json, d.verified_at, d.slug, d.supersedes_json,
		d.refs_json, d.provenance_json,
		d.head_revision_id, d.head_revision_number, d.created_at, d.created_by, d.updated_at, d.updated_by,
		d.trashed_at, d.trashed_by, d.trash_reason,
		d.archived_at, d.archived_by,
		dr.artifact_id, a.content_type, dr.created_at, dr.created_by
		FROM documents d
		LEFT JOIN document_revisions dr ON dr.revision_id = d.head_revision_id
		LEFT JOIN artifacts a ON a.id = dr.artifact_id`
	args := make([]any, 0, 6)
	var conditions []string
	if filter.ids != nil {
		raw, _ := json.Marshal(filter.ids)
		conditions = append(conditions, `(d.id IN (SELECT value FROM json_each(?)) OR d.handle IN (SELECT value FROM json_each(?)))`)
		args = append(args, string(raw), string(raw))
	}
	if threadID := strings.TrimSpace(filter.ThreadID); threadID != "" {
		conditions = append(conditions, "d.thread_id = ?")
		args = append(args, threadID)
	}
	conditions = append(conditions, LifecycleStatesOrGroup("d.archived_at", "d.trashed_at", filter.States))
	if q := strings.TrimSpace(filter.Query); q != "" {
		searchPattern := "%" + strings.ToLower(q) + "%"
		conditions = append(conditions, "(LOWER(d.id) LIKE ? OR LOWER(COALESCE(d.handle, '')) LIKE ? OR LOWER('document:' || COALESCE(d.handle, '')) LIKE ? OR LOWER(d.title) LIKE ? OR LOWER(d.summary) LIKE ?)")
		args = append(args, searchPattern, searchPattern, searchPattern, searchPattern, searchPattern)
	}
	if tagConditions, tagArgs := documentTagFilterSQL("d.tags_json", filter.Tag, filter.Knowledge); len(tagConditions) > 0 {
		conditions = append(conditions, tagConditions...)
		args = append(args, tagArgs...)
	}
	if len(conditions) > 0 {
		inner += ` WHERE ` + strings.Join(conditions, ` AND `)
	}
	inner += ` ORDER BY d.updated_at DESC, d.id ASC`
	if filter.Limit != nil && *filter.Limit > 0 {
		inner += ` LIMIT ?`
		args = append(args, *filter.Limit+1)
		if filter.Cursor != "" {
			if offset, err := decodeCursor(filter.Cursor); err == nil && offset > 0 {
				inner += ` OFFSET ?`
				args = append(args, offset)
			}
		}
	}

	if filter.metadataOnly {
		return `WITH doc_page AS (` + inner + `) SELECT dp.*,NULL,NULL,NULL,NULL,NULL FROM doc_page dp`, args
	}
	// Scoped aggregates keyed to the current limit page avoid scanning all revisions/events.
	query := `WITH doc_page AS (` + inner + `)
SELECT dp.*,
	COALESCE(rc.revision_cnt, 0),
	COALESCE(tmc.timeline_msg_cnt, 0),
	tml.last_body, tml.last_at, tml.last_by
FROM doc_page dp
LEFT JOIN (
	SELECT dr2.document_id, COUNT(*) AS revision_cnt FROM document_revisions dr2
	WHERE dr2.document_id IN (SELECT id FROM doc_page)
	GROUP BY dr2.document_id
) rc ON dp.id = rc.document_id
LEFT JOIN (
	SELECT trim(COALESCE(e.thread_id,'')) AS tid, COUNT(*) AS timeline_msg_cnt FROM events e
	WHERE e.type = 'message_posted'
	  AND COALESCE(trim(e.thread_id),'') <> ''
	  AND COALESCE(trim(e.trashed_at),'') = ''
	  AND trim(COALESCE(e.thread_id,'')) IN (
		SELECT DISTINCT trim(COALESCE(thread_id,'')) FROM doc_page WHERE COALESCE(trim(thread_id),'') <> ''
	  )
	GROUP BY tid
) tmc ON trim(COALESCE(dp.thread_id,'')) = tmc.tid
LEFT JOIN (
	SELECT tid, last_body, last_at, last_by FROM (
		SELECT trim(COALESCE(e.thread_id,'')) AS tid,
			TRIM(COALESCE(NULLIF(json_extract(e.payload_json, '$.text'), ''), json_extract(e.payload_json, '$.summary'))) AS last_body,
			e.ts AS last_at, e.actor_id AS last_by,
			ROW_NUMBER() OVER (PARTITION BY trim(COALESCE(e.thread_id,'')) ORDER BY e.ts DESC, e.id DESC) AS rn
		FROM events e
		WHERE e.type = 'message_posted'
		  AND COALESCE(trim(e.thread_id),'') <> ''
		  AND COALESCE(trim(e.trashed_at),'') = ''
		  AND trim(COALESCE(e.thread_id,'')) IN (
			SELECT DISTINCT trim(COALESCE(thread_id,'')) FROM doc_page WHERE COALESCE(trim(thread_id),'') <> ''
		  )
	) ranked WHERE rn = 1
) tml ON trim(COALESCE(dp.thread_id,'')) = tml.tid`
	return query, args
}

func (s *Store) ListDocuments(ctx context.Context, filter DocumentListFilter) ([]map[string]any, string, error) {
	if s == nil || s.db == nil {
		return nil, "", fmt.Errorf("primitives store database is not initialized")
	}
	filter.States = NormalizeListLifecycleStates(filter.States)
	if filter.Cursor != "" {
		if _, err := decodeCursor(filter.Cursor); err != nil {
			return nil, "", fmt.Errorf("%w: %v", ErrInvalidCursor, err)
		}
	}

	query, args := buildListDocumentsQuery(filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("query documents: %w", err)
	}
	defer rows.Close()

	documents := make([]map[string]any, 0)
	for rows.Next() {
		var row documentRow
		if err := rows.Scan(
			&row.ID,
			&row.Handle,
			&row.ThreadID,
			&row.Title,
			&row.Summary,
			&row.Source,
			&row.TagsJSON,
			&row.HostsJSON,
			&row.VerifiedAt,
			&row.Slug,
			&row.SupersedesJSON,
			&row.RefsJSON,
			&row.ProvenanceJSON,
			&row.HeadRevisionID,
			&row.HeadRevisionNum,
			&row.CreatedAt,
			&row.CreatedBy,
			&row.UpdatedAt,
			&row.UpdatedBy,
			&row.TrashedAt,
			&row.TrashedBy,
			&row.TrashReason,
			&row.ArchivedAt,
			&row.ArchivedBy,
			&row.HeadArtifactID,
			&row.HeadContentType,
			&row.HeadCreatedAt,
			&row.HeadCreatedBy,
			&row.ListRevisionCount,
			&row.ListTimelineMessageCount,
			&row.ListLastCommentBody,
			&row.ListLastCommentAt,
			&row.ListLastCommentBy,
		); err != nil {
			return nil, "", fmt.Errorf("scan document row: %w", err)
		}
		document, mapErr := row.toMap()
		if mapErr != nil {
			return nil, "", mapErr
		}
		documents = append(documents, document)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("iterate document rows: %w", err)
	}

	var nextCursor string
	if filter.Limit != nil && len(documents) > *filter.Limit {
		documents = documents[:*filter.Limit]
		offset := 0
		if filter.Cursor != "" {
			offset, _ = decodeCursor(filter.Cursor)
		}
		nextCursor = encodeCursor(offset + *filter.Limit)
	}

	if !filter.metadataOnly {
		s.attachDocumentListCharacterCounts(ctx, documents)
	}

	return documents, nextCursor, nil
}

const documentListHeadCharacterMaxDecodedBytes int64 = 1 << 20 // 1 MiB cap for UTF-8 rune counts on list payloads
const documentListBlobReadConcurrency = 8                      // bounded parallel blob reads during list enrichment

func uniqueNonEmptyDocumentIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// attachDocumentListCharacterCounts adds best-effort head revision body counts to list rows.
func (s *Store) attachDocumentListCharacterCounts(ctx context.Context, docs []map[string]any) {
	if len(docs) == 0 {
		return
	}
	docIDs := make([]string, 0, len(docs))
	for _, doc := range docs {
		id := strings.TrimSpace(anyStringValue(doc["id"]))
		if id != "" {
			docIDs = append(docIDs, id)
		}
	}
	if len(docIDs) == 0 {
		return
	}

	charCounts, err := s.batchHeadRevisionCharacterCountsByDocumentIDs(ctx, docIDs)
	if err != nil {
		log.Printf("document list character count enrichment failed: %v", err)
		return
	}
	for _, doc := range docs {
		id := strings.TrimSpace(anyStringValue(doc["id"]))
		if id == "" {
			continue
		}
		if n, ok := charCounts[id]; ok {
			doc["head_revision_character_count"] = n
		}
	}
}

// batchHeadRevisionCharacterCountsByDocumentIDs counts UTF-8 text runes on the decoded head revision body.
// Duplicated content_hashes are read once; reads run with bounded parallelism.
// Larger blobs than documentListHeadCharacterMaxDecodedBytes are skipped (omit from result map).
func (s *Store) batchHeadRevisionCharacterCountsByDocumentIDs(ctx context.Context, documentIDs []string) (map[string]int, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	if s.blob == nil {
		return map[string]int{}, nil
	}
	out := map[string]int{}
	uniq := uniqueNonEmptyDocumentIDs(documentIDs)
	if len(uniq) == 0 {
		return out, nil
	}
	ph := strings.Repeat("?,", len(uniq))
	ph = ph[:len(ph)-1]
	args := make([]any, 0, len(uniq))
	for _, id := range uniq {
		args = append(args, id)
	}
	q := fmt.Sprintf(
		`SELECT trim(d.id), trim(a.content_hash), l.size_bytes
			FROM documents d
			INNER JOIN document_revisions dr ON dr.revision_id = d.head_revision_id
			INNER JOIN artifacts a ON a.id = dr.artifact_id
				AND trim(COALESCE(a.trashed_at, '')) = ''
			LEFT JOIN blob_usage_ledger l ON trim(l.content_hash) = trim(a.content_hash)
			WHERE trim(d.id) IN (%s)`,
		ph,
	)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("batch resolve head blobs for characters: %w", err)
	}
	defer rows.Close()

	type headWork struct {
		docID string
		hash  string
	}

	work := make([]headWork, 0, len(uniq))
	for rows.Next() {
		var docID, hash string
		var ledgerSize sql.NullInt64
		if err := rows.Scan(&docID, &hash, &ledgerSize); err != nil {
			return nil, fmt.Errorf("scan head blob ledger: %w", err)
		}
		docID = strings.TrimSpace(docID)
		hash = strings.TrimSpace(hash)
		if docID == "" || hash == "" {
			continue
		}
		if ledgerSize.Valid && ledgerSize.Int64 > documentListHeadCharacterMaxDecodedBytes {
			continue
		}
		work = append(work, headWork{docID: docID, hash: hash})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate head blobs: %w", err)
	}

	byHash := make(map[string][]string, len(work))
	for _, w := range work {
		byHash[w.hash] = append(byHash[w.hash], w.docID)
	}
	if len(byHash) == 0 {
		return out, nil
	}

	var mu sync.Mutex
	sem := make(chan struct{}, documentListBlobReadConcurrency)
	g := new(errgroup.Group)
	for hash, docIDs := range byHash {
		hash := hash
		docIDs := docIDs
		g.Go(func() error {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return ctx.Err()
			}
			body, err := s.blob.Read(ctx, hash)
			if err != nil {
				if errors.Is(err, blob.ErrBlobNotFound) {
					return nil
				}
				return fmt.Errorf("read head revision blobs for hash=%s: %w", hash, err)
			}
			if int64(len(body)) > documentListHeadCharacterMaxDecodedBytes {
				return nil
			}
			n := utf8.RuneCount(body)
			mu.Lock()
			for _, id := range docIDs {
				out[id] = n
			}
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		// List enrichment only: return whichever hashes succeeded; never fail all rows
		// because one read errored or the request context was canceled mid-batch.
		log.Printf("batch head revision character reads (partial=%d): %v", len(out), err)
	}
	return out, nil
}
