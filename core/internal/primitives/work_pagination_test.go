package primitives_test

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/storage"
)

func TestWorkListNewestUpdatedFirstAndStableCursor(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := s.CreateBoard(ctx, "actor-1", map[string]any{"title": "Inbox"})
	if err != nil {
		t.Fatal(err)
	}
	type expected struct {
		id string
		at time.Time
	}
	var want []expected
	base := time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 60; i++ {
		card, err := s.CreateWork(ctx, "actor-1", board["id"].(string), map[string]any{"title": fmt.Sprint(i), "source": map[string]any{"authority": "nexus"}})
		if err != nil {
			t.Fatal(err)
		}
		id := card["id"].(string)
		at := base.Add(time.Duration(((i*17)%60)/3) * time.Nanosecond)
		if _, err := ws.DB().Exec(`UPDATE cards SET updated_at=? WHERE id=?`, at.Format(time.RFC3339Nano), id); err != nil {
			t.Fatal(err)
		}
		want = append(want, expected{id, at})
	}
	sort.Slice(want, func(i, j int) bool {
		if want[i].at.Equal(want[j].at) {
			return want[i].id > want[j].id
		}
		return want[i].at.After(want[j].at)
	})
	first, err := s.ListWork(ctx, primitives.WorkListFilter{})
	if err != nil || len(first.Work) != 50 || first.NextCursor == "" {
		t.Fatalf("first page: %d %v", len(first.Work), err)
	}
	for i, item := range first.Work {
		if item["id"] != want[i].id {
			t.Fatalf("row %d: %v, want %s", i, item["id"], want[i].id)
		}
	}
	// Move the boundary row ahead and insert a new card. Resuming must use the
	// timestamp in the cursor, not the boundary row's current timestamp.
	if _, err := ws.DB().Exec(`UPDATE cards SET updated_at=? WHERE id=?`, base.Add(time.Hour).Format(time.RFC3339Nano), want[49].id); err != nil {
		t.Fatal(err)
	}
	newer, err := s.CreateWork(ctx, "actor-1", board["id"].(string), map[string]any{"title": "New obligation", "source": map[string]any{"authority": "nexus"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.DB().Exec(`UPDATE cards SET updated_at=? WHERE id=?`, base.Add(2*time.Hour).Format(time.RFC3339Nano), newer["id"]); err != nil {
		t.Fatal(err)
	}
	second, err := s.ListWork(ctx, primitives.WorkListFilter{Cursor: first.NextCursor})
	if err != nil || len(second.Work) != 10 || second.NextCursor != "" {
		t.Fatalf("second page: %d %v", len(second.Work), err)
	}
	for i, item := range second.Work {
		if item["id"] != want[i+50].id {
			t.Fatalf("row %d: %v, want %s", i, item["id"], want[i+50].id)
		}
	}
	fresh, err := s.ListWork(ctx, primitives.WorkListFilter{Limit: 2})
	if err != nil || len(fresh.Work) != 2 || fresh.Work[0]["id"] != newer["id"] || fresh.Work[1]["id"] != want[49].id {
		t.Fatalf("new updates hidden: %+v %v", fresh, err)
	}
	for _, cursor := range []string{"!", base64.RawURLEncoding.EncodeToString([]byte(`{}`)), base64.RawURLEncoding.EncodeToString([]byte(want[0].id))} {
		if _, err := s.ListWork(ctx, primitives.WorkListFilter{Cursor: cursor}); !errors.Is(err, primitives.ErrInvalidCursor) {
			t.Fatalf("cursor %q: %v", cursor, err)
		}
	}
}
