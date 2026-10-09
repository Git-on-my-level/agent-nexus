package primitives

import (
	"context"
	"database/sql"
	"testing"
)

func TestStreamRevisionTracksSeparateWriterAndReleasesConnection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	s := NewTestStore(w.DB(), w.Layout().ArtifactContentDir)
	clock, err := s.OpenStreamRevision(ctx)
	if err != nil {
		t.Fatal(err)
	}
	v, err := clock.Current(ctx)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := sql.Open("sqlite", "file:"+w.Layout().DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	if _, err := writer.Exec(`INSERT INTO threads(id,updated_at,updated_by,body_json) VALUES('external','now','owner','{}')`); err != nil {
		t.Fatal(err)
	}
	next, err := clock.Current(ctx)
	if err != nil || next == v {
		t.Fatalf("external commit: %d => %d, err=%v", v, next, err)
	}
	if _, err := w.DB().Exec(`UPDATE threads SET body_json='{"pm_actor_id":"owner"}' WHERE id='external'`); err != nil {
		t.Fatal(err)
	}
	after, err := clock.Current(ctx)
	if err != nil || after == next {
		t.Fatalf("pool commit: %d => %d, err=%v", next, after, err)
	}
	if unchanged, err := clock.Current(ctx); err != nil || unchanged != after {
		t.Fatalf("read advanced version: %d err=%v", unchanged, err)
	}
	if err := clock.Close(); err != nil {
		t.Fatal(err)
	}
	if w.DB().Stats().InUse != 0 {
		t.Fatal("observer retained pool connection")
	}
	w.DB().SetMaxOpenConns(1)
	if single, err := s.OpenStreamRevision(ctx); err != nil || single != nil {
		t.Fatalf("single-connection pool must fall back, got %v %v", single, err)
	}
	if _, err := w.DB().Exec(`UPDATE threads SET updated_at='later' WHERE id='external'`); err != nil {
		t.Fatal(err)
	}
}
