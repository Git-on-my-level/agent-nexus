package storage

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestCompletedMaintenanceDoesNotAcquireWriter(t *testing.T) {
	ctx := context.Background()
	w, err := InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for _, q := range []string{`UPDATE ask_subjects_job SET done=1`, `UPDATE resource_access_document_history_job SET done=1`, `UPDATE resource_access_compact_control SET stable=1`} {
		if _, err = w.DB().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	// Warm read connections before holding the WAL writer, so no connection
	// initialization pragma is confused with the worker's lock admission.
	var warm []*sql.Conn
	for range 3 {
		c, e := w.DB().Conn(ctx)
		if e != nil {
			t.Fatal(e)
		}
		warm = append(warm, c)
	}
	holder := warm[0]
	defer holder.Close()
	for _, c := range warm[1:] {
		c.Close()
	}
	tx, err := holder.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for _, tc := range []struct {
		name string
		run  func(context.Context) (bool, error)
	}{
		{"ask_subjects", w.MaintainAskSubjectsBatch}, {"document_history", w.MaintainDocumentAccessHistoryBatch}, {"compact_retired", w.maintainCompactAccessRetiredBatch},
		{"replay_gc", func(c context.Context) (bool, error) {
			n, e := CollectReplayReceipts(c, w.DB(), time.Now())
			return n == 0, e
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
			defer cancel()
			done, e := tc.run(c)
			if e != nil || !done {
				t.Fatalf("idle maintenance contended for writer: done=%v err=%v", done, e)
			}
		})
	}
}

func TestAskSubjectsContentionBacksOffAndResumes(t *testing.T) {
	ctx := context.Background()
	w, err := InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	holder, err := w.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Close()
	warm, err := w.DB().Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	warm.Close()
	tx, err := holder.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	reports := make(chan error, 20)
	done := make(chan struct{})
	go func() { defer close(done); w.RunAskSubjectsMaintenance(runCtx, func(e error) { reports <- e }) }()
	timer := time.NewTimer(900 * time.Millisecond)
	defer timer.Stop()
	<-timer.C
	attempts := len(reports)
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		cancel()
		<-done
		t.Fatal("worker did not resume and finish after writer released")
	}
	if attempts > 4 || attempts == 0 {
		t.Fatalf("contention generated %d retries in 900ms; expected backed-off attempts", attempts)
	}
}
