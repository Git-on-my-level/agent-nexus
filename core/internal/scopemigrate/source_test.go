package scopemigrate_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"agent-nexus-core/internal/pm"
	"agent-nexus-core/internal/resourceaccess"
	"agent-nexus-core/internal/scopedrepo"
	"agent-nexus-core/internal/scopemigrate"
	"agent-nexus-core/internal/scopes"
	"agent-nexus-core/internal/storage"
)

func metadataFixture(t *testing.T, n int) (*storage.Workspace, *scopemigrate.Runner) {
	t.Helper()
	w, r := fixture(t, 0)
	ctx := context.Background()
	must(t, scopedrepo.New(w.DB()).Initialize(ctx))
	_, err := pm.NewStore(w.DB())
	must(t, err)
	tx, err := w.DB().BeginTx(ctx, nil)
	must(t, err)
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO scope_domains VALUES('legacy','active',1),('no-grants','inaccessible',1)`)
	must(t, err)
	for i := 0; i < n; i++ {
		_, err = tx.Exec(`INSERT INTO artifacts(id,kind,created_at,created_by,content_type,content_hash) VALUES(?,'note','now','owner','text/plain','unavailable')`, fmt.Sprintf("canonical-%06d", i))
		must(t, err)
		_, err = tx.Exec(`INSERT INTO scope_resources VALUES('legacy','artifact',?,?,1)`, fmt.Sprintf("opaque-%06d", i), fmt.Sprintf("canonical-%06d", i))
		must(t, err)
	}
	must(t, storage.InstallScopeMigrationEpoch(ctx, tx))
	must(t, storage.BindScopeMigrationAuthority(ctx, tx, "synthetic-pm"))
	must(t, tx.Commit())
	r.Source = scopemigrate.MetadataSource{AuthorityToken: "synthetic-pm"}
	return w, r
}

func sourceEpoch(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	must(t, err)
	defer tx.Rollback()
	e, err := (scopemigrate.MetadataSource{AuthorityToken: "synthetic-pm"}).Epoch(context.Background(), tx)
	must(t, err)
	return e
}

func TestMetadataSourcePersonalAnd10x(t *testing.T) {
	if testing.Short() {
		t.Skip("real registry metadata scale fixture")
	}
	for _, n := range []int{806, 8060} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			w, r := metadataFixture(t, n)
			token := begin(t, r)
			ctx := context.Background()
			chunks := 0
			for {
				if chunks == 3 {
					// Resume the production adapter against the same durable registry,
					// epoch and success watermark after a compatible workspace reopen.
					root := w.Layout().RootDir
					must(t, w.Close())
					reopened, err := storage.InitializeWorkspace(ctx, root)
					must(t, err)
					w = reopened
					defer w.Close()
					r.DB = w.DB()
				}
				p, err := r.Step(ctx, token, MaxRecordsForTest)
				must(t, err)
				chunks++
				if p.Done {
					if p.Processed != int64(n) || p.Exceptions != int64(n) {
						t.Fatal("unproved audience must be counted and sealed", p)
					}
					break
				}
			}
			var bad int
			must(t, w.DB().QueryRow(`SELECT count(*) FROM scope_migration_placements WHERE scope_id!='no-grants' OR exception!=1 OR content_unavailable!=1 OR resource_id NOT LIKE 'opaque-%'`).Scan(&bad))
			if bad != 0 {
				t.Fatal("canonical IDs or audience guesses escaped", bad)
			}
		})
	}
}

const MaxRecordsForTest = 64

func TestMetadataPageByteCursorAndSink(t *testing.T) {
	w, r := metadataFixture(t, 8)
	ctx := context.Background()
	token := begin(t, r)
	before, err := r.Report(ctx)
	must(t, err)
	r.SealedScope = "legacy"
	_, err = r.Step(ctx, token, 64)
	if !errors.Is(err, scopes.ErrDenied) {
		t.Fatal("active domain admitted as permanent sink", err)
	}
	after, err := r.Report(ctx)
	must(t, err)
	if after != before {
		t.Fatal("invalid sink advanced checkpoint")
	}
	r.SealedScope = "no-grants"
	store := &scopemigrate.Store{Runner: r}
	result, err := store.Step(ctx, scopes.StepRequest{JobID: r.Job, ExpectedEpoch: before.SourceEpoch, LeaseToken: token, MaxRecords: 64, MaxBytes: 110, MaxDuration: 50 * time.Millisecond})
	must(t, err)
	if result.Visited != 1 || result.Complete || result.PermanentPrivateExceptions != 1 {
		t.Fatal("byte page claimed completeness", result)
	}
	p, err := r.Step(ctx, token, 64)
	must(t, err)
	if !p.Done || p.Processed != 8 {
		t.Fatal("byte page skipped unconsumed candidate", p)
	}
	tx, err := w.DB().BeginTx(ctx, nil)
	must(t, err)
	defer tx.Rollback()
	for _, cursor := range []string{"1", "-000000000000000001", "0000000000000000000", "9223372036854775808"} {
		_, _, err = (scopemigrate.MetadataSource{}).Page(ctx, tx, cursor, 64, 1024)
		if !errors.Is(err, scopemigrate.ErrBudget) {
			t.Fatal(cursor, err)
		}
	}
}

func TestEpochCoversDatabaseMutationsAndRollback(t *testing.T) {
	w, _ := metadataFixture(t, 1)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO agents(id,username,actor_id,created_at,updated_at) VALUES('series-agent','series-agent','series-actor','now','now')`,
		`INSERT INTO hosts(id,slug,display_name,os_user,hostname,discovered_adapters_json,created_at) VALUES('host','host','host','user','host','[]','now')`,
		`INSERT INTO series_adapters(name,description,agent_id,host_id,expected_interval,created_at) VALUES('adapter','','series-agent','host',60,'now')`,
		`INSERT INTO series_definitions VALUES('series','adapter','','gauge')`,
		`INSERT INTO series_labels VALUES('series','{}')`,
	} {
		_, err := w.DB().Exec(q)
		must(t, err)
	}
	// Real canonical/authority paths with empty references. Optional role schema
	// is supplied as metadata here; separate historical tests retain #275's exact
	// schema-64/65 fixture. No hosted data is used.
	statements := []string{
		`UPDATE hosts SET revoked_at='now' WHERE id='host'`,
		`INSERT INTO series_points VALUES('series','{}',1,1,NULL,0)`,
		`UPDATE series_points SET value=2 WHERE series='series'`,
		`DELETE FROM series_points WHERE series='series'`,
		`INSERT INTO series_daily VALUES('series','{}',0,1,1,1,1,1,1,NULL)`,
		`UPDATE series_daily SET total=2 WHERE series='series'`,
		`DELETE FROM series_daily WHERE series='series'`,
		`INSERT INTO artifacts(id,kind,created_at,created_by,content_type,content_hash,content_refs_json) VALUES('new','note','now','owner','text/plain','missing','[]')`,
		`UPDATE artifacts SET archived_at='now' WHERE id='new'`,
		`UPDATE artifacts SET trashed_at='now' WHERE id='new'`,
		`INSERT INTO resource_handle_aliases(id,resource_type,alias_handle,resource_id,canonical_handle,created_at) VALUES('alias-id','artifact','old-alias','new','new','now')`,
		`UPDATE resource_handle_aliases SET alias_handle='next-alias' WHERE alias_handle='old-alias'`,
		`DELETE FROM resource_handle_aliases WHERE alias_handle='next-alias'`,
		`INSERT INTO pm_records(kind,id,workspace_id,actor_id,revision,body) VALUES('configuration','selected','workspace','pm',1,'{}')`,
		`UPDATE pm_records SET body='{}' WHERE id='selected'`,
		`DELETE FROM pm_records WHERE id='selected'`,
		`INSERT INTO scope_memberships VALUES('reader','legacy','reader',1)`,
		`UPDATE scope_memberships SET role='owner',generation=2 WHERE principal='reader'`,
		`DELETE FROM scope_memberships WHERE principal='reader'`,
		`INSERT INTO scope_aliases VALUES('legacy','artifact','alias','opaque-000000',0)`,
		`UPDATE scope_aliases SET retired=1 WHERE alias='alias'`,
		`DELETE FROM scope_aliases WHERE alias='alias'`,
		`UPDATE scope_domains SET state='transitioning',generation=2 WHERE id='legacy'`,
		`UPDATE scope_resources SET version=2 WHERE id='opaque-000000'`,
		`INSERT INTO resource_access_tombstones VALUES('artifact','new','alias','owner')`,
		`DELETE FROM artifacts WHERE id='new'`,
		`INSERT INTO resource_access_series_unknown VALUES('new-series','{}')`,
		`DELETE FROM resource_access_series_unknown WHERE series='new-series'`,
		`INSERT INTO agents(id,username,actor_id,created_at,updated_at) VALUES('agent','agent','actor','now','now')`,
		`UPDATE agents SET revoked_at='now' WHERE id='agent'`,
		`DELETE FROM agents WHERE id='agent'`,
		`INSERT INTO actors(id,display_name,created_at) VALUES('actor','actor','now')`,
		`UPDATE actors SET tags_json='[]' WHERE id='actor'`,
		`DELETE FROM actors WHERE id='actor'`,
	}
	for _, q := range statements {
		before := sourceEpoch(t, w.DB())
		tx, err := w.DB().BeginTx(ctx, nil)
		must(t, err)
		_, err = tx.Exec(q)
		must(t, err)
		after, err := (scopemigrate.MetadataSource{AuthorityToken: "synthetic-pm"}).Epoch(ctx, tx)
		must(t, err)
		if after <= before {
			t.Fatal("source/authority mutation did not invalidate", q)
		}
		must(t, tx.Rollback())
		if sourceEpoch(t, w.DB()) != before {
			t.Fatal("rollback retained epoch", q)
		}
		// Keep the source for the following UPDATE/DELETE, but assert it and
		// its epoch commit together, independently of the rollback trial.
		_, err = w.DB().Exec(q)
		must(t, err)
		if sourceEpoch(t, w.DB()) <= before {
			t.Fatal("commit lost epoch", q)
		}
	}
}

func TestRuntimeAuthorityBindingInvalidationAndRollback(t *testing.T) {
	w, r := metadataFixture(t, 1)
	ctx := context.Background()
	token := begin(t, r)
	epoch := sourceEpoch(t, w.DB())
	tx, err := w.DB().BeginTx(ctx, nil)
	must(t, err)
	must(t, storage.BindScopeMigrationAuthority(ctx, tx, "other-pm"))
	_, err = r.Source.Epoch(ctx, tx)
	if !errors.Is(err, scopemigrate.ErrEpochCoverage) {
		t.Fatal("old process authority remained valid", err)
	}
	nextSource := scopemigrate.MetadataSource{AuthorityToken: "other-pm"}
	nextEpoch, err := nextSource.Epoch(ctx, tx)
	must(t, err)
	if nextEpoch <= epoch {
		t.Fatal("PM replacement did not invalidate source")
	}
	must(t, tx.Rollback())
	if sourceEpoch(t, w.DB()) != epoch {
		t.Fatal("rolled back PM binding advanced epoch")
	}
	tx, err = w.DB().BeginTx(ctx, nil)
	must(t, err)
	must(t, storage.BindScopeMigrationAuthority(ctx, tx, "other-pm"))
	must(t, tx.Commit())
	_, err = r.Step(ctx, token, 64)
	if !errors.Is(err, scopemigrate.ErrEpochCoverage) {
		t.Fatal(err)
	}
	r.Source = nextSource
	p, err := r.Step(ctx, token, 64)
	must(t, err)
	if p.Generation != 2 || p.Processed != 0 {
		t.Fatal("new PM retained captured snapshot", p)
	}
}

func TestCaptureMismatchNeverAdvancesPlacement(t *testing.T) {
	w, r := metadataFixture(t, 1)
	_, err := w.DB().Exec(`INSERT INTO scope_migration_captures VALUES(1,'legacy','artifact','opaque-000000','wrong',1)`)
	must(t, err)
	token := begin(t, r)
	before, err := r.Report(context.Background())
	must(t, err)
	_, err = r.Step(context.Background(), token, 64)
	if !errors.Is(err, scopemigrate.ErrSourceChanged) {
		t.Fatal(err)
	}
	after, err := r.Report(context.Background())
	must(t, err)
	if after != before {
		t.Fatal("mismatched capture advanced watermark")
	}
}

func TestReaderTelemetryDoesNotInvalidateMigration(t *testing.T) {
	w, _ := metadataFixture(t, 1)
	// Reinstall must remove a stale broader experimental epoch trigger, rather
	// than merely skip creating another one for an excluded table.
	_, err := w.DB().Exec(`CREATE TRIGGER scope_migration_epoch_home_read_cursors_UPDATE AFTER UPDATE ON home_read_cursors BEGIN UPDATE scope_migration_epoch SET version=version+1 WHERE singleton=1; END`)
	must(t, err)
	tx, err := w.DB().BeginTx(context.Background(), nil)
	must(t, err)
	must(t, storage.InstallScopeMigrationEpoch(context.Background(), tx))
	must(t, tx.Commit())
	var stale int
	must(t, w.DB().QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name='scope_migration_epoch_home_read_cursors_UPDATE'`).Scan(&stale))
	if stale != 0 {
		t.Fatal("reinstall retained obsolete telemetry trigger")
	}
	epoch := sourceEpoch(t, w.DB())
	for _, q := range []string{
		`INSERT INTO home_read_cursors VALUES('reader','group','now','event','now')`,
		`UPDATE home_read_cursors SET updated_at='later' WHERE reader_id='reader'`,
		`DELETE FROM home_read_cursors WHERE reader_id='reader'`,
		`INSERT INTO overview_visits VALUES('reader','now','{}')`,
		`UPDATE overview_visits SET visited_at='later' WHERE principal_id='reader'`,
		`DELETE FROM overview_visits WHERE principal_id='reader'`,
		`INSERT INTO series_request_budgets VALUES('reader',1,1)`,
		`UPDATE series_request_budgets SET n=n+1 WHERE scope='reader'`,
		`DELETE FROM series_request_budgets WHERE scope='reader'`,
		`INSERT INTO series_ingestion_days VALUES(1,1)`,
		`UPDATE series_ingestion_days SET n=n+1 WHERE day=1`,
		`DELETE FROM series_ingestion_days WHERE day=1`,
	} {
		_, err := w.DB().Exec(q)
		must(t, err)
		if sourceEpoch(t, w.DB()) != epoch {
			t.Fatal("reader telemetry restarted migration", q)
		}
	}
}

func TestEpochRejectsAlterDropAndTriggerReplacement(t *testing.T) {
	for _, q := range []string{
		`ALTER TABLE pm_records ADD COLUMN authority_generation INTEGER`,
		`DROP TABLE overview_visits`,
		`DROP TRIGGER scope_migration_epoch_scope_memberships_UPDATE`,
		`CREATE TRIGGER extra_authority_rule BEFORE UPDATE ON scope_memberships BEGIN SELECT RAISE(ABORT,'new rule'); END`,
	} {
		t.Run(q, func(t *testing.T) {
			w, r := metadataFixture(t, 1)
			token := begin(t, r)
			before, err := r.Report(context.Background())
			must(t, err)
			_, err = w.DB().Exec(q)
			must(t, err)
			_, err = r.Step(context.Background(), token, 64)
			if !errors.Is(err, scopemigrate.ErrEpochCoverage) {
				t.Fatal("schema mutation retained coverage", err)
			}
			after, err := r.Report(context.Background())
			must(t, err)
			if after != before {
				t.Fatal("invalidated schema advanced checkpoint")
			}
		})
	}
}

func TestEpochSchemaChangeFailsClosedAndReinstallInvalidates(t *testing.T) {
	w, r := metadataFixture(t, 1)
	ctx := context.Background()
	token := begin(t, r)
	before, err := r.Report(ctx)
	must(t, err)
	_, err = w.DB().Exec(`CREATE TABLE optional_roles(principal TEXT PRIMARY KEY,role TEXT)`)
	must(t, err)
	_, err = r.Step(ctx, token, 64)
	if !errors.Is(err, scopemigrate.ErrEpochCoverage) {
		t.Fatal("new optional schema silently bypassed epoch", err)
	}
	after, err := r.Report(ctx)
	must(t, err)
	if before != after {
		t.Fatal("uncovered schema advanced watermark")
	}
	tx, err := w.DB().BeginTx(ctx, nil)
	must(t, err)
	must(t, storage.InstallScopeMigrationEpoch(ctx, tx))
	must(t, tx.Commit())
	p, err := r.Step(ctx, token, 64)
	must(t, err)
	if p.Generation != 2 || p.Processed != 0 {
		t.Fatal("coverage reinstall did not restart snapshot", p)
	}
	epoch := sourceEpoch(t, w.DB())
	_, err = w.DB().Exec(`INSERT INTO optional_roles VALUES('owner','admin')`)
	must(t, err)
	if sourceEpoch(t, w.DB()) <= epoch {
		t.Fatal("optional authority schema not captured")
	}
	// Verify unconditional I/U/D coverage on every ordinary table, including
	// ancillary agents/actors/hosts and reference-free scalar columns.
	rows, err := w.DB().Query(`SELECT name FROM pragma_table_list WHERE schema='main' AND type='table' AND name NOT GLOB 'sqlite_*' AND name NOT IN ('scope_migration_epoch','scope_migration_jobs','scope_migration_placements','home_read_cursors','overview_visits','series_request_budgets','series_ingestion_days','scope_feed','scope_counters')`)
	must(t, err)
	var tables []string
	for rows.Next() {
		var table string
		must(t, rows.Scan(&table))
		tables = append(tables, table)
	}
	must(t, rows.Err())
	rows.Close()
	for _, table := range tables {
		for _, op := range []string{"INSERT", "UPDATE", "DELETE"} {
			var sqlText string
			must(t, w.DB().QueryRow(`SELECT sql FROM sqlite_schema WHERE type='trigger' AND name=?`, "scope_migration_epoch_"+table+"_"+op).Scan(&sqlText))
			if !strings.Contains(sqlText, "AFTER "+op+" ON") || !strings.Contains(sqlText, "version=version+1 WHERE singleton=1") || strings.Contains(sqlText, "WHEN") {
				t.Fatal("nonunconditional epoch trigger", table, op, sqlText)
			}
		}
	}
}

type captureAdapter struct{}

func (captureAdapter) ApplyCanonical(ctx context.Context, tx scopedrepo.MutationTx, m scopes.CanonicalMutation) error {
	return scopemigrate.CaptureCanonical(ctx, tx, m)
}

func capturedMutation() scopes.CanonicalMutation {
	return scopes.CanonicalMutation{Identity: scopes.ResourceIdentity{ScopeID: "legacy", Kind: "artifact", ResourceID: "opaque-000000", RID: 1, CanonicalID: "canonical-000000", CanonicalVersion: 2}, PreviousVersion: 1, Changes: []scopes.Change{{ScopeID: "legacy", Kind: "artifact", ResourceID: "opaque-000000", CanonicalVersion: 2, Family: "work", Audience: "all", After: &scopes.Projection{Title: "not captured"}}}}
}

func TestCanonicalCaptureSharesSourceTransaction(t *testing.T) {
	for _, mode := range []string{"success", "wrong_rid", "wrong_opaque", "wrong_canonical", "wrong_version", "sql_failure"} {
		t.Run(mode, func(t *testing.T) {
			w, _ := metadataFixture(t, 1)
			ctx := context.Background()
			if mode == "sql_failure" {
				_, err := w.DB().Exec(`CREATE TRIGGER capture_failure BEFORE INSERT ON scope_migration_captures BEGIN SELECT RAISE(ABORT,'injected failure'); END`)
				must(t, err)
				// Reviewed schema change must explicitly renew coverage.
				tx, err := w.DB().BeginTx(ctx, nil)
				must(t, err)
				must(t, storage.InstallScopeMigrationEpoch(ctx, tx))
				must(t, tx.Commit())
			}
			epoch := sourceEpoch(t, w.DB())
			tx, err := resourceaccess.NewDB(w.DB()).BeginTx(ctx, nil)
			must(t, err)
			_, err = tx.ExecContext(ctx, `UPDATE scope_resources SET version=2 WHERE scope_id='legacy' AND kind='artifact' AND id='opaque-000000'`)
			must(t, err)
			_, err = tx.ExecContext(ctx, `UPDATE artifacts SET archived_at='now' WHERE id='canonical-000000'`)
			must(t, err)
			m := capturedMutation()
			switch mode {
			case "wrong_rid":
				m.Identity.RID++
			case "wrong_opaque":
				m.Identity.ResourceID = "other"
				m.Changes[0].ResourceID = "other"
			case "wrong_canonical":
				m.Identity.CanonicalID = "other"
			case "wrong_version":
				m.Identity.CanonicalVersion++
				m.PreviousVersion++
				m.Changes[0].CanonicalVersion++
			}
			err = scopedrepo.ApplyCanonicalHooks(ctx, tx, m, captureAdapter{})
			if mode == "success" {
				must(t, err)
				must(t, tx.Commit())
			} else {
				if err == nil || !errors.Is(tx.Commit(), sql.ErrTxDone) {
					t.Fatal("failed capture left canonical transaction committable", err)
				}
			}
			var version, captures int
			var archived sql.NullString
			must(t, w.DB().QueryRow(`SELECT version FROM scope_resources WHERE id='opaque-000000'`).Scan(&version))
			must(t, w.DB().QueryRow(`SELECT count(*) FROM scope_migration_captures`).Scan(&captures))
			must(t, w.DB().QueryRow(`SELECT archived_at FROM artifacts WHERE id='canonical-000000'`).Scan(&archived))
			if mode == "success" {
				if version != 2 || captures != 1 || !archived.Valid || sourceEpoch(t, w.DB()) <= epoch {
					t.Fatal(version, captures)
				}
			} else if version != 1 || captures != 0 || archived.Valid || sourceEpoch(t, w.DB()) != epoch {
				t.Fatal("source/capture/epoch did not roll back together", version, captures)
			}
		})
	}
}

func TestWorkerDisabledAndShutdownJoin(t *testing.T) {
	w, err := scopemigrate.StartWorker(context.Background(), false, nil, 0, nil)
	must(t, err)
	must(t, w.Close())
	_, r := metadataFixture(t, 2)
	r.CheckDisk = func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
	w, err = scopemigrate.StartWorker(context.Background(), true, r, time.Millisecond, nil)
	must(t, err)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := w.Close(); !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}
