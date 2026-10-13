package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

// The trigger failures run after canonical SQL has executed. The deferred FK
// variant rejects COMMIT itself, after both dirty tables have been written.
func TestProjectionInvalidationCanonicalCommitBoundary(t *testing.T) {
	for _, boundary := range []string{"queue", "generation", "commit"} {
		t.Run(boundary, func(t *testing.T) {
			for _, operation := range []string{"event", "topic_create", "topic_patch", "topic_archive", "document_create", "document_update", "document_patch", "document_archive", "document_unarchive", "document_trash", "document_restore", "board_create", "board_update", "board_archive", "board_unarchive", "board_trash", "board_restore", "card_create", "card_update", "card_archive", "work"} {
				t.Run(operation, func(t *testing.T) {
					ctx := context.Background()
					ws, err := initializeTestWorkspace(ctx, t.TempDir())
					if err != nil {
						t.Fatal(err)
					}
					defer ws.Close()
					store := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
					topic, err := store.CreateTopic(ctx, "actor", map[string]any{"title": "Seed", "summary": "seed summary"})
					if err != nil {
						t.Fatal(err)
					}
					topicID := anyStringValue(topic.Topic["id"])
					threadID := anyStringValue(topic.Topic["thread_id"])
					doc, revision, err := store.CreateDocument(ctx, "actor", map[string]any{"title": "Seed document"}, "body", "text", nil)
					if err != nil {
						t.Fatal(err)
					}
					docID := anyStringValue(doc["id"])
					board, err := store.CreateBoard(ctx, "actor", map[string]any{"title": "Seed board"})
					if err != nil {
						t.Fatal(err)
					}
					boardID := anyStringValue(board["id"])
					card, err := store.CreateBoardCard(ctx, "actor", boardID, AddBoardCardInput{Title: "Seed card"})
					if err != nil {
						t.Fatal(err)
					}
					cardID := anyStringValue(card.Card["id"])
					switch operation {
					case "document_unarchive":
						_, _, err = store.ArchiveDocument(ctx, "actor", docID)
					case "document_restore":
						_, _, err = store.TrashDocument(ctx, "actor", docID, "test")
					case "board_unarchive":
						_, err = store.ArchiveBoard(ctx, "actor", boardID)
					case "board_restore":
						_, err = store.TrashBoard(ctx, "actor", boardID, "test")
					}
					if err != nil {
						t.Fatal(err)
					}
					mutate := func(ctx context.Context) error {
						var err error
						switch operation {
						case "event":
							_, err = store.AppendEvent(ctx, "actor", map[string]any{"type": "message_posted", "thread_id": threadID, "summary": "new", "refs": []string{}})
						case "topic_create":
							_, err = store.CreateTopic(ctx, "actor", map[string]any{"title": "new topic", "summary": "new summary"})
						case "topic_patch":
							_, err = store.PatchTopic(ctx, "actor", topicID, map[string]any{"summary": "new"}, nil)
						case "topic_archive":
							_, err = store.ArchiveTopic(ctx, "actor", topicID)
						case "document_create":
							_, _, err = store.CreateDocument(ctx, "actor", map[string]any{"title": "new document"}, "new", "text", nil)
						case "document_update":
							_, _, err = store.UpdateDocument(ctx, "actor", docID, nil, anyStringValue(revision["revision_id"]), "new", "text", nil, nil)
						case "document_patch":
							_, _, err = store.PatchDocument(ctx, "actor", docID, map[string]any{"summary": "new"}, nil)
						case "document_archive":
							_, _, err = store.ArchiveDocument(ctx, "actor", docID)
						case "document_unarchive":
							_, _, err = store.UnarchiveDocument(ctx, "actor", docID)
						case "document_trash":
							_, _, err = store.TrashDocument(ctx, "actor", docID, "test")
						case "document_restore":
							_, _, err = store.RestoreDocument(ctx, "actor", docID, "test")
						case "board_create":
							_, err = store.CreateBoard(ctx, "actor", map[string]any{"title": "new board"})
						case "board_update":
							_, err = store.UpdateBoard(ctx, "actor", boardID, map[string]any{"summary": "new"}, nil)
						case "board_archive":
							_, err = store.ArchiveBoard(ctx, "actor", boardID)
						case "board_unarchive":
							_, err = store.UnarchiveBoard(ctx, "actor", boardID)
						case "board_trash":
							_, err = store.TrashBoard(ctx, "actor", boardID, "test")
						case "board_restore":
							_, err = store.RestoreBoard(ctx, "actor", boardID)
						case "card_create":
							_, err = store.CreateBoardCard(ctx, "actor", boardID, AddBoardCardInput{Title: "new card"})
						case "card_update":
							assignee := "actor"
							_, err = store.UpdateBoardCard(ctx, "actor", boardID, cardID, UpdateBoardCardInput{Assignee: &assignee})
						case "card_archive":
							_, err = store.ArchiveBoardCard(ctx, "actor", boardID, cardID, RemoveBoardCardInput{})
						case "work":
							_, err = store.CreateWork(ctx, "actor", boardID, map[string]any{"title": "new work"})
						}
						return err
					}
					tables := []string{"events", "threads", "topics", "documents", "document_revisions", "artifacts", "boards", "cards", "ref_edges", "derived_topic_dirty_queue", "topic_projection_refresh_status"}
					before := projectionAtomicSnapshot(t, ws.DB(), tables)
					table := "derived_topic_dirty_queue"
					statement := "SELECT RAISE(ABORT, 'injected projection failure');"
					if boundary != "queue" {
						table = "topic_projection_refresh_status"
					}
					if boundary == "commit" {
						ws.DB().SetMaxOpenConns(1)
						if _, err := ws.DB().ExecContext(ctx, "PRAGMA foreign_keys=ON"); err != nil {
							t.Fatal(err)
						}
						for _, query := range []string{`CREATE TABLE projection_guard_parent(id TEXT PRIMARY KEY)`, `CREATE TABLE projection_guard_child(id TEXT REFERENCES projection_guard_parent(id) DEFERRABLE INITIALLY DEFERRED)`} {
							if _, err := ws.DB().ExecContext(ctx, query); err != nil {
								t.Fatal(err)
							}
						}
						statement = "INSERT INTO projection_guard_child(id) VALUES ('missing');"
					}
					if _, err := ws.DB().ExecContext(ctx, fmt.Sprintf("CREATE TRIGGER fail_projection BEFORE INSERT ON %s BEGIN %s END", table, statement)); err != nil {
						t.Fatal(err)
					}
					err = mutate(ctx)
					if err == nil || (boundary != "commit" && !strings.Contains(err.Error(), "injected projection failure")) || (boundary == "commit" && !strings.Contains(strings.ToLower(err.Error()), "foreign key")) {
						t.Fatalf("expected %s failure, got %v", boundary, err)
					}
					if boundary == "commit" {
						// A deferred SQLite COMMIT rejection leaves the driver connection
						// in its failed transaction. Discard it to model process loss;
						// the assertions below read durable state on a fresh connection.
						ws.DB().SetMaxIdleConns(0)
						ws.DB().SetMaxIdleConns(1)
					}
					after := projectionAtomicSnapshot(t, ws.DB(), tables)
					if !reflect.DeepEqual(before, after) {
						for _, table := range tables {
							if before[table] != after[table] {
								t.Errorf("%s changed despite failed transaction", table)
							}
						}
					}
					if _, err := ws.DB().ExecContext(ctx, "DROP TRIGGER fail_projection"); err != nil {
						t.Fatal(err)
					}
					// A response canceled immediately after the canonical store returns must
					// leave the generation durable without any handler-owned enqueue call.
					writeCtx, cancel := context.WithCancel(ctx)
					err = mutate(writeCtx)
					cancel()
					if err != nil {
						t.Fatalf("retry without injection: %v", err)
					}
					after = projectionAtomicSnapshot(t, ws.DB(), tables)
					if before["topic_projection_refresh_status"] == after["topic_projection_refresh_status"] {
						t.Fatal("successful canonical write did not invalidate projections")
					}
					restarted := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
					entries, err := restarted.ListDerivedTopicProjectionDirtyEntries(ctx, 100, time.Now())
					if err != nil || len(entries) == 0 {
						t.Fatalf("lost durable dirty work after canceled response: %v %v", entries, err)
					}
				})
			}
		})
	}
}

func projectionAtomicSnapshot(t *testing.T, db *sql.DB, tables []string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, table := range tables {
		rows, err := db.Query("SELECT * FROM " + table + " ORDER BY 1")
		if err != nil {
			t.Fatal(err)
		}
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		var data [][]any
		for rows.Next() {
			values := make([]any, len(columns))
			dest := make([]any, len(columns))
			for i := range values {
				dest[i] = &values[i]
			}
			if err := rows.Scan(dest...); err != nil {
				t.Fatal(err)
			}
			data = append(data, values)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
		raw, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		out[table] = string(raw)
	}
	return out
}

func TestProjectionInvalidationReassociation(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	store := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	oldTopic, err := store.CreateThread(ctx, "actor", map[string]any{"title": "Old", "type": "incident"})
	if err != nil {
		t.Fatal(err)
	}
	newTopic, err := store.CreateThread(ctx, "actor", map[string]any{"title": "New", "type": "incident"})
	if err != nil {
		t.Fatal(err)
	}
	oldThread := anyStringValue(oldTopic.Thread["id"])
	newThread := anyStringValue(newTopic.Thread["id"])
	doc, revision, err := store.CreateDocument(ctx, "actor", map[string]any{"title": "Move", "thread_id": oldThread}, "body", "text", nil)
	if err != nil {
		t.Fatal(err)
	}
	board, err := store.CreateBoard(ctx, "actor", map[string]any{"title": "Board"})
	if err != nil {
		t.Fatal(err)
	}
	card, err := store.CreateBoardCard(ctx, "actor", anyStringValue(board["id"]), AddBoardCardInput{Title: "Card", ParentThreadID: oldThread, Refs: []string{"thread:" + oldThread}})
	if err != nil {
		t.Fatal(err)
	}
	assertAdvanced := func(mutate func() error) {
		t.Helper()
		before, err := store.GetTopicProjectionRefreshStatuses(ctx, []string{oldThread, newThread})
		if err != nil {
			t.Fatal(err)
		}
		if err := mutate(); err != nil {
			t.Fatal(err)
		}
		after, err := store.GetTopicProjectionRefreshStatuses(ctx, []string{oldThread, newThread})
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range []string{oldThread, newThread} {
			if after[id].DesiredGeneration <= before[id].DesiredGeneration {
				t.Fatalf("reassociation missed thread %s: before=%#v after=%#v", id, before[id], after[id])
			}
		}
	}
	assertAdvanced(func() error {
		_, _, err := store.UpdateDocument(ctx, "actor", anyStringValue(doc["id"]), map[string]any{"thread_id": newThread}, anyStringValue(revision["revision_id"]), "new body", "text", nil, nil)
		return err
	})
	assertAdvanced(func() error {
		refs := []string{"thread:" + newThread}
		_, err := store.UpdateBoardCard(ctx, "actor", anyStringValue(board["id"]), anyStringValue(card.Card["id"]), UpdateBoardCardInput{ParentThreadID: &newThread, Refs: &refs})
		return err
	})
	// Event references can arrive as public handles; queue keys remain canonical IDs.
	assertAdvanced(func() error {
		_, err := store.AppendEvent(ctx, "actor", map[string]any{"type": "card_updated", "thread_id": oldThread, "refs": []string{}, "payload": map[string]any{"related_refs": []string{makePublicTypedRef(ctx, store.db, "thread", newThread)}}})
		return err
	})
}

func TestProjectionRetryBackoffCapAndErrorRetention(t *testing.T) {
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	store := NewTestStore(ws.DB(), "")
	now := time.Now().UTC()
	if err := store.MarkTopicProjectionsDirty(ctx, []string{"retry-thread"}, now); err != nil {
		t.Fatal(err)
	}
	for _, delay := range []time.Duration{5, 10, 20, 40, 80, 160, 300, 300} {
		generation, err := store.MarkTopicProjectionRefreshStarted(ctx, "retry-thread", now)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.MarkTopicProjectionRefreshFailed(ctx, "retry-thread", generation, now, "failure"); err != nil {
			t.Fatal(err)
		}
		var retry int64
		if err := ws.DB().QueryRowContext(ctx, "SELECT retry_after FROM topic_projection_refresh_status WHERE thread_id=?", "retry-thread").Scan(&retry); err != nil {
			t.Fatal(err)
		}
		if want := now.Add(delay * time.Second).UnixMilli(); retry != want {
			t.Fatalf("retry eligibility=%d want %d", retry, want)
		}
		now = now.Add(delay * time.Second)
	}
	if _, err := store.MarkTopicProjectionRefreshStarted(ctx, "retry-thread", now); err != nil {
		t.Fatal(err)
	}
	statuses, err := store.GetTopicProjectionRefreshStatuses(ctx, []string{"retry-thread"})
	if err != nil {
		t.Fatal(err)
	}
	if statuses["retry-thread"].LastErrorMessage != "failure" {
		t.Fatal("starting a retry hid the unresolved error")
	}
}
