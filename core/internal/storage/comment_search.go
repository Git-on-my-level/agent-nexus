package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"strings"
	"time"

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
const commentSearchCapture = `INSERT INTO comment_search_heads(event_id,thread_id,projection_json,source_version,authority_binding,source_ts,actor_id)
 SELECT id,thread_id,anx_comment_search_projection(CAST(CASE WHEN json_valid(payload_json) THEN COALESCE(json_extract(payload_json,'$.payload.text'),'') ELSE '' END AS BLOB)),1,'canonical-event-v1',ts,actor_id
 FROM events WHERE id=? AND type='message_posted' AND COALESCE(thread_id,'')<>'' AND COALESCE(trim(trashed_at),'')=''
 ON CONFLICT(event_id) DO UPDATE SET thread_id=excluded.thread_id,projection_json=excluded.projection_json,source_version=comment_search_heads.source_version+1,source_ts=excluded.source_ts,actor_id=excluded.actor_id`

const commentSearchSchema = `
CREATE TABLE IF NOT EXISTS comment_search_heads(
 event_id TEXT PRIMARY KEY,thread_id TEXT NOT NULL,
 projection_json TEXT NOT NULL CHECK(json_valid(projection_json) AND length(CAST(projection_json AS BLOB))<=262144 AND length(CAST(json_extract(projection_json,'$.text') AS BLOB))<=65536 AND json_array_length(projection_json,'$.terms')<=4096),
 source_version INTEGER NOT NULL CHECK(typeof(source_version)='integer' AND source_version>0),
 authority_binding TEXT NOT NULL CHECK(authority_binding='canonical-event-v1'),source_ts TEXT NOT NULL DEFAULT '',actor_id TEXT NOT NULL DEFAULT '',
 indexed_bytes INTEGER GENERATED ALWAYS AS(length(CAST(json_extract(projection_json,'$.text') AS BLOB))) STORED,
 truncated INTEGER GENERATED ALWAYS AS(json_extract(projection_json,'$.truncated')) STORED,
 preview TEXT GENERATED ALWAYS AS(json_extract(projection_json,'$.preview')) STORED
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS comment_search_postings(
 thread_id TEXT NOT NULL,term TEXT NOT NULL,event_id TEXT NOT NULL,
 slot INTEGER NOT NULL CHECK(typeof(slot)='integer' AND slot BETWEEN 0 AND 4095),
 PRIMARY KEY(event_id,term),UNIQUE(event_id,slot)
) WITHOUT ROWID;
CREATE UNIQUE INDEX IF NOT EXISTS comment_search_postings_scope ON comment_search_postings(thread_id,term,event_id);
CREATE TABLE IF NOT EXISTS comment_search_job(
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),cursor TEXT,
 normalization_version INTEGER NOT NULL DEFAULT 2,
 done INTEGER NOT NULL DEFAULT 0 CHECK(done IN(0,1)),
 authority_binding TEXT NOT NULL DEFAULT 'canonical-event-v1' CHECK(authority_binding='canonical-event-v1')
);
INSERT OR IGNORE INTO comment_search_job(singleton) VALUES(1);
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
	capture := stringsReplaceCommentID(commentSearchCapture)
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

func stringsReplaceCommentID(query string) string {
	// The reviewed template has exactly one parameter, its source identity.
	return strings.Replace(query, "id=?", "id=NEW.id", 1)
}

// MaintainCommentSearchBatch advances one indexed source position per short
// transaction. A worst-case row has 4096 postings; one row prevents dense legacy
// comments from starving a fixed transaction time budget. Triggers cover writes
// behind the cursor and the final empty seek certifies complete source coverage.
func (w *Workspace) MaintainCommentSearchBatch(ctx context.Context) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
	defer cancel()
	tx, cleanup, err := sqliteutil.BeginMaintenanceChunk(ctx, w.db)
	if err != nil {
		return false, err
	}
	defer cleanup()
	var cursor sql.NullString
	var done bool
	if err = tx.QueryRowContext(ctx, `SELECT cursor,done FROM comment_search_job WHERE singleton=1 AND normalization_version=2 AND authority_binding='canonical-event-v1'`).Scan(&cursor, &done); err != nil {
		return false, err
	}
	if done {
		return true, tx.Commit()
	}
	var id string
	if cursor.Valid {
		err = tx.QueryRowContext(ctx, `SELECT id FROM events WHERE id>? ORDER BY id LIMIT 1`, cursor.String).Scan(&id)
	} else {
		err = tx.QueryRowContext(ctx, `SELECT id FROM events ORDER BY id LIMIT 1`).Scan(&id)
	}
	if err == sql.ErrNoRows {
		_, err = tx.ExecContext(ctx, `UPDATE comment_search_job SET done=1 WHERE singleton=1`)
		if err != nil {
			return false, err
		}
		return true, tx.Commit()
	}
	if err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, commentSearchCapture, id); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE comment_search_job SET cursor=? WHERE singleton=1`, id); err != nil {
		return false, err
	}
	return false, tx.Commit()
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
				return
			}
		}
	}
}
