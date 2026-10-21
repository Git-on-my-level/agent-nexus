package primitives

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"agent-nexus-core/internal/handles"
)

// revisionRefKinds maps a revision ref prefix to its parent resource kind and
// the tables that hold it. Table and column names are constants, never input.
type revisionRefKind struct {
	parentKind, revisions, parentColumn, parents, parentPath string
}

var revisionRefKinds = map[string]revisionRefKind{
	"document_revision": {"document", "document_revisions", "document_id", "documents", "docs"},
	"card_revision":     {"card", "card_revisions", "card_id", "cards", "tasks"},
}

type revisionLookup struct {
	value, parent string
	number        int
}

type revisionHit struct {
	parentID, revisionID string
	number               int
}

// splitRevisionHandle parses the public `<parent-handle>-r<N>` revision handle.
func splitRevisionHandle(value string) (string, int, bool) {
	at := strings.LastIndex(value, "-r")
	if at <= 0 || at+2 >= len(value) {
		return "", 0, false
	}
	number, err := strconv.Atoi(value[at+2:])
	if err != nil || number < 1 || strconv.Itoa(number) != value[at+2:] {
		return "", 0, false
	}
	return value[:at], number, true
}

// resolveRevisionPreviews resolves `document_revision:` and `card_revision:`
// refs in place for the batch ref resolve read. A revision resolves when its
// parent document or card resolves for this reader; it then carries the
// parent's title, a `parent_ref`, and a workspace-relative URL that opens the
// parent at that revision.
//
// Cost: at most two statements per revision kind for the whole batch (one by
// revision primary key, one by (parent handle or id, revision number) through
// the parent's handle/primary key and the revision UNIQUE(parent,
// revision_number) index), plus one parent projection batch. Nothing scales
// with workspace size.
func resolveRevisionPreviews(ctx context.Context, q briefQueryer, refs []string, out []RefPreview, visible func(string, string) bool) error {
	for prefix, spec := range revisionRefKinds {
		var ids []string
		var pairs []map[string]any
		for _, ref := range refs {
			kind, value, ok := strings.Cut(ref, ":")
			value = strings.TrimSpace(value)
			if !ok || kind != prefix || value == "" {
				continue
			}
			ids = append(ids, value)
			if parent, number, ok := splitRevisionHandle(value); ok {
				pairs = append(pairs, map[string]any{"k": value, "p": parent, "h": handles.Normalize(parent), "n": number})
			}
		}
		if len(ids) == 0 {
			continue
		}
		hits := map[string]revisionHit{}
		encodedIDs, err := json.Marshal(uniqueSortedStrings(ids))
		if err != nil {
			return err
		}
		byID := revisionByIDQuery(spec)
		if err := scanRevisionHits(ctx, q, byID, hits, string(encodedIDs)); err != nil {
			return err
		}
		if len(pairs) > 0 {
			encodedPairs, err := json.Marshal(pairs)
			if err != nil {
				return err
			}
			byHandle := revisionByHandleQuery(spec)
			if err := scanRevisionHits(ctx, q, byHandle, hits, string(encodedPairs)); err != nil {
				return err
			}
		}
		if len(hits) == 0 {
			continue
		}
		parentRefs := []string{}
		seen := map[string]bool{}
		for _, hit := range hits {
			ref := spec.parentKind + ":" + hit.parentID
			if !seen[ref] {
				seen[ref] = true
				parentRefs = append(parentRefs, ref)
			}
		}
		parents, err := readRefProjection(ctx, q, parentRefs, visible, true)
		if err != nil {
			return err
		}
		byParent := map[string]RefPreview{}
		for i, parent := range parents {
			if parent.Resolvable {
				byParent[parentRefs[i]] = parent
			}
		}
		for i, ref := range refs {
			kind, value, _ := strings.Cut(ref, ":")
			if kind != prefix {
				continue
			}
			hit, ok := hits[strings.TrimSpace(value)]
			if !ok {
				continue
			}
			parent, ok := byParent[spec.parentKind+":"+hit.parentID]
			if !ok {
				continue
			}
			parentSegment := strings.TrimPrefix(parent.URL, "/"+spec.parentPath+"/")
			if parentSegment == parent.URL || parentSegment == "" {
				parentSegment = url.PathEscape(hit.parentID)
			}
			parentHandle, _ := url.PathUnescape(parentSegment)
			preview := RefPreview{
				Ref:        ref,
				ID:         hit.revisionID,
				Kind:       prefix,
				Title:      parent.Title,
				ParentRef:  spec.parentKind + ":" + parentHandle,
				Resolvable: true,
				URL:        "/" + spec.parentPath + "/" + parentSegment,
			}
			if prefix == "document_revision" {
				preview.URL += "?revision=" + url.QueryEscape(revisionHandle(parentHandle, hit.number))
			}
			out[i] = preview
		}
	}
	return nil
}

func scanRevisionHits(ctx context.Context, q briefQueryer, query string, hits map[string]revisionHit, arg string) error {
	rows, err := q.QueryContext(ctx, query, arg)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var hit revisionHit
		if err := rows.Scan(&key, &hit.parentID, &hit.number, &hit.revisionID); err != nil {
			return err
		}
		if _, exists := hits[key]; !exists {
			hits[key] = hit
		}
	}
	return rows.Err()
}

func revisionByIDQuery(spec revisionRefKind) string {
	return `SELECT r.revision_id, r.` + spec.parentColumn + `, r.revision_number, r.revision_id
		FROM json_each(?) j JOIN ` + spec.revisions + ` r ON r.revision_id = j.value`
}

// revisionByHandleQuery matches `<parent>-r<N>` through the parent's handle or
// primary key and the revision UNIQUE(parent, revision_number) index.
func revisionByHandleQuery(spec revisionRefKind) string {
	return `WITH wanted AS (
		SELECT json_extract(j.value,'$.k') AS k, json_extract(j.value,'$.p') AS p, json_extract(j.value,'$.h') AS h, json_extract(j.value,'$.n') AS n FROM json_each(?) j
	), matched AS (
		SELECT w.k, w.n, COALESCE(
			(SELECT p.id FROM ` + spec.parents + ` p WHERE p.handle IS NOT NULL AND trim(p.handle) <> '' AND p.handle = w.h),
			(SELECT p.id FROM ` + spec.parents + ` p WHERE p.id = w.p)
		) AS id FROM wanted w
	) SELECT matched.k, r.` + spec.parentColumn + `, r.revision_number, r.revision_id
		FROM matched CROSS JOIN ` + spec.revisions + ` r
		WHERE r.` + spec.parentColumn + ` = matched.id AND r.revision_number = matched.n`
}
