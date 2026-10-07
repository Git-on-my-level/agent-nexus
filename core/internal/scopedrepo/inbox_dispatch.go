package scopedrepo

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/readmodel"
	"agent-nexus-core/internal/scopes"
)

//go:embed inbox_dispatch_schema.sql
var inboxDispatchSchemaSQL string

// These values are implementation versions. No request can select a version or
// claim canonical/discovery/enrichment/derivation completeness.
const inboxServingVersion = 1

// InboxFallback is an INTERNAL dispatch outcome, never a public diagnosis of
// hidden activity. A fallback applies to the whole legacy response: mixed scope
// merges and silently truncated counts have not been approved.
type InboxFallback string

const (
	InboxNoFallback       InboxFallback = ""
	InboxNoDirectory      InboxFallback = "no_directory"
	InboxDirectoryBudget  InboxFallback = "directory_budget"
	InboxProofUnavailable InboxFallback = "proof_unavailable"
)

type InboxDispatchResult struct {
	Page     readmodel.Page   `json:"-"`
	Counts   map[string]int64 `json:"-"`
	Fallback InboxFallback    `json:"-"`
}

// InboxDispatcher is a closed, render-only inbox computation. It accepts no
// caller scopes, audiences, SQL, projection callbacks or capability factories.
// Canonical mutation is unreachable through it. It remains disabled: import
// guards forbid every live consumer, and NO production writer mints its proof.
type InboxDispatcher struct {
	store            *Store
	codec            *readmodel.CursorCodec
	requests         atomic.Uint64
	fallbackRequests atomic.Uint64
	scopeAttempts    atomic.Uint64
	fallbackScopes   atomic.Uint64
	noDirectory      atomic.Uint64
	directoryBudget  atomic.Uint64
	proofUnavailable atomic.Uint64
	elapsedNanos     atomic.Uint64
}

type InboxDispatchDiagnostics struct {
	Requests, FallbackRequests, ScopeAttempts, FallbackScopes uint64
	NoDirectory, DirectoryBudget, ProofUnavailable            uint64
	ElapsedNanos                                              uint64
}

func NewInboxDispatcher(store *Store, codec *readmodel.CursorCodec) *InboxDispatcher {
	return &InboxDispatcher{store: store, codec: codec}
}

// Diagnostics is trusted maintenance data. It has no principal/scope labels and
// is not mounted at any HTTP endpoint. Latency includes admission and errors;
// the eventual handler must also measure the complete legacy fallback request.
func (d *InboxDispatcher) Diagnostics() InboxDispatchDiagnostics {
	return InboxDispatchDiagnostics{d.requests.Load(), d.fallbackRequests.Load(), d.scopeAttempts.Load(), d.fallbackScopes.Load(), d.noDirectory.Load(), d.directoryBudget.Load(), d.proofUnavailable.Load(), d.elapsedNanos.Load()}
}

func (s *Store) InitializeInboxDispatcherSchema(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, inboxDispatchSchemaSQL); err != nil {
		return err
	}
	return tx.Commit()
}

// Read discovers the principal's complete directory and validates serving proof
// in the SAME snapshot as candidates, exact identity hydration and counts. More
// than 64 scopes/256 streams falls back, preserving the complete legacy contract.
// Only missing/stale proof or directory coverage permits availability fallback;
// revocation, corruption and database failures return errors without any data.
func (d *InboxDispatcher) Read(ctx context.Context, principal string, size int, cursor string) (out InboxDispatchResult, err error) {
	if d == nil || d.store == nil || d.codec == nil || !feedText(principal, 512) || size < 1 || size > scopes.MaxPage {
		return out, scopes.ErrBudget
	}
	started := time.Now()
	d.requests.Add(1)
	selected := 0
	defer func() {
		d.elapsedNanos.Add(uint64(time.Since(started)))
		d.scopeAttempts.Add(uint64(selected))
		if out.Fallback != InboxNoFallback {
			d.fallbackRequests.Add(1)
			d.fallbackScopes.Add(uint64(selected))
			switch out.Fallback {
			case InboxNoDirectory:
				d.noDirectory.Add(1)
			case InboxDirectoryBudget:
				d.directoryBudget.Add(1)
			case InboxProofUnavailable:
				d.proofUnavailable.Add(1)
			}
		}
	}()
	tx, err := d.store.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	ids, err := inboxDirectory(ctx, tx, principal)
	if err != nil {
		return out, fmt.Errorf("inbox directory: %w", err)
	}
	selected = len(ids)
	if len(ids) == 0 {
		out.Fallback = InboxNoDirectory
		return out, nil
	}
	if len(ids) > scopes.MaxScopes {
		out.Fallback = InboxDirectoryBudget
		return out, nil
	}
	streams, err := inboxDirectoryStreams(ctx, tx, principal, ids)
	if err != nil {
		return out, fmt.Errorf("inbox directory streams: %w", err)
	}
	if err = scopes.ValidateStreams(streams); err != nil {
		if errors.Is(err, scopes.ErrBudget) {
			out.Fallback = InboxDirectoryBudget
			return out, nil
		}
		return out, err
	}
	request := scopes.RequestSelection{Principal: principal, ScopeIDs: ids}
	err = d.store.readOrderedBatchFeedTx(ctx, tx, request, streams, func(r OrderedBatchFeedReader) error {
		snapshot, e := r.Snapshot()
		if e != nil {
			return e
		}
		if e = inboxServingAdmission(ctx, tx, snapshot.Binding); e != nil {
			return e
		}
		adapter := AdaptOrderedReadModel(r, scopes.DirectoryPage{}, "")
		out.Page, e = readmodel.ReadOrdered(ctx, adapter, d.codec, size, cursor)
		if e != nil {
			return e
		}
		out.Counts, e = adapter.Buckets(ctx, []string{"total", "escalate", "ask", "review"})
		return e
	})
	if errors.Is(err, scopes.ErrUpdating) {
		// Discard everything even if admission stopped a later operation.
		return InboxDispatchResult{Fallback: InboxProofUnavailable}, nil
	}
	if err != nil {
		return InboxDispatchResult{}, err
	}
	if err = tx.Commit(); err != nil {
		return InboxDispatchResult{}, err
	}
	return out, nil
}

func inboxDirectory(ctx context.Context, tx *sql.Tx, principal string) ([]scopes.ID, error) {
	rows, err := tx.QueryContext(ctx, `SELECT scope_id FROM scope_memberships WHERE principal=? ORDER BY scope_id LIMIT ?`, principal, scopes.MaxScopes+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []scopes.ID
	for rows.Next() {
		var id scopes.ID
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		if !feedText(string(id), 512) {
			return nil, ErrFeedProjection
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Each branch stops after five exact current-generation inbox bindings. Thus
// corruption cannot cause an unbounded sort/filter over old or other families.
func inboxDirectoryStreams(ctx context.Context, tx *sql.Tx, principal string, ids []scopes.ID) ([]scopes.Stream, error) {
	parts := make([]string, len(ids))
	args := make([]any, 0, 2*len(ids))
	for i, id := range ids {
		parts[i] = `SELECT scope_id,family,audience_key FROM (SELECT b.scope_id,b.family,b.audience_key FROM scope_feed_bindings b WHERE b.principal=? AND b.scope_id=? AND b.generation=(SELECT generation FROM scope_domains WHERE id=b.scope_id) AND b.family='inbox' ORDER BY b.audience_key LIMIT 5)`
		args = append(args, principal, id)
	}
	rows, err := tx.QueryContext(ctx, strings.Join(parts, " UNION ALL "), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var streams []scopes.Stream
	for rows.Next() {
		var stream scopes.Stream
		if err = rows.Scan(&stream.Scope, &stream.Family, &stream.Audience); err != nil {
			return nil, err
		}
		streams = append(streams, stream)
	}
	return streams, rows.Err()
}

func inboxServingAdmission(ctx context.Context, tx *sql.Tx, binding string) error {
	hash := primitives.ScopeInboxInvalidationRegistryHash()
	var valid bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM scope_inbox_serving_receipts p JOIN scope_inbox_source_clock c ON c.singleton=1
 WHERE p.snapshot_binding=? AND p.format_version=? AND p.policy_version=? AND p.projector_version=?
 AND p.discovery_version=? AND p.enrichment_version=? AND p.derivation_version=?
 AND c.installation_complete=1 AND c.registry_hash=? AND p.registry_hash=c.registry_hash
 AND c.installed_schema_version=(SELECT schema_version FROM pragma_schema_version)
 AND p.source_revision=c.source_revision AND p.authority_revision=c.authority_revision AND p.directory_revision=c.directory_revision
 AND c.directory_coverage_revision=c.directory_revision)`, binding, inboxServingVersion, feedPolicyVersion, feedProjectorVersion, inboxServingVersion, inboxServingVersion, inboxServingVersion, hash).Scan(&valid)
	if err != nil {
		return fmt.Errorf("inbox admission: %w", err)
	}
	if !valid {
		return scopes.ErrUpdating
	}
	return nil
}
