package scopedrepo

import (
	"agent-nexus-core/internal/readmodel"
	"agent-nexus-core/internal/scopes"
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

type adapterFeed struct{ snapshot FeedSnapshot }

func (r *adapterFeed) Snapshot() (FeedSnapshot, error)                      { return r.snapshot, nil }
func (*adapterFeed) Candidates(int, *FeedKey, int) ([]FeedCandidate, error) { return nil, nil }
func (*adapterFeed) Hydrate([]FeedReference) ([]FeedItem, error)            { return nil, nil }
func (*adapterFeed) Buckets([]string) (map[string]int64, error)             { return nil, nil }
func TestReadModelAdapterFreeze(t *testing.T) {
	r := &adapterFeed{snapshot: FeedSnapshot{Binding: "binding", AsOf: time.Now(), Scopes: []FeedScope{{ID: "public", Generation: 1, Availability: FeedAvailable, Ready: true}}}}
	d := scopes.DirectoryPage{Bindings: []scopes.Binding{{ID: "public", Available: true}}, MoreScopes: true}
	a := AdaptReadModel(r, d, "opaque")
	d.Bindings[0].ID = "changed"
	s, err := a.Snapshot(context.Background())
	if err != nil || !s.MoreScopes || s.DirectoryContinuation != "opaque" {
		t.Fatal(s, err)
	}
	s.Scopes[0].Generation = 77
	if _, err = a.Snapshot(context.Background()); err != nil {
		t.Fatal("returned snapshot aliases frozen state", err)
	}
	r.snapshot.Scopes[0].Generation = 2
	if _, err = a.Snapshot(context.Background()); !errors.Is(err, readmodel.ErrProjection) {
		t.Fatal("generation drift", err)
	}
	r.snapshot.Scopes[0].Generation = 1
	if _, err = a.Snapshot(context.Background()); !errors.Is(err, readmodel.ErrProjection) {
		t.Fatal("failure not sticky", err)
	}
}
func TestReadModelAdapterDirectoryMismatch(t *testing.T) {
	for _, d := range []scopes.DirectoryPage{{Bindings: []scopes.Binding{}}, {Bindings: []scopes.Binding{{ID: "public", Available: false}}}, {Bindings: []scopes.Binding{{ID: "wrong", Available: true}}}, {Bindings: []scopes.Binding{{ID: "public", Available: true}}, MoreScopes: true}} {
		r := &adapterFeed{snapshot: FeedSnapshot{Binding: "binding", AsOf: time.Now(), Scopes: []FeedScope{{ID: "public", Generation: 1, Availability: FeedAvailable, Ready: true}}}}
		if _, err := AdaptReadModel(r, d, "").Snapshot(context.Background()); !errors.Is(err, readmodel.ErrProjection) {
			t.Fatal(d, err)
		}
	}
}

type noSQLTx struct{ called bool }

func (t *noSQLTx) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	t.called = true
	return nil, errors.New("unexpected")
}
func (t *noSQLTx) QueryContext(context.Context, string, ...any) (*sql.Rows, error) {
	t.called = true
	return nil, errors.New("unexpected")
}
func TestReadModelExecutorExactSQL(t *testing.T) {
	for _, q := range []string{"COMMIT", readmodel.InsertFeed + "; DELETE FROM documents", " " + readmodel.InsertFeed, "SELECT * FROM documents"} {
		tx := &noSQLTx{}
		e := &hookExecutor{tx: tx}
		if _, err := e.Exec(context.Background(), q); !errors.Is(err, readmodel.ErrProjection) || tx.called {
			t.Fatal(q, err, tx.called)
		}
		if _, err := e.Exec(context.Background(), readmodel.InsertFeed); !errors.Is(err, readmodel.ErrProjection) || tx.called {
			t.Fatal("ignored failure lost", err)
		}
	}
}
