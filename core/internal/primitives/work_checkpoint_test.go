package primitives

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"agent-nexus-core/internal/storage"
)

func checkpointTestFixture(t *testing.T) (*storage.Workspace, *Store, map[string]any) {
	t.Helper()
	ctx := context.Background()
	ws, err := initializeTestWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	s := NewTestStore(ws.DB(), ws.Layout().ArtifactContentDir)
	board, err := s.CreateBoard(ctx, "checkpoint-owner", map[string]any{"title": "Checkpoint board"})
	if err != nil {
		t.Fatal(err)
	}
	work, err := s.CreateWork(ctx, "checkpoint-owner", workString(board["id"]), map[string]any{"title": "Review draft"})
	if err != nil {
		t.Fatal(err)
	}
	return ws, s, work
}

func checkpointCounts(t *testing.T, db *sql.DB) (events, observations, replays int) {
	t.Helper()
	for query, dest := range map[string]*int{
		`SELECT count(*) FROM events`:              &events,
		`SELECT count(*) FROM work_observations`:   &observations,
		`SELECT count(*) FROM idempotency_replays`: &replays,
	} {
		if err := db.QueryRow(query).Scan(dest); err != nil {
			t.Fatal(err)
		}
	}
	return
}

func checkpointEnvelope(t *testing.T, work map[string]any) map[string]any {
	t.Helper()
	envelope, ok := work["checkpoint"].(map[string]any)
	if !ok {
		t.Fatalf("checkpoint missing from work projection: %#v", work)
	}
	return envelope
}

func TestPutWorkCheckpointCASDedupNoActivityAndRestart(t *testing.T) {
	ctx := context.Background()
	ws, store, work := checkpointTestFixture(t)
	cardID := workString(work["id"])
	content := map[string]any{
		"summary":     "Draft and reviewer context preserved",
		"next_action": "Resolve the remaining objection",
		"blockers":    []string{"Waiting for reviewer feedback"},
		"artifacts":   []any{map[string]any{"ref": "file:notes.txt", "portability": "local_only", "host": "author-laptop"}},
		"execution":   map[string]any{"authority": "agentctl", "run_id": "run-17"},
	}
	var workVersion int64
	var metadataAt, metadataBy, cardAt string
	if err := ws.DB().QueryRow(`SELECT version,updated_at,updated_by FROM work_metadata WHERE card_id=?`, cardID).Scan(&workVersion, &metadataAt, &metadataBy); err != nil {
		t.Fatal(err)
	}
	if err := ws.DB().QueryRow(`SELECT updated_at FROM cards WHERE id=?`, cardID).Scan(&cardAt); err != nil {
		t.Fatal(err)
	}
	beforeEvents, beforeObservations, beforeReplays := checkpointCounts(t, ws.DB())
	first, err := store.PutWorkCheckpoint(ctx, "checkpoint-owner", workString(work["ref"]), 0, content)
	if err != nil {
		t.Fatal(err)
	}
	if first["version"] != int64(1) || first["unchanged"] != false {
		t.Fatalf("unexpected initial checkpoint result: %#v", first)
	}
	duplicate, err := store.PutWorkCheckpoint(ctx, "checkpoint-owner", cardID, 0, content)
	if err != nil || duplicate["unchanged"] != true || duplicate["version"] != int64(1) {
		t.Fatalf("same actor/content should deduplicate before CAS: %#v %v", duplicate, err)
	}
	if _, err = store.PutWorkCheckpoint(ctx, "checkpoint-owner", cardID, 0, map[string]any{"summary": "Changed", "next_action": "Retry"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale checkpoint version accepted: %v", err)
	}
	if _, err = store.PutWorkCheckpoint(ctx, "different-reporter", cardID, 1, content); err != nil {
		t.Fatalf("identical content from a different actor is still an attributed write: %v", err)
	}
	current, err := store.GetWork(ctx, cardID)
	if err != nil {
		t.Fatal(err)
	}
	if checkpointEnvelope(t, current)["reporter"] != "different-reporter" || checkpointEnvelope(t, current)["version"] != float64(2) {
		t.Fatalf("wrong current checkpoint: %#v", current["checkpoint"])
	}
	var afterVersion int64
	var afterAt, afterBy, afterCardAt string
	if err = ws.DB().QueryRow(`SELECT version,updated_at,updated_by FROM work_metadata WHERE card_id=?`, cardID).Scan(&afterVersion, &afterAt, &afterBy); err != nil {
		t.Fatal(err)
	}
	if err = ws.DB().QueryRow(`SELECT updated_at FROM cards WHERE id=?`, cardID).Scan(&afterCardAt); err != nil {
		t.Fatal(err)
	}
	if afterVersion != workVersion || afterAt != metadataAt || afterBy != metadataBy || afterCardAt != cardAt {
		t.Fatalf("checkpoint changed progress/activity fields: work=%d/%s/%s card=%s", afterVersion, afterAt, afterBy, afterCardAt)
	}
	afterEvents, afterObservations, afterReplays := checkpointCounts(t, ws.DB())
	if afterEvents != beforeEvents || afterObservations != beforeObservations || afterReplays != beforeReplays {
		t.Fatalf("checkpoint appended audit/replay history: events %d->%d observations %d->%d replays %d->%d", beforeEvents, afterEvents, beforeObservations, afterObservations, beforeReplays, afterReplays)
	}

	root := ws.Layout().RootDir
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := storage.InitializeWorkspace(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	restarted := NewTestStore(reopened.DB(), reopened.Layout().ArtifactContentDir)
	persisted, err := restarted.GetWork(ctx, cardID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(checkpointEnvelope(t, persisted), checkpointEnvelope(t, current)) {
		t.Fatalf("checkpoint did not survive workspace restart: %#v != %#v", persisted["checkpoint"], current["checkpoint"])
	}
}

func TestPutWorkCheckpointBoundsAndReservedInput(t *testing.T) {
	ctx := context.Background()
	_, store, work := checkpointTestFixture(t)
	cardID := workString(work["id"])
	spoofed, err := store.CreateWork(ctx, "checkpoint-owner", "", map[string]any{
		"title":               "Ignore client checkpoint",
		recoveryCheckpointKey: map[string]any{"version": 999, "reporter": "spoofed"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := spoofed["checkpoint"]; ok {
		t.Fatalf("CreateWork accepted the reserved checkpoint key: %#v", spoofed["checkpoint"])
	}

	blockers := make([]string, 16)
	for i := range blockers {
		blockers[i] = strings.Repeat("b", 512)
	}
	artifacts := make([]any, 16)
	for i := range artifacts {
		artifacts[i] = map[string]any{"ref": strings.Repeat("r", 1024), "portability": "portable"}
	}
	oversized := map[string]any{"summary": "Summary", "next_action": "Continue", "blockers": blockers, "artifacts": artifacts}
	if _, err = store.PutWorkCheckpoint(ctx, "checkpoint-owner", cardID, 0, oversized); !errors.Is(err, ErrInvalidWorkRequest) {
		t.Fatalf("oversized checkpoint accepted: %v", err)
	}
	if got, err := store.GetWork(ctx, cardID); err != nil {
		t.Fatal(err)
	} else if _, exists := got["checkpoint"]; exists {
		t.Fatalf("oversized checkpoint changed durable state: %#v", got["checkpoint"])
	}
	for name, invalid := range map[string]map[string]any{
		"unknown field":          {"summary": "s", "next_action": "n", "transcript": "raw"},
		"unknown artifact field": {"summary": "s", "next_action": "n", "artifacts": []any{map[string]any{"ref": "file:x", "portability": "portable", "path": "/private"}}},
		"missing local host":     {"summary": "s", "next_action": "n", "artifacts": []any{map[string]any{"ref": "file:x", "portability": "local_only"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := store.PutWorkCheckpoint(ctx, "checkpoint-owner", cardID, 0, invalid); !errors.Is(err, ErrInvalidWorkRequest) {
				t.Fatalf("invalid checkpoint accepted: %v", err)
			}
		})
	}
}

func TestPutWorkCheckpointCreatesLegacyMetadataWithoutActivityBump(t *testing.T) {
	ctx := context.Background()
	ws, store, work := checkpointTestFixture(t)
	cardID := workString(work["id"])
	var cardAt, cardBy string
	if err := ws.DB().QueryRow(`SELECT updated_at,updated_by FROM cards WHERE id=?`, cardID).Scan(&cardAt, &cardBy); err != nil {
		t.Fatal(err)
	}
	if _, err := ws.DB().Exec(`DELETE FROM work_metadata WHERE card_id=?`, cardID); err != nil {
		t.Fatal(err)
	}
	before, err := store.GetWork(ctx, cardID)
	if err != nil {
		t.Fatal(err)
	}
	if before["version"] != int64(0) {
		t.Fatalf("legacy metadata fallback version: %#v", before["version"])
	}
	if _, err = store.PutWorkCheckpoint(ctx, "checkpoint-owner", cardID, 0, map[string]any{"summary": "Recovered legacy work", "next_action": "Continue review"}); err != nil {
		t.Fatal(err)
	}
	var version int64
	var updatedAt, updatedBy string
	if err = ws.DB().QueryRow(`SELECT version,updated_at,updated_by FROM work_metadata WHERE card_id=?`, cardID).Scan(&version, &updatedAt, &updatedBy); err != nil {
		t.Fatal(err)
	}
	if version != 0 || updatedAt != cardAt || updatedBy != cardBy {
		t.Fatalf("legacy checkpoint created false activity: version=%d at=%q by=%q want card at=%q by=%q", version, updatedAt, updatedBy, cardAt, cardBy)
	}
	after, err := store.GetWork(ctx, cardID)
	if err != nil {
		t.Fatal(err)
	}
	if WorkDecisionRevision(before) != WorkDecisionRevision(after) {
		t.Fatalf("checkpoint changed decision revision: %s -> %s", WorkDecisionRevision(before), WorkDecisionRevision(after))
	}
}

func TestPatchWorkPreservesCheckpointWrittenAfterItsSnapshot(t *testing.T) {
	ctx := context.Background()
	_, store, work := checkpointTestFixture(t)
	cardID := workString(work["id"])
	originalBegin := mutationTxBegin
	fired := false
	mutationTxBegin = func(ctx context.Context, db *accessDB) (*accessTx, error) {
		if !fired {
			fired = true
			mutationTxBegin = originalBegin
			_, err := store.PutWorkCheckpoint(ctx, "checkpoint-owner", cardID, 0, map[string]any{"summary": "Concurrent checkpoint", "next_action": "Keep writing"})
			if err != nil {
				return nil, err
			}
		}
		return originalBegin(ctx, db)
	}
	defer func() { mutationTxBegin = originalBegin }()
	updated, err := store.PatchWork(ctx, "checkpoint-owner", cardID, 1, map[string]any{"next_action": "Review annotations"})
	if err != nil {
		t.Fatal(err)
	}
	if workString(updated["next_action"]) != "Review annotations" {
		t.Fatalf("annotation patch not applied: %#v", updated)
	}
	checkpoint := checkpointEnvelope(t, updated)
	if checkpoint["version"] != float64(1) || checkpoint["reporter"] != "checkpoint-owner" {
		t.Fatalf("PatchWork erased the concurrent checkpoint: %#v", checkpoint)
	}
}

func TestCheckpointReferencesInheritCardPrivacy(t *testing.T) {
	ctx := context.Background()
	ws, store, _ := checkpointTestFixture(t)
	privateBoard, err := store.CreateBoard(ctx, "checkpoint-owner", map[string]any{"title": "Private board"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.PatchThread(ctx, "checkpoint-owner", workString(privateBoard["thread_id"]), map[string]any{"pm_actor_id": "checkpoint-owner"}, nil); err != nil {
		t.Fatal(err)
	}
	private, err := store.CreateWork(ctx, "checkpoint-owner", workString(privateBoard["id"]), map[string]any{"title": "Private referenced work"})
	if err != nil {
		t.Fatal(err)
	}
	publicBoard, err := store.CreateBoard(ctx, "checkpoint-owner", map[string]any{"title": "Shared board"})
	if err != nil {
		t.Fatal(err)
	}
	public, err := store.CreateWork(ctx, "checkpoint-owner", workString(publicBoard["id"]), map[string]any{"title": "Public work"})
	if err != nil {
		t.Fatal(err)
	}
	reader := WithAccessScope(ctx, AccessScope{ActorID: "checkpoint-stranger"})
	if _, err = store.GetWork(reader, workString(public["id"])); err != nil {
		t.Fatalf("fixture card should start visible: %v", err)
	}
	content := map[string]any{"summary": "Depends on " + workString(private["ref"]), "next_action": "Ask the owner"}
	if _, err = store.PutWorkCheckpoint(WithAccessScope(ctx, AccessScope{ActorID: "checkpoint-owner"}), "checkpoint-owner", workString(public["id"]), 0, content); err != nil {
		t.Fatal(err)
	}
	if _, err = store.GetWork(reader, workString(public["id"])); !errors.Is(err, ErrNotFound) {
		t.Fatalf("checkpoint reference did not inherit metadata privacy: %v", err)
	}
	var saved string
	if err = ws.DB().QueryRow(`SELECT metadata_json FROM work_metadata WHERE card_id=?`, public["id"]).Scan(&saved); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(saved, workString(private["ref"])) {
		t.Fatalf("test checkpoint reference missing from metadata: %s (%s)", saved, fmt.Sprint(private["ref"]))
	}
}

func TestPutWorkCheckpointConcurrentWriters(t *testing.T) {
	_, store, work := checkpointTestFixture(t)
	ready := make(chan struct{})
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			<-ready
			_, err := store.PutWorkCheckpoint(context.Background(), fmt.Sprintf("writer-%d", i), workString(work["ref"]), 0, map[string]any{"summary": fmt.Sprintf("Draft %d", i), "next_action": "Review"})
			results <- err
		}(i)
	}
	close(ready)
	success, conflicts := 0, 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			success++
		} else if errors.Is(err, ErrConflict) {
			conflicts++
		} else {
			t.Errorf("unexpected concurrent write result: %v", err)
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d", success, conflicts)
	}
	current, err := store.GetWork(context.Background(), workString(work["ref"]))
	if err != nil {
		t.Fatal(err)
	}
	if checkpointEnvelope(t, current)["version"] != float64(1) {
		t.Fatal("concurrent writers both advanced checkpoint")
	}
}

func TestWorkCheckpointExcludedFromBulkProjection(t *testing.T) {
	card := map[string]any{"ref": "card:research", "column_key": "ready"}
	metadata := map[string]any{"source": map[string]any{"authority": "nexus"}, recoveryCheckpointKey: map[string]any{"content": "large recovery report"}}
	got := projectWork(card, metadata, 1, nil, nil, map[string]any{})
	if _, ok := got["checkpoint"]; ok {
		t.Fatal("bulk projection must not copy recovery content into every list item")
	}
}
