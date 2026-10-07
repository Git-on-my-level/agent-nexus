package pm

import (
	"context"
	"errors"
)

func (s *Service) authorizeReadBatch(ctx context.Context, p Principal, permission string, refs []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(refs) == 0 {
		return out, nil
	}
	if p.ActorID == "" || p.WorkspaceID != s.cfg.WorkspaceID || (permission != "pm.read" && permission != "pm.approve") {
		return out, nil
	}
	if s.deps.AuthorizeReadBatch != nil {
		allowed, err := s.deps.AuthorizeReadBatch(ctx, p, permission, refs)
		if errors.Is(err, ErrUnavailable) {
			return nil, err
		}
		if err != nil {
			return out, nil
		}
		return allowed, nil
	}
	for _, ref := range refs {
		out[ref] = s.authorize(ctx, p, permission, ref) == nil
	}
	return out, nil
}
