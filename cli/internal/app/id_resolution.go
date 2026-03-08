package app

import (
	"context"
	"strings"

	"organization-autorunner-cli/internal/config"
)

var typedRefLookupByPrefix = map[string]resourceIDLookupSpec{
	"thread":     threadIDLookupSpec,
	"artifact":   artifactIDLookupSpec,
	"commitment": commitmentIDLookupSpec,
}

type mutationFieldKind int

const (
	mutationFieldThreadID mutationFieldKind = iota + 1
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

func (a *App) normalizeMutationBodyIDs(ctx context.Context, cfg config.Resolved, commandID string, body any) (any, error) {
	_, ok := body.(map[string]any)
	if !ok || !commandSupportsMutationIDResolution(commandID) {
		return body, nil
	}
	cloned, _ := deepCloneJSONValue(body).(map[string]any)
	if err := a.normalizeMutationCommandBody(ctx, cfg, strings.TrimSpace(commandID), cloned); err != nil {
		return nil, err
	}
	return cloned, nil
}

func commandSupportsMutationIDResolution(commandID string) bool {
	switch strings.TrimSpace(commandID) {
	case "commitments.create",
		"commitments.patch",
		"docs.create",
		"docs.update",
		"events.create",
		"inbox.ack",
		"packets.receipts.create",
		"packets.reviews.create",
		"packets.work-orders.create",
		"threads.create",
		"threads.patch":
		return true
	default:
		return false
	}
}

func (a *App) normalizeMutationCommandBody(ctx context.Context, cfg config.Resolved, commandID string, body map[string]any) error {
	switch commandID {
	case "threads.create":
		return a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "thread"), []mutationFieldSpec{
			{key: "key_artifacts", kind: mutationFieldTypedRefList},
		})
	case "threads.patch":
		return a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "patch"), []mutationFieldSpec{
			{key: "key_artifacts", kind: mutationFieldTypedRefList},
		})
	case "commitments.create":
		return a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "commitment"), []mutationFieldSpec{
			{key: "thread_id", kind: mutationFieldThreadID},
			{key: "links", kind: mutationFieldTypedRefList},
		})
	case "commitments.patch":
		if err := a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "patch"), []mutationFieldSpec{
			{key: "links", kind: mutationFieldTypedRefList},
		}); err != nil {
			return err
		}
		return a.normalizeMutationFields(ctx, cfg, body, []mutationFieldSpec{
			{key: "refs", kind: mutationFieldTypedRefList},
		})
	case "docs.create", "docs.update":
		return a.normalizeMutationFields(ctx, cfg, body, []mutationFieldSpec{
			{key: "refs", kind: mutationFieldTypedRefList},
		})
	case "events.create":
		return a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "event"), []mutationFieldSpec{
			{key: "thread_id", kind: mutationFieldThreadID},
			{key: "refs", kind: mutationFieldTypedRefList},
		})
	case "inbox.ack":
		return a.normalizeMutationFields(ctx, cfg, body, []mutationFieldSpec{
			{key: "thread_id", kind: mutationFieldThreadID},
		})
	case "packets.work-orders.create":
		if err := a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "artifact"), []mutationFieldSpec{
			{key: "refs", kind: mutationFieldTypedRefList},
		}); err != nil {
			return err
		}
		return a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "packet"), []mutationFieldSpec{
			{key: "thread_id", kind: mutationFieldThreadID},
			{key: "context_refs", kind: mutationFieldTypedRefList},
		})
	case "packets.receipts.create":
		if err := a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "artifact"), []mutationFieldSpec{
			{key: "refs", kind: mutationFieldTypedRefList},
		}); err != nil {
			return err
		}
		return a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "packet"), []mutationFieldSpec{
			{key: "thread_id", kind: mutationFieldThreadID},
			{key: "outputs", kind: mutationFieldTypedRefList},
			{key: "verification_evidence", kind: mutationFieldTypedRefList},
		})
	case "packets.reviews.create":
		if err := a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "artifact"), []mutationFieldSpec{
			{key: "refs", kind: mutationFieldTypedRefList},
		}); err != nil {
			return err
		}
		return a.normalizeMutationFields(ctx, cfg, nestedMutationMap(body, "packet"), []mutationFieldSpec{
			{key: "evidence_refs", kind: mutationFieldTypedRefList},
		})
	default:
		return nil
	}
}

func nestedMutationMap(root map[string]any, key string) map[string]any {
	value, _ := root[key].(map[string]any)
	return value
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

func shouldResolveDisplayedShortID(raw string) bool {
	return len(strings.TrimSpace(raw)) == shortIDLength
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
