-- Additive shadow schema. No production initializer invokes this yet; no released
-- migration number is allocated. All identities below are opaque external IDs.
-- Minted once by Initialize. Identity survives reopen/reinitialization and cannot
-- be replaced, deleted or edited (including INSERT OR REPLACE with triggers off).
CREATE TABLE IF NOT EXISTS scope_workspace_namespace (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 namespace TEXT NOT NULL CHECK(typeof(namespace)='text' AND length(CAST(namespace AS BLOB))=32 AND namespace NOT GLOB '*[^0-9a-f]*')
);
CREATE TRIGGER IF NOT EXISTS scope_workspace_namespace_no_update
BEFORE UPDATE ON scope_workspace_namespace
BEGIN SELECT RAISE(ABORT,'workspace namespace is immutable'); END;
CREATE TRIGGER IF NOT EXISTS scope_workspace_namespace_no_delete
BEFORE DELETE ON scope_workspace_namespace
BEGIN SELECT RAISE(ABORT,'workspace namespace remains reserved'); END;
CREATE TRIGGER IF NOT EXISTS scope_workspace_namespace_no_replace
BEFORE INSERT ON scope_workspace_namespace
WHEN EXISTS(SELECT 1 FROM scope_workspace_namespace)
BEGIN SELECT RAISE(ABORT,'workspace namespace remains reserved'); END;

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

-- The workspace connection pool does not enable foreign_keys. Persist these
-- guards in the schema so every connection enforces the declared relationships
-- without changing the behavior of unrelated legacy tables.
CREATE TRIGGER IF NOT EXISTS scope_membership_parent_insert
BEFORE INSERT ON scope_memberships
WHEN NOT EXISTS(SELECT 1 FROM scope_domains WHERE id=NEW.scope_id)
BEGIN SELECT RAISE(ABORT,'membership scope missing'); END;
CREATE TRIGGER IF NOT EXISTS scope_membership_parent_update
BEFORE UPDATE OF scope_id ON scope_memberships
WHEN NOT EXISTS(SELECT 1 FROM scope_domains WHERE id=NEW.scope_id)
BEGIN SELECT RAISE(ABORT,'membership scope missing'); END;
CREATE TRIGGER IF NOT EXISTS scope_resource_parent_insert
BEFORE INSERT ON scope_resources
WHEN NOT EXISTS(SELECT 1 FROM scope_domains WHERE id=NEW.scope_id)
BEGIN SELECT RAISE(ABORT,'resource scope missing'); END;
CREATE TRIGGER IF NOT EXISTS scope_alias_parent_insert
BEFORE INSERT ON scope_aliases
WHEN NOT EXISTS(SELECT 1 FROM scope_resources WHERE scope_id=NEW.scope_id AND kind=NEW.kind AND id=NEW.resource_id)
BEGIN SELECT RAISE(ABORT,'alias resource missing'); END;
CREATE TRIGGER IF NOT EXISTS scope_alias_parent_update
BEFORE UPDATE OF scope_id,kind,resource_id ON scope_aliases
WHEN NOT EXISTS(SELECT 1 FROM scope_resources WHERE scope_id=NEW.scope_id AND kind=NEW.kind AND id=NEW.resource_id)
BEGIN SELECT RAISE(ABORT,'alias resource missing'); END;
CREATE TRIGGER IF NOT EXISTS scope_projection_parent_insert
BEFORE INSERT ON scope_projection_values
WHEN NOT EXISTS(SELECT 1 FROM scope_domains WHERE id=NEW.scope_id)
BEGIN SELECT RAISE(ABORT,'projection scope missing'); END;
CREATE TRIGGER IF NOT EXISTS scope_projection_parent_update
BEFORE UPDATE OF scope_id ON scope_projection_values
WHEN NOT EXISTS(SELECT 1 FROM scope_domains WHERE id=NEW.scope_id)
BEGIN SELECT RAISE(ABORT,'projection scope missing'); END;
CREATE TRIGGER IF NOT EXISTS scope_rid_parent_insert
BEFORE INSERT ON scope_resource_rids
WHEN NOT EXISTS(SELECT 1 FROM scope_resources WHERE scope_id=NEW.scope_id AND kind=NEW.kind AND id=NEW.resource_id)
BEGIN SELECT RAISE(ABORT,'RID resource missing'); END;
CREATE TRIGGER IF NOT EXISTS scope_domain_children_delete
BEFORE DELETE ON scope_domains
WHEN EXISTS(SELECT 1 FROM scope_memberships WHERE scope_id=OLD.id)
 OR EXISTS(SELECT 1 FROM scope_resources WHERE scope_id=OLD.id)
 OR EXISTS(SELECT 1 FROM scope_projection_values WHERE scope_id=OLD.id)
BEGIN SELECT RAISE(ABORT,'scope still has children'); END;

-- The opaque handle and RID reserve one canonical source forever. Only version
-- advances are mutable; moves/reclassification require a separate reviewed path.
-- BEFORE INSERT covers both uniqueness keys even with recursive_triggers=OFF,
-- where INSERT OR REPLACE otherwise silently deletes the conflicting parent.
CREATE TRIGGER IF NOT EXISTS scope_resource_source_immutable
BEFORE UPDATE OF scope_id,kind,id,canonical_id ON scope_resources
WHEN NEW.scope_id IS NOT OLD.scope_id OR NEW.kind IS NOT OLD.kind
 OR NEW.id IS NOT OLD.id OR NEW.canonical_id IS NOT OLD.canonical_id
BEGIN SELECT RAISE(ABORT,'resource canonical source is immutable'); END;
CREATE TRIGGER IF NOT EXISTS scope_resource_reserved
BEFORE DELETE ON scope_resources
BEGIN SELECT RAISE(ABORT,'resource identity remains reserved'); END;
CREATE TRIGGER IF NOT EXISTS scope_resource_no_replace
BEFORE INSERT ON scope_resources
WHEN EXISTS(SELECT 1 FROM scope_resources WHERE scope_id=NEW.scope_id AND kind=NEW.kind AND id=NEW.id)
 OR EXISTS(SELECT 1 FROM scope_resources WHERE kind=NEW.kind AND canonical_id=NEW.canonical_id)
BEGIN SELECT RAISE(ABORT,'resource identity remains reserved'); END;

-- INSERT OR REPLACE otherwise bypasses delete triggers with recursive triggers off.
CREATE TRIGGER IF NOT EXISTS scope_resource_rid_no_replace
BEFORE INSERT ON scope_resource_rids
WHEN EXISTS(SELECT 1 FROM scope_resource_rids WHERE rid=NEW.rid)
 OR EXISTS(SELECT 1 FROM scope_resource_rids WHERE scope_id=NEW.scope_id AND kind=NEW.kind AND resource_id=NEW.resource_id)
BEGIN SELECT RAISE(ABORT,'resource RID remains reserved'); END;
