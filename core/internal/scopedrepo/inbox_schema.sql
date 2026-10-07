-- Disabled, empty shadow schema. No startup registration or certification.
-- Inbox ordering lives here only; it must not also use the integer scope_feed.
CREATE TABLE scope_inbox_order (
 scope_id TEXT NOT NULL, generation INTEGER NOT NULL CHECK(typeof(generation)='integer' AND generation>0),
 family TEXT NOT NULL CHECK(family='inbox'), audience_key TEXT NOT NULL,
 order_key BLOB NOT NULL CHECK(typeof(order_key)='blob' AND length(order_key) BETWEEN 1 AND 770),
 rid INTEGER NOT NULL CHECK(typeof(rid)='integer' AND rid>0),
 version INTEGER NOT NULL CHECK(typeof(version)='integer' AND version>0),
 PRIMARY KEY(scope_id,generation,family,audience_key,order_key,rid)
) WITHOUT ROWID;
CREATE UNIQUE INDEX scope_inbox_order_resource
 ON scope_inbox_order(scope_id,generation,family,audience_key,rid);
CREATE TRIGGER scope_inbox_order_proof_insert AFTER INSERT ON scope_inbox_order
BEGIN UPDATE scope_feed_proof_clock SET revision=revision+1 WHERE singleton=1; END;
CREATE TRIGGER scope_inbox_order_proof_update AFTER UPDATE ON scope_inbox_order
BEGIN UPDATE scope_feed_proof_clock SET revision=revision+1 WHERE singleton=1; END;
CREATE TRIGGER scope_inbox_order_proof_delete AFTER DELETE ON scope_inbox_order
BEGIN UPDATE scope_feed_proof_clock SET revision=revision+1 WHERE singleton=1; END;
