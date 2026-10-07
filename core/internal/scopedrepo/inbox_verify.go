package scopedrepo

import (
	"bytes"
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/scopes"
)

const InboxVerificationSliceLimit = 64

var ErrInboxVerificationMismatch = errors.New("inbox canonical comparison mismatch")
var ErrInboxVerificationCoverage = errors.New("inbox canonical source coverage unavailable")
var ErrInboxVerificationStale = errors.New("inbox verification snapshot changed")

//go:embed inbox_proof_schema.sql
var inboxProofSchema string

// InitializeInboxVerificationSchema installs empty, disabled maintenance tables.
// It is not a migration or a production initializer. Install canonical invalidation
// AFTER all shadow DDL so its schema-version fence covers the final schema.
func (s *Store) InitializeInboxVerificationSchema(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, inboxProofSchema); err != nil {
		return err
	}
	return tx.Commit()
}

type inboxVerificationFence struct {
	Source, Authority, Directory, Shadow, Legacy int64
	Registry                                     string
}

func inboxVerificationClock(ctx context.Context, tx *sql.Tx) (inboxVerificationFence, error) {
	var f inboxVerificationFence
	if _, err := primitives.ReadScopeInboxSourceSnapshot(ctx, tx); err != nil {
		return f, fmt.Errorf("%w: %v", ErrInboxVerificationCoverage, err)
	}
	var complete, installed, current int64
	err := tx.QueryRowContext(ctx, `SELECT c.source_revision,c.authority_revision,c.directory_revision,p.revision,e.version,c.registry_hash,c.installation_complete,c.installed_schema_version,v.schema_version
 FROM scope_inbox_source_clock c JOIN scope_feed_proof_clock p ON p.singleton=1
 JOIN resource_access_epoch e ON e.singleton=1 CROSS JOIN pragma_schema_version v WHERE c.singleton=1`).Scan(&f.Source, &f.Authority, &f.Directory, &f.Shadow, &f.Legacy, &f.Registry, &complete, &installed, &current)
	if err != nil {
		return f, fmt.Errorf("%w: %v", ErrInboxVerificationCoverage, err)
	}
	if complete != 1 || installed != current || f.Source < 1 || f.Authority < 1 || f.Directory < 1 || f.Shadow < 1 || f.Legacy < 0 || len(f.Registry) != 64 {
		return f, ErrInboxVerificationCoverage
	}
	return f, nil
}

// StartInboxVerification accepts identity, never scope selections, source rows,
// cursors, callbacks, readiness assertions or certificates. The trusted caller
// must authenticate identity; this disabled maintenance API is not an HTTP API.
func (s *Store) StartInboxVerification(ctx context.Context, principal, pmActor string) (string, error) {
	if !feedText(principal, 512) || pmActor != "" && !feedText(pmActor, 512) {
		return "", scopes.ErrBudget
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	f, err := inboxVerificationClock(ctx, tx)
	if err != nil {
		return "", err
	}
	id, err := opaque()
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO scope_inbox_verification_jobs(id,principal,pm_actor_id,source_revision,authority_revision,directory_revision,shadow_revision,legacy_epoch,registry_hash,phase) VALUES(?,?,?,?,?,?,?,?,?,'directory')`, id, principal, pmActor, f.Source, f.Authority, f.Directory, f.Shadow, f.Legacy, f.Registry)
	if err != nil {
		return "", err
	}
	return id, tx.Commit()
}

type InboxVerificationProgress struct {
	Examined                int
	TotalExamined, Eligible int64
	Complete                bool
	// ServingProof is always false: canonical base rows do not cover route enrichments.
	ServingProof bool
}
type inboxVerificationJob struct {
	Principal, PM, Phase          string
	Fence                         inboxVerificationFence
	Cursor                        inboxVerificationCursor
	Examined, Canonical, Eligible int64
}
type inboxVerificationCursor struct {
	ID                       string
	Scope, Family, Audience  string
	Generation, RID, Version int64
	Bucket                   string
}

// RunInboxVerificationSlice examines at most 64 enumeration rows. Every slice
// atomically saves progress; the caller only supplies an opaque job identity.
// All canonical and shadow work shares one transaction and all persisted epoch
// fences are checked before resume and receipt publication. No completed cursor
// supplied by another worker can establish completeness.
func (s *Store) RunInboxVerificationSlice(ctx context.Context, id string) (InboxVerificationProgress, error) {
	var out InboxVerificationProgress
	if !feedText(id, 64) {
		return out, scopes.ErrBudget
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	var j inboxVerificationJob
	var raw, failure string
	err = tx.QueryRowContext(ctx, `SELECT principal,pm_actor_id,source_revision,authority_revision,directory_revision,shadow_revision,legacy_epoch,registry_hash,phase,checkpoint,examined,canonical_rows,eligible,failure FROM scope_inbox_verification_jobs WHERE id=?`, id).Scan(&j.Principal, &j.PM, &j.Fence.Source, &j.Fence.Authority, &j.Fence.Directory, &j.Fence.Shadow, &j.Fence.Legacy, &j.Fence.Registry, &j.Phase, &raw, &j.Examined, &j.Canonical, &j.Eligible, &failure)
	if err != nil {
		return out, err
	}
	if failure != "" {
		return out, ErrInboxVerificationMismatch
	}
	if json.Unmarshal([]byte(raw), &j.Cursor) != nil {
		return out, ErrInboxVerificationMismatch
	}
	fence, err := inboxVerificationClock(ctx, tx)
	if err == nil && fence != j.Fence {
		err = ErrInboxVerificationStale
	}
	if err != nil {
		return out, err
	}
	if j.Phase == "done" {
		var exists int
		err = tx.QueryRowContext(ctx, `SELECT count(*) FROM scope_inbox_comparison_receipts WHERE job_id=? AND principal=? AND pm_actor_id=? AND source_revision=? AND authority_revision=? AND directory_revision=? AND shadow_revision=? AND legacy_epoch=? AND registry_hash=? AND policy_version=? AND projector_version=? AND canonical_rows=? AND eligible_rows=? AND comparison_kind='canonical_base_v1' AND serving_eligible=0`, id, j.Principal, j.PM, j.Fence.Source, j.Fence.Authority, j.Fence.Directory, j.Fence.Shadow, j.Fence.Legacy, j.Fence.Registry, feedPolicyVersion, feedProjectorVersion, j.Canonical, j.Eligible).Scan(&exists)
		if err != nil {
			return out, err
		}
		if exists != 1 {
			return out, ErrInboxVerificationMismatch
		}
		out.Complete = true
		out.TotalExamined = j.Examined
		out.Eligible = j.Eligible
		return out, tx.Commit()
	}
	switch j.Phase {
	case "directory":
		out.Examined, err = inboxVerifyDirectory(ctx, tx, id, &j)
	case "bindings":
		out.Examined, err = inboxVerifyBindings(ctx, tx, id, &j)
	case "canonical":
		out.Examined, err = inboxVerifyCanonical(ctx, tx, id, &j)
	case "ordered", "payloads":
		out.Examined, err = inboxVerifyReverse(ctx, tx, id, &j)
	case "counters":
		out.Examined, err = inboxVerifyCounters(ctx, tx, id, &j)
	case "missing_counters":
		out.Examined, err = inboxVerifyMissingCounters(ctx, tx, id, &j)
	default:
		err = ErrInboxVerificationMismatch
	}
	if err != nil {
		// Persist refusal, never partial comparison success. Generic SQL/cancellation
		// errors roll back so a transient failure can resume the same durable cursor.
		if errors.Is(err, ErrInboxVerificationMismatch) || errors.Is(err, scopes.ErrBudget) || errors.Is(err, scopes.ErrUpdating) {
			if _, e := tx.ExecContext(ctx, `UPDATE scope_inbox_verification_jobs SET failure=? WHERE id=?`, err.Error(), id); e != nil {
				return out, e
			}
			if e := tx.Commit(); e != nil {
				return out, e
			}
		}
		return out, err
	}
	j.Examined += int64(out.Examined)
	rawBytes, err := json.Marshal(j.Cursor)
	if err != nil {
		return out, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE scope_inbox_verification_jobs SET phase=?,checkpoint=?,examined=?,canonical_rows=?,eligible=? WHERE id=?`, j.Phase, string(rawBytes), j.Examined, j.Canonical, j.Eligible, id); err != nil {
		return out, err
	}
	if j.Phase == "done" {
		// This receipt deliberately cannot satisfy scope_feed_selection_proofs, nor
		// mark directory_coverage_revision. Complete HTTP/PM enrichment is a later gate.
		_, err = tx.ExecContext(ctx, `INSERT INTO scope_inbox_comparison_receipts VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,'canonical_base_v1',0)`, id, j.Principal, j.PM, j.Fence.Source, j.Fence.Authority, j.Fence.Directory, j.Fence.Shadow, j.Fence.Legacy, j.Fence.Registry, feedPolicyVersion, feedProjectorVersion, j.Canonical, j.Eligible)
		if err != nil {
			return out, err
		}
		out.Complete = true
	}
	out.TotalExamined = j.Examined
	out.Eligible = j.Eligible
	return out, tx.Commit()
}

func inboxVerificationNext(j *inboxVerificationJob, phase string) {
	j.Phase = phase
	j.Cursor = inboxVerificationCursor{}
}

func inboxVerifyDirectory(ctx context.Context, tx *sql.Tx, id string, j *inboxVerificationJob) (int, error) {
	q := `SELECT m.scope_id,m.role,m.generation,d.state,d.generation FROM scope_memberships m LEFT JOIN scope_domains d ON d.id=m.scope_id WHERE m.principal=?`
	args := []any{j.Principal}
	if j.Cursor.Generation != 0 {
		q += ` AND m.scope_id>?`
		args = append(args, j.Cursor.Scope)
	}
	q += ` ORDER BY m.scope_id LIMIT 64`
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type entry struct {
		scope, role, state     string
		membership, generation int64
	}
	var entries []entry
	for rows.Next() {
		var v entry
		if err = rows.Scan(&v.scope, &v.role, &v.membership, &v.state, &v.generation); err != nil {
			return 0, err
		}
		entries = append(entries, v)
	}
	if err = rows.Err(); err != nil {
		return 0, err
	}
	rows.Close()
	var n int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM scope_inbox_verification_scopes WHERE job_id=?`, id).Scan(&n); err != nil {
		return 0, err
	}
	if n+len(entries) > scopes.MaxScopes {
		return len(entries), scopes.ErrBudget
	}
	for _, v := range entries {
		if !feedText(v.scope, 512) || !scopes.Role(v.role).CanRead() || v.membership < 1 || v.generation < 1 {
			return len(entries), ErrInboxVerificationMismatch
		}
		if v.state != "active" {
			return len(entries), scopes.ErrUpdating
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO scope_inbox_verification_scopes VALUES(?,?,?,?,?)`, id, v.scope, v.generation, v.role, v.membership); err != nil {
			return len(entries), err
		}
		j.Cursor.Scope = v.scope
		j.Cursor.Generation = 1
	}
	if len(entries) < 64 {
		inboxVerificationNext(j, "bindings")
	}
	return len(entries), nil
}

func inboxVerifyBindings(ctx context.Context, tx *sql.Tx, id string, j *inboxVerificationJob) (int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT scope_id,generation,family,audience_key,membership_generation,binding_generation FROM scope_feed_bindings WHERE principal=? AND (scope_id,generation,family,audience_key)>(?,?,?,?) ORDER BY scope_id,generation,family,audience_key LIMIT 64`, j.Principal, j.Cursor.Scope, j.Cursor.Generation, j.Cursor.Family, j.Cursor.Audience)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type entry struct {
		scope, family, audience         string
		generation, membership, binding int64
	}
	var entries []entry
	for rows.Next() {
		var v entry
		if err = rows.Scan(&v.scope, &v.generation, &v.family, &v.audience, &v.membership, &v.binding); err != nil {
			return 0, err
		}
		entries = append(entries, v)
	}
	if err = rows.Err(); err != nil {
		return 0, err
	}
	rows.Close()
	for _, v := range entries {
		j.Cursor = inboxVerificationCursor{Scope: v.scope, Generation: v.generation, Family: v.family, Audience: v.audience}
		if v.family != "inbox" {
			continue
		}
		var generation, membership int64
		err = tx.QueryRowContext(ctx, `SELECT generation,membership_generation FROM scope_inbox_verification_scopes WHERE job_id=? AND scope_id=?`, id, v.scope).Scan(&generation, &membership)
		if errors.Is(err, sql.ErrNoRows) {
			return len(entries), ErrInboxVerificationMismatch
		}
		if err != nil {
			return len(entries), err
		}
		if generation != v.generation {
			continue
		}
		if membership != v.membership || v.binding < 1 || !feedText(v.audience, 512) {
			return len(entries), ErrInboxVerificationMismatch
		}
		var n int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM scope_inbox_verification_streams WHERE job_id=? AND scope_id=?`, id, v.scope).Scan(&n); err != nil {
			return len(entries), err
		}
		if n >= scopes.MaxStreamsPerScope {
			return len(entries), scopes.ErrBudget
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO scope_inbox_verification_streams VALUES(?,?,?,?,?)`, id, v.scope, v.generation, v.audience, v.binding); err != nil {
			return len(entries), err
		}
		for _, bucket := range []string{"total", "ask", "review", "escalate"} {
			if _, err = tx.ExecContext(ctx, `INSERT INTO scope_inbox_verification_counts(job_id,scope_id,audience_key,bucket) VALUES(?,?,?,?)`, id, v.scope, v.audience, bucket); err != nil {
				return len(entries), err
			}
		}
	}
	if len(entries) < 64 {
		inboxVerificationNext(j, "canonical")
	}
	return len(entries), nil
}

func inboxVerifyCanonical(ctx context.Context, tx *sql.Tx, id string, j *inboxVerificationJob) (int, error) {
	q := `SELECT id,thread_id,category,trigger_at,due_at,has_due_at,source_event_id,source_card_id,generated_at,CASE WHEN length(CAST(data_json AS BLOB))<=16384 THEN data_json END,source_hash FROM main.derived_inbox_items`
	var args []any
	if j.Cursor.Generation != 0 {
		q += ` WHERE id>?`
		args = append(args, j.Cursor.ID)
	}
	q += ` ORDER BY id LIMIT 64`
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var items []primitives.DerivedInboxItem
	for rows.Next() {
		var item primitives.DerivedInboxItem
		var due, event, card, hash sql.NullString
		var hasDue int
		var raw sql.NullString
		if err = rows.Scan(&item.ID, &item.ThreadID, &item.Category, &item.TriggerAt, &due, &hasDue, &event, &card, &item.GeneratedAt, &raw, &hash); err != nil {
			return 0, err
		}
		if !raw.Valid || !json.Valid([]byte(raw.String)) || hasDue < 0 || hasDue > 1 {
			return len(items) + 1, ErrInboxVerificationMismatch
		}
		item.DueAt = due.String
		item.SourceEventID = event.String
		item.SourceCardID = card.String
		item.SourceHash = hash.String
		item.HasDueAt = hasDue == 1
		d := json.NewDecoder(bytes.NewBufferString(raw.String))
		d.UseNumber()
		if d.Decode(&item.Data) != nil {
			return len(items) + 1, ErrInboxVerificationMismatch
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return 0, err
	}
	rows.Close()
	legacyCtx := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: j.Principal, PMActorID: j.PM})
	legacySQL, legacyArgs, err := primitives.ScopeInboxLegacyReadSQL(legacyCtx)
	if err != nil {
		return len(items), err
	}
	// Compare one internally enumerated batch against the real legacy relation.
	// The expensive denial graph is evaluated only off-request and never replaced
	// by a callback, a shared denial cache, or a caller-authored eligible-ID list.
	eligible := make(map[string]bool, len(items))
	if len(items) > 0 {
		args := append([]any(nil), legacyArgs...)
		for _, item := range items {
			args = append(args, item.ID)
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(items)), ",")
		visible, err := tx.QueryContext(ctx, `SELECT legacy.id FROM (`+legacySQL+`) legacy WHERE legacy.id IN (`+placeholders+`)`, args...)
		if err != nil {
			return len(items), err
		}
		for visible.Next() {
			var canonicalID string
			if err = visible.Scan(&canonicalID); err != nil {
				visible.Close()
				return len(items), err
			}
			if eligible[canonicalID] {
				visible.Close()
				return len(items), ErrInboxVerificationMismatch
			}
			eligible[canonicalID] = true
		}
		err = visible.Err()
		visible.Close()
		if err != nil {
			return len(items), err
		}
	}
	for _, item := range items {
		j.Cursor.ID = item.ID
		j.Cursor.Generation = 1
		if !eligible[item.ID] {
			continue
		}
		if err = inboxVerifyCanonicalItem(ctx, tx, id, item); err != nil {
			return len(items), err
		}
		j.Eligible++
	}
	j.Canonical += int64(len(items))
	if len(items) < 64 {
		inboxVerificationNext(j, "ordered")
	}
	return len(items), nil
}

func inboxVerifyCanonicalItem(ctx context.Context, tx *sql.Tx, jobID string, item primitives.DerivedInboxItem) error {
	var identity scopes.ResourceIdentity
	err := tx.QueryRowContext(ctx, `SELECT r.scope_id,r.id,r.version,k.rid FROM scope_resources r JOIN scope_resource_rids k ON k.scope_id=r.scope_id AND k.kind=r.kind AND k.resource_id=r.id WHERE r.kind='inbox' AND r.canonical_id=?`, item.ID).Scan(&identity.ScopeID, &identity.ResourceID, &identity.CanonicalVersion, &identity.RID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrInboxVerificationMismatch
	}
	if err != nil {
		return err
	}
	identity.Kind = "inbox"
	identity.CanonicalID = item.ID
	expected, err := primitives.EncodeScopeInbox(identity, item)
	if err != nil {
		return ErrInboxVerificationMismatch
	}
	// The canonical store rehydrates mirrored/default fields before projection.
	// Reuse its pure codec to apply that shape without duplicating store logic.
	normalized, err := primitives.DecodeScopeInbox(identity, expected)
	if err != nil {
		return ErrInboxVerificationMismatch
	}
	expected, err = primitives.EncodeScopeInbox(identity, normalized)
	if err != nil {
		return ErrInboxVerificationMismatch
	}
	order, err := primitives.ScopeInboxSortKey(item)
	if err != nil {
		return ErrInboxVerificationMismatch
	}
	rows, err := tx.QueryContext(ctx, `SELECT s.audience_key,CASE WHEN typeof(o.order_key)='blob' AND length(o.order_key)<=770 THEN o.order_key END,o.version,CASE WHEN typeof(p.data)='text' AND length(CAST(p.data AS BLOB))<=16384 THEN p.data END FROM scope_inbox_verification_streams s JOIN scope_inbox_order o ON o.scope_id=s.scope_id AND o.generation=s.generation AND o.family='inbox' AND o.audience_key=s.audience_key AND o.rid=? LEFT JOIN scope_feed_payloads p ON p.scope_id=o.scope_id AND p.generation=o.generation AND p.family=o.family AND p.audience_key=o.audience_key AND p.rid=o.rid AND p.version=o.version WHERE s.job_id=? AND s.scope_id=? LIMIT 2`, identity.RID, jobID, identity.ScopeID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var audience string
	var key []byte
	var version int64
	var data sql.NullString
	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return err
		}
		return ErrInboxVerificationMismatch
	}
	if err = rows.Scan(&audience, &key, &version, &data); err != nil {
		return err
	}
	if rows.Next() {
		return ErrInboxVerificationMismatch
	}
	if err = rows.Err(); err != nil {
		return err
	}
	rows.Close()
	// Decode + re-encode with the same pure codec makes JSON ordering immaterial
	// while refusing unknown envelope fields, stale identity and lossy numbers.
	actual, err := primitives.DecodeScopeInbox(identity, json.RawMessage(data.String))
	if err != nil {
		return ErrInboxVerificationMismatch
	}
	actualBytes, err := primitives.EncodeScopeInbox(identity, actual)
	if err != nil || !data.Valid || version != identity.CanonicalVersion || !bytes.Equal(order, key) || !bytes.Equal(expected, actualBytes) {
		return ErrInboxVerificationMismatch
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO scope_inbox_verification_seen VALUES(?,?,?,?,?)`, jobID, identity.RID, identity.ScopeID, audience, version); err != nil {
		return fmt.Errorf("%w: repeated RID", ErrInboxVerificationMismatch)
	}
	buckets := []string{"total"}
	switch strings.TrimSpace(item.Category) {
	case "ask", "review", "escalate":
		buckets = append(buckets, strings.TrimSpace(item.Category))
	}
	for _, bucket := range buckets {
		r, e := tx.ExecContext(ctx, `UPDATE scope_inbox_verification_counts SET expected=expected+1 WHERE job_id=? AND scope_id=? AND audience_key=? AND bucket=?`, jobID, identity.ScopeID, audience, bucket)
		if e != nil {
			return e
		}
		n, e := r.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return ErrInboxVerificationMismatch
		}
	}
	return nil
}

func inboxVerificationStream(ctx context.Context, tx *sql.Tx, id string, j *inboxVerificationJob) (int64, bool, error) {
	var generation int64
	if j.Cursor.Scope != "" {
		err := tx.QueryRowContext(ctx, `SELECT generation FROM scope_inbox_verification_streams WHERE job_id=? AND scope_id=? AND audience_key=?`, id, j.Cursor.Scope, j.Cursor.Audience).Scan(&generation)
		return generation, true, err
	}
	err := tx.QueryRowContext(ctx, `SELECT scope_id,audience_key,generation FROM scope_inbox_verification_streams WHERE job_id=? ORDER BY scope_id,audience_key LIMIT 1`, id).Scan(&j.Cursor.Scope, &j.Cursor.Audience, &generation)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return generation, err == nil, err
}
func inboxVerificationAdvanceStream(ctx context.Context, tx *sql.Tx, id string, j *inboxVerificationJob, next string) error {
	var scope, audience string
	err := tx.QueryRowContext(ctx, `SELECT scope_id,audience_key FROM scope_inbox_verification_streams WHERE job_id=? AND (scope_id,audience_key)>(?,?) ORDER BY scope_id,audience_key LIMIT 1`, id, j.Cursor.Scope, j.Cursor.Audience).Scan(&scope, &audience)
	if errors.Is(err, sql.ErrNoRows) {
		inboxVerificationNext(j, next)
		return nil
	}
	if err != nil {
		return err
	}
	j.Cursor = inboxVerificationCursor{Scope: scope, Audience: audience}
	return nil
}
func inboxVerifyReverse(ctx context.Context, tx *sql.Tx, id string, j *inboxVerificationJob) (int, error) {
	generation, ok, err := inboxVerificationStream(ctx, tx, id, j)
	if err != nil {
		return 0, err
	}
	next := "payloads"
	if j.Phase == "payloads" {
		next = "counters"
	}
	if !ok {
		inboxVerificationNext(j, next)
		return 0, nil
	}
	q := `SELECT rid,version,typeof(rid),typeof(version),typeof(order_key) FROM scope_inbox_order WHERE scope_id=? AND generation=? AND family='inbox' AND audience_key=?`
	args := []any{j.Cursor.Scope, generation, j.Cursor.Audience}
	if j.Phase == "payloads" {
		q = `SELECT rid,version,typeof(rid),typeof(version),typeof(data) FROM scope_feed_payloads WHERE scope_id=? AND generation=? AND family='inbox' AND audience_key=?`
		if j.Cursor.Generation != 0 {
			q += ` AND (rid,version)>(?,?)`
			args = append(args, j.Cursor.RID, j.Cursor.Version)
		}
		q += ` ORDER BY rid,version LIMIT 64`
	} else {
		if j.Cursor.Generation != 0 {
			q += ` AND rid>?`
			args = append(args, j.Cursor.RID)
		}
		q += ` ORDER BY rid LIMIT 64`
	}
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type entry struct {
		rid, version                    int64
		ridType, versionType, valueType string
	}
	var entries []entry
	for rows.Next() {
		var v entry
		if err = rows.Scan(&v.rid, &v.version, &v.ridType, &v.versionType, &v.valueType); err != nil {
			return 0, err
		}
		entries = append(entries, v)
	}
	if err = rows.Err(); err != nil {
		return 0, err
	}
	rows.Close()
	for _, v := range entries {
		valueType := "blob"
		if j.Phase == "payloads" {
			valueType = "text"
		}
		if v.ridType != "integer" || v.versionType != "integer" || v.valueType != valueType || v.rid < 1 || v.version < 1 {
			return len(entries), ErrInboxVerificationMismatch
		}
		var found int
		err = tx.QueryRowContext(ctx, `SELECT count(*) FROM scope_inbox_verification_seen WHERE job_id=? AND rid=? AND scope_id=? AND audience_key=? AND version=?`, id, v.rid, j.Cursor.Scope, j.Cursor.Audience, v.version).Scan(&found)
		if err != nil {
			return len(entries), err
		}
		if found != 1 {
			return len(entries), ErrInboxVerificationMismatch
		}
		j.Cursor.RID = v.rid
		j.Cursor.Version = v.version
		j.Cursor.Generation = 1
	}
	if len(entries) < 64 {
		err = inboxVerificationAdvanceStream(ctx, tx, id, j, next)
	}
	return len(entries), err
}
func inboxVerifyCounters(ctx context.Context, tx *sql.Tx, id string, j *inboxVerificationJob) (int, error) {
	generation, ok, err := inboxVerificationStream(ctx, tx, id, j)
	if err != nil {
		return 0, err
	}
	if !ok {
		inboxVerificationNext(j, "missing_counters")
		return 0, nil
	}
	q := `SELECT bucket,value,typeof(value),typeof(bucket) FROM scope_counters WHERE scope_id=? AND generation=? AND family='inbox' AND audience_key=?`
	args := []any{j.Cursor.Scope, generation, j.Cursor.Audience}
	if j.Cursor.Generation != 0 {
		q += ` AND bucket>?`
		args = append(args, j.Cursor.Bucket)
	}
	q += ` ORDER BY bucket LIMIT 64`
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type entry struct {
		bucket, typ, bucketType string
		value                   int64
	}
	var entries []entry
	for rows.Next() {
		var v entry
		if err = rows.Scan(&v.bucket, &v.value, &v.typ, &v.bucketType); err != nil {
			return 0, err
		}
		entries = append(entries, v)
	}
	if err = rows.Err(); err != nil {
		return 0, err
	}
	rows.Close()
	for _, v := range entries {
		if v.bucketType != "text" {
			return len(entries), ErrInboxVerificationMismatch
		}
		var expected int64
		err = tx.QueryRowContext(ctx, `SELECT expected FROM scope_inbox_verification_counts WHERE job_id=? AND scope_id=? AND audience_key=? AND bucket=?`, id, j.Cursor.Scope, j.Cursor.Audience, v.bucket).Scan(&expected)
		if errors.Is(err, sql.ErrNoRows) {
			return len(entries), ErrInboxVerificationMismatch
		}
		if err != nil {
			return len(entries), err
		}
		if expected != v.value || v.typ != "integer" || v.value < 0 {
			return len(entries), ErrInboxVerificationMismatch
		}
		if _, err = tx.ExecContext(ctx, `UPDATE scope_inbox_verification_counts SET matched=1 WHERE job_id=? AND scope_id=? AND audience_key=? AND bucket=?`, id, j.Cursor.Scope, j.Cursor.Audience, v.bucket); err != nil {
			return len(entries), err
		}
		j.Cursor.Bucket = v.bucket
		j.Cursor.Generation = 1
	}
	if len(entries) < 64 {
		err = inboxVerificationAdvanceStream(ctx, tx, id, j, "missing_counters")
	}
	return len(entries), err
}
func inboxVerifyMissingCounters(ctx context.Context, tx *sql.Tx, id string, j *inboxVerificationJob) (int, error) {
	rows, err := tx.QueryContext(ctx, `SELECT scope_id,audience_key,bucket,expected,matched FROM scope_inbox_verification_counts WHERE job_id=? AND (scope_id,audience_key,bucket)>(?,?,?) ORDER BY scope_id,audience_key,bucket LIMIT 64`, id, j.Cursor.Scope, j.Cursor.Audience, j.Cursor.Bucket)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var scope, audience, bucket string
		var expected, matched int64
		if err = rows.Scan(&scope, &audience, &bucket, &expected, &matched); err != nil {
			return n, err
		}
		n++
		if expected > 0 && matched != 1 {
			return n, ErrInboxVerificationMismatch
		}
		j.Cursor = inboxVerificationCursor{Scope: scope, Audience: audience, Bucket: bucket}
	}
	if err = rows.Err(); err != nil {
		return n, err
	}
	if n < 64 {
		inboxVerificationNext(j, "done")
	}
	return n, nil
}
