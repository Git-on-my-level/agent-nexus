package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"
)

type storedEventRow struct {
	id, handle, kind, ts, actor, refs, payload                        string
	thread, archivedAt, archivedBy, trashedAt, trashedBy, trashReason sql.NullString
}
type publicRefCacheKey struct{}

// This page-local map caches presentation handles only. Every batch uses the
// canonical scoped relation; selectors and authorization never consult this map.
func withBatchPublicRefs(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, values []any) (context.Context, error) {
	grouped := map[string][]string{}
	refs := map[string]string{}
	var visit func(string, any)
	var refValue func(any)
	refValue = func(value any) {
		switch v := value.(type) {
		case string:
			kind, id, ok := normalizeTypedRef(v)
			if !ok || id == "" || resourceTables[kind] == "" {
				return
			}
			key := makeTypedRef(kind, id)
			if _, seen := refs[key]; seen {
				return
			}
			refs[key] = key
			grouped[kind] = append(grouped[kind], id)
		case []any:
			for _, item := range v {
				refValue(item)
			}
		case []string:
			for _, item := range v {
				refValue(item)
			}
		case map[string]any:
			for key, item := range v {
				visit(key, item)
			}
		}
	}
	visit = func(key string, value any) {
		if isPublicRefFieldKey(key) {
			refValue(value)
			return
		}
		switch v := value.(type) {
		case []any:
			for _, item := range v {
				visit("", item)
			}
		case map[string]any:
			for key, item := range v {
				visit(key, item)
			}
		}
	}
	for _, value := range values {
		visit("", value)
	}
	keys := []string{}
	for kind := range grouped {
		keys = append(keys, kind)
	}
	sort.Strings(keys)
	for _, kind := range keys {
		ids, _ := json.Marshal(grouped[kind])
		rows, err := q.QueryContext(ctx, `SELECT id,handle FROM `+resourceTables[kind]+` WHERE id IN (SELECT value FROM json_each(?))`, string(ids))
		if err != nil {
			return ctx, err
		}
		for rows.Next() {
			var id string
			var handle sql.NullString
			if err := rows.Scan(&id, &handle); err != nil {
				rows.Close()
				return ctx, err
			}
			value := strings.TrimSpace(handle.String)
			if value == "" {
				value = id
			}
			refs[makeTypedRef(kind, id)] = makeTypedRef(kind, value)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return ctx, err
		}
	}
	return context.WithValue(ctx, publicRefCacheKey{}, refs), nil
}
