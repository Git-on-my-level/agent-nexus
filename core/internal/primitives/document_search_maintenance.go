package primitives

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"agent-nexus-core/internal/scopesearch"
)

// documentSearchHeadCaptureSQL takes a bounded canonical body, its revision,
// and document id. Callers bind all three inside the source transaction, or
// recheck the revision after reading the blob during online maintenance.
const documentSearchHeadCaptureSQL = `UPDATE document_search_heads SET
 body_text=?,body_revision=?,body_truncated=?,body_artifact=(SELECT artifact_id FROM document_revisions WHERE revision_id=(SELECT head_revision_id FROM documents WHERE id=document_id)),ready=1,
 projection_json=json_patch(anx_comment_search_projection(CAST(
 (SELECT COALESCE(title,'')||' '||summary||' '||source||' '||tags_json FROM documents WHERE id=document_id)||' '||? AS BLOB)),CASE WHEN ? THEN '{"truncated":true}' ELSE '{}' END)
 WHERE document_id=? AND EXISTS(SELECT 1 FROM documents d WHERE d.id=document_id AND d.head_revision_id=?)`

func boundedSearchBody(body string) string {
	if len(body) <= scopesearch.MaxTextBytes {
		return body
	}
	end := scopesearch.MaxTextBytes
	for end > 0 && !utf8.RuneStart(body[end]) {
		end--
	}
	return body[:end]
}

// readSearchBody is maintenance-only. Ordinary comment mutations never load a
// document revision or blob. The caller rechecks this revision under its write
// transaction before certifying the derived head.
const searchBodyBindingSQL = `SELECT json_array(d.head_revision_id,r.artifact_id,a.content_hash,a.content_type) FROM documents d JOIN document_revisions r ON r.revision_id=d.head_revision_id AND r.document_id=d.id JOIN artifacts a ON a.id=r.artifact_id WHERE d.id=?`

func (s *Store) readSearchBody(ctx context.Context, id string) (revision, body, binding string, err error) {
	var hash, kind string
	err = s.db.QueryRowContext(ctx, `SELECT d.head_revision_id,a.content_hash,a.content_type,json_array(d.head_revision_id,r.artifact_id,a.content_hash,a.content_type) FROM documents d JOIN document_revisions r ON r.revision_id=d.head_revision_id AND r.document_id=d.id JOIN artifacts a ON a.id=r.artifact_id WHERE d.id=?`, id).Scan(&revision, &hash, &kind, &binding)
	if err != nil {
		return
	}
	if kind == "binary" {
		return
	}
	var content []byte
	content, err = s.blob.Read(ctx, hash)
	if err == nil {
		body = string(content)
	}
	return
}

// MaintainDocumentSearchBatch performs one head capture or one parent-link
// capture. Blob I/O is outside the write transaction; its revision is checked
// again before commit. Triggers maintain writes behind both durable cursors.
func (s *Store) MaintainDocumentSearchBatch(ctx context.Context) (bool, error) {
	ctx = CanonicalMaintenanceContext(ctx)
	var id string
	var cursor sql.NullString
	var scanDone bool
	if err := s.db.QueryRowContext(ctx, `SELECT cursor,done FROM document_search_state WHERE singleton=1 AND normalization_version=2`).Scan(&cursor, &scanDone); err != nil {
		return false, err
	}
	err := s.db.QueryRowContext(ctx, `SELECT document_id FROM document_search_heads WHERE ready=0 ORDER BY document_id LIMIT 1`).Scan(&id)
	advance := false
	if errors.Is(err, sql.ErrNoRows) && !scanDone {
		if cursor.Valid {
			err = s.db.QueryRowContext(ctx, `SELECT id FROM documents WHERE id>? ORDER BY id LIMIT 1`, cursor.String).Scan(&id)
		} else {
			err = s.db.QueryRowContext(ctx, `SELECT id FROM documents ORDER BY id LIMIT 1`).Scan(&id)
		}
		advance = true
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if err == nil {
		revision, body, binding, readErr := s.readSearchBody(ctx, id)
		if readErr != nil {
			return false, readErr
		}
		chunkCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
		defer cancel()
		tx, cleanup, err := s.db.BeginMaintenanceChunk(chunkCtx)
		if err != nil {
			return false, err
		}
		defer cleanup()
		ctx = chunkCtx
		var current string
		if err = tx.QueryRowContext(ctx, searchBodyBindingSQL, id).Scan(&current); err != nil {
			return false, err
		}
		if current != binding {
			return false, nil
		}
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO document_search_heads(document_id) VALUES(?)`, id); err != nil {
			return false, err
		}
		if _, err = tx.ExecContext(ctx, documentSearchHeadCaptureSQL, boundedSearchBody(body), revision, len(body) > scopesearch.MaxTextBytes, boundedSearchBody(body), len(body) > scopesearch.MaxTextBytes, id, revision); err != nil {
			return false, err
		}
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO document_search_links_pending(document_id) VALUES(?)`, id); err != nil {
			return false, err
		}
		if advance {
			if _, err = tx.ExecContext(ctx, `UPDATE document_search_state SET cursor=? WHERE singleton=1`, id); err != nil {
				return false, err
			}
		}
		return false, tx.Commit()
	}
	if scanDone {
		var pending bool
		if err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM document_search_links_pending LIMIT 1)`).Scan(&pending); err != nil {
			return false, err
		}
		if !pending {
			return true, nil
		}
	}
	// Completion and one bounded link step share a source transaction.
	chunkCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	tx, cleanup, err := s.db.BeginMaintenanceChunk(chunkCtx)
	if err != nil {
		return false, err
	}
	defer cleanup()
	ctx = chunkCtx
	if !scanDone {
		// Recheck the empty tail after acquiring the writer; newly inserted
		// documents already have trigger-created heads and pending links.
		var remains bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM documents WHERE ? IS NULL OR id>? LIMIT 1)`, cursor, cursor).Scan(&remains); err != nil {
			return false, err
		}
		if remains {
			return false, nil
		}
		if _, err = tx.ExecContext(ctx, `UPDATE document_search_state SET done=1 WHERE singleton=1`); err != nil {
			return false, err
		}
	}
	var linkCursor sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT document_id,cursor FROM document_search_links_pending ORDER BY document_id LIMIT 1`).Scan(&id, &linkCursor)
	if errors.Is(err, sql.ErrNoRows) {
		return true, tx.Commit()
	}
	if err != nil {
		return false, err
	}
	var event string
	if linkCursor.Valid {
		err = tx.QueryRowContext(ctx, `SELECT h.event_id FROM comment_search_heads h WHERE h.thread_id=(SELECT thread_id FROM documents WHERE id=?) AND h.event_id>? ORDER BY h.event_id LIMIT 1`, id, linkCursor.String).Scan(&event)
	} else {
		err = tx.QueryRowContext(ctx, `SELECT h.event_id FROM comment_search_heads h WHERE h.thread_id=(SELECT thread_id FROM documents WHERE id=?) ORDER BY h.event_id LIMIT 1`, id).Scan(&event)
	}
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `DELETE FROM document_search_links_pending WHERE document_id=?`, id)
	} else if err == nil {
		_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO document_search_resources(doc_n,kind,resource_id) SELECT n,'comment',? FROM document_search_heads WHERE document_id=?`, event, id)
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE document_search_links_pending SET cursor=? WHERE document_id=?`, event, id)
		}
	}
	if err != nil {
		return false, err
	}
	return false, tx.Commit()
}

func (s *Store) RunDocumentSearchMaintenance(ctx context.Context, report func(error)) {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	var last time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			done, err := s.MaintainDocumentSearchBatch(ctx)
			var commentsDone bool
			if err == nil {
				err = s.db.QueryRowContext(ctx, `SELECT done FROM comment_search_job WHERE singleton=1`).Scan(&commentsDone)
			}
			if err == nil && (!independentDocumentSearchEnabled() || !done || !commentsDone) {
				var repaired bool
				repaired, err = s.repairAggregateSearchOne(ctx)
				done = done && repaired
			}
			if done && err == nil {
				ticker.Reset(250 * time.Millisecond)
			} else {
				ticker.Reset(25 * time.Millisecond)
			}
			if err != nil && ctx.Err() == nil && report != nil && time.Since(last) > time.Minute {
				report(err)
				last = time.Now()
			}
		}
	}
}

// A fallback activation repairs at most one dirty document per call. It never
// serves a successful response from a stale aggregate. A larger dirty backlog
// returns a retryable 503 and is drained by maintenance, not an O(workspace)
// request loop. Repair uses canonical source data, never reader-filtered text.
func (s *Store) repairAggregateSearchOne(ctx context.Context) (bool, error) {
	ctx = CanonicalMaintenanceContext(ctx)
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT document_id FROM document_search_dirty ORDER BY document_id LIMIT 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	revision, body, binding, err := s.readSearchBody(ctx, id)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	chunkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, cleanup, err := s.db.BeginMaintenanceChunk(chunkCtx)
	if err != nil {
		return false, err
	}
	defer cleanup()
	ctx = chunkCtx
	var title, summary, source, tags, thread, current string
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(title,''),summary,source,tags_json,COALESCE(thread_id,''),head_revision_id FROM documents WHERE id=?`, id).Scan(&title, &summary, &source, &tags, &thread, &current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if err == nil {
		var currentBinding string
		if e := tx.QueryRowContext(ctx, searchBodyBindingSQL, id).Scan(&currentBinding); e != nil {
			return false, e
		}
		if current != revision || currentBinding != binding {
			return false, nil
		}
	}
	if _, e := tx.ExecContext(ctx, `DELETE FROM document_fts WHERE document_id=?`, id); e != nil {
		return false, e
	}
	if err == nil {
		comments, e := documentCommentSearchText(ctx, tx, thread)
		if e != nil {
			return false, e
		}
		list, e := decodeStoredJSONList(tags, "document.tags")
		if e != nil {
			return false, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO document_fts(document_id,title,body,summary,source,tags,comments) VALUES(?,?,?,?,?,?,?)`, id, title, body, summary, source, strings.Join(list, " "), comments); e != nil {
			return false, e
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM document_search_dirty WHERE document_id=?`, id); err != nil {
		return false, err
	}
	return false, tx.Commit()
}
