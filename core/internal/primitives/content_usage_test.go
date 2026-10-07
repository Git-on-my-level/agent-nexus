package primitives

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"agent-nexus-core/internal/blob"
	"agent-nexus-core/internal/storage"
	"agent-nexus-core/internal/testsql"
)

func TestStorageQuotaExcludesDerivedData(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	newStore := func(quota WorkspaceQuota) *Store {
		return NewStore(ws.DB(), blob.NewFilesystemBackend(ws.Layout().ArtifactContentDir), ws.Layout().ArtifactContentDir,
			WithDatabasePath(ws.Layout().DatabasePath), WithWorkspaceQuota(quota))
	}
	initial, err := newStore(WorkspaceQuota{}).GetWorkspaceUsageSummary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	limit := initial.Usage.StorageBytes + 64*1024
	// Several MiB of rebuildable authorization data, plus a secondary index.
	tx, err := ws.DB().BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	for i := 0; i < 256; i++ {
		_, err = tx.ExecContext(ctx, `INSERT INTO resource_access_external_edges(source_kind,source_id,target_key) VALUES('event',?,?)`, strings.Repeat("x", 8192)+fmt.Sprint(i), fmt.Sprintf("%064x", i))
		if err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// Use a fresh Store so exclusion cannot be an artifact of a warm cache.
	s := newStore(WorkspaceQuota{MaxBlobBytes: limit})
	summary, err := s.GetWorkspaceUsageSummary(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Usage.StorageBytes != initial.Usage.StorageBytes {
		t.Fatalf("derived data charged: before=%d after=%d", initial.Usage.StorageBytes, summary.Usage.StorageBytes)
	}
	if summary.Usage.DatabaseBytes <= limit {
		t.Fatalf("fixture must exceed physical cap: %#v", summary)
	}
	if _, err = s.AppendEvent(ctx, "actor", map[string]any{"type": "message_posted", "refs": []string{}, "payload": map[string]any{"text": "still writable"}}); err != nil {
		t.Fatalf("small write with large derived tables: %v", err)
	}
	_, err = s.AppendEvent(ctx, "actor", map[string]any{"type": "message_posted", "refs": []string{}, "payload": map[string]any{"text": strings.Repeat("user content", int(limit))}})
	var violation *QuotaViolation
	if !errors.As(err, &violation) || violation.Metric != "storage_bytes" {
		t.Fatalf("large canonical content must still hit quota: %v", err)
	}
}

func TestContentUsageCacheAndReaderScope(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	counted, counter := testsql.Open("file:" + ws.Layout().DatabasePath)
	defer counted.Close()
	s := NewStore(counted, blob.NewFilesystemBackend(ws.Layout().ArtifactContentDir), ws.Layout().ArtifactContentDir, WithDatabasePath(ws.Layout().DatabasePath))
	before, err := s.databaseContentUsageBytes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CreateThread(ctx, "owner", map[string]any{"title": "private content", "pm_actor_id": "owner", "summary": strings.Repeat("secret", 1024)})
	if err != nil {
		t.Fatal(err)
	}
	counter.Reset()
	cached, err := s.databaseContentUsageBytes(ctx)
	if err != nil || cached != before {
		t.Fatalf("expected cached measurement: %d %d %v", before, cached, err)
	}
	if counter.Count() != 0 {
		t.Fatalf("warm content measurement performed %d SQL reads", counter.Count())
	}
	// The storage-only hot path reads indexed blob totals, without counting
	// every canonical row or rediscovering the schema on each append.
	s.quota.MaxBlobBytes = 1024 * 1024
	if _, err = s.GetWorkspaceUsageSummary(ctx); err != nil {
		t.Fatal(err)
	}
	counter.Reset()
	if err = s.checkWorkspaceWriteQuota(ctx, 0, quotaWriteDelta{dbBytes: 512}, blobLedgerWritePlan{}); err != nil {
		t.Fatal(err)
	}
	if counter.Count() > 2 {
		t.Fatalf("warm storage quota check performed %d reads", counter.Count())
	}
	s.contentUsageAt = time.Now().Add(-contentUsageCacheTTL)
	refreshed, err := s.databaseContentUsageBytes(ctx)
	if err != nil || refreshed <= before {
		t.Fatalf("expected refreshed content: %d %d %v", before, refreshed, err)
	}
	visible, err := s.databaseContentUsageBytes(WithAccessScope(ctx, AccessScope{ActorID: "stranger"}))
	if err != nil {
		t.Fatal(err)
	}
	if visible != before {
		t.Fatalf("private content leaked from canonical cache: before=%d visible=%d canonical=%d", before, visible, refreshed)
	}
}

func TestContentUsageCountsCanonicalRelationsAndWakeups(t *testing.T) {
	ctx := context.Background()
	ws, err := storage.InitializeWorkspace(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	s := NewStore(ws.DB(), blob.NewFilesystemBackend(ws.Layout().ArtifactContentDir), ws.Layout().ArtifactContentDir, WithDatabasePath(ws.Layout().DatabasePath))
	before, err := s.measureDatabaseContentUsageBytes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	metadata := `{"topic_ref_field":"related_refs","note":"` + strings.Repeat("relation content", 1024) + `"}`
	if _, err = ws.DB().ExecContext(ctx, `INSERT INTO ref_edges(id,source_type,source_id,target_type,target_id,edge_type,created_at,metadata_json) VALUES('quota-edge','topic','source','thread','target','related_to','2026-10-07',?)`, metadata); err != nil {
		t.Fatal(err)
	}
	relations, err := s.measureDatabaseContentUsageBytes(ctx)
	if err != nil || relations-before < int64(len(metadata)) {
		t.Fatalf("canonical relation not counted: before=%d after=%d err=%v", before, relations, err)
	}
	reason := strings.Repeat("durable failure reason", 1024)
	if _, err = s.UpsertAgentWakeup(ctx, AgentWakeup{WakeupID: "quota-wake", TargetActorID: "agent", FailureReason: reason}); err != nil {
		t.Fatal(err)
	}
	wakeups, err := s.measureDatabaseContentUsageBytes(ctx)
	if err != nil || wakeups-relations < int64(len(reason)) {
		t.Fatalf("canonical wakeup not counted: before=%d after=%d err=%v", relations, wakeups, err)
	}
}
