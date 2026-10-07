package readmodel

// The following templates and schema are integration proposals for A's reviewed
// manifest/migration. Nothing in this package opens a database or installs them.
// Generation is in every key so a staged rebuild cannot alter active totals.
const SchemaProposal = `
CREATE TABLE scope_feed (
 scope_id TEXT NOT NULL, generation INTEGER NOT NULL,
 family TEXT NOT NULL, audience_key TEXT NOT NULL,
 sort_key INTEGER NOT NULL, rid INTEGER NOT NULL, version INTEGER NOT NULL,
 PRIMARY KEY(scope_id,generation,family,audience_key,sort_key,rid)
) WITHOUT ROWID;
CREATE UNIQUE INDEX scope_feed_resource
 ON scope_feed(scope_id,generation,family,audience_key,rid);
CREATE TABLE scope_counters (
 scope_id TEXT NOT NULL, generation INTEGER NOT NULL,
 family TEXT NOT NULL, audience_key TEXT NOT NULL, bucket TEXT NOT NULL,
 value INTEGER NOT NULL CHECK(typeof(value)='integer' AND value >= 0),
 PRIMARY KEY(scope_id,generation,family,audience_key,bucket)
) WITHOUT ROWID;
`

const SeekFeedStart = `SELECT sort_key,rid,version FROM scope_feed
 WHERE scope_id=? AND generation=? AND family=? AND audience_key=?
 ORDER BY sort_key,rid LIMIT ?`

const SeekFeedAfter = `SELECT sort_key,rid,version FROM scope_feed
 WHERE scope_id=? AND generation=? AND family=? AND audience_key=?
 AND (sort_key,rid)>(?,?) ORDER BY sort_key,rid LIMIT ?`

const ReadCounterBucket = `SELECT value FROM scope_counters
 WHERE scope_id=? AND generation=? AND family=? AND audience_key=? AND bucket=?`

const DeleteFeed = `DELETE FROM scope_feed WHERE scope_id=? AND generation=?
 AND family=? AND audience_key=? AND sort_key=? AND rid=? AND version=?`

const InsertFeed = `INSERT INTO scope_feed
 (scope_id,generation,family,audience_key,sort_key,rid,version) VALUES(?,?,?,?,?,?,?)`

// Deletes and negative deltas require an exact affected-row check; a missing or
// stale old feed must abort rather than decrement counters for an absent row.
// Negative deltas require an existing bucket and an exact affected-row check.
// Positive deltas can insert a missing bucket. Both use CHECK(value>=0), and
// the enclosing source transaction rolls back on overflow/constraint failure.
const IncrementCounter = `INSERT INTO scope_counters
 (scope_id,generation,family,audience_key,bucket,value) VALUES(?,?,?,?,?,?)
 ON CONFLICT(scope_id,generation,family,audience_key,bucket)
 DO UPDATE SET value=value+excluded.value`

const DecrementCounter = `UPDATE scope_counters SET value=value+?
 WHERE scope_id=? AND generation=? AND family=? AND audience_key=? AND bucket=?`
