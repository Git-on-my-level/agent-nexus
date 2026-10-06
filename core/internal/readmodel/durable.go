package readmodel

import (
	"context"
	"encoding/json"
	"math"

	"agent-nexus-core/internal/scopes"
)

// Unregistered proposal: D owns worker claims/leases and A owns installation.
const LifecycleSchemaProposal = `CREATE TABLE scope_feed_jobs (
 scope_id TEXT PRIMARY KEY, generation INTEGER NOT NULL, fence INTEGER NOT NULL,
 cursor INTEGER NOT NULL CHECK(cursor>=0), CHECK(generation=fence+1)
) WITHOUT ROWID;`

const FenceScope = `UPDATE scope_domains SET state='transitioning',generation=generation+1
 WHERE id=? AND state='active' AND generation=?`
const InsertJob = `INSERT INTO scope_feed_jobs(scope_id,generation,fence,cursor) VALUES(?,?,?,0)`
const ReadJob = `SELECT generation,fence,cursor FROM scope_feed_jobs WHERE scope_id=?`
const CheckpointJob = `UPDATE scope_feed_jobs SET cursor=?
 WHERE scope_id=? AND generation=? AND fence=? AND cursor=?
 AND EXISTS(SELECT 1 FROM scope_domains WHERE id=? AND state='transitioning' AND generation=?)`
const ActivateScope = `UPDATE scope_domains SET state='active',generation=?
 WHERE id=? AND state='transitioning' AND generation=?
 AND EXISTS(SELECT 1 FROM scope_feed_jobs WHERE scope_id=? AND generation=? AND fence=? AND cursor=?)
 AND EXISTS(SELECT 1 FROM scope_feed_generations g JOIN resource_access_epoch e ON e.singleton=1
 WHERE g.scope_id=? AND g.generation=? AND g.projection_version=1 AND g.audience_version=1
 AND g.lifecycle_version=1 AND g.legacy_auth_version=1 AND g.legacy_auth_epoch=e.version)`
const DeleteJob = `DELETE FROM scope_feed_jobs WHERE scope_id=? AND generation=? AND fence=? AND cursor=?`

// BeginLifecycle belongs in the SAME transaction as the canonical parent change.
// It fences in two statements without inspecting descendants. Every generation
// and fence is monotonic and reserved; no interrupted generation may be reused.
// While transitioning, canonical writes must be refused or restart under a new
// fence by A's capture hook. This function never creates readiness receipts.
func BeginLifecycle(ctx context.Context, tx Executor, scope scopes.ID, activeGeneration int64) error {
	if !boundedText(string(scope), 512) || activeGeneration < 1 || activeGeneration > math.MaxInt64-2 {
		return ErrProjection
	}
	if err := exact(ctx, tx, FenceScope, scope, activeGeneration); err != nil {
		return err
	}
	return exact(ctx, tx, InsertJob, scope, activeGeneration+2, activeGeneration+1)
}

type RebuildRecord struct {
	Row      LifecycleRow
	Payloads map[Stream]json.RawMessage
}

// RebuildLoader is D's indexed metadata/projection adapter. It must examine
// every candidate (including dead descendants) using (scope_id,rid)>cursor;
// it must not scan until it finds live rows. It shares QueryTx's transaction.
type RebuildLoader func(context.Context, Job, int) ([]RebuildRecord, error)

type DurableLifecycle struct {
	tx                QueryTx
	scope             scopes.ID
	load              RebuildLoader
	job               *Job
	records           []RebuildRecord
	loaded            bool
	processedNonempty bool
	lastRID           int64
}

func NewDurableLifecycle(tx QueryTx, scope scopes.ID, load RebuildLoader) *DurableLifecycle {
	return &DurableLifecycle{tx: tx, scope: scope, load: load}
}

func (d *DurableLifecycle) Job(ctx context.Context) (Job, error) {
	if d.job != nil {
		return *d.job, nil
	}
	if d.load == nil || !boundedText(string(d.scope), 512) {
		return Job{}, ErrProjection
	}
	rows, err := d.tx.Query(ctx, ReadJob, d.scope)
	if err != nil {
		return Job{}, err
	}
	defer rows.Close()
	j := Job{ScopeID: d.scope}
	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return Job{}, err
		}
		return Job{}, ErrProjection
	}
	if err = rows.Scan(&j.Generation, &j.Fence, &j.Cursor); err != nil {
		return Job{}, err
	}
	if rows.Next() || rows.Err() != nil || j.Fence < 1 || j.Fence == math.MaxInt64 || j.Generation != j.Fence+1 || j.Cursor < 0 {
		return Job{}, ErrProjection
	}
	d.job = &j
	return j, nil
}

func (d *DurableLifecycle) Rows(ctx context.Context, limit int) ([]LifecycleRow, error) {
	if d.loaded || limit != MaxLifecycleChunk {
		return nil, ErrBudget
	}
	j, err := d.Job(ctx)
	if err != nil {
		return nil, err
	}
	d.loaded = true
	d.records, err = d.load(ctx, j, limit)
	if err != nil {
		return nil, err
	}
	if len(d.records) > limit {
		return nil, ErrBudget
	}
	d.processedNonempty = len(d.records) != 0
	rows := make([]LifecycleRow, len(d.records))
	for i, record := range d.records {
		if err := validateProjectionPayloads(record.Row.Before, record.Row.After, record.Payloads); err != nil {
			return nil, err
		}
		rows[i] = record.Row
	}
	return rows, nil
}

func (d *DurableLifecycle) Apply(ctx context.Context, delta Delta) error {
	if !d.loaded || len(d.records) == 0 {
		return ErrProjection
	}
	// Step invokes Apply once for each record, including records with no feeds.
	record := d.records[0]
	expected, err := planDelta(record.Row.Before, record.Row.After, true)
	if err != nil || !sameDelta(expected, delta) {
		return ErrProjection
	}
	if err = ApplyProjection(ctx, d.tx, record.Row.Before, record.Row.After, record.Payloads, true); err != nil {
		return err
	}
	d.records = d.records[1:]
	d.lastRID = record.Row.RID
	return nil
}

func sameDelta(a, b Delta) bool {
	// Bounded metadata only (at most 16 changes); not stored history or payload.
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

func (d *DurableLifecycle) Checkpoint(ctx context.Context, j Job, cursor int64) error {
	if !d.loaded || len(d.records) != 0 || d.job == nil || j != *d.job || cursor <= j.Cursor || cursor != d.lastRID {
		return ErrProjection
	}
	return exact(ctx, d.tx, CheckpointJob, cursor, j.ScopeID, j.Generation, j.Fence, j.Cursor, j.ScopeID, j.Fence)
}

func (d *DurableLifecycle) Activate(ctx context.Context, j Job) error {
	if !d.loaded || len(d.records) != 0 || d.job == nil || j != *d.job {
		return ErrProjection
	}
	// Only Step's empty keyset seek may activate. No public Activate call after
	// a nonempty processed chunk can certify unseen descendants.
	if d.processedNonempty {
		return ErrProjection
	}
	if err := exact(ctx, d.tx, ActivateScope, j.Generation, j.ScopeID, j.Fence, j.ScopeID, j.Generation, j.Fence, j.Cursor, j.ScopeID, j.Generation); err != nil {
		return err
	}
	return exact(ctx, d.tx, DeleteJob, j.ScopeID, j.Generation, j.Fence, j.Cursor)
}
