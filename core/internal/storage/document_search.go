package storage

import (
	"context"
	"database/sql"
)

// These are indexes of current canonical projections, not a second source of
// search text or authority. The original per-event postings support bounded
// parent attachment; the document-keyed posting index supports grouped paging.
const documentSearchSchema = `
CREATE INDEX IF NOT EXISTS comment_search_heads_thread ON comment_search_heads(thread_id,event_id);
CREATE INDEX IF NOT EXISTS comment_search_heads_recent ON comment_search_heads(thread_id,source_ts DESC,event_id DESC);
CREATE TABLE IF NOT EXISTS comment_search_threads(thread_id TEXT PRIMARY KEY,count INTEGER NOT NULL CHECK(count>=0));
CREATE TRIGGER IF NOT EXISTS comment_search_thread_insert AFTER INSERT ON comment_search_heads BEGIN
 INSERT INTO comment_search_threads VALUES(NEW.thread_id,1) ON CONFLICT(thread_id) DO UPDATE SET count=count+1;
END;
CREATE TRIGGER IF NOT EXISTS comment_search_thread_delete AFTER DELETE ON comment_search_heads BEGIN
 UPDATE comment_search_threads SET count=count-1 WHERE thread_id=OLD.thread_id;
END;
CREATE TRIGGER IF NOT EXISTS comment_search_thread_move AFTER UPDATE OF thread_id ON comment_search_heads WHEN OLD.thread_id<>NEW.thread_id BEGIN
 UPDATE comment_search_threads SET count=count-1 WHERE thread_id=OLD.thread_id;
 INSERT INTO comment_search_threads VALUES(NEW.thread_id,1) ON CONFLICT(thread_id) DO UPDATE SET count=count+1;
END;
CREATE TABLE IF NOT EXISTS document_search_state(
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 generation INTEGER NOT NULL CHECK(typeof(generation)='integer' AND generation>0),
 cursor_key BLOB NOT NULL CHECK(length(cursor_key)=32),
 normalization_version INTEGER NOT NULL DEFAULT 2,
 cursor TEXT,done INTEGER NOT NULL DEFAULT 0 CHECK(done IN(0,1))
);
INSERT INTO document_search_state(singleton,generation,cursor_key,done) SELECT 1,1,randomblob(32),NOT EXISTS(SELECT 1 FROM documents LIMIT 1) ON CONFLICT DO NOTHING;
UPDATE comment_search_job SET done=1 WHERE cursor IS NULL AND NOT EXISTS(SELECT 1 FROM events LIMIT 1);
CREATE TABLE IF NOT EXISTS document_search_heads(
 n INTEGER PRIMARY KEY AUTOINCREMENT,document_id TEXT NOT NULL UNIQUE,
 body_text TEXT NOT NULL DEFAULT '' CHECK(length(CAST(body_text AS BLOB))<=65536),
 body_truncated INTEGER NOT NULL DEFAULT 0 CHECK(body_truncated IN(0,1)),
 body_revision TEXT NOT NULL DEFAULT '',body_artifact TEXT NOT NULL DEFAULT '',ready INTEGER NOT NULL DEFAULT 0 CHECK(ready IN(0,1)),
 projection_json TEXT NOT NULL DEFAULT '{"text":"","terms":[],"truncated":false}' CHECK(length(CAST(projection_json AS BLOB))<=262144),
 indexed_bytes INTEGER GENERATED ALWAYS AS(length(CAST(json_extract(projection_json,'$.text') AS BLOB))) STORED,
 truncated INTEGER GENERATED ALWAYS AS(json_extract(projection_json,'$.truncated')) STORED
);
CREATE INDEX IF NOT EXISTS document_search_heads_revision ON document_search_heads(body_revision);
CREATE INDEX IF NOT EXISTS document_search_heads_artifact ON document_search_heads(body_artifact);
CREATE INDEX IF NOT EXISTS document_search_heads_pending ON document_search_heads(ready,document_id);
CREATE VIEW IF NOT EXISTS document_search_pending_heads AS SELECT document_id FROM document_search_heads WHERE ready=0;
CREATE TABLE IF NOT EXISTS document_search_resources(
 n INTEGER PRIMARY KEY AUTOINCREMENT,doc_n INTEGER NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN('document','comment')),resource_id TEXT NOT NULL,
 UNIQUE(doc_n,kind,resource_id)
);
CREATE INDEX IF NOT EXISTS document_search_resources_source ON document_search_resources(kind,resource_id,doc_n);
CREATE TABLE IF NOT EXISTS document_search_postings(
 term TEXT NOT NULL,doc_n INTEGER NOT NULL,res_n INTEGER NOT NULL,
 PRIMARY KEY(term,doc_n,res_n)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS document_search_postings_resource ON document_search_postings(res_n,term);
CREATE TABLE IF NOT EXISTS document_search_links_pending(document_id TEXT PRIMARY KEY,cursor TEXT);
CREATE TABLE IF NOT EXISTS document_search_dirty(document_id TEXT PRIMARY KEY);
CREATE TRIGGER IF NOT EXISTS document_search_resource_delete BEFORE DELETE ON document_search_resources BEGIN
 DELETE FROM document_search_postings WHERE res_n=OLD.n;
END;
CREATE TRIGGER IF NOT EXISTS document_search_resource_insert AFTER INSERT ON document_search_resources BEGIN
 INSERT INTO document_search_postings
 SELECT value,NEW.doc_n,NEW.n FROM document_search_heads h,json_each(h.projection_json,'$.terms')
 WHERE NEW.kind='document' AND h.n=NEW.doc_n AND h.ready=1
 UNION ALL
 SELECT term,NEW.doc_n,NEW.n FROM comment_search_postings WHERE NEW.kind='comment' AND event_id=NEW.resource_id;
END;
CREATE TRIGGER IF NOT EXISTS document_search_term_insert AFTER INSERT ON comment_search_postings BEGIN
 INSERT INTO document_search_postings
 SELECT NEW.term,doc_n,n FROM document_search_resources WHERE kind='comment' AND resource_id=NEW.event_id ON CONFLICT DO NOTHING;
END;
CREATE TRIGGER IF NOT EXISTS document_search_term_delete AFTER DELETE ON comment_search_postings BEGIN
 DELETE FROM document_search_postings WHERE term=OLD.term AND res_n IN(SELECT n FROM document_search_resources WHERE kind='comment' AND resource_id=OLD.event_id);
END;
CREATE TRIGGER IF NOT EXISTS document_search_head_update AFTER UPDATE ON document_search_heads BEGIN
 DELETE FROM document_search_resources WHERE doc_n=NEW.n AND kind='document';
 INSERT INTO document_search_resources(doc_n,kind,resource_id) SELECT NEW.n,'document',NEW.document_id WHERE NEW.ready=1;
 UPDATE document_search_state SET generation=generation+1 WHERE singleton=1;
END;
CREATE TRIGGER IF NOT EXISTS document_search_head_delete BEFORE DELETE ON document_search_heads BEGIN
 DELETE FROM document_search_resources WHERE doc_n=OLD.n;
 DELETE FROM document_search_links_pending WHERE document_id=OLD.document_id;
 UPDATE document_search_state SET generation=generation+1 WHERE singleton=1;
END;
CREATE TRIGGER IF NOT EXISTS document_search_comment_insert AFTER INSERT ON comment_search_heads BEGIN
 SELECT RAISE(ABORT,'too many document bindings for one comment') WHERE
 (SELECT count(*) FROM (SELECT 1 FROM documents WHERE thread_id=NEW.thread_id LIMIT 65))>64;
 INSERT INTO document_search_resources(doc_n,kind,resource_id)
 SELECT h.n,'comment',NEW.event_id FROM documents d JOIN document_search_heads h ON h.document_id=d.id WHERE d.thread_id=NEW.thread_id ON CONFLICT DO NOTHING;
 INSERT INTO document_search_dirty SELECT id FROM documents WHERE thread_id=NEW.thread_id ON CONFLICT DO NOTHING;
 UPDATE document_search_state SET generation=generation+1 WHERE singleton=1;
END;
CREATE TRIGGER IF NOT EXISTS document_search_comment_update AFTER UPDATE ON comment_search_heads BEGIN
 SELECT RAISE(ABORT,'too many document bindings for one comment') WHERE
 (SELECT count(*) FROM (SELECT 1 FROM documents WHERE thread_id=NEW.thread_id LIMIT 65))>64;
 INSERT INTO document_search_dirty SELECT id FROM documents WHERE thread_id IN(OLD.thread_id,NEW.thread_id) ON CONFLICT DO NOTHING;
 DELETE FROM document_search_resources WHERE kind='comment' AND resource_id=OLD.event_id;
 INSERT INTO document_search_resources(doc_n,kind,resource_id)
 SELECT h.n,'comment',NEW.event_id FROM documents d JOIN document_search_heads h ON h.document_id=d.id WHERE d.thread_id=NEW.thread_id;
 UPDATE document_search_state SET generation=generation+1 WHERE singleton=1;
END;
CREATE TRIGGER IF NOT EXISTS document_search_comment_delete BEFORE DELETE ON comment_search_heads BEGIN
 INSERT INTO document_search_dirty SELECT id FROM documents WHERE thread_id=OLD.thread_id ON CONFLICT DO NOTHING;
 DELETE FROM document_search_resources WHERE kind='comment' AND resource_id=OLD.event_id;
 UPDATE document_search_state SET generation=generation+1 WHERE singleton=1;
END;
CREATE TRIGGER IF NOT EXISTS document_search_document_insert AFTER INSERT ON documents BEGIN
 SELECT RAISE(ABORT,'too many documents sharing one thread') WHERE
 (SELECT count(*) FROM (SELECT 1 FROM documents WHERE thread_id=NEW.thread_id LIMIT 65))>64;
 INSERT INTO document_search_heads(document_id) VALUES(NEW.id) ON CONFLICT DO NOTHING;
 INSERT INTO document_search_links_pending(document_id) VALUES(NEW.id) ON CONFLICT DO NOTHING;
 INSERT INTO document_search_dirty VALUES(NEW.id) ON CONFLICT DO NOTHING;
 UPDATE document_search_state SET generation=generation+1 WHERE singleton=1;
END;
CREATE TRIGGER IF NOT EXISTS document_search_document_update AFTER UPDATE ON documents BEGIN
 SELECT RAISE(ABORT,'too many documents sharing one thread') WHERE
 (SELECT count(*) FROM (SELECT 1 FROM documents WHERE thread_id=NEW.thread_id LIMIT 65))>64;
 DELETE FROM document_search_heads WHERE document_id=OLD.id AND OLD.id<>NEW.id;
 INSERT INTO document_search_dirty SELECT OLD.id WHERE OLD.id<>NEW.id ON CONFLICT DO NOTHING;
 INSERT INTO document_search_heads(document_id) VALUES(NEW.id) ON CONFLICT DO NOTHING;
 UPDATE document_search_heads SET ready=CASE WHEN body_revision=NEW.head_revision_id THEN ready ELSE 0 END,
 projection_json=json_patch(anx_comment_search_projection(CAST(COALESCE(NEW.title,'')||' '||NEW.summary||' '||NEW.source||' '||NEW.tags_json||' '||body_text AS BLOB)),CASE WHEN body_truncated THEN '{"truncated":true}' ELSE '{}' END) WHERE document_id=NEW.id;
 DELETE FROM document_search_resources WHERE kind='comment' AND doc_n=(SELECT n FROM document_search_heads WHERE document_id=NEW.id) AND COALESCE(OLD.thread_id,'')<>COALESCE(NEW.thread_id,'');
 INSERT INTO document_search_links_pending(document_id) SELECT NEW.id WHERE COALESCE(OLD.thread_id,'')<>COALESCE(NEW.thread_id,'') ON CONFLICT(document_id) DO UPDATE SET cursor=NULL;
 INSERT INTO document_search_dirty VALUES(NEW.id) ON CONFLICT DO NOTHING;
END;
CREATE TRIGGER IF NOT EXISTS document_search_document_delete AFTER DELETE ON documents BEGIN
 DELETE FROM document_search_heads WHERE document_id=OLD.id;
 INSERT INTO document_search_dirty VALUES(OLD.id) ON CONFLICT DO NOTHING;
END;
`

func installDocumentSearch(ctx context.Context, tx *sql.Tx) error {
	exists, err := sqliteTableExists(ctx, tx, "documents")
	if err != nil || !exists {
		return err
	}
	_, err = tx.ExecContext(ctx, documentSearchSchema)
	if err != nil {
		return err
	}
	for _, table := range []string{"artifacts", "document_revisions"} {
		exists, err = sqliteTableExists(ctx, tx, table)
		if err != nil || !exists {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `CREATE TRIGGER IF NOT EXISTS document_search_artifact_update AFTER UPDATE OF id,content_hash,content_type ON artifacts WHEN OLD.id<>NEW.id OR OLD.content_hash<>NEW.content_hash OR OLD.content_type<>NEW.content_type BEGIN
 UPDATE document_search_heads SET ready=0 WHERE body_artifact=OLD.id;
 INSERT INTO document_search_dirty SELECT document_id FROM document_search_heads WHERE body_artifact=OLD.id ON CONFLICT DO NOTHING;
 END;
 CREATE TRIGGER IF NOT EXISTS document_search_revision_update AFTER UPDATE OF artifact_id,revision_id,document_id ON document_revisions WHEN OLD.artifact_id<>NEW.artifact_id OR OLD.revision_id<>NEW.revision_id OR OLD.document_id<>NEW.document_id BEGIN
 UPDATE document_search_heads SET ready=0 WHERE body_revision=OLD.revision_id;
 INSERT INTO document_search_dirty VALUES(OLD.document_id) ON CONFLICT DO NOTHING;
 END;
 CREATE TRIGGER IF NOT EXISTS document_search_artifact_delete BEFORE DELETE ON artifacts BEGIN
 UPDATE document_search_heads SET ready=0 WHERE body_artifact=OLD.id;
 INSERT INTO document_search_dirty SELECT document_id FROM document_search_heads WHERE body_artifact=OLD.id ON CONFLICT DO NOTHING;
 END;
 CREATE TRIGGER IF NOT EXISTS document_search_revision_delete BEFORE DELETE ON document_revisions BEGIN
 UPDATE document_search_heads SET ready=0 WHERE body_revision=OLD.revision_id;
 INSERT INTO document_search_dirty SELECT document_id FROM document_search_heads WHERE body_revision=OLD.revision_id ON CONFLICT DO NOTHING;
 END;`)
	return err
}
