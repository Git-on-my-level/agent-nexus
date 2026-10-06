package readmodel

import (
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/scopes"
	"context"
	"database/sql"
	"errors"
	"testing"
)

const hookIdentityProposal = `SELECT d.state,d.generation,r.version,r.canonical_id,k.rid
 FROM scope_domains d JOIN scope_resources r ON r.scope_id=d.id
 JOIN scope_resource_rids k ON k.scope_id=r.scope_id AND k.kind=r.kind AND k.resource_id=r.id
 WHERE d.id=? AND r.kind=? AND r.id=?`

type hookExecutor struct{ tx scopedrepo.MutationTx }

func (e hookExecutor) Exec(ctx context.Context, q string, args ...any) (int64, error) {
	r, err := e.tx.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}

// Candidate for A: identity comes from the private registry in the SAME source
// transaction. This check is necessary, but complete canonical before/after
// provenance and capture coverage must still be supplied by A's actual writers.
type feedHookProposal struct{ capture Capture }

func (h feedHookProposal) ApplyCanonical(ctx context.Context, tx scopedrepo.MutationTx, m scopes.CanonicalMutation) error {
	if err := m.Validate(); err != nil {
		return err
	}
	i := m.Identity
	rows, err := tx.QueryContext(ctx, hookIdentityProposal, i.ScopeID, i.Kind, i.ResourceID)
	if err != nil {
		return err
	}
	var state, canonical string
	var generation, version, rid int64
	if !rows.Next() {
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		return ErrProjection
	}
	if err = rows.Scan(&state, &generation, &version, &canonical, &rid); err != nil {
		rows.Close()
		return err
	}
	extra := rows.Next()
	err = rows.Err()
	closeErr := rows.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if extra || state != "active" || generation < 1 || version != i.CanonicalVersion || canonical != i.CanonicalID || rid != i.RID {
		return ErrProjection
	}
	old, next, payloads, err := CaptureCanonical(m, generation, h.capture)
	if err != nil {
		return err
	}
	// Never discard semantic affected-row errors: returning them is what makes
	// ApplyCanonicalHooks roll back the source, including earlier adapters.
	return ApplyProjection(ctx, hookExecutor{tx: tx}, old, next, payloads, false)
}

var _ scopedrepo.CanonicalHook = feedHookProposal{}

func TestTrustedHookIdentityAndFenceRejectWholeSource(t *testing.T) {
	for _, mode := range []string{"success", "wrong-rid", "wrong-canonical", "stale-registry", "transitioning"} {
		t.Run(mode, func(t *testing.T) {
			db, _, request, _ := adapterFixture(t, 1, 1)
			ctx := context.Background()
			id := "opaque-0-0-0"
			if _, err := db.Exec(`CREATE TABLE test_source(state TEXT);INSERT INTO test_source VALUES('old')`); err != nil {
				t.Fatal(err)
			}
			if mode == "transitioning" {
				if _, err := db.Exec(`UPDATE scope_domains SET state='transitioning'`); err != nil {
					t.Fatal(err)
				}
			}
			source, err := resourceaccess.NewDB(db).BeginTx(ctx, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer source.Rollback()
			if _, err = source.ExecContext(ctx, `UPDATE test_source SET state='new'`); err != nil {
				t.Fatal(err)
			}
			if mode != "stale-registry" {
				if _, err = source.ExecContext(ctx, `UPDATE scope_resources SET version=2 WHERE id=?`, id); err != nil {
					t.Fatal(err)
				}
			}
			m := scopes.CanonicalMutation{Identity: scopes.ResourceIdentity{ScopeID: request.ScopeIDs[0], Kind: "card", ResourceID: id, CanonicalID: id, RID: 1, CanonicalVersion: 2}, PreviousVersion: 1, Changes: []scopes.Change{{ScopeID: request.ScopeIDs[0], Kind: "card", ResourceID: id, CanonicalVersion: 2, Family: "inbox", Audience: "reader", After: &scopes.Projection{Status: "open"}}}}
			if mode == "wrong-rid" {
				m.Identity.RID = 999
			}
			if mode == "wrong-canonical" {
				m.Identity.CanonicalID = "wrong"
			}
			err = scopedrepo.ApplyCanonicalHooks(ctx, source, m, feedHookProposal{capture: testCapture})
			if mode == "success" {
				if err != nil {
					t.Fatal(err)
				}
				if err = source.Commit(); err != nil {
					t.Fatal(err)
				}
			} else {
				if !errors.Is(err, ErrProjection) {
					t.Fatal(err)
				}
				if !errors.Is(source.Commit(), sql.ErrTxDone) {
					t.Fatal("source committable")
				}
			}
			var state string
			if err = db.QueryRow(`SELECT state FROM test_source`).Scan(&state); err != nil {
				t.Fatal(err)
			}
			want := "old"
			if mode == "success" {
				want = "new"
			}
			if state != want {
				t.Fatal(state)
			}
		})
	}
}
