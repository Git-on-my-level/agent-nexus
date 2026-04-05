package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

type RefEdge struct {
	SourceType string
	SourceID   string
	TargetType string
	TargetID   string
	EdgeType   string
	Metadata   map[string]any
}

func (s *Store) ListRefEdgesBySource(ctx context.Context, sourceType, sourceID string) ([]RefEdge, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}

	sourceType = strings.TrimSpace(sourceType)
	sourceID = strings.TrimSpace(sourceID)
	if sourceType == "" || sourceID == "" {
		return []RefEdge{}, nil
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT source_type, source_id, target_type, target_id, edge_type, metadata_json
		   FROM ref_edges
		  WHERE source_type = ?
		    AND source_id = ?
		  ORDER BY edge_type ASC, target_type ASC, target_id ASC`,
		sourceType,
		sourceID,
	)
	if err != nil {
		return nil, fmt.Errorf("query ref edges by source: %w", err)
	}
	defer rows.Close()

	out := make([]RefEdge, 0)
	for rows.Next() {
		var (
			edge         RefEdge
			metadataJSON sql.NullString
		)
		if err := rows.Scan(
			&edge.SourceType,
			&edge.SourceID,
			&edge.TargetType,
			&edge.TargetID,
			&edge.EdgeType,
			&metadataJSON,
		); err != nil {
			return nil, fmt.Errorf("scan ref edge by source: %w", err)
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
		return nil, fmt.Errorf("iterate ref edges by source: %w", err)
	}
	return out, nil
}

func (s *Store) ListRefEdgesByTarget(ctx context.Context, targetType, targetID string) ([]RefEdge, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("primitives store database is not initialized")
	}

	targetType = strings.TrimSpace(targetType)
	targetID = strings.TrimSpace(targetID)
	if targetType == "" || targetID == "" {
		return []RefEdge{}, nil
	}

	rows, err := s.db.QueryContext(
		ctx,
		`SELECT source_type, source_id, target_type, target_id, edge_type, metadata_json
		   FROM ref_edges
		  WHERE target_type = ?
		    AND target_id = ?
		  ORDER BY source_type ASC, source_id ASC, edge_type ASC`,
		targetType,
		targetID,
	)
	if err != nil {
		return nil, fmt.Errorf("query ref edges by target: %w", err)
	}
	defer rows.Close()

	out := make([]RefEdge, 0)
	for rows.Next() {
		var (
			edge         RefEdge
			metadataJSON sql.NullString
		)
		if err := rows.Scan(
			&edge.SourceType,
			&edge.SourceID,
			&edge.TargetType,
			&edge.TargetID,
			&edge.EdgeType,
			&metadataJSON,
		); err != nil {
			return nil, fmt.Errorf("scan ref edge by target: %w", err)
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
		return nil, fmt.Errorf("iterate ref edges by target: %w", err)
	}
	return out, nil
}
