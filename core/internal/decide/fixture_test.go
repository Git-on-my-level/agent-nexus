package decide

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func fixture(t *testing.T) (*Service, *Store, Principal, *int) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "decide.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	st, err := NewStore(db)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	deps := Dependencies{
		Authorize: func(_ context.Context, p Principal, permission, ref string) error {
			if p.ActorID == "other" {
				return ErrForbidden
			}
			return nil
		},
		ReadContext: func(context.Context, Principal, string, string, int) (ContextPage, error) {
			return ContextPage{Items: []any{"current evidence"}}, nil
		},
		CheckDelivery: func(context.Context, Action) error { return nil },
		DecisionWork: func(context.Context, Principal, string) (DecisionWork, error) {
			return DecisionWork{Revision: "r1", Phase: "backlog"}, nil
		},
		CurrentRevision: func(context.Context, Principal, string) (string, error) { return "r1", nil },
		Execute: func(context.Context, Action) (Receipt, error) {
			return Receipt{Status: Delivered, ExternalID: "remote-1"}, nil
		},
	}
	s, err := NewService(st, Config{WorkspaceID: "ws"}, deps)
	if err != nil {
		t.Fatal(err)
	}
	return s, st, Principal{WorkspaceID: "ws", ActorID: "human", Human: true}, &count
}
