// Artifact list query construction and backing-scope clauses, plus artifact
// metadata scrubbing and public projection.

package primitives

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

func (s *Store) ListArtifacts(ctx context.Context, filter ArtifactListFilter) ([]map[string]any, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}
	filter.States = NormalizeListLifecycleStates(filter.States)

	query, args := buildListArtifactsQuery(filter)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query artifacts: %w", err)
	}
	defer rows.Close()

	artifacts := make([]map[string]any, 0)
	for rows.Next() {
		var artifactID, metadataJSON string
		if err := rows.Scan(&artifactID, &metadataJSON); err != nil {
			return nil, fmt.Errorf("scan artifact row: %w", err)
		}

		metadata, err := decodeArtifactMetadataJSON(metadataJSON)
		if err != nil {
			return nil, err
		}
		publicizeArtifactMetadata(ctx, s.db, metadata, artifactID, "")
		if owner, err := s.loadArtifactOwner(ctx, s.db, artifactID); err != nil {
			return nil, err
		} else if owner.Kind != "" {
			owner.applyPublic(ctx, s.db, metadata)
		}

		artifacts = append(artifacts, metadata)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate artifact rows: %w", err)
	}

	return artifacts, nil
}

func buildListArtifactsQuery(filter ArtifactListFilter) (string, []any) {
	if ids := NormalizeArtifactIDFilter(filter.IDs, 48); len(ids) > 0 {
		query := `SELECT id, metadata_json FROM artifacts WHERE id IN (` + placeholders(len(ids)) + `)`
		query += ` AND ` + LifecycleStatesOrGroup("archived_at", "trashed_at", filter.States)
		query += ` ORDER BY created_at ASC, id ASC`
		args := make([]any, len(ids))
		for i, id := range ids {
			args[i] = id
		}
		return query, args
	}
	q := strings.TrimSpace(filter.Q)
	qPattern := "%" + q + "%"
	backingScope := normalizeBackingScope(filter.BackingScope)

	if threadIDs := artifactThreadFilterIDs(filter); len(threadIDs) > 0 {
		primaryClauses := []string{"thread_id IN (" + placeholders(len(threadIDs)) + ")"}
		secondaryClauses := []string{
			"COALESCE(artifacts.thread_id, '') NOT IN (" + placeholders(len(threadIDs)) + ")",
			"ref_edges.source_type = ?",
			"ref_edges.target_type = ?",
			"ref_edges.target_id IN (" + placeholders(len(threadIDs)) + ")",
			"ref_edges.edge_type = ?",
		}
		primaryArgs := make([]any, 0, len(threadIDs))
		for _, threadID := range threadIDs {
			primaryArgs = append(primaryArgs, threadID)
		}
		secondaryArgs := make([]any, 0, len(threadIDs)*2+3)
		for _, threadID := range threadIDs {
			secondaryArgs = append(secondaryArgs, threadID)
		}
		secondaryArgs = append(secondaryArgs, "artifact", "thread")
		for _, threadID := range threadIDs {
			secondaryArgs = append(secondaryArgs, threadID)
		}
		secondaryArgs = append(secondaryArgs, refEdgeTypeRef)
		lifecyclePrimary := LifecycleStatesOrGroup("archived_at", "trashed_at", filter.States)
		lifecycleSecondary := LifecycleStatesOrGroup("artifacts.archived_at", "artifacts.trashed_at", filter.States)
		primaryClauses = append(primaryClauses, lifecyclePrimary)
		secondaryClauses = append(secondaryClauses, lifecycleSecondary)
		if kind := strings.TrimSpace(filter.Kind); kind != "" {
			primaryClauses = append(primaryClauses, "kind = ?")
			secondaryClauses = append(secondaryClauses, "artifacts.kind = ?")
			primaryArgs = append(primaryArgs, kind)
			secondaryArgs = append(secondaryArgs, kind)
		}
		if createdAfter := strings.TrimSpace(filter.CreatedAfter); createdAfter != "" {
			primaryClauses = append(primaryClauses, "created_at >= ?")
			secondaryClauses = append(secondaryClauses, "artifacts.created_at >= ?")
			primaryArgs = append(primaryArgs, createdAfter)
			secondaryArgs = append(secondaryArgs, createdAfter)
		}
		if createdBefore := strings.TrimSpace(filter.CreatedBefore); createdBefore != "" {
			primaryClauses = append(primaryClauses, "created_at <= ?")
			secondaryClauses = append(secondaryClauses, "artifacts.created_at <= ?")
			primaryArgs = append(primaryArgs, createdBefore)
			secondaryArgs = append(secondaryArgs, createdBefore)
		}
		if q != "" {
			searchClause := "(id LIKE ? OR kind LIKE ? OR COALESCE(json_extract(metadata_json, '$.summary'), '') LIKE ?)"
			primaryClauses = append(primaryClauses, searchClause)
			secondaryClauses = append(secondaryClauses, "(artifacts.id LIKE ? OR artifacts.kind LIKE ? OR COALESCE(json_extract(artifacts.metadata_json, '$.summary'), '') LIKE ?)")
			primaryArgs = append(primaryArgs, qPattern, qPattern, qPattern)
			secondaryArgs = append(secondaryArgs, qPattern, qPattern, qPattern)
		}
		if clause := artifactBackingScopeClause("kind", backingScope); clause != "" {
			primaryClauses = append(primaryClauses, clause)
		}
		if clause := artifactBackingScopeClause("artifacts.kind", backingScope); clause != "" {
			secondaryClauses = append(secondaryClauses, clause)
		}
		innerQuery := `SELECT id, metadata_json, created_at FROM artifacts WHERE ` + strings.Join(primaryClauses, " AND ") + `
			UNION ALL
			SELECT artifacts.id, artifacts.metadata_json, artifacts.created_at
			  FROM ref_edges
			  JOIN artifacts ON artifacts.id = ref_edges.source_id
			 WHERE ` + strings.Join(secondaryClauses, " AND ")
		query := `SELECT id, metadata_json FROM (` + innerQuery + `) ORDER BY created_at ASC, id ASC`
		if filter.Limit != nil && *filter.Limit > 0 {
			query += fmt.Sprintf(` LIMIT %d`, *filter.Limit)
		}
		args := append(primaryArgs, secondaryArgs...)
		return query, args
	}

	query := `SELECT id, metadata_json FROM artifacts WHERE 1=1`
	args := make([]any, 0, 8)
	query += ` AND ` + LifecycleStatesOrGroup("archived_at", "trashed_at", filter.States)
	if kind := strings.TrimSpace(filter.Kind); kind != "" {
		query += ` AND kind = ?`
		args = append(args, kind)
	}
	if clause := artifactBackingScopeClause("kind", backingScope); clause != "" {
		query += ` AND ` + clause
	}
	if createdAfter := strings.TrimSpace(filter.CreatedAfter); createdAfter != "" {
		query += ` AND created_at >= ?`
		args = append(args, createdAfter)
	}
	if createdBefore := strings.TrimSpace(filter.CreatedBefore); createdBefore != "" {
		query += ` AND created_at <= ?`
		args = append(args, createdBefore)
	}
	if q != "" {
		query += ` AND (id LIKE ? OR kind LIKE ? OR COALESCE(json_extract(metadata_json, '$.summary'), '') LIKE ?)`
		args = append(args, qPattern, qPattern, qPattern)
	}
	query += ` ORDER BY created_at ASC, id ASC`
	if filter.Limit != nil && *filter.Limit > 0 {
		query += fmt.Sprintf(` LIMIT %d`, *filter.Limit)
	}
	return query, args
}

func artifactThreadFilterIDs(filter ArtifactListFilter) []string {
	values := make([]string, 0, len(filter.ThreadIDs)+1)
	if threadID := strings.TrimSpace(filter.ThreadID); threadID != "" {
		values = append(values, threadID)
	}
	values = append(values, filter.ThreadIDs...)
	return dedupeStrings(values)
}

func normalizeBackingScope(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return "all"
	}
	return scope
}

func artifactBackingScopeClause(column string, scope string) string {
	switch normalizeBackingScope(scope) {
	case "standalone":
		return column + ` NOT IN ('doc', 'card')`
	case "backing_only":
		return column + ` IN ('doc', 'card')`
	default:
		return ""
	}
}

func scrubArtifactMetadataMap(metadata map[string]any) map[string]any {
	if metadata == nil {
		return nil
	}
	delete(metadata, "content_path")
	return metadata
}

func publicizeArtifactMetadata(ctx context.Context, q queryRower, metadata map[string]any, artifactID, handle string) {
	if metadata == nil {
		return
	}
	artifactID = firstNonEmpty(strings.TrimSpace(artifactID), anyStringValue(metadata["id"]))
	handle = firstNonEmpty(strings.TrimSpace(handle), anyStringValue(metadata["handle"]), artifactID)
	if handle == "" {
		return
	}
	metadata["handle"] = handle
	metadata["ref"] = "artifact:" + handle
	if refs, err := normalizeStringSlice(metadata["refs"]); err == nil {
		metadata["refs"] = publicTypedRefs(ctx, q, refs)
	}
}

func decodeArtifactMetadataJSON(metadataJSON string) (map[string]any, error) {
	var metadata map[string]any
	if err := json.Unmarshal([]byte(metadataJSON), &metadata); err != nil {
		return nil, fmt.Errorf("decode artifact metadata: %w", err)
	}
	return scrubArtifactMetadataMap(metadata), nil
}
