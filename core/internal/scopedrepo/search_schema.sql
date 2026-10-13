-- Shadow generation only. Never initialized by workspace startup or serving.
CREATE TABLE IF NOT EXISTS scope_search_bindings (
 scope_id TEXT NOT NULL, kind TEXT NOT NULL CHECK(kind IN ('document','comment')),
 rid TEXT NOT NULL, parent TEXT NOT NULL,
 PRIMARY KEY(scope_id,kind,rid),
 CHECK((kind='document' AND parent='') OR (kind='comment' AND parent<>''))
) WITHOUT ROWID;
CREATE TRIGGER IF NOT EXISTS scope_search_binding_insert BEFORE INSERT ON scope_search_bindings
BEGIN
 SELECT RAISE(ABORT,'search identity unavailable') WHERE NOT EXISTS
  (SELECT 1 FROM scope_resources WHERE scope_id=NEW.scope_id AND kind=NEW.kind AND id=NEW.rid);
 SELECT RAISE(ABORT,'search parent unavailable') WHERE NEW.kind='comment' AND NOT EXISTS
  (SELECT 1 FROM scope_resources WHERE scope_id=NEW.scope_id AND kind='document' AND id=NEW.parent);
 SELECT RAISE(ABORT,'search parent is immutable') WHERE EXISTS
  (SELECT 1 FROM scope_search_bindings WHERE scope_id=NEW.scope_id AND kind=NEW.kind AND rid=NEW.rid AND parent<>NEW.parent);
END;
CREATE TRIGGER IF NOT EXISTS scope_search_binding_update BEFORE UPDATE ON scope_search_bindings
BEGIN SELECT RAISE(ABORT,'search binding is immutable'); END;
CREATE TRIGGER IF NOT EXISTS scope_search_binding_delete BEFORE DELETE ON scope_search_bindings
BEGIN SELECT RAISE(ABORT,'search binding remains reserved'); END;

CREATE TABLE IF NOT EXISTS scope_search_generations (
 scope_id TEXT PRIMARY KEY, generation INTEGER NOT NULL CHECK(typeof(generation)='integer' AND generation>0)
) WITHOUT ROWID;
CREATE TRIGGER IF NOT EXISTS scope_search_generation_insert BEFORE INSERT ON scope_search_generations
BEGIN
 SELECT RAISE(ABORT,'search scope unavailable') WHERE NOT EXISTS(SELECT 1 FROM scope_domains WHERE id=NEW.scope_id);
 SELECT RAISE(ABORT,'search generation remains reserved') WHERE EXISTS(SELECT 1 FROM scope_search_generations WHERE scope_id=NEW.scope_id);
END;
CREATE TRIGGER IF NOT EXISTS scope_search_generation_update BEFORE UPDATE ON scope_search_generations
WHEN NEW.scope_id<>OLD.scope_id OR NEW.generation<=OLD.generation
BEGIN SELECT RAISE(ABORT,'search generation must advance'); END;
CREATE TRIGGER IF NOT EXISTS scope_search_generation_delete BEFORE DELETE ON scope_search_generations
BEGIN SELECT RAISE(ABORT,'search generation remains reserved'); END;

CREATE TABLE IF NOT EXISTS scope_search_resources (
 scope_id TEXT NOT NULL, kind TEXT NOT NULL, rid TEXT NOT NULL,
 version INTEGER NOT NULL CHECK(typeof(version)='integer' AND version>0),
 parent TEXT NOT NULL, sort_key INTEGER NOT NULL CHECK(sort_key<=0),
 text TEXT NOT NULL, indexed_bytes INTEGER NOT NULL CHECK(indexed_bytes BETWEEN 0 AND 65536 AND indexed_bytes=length(CAST(text AS BLOB))),
 truncated INTEGER NOT NULL CHECK(truncated IN (0,1)),
 PRIMARY KEY(scope_id,kind,rid)
) WITHOUT ROWID;
CREATE TRIGGER IF NOT EXISTS scope_search_resource_insert BEFORE INSERT ON scope_search_resources
BEGIN
 SELECT RAISE(ABORT,'search binding unavailable') WHERE NOT EXISTS
  (SELECT 1 FROM scope_search_bindings WHERE scope_id=NEW.scope_id AND kind=NEW.kind AND rid=NEW.rid AND parent=NEW.parent);
 SELECT RAISE(ABORT,'search version unavailable') WHERE NOT EXISTS
  (SELECT 1 FROM scope_resources WHERE scope_id=NEW.scope_id AND kind=NEW.kind AND id=NEW.rid AND version=NEW.version);
 SELECT RAISE(ABORT,'search head must be removed first') WHERE EXISTS
  (SELECT 1 FROM scope_search_resources WHERE scope_id=NEW.scope_id AND kind=NEW.kind AND rid=NEW.rid);
END;
CREATE TRIGGER IF NOT EXISTS scope_search_resource_update BEFORE UPDATE ON scope_search_resources
BEGIN SELECT RAISE(ABORT,'replace search head atomically'); END;

CREATE TABLE IF NOT EXISTS scope_search_postings (
 scope_id TEXT NOT NULL, term TEXT NOT NULL CHECK(length(CAST(term AS BLOB)) BETWEEN 1 AND 65536),
 sort_key INTEGER NOT NULL, rid TEXT NOT NULL, kind TEXT NOT NULL,
 slot INTEGER NOT NULL CHECK(typeof(slot)='integer' AND slot BETWEEN 0 AND 4095),
 PRIMARY KEY(scope_id,term,sort_key,rid,kind),
 UNIQUE(scope_id,kind,rid,slot)
) WITHOUT ROWID;
CREATE UNIQUE INDEX IF NOT EXISTS scope_search_postings_resource ON scope_search_postings(scope_id,kind,rid,term);
CREATE TRIGGER IF NOT EXISTS scope_search_posting_insert BEFORE INSERT ON scope_search_postings
WHEN NOT EXISTS(SELECT 1 FROM scope_search_resources WHERE scope_id=NEW.scope_id AND kind=NEW.kind AND rid=NEW.rid AND sort_key=NEW.sort_key)
BEGIN SELECT RAISE(ABORT,'search head unavailable'); END;
CREATE TRIGGER IF NOT EXISTS scope_search_posting_update BEFORE UPDATE ON scope_search_postings
BEGIN SELECT RAISE(ABORT,'replace search postings atomically'); END;
CREATE TRIGGER IF NOT EXISTS scope_search_resource_delete BEFORE DELETE ON scope_search_resources
WHEN EXISTS(SELECT 1 FROM scope_search_postings INDEXED BY scope_search_postings_resource WHERE scope_id=OLD.scope_id AND kind=OLD.kind AND rid=OLD.rid)
BEGIN SELECT RAISE(ABORT,'remove search postings before head'); END;
