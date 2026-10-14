package storage

import (
	"context"
	"os"
	"testing"
	"time"

	"agent-nexus-core/internal/testutil/perfguard"
)

func TestCompactAccessPreServeCertifiesBeforeWriters(t *testing.T) {
	w := compactFixture(t, 450)
	start := time.Now()
	proof, resume, err := w.PrepareCompactAccessMigration(context.Background())
	if err != nil || resume || proof.Rows != 450 {
		t.Fatalf("proof=%+v resume=%v error=%v", proof, resume, err)
	}
	if time.Since(start) > 11*time.Second {
		t.Fatal("startup exceeded bounded preparation")
	}
}

func TestCompactAccessRetiredCleanupResumesWithoutChangingProof(t *testing.T) {
	w := compactFixture(t, 450)
	proof := compactCertify(t, w)
	if err := w.CutoverCompactAccess(context.Background(), proof); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := w.db.QueryRow(`SELECT count(*) FROM resource_access_unstable_edges`).Scan(&n); err != nil || n != 450 {
		t.Fatalf("retired rows=%d err=%v", n, err)
	}
	if done, err := w.maintainCompactAccessRetiredBatch(context.Background()); err != nil || done {
		t.Fatalf("first cleanup: %v %v", done, err)
	}
	if err := w.db.QueryRow(`SELECT count(*) FROM resource_access_unstable_edges`).Scan(&n); err != nil || n != 250 {
		t.Fatalf("cleanup did not stay bounded: %d %v", n, err)
	}
	root := w.layout.RootDir
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := InitializeWorkspace(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	var epoch int64
	if err = reopened.db.QueryRow(`SELECT version FROM resource_access_epoch`).Scan(&epoch); err != nil {
		t.Fatal(err)
	}
	reopened.RunCompactAccessRetiredMaintenance(context.Background())
	var after int64
	if err = reopened.db.QueryRow(`SELECT version FROM resource_access_epoch`).Scan(&after); err != nil || after != epoch {
		t.Fatalf("cleanup changed authority epoch %d -> %d: %v", epoch, after, err)
	}
	if p, e := reopened.VerifyCompactAccessCutover(context.Background()); e != nil || p.Rows != 450 {
		t.Fatalf("cleanup changed certified data: %+v %v", p, e)
	}
	if err = reopened.db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name='resource_access_unstable_edges' OR name='idx_resource_access_edges_target'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("retired schema retained: %d %v", n, err)
	}
}

func TestPerformanceCompactAccessPreServeStartup(t *testing.T) {
	if testing.Short() || os.Getenv("ANX_PERFORMANCE_TEST") != "1" {
		t.Skip("performance tier")
	}
	w := compactFixture(t, 0)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()
	if err := perfguard.Seed(ctx, w.db, w.layout.ArtifactContentDir, "startup-owner", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	var busy, logPages, copied int
	if err := w.db.QueryRow(`PRAGMA wal_checkpoint(TRUNCATE)`).Scan(&busy, &logPages, &copied); err != nil || busy != 0 {
		t.Fatalf("checkpoint %d %v", busy, err)
	}
	info, err := os.Stat(w.layout.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	proof, resume, err := w.PrepareCompactAccessMigration(ctx)
	ready := time.Since(start)
	if ready > 11*time.Second {
		t.Fatalf("startup exceeded 10s preparation budget: %s", ready)
	}
	expectedMirrors := 1 // Certified cutover removes the transient stable mirror.
	if resume {
		expectedMirrors = 2
	}
	var mirrors int
	if e := w.db.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name IN ('access_compact_insert','access_stable_insert')`).Scan(&mirrors); e != nil || mirrors != expectedMirrors {
		t.Fatalf("startup did not install mirrors: %d %v", mirrors, e)
	}
	if resume {
		// Maintenance writers begin only after installation, matching main's order.
		if _, e := w.db.Exec(`CREATE TABLE startup_writer_fixture(id INTEGER PRIMARY KEY,n INTEGER);INSERT INTO startup_writer_fixture VALUES(1,0)`); e != nil {
			t.Fatal(e)
		}
		writerCtx, stop := context.WithCancel(ctx)
		writerDone := make(chan error, 1)
		go func() {
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-writerCtx.Done():
					writerDone <- nil
					return
				case <-ticker.C:
				}
				_, e := w.db.ExecContext(writerCtx, `UPDATE startup_writer_fixture SET n=n+1`)
				if e != nil {
					if writerCtx.Err() != nil {
						e = nil
					}
					writerDone <- e
					return
				}
			}
		}()
		defer func() {
			stop()
			if e := <-writerDone; e != nil {
				t.Errorf("startup background writer: %v", e)
			}
		}()
		reports := 0
		w.RunCompactAccessMigration(ctx, func(p CompactAccessVerification, e error) { proof, err = p, e; reports++ })
		if reports != 1 {
			t.Fatalf("final reports=%d", reports)
		}
	}
	if err != nil || proof.Rows < 100000 {
		t.Fatalf("startup did not certify: %+v %v", proof, err)
	}
	t.Logf("startup_db_bytes=%d pre_serve=%s online_resume=%v total_certification=%s rows=%d", info.Size(), ready, resume, time.Since(start), proof.Rows)
}

func TestCompactAccessPreServeBudgetResumesWithoutSelectingReader(t *testing.T) {
	w := compactFixture(t, 1)
	held, err := w.db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Rollback()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, resume, err := w.PrepareCompactAccessMigration(ctx)
	if !resume || err == nil {
		t.Fatalf("startup contention did not request online resume: %v %v", resume, err)
	}
	if err = held.Rollback(); err != nil {
		t.Fatal(err)
	}
	var reader int
	if err = w.db.QueryRow(`SELECT reader FROM resource_access_compact_control`).Scan(&reader); err != nil || reader != 0 {
		t.Fatalf("uncertified reader=%d error=%v", reader, err)
	}
}

func TestCompactAccessInstalledShadowResumesWithoutReinstall(t *testing.T) {
	w := compactFixture(t, 20)
	if err := w.InitializeCompactAccessShadow(context.Background()); err != nil {
		t.Fatal(err)
	}
	ready, err := w.compactShadowReady(context.Background())
	if !ready || err != nil {
		t.Fatalf("ready=%v error=%v", ready, err)
	}
	// A missing generation trigger cannot be treated as a completed installation.
	compactExec(t, w, `DROP TRIGGER compact_generation_resource_access_exact_edges_insert`)
	ready, err = w.compactShadowReady(context.Background())
	if ready || err != nil {
		t.Fatalf("partial shadow accepted: %v %v", ready, err)
	}
	if err = w.InitializeCompactAccessShadow(context.Background()); err != nil {
		t.Fatal(err)
	}
	installs := 0
	ctx := WithCompactAccessMigrationObserver(context.Background(), func(stage string, e error) {
		if stage == "shadow_install" {
			installs++
		}
	})
	proof, resume, err := w.PrepareCompactAccessMigration(ctx)
	if err != nil || resume || installs != 0 || proof.Rows != 20 {
		t.Fatalf("proof=%+v resume=%v reinstalls=%d error=%v", proof, resume, installs, err)
	}
}
