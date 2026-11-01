// API reads and writes the move engine performs against source and destination
// workspaces: the generic call wrapper, card reads and plan hydration, and
// source resource reads.

package app

import (
	"context"
	"net/http"
	"net/url"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func moveCall(ctx context.Context, a *App, cfg config.Resolved, method, path string, body any) (map[string]any, error) {
	result, err := a.invokeRawJSON(ctx, cfg, "move", method, path, body)
	if err != nil {
		return nil, err
	}
	return commandResultBody(result), nil
}

func moveRead(ctx context.Context, a *App, cfg config.Resolved, path, key string) (map[string]any, error) {
	body, err := moveCall(ctx, a, cfg, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if key == "" {
		return body, nil
	}
	value := asMap(body[key])
	if value == nil {
		return nil, errnorm.Internal("move_response_invalid", "move source API response omitted "+key)
	}
	return value, nil
}

func moveReadCardPlan(ctx context.Context, a *App, cfg config.Resolved, ref string) (map[string]any, error) {
	return moveCall(ctx, a, cfg, http.MethodGet, "/cards/"+url.PathEscape(ref)+"/plan", nil)
}

func moveHydrateCardIdentity(ctx context.Context, a *App, cfg config.Resolved, work map[string]any) error {
	ref := moveFieldString(work, "ref")
	if ref == "" {
		return errnorm.Internal("move_response_invalid", "card read omitted its ref")
	}
	card, err := moveRead(ctx, a, cfg, "/cards/"+url.PathEscape(ref), "card")
	if err != nil {
		return err
	}
	cardID := moveFieldString(card, "id")
	cardRef := moveFieldString(card, "ref")
	if cardID == "" || cardRef == "" {
		return errnorm.Internal("move_response_invalid", "canonical card read omitted its id or ref")
	}
	if workID := moveFieldString(work, "id"); workID != "" && workID != cardID {
		return errnorm.New(errnorm.KindRemote, "source_changed", "source card identity changed while the move snapshot was being read; retry the move")
	}
	if workUpdatedAt, cardUpdatedAt := moveFieldString(work, "updated_at"), moveFieldString(card, "updated_at"); workUpdatedAt != "" && cardUpdatedAt != "" && workUpdatedAt != cardUpdatedAt {
		return errnorm.New(errnorm.KindRemote, "source_changed", "source card changed while its canonical identity was being read; retry the move")
	}
	work["id"] = cardID
	work["ref"] = cardRef
	if handle := moveFieldString(card, "handle"); handle != "" {
		work["handle"] = handle
	}
	return nil
}

// Cards expose a plan in several read projections, but only /cards/{ref}/plan
// reads the canonical plan store. Hydrate the snapshot from that API and fence
// it against the card read so a concurrent plan edit cannot be lost.
func moveHydrateCardPlan(ctx context.Context, a *App, cfg config.Resolved, work map[string]any) error {
	ref := moveFieldString(work, "ref")
	if ref == "" {
		return errnorm.Internal("move_response_invalid", "card read omitted its ref")
	}
	if err := moveHydrateCardIdentity(ctx, a, cfg, work); err != nil {
		return err
	}
	ref = moveFieldString(work, "ref")
	plan, err := moveReadCardPlan(ctx, a, cfg, ref)
	if err != nil {
		return err
	}
	cardUpdatedAt := moveFieldString(work, "updated_at")
	planUpdatedAt := moveFieldString(plan, "if_updated_at")
	if cardUpdatedAt != "" && planUpdatedAt != cardUpdatedAt {
		return errnorm.New(errnorm.KindRemote, "source_changed", "source card or plan changed while the move snapshot was being read; retry the move")
	}
	work["plan"] = plan["plan"]
	return nil
}

func moveWriteCardPlan(ctx context.Context, a *App, cfg config.Resolved, ref string, expected any) error {
	if expected == nil {
		return nil
	}
	path := "/cards/" + url.PathEscape(ref) + "/plan"
	current, err := moveCall(ctx, a, cfg, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	if moveJSONEqual(expected, current["plan"]) {
		return nil
	}
	updatedAt := moveFieldString(current, "if_updated_at")
	if updatedAt == "" {
		return errnorm.Internal("move_response_invalid", "canonical plan read omitted its concurrency token")
	}
	_, err = moveCall(ctx, a, cfg, http.MethodPut, path, map[string]any{
		"plan": expected, "if_updated_at": updatedAt,
	})
	return err
}

func (a *App) readMoveSourceResource(ctx context.Context, cfg config.Resolved, resource *topicMoveResource) (map[string]any, error) {
	path, key := "", ""
	switch resource.kind {
	case "document":
		path, key = "/docs/"+url.PathEscape(resource.sourceRef), "document"
	case "board":
		path, key = "/boards/"+url.PathEscape(resource.sourceRef), "board"
	case "card":
		path, key = "/work/"+url.PathEscape(resource.sourceRef), "work"
	default:
		return nil, errnorm.Internal("move_resource_kind_invalid", "unsupported move resource kind "+resource.kind)
	}
	if resource.kind != "document" {
		object, err := moveRead(ctx, a, cfg, path, key)
		if err != nil {
			return nil, err
		}
		if resource.kind == "card" {
			if err := moveHydrateCardPlan(ctx, a, cfg, object); err != nil {
				return nil, err
			}
		}
		return object, nil
	}
	body, err := moveCall(ctx, a, cfg, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	doc := asMap(body["document"])
	if doc == nil {
		return nil, errnorm.Internal("move_response_invalid", "document read omitted document")
	}
	out := cloneMoveMap(doc)
	out["revision"] = body["revision"]
	return out, nil
}
