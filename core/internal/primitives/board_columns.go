// Board column schema: canonical column keys and titles, schema
// normalization for create and patch, and column-key validation and ordering.

package primitives

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"

	contractenums "agent-nexus-contracts-go-client/enums"
)

var canonicalBoardColumnOrder = contractenums.BoardColumnKeyValues

var canonicalBoardColumnTitles = map[string]string{
	"backlog":     "Backlog",
	"ready":       "Ready",
	"in_progress": "In Progress",
	"blocked":     "Blocked",
	"review":      "Review",
	"done":        "Done",
}

const (
	boardDefaultColumn = "backlog"
	boardRankWidth     = 19
	boardRankStep      = uint64(1024)
)

func canonicalBoardCardRisk(raw string) string {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "medium", "high", "critical":
		return strings.TrimSpace(strings.ToLower(raw))
	default:
		return "low"
	}
}

func normalizeBoardColumnSchema(raw any, allowDefault bool) ([]map[string]any, error) {
	if raw == nil {
		if allowDefault {
			return defaultBoardColumnSchema(), nil
		}
		return nil, fmt.Errorf("board.column_schema is required")
	}

	items, ok := raw.([]any)
	if !ok {
		switch typed := raw.(type) {
		case []map[string]any:
			items = make([]any, 0, len(typed))
			for _, item := range typed {
				items = append(items, item)
			}
		default:
			return nil, fmt.Errorf("board.column_schema must be a list of objects")
		}
	}
	if len(items) != len(canonicalBoardColumnOrder) {
		return nil, fmt.Errorf("board.column_schema must contain the six canonical columns in order")
	}

	out := make([]map[string]any, 0, len(items))
	for i, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("board.column_schema must contain only objects")
		}
		expectedKey := canonicalBoardColumnOrder[i]
		key := strings.TrimSpace(anyStringValue(item["key"]))
		if key != expectedKey {
			return nil, fmt.Errorf("board.column_schema must preserve canonical key order")
		}
		title := strings.TrimSpace(anyStringValue(item["title"]))
		if title == "" {
			return nil, fmt.Errorf("board.column_schema[%d].title is required", i)
		}

		var wipLimit any
		if rawWIP, exists := item["wip_limit"]; exists && rawWIP != nil {
			limit, err := normalizeBoardWIPLimit(rawWIP)
			if err != nil {
				return nil, fmt.Errorf("board.column_schema[%d].wip_limit: %w", i, err)
			}
			wipLimit = limit
		} else {
			wipLimit = nil
		}

		out = append(out, map[string]any{
			"key":       key,
			"title":     title,
			"wip_limit": wipLimit,
		})
	}

	return out, nil
}

func normalizeBoardWIPLimit(raw any) (int, error) {
	switch value := raw.(type) {
	case int:
		if value < 0 {
			return 0, fmt.Errorf("must be non-negative")
		}
		return value, nil
	case int64:
		if value < 0 {
			return 0, fmt.Errorf("must be non-negative")
		}
		return int(value), nil
	case float64:
		if value < 0 || value != math.Trunc(value) {
			return 0, fmt.Errorf("must be a non-negative integer")
		}
		return int(value), nil
	default:
		return 0, fmt.Errorf("must be an integer")
	}
}

func defaultBoardColumnSchema() []map[string]any {
	out := make([]map[string]any, 0, len(canonicalBoardColumnOrder))
	for _, key := range canonicalBoardColumnOrder {
		out = append(out, map[string]any{
			"key":       key,
			"title":     canonicalBoardColumnTitles[key],
			"wip_limit": nil,
		})
	}
	return out
}

func decodeBoardColumnSchema(raw string) ([]map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return defaultBoardColumnSchema(), nil
	}
	var items []map[string]any
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("decode board column schema: %w", err)
	}
	if len(items) == 0 {
		return defaultBoardColumnSchema(), nil
	}
	return items, nil
}

func ensureBoardColumnAllowed(board boardRow, columnKey string) error {
	columns, err := decodeBoardColumnSchema(board.ColumnSchemaJSON)
	if err != nil {
		return err
	}
	for _, column := range columns {
		if strings.TrimSpace(anyStringValue(column["key"])) == columnKey {
			return nil
		}
	}
	return invalidBoardRequest("column_key is not on this board")
}

func validateBoardColumnKey(columnKey string) error {
	columnKey = strings.TrimSpace(columnKey)
	for _, allowed := range canonicalBoardColumnOrder {
		if columnKey == allowed {
			return nil
		}
	}
	return fmt.Errorf("column_key must be one of: %s", strings.Join(canonicalBoardColumnOrder, ", "))
}

func boardColumnOrderSQL(columnExpr string) string {
	return `CASE ` + columnExpr + `
		WHEN 'backlog' THEN 0
		WHEN 'ready' THEN 1
		WHEN 'in_progress' THEN 2
		WHEN 'blocked' THEN 3
		WHEN 'review' THEN 4
		WHEN 'done' THEN 5
		ELSE 6
	END`
}
