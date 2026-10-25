package primitives

import (
	"agent-nexus-core/internal/contentusage"
	"context"
	"encoding/json"
	"time"
)

type canonicalUsageSnapshot struct {
	revision, schema int64
	usage            workspaceUsage
}

// Cache canonical metrics only. Scoped readers always execute their own policy;
// file allocation remains a fresh physical observation on every response.
func (s *Store) workspaceUsageForSummary(ctx context.Context) (workspaceUsage, error) {
	if _, scoped := accessScopeFrom(ctx); scoped {
		return s.currentWorkspaceUsage(ctx, true)
	}
	s.canonicalUsageMu.Lock()
	defer s.canonicalUsageMu.Unlock()
	// Bootstrap the constant-size blob totals before capturing its dirty revision.
	if s.canonicalUsageCache == nil {
		if err := s.ensureBlobUsageLedgerInitialized(ctx); err != nil {
			return workspaceUsage{}, err
		}
	}
	var revision, schema int64
	err := s.db.QueryRowContext(ctx, `PRAGMA schema_version`).Scan(&schema)
	if err != nil {
		return workspaceUsage{}, err
	}
	if err = s.db.QueryRowContext(ctx, `SELECT revision FROM background_usage_revision WHERE singleton=1`).Scan(&revision); err != nil {
		return s.currentWorkspaceUsage(ctx, true)
	}
	if c := s.canonicalUsageCache; c != nil && c.schema == schema && c.revision == revision {
		usage := c.usage
		usage.dbBytes, err = s.databaseUsageBytes()
		return usage, err
	}
	// A late optional module without tracking must never freeze old totals.
	names, _ := json.Marshal(contentusage.TrackingTables())
	var incomplete bool
	err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM json_each(?) t JOIN sqlite_schema s ON s.type='table' AND s.name=t.value WHERE NOT EXISTS(SELECT 1 FROM sqlite_schema WHERE type='trigger' AND name='background_usage_'||t.value||'_insert') OR NOT EXISTS(SELECT 1 FROM sqlite_schema WHERE type='trigger' AND name='background_usage_'||t.value||'_update') OR NOT EXISTS(SELECT 1 FROM sqlite_schema WHERE type='trigger' AND name='background_usage_'||t.value||'_delete'))`, string(names)).Scan(&incomplete)
	if err != nil {
		return workspaceUsage{}, err
	}
	// A dirty canonical snapshot must not capture a still-valid TTL byte sample.
	s.contentUsageMu.Lock()
	s.contentUsageAt = time.Time{}
	s.contentUsageMu.Unlock()
	usage, err := s.currentWorkspaceUsage(ctx, true)
	if err != nil {
		return usage, err
	}
	var after int64
	if err = s.db.QueryRowContext(ctx, `SELECT revision FROM background_usage_revision WHERE singleton=1`).Scan(&after); err != nil {
		return workspaceUsage{}, err
	}
	if !incomplete && after == revision {
		s.canonicalUsageCache = &canonicalUsageSnapshot{revision: revision, schema: schema, usage: usage}
	} else {
		s.canonicalUsageCache = nil
	}
	return usage, nil
}
