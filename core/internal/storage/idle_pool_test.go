package storage

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestWorkspacePoolReusesConcurrentMaintenanceConnections(t *testing.T) {
	w, err := InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	const burst = 8
	warm := func() {
		var conns []*sql.Conn
		defer func() {
			for _, c := range conns {
				c.Close()
			}
		}()
		for range burst {
			c, err := w.DB().Conn(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			conns = append(conns, c)
		}
	}
	warm()
	before := w.DB().Stats().MaxIdleClosed
	warm()
	after := w.DB().Stats().MaxIdleClosed
	if after != before {
		t.Fatalf("repeated maintenance burst discarded %d warmed schema connections", after-before)
	}
	if idle := w.DB().Stats().Idle; idle > burst {
		t.Fatalf("retained %d idle connections beyond the cache bound", idle)
	}
}

func TestWorkspacePoolPreservesNestedReadAdmission(t *testing.T) {
	w, err := InitializeWorkspace(context.Background(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	var held []*sql.Conn
	defer func() {
		for _, c := range held {
			c.Close()
		}
	}()
	for range 8 {
		c, err := w.DB().Conn(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, c)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var one int
	if err = w.DB().QueryRowContext(ctx, `SELECT 1`).Scan(&one); err != nil || one != 1 {
		t.Fatalf("nested read could not acquire a connection: %v", err)
	}
}
