package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

type RefEdge struct {
	SourceRef    string
	TargetRef    string
	Relation     string
	DiscoveredAt string
	Metadata     map[string]any
}

func (s *Store) ListRefEdgesBySource(ctx context.Context, sourceRef string, relationFilter string) ([]RefEdge, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}

	sourceRef = strings.TrimSpace(sourceRef)
	if sourceRef == "" {
		return []RefEdge{}, nil
	}

	sourceType, sourceID, ok := splitTypedRef(sourceRef)
	if !ok {
		return nil, fmt.Errorf("invalid source_ref %q: must be typed-ref format", sourceRef)
	}

	query := `SELECT source_type, source_id, target_type, target_id, edge_type, created_at, metadata_json
		   FROM ref_edges
		  WHERE source_type = ?
		    AND source_id = ?`
	args := []any{sourceType, sourceID}

	if relationFilter != "" {
		query += ` AND edge_type = ?`
		args = append(args, strings.TrimSpace(relationFilter))
	}

	query += ` ORDER BY edge_type ASC, target_type ASC, target_id ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query ref edges by source: %w", err)
	}
	defer rows.Close()

	return scanRefEdges(rows)
}

func (s *Store) ListRefEdgesByTarget(ctx context.Context, targetRef string, relationFilter string) ([]RefEdge, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}

	targetRef = strings.TrimSpace(targetRef)
	if targetRef == "" {
		return []RefEdge{}, nil
	}

	targetType, targetID, ok := splitTypedRef(targetRef)
	if !ok {
		return nil, fmt.Errorf("invalid target_ref %q: must be typed-ref format", targetRef)
	}

	query := `SELECT source_type, source_id, target_type, target_id, edge_type, created_at, metadata_json
		   FROM ref_edges
		  WHERE target_type = ?
		    AND target_id = ?`
	args := []any{targetType, targetID}

	if relationFilter != "" {
		query += ` AND edge_type = ?`
		args = append(args, strings.TrimSpace(relationFilter))
	}

	query += ` ORDER BY source_type ASC, source_id ASC, edge_type ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query ref edges by target: %w", err)
	}
	defer rows.Close()

	return scanRefEdges(rows)
}

func scanRefEdges(rows *sql.Rows) ([]RefEdge, error) {
	out := make([]RefEdge, 0)
	for rows.Next() {
		var (
			sourceType   string
			sourceID     string
			targetType   string
			targetID     string
			edgeType     string
			createdAt    string
			metadataJSON sql.NullString
		)
		if err := rows.Scan(
			&sourceType,
			&sourceID,
			&targetType,
			&targetID,
			&edgeType,
			&createdAt,
			&metadataJSON,
		); err != nil {
			return nil, fmt.Errorf("scan ref edge: %w", err)
		}

		edge := RefEdge{
			SourceRef:    makeTypedRef(sourceType, sourceID),
			TargetRef:    makeTypedRef(targetType, targetID),
			Relation:     edgeType,
			DiscoveredAt: createdAt,
		}
		if strings.TrimSpace(metadataJSON.String) != "" {
			edge.Metadata = map[string]any{}
			if err := json.Unmarshal([]byte(metadataJSON.String), &edge.Metadata); err != nil {
				return nil, fmt.Errorf("decode ref edge metadata: %w", err)
			}
		}
		out = append(out, edge)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ref edges: %w", err)
	}
	return out, nil
}
