package primitives

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
	"strings"

	"agent-nexus-core/internal/handles"
)

type storedEventRow struct {
	id, handle, kind, ts, actor, refs, payload                        string
	thread, archivedAt, archivedBy, trashedAt, trashedBy, trashReason sql.NullString
}
type publicRefCacheKey struct{}

type publicRefQuery interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// Legacy presentation helpers cannot return query errors. A failed batch falls
// back to the existing scoped per-ref path, which never exposes hidden handles.
func batchPublicRefValue(ctx context.Context, q queryRower, value any) context.Context {
	batch, ok := q.(publicRefQuery)
	if !ok {
		return ctx
	}
	next, err := withBatchPublicRefs(ctx, batch, []any{value})
	if err != nil {
		return ctx
	}
	return next
}

// This page-local map caches presentation handles only. Every batch uses the
// canonical scoped relation; selectors and authorization never consult this map.
func withBatchPublicRefs(ctx context.Context, q interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, values []any) (context.Context, error) {
	grouped := map[string][]string{}
	refs := map[string]string{}
	cached, _ := ctx.Value(publicRefCacheKey{}).(map[string]string)
	var visit func(string, any)
	var refValue func(any)
	refValue = func(value any) {
		switch v := value.(type) {
		case string:
			kind, id, ok := normalizeTypedRef(v)
			if !ok || id == "" || (resourceTables[kind] == "" && kind != "document_revision" && kind != "card_revision") {
				return
			}
			key := makeTypedRef(kind, id)
			if _, seen := cached[key]; seen {
				return
			}
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
	if len(grouped) == 0 {
		return ctx, nil
	}
	// Copy only when this projection adds refs. Cached maps remain immutable.
	for key, value := range cached {
		refs[key] = value
	}
	keys := []string{}
	for kind := range grouped {
		keys = append(keys, kind)
	}
	sort.Strings(keys)
	for _, kind := range keys {
		for start := 0; start < len(grouped[kind]); start += 200 {
			chunk := grouped[kind][start:min(start+200, len(grouped[kind]))]
			ids, _ := json.Marshal(chunk)
			query := `SELECT id,handle FROM ` + resourceTables[kind] + ` WHERE id IN (SELECT value FROM json_each(?))`
			if kind == "document_revision" {
				query = `SELECT r.revision_id,COALESCE(d.handle,d.id,r.document_id)||'-r'||r.revision_number FROM document_revisions r LEFT JOIN documents d ON d.id=r.document_id WHERE r.revision_id IN (SELECT value FROM json_each(?))`
			} else if kind == "card_revision" {
				query = `SELECT r.revision_id,COALESCE(c.handle,c.id)||'-r'||r.revision_number FROM card_revisions r JOIN cards c ON c.id=r.card_id WHERE r.revision_id IN (SELECT value FROM json_each(?))`
			}
			readCtx := eventPageRefScope(ctx, kind, chunk)
			args := []any{string(ids)}
			aliases := map[string][]string{}
			if kind == "document_revision" || kind == "card_revision" {
				// Revision helpers also accept normalized virtual handles. Seek
				// the identity index, then authorize the resulting revisions
				// and parents through their complete scoped relations.
				normalized := []string{}
				handleLookups := []string{}
				for _, input := range chunk {
					value := handles.Normalize(input)
					normalized = append(normalized, value)
					aliases[value] = append(aliases[value], input)
					if isVirtualRevisionHandle(value) {
						handleLookups = append(handleLookups, value)
					}
				}
				data, _ := json.Marshal(normalized)
				query += " OR r.revision_id IN (SELECT resource_id FROM resource_access_identities WHERE kind='" + kind + "' AND ref IN (SELECT value FROM json_each(?)))"
				args = append(args, string(data))
				if len(handleLookups) > 0 {
					// Identity rows are not guaranteed. Match the per-ref
					// computed handle so case-variant and handle-only inputs
					// still resolve when the index has no matching ref.
					parent := "COALESCE(d.handle,d.id,r.document_id)"
					if kind == "card_revision" {
						parent = "COALESCE(c.handle,c.id)"
					}
					handleData, _ := json.Marshal(handleLookups)
					query += " OR lower(" + parent + "||'-r'||r.revision_number) IN (SELECT value FROM json_each(?))"
					args = append(args, string(handleData))
				}
				// A handle routes to a different key: do not reuse the event
				// pager's denial subset indexed by the input strings.
				readCtx = context.WithValue(ctx, eventPageTargetsKey{}, struct{}{})
			}
			rows, err := q.QueryContext(readCtx, query, args...)
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
				for _, input := range aliases[handles.Normalize(value)] {
					refs[makeTypedRef(kind, input)] = makeTypedRef(kind, value)
				}
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return ctx, err
			}
		}
	}
	return context.WithValue(ctx, publicRefCacheKey{}, refs), nil
}

func isVirtualRevisionHandle(normalized string) bool {
	i := strings.LastIndex(normalized, "-r")
	if i <= 0 || i+2 >= len(normalized) {
		return false
	}
	for _, c := range normalized[i+2:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
