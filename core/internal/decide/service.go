package decide

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

type Service struct {
	store *Store
	cfg   Config
	deps  Dependencies
}

func NewService(store *Store, cfg Config, deps Dependencies) (*Service, error) {
	if store == nil || cfg.WorkspaceID == "" || deps.Authorize == nil {
		return nil, ErrInvalid
	}
	return &Service{store: store, cfg: cfg, deps: deps}, nil
}

func (s *Service) authorize(ctx context.Context, p Principal, permission, ref string) error {
	return s.authorizePermission(ctx, p, permission, ref)
}

func (s *Service) authorizePermission(ctx context.Context, p Principal, permission, ref string) error {
	if p.ActorID == "" || p.WorkspaceID != s.cfg.WorkspaceID {
		return ErrForbidden
	}
	if permission == "pm.propose" && !p.Human {
		return ErrForbidden
	}
	if err := s.deps.Authorize(ctx, p, permission, ref); err != nil {
		if errors.Is(err, ErrUnavailable) {
			return err
		}
		var nonHuman *ConversationOwnerNotHumanError
		if permission == "pm.propose" && errors.As(err, &nonHuman) {
			return nonHuman
		}
		// Authorizers may report missing work only after checking the principal's
		// permission. Preserve that diagnosis for proposals/source actions. Other
		// lookups retain the forbidden fold to prevent cross-principal existence leaks.
		if errors.Is(err, ErrNotFound) && (permission == "pm.propose" || strings.HasPrefix(permission, "pm.action.")) && s.deps.Authorize(ctx, p, permission, "") == nil {
			return ErrNotFound
		}
		return ErrForbidden
	}
	return nil
}

func validText(text string, max int) bool { return strings.TrimSpace(text) != "" && len(text) <= max }

func (s *Service) QueryContext(ctx context.Context, p Principal, workRef, query string, limit int) (ContextPage, error) {
	return s.QueryContextPage(ctx, p, workRef, query, "", limit)
}

func (s *Service) QueryContextPage(ctx context.Context, p Principal, workRef, query, cursor string, limit int) (ContextPage, error) {
	if err := s.authorize(ctx, p, "pm.read", workRef); err != nil {
		return ContextPage{}, err
	}
	if workRef != "" && cursor != "" {
		return ContextPage{}, ErrContextWorkCursor
	}
	if len(cursor) > 2000 {
		return ContextPage{}, ErrContextCursor
	}
	if limit < 1 || limit > 50 || len(query) > 2000 {
		return ContextPage{}, ErrInvalid
	}
	var page ContextPage
	var err error
	if s.deps.ReadContextPage != nil {
		page, err = s.deps.ReadContextPage(ctx, p, workRef, query, cursor, limit)
	} else if cursor == "" && s.deps.ReadContext != nil {
		page, err = s.deps.ReadContext(ctx, p, workRef, query, limit)
		if page.NextCursor != "" {
			page.NextCursor = ""
			page.Limitations = append(page.Limitations, "Context is partial; paginated tracker callback is not configured")
		}
	} else {
		return ContextPage{}, ErrUnavailable
	}
	if err != nil {
		return ContextPage{}, err
	}
	if len(page.Items) > limit || len(page.NextCursor) > 2000 || len(page.Limitations) > 50 {
		return ContextPage{}, ErrInvalid
	}
	raw, err := json.Marshal(page)
	if err != nil || len(raw) > 128*1024 {
		return ContextPage{}, ErrInvalid
	}
	return page, nil
}
