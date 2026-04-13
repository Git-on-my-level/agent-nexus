package app

import (
	"context"
	"strings"

	"organization-autorunner-cli/internal/config"
)

var typedRefLookupByPrefix = map[string]resourceIDLookupSpec{
	"thread":   threadIDLookupSpec,
	"topic":    topicIDLookupSpec,
	"card":     cardIDLookupSpec,
	"artifact": artifactIDLookupSpec,
	"board":    boardIDLookupSpec,
}

type mutationFieldKind int

const (
	mutationFieldThreadID mutationFieldKind = iota + 1
	mutationFieldTypedRef
	mutationFieldTypedRefList
)

type mutationFieldSpec struct {
	key  string
	kind mutationFieldKind
}

func (a *App) resolveThreadIDFilters(ctx context.Context, cfg config.Resolved, rawIDs []string) ([]string, error) {
	if len(rawIDs) == 0 {
		return nil, nil
	}
	resolved := make([]string, 0, len(rawIDs))
	for _, rawID := range normalizeIDFilters(rawIDs) {
		resolvedID := rawID
		if shouldResolveDisplayedShortID(rawID) {
			var err error
			resolvedID, err = a.resolveResourceIDFromList(ctx, cfg, rawID, threadIDLookupSpec)
			if err != nil {
				return nil, err
			}
		}
		resolved = append(resolved, resolvedID)
	}
	return normalizeIDFilters(resolved), nil
}

func (a *App) normalizeMutationBodyIDs(ctx context.Context, cfg config.Resolved, commandID string, pathParams map[string]string, body any) (any, error) {
	_, ok := body.(map[string]any)
	if !ok || !commandSupportsMutationIDResolution(commandID) {
		return body, nil
	}
	cloned, _ := deepCloneJSONValue(body).(map[string]any)
	if err := a.normalizeMutationCommandBody(ctx, cfg, strings.TrimSpace(commandID), pathParams, cloned); err != nil {
		return nil, err
	}
	return cloned, nil
}

func commandSupportsMutationIDResolution(commandID string) bool {
	switch strings.TrimSpace(commandID) {
	case "topics.create",
		"topics.patch",
		"boards.create",
		"boards.update",
		"boards.cards.add",
		"boards.cards.create",
		"boards.cards.batch_add",
		"cards.patch",
		"cards.move",
		"docs.create",
		"docs.update",
		"docs.revisions.create",
		"events.create",
		"inbox.acknowledge",
		"packets.receipts.create",
		"packets.reviews.create",
		"agent.notifications.read",
		"agent.notifications.dismiss":
		return true
	default:
		return false
	}
}

func nestedMutationMap(root map[string]any, key string) map[string]any {
	value, _ := root[key].(map[string]any)
	return value
}

// effectiveCardMoveMutationMap returns the map holding move fields: either the
// legacy nested `move` object (when root column_key is not set) or the root
// body for flat requests aligned with OpenAPI MoveCardRequest.
func effectiveCardMoveMutationMap(body map[string]any) map[string]any {
	if body == nil {
		return nil
	}
	nested := nestedMutationMap(body, "move")
	if nested != nil && strings.TrimSpace(anyString(body["column_key"])) == "" {
		return nested
	}
	if strings.TrimSpace(anyString(body["column_key"])) != "" {
		return body
	}
	return nested
}

func (a *App) normalizeMutationFields(ctx context.Context, cfg config.Resolved, target map[string]any, specs []mutationFieldSpec) error {
	if target == nil {
		return nil
	}
	for _, spec := range specs {
		rawValue, exists := target[spec.key]
		if !exists {
			continue
		}
		switch spec.kind {
		case mutationFieldThreadID:
			normalized, err := a.normalizeThreadIDValue(ctx, cfg, rawValue)
			if err != nil {
				return err
			}
			target[spec.key] = normalized
		case mutationFieldTypedRef:
			normalized, err := a.normalizeTypedRefValue(ctx, cfg, rawValue)
			if err != nil {
				return err
			}
			target[spec.key] = normalized
		case mutationFieldTypedRefList:
			normalized, err := a.normalizeTypedRefList(ctx, cfg, rawValue)
			if err != nil {
				return err
			}
			target[spec.key] = normalized
		}
	}
	return nil
}

func (a *App) normalizeThreadIDValue(ctx context.Context, cfg config.Resolved, value any) (any, error) {
	raw := strings.TrimSpace(anyString(value))
	if raw == "" || !shouldResolveDisplayedShortID(raw) {
		return value, nil
	}
	return a.resolveResourceIDFromList(ctx, cfg, raw, threadIDLookupSpec)
}

func (a *App) normalizeTypedRefList(ctx context.Context, cfg config.Resolved, value any) (any, error) {
	refs, ok := asStringList(value)
	if !ok {
		return value, nil
	}
	out := make([]any, 0, len(refs))
	for _, ref := range refs {
		normalized, err := a.normalizeTypedRef(ctx, cfg, ref)
		if err != nil {
			return nil, err
		}
		out = append(out, normalized)
	}
	return out, nil
}

func (a *App) normalizeTypedRef(ctx context.Context, cfg config.Resolved, raw string) (string, error) {
	kind, value, _, err := parseTypedRef(raw)
	if err != nil {
		return raw, nil
	}
	spec, ok := typedRefLookupByPrefix[kind]
	if !ok || !shouldResolveDisplayedShortID(value) {
		return raw, nil
	}
	resolvedID, err := a.resolveResourceIDFromList(ctx, cfg, value, spec)
	if err != nil {
		return "", err
	}
	return kind + ":" + resolvedID, nil
}

func (a *App) normalizeTypedRefValue(ctx context.Context, cfg config.Resolved, value any) (any, error) {
	raw := strings.TrimSpace(anyString(value))
	if raw == "" {
		return value, nil
	}
	normalized, err := a.normalizeTypedRef(ctx, cfg, raw)
	if err != nil {
		return nil, err
	}
	return normalized, nil
}

func shouldResolveDisplayedShortID(raw string) bool {
	return len(strings.TrimSpace(raw)) == shortIDLength
}

func ensureEmptyListDefaults(body map[string]any, nestKey string, fields []string) {
	if body == nil {
		return
	}
	nested, _ := body[nestKey].(map[string]any)
	if nested == nil {
		return
	}
	for _, field := range fields {
		if _, exists := nested[field]; !exists {
			nested[field] = []any{}
		}
	}
}

func deepCloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, nested := range typed {
			out[key] = deepCloneJSONValue(nested)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, nested := range typed {
			out = append(out, deepCloneJSONValue(nested))
		}
		return out
	default:
		return value
	}
}
