package primitives

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"agent-nexus-core/internal/handles"
	"agent-nexus-core/internal/schema"
)

var ErrInvalidResourceRef = errors.New("invalid resource ref")

type ResourceRefInput struct {
	// Type is used for endpoint-implied refs such as GET /topics/{handle}.
	Type string
	// Ref may be a typed ref (topic:roadmap), an endpoint-implied handle, or an internal UUID.
	Ref string
}

type ResolvedResourceRef struct {
	Type         string
	ID           string
	Handle       string
	CanonicalRef string
	FromAlias    bool
}

var resourceTables = map[string]string{
	"topic":    "topics",
	"board":    "boards",
	"card":     "cards",
	"document": "documents",
	"artifact": "artifacts",
	"event":    "events",
	"thread":   "threads",
}

func (s *Store) ResolveResourceRef(ctx context.Context, input ResourceRefInput) (ResolvedResourceRef, error) {
	if s == nil || s.db == nil {
		return ResolvedResourceRef{}, fmt.Errorf("primitives store database is not initialized")
	}
	typ := strings.TrimSpace(input.Type)
	value := strings.TrimSpace(input.Ref)
	if value == "" {
		return ResolvedResourceRef{}, ErrInvalidResourceRef
	}
	if strings.Contains(value, ":") {
		prefix, suffix, err := schema.SplitTypedRef(value)
		if err != nil {
			return ResolvedResourceRef{}, fmt.Errorf("%w: %v", ErrInvalidResourceRef, err)
		}
		if typ != "" && typ != prefix {
			return ResolvedResourceRef{}, fmt.Errorf("%w: typed ref %q does not match expected resource type %q", ErrInvalidResourceRef, prefix, typ)
		}
		typ = prefix
		value = suffix
	}
	if typ == "document_revision" || typ == "card_revision" {
		return resolveRevisionResourceRef(ctx, s.db, typ, value)
	}
	table := resourceTables[typ]
	if table == "" {
		return ResolvedResourceRef{}, fmt.Errorf("%w: unknown resource type %q", ErrInvalidResourceRef, typ)
	}

	normalized := handles.Normalize(value)
	if normalized != "" {
		resolved, err := resolveResourceByColumn(ctx, s.db, typ, table, "handle", normalized)
		if err == nil {
			return resolved, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return ResolvedResourceRef{}, err
		}

		var id, canonical string
		aliasErr := s.db.QueryRowContext(ctx,
			`SELECT resource_id, canonical_handle FROM resource_handle_aliases WHERE resource_type = ? AND alias_handle = ?`,
			typ, normalized,
		).Scan(&id, &canonical)
		if aliasErr == nil {
			resolved, err = resolveResourceByColumn(ctx, s.db, typ, table, "id", id)
			if err != nil {
				return ResolvedResourceRef{}, err
			}
			resolved.FromAlias = true
			if strings.TrimSpace(canonical) != "" {
				resolved.Handle = strings.TrimSpace(canonical)
				resolved.CanonicalRef = typ + ":" + resolved.Handle
			}
			return resolved, nil
		}
		if !errors.Is(aliasErr, sql.ErrNoRows) {
			return ResolvedResourceRef{}, fmt.Errorf("query resource handle alias: %w", aliasErr)
		}
	}

	if handles.IsUUIDLike(value) || rowExistsByID(ctx, s.db, table, value) {
		resolved, err := resolveResourceByColumn(ctx, s.db, typ, table, "id", value)
		if err == nil {
			return resolved, nil
		}
		if !errors.Is(err, ErrNotFound) {
			return ResolvedResourceRef{}, err
		}
	}
	return ResolvedResourceRef{}, ErrNotFound
}

func resolveRevisionResourceRef(ctx context.Context, q queryRower, typ, value string) (ResolvedResourceRef, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return ResolvedResourceRef{}, ErrInvalidResourceRef
	}
	if typ == "document_revision" {
		return resolveDocumentRevisionRef(ctx, q, value)
	}
	if typ == "card_revision" {
		return resolveCardRevisionRef(ctx, q, value)
	}
	return ResolvedResourceRef{}, fmt.Errorf("%w: unknown resource type %q", ErrInvalidResourceRef, typ)
}

func resolveDocumentRevisionRef(ctx context.Context, q queryRower, value string) (ResolvedResourceRef, error) {
	normalized := handles.Normalize(value)
	if normalized != "" {
		var revisionID, docHandle string
		var revisionNumber int
		err := q.QueryRowContext(ctx, `
			SELECT dr.revision_id, COALESCE(d.handle, d.id, dr.document_id), dr.revision_number
			  FROM document_revisions dr
			  LEFT JOIN documents d ON d.id = dr.document_id
			 WHERE lower(COALESCE(d.handle, d.id, dr.document_id) || '-r' || dr.revision_number) = ?`,
			normalized,
		).Scan(&revisionID, &docHandle, &revisionNumber)
		if err == nil {
			handle := revisionHandle(docHandle, revisionNumber)
			return ResolvedResourceRef{Type: "document_revision", ID: revisionID, Handle: handle, CanonicalRef: "document_revision:" + handle}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return ResolvedResourceRef{}, fmt.Errorf("query document revision by handle: %w", err)
		}
	}

	var docHandle string
	var revisionNumber int
	err := q.QueryRowContext(ctx, `
		SELECT COALESCE(d.handle, d.id, dr.document_id), dr.revision_number
		  FROM document_revisions dr
		  LEFT JOIN documents d ON d.id = dr.document_id
		 WHERE dr.revision_id = ?`,
		value,
	).Scan(&docHandle, &revisionNumber)
	if errors.Is(err, sql.ErrNoRows) {
		return ResolvedResourceRef{}, ErrNotFound
	}
	if err != nil {
		return ResolvedResourceRef{}, fmt.Errorf("query document revision by id: %w", err)
	}
	handle := revisionHandle(docHandle, revisionNumber)
	return ResolvedResourceRef{Type: "document_revision", ID: value, Handle: handle, CanonicalRef: "document_revision:" + handle}, nil
}

func resolveCardRevisionRef(ctx context.Context, q queryRower, value string) (ResolvedResourceRef, error) {
	normalized := handles.Normalize(value)
	if normalized != "" {
		var revisionID, cardHandle string
		var revisionNumber int
		err := q.QueryRowContext(ctx, `
			SELECT cr.revision_id, COALESCE(c.handle, c.id), cr.revision_number
			  FROM card_revisions cr
			  JOIN cards c ON c.id = cr.card_id
			 WHERE lower(COALESCE(c.handle, c.id) || '-r' || cr.revision_number) = ?`,
			normalized,
		).Scan(&revisionID, &cardHandle, &revisionNumber)
		if err == nil {
			handle := revisionHandle(cardHandle, revisionNumber)
			return ResolvedResourceRef{Type: "card_revision", ID: revisionID, Handle: handle, CanonicalRef: "card_revision:" + handle}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return ResolvedResourceRef{}, fmt.Errorf("query card revision by handle: %w", err)
		}
	}

	var cardHandle string
	var revisionNumber int
	err := q.QueryRowContext(ctx, `
		SELECT COALESCE(c.handle, c.id), cr.revision_number
		  FROM card_revisions cr
		  JOIN cards c ON c.id = cr.card_id
		 WHERE cr.revision_id = ?`,
		value,
	).Scan(&cardHandle, &revisionNumber)
	if errors.Is(err, sql.ErrNoRows) {
		return ResolvedResourceRef{}, ErrNotFound
	}
	if err != nil {
		return ResolvedResourceRef{}, fmt.Errorf("query card revision by id: %w", err)
	}
	handle := revisionHandle(cardHandle, revisionNumber)
	return ResolvedResourceRef{Type: "card_revision", ID: value, Handle: handle, CanonicalRef: "card_revision:" + handle}, nil
}

func revisionHandle(parentHandle string, revisionNumber int) string {
	return strings.TrimSpace(parentHandle) + "-r" + fmt.Sprintf("%d", revisionNumber)
}

func resolveResourceByColumn(ctx context.Context, db *sql.DB, typ, table, column, value string) (ResolvedResourceRef, error) {
	var id string
	var handle sql.NullString
	err := db.QueryRowContext(ctx, `SELECT id, handle FROM `+table+` WHERE `+column+` = ?`, value).Scan(&id, &handle)
	if errors.Is(err, sql.ErrNoRows) {
		return ResolvedResourceRef{}, ErrNotFound
	}
	if err != nil {
		return ResolvedResourceRef{}, fmt.Errorf("query %s by %s: %w", typ, column, err)
	}
	h := strings.TrimSpace(handle.String)
	canonical := typ + ":" + id
	if h != "" {
		canonical = typ + ":" + h
	}
	return ResolvedResourceRef{Type: typ, ID: id, Handle: h, CanonicalRef: canonical}, nil
}

func rowExistsByID(ctx context.Context, db *sql.DB, table, id string) bool {
	var one int
	return db.QueryRowContext(ctx, `SELECT 1 FROM `+table+` WHERE id = ?`, strings.TrimSpace(id)).Scan(&one) == nil
}

func uniqueHandleTx(ctx context.Context, q queryRower, typ, desired, fallbackSeed string) (string, error) {
	table := resourceTables[typ]
	if table == "" {
		return "", fmt.Errorf("unknown handle resource type %q", typ)
	}
	base := handles.Candidate(desired, fallbackSeed)
	for i := 0; i < 1000; i++ {
		candidate := base
		if i > 0 {
			suffix := fmt.Sprintf("-%d", i+1)
			cut := handles.MaxLength - len(suffix)
			if cut < 1 {
				cut = 1
			}
			if len(base) > cut {
				candidate = strings.TrimRight(base[:cut], "-") + suffix
			} else {
				candidate = base + suffix
			}
		}
		var n int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(1) FROM `+table+` WHERE handle = ?`, candidate).Scan(&n); err != nil {
			return "", err
		}
		var idN int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(1) FROM `+table+` WHERE id = ?`, candidate).Scan(&idN); err != nil {
			return "", err
		}
		var aliasN int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(1) FROM resource_handle_aliases WHERE resource_type = ? AND alias_handle = ?`, typ, candidate).Scan(&aliasN); err != nil {
			return "", err
		}
		if n == 0 && idN == 0 && aliasN == 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not allocate unique %s handle", typ)
}
