-- Additive shadow schema. No production initializer invokes this yet; no released
-- migration number is allocated. All identities below are opaque external IDs.
CREATE TABLE IF NOT EXISTS scope_domains (
 id TEXT PRIMARY KEY,
 state TEXT NOT NULL CHECK(state IN ('active','transitioning','inaccessible')),
 generation INTEGER NOT NULL CHECK(generation > 0)
) WITHOUT ROWID;
CREATE INDEX IF NOT EXISTS scope_domains_state ON scope_domains(state,id);
CREATE TABLE IF NOT EXISTS scope_memberships (
 principal TEXT NOT NULL,
 scope_id TEXT NOT NULL,
 role TEXT NOT NULL CHECK(role IN ('reader','writer','owner','admin')),
 generation INTEGER NOT NULL CHECK(generation > 0),
 PRIMARY KEY(principal,scope_id),
 FOREIGN KEY(scope_id) REFERENCES scope_domains(id)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS scope_resources (
 scope_id TEXT NOT NULL,
 kind TEXT NOT NULL,
 id TEXT NOT NULL,
 canonical_id TEXT NOT NULL,
 version INTEGER NOT NULL CHECK(version > 0),
 PRIMARY KEY(scope_id,kind,id),
 UNIQUE(kind,canonical_id),
 FOREIGN KEY(scope_id) REFERENCES scope_domains(id)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS scope_aliases (
 scope_id TEXT NOT NULL,
 kind TEXT NOT NULL,
 alias TEXT NOT NULL,
 resource_id TEXT NOT NULL,
 retired INTEGER NOT NULL DEFAULT 0 CHECK(retired IN (0,1)),
 PRIMARY KEY(scope_id,kind,alias),
 FOREIGN KEY(scope_id,kind,resource_id) REFERENCES scope_resources(scope_id,kind,id)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS scope_replays (
 principal TEXT NOT NULL,
 replay_key TEXT NOT NULL,
 scope_id TEXT NOT NULL,
 kind TEXT NOT NULL,
 request_hash TEXT NOT NULL,
 resource_id TEXT NOT NULL,
 PRIMARY KEY(principal,replay_key)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS scope_projection_values (
 scope_id TEXT NOT NULL,
 projection_key TEXT NOT NULL,
 value TEXT NOT NULL CHECK(length(CAST(value AS BLOB)) <= 65536),
 PRIMARY KEY(scope_id,projection_key),
 FOREIGN KEY(scope_id) REFERENCES scope_domains(id)
) WITHOUT ROWID;

-- No-grants legacy exceptions stay out of the all-active-scopes PM directory.
-- Indexed EXISTS checks examine at most one membership candidate.
CREATE INDEX IF NOT EXISTS scope_memberships_by_scope ON scope_memberships(scope_id,principal);
CREATE TRIGGER IF NOT EXISTS scope_no_grants_insert
BEFORE INSERT ON scope_memberships
WHEN EXISTS(SELECT 1 FROM scope_domains WHERE id=NEW.scope_id AND state='inaccessible')
BEGIN SELECT RAISE(ABORT,'scope does not admit grants'); END;
CREATE TRIGGER IF NOT EXISTS scope_no_grants_update
BEFORE UPDATE OF scope_id ON scope_memberships
WHEN EXISTS(SELECT 1 FROM scope_domains WHERE id=NEW.scope_id AND state='inaccessible')
BEGIN SELECT RAISE(ABORT,'scope does not admit grants'); END;
CREATE TRIGGER IF NOT EXISTS scope_no_grants_state
BEFORE UPDATE OF state ON scope_domains
WHEN NEW.state='inaccessible' AND EXISTS(SELECT 1 FROM scope_memberships WHERE scope_id=NEW.id)
BEGIN SELECT RAISE(ABORT,'scope still has grants'); END;
CREATE TRIGGER IF NOT EXISTS scope_no_grants_create
BEFORE INSERT ON scope_domains
WHEN NEW.state='inaccessible' AND EXISTS(SELECT 1 FROM scope_memberships WHERE scope_id=NEW.id)
BEGIN SELECT RAISE(ABORT,'scope still has grants'); END;
CREATE TRIGGER IF NOT EXISTS scope_no_grants_reactivate
BEFORE UPDATE OF state ON scope_domains
WHEN OLD.state='inaccessible' AND NEW.state!='inaccessible'
BEGIN SELECT RAISE(ABORT,'scope is permanently inaccessible'); END;
CREATE TRIGGER IF NOT EXISTS scope_no_grants_replace
BEFORE INSERT ON scope_domains
WHEN NEW.state!='inaccessible' AND EXISTS(SELECT 1 FROM scope_domains WHERE id=NEW.id AND state='inaccessible')
BEGIN SELECT RAISE(ABORT,'scope is permanently inaccessible'); END;
CREATE TRIGGER IF NOT EXISTS scope_identity_immutable
BEFORE UPDATE OF id ON scope_domains
WHEN NEW.id!=OLD.id
BEGIN SELECT RAISE(ABORT,'scope identity is immutable'); END;
CREATE TRIGGER IF NOT EXISTS scope_no_grants_delete
BEFORE DELETE ON scope_domains
WHEN OLD.state='inaccessible'
BEGIN SELECT RAISE(ABORT,'scope identity remains reserved'); END;

-- Private integer keys are never external handles. The insert trigger gives
-- each new registry identity one globally unique RID in the same transaction.
CREATE TABLE IF NOT EXISTS scope_resource_rids (
 rid INTEGER PRIMARY KEY AUTOINCREMENT CHECK(rid > 0),
 scope_id TEXT NOT NULL,
 kind TEXT NOT NULL,
 resource_id TEXT NOT NULL,
 UNIQUE(scope_id,kind,resource_id),
 FOREIGN KEY(scope_id,kind,resource_id) REFERENCES scope_resources(scope_id,kind,id)
);
CREATE TRIGGER IF NOT EXISTS scope_resource_rid_allocate
AFTER INSERT ON scope_resources
BEGIN INSERT INTO scope_resource_rids(scope_id,kind,resource_id) VALUES(NEW.scope_id,NEW.kind,NEW.id); END;
CREATE TRIGGER IF NOT EXISTS scope_resource_rid_immutable
BEFORE UPDATE ON scope_resource_rids
BEGIN SELECT RAISE(ABORT,'resource RID is immutable'); END;
CREATE TRIGGER IF NOT EXISTS scope_resource_rid_reserved
BEFORE DELETE ON scope_resource_rids
BEGIN SELECT RAISE(ABORT,'resource RID remains reserved'); END;

-- INSERT OR REPLACE otherwise bypasses delete triggers with recursive triggers off.
CREATE TRIGGER IF NOT EXISTS scope_resource_rid_no_replace
BEFORE INSERT ON scope_resource_rids
WHEN EXISTS(SELECT 1 FROM scope_resource_rids WHERE rid=NEW.rid)
 OR EXISTS(SELECT 1 FROM scope_resource_rids WHERE scope_id=NEW.scope_id AND kind=NEW.kind AND resource_id=NEW.resource_id)
BEGIN SELECT RAISE(ABORT,'resource RID remains reserved'); END;
