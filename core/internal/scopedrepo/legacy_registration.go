package scopedrepo

import (
	"context"
	"database/sql"
	"errors"
	"unicode"
	"unicode/utf8"

	"agent-nexus-core/internal/scopes"
)

// LegacyRecord is bounded identity metadata supplied by a trusted migration
// worker after reading its canonical keyset. It contains no aliases or content.
// CanonicalID preserves arbitrary legacy bytes, including NUL and invalid UTF-8.
// IDs outside the 1..512 byte budget must remain unprocessed in the worker's
// census; a refusal must never be interpreted as complete migration coverage.
type LegacyRecord struct {
	Kind, CanonicalID string
	CanonicalVersion  int64
}

// RegisterLegacyBatch registers 1..64 canonical identities in a permanently
// inaccessible, no-grants sink. This disabled migration boundary accepts only a
// trusted worker's existing transaction, never a request-controlled transaction.
// The caller must commit registration and its checkpoint together. Any failure
// (including invalid input or panic) rolls back the entire supplied transaction,
// so ignoring the error cannot commit a partial checkpoint. Success leaves it open.
//
// Exact replays preserve both the opaque ID and RID. Conflicting scope/version
// assignments fail closed: there is no audience certificate or reclassification
// mechanism here. No canonical content, global alias, or replay handle is read.
func RegisterLegacyBatch(ctx context.Context, tx *sql.Tx, sink scopes.ID, records []LegacyRecord) ([]scopes.ResourceIdentity, error) {
	if tx == nil {
		return nil, scopes.ErrBudget
	}
	success := false
	defer func() {
		if !success {
			_ = tx.Rollback()
		}
	}()
	if ctx == nil || !legacyText(string(sink), 256) || len(records) < 1 || len(records) > 64 {
		return nil, scopes.ErrBudget
	}
	// Validate the entire bounded input before any database work. Duplicate exact
	// records are harmless replay; a conflicting duplicate rolls back the batch.
	for _, r := range records {
		if !legacyText(r.Kind, 32) || len(r.CanonicalID) == 0 || len(r.CanonicalID) > 512 || r.CanonicalVersion < 1 {
			return nil, scopes.ErrBudget
		}
	}
	var allowed int
	if err := tx.QueryRowContext(ctx, query_legacy_sink, sink).Scan(&allowed); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, scopes.ErrDenied
		}
		return nil, err
	}
	out := make([]scopes.ResourceIdentity, 0, len(records))
	for _, r := range records {
		identity, err := legacyIdentity(ctx, tx, r)
		if errors.Is(err, sql.ErrNoRows) {
			id, err := opaque()
			if err != nil {
				return nil, err
			}
			if _, err = tx.ExecContext(ctx, query_legacy_register, sink, r.Kind, id, r.CanonicalID, r.CanonicalVersion); err != nil {
				return nil, err
			}
			// The registry's insert trigger assigns the stable global RID. Read it
			// through the same exact keys, without relying on last_insert_rowid.
			identity, err = legacyIdentity(ctx, tx, r)
			if err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		}
		if identity.ScopeID != sink || identity.CanonicalVersion != r.CanonicalVersion || identity.RID < 1 || !legacyText(identity.ResourceID, 32) {
			return nil, scopes.ErrDenied
		}
		out = append(out, identity)
	}
	success = true
	return out, nil
}

func legacyIdentity(ctx context.Context, tx *sql.Tx, r LegacyRecord) (scopes.ResourceIdentity, error) {
	out := scopes.ResourceIdentity{Kind: r.Kind, CanonicalID: r.CanonicalID}
	err := tx.QueryRowContext(ctx, query_legacy_identity, r.Kind, r.CanonicalID).Scan(&out.ScopeID, &out.ResourceID, &out.CanonicalVersion, &out.RID)
	return out, err
}

func legacyText(s string, maxBytes int) bool {
	if len(s) == 0 || len(s) > maxBytes || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
