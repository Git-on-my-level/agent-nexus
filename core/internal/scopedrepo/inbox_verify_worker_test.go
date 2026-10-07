package scopedrepo_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"agent-nexus-core/internal/scopedrepo"
)

func TestInboxVerificationSupervisorCompletionAndShutdown(t *testing.T) {
	// Default-disabled startup needs neither a DB, a valid job nor options.
	w, err := scopedrepo.StartInboxVerificationWorker(context.Background(), false, nil, "", scopedrepo.InboxVerificationWorkerOptions{})
	must(t, err)
	must(t, w.Close())
	if w.Diagnostics() != (scopedrepo.InboxVerificationWorkerDiagnostics{}) {
		t.Fatal("disabled worker performed work")
	}
	for _, n := range []int{16, 160} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			db, s, _ := inboxVerificationFixture(t, n, true)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			id, err := s.StartInboxVerification(ctx, "owner", "")
			must(t, err)
			options := scopedrepo.DefaultInboxVerificationWorkerOptions()
			options.Interval = time.Millisecond
			w, err := scopedrepo.StartInboxVerificationWorker(ctx, true, s, id, options)
			must(t, err)
			t.Cleanup(func() { w.Close() })
			// Legacy oracle work is not a whole-job time bound. Instrumented
			// SQLite can make a valid multi-slice job exceed ten seconds. Watch
			// for stalled progress using the actual lease window instead, while
			// retaining production per-slice deadline and receipt checks.
			deadline := time.NewTimer(options.LeaseDuration)
			defer deadline.Stop()
			ticker := time.NewTicker(5 * time.Millisecond)
			defer ticker.Stop()
			var slices uint64
			for {
				diagnostics := w.Diagnostics()
				// These finite 16/160-row fixtures need far fewer slices. An
				// endless loop cannot keep renewing the progress watchdog.
				if diagnostics.Slices > 64 {
					t.Fatal("supervisor exceeded finite fixture work", diagnostics)
				}
				if diagnostics.Completed == 1 {
					break
				}
				if diagnostics.Slices > slices {
					slices = diagnostics.Slices
					deadline.Reset(options.LeaseDuration)
				}
				select {
				case <-deadline.C:
					t.Fatal("supervisor stopped progressing", diagnostics)
				case <-ticker.C:
				}
			}
			must(t, w.Close())
			var leases, finished, receipts, proofs int
			must(t, db.QueryRow(`SELECT lease_until,finished_at,(SELECT count(*) FROM scope_inbox_comparison_receipts),(SELECT count(*) FROM scope_feed_selection_proofs) FROM scope_inbox_verification_jobs WHERE id=?`, id).Scan(&leases, &finished, &receipts, &proofs))
			if leases != 0 || finished <= 0 || receipts != 1 || proofs != 0 {
				t.Fatal("bad supervisor finish", leases, finished, receipts, proofs)
			}
		})
	}
}

func TestInboxVerificationSupervisorCloseJoinsAndResume(t *testing.T) {
	db, s, _ := inboxVerificationFixture(t, 160, true)
	id, err := s.StartInboxVerification(context.Background(), "owner", "")
	must(t, err)
	options := scopedrepo.DefaultInboxVerificationWorkerOptions()
	// A long interval guarantees shutdown can interrupt the pause rather than
	// waiting for another expensive oracle slice.
	options.Interval = time.Minute
	w, err := scopedrepo.StartInboxVerificationWorker(context.Background(), true, s, id, options)
	must(t, err)
	t.Cleanup(func() { w.Close() })
	deadline := time.Now().Add(3 * time.Second)
	for w.Diagnostics().Slices == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if w.Diagnostics().Slices == 0 {
		t.Fatal("worker did not checkpoint initial slice")
	}
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := w.Close(); !errors.Is(err, context.Canceled) {
				t.Errorf("shutdown result %v", err)
			}
		}()
	}
	wg.Wait()
	before := w.Diagnostics()
	var lease int
	must(t, db.QueryRow(`SELECT lease_until FROM scope_inbox_verification_jobs WHERE id=?`, id).Scan(&lease))
	if lease != 0 {
		t.Fatal("joined worker retained lease")
	}
	// A new Store has no in-process cursor or owner state to reuse.
	result, err := finishInboxVerification(t, scopedrepo.New(db), id)
	must(t, err)
	if !result.Complete || result.Eligible != 160 || w.Diagnostics() != before {
		t.Fatal("joined worker still active or restart lost data")
	}
}

func TestInboxVerificationRetentionBoundedAndIsolated(t *testing.T) {
	db, s, _ := inboxVerificationFixture(t, 160, true)
	id, err := s.StartInboxVerification(context.Background(), "owner", "")
	must(t, err)
	_, err = finishInboxVerification(t, s, id)
	must(t, err)
	active, err := s.StartInboxVerification(context.Background(), "owner", "")
	must(t, err)
	_, err = s.RunInboxVerificationSlice(context.Background(), active)
	must(t, err)
	var shadowBefore int
	must(t, db.QueryRow(`SELECT (SELECT count(*) FROM derived_inbox_items)+(SELECT count(*) FROM scope_inbox_order)+(SELECT count(*) FROM scope_feed_payloads)+(SELECT count(*) FROM scope_counters)`).Scan(&shadowBefore))
	deleted := 0
	for i := 0; i < 20; i++ {
		progress, err := s.PruneInboxVerification(context.Background(), time.Now().Add(time.Hour), 7)
		must(t, err)
		if progress.Deleted < 0 || progress.Deleted > 7 {
			t.Fatal("prune exceeded row bound", progress)
		}
		deleted += progress.Deleted
		if progress.Complete {
			break
		}
	}
	// 160 seen identities + one scope + one stream + four bucket rows + receipt
	// + job: seven-row chunks need more than the first twenty transactions.
	if deleted != 140 {
		t.Fatal("unexpected cleanup progress", deleted)
	}
	var activeRows int
	must(t, db.QueryRow(`SELECT count(*) FROM scope_inbox_verification_scopes WHERE job_id=?`, active).Scan(&activeRows))
	if activeRows != 1 {
		t.Fatal("cleanup touched active job")
	}
	for i := 0; i < 100; i++ {
		progress, err := s.PruneInboxVerification(context.Background(), time.Now().Add(time.Hour), 7)
		must(t, err)
		if progress.Deleted > 7 {
			t.Fatal("prune exceeded row bound", progress)
		}
		if progress.Complete && progress.Deleted == 0 {
			break
		}
		if i == 99 {
			t.Fatal("prune did not terminate")
		}
	}
	var jobs, receipts, shadowAfter int
	must(t, db.QueryRow(`SELECT (SELECT count(*) FROM scope_inbox_verification_jobs WHERE id=?),(SELECT count(*) FROM scope_inbox_comparison_receipts),(SELECT count(*) FROM derived_inbox_items)+(SELECT count(*) FROM scope_inbox_order)+(SELECT count(*) FROM scope_feed_payloads)+(SELECT count(*) FROM scope_counters)`, id).Scan(&jobs, &receipts, &shadowAfter))
	if jobs != 0 || receipts != 0 || shadowAfter != shadowBefore {
		t.Fatal("cleanup removed authoritative data or left receipt", jobs, receipts, shadowBefore, shadowAfter)
	}
	must(t, s.CancelInboxVerification(context.Background(), active))
	if _, err = s.RunInboxVerificationSlice(context.Background(), active); !errors.Is(err, scopedrepo.ErrInboxVerificationCancelled) {
		t.Fatal("cancelled job resumed", err)
	}
}
