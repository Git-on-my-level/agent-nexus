// Actor alias resolution for mutation bodies and public-identity decoration
// of list responses.

package app

import (
	"encoding/json"
	"fmt"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

// mutationActorRawFromMap returns the trimmed string form of body["actor_id"], or "" if absent/null/blank.
func mutationActorRawFromMap(body map[string]any) string {
	v, ok := body["actor_id"]
	if !ok || v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case json.Number:
		return strings.TrimSpace(x.String())
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

// finalizeMutationActorID resolves actor_id alias "me", and when actor_id is missing or
// blank fills from resolveActorIDAlias("", cfg) and then cfg.ActorID (same pattern as
// flag-built mutation bodies for trash/archive-style commands).
func finalizeMutationActorID(body map[string]any, cfg config.Resolved) error {
	raw := mutationActorRawFromMap(body)
	if raw == "" {
		aid, err := resolveActorIDAlias("", cfg)
		if err != nil {
			return err
		}
		if aid != "" {
			body["actor_id"] = aid
			return nil
		}
		if prof := strings.TrimSpace(cfg.ActorID); prof != "" {
			body["actor_id"] = prof
		}
		return nil
	}
	aid, err := resolveActorIDAlias(raw, cfg)
	if err != nil {
		return err
	}
	if aid != "" {
		body["actor_id"] = aid
	} else {
		delete(body, "actor_id")
	}
	return nil
}

// finalizeOptionalMutationBodyActorID applies finalizeMutationActorID to a decoded JSON object body.
func finalizeOptionalMutationBodyActorID(body map[string]any, cfg config.Resolved) error {
	if body == nil {
		return nil
	}
	return finalizeMutationActorID(body, cfg)
}

func resolveActorIDAlias(raw string, cfg config.Resolved) (string, error) {
	actorID := strings.TrimSpace(raw)
	if actorID == "" {
		return "", nil
	}
	if actorID != "me" {
		return actorID, nil
	}
	resolved := strings.TrimSpace(cfg.ActorID)
	if resolved != "" {
		return resolved, nil
	}
	return "", errnorm.Usage(
		"invalid_request",
		"--actor-id me requires actor_id from the resolved derived agent",
	)
}

func enrichListBodyWithPublicIdentity(commandID string, body any) (any, bool) {
	typedBody, _ := body.(map[string]any)
	if typedBody == nil {
		return body, false
	}
	switch strings.TrimSpace(commandID) {
	case "threads.list":
		return body, addPublicIdentityToListField(typedBody, "threads", "thread")
	case "topics.list":
		return body, addPublicIdentityToListField(typedBody, "topics", "topic")
	case "artifacts.list":
		return body, addPublicIdentityToListField(typedBody, "artifacts", "artifact")
	case "boards.list":
		return body, addPublicIdentityToNestedListField(typedBody, "boards", []string{"board"}, "board")
	case "boards.cards.list":
		return body, addPublicIdentityToListField(typedBody, "cards", "card")
	case "docs.list", "docs.search":
		return body, addPublicIdentityToListField(typedBody, "documents", "document")
	case "events.list":
		return body, addPublicIdentityToListField(typedBody, "events", "event")
	case "inbox.list":
		return body, addInboxAliasesToListField(typedBody, "items")
	case "inbox.get":
		return body, addInboxAliasToItemField(typedBody, "item")
	case "threads.workspace":
		return body, enrichThreadWorkspaceBodyWithPublicIdentity(typedBody)
	default:
		return body, false
	}
}

// enrichThreadWorkspaceBodyWithPublicIdentity mirrors list-response public identity enrichment for GET /threads/{id}/workspace.
func enrichThreadWorkspaceBodyWithPublicIdentity(body map[string]any) bool {
	if body == nil {
		return false
	}
	changed := false
	if thread := asMap(body["thread"]); thread != nil {
		changed = addPublicIdentityToObjectIfPresent(thread, "thread") || changed
	}
	contextSliceKeys := []string{"recent_events", "key_artifacts", "open_cards", "documents"}
	if ctx := asMap(body["context"]); ctx != nil {
		for _, key := range contextSliceKeys {
			if items, ok := ctx[key].([]any); ok {
				changed = addPublicIdentityToObjectSlice(items, resourceKindForWorkspaceSlice(key)) || changed
			}
		}
	}
	collabSliceKeys := []string{"key_artifacts", "open_cards"}
	if collab := asMap(body["collaboration"]); collab != nil {
		for _, key := range collabSliceKeys {
			if items, ok := collab[key].([]any); ok {
				changed = addPublicIdentityToObjectSlice(items, resourceKindForWorkspaceSlice(key)) || changed
			}
		}
	}
	if inbox := asMap(body["inbox"]); inbox != nil {
		changed = addInboxAliasesToListField(inbox, "items") || changed
	}
	if pending := asMap(body["pending_attention"]); pending != nil {
		changed = addInboxAliasesToListField(pending, "items") || changed
	}
	if memberships := asMap(body["board_memberships"]); memberships != nil {
		if items, ok := memberships["items"].([]any); ok {
			for _, raw := range items {
				item, _ := raw.(map[string]any)
				if item == nil {
					continue
				}
				if board := asMap(item["board"]); board != nil {
					if addPublicIdentityToObjectIfPresent(board, "board") {
						changed = true
					}
				}
				if card := asMap(item["card"]); card != nil {
					if addPublicIdentityToObjectIfPresent(card, "card") {
						changed = true
					}
				}
			}
		}
	}
	if rt := asMap(body["related_threads"]); rt != nil {
		if items, ok := rt["items"].([]any); ok {
			for _, raw := range items {
				item, _ := raw.(map[string]any)
				if item == nil {
					continue
				}
				if t := asMap(item["thread"]); t != nil {
					if addPublicIdentityToObjectIfPresent(t, "thread") {
						changed = true
					}
				}
			}
		}
	}
	return changed
}

func resourceKindForWorkspaceSlice(key string) string {
	switch key {
	case "recent_events":
		return "event"
	case "key_artifacts":
		return "artifact"
	case "open_cards":
		return "card"
	case "documents":
		return "document"
	default:
		return ""
	}
}

func addPublicIdentityToListField(body map[string]any, field string, kind string) bool {
	items, _ := body[field].([]any)
	if len(items) == 0 {
		return false
	}
	changed := false
	for _, rawItem := range items {
		item, _ := rawItem.(map[string]any)
		if item == nil {
			continue
		}
		if addPublicIdentityToObjectIfPresent(item, kind) {
			changed = true
		}
	}
	return changed
}

func addPublicIdentityToNestedListField(body map[string]any, field string, path []string, kind string) bool {
	items, _ := body[field].([]any)
	if len(items) == 0 {
		return false
	}
	changed := false
	for _, rawItem := range items {
		item, _ := rawItem.(map[string]any)
		if item == nil {
			continue
		}
		target := item
		for _, segment := range path {
			nested, _ := target[segment].(map[string]any)
			if nested == nil {
				target = nil
				break
			}
			target = nested
		}
		if target == nil {
			continue
		}
		if addPublicIdentityToObjectIfPresent(target, kind) {
			changed = true
		}
	}
	return changed
}

func addPublicIdentityToObjectIfPresent(obj map[string]any, kind string) bool {
	if obj == nil {
		return false
	}
	changed := false
	refKey := canonicalResourceKind(kind) + "_ref"
	handleKey := canonicalResourceKind(kind) + "_handle"
	if strings.TrimSpace(anyString(obj["ref"])) == "" {
		if ref := firstNonEmpty(strings.TrimSpace(anyString(obj[refKey])), publicRefFromID(kind, anyString(obj["id"]))); ref != "" {
			obj["ref"] = ref
			changed = true
		}
	}
	if strings.TrimSpace(anyString(obj["handle"])) == "" {
		if handle := firstNonEmpty(strings.TrimSpace(anyString(obj[handleKey])), publicHandleFromRef(anyString(obj["ref"]))); handle != "" {
			obj["handle"] = handle
			changed = true
		}
	}
	return changed
}

func addPublicIdentityToObjectSlice(slice []any, kind string) bool {
	changed := false
	for _, raw := range slice {
		item, _ := raw.(map[string]any)
		if item == nil {
			continue
		}
		if addPublicIdentityToObjectIfPresent(item, kind) {
			changed = true
		}
	}
	return changed
}

func publicRefFromID(kind string, id string) string {
	id = strings.TrimSpace(id)
	kind = canonicalResourceKind(kind)
	if id == "" || kind == "" {
		return ""
	}
	if strings.Contains(id, ":") {
		return id
	}
	return kind + ":" + id
}

func publicHandleFromRef(ref string) string {
	_, value, _, err := parseTypedRef(ref)
	if err != nil {
		return ""
	}
	return value
}
