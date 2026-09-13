package primitives

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// ResolutionRef is a live workspace projection, not acceptance or delivery proof.
type ResolutionRef struct {
	Ref            string `json:"ref"`
	Kind           string `json:"kind"`
	TitleOrSummary string `json:"title_or_summary"`
	Exists         bool   `json:"exists"`
}

func (s *Store) ResolveResolutionRef(ctx context.Context, ref string) (ResolutionRef, error) {
	return resolveResolutionRef(ctx, s.db, ref)
}

func resolveResolutionRef(ctx context.Context, q queryRower, ref string) (ResolutionRef, error) {
	kind, value, ok := normalizeTypedRef(ref)
	out := ResolutionRef{Ref: ref, Kind: kind}
	if !ok || (resourceTables[kind] == "" && kind != "document_revision" && kind != "card_revision") {
		return out, nil
	}
	resolved, err := resolveResourceByTypedValue(ctx, q, kind, value)
	if errors.Is(err, ErrNotFound) && resourceTables[kind] != "" {
		var id string
		err = q.QueryRowContext(ctx, `SELECT resource_id FROM resource_handle_aliases WHERE resource_type=? AND alias_handle=?`, kind, value).Scan(&id)
		if err == nil {
			resolved, err = resolveResourceByTypedValue(ctx, q, kind, id)
		}
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	var title string
	switch kind {
	case "document_revision", "card_revision":
		parent, revisions, column := "documents", "document_revisions", "document_id"
		if kind == "card_revision" {
			parent, revisions, column = "cards", "card_revisions", "card_id"
		}
		err = q.QueryRowContext(ctx, `SELECT COALESCE(p.title,'') FROM `+revisions+` r JOIN `+parent+` p ON p.id=r.`+column+` JOIN artifacts a ON a.id=r.artifact_id WHERE r.revision_id=? AND p.trashed_at IS NULL AND a.trashed_at IS NULL`, resolved.ID).Scan(&title)
	default:
		expression := "COALESCE(title,'')"
		switch kind {
		case "artifact":
			expression = "COALESCE(json_extract(metadata_json,'$.title'),json_extract(metadata_json,'$.summary'),kind,'')"
		case "event":
			expression = "COALESCE(json_extract(payload_json,'$.summary'),json_extract(payload_json,'$.payload.summary'),json_extract(payload_json,'$.payload.text'),json_extract(payload_json,'$.text'),type,'')"
		case "thread":
			expression = "COALESCE(json_extract(body_json,'$.title'),'')"
		}
		err = q.QueryRowContext(ctx, `SELECT `+expression+` FROM `+resourceTables[kind]+` WHERE id=? AND trashed_at IS NULL`, resolved.ID).Scan(&title)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = resolved.CanonicalRef
	}
	if runes := []rune(title); len(runes) > 240 {
		title = string(runes[:240]) + "…"
	}
	out.Exists, out.TitleOrSummary = true, title
	return out, nil
}

// Use the same transaction as the card mutation so trash/delete cannot race
// between validation and completion. The workspace database is the scope.
func validateResolutionRefs(ctx context.Context, q queryRower, refs []string) error {
	for _, ref := range refs {
		resolved, err := resolveResolutionRef(ctx, q, ref)
		if err != nil {
			return err
		}
		if !resolved.Exists {
			return invalidBoardRequest(fmt.Sprintf("resolution_refs: missing or trashed resolution ref %q", ref))
		}
	}
	return nil
}
