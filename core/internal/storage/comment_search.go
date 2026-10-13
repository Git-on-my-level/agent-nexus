package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"
	"time"

	"agent-nexus-core/internal/commentindex"
	"agent-nexus-core/internal/scopesearch"
	"agent-nexus-core/internal/sqliteutil"
	"modernc.org/sqlite"
)

func init() {
	sqlite.MustRegisterDeterministicScalarFunction("anx_comment_search_projection", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		var input string
		switch value := args[0].(type) {
		case []byte:
			input = string(value)
		case string:
			input = value
		}
		return scopesearch.EncodeComment(input)
	})
}

// This is the canonical writer for both triggers and backfill. It selects one
// current event inside the source transaction; no caller supplies projection
// text, parent or audience. All thread messages are captured so later document
// attachment/reparenting requires no unbounded history work on that mutation.
const commentSearchCapture = `INSERT INTO comment_search_heads(event_id,thread_id,projection_json,source_version,authority_binding,source_ts,actor_id,raw_text,raw_truncated,ready)
 SELECT id,thread_id,'{"text":"","terms":[],"preview":"","truncated":false}',1,'canonical-event-v1',ts,actor_id,
 COALESCE(substr(CAST(CASE WHEN json_valid(payload_json) THEN COALESCE(json_extract(payload_json,'$.payload.text'),'') ELSE '' END AS BLOB),1,65536),X''),
 length(CAST(CASE WHEN json_valid(payload_json) THEN COALESCE(json_extract(payload_json,'$.payload.text'),'') ELSE '' END AS BLOB))>65536,0
 FROM events WHERE id=? AND type='message_posted' AND COALESCE(thread_id,'')<>'' AND COALESCE(trim(trashed_at),'')=''
 ON CONFLICT(event_id) DO UPDATE SET thread_id=excluded.thread_id,projection_json=excluded.projection_json,source_version=comment_search_heads.source_version+1,source_ts=excluded.source_ts,actor_id=excluded.actor_id,raw_text=excluded.raw_text,raw_truncated=excluded.raw_truncated,ready=0`

const commentSearchSchema = `
CREATE TABLE IF NOT EXISTS comment_search_heads(
 event_id TEXT PRIMARY KEY,thread_id TEXT NOT NULL,
 raw_text BLOB NOT NULL DEFAULT X'' CHECK(length(raw_text)<=65536),
 raw_truncated INTEGER NOT NULL DEFAULT 0 CHECK(raw_truncated IN(0,1)),
 ready INTEGER NOT NULL DEFAULT 0 CHECK(ready IN(0,1)),
 projection_json TEXT NOT NULL CHECK(json_valid(projection_json) AND length(CAST(projection_json AS BLOB))<=262144 AND length(CAST(json_extract(projection_json,'$.text') AS BLOB))<=65536 AND json_array_length(projection_json,'$.terms')<=4096),
 source_version INTEGER NOT NULL CHECK(typeof(source_version)='integer' AND source_version>0),
 authority_binding TEXT NOT NULL CHECK(authority_binding='canonical-event-v1'),source_ts TEXT NOT NULL DEFAULT '',actor_id TEXT NOT NULL DEFAULT '',
 indexed_bytes INTEGER GENERATED ALWAYS AS(length(CAST(json_extract(projection_json,'$.text') AS BLOB))) STORED,
 truncated INTEGER GENERATED ALWAYS AS(json_extract(projection_json,'$.truncated')) STORED,
 preview TEXT GENERATED ALWAYS AS(json_extract(projection_json,'$.preview')) STORED
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS comment_search_heads_pending ON comment_search_heads(ready,event_id);
CREATE VIEW IF NOT EXISTS comment_search_pending_heads AS SELECT event_id,thread_id FROM comment_search_heads WHERE ready=0;
CREATE TABLE IF NOT EXISTS comment_search_postings(
 thread_id TEXT NOT NULL,term TEXT NOT NULL,event_id TEXT NOT NULL,
 slot INTEGER NOT NULL CHECK(typeof(slot)='integer' AND slot BETWEEN 0 AND 4095),
 PRIMARY KEY(event_id,term),UNIQUE(event_id,slot)
) WITHOUT ROWID;
CREATE UNIQUE INDEX IF NOT EXISTS comment_search_postings_scope ON comment_search_postings(thread_id,term,event_id);
CREATE TABLE IF NOT EXISTS comment_search_job(
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),cursor TEXT,pending_cursor TEXT,
 normalization_version INTEGER NOT NULL DEFAULT 2,
 done INTEGER NOT NULL DEFAULT 0 CHECK(done IN(0,1)),
 authority_binding TEXT NOT NULL DEFAULT 'canonical-event-v1' CHECK(authority_binding='canonical-event-v1')
);
INSERT OR IGNORE INTO comment_search_job(singleton) VALUES(1);
CREATE TRIGGER IF NOT EXISTS comment_search_raw_insert BEFORE INSERT ON comment_search_heads
 WHEN length(NEW.raw_text)>65536 OR NEW.raw_truncated NOT IN(0,1) OR NEW.ready NOT IN(0,1)
 BEGIN SELECT RAISE(ABORT,'invalid raw search capture');END;
CREATE TRIGGER IF NOT EXISTS comment_search_raw_update BEFORE UPDATE OF raw_text,raw_truncated,ready ON comment_search_heads
 WHEN length(NEW.raw_text)>65536 OR NEW.raw_truncated NOT IN(0,1) OR NEW.ready NOT IN(0,1)
 BEGIN SELECT RAISE(ABORT,'invalid raw search capture');END;
CREATE TRIGGER IF NOT EXISTS comment_search_head_update BEFORE UPDATE ON comment_search_heads BEGIN
 SELECT RAISE(ABORT,'comment source identity is immutable') WHERE OLD.event_id<>NEW.event_id;
 SELECT RAISE(ABORT,'comment source binding missing') WHERE NOT EXISTS(SELECT 1 FROM events WHERE id=NEW.event_id AND thread_id=NEW.thread_id AND type='message_posted' AND COALESCE(trim(trashed_at),'')='');
 DELETE FROM comment_search_postings WHERE event_id=OLD.event_id;
END;
CREATE TRIGGER IF NOT EXISTS comment_search_head_bind BEFORE INSERT ON comment_search_heads
 WHEN NOT EXISTS(SELECT 1 FROM events WHERE id=NEW.event_id AND thread_id=NEW.thread_id AND type='message_posted' AND COALESCE(trim(trashed_at),'')='')
 BEGIN SELECT RAISE(ABORT,'comment source binding missing'); END;
CREATE TRIGGER IF NOT EXISTS comment_search_head_delete BEFORE DELETE ON comment_search_heads BEGIN
 DELETE FROM comment_search_postings WHERE event_id=OLD.event_id;
END;
CREATE TRIGGER IF NOT EXISTS comment_search_head_insert AFTER INSERT ON comment_search_heads BEGIN
 INSERT INTO comment_search_postings SELECT NEW.thread_id,value,NEW.event_id,CAST(key AS INTEGER) FROM json_each(NEW.projection_json,'$.terms');
END;
CREATE TRIGGER IF NOT EXISTS comment_search_head_reindex AFTER UPDATE ON comment_search_heads BEGIN
 INSERT INTO comment_search_postings SELECT NEW.thread_id,value,NEW.event_id,CAST(key AS INTEGER) FROM json_each(NEW.projection_json,'$.terms');
END;
CREATE TRIGGER IF NOT EXISTS comment_search_posting_insert BEFORE INSERT ON comment_search_postings
 WHEN NOT EXISTS(SELECT 1 FROM comment_search_heads WHERE event_id=NEW.event_id AND thread_id=NEW.thread_id)
 BEGIN SELECT RAISE(ABORT,'comment source binding missing'); END;
CREATE TRIGGER IF NOT EXISTS comment_search_posting_update BEFORE UPDATE ON comment_search_postings
 BEGIN SELECT RAISE(ABORT,'replace comment postings with source'); END;
`

func installCommentSearch(ctx context.Context, tx *sql.Tx) error {
	exists, err := sqliteTableExists(ctx, tx, "events")
	if err != nil || !exists {
		return err
	}
	if _, err := tx.ExecContext(ctx, commentSearchSchema); err != nil {
		return err
	}
	// Capture is an exact point selector in all three forms. The worker uses
	// the parameterized form, while triggers bind NEW.id from the canonical row.
	capture := strings.Replace(commentSearchCapture, "id=?", "id=NEW.id", 1)
	_, err = tx.ExecContext(ctx, `CREATE TRIGGER IF NOT EXISTS comment_search_event_insert AFTER INSERT ON events BEGIN
 DELETE FROM comment_search_heads WHERE event_id=NEW.id AND (NEW.type<>'message_posted' OR COALESCE(NEW.thread_id,'')='' OR COALESCE(trim(NEW.trashed_at),'')<>'');
 `+capture+`; END;
CREATE TRIGGER IF NOT EXISTS comment_search_event_update AFTER UPDATE OF id,type,thread_id,payload_json,trashed_at,archived_at,refs_json,actor_id,ts ON events BEGIN
 DELETE FROM comment_search_heads WHERE event_id=OLD.id AND (OLD.id<>NEW.id OR NEW.type<>'message_posted' OR COALESCE(NEW.thread_id,'')='' OR COALESCE(trim(NEW.trashed_at),'')<>'');
 `+capture+`;
END;
CREATE TRIGGER IF NOT EXISTS comment_search_event_delete AFTER DELETE ON events BEGIN DELETE FROM comment_search_heads WHERE event_id=OLD.id; END;`)
	if err != nil {
		return err
	}
	return installDocumentSearch(ctx, tx)
}

// MaintainCommentSearchBatch reserves one source in a bounded capture chunk.
// CPU preparation runs after releasing that writer. Applying at most 4096 terms
// has its own five-second deadline, matching the document-head maintenance budget.
// The durable pending cursor advances before attempting normalization, including
// failed attempts; failures are reported by Run and cannot starve later sources.
func (w *Workspace) MaintainCommentSearchBatch(ctx context.Context) (bool, error) {
	return w.maintainCommentSearchBatch(ctx, commentindex.Prepare)
}

func (w *Workspace) maintainCommentSearchBatch(ctx context.Context, prepare func([]byte, bool) (string, error)) (bool, error) {
	var idle bool
	if err := w.db.QueryRowContext(ctx, `SELECT done AND NOT EXISTS(SELECT 1 FROM comment_search_heads WHERE ready=0 LIMIT 1) FROM comment_search_job WHERE singleton=1`).Scan(&idle); err != nil {
		return false, err
	}
	if idle {
		return true, nil
	}
	id, done, err := w.reserveCommentSearchSource(ctx)
	if err != nil || done {
		return done, err
	}
	var raw []byte
	var truncated bool
	var version int64
	err = w.db.QueryRowContext(ctx, `SELECT raw_text,raw_truncated,source_version FROM comment_search_heads WHERE event_id=? AND ready=0`, id).Scan(&raw, &truncated, &version)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	body, err := prepare(raw, truncated)
	if err != nil {
		return false, err
	}
	// No writer lock or chunk deadline covers preparation. Source edits/deletes
	// during that work invalidate this proposal; the current capture stays pending.
	applyCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, cleanup, err := sqliteutil.BeginMaintenanceChunk(applyCtx, w.db)
	if err != nil {
		return false, err
	}
	defer cleanup()
	_, err = tx.ExecContext(applyCtx, `UPDATE comment_search_heads SET projection_json=?,ready=1 WHERE event_id=? AND ready=0 AND source_version=? AND raw_text=CAST(? AS BLOB) AND raw_truncated=?`, body, id, version, string(raw), truncated)
	if err != nil {
		return false, err
	}
	return false, tx.Commit()
}

func (w *Workspace) reserveCommentSearchSource(ctx context.Context) (string, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	tx, cleanup, err := sqliteutil.BeginMaintenanceChunk(ctx, w.db)
	if err != nil {
		return "", false, err
	}
	defer cleanup()
	var cursor, pendingCursor sql.NullString
	var done bool
	if err = tx.QueryRowContext(ctx, `SELECT cursor,pending_cursor,done FROM comment_search_job WHERE singleton=1 AND normalization_version=2 AND authority_binding='canonical-event-v1'`).Scan(&cursor, &pendingCursor, &done); err != nil {
		return "", false, err
	}
	var id string
	// Two index seeks implement wraparound without sorting the entire queue.
	err = sql.ErrNoRows
	if pendingCursor.Valid {
		err = tx.QueryRowContext(ctx, `SELECT event_id FROM comment_search_heads WHERE ready=0 AND event_id>? ORDER BY event_id LIMIT 1`, pendingCursor.String).Scan(&id)
	}
	if err == sql.ErrNoRows {
		err = tx.QueryRowContext(ctx, `SELECT event_id FROM comment_search_heads WHERE ready=0 ORDER BY event_id LIMIT 1`).Scan(&id)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, `UPDATE comment_search_job SET pending_cursor=? WHERE singleton=1`, id)
		if err != nil {
			return "", false, err
		}
		return id, false, tx.Commit()
	}
	if err != sql.ErrNoRows {
		return "", false, err
	}
	if done {
		return "", true, tx.Commit()
	}
	if cursor.Valid {
		err = tx.QueryRowContext(ctx, `SELECT id FROM events WHERE id>? ORDER BY id LIMIT 1`, cursor.String).Scan(&id)
	} else {
		err = tx.QueryRowContext(ctx, `SELECT id FROM events ORDER BY id LIMIT 1`).Scan(&id)
	}
	if err == sql.ErrNoRows {
		_, err = tx.ExecContext(ctx, `UPDATE comment_search_job SET done=1 WHERE singleton=1`)
		if err != nil {
			return "", false, err
		}
		return "", true, tx.Commit()
	}
	if err != nil {
		return "", false, err
	}
	// Re-capture even an existing head: an old normalization version may
	// have reset the historical cursor. Replacing its old 4096 terms shares
	// the same five-second bound as applying a prepared projection.
	if _, err = tx.ExecContext(ctx, commentSearchCapture, id); err != nil {
		return "", false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE comment_search_job SET cursor=? WHERE singleton=1`, id); err != nil {
		return "", false, err
	}
	return id, false, tx.Commit()
}

// RunCommentSearchMaintenance belongs to the server maintenance lifecycle,
// never to a search request or startup readiness. Cancellation aborts only the
// current chunk; committed source positions survive reopen.
func (w *Workspace) RunCommentSearchMaintenance(ctx context.Context, report func(error)) {
	ticker := time.NewTicker(25 * time.Millisecond)
	defer ticker.Stop()
	var lastReport time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			done, err := w.MaintainCommentSearchBatch(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				if report != nil && time.Since(lastReport) >= time.Minute {
					report(err)
					lastReport = time.Now()
				}
			} else if done {
				ticker.Reset(250 * time.Millisecond)
			} else {
				ticker.Reset(25 * time.Millisecond)
			}
		}
	}
}
