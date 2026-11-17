package primitives

import (
	"errors"
)

const MaxScopeInboxPayloadBytes = 16 * 1024

var ErrScopeInboxProjection = errors.New("invalid scoped inbox projection")

// Empty shadow DDL/query proposals for A's migration/template registry. They
// are never executed by this package or installed in a production workspace.
const ScopeInboxOrderSchemaProposal = `CREATE TABLE scope_inbox_order (
 scope_id TEXT NOT NULL,generation INTEGER NOT NULL,family TEXT NOT NULL,audience_key TEXT NOT NULL,
 order_key BLOB NOT NULL CHECK(typeof(order_key)='blob' AND length(order_key)<=770),
 rid INTEGER NOT NULL,version INTEGER NOT NULL,
 PRIMARY KEY(scope_id,generation,family,audience_key,order_key,rid)
) WITHOUT ROWID;
CREATE UNIQUE INDEX scope_inbox_order_resource ON scope_inbox_order(scope_id,generation,family,audience_key,rid);`

const ScopeInboxOrderStartProposal = `SELECT order_key,rid,version FROM scope_inbox_order
 WHERE scope_id=? AND generation=? AND family=? AND audience_key=? ORDER BY order_key,rid LIMIT ?`
const ScopeInboxOrderAfterProposal = `SELECT order_key,rid,version FROM scope_inbox_order
 WHERE scope_id=? AND generation=? AND family=? AND audience_key=? AND (order_key,rid)>(?,?)
 ORDER BY order_key,rid LIMIT ?`
