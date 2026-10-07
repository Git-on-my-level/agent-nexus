package scopemigrate

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"agent-nexus-core/internal/scopes"
)

var ErrEpochCoverage = errors.New("scope migration epoch coverage unavailable")

// MetadataSource consumes A's recorded registry and immutable integer RID
// mapping. It does not claim that the registry covers all historical canonical
// rows: A must finish its bounded legacy registration census before cutover.
// No authority classifier has proved audience subsets yet. Every record is
// therefore a counted permanent no-grants exception, never an owner-ID guess.
// This adapter has no content, blob, graph or retry dependency.
type MetadataSource struct{ AuthorityToken string }

var _ Source = MetadataSource{}

func (s MetadataSource) Epoch(ctx context.Context, tx *sql.Tx) (int64, error) {
	if s.AuthorityToken == "" || len(s.AuthorityToken) > 512 {
		return 0, ErrEpochCoverage
	}
	var version int64
	var covered bool
	err := tx.QueryRowContext(ctx, `SELECT version,schema_cookie=(SELECT schema_version FROM pragma_schema_version) AND authority_token=? FROM scope_migration_epoch WHERE singleton=1`, s.AuthorityToken).Scan(&version, &covered)
	if err != nil {
		return 0, err
	}
	if !covered || version < 0 {
		return 0, ErrEpochCoverage
	}
	return version, nil
}

// RID is the SQL keyset. Fixed-width encoding preserves that same strict order
// in Runner's durable TEXT cursor. Registry and capture probes are unique keys.
const metadataPageSQL = `WITH candidates AS MATERIALIZED (SELECT rid,scope_id,kind,resource_id FROM scope_resource_rids WHERE rid>? ORDER BY rid LIMIT ?)
 SELECT k.rid,CASE WHEN length(CAST(r.kind AS BLOB))<=64 THEN r.kind END,
 CASE WHEN length(CAST(r.id AS BLOB))<=512 THEN r.id END,r.version,
 c.rid IS NULL OR (c.scope_id=r.scope_id AND c.kind=r.kind AND c.resource_id=r.id AND c.canonical_id=r.canonical_id AND c.version=r.version)
 FROM candidates k
 LEFT JOIN scope_resources r ON r.scope_id=k.scope_id AND r.kind=k.kind AND r.id=k.resource_id
 LEFT JOIN scope_migration_captures c ON c.rid=k.rid
 ORDER BY k.rid`

func (MetadataSource) Page(ctx context.Context, tx *sql.Tx, after string, limit, maxBytes int) ([]Record, bool, error) {
	if limit < 1 || limit > MaxChunk || maxBytes < 1 || maxBytes > 4<<20 {
		return nil, false, ErrBudget
	}
	var cursor int64
	if after != "" {
		var err error
		cursor, err = strconv.ParseInt(after, 10, 64)
		if err != nil || cursor < 1 || len(after) != 19 || ridKey(cursor) != after {
			return nil, false, ErrBudget
		}
	}
	rows, err := tx.QueryContext(ctx, metadataPageSQL, cursor, limit)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	batch := make([]Record, 0, limit)
	bytes := 0
	for rows.Next() {
		var rid int64
		var valid bool
		var r Record
		if err := rows.Scan(&rid, &r.Kind, &r.ID, &r.Version, &valid); err != nil {
			return nil, false, err
		}
		if !valid || rid <= cursor || r.Version < 1 {
			return nil, false, ErrSourceChanged
		}
		r.Key = ridKey(rid)
		r.Uncertain = true
		// Neither registry membership nor a capture proves historical manifest
		// or blob availability. Missing evidence remains unavailable permanently.
		r.ContentUnavailable = true
		bytes += recordBytes(r)
		if bytes > maxBytes {
			if len(batch) == 0 {
				return nil, false, ErrBudget
			}
			// The unconsumed row remains after the last committed key. A byte
			// page is never mistaken for complete coverage.
			return batch, false, nil
		}
		batch = append(batch, r)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	return batch, len(batch) < limit, nil
}

func ridKey(rid int64) string {
	s := strconv.FormatInt(rid, 10)
	return "0000000000000000000"[:19-len(s)] + s
}

// CaptureCanonical is an unwired trusted canonical adapter. It uses the supplied
// canonical transaction, and verifies the opaque/RID/canonical tuple against
// A's registry rather than treating descriptor validation as provenance.
// No payload or asserted audience proof is retained. A must call this after
// advancing the registry version in every canonical writer/import transaction.
// CaptureTx is structurally satisfied by scopedrepo.MutationTx. A supplies the
// exact CanonicalHook shim; keeping it there preserves the existing factory
// import gate instead of granting migration code a scopedrepo Store factory.
type CaptureTx interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

const captureSQL = `INSERT INTO scope_migration_captures(rid,scope_id,kind,resource_id,canonical_id,version)
 SELECT k.rid,r.scope_id,r.kind,r.id,r.canonical_id,r.version
 FROM scope_resource_rids k JOIN scope_resources r ON r.scope_id=k.scope_id AND r.kind=k.kind AND r.id=k.resource_id
 WHERE k.rid=? AND r.scope_id=? AND r.kind=? AND r.id=? AND r.canonical_id=? AND r.version=?
 ON CONFLICT(rid) DO UPDATE SET version=excluded.version
 WHERE scope_migration_captures.scope_id=excluded.scope_id AND scope_migration_captures.kind=excluded.kind
 AND scope_migration_captures.resource_id=excluded.resource_id AND scope_migration_captures.canonical_id=excluded.canonical_id
 AND scope_migration_captures.version=?`

func CaptureCanonical(ctx context.Context, tx CaptureTx, m scopes.CanonicalMutation) error {
	if err := m.Validate(); err != nil {
		return err
	}
	i := m.Identity
	result, err := tx.ExecContext(ctx, captureSQL, i.RID, i.ScopeID, i.Kind, i.ResourceID, i.CanonicalID, i.CanonicalVersion, m.PreviousVersion)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrSourceChanged
	}
	return nil
}

// VerifySink rejects an active/transitioning scope, implicit PM admission and
// explicit grants before any production placement can be staged.
func (MetadataSource) VerifySink(ctx context.Context, tx *sql.Tx, sink string) error {
	var valid bool
	err := tx.QueryRowContext(ctx, `SELECT state='inaccessible' AND NOT EXISTS(SELECT 1 FROM scope_memberships WHERE scope_id=?) FROM scope_domains WHERE id=?`, sink, sink).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return scopes.ErrDenied
	}
	return nil
}
