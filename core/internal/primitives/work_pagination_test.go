package primitives_test

import (
	"agent-nexus-core/internal/scopesearch"
	"agent-nexus-core/internal/storage"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/primitives"
)

func TestWorkListNewestUpdatedFirstAndStableCursor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
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
	first, err := s.ListWork(primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "actor-1"}), primitives.WorkListFilter{})
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
	second, err := s.ListWork(primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "actor-1"}), primitives.WorkListFilter{Cursor: first.NextCursor})
	if err != nil || len(second.Work) != 10 || second.NextCursor != "" {
		t.Fatalf("second page: %d %v", len(second.Work), err)
	}
	for i, item := range second.Work {
		if item["id"] != want[i+50].id {
			t.Fatalf("row %d: %v, want %s", i, item["id"], want[i+50].id)
		}
	}
	fresh, err := s.ListWork(primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "actor-1"}), primitives.WorkListFilter{Limit: 2})
	if err != nil || len(fresh.Work) != 2 || fresh.Work[0]["id"] != newer["id"] || fresh.Work[1]["id"] != want[49].id {
		t.Fatalf("new updates hidden: %+v %v", fresh, err)
	}
	for _, cursor := range []string{"!", base64.RawURLEncoding.EncodeToString([]byte(`{}`)), base64.RawURLEncoding.EncodeToString([]byte(want[0].id))} {
		if _, err := s.ListWork(primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "actor-1"}), primitives.WorkListFilter{Cursor: cursor}); !errors.Is(err, primitives.ErrInvalidCursor) {
			t.Fatalf("cursor %q: %v", cursor, err)
		}
	}
}

func TestWorkListPaginationAdvancesPastAccessFilteredPages(t *testing.T) {
	if testing.Short() {
		t.Skip("scale-corpus budget guard; the non-short CI tier keeps the full fixture")
	}
	t.Parallel()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	publicBoard, err := s.CreateBoard(ctx, "reader", map[string]any{"title": "Public Board"})
	if err != nil {
		t.Fatal(err)
	}
	privateBoard, err := s.CreateBoard(ctx, "other-owner", map[string]any{"title": "Private Board"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.PatchThread(ctx, "other-owner", privateBoard["thread_id"].(string), map[string]any{"pm_actor_id": "other-owner"}, nil); err != nil {
		t.Fatal(err)
	}

	base := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	var wantVisibleIDs []string
	// Create 4 older visible cards.
	for i := 0; i < 4; i++ {
		card, err := s.CreateWork(ctx, "reader", publicBoard["id"].(string), map[string]any{"title": fmt.Sprintf("visible-%d", i)})
		if err != nil {
			t.Fatal(err)
		}
		id := card["id"].(string)
		at := base.Add(time.Duration(i) * time.Minute)
		if _, err := ws.DB().Exec(`UPDATE cards SET updated_at=? WHERE id=?`, at.Format(time.RFC3339Nano), id); err != nil {
			t.Fatal(err)
		}
		wantVisibleIDs = append([]string{id}, wantVisibleIDs...)
	}
	// Create 1,010 newer PM-private cards that exceed the 20-pass filter budget at limit=2.
	privateIDs := map[string]bool{}
	privateTimestamps := map[string]bool{}
	// Seed the rejected prefix in one transaction: identical rows and trigger
	// work, without 1,010 per-statement WAL commits.
	seedTx, err := ws.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer seedTx.Rollback()
	for i := 0; i < 1010; i++ {
		cardID := fmt.Sprintf("private-card-%04d", i)
		at := base.Add(time.Hour + time.Duration(i)*time.Second).Format(time.RFC3339Nano)
		privateIDs[cardID] = true
		privateTimestamps[at] = true
		if _, err := seedTx.Exec(
			`INSERT INTO cards(id,handle,board_id,rank,title,summary,column_key,thread_id,created_at,created_by,updated_at,updated_by) VALUES(?,?,?,'',?,'','backlog',?,?,?,?,'other-owner')`,
			cardID, cardID, privateBoard["id"], cardID, privateBoard["thread_id"], at, "other-owner", at,
		); err != nil {
			t.Fatal(err)
		}
	}
	if err := seedTx.Commit(); err != nil {
		t.Fatal(err)
	}

	visibleForReader := func(_ string, privateOwner string) bool {
		return privateOwner == "" || privateOwner == "reader"
	}
	var gotIDs []string
	cursor := ""
	for pageNum := 0; pageNum < 10; pageNum++ {
		// Every page can land on another server process.
		s = primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
		// Admit the private prefix as its owner so the explicit narrower
		// visibility callback still exercises pagination across rejected rows.
		page, err := s.ListWork(primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "other-owner"}), primitives.WorkListFilter{
			Limit:   2,
			Cursor:  cursor,
			Visible: visibleForReader,
		})
		if err != nil {
			t.Fatalf("page %d failed: %v", pageNum, err)
		}
		if page.NextCursor != "" {
			decoded, err := base64.RawURLEncoding.DecodeString(page.NextCursor)
			if err != nil {
				t.Fatalf("decode NextCursor: %v", err)
			}
			var payload struct {
				ID        string `json:"id"`
				UpdatedAt string `json:"updated_at"`
			}
			_ = json.Unmarshal(decoded, &payload)
			if privateIDs[payload.ID] || privateTimestamps[payload.UpdatedAt] || strings.Contains(string(decoded), "private-card-") {
				t.Fatalf("NextCursor exposed rejected card position: %s", string(decoded))
			}
		}
		for _, item := range page.Work {
			gotIDs = append(gotIDs, item["id"].(string))
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	if len(gotIDs) != len(wantVisibleIDs) {
		t.Fatalf("got %d visible work items (%v), want %d (%v)", len(gotIDs), gotIDs, len(wantVisibleIDs), wantVisibleIDs)
	}
	for i := range wantVisibleIDs {
		if gotIDs[i] != wantVisibleIDs[i] {
			t.Fatalf("visible item %d = %s, want %s", i, gotIDs[i], wantVisibleIDs[i])
		}
	}
}

func TestWorkCursorReaderFiltersRestartAndNoEviction(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root := t.TempDir()
	ws, err := initializeTestWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { ws.Close() }()
	s := primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := s.CreateBoard(ctx, "reader", map[string]any{"title": "Public"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		if _, err = s.CreateWork(ctx, "reader", board["id"].(string), map[string]any{"title": fmt.Sprint(i)}); err != nil {
			t.Fatal(err)
		}
	}
	reader := primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "reader"})
	first, err := s.ListWork(reader, primitives.WorkListFilter{Limit: 1})
	if err != nil || first.NextCursor == "" {
		t.Fatal(first, err)
	}
	expected, err := s.ListWork(reader, primitives.WorkListFilter{Limit: 2, Cursor: first.NextCursor})
	if err != nil || len(expected.Work) != 2 {
		t.Fatal(expected, err)
	}
	other := primitives.WithAccessScope(ctx, primitives.AccessScope{ActorID: "other"})
	if _, err = s.ListWork(other, primitives.WorkListFilter{Limit: 1, Cursor: first.NextCursor}); !errors.Is(err, primitives.ErrInvalidCursor) {
		t.Fatalf("cross-reader cursor accepted: %v", err)
	}
	for _, filter := range []primitives.WorkListFilter{
		{ProjectRef: "board:other"}, {Source: "remote"}, {Owner: "other"}, {Phase: "done"},
		{Freshness: "stale"}, {Query: "different"}, {Visible: func(string, string) bool { return true }},
	} {
		filter.Cursor = first.NextCursor
		if _, err = s.ListWork(reader, filter); !errors.Is(err, primitives.ErrInvalidCursor) {
			t.Fatalf("cross-filter cursor accepted: %+v %v", filter, err)
		}
	}
	// Previously the 257th key evicted an issued cursor.
	// Exercise cursor issuance under the same read boundary as HTTP handlers.
	// This loop tests key retention, not 260 repeated canonical policy compiles.
	read, closeRead, err := s.BeginResourceRead(reader)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 260; i++ {
		if _, err = s.ListWork(read, primitives.WorkListFilter{Limit: 1}); err != nil {
			closeRead()
			t.Fatal(err)
		}
	}
	closeRead()
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}
	ws, err = storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	s = primitives.NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	reader = primitives.WithRequestAccessScope(ctx, primitives.AccessScope{ActorID: "reader"})
	resumed, err := s.ListWork(reader, primitives.WorkListFilter{Limit: 2, Cursor: first.NextCursor})
	if err != nil || len(resumed.Work) != len(expected.Work) {
		t.Fatal(resumed, err)
	}
	for i := range expected.Work {
		if resumed.Work[i]["id"] != expected.Work[i]["id"] {
			t.Fatalf("restart skipped visible work: %v / %v", resumed, expected)
		}
	}
	// A fresh reader's own pagination is unaffected by another reader's traversal.
	own, err := s.ListWork(other, primitives.WorkListFilter{Limit: 2})
	if err != nil || len(own.Work) != 2 || own.Work[0]["id"] != first.Work[0]["id"] {
		t.Fatal(own, err)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(first.NextCursor)
	raw[len(raw)-1] ^= 1
	if _, err = s.ListWork(reader, primitives.WorkListFilter{Cursor: base64.RawURLEncoding.EncodeToString(raw)}); !errors.Is(err, primitives.ErrInvalidCursor) {
		t.Fatalf("tampered token accepted: %v", err)
	}
	var key []byte
	if err = ws.DB().QueryRow(`SELECT cursor_key FROM document_search_state WHERE singleton=1`).Scan(&key); err != nil {
		t.Fatal(err)
	}
	derived := sha256.Sum256(append([]byte("anx:work-cursor:v1:"), key...))
	codec, err := scopesearch.NewTokens(derived[:])
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"ExpiresAt", "Version"} {
		var payload map[string]any
		if err = codec.OpenContinuation(first.NextCursor, &payload); err != nil {
			t.Fatal(err)
		}
		payload[field] = 0
		token, err := codec.SealContinuation(payload)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.ListWork(reader, primitives.WorkListFilter{Cursor: token}); !errors.Is(err, primitives.ErrInvalidCursor) {
			t.Fatalf("invalid %s accepted: %v", field, err)
		}
	}
}
