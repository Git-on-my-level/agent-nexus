package server

import (
	"errors"
	"net/http"
	"strings"

	"agent-nexus-core/internal/primitives"
)

// PM conversation history lives on canonical threads, events, and wake
// artifacts. Protecting only /pm/ is insufficient: generic native routes must
// hide those records from anyone who is not the conversation owner or the
// selected PM agent. Missing identity fails closed (the resource looks absent).
func pmThreadOwner(thread map[string]any) string {
	if thread == nil {
		return ""
	}
	return strings.TrimSpace(anyString(thread["pm_actor_id"]))
}

func pmAgentActorID(opts handlerOptions) string {
	if opts.pmRuntime == nil {
		return ""
	}
	return strings.TrimSpace(opts.pmRuntime.AgentActorID())
}

func canAccessPMThread(r *http.Request, opts handlerOptions, thread map[string]any) bool {
	owner := pmThreadOwner(thread)
	if owner == "" {
		return true
	}
	principal, ok := cachedAuthenticatedPrincipal(r)
	if !ok || principal == nil || strings.TrimSpace(principal.ActorID) == "" {
		return false
	}
	if principal.ActorID == owner {
		return true
	}
	agent := pmAgentActorID(opts)
	return agent != "" && principal.ActorID == agent
}

func denyPMNotFound(w http.ResponseWriter, resource string) {
	writeError(w, http.StatusNotFound, "not_found", resource+" not found")
}

func threadAccessible(r *http.Request, opts handlerOptions, threadID string) bool {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" || opts.primitiveStore == nil {
		return true
	}
	thread, err := opts.primitiveStore.GetThread(r.Context(), threadID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			return true
		}
		return false
	}
	return canAccessPMThread(r, opts, thread)
}

func requireAccessibleThreadFilter(w http.ResponseWriter, r *http.Request, opts handlerOptions, threadID, resource string) bool {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" || opts.primitiveStore == nil {
		return true
	}
	thread, err := opts.primitiveStore.GetThread(r.Context(), threadID)
	if err != nil {
		if errors.Is(err, primitives.ErrNotFound) {
			return true
		}
		denyPMNotFound(w, resource)
		return false
	}
	if canAccessPMThread(r, opts, thread) {
		return true
	}
	denyPMNotFound(w, resource)
	return false
}

func requireAccessibleThreadMap(w http.ResponseWriter, r *http.Request, opts handlerOptions, thread map[string]any, resource string) bool {
	if canAccessPMThread(r, opts, thread) {
		return true
	}
	denyPMNotFound(w, resource)
	return false
}

func requireAccessibleThreadID(w http.ResponseWriter, r *http.Request, opts handlerOptions, threadID, resource string) bool {
	if threadAccessible(r, opts, threadID) {
		return true
	}
	denyPMNotFound(w, resource)
	return false
}

func filterAccessibleThreads(r *http.Request, opts handlerOptions, threads []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(threads))
	for _, thread := range threads {
		if canAccessPMThread(r, opts, thread) {
			out = append(out, thread)
		}
	}
	return out
}

func resourceLooksLikePM(resource map[string]any) bool {
	if resource == nil {
		return false
	}
	if strings.TrimSpace(anyString(resource["pm_conversation_id"])) != "" {
		return true
	}
	payload, _ := resource["payload"].(map[string]any)
	if payload != nil && (payload["pm_turn_id"] != nil || payload["pm_execution"] != nil) {
		return true
	}
	return false
}

func eventThreadID(event map[string]any) string {
	if id := strings.TrimSpace(anyString(event["thread_id"])); id != "" {
		return id
	}
	for _, ref := range stringSliceAny(event["refs"]) {
		if strings.HasPrefix(ref, "thread:") {
			return strings.TrimPrefix(ref, "thread:")
		}
	}
	return ""
}

func artifactThreadID(artifact map[string]any) string {
	if id := strings.TrimSpace(anyString(artifact["thread_id"])); id != "" {
		return id
	}
	for _, ref := range stringSliceAny(artifact["refs"]) {
		if strings.HasPrefix(ref, "thread:") {
			return strings.TrimPrefix(ref, "thread:")
		}
	}
	return ""
}

func stringSliceAny(raw any) []string {
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s := strings.TrimSpace(anyString(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func requireAccessibleEvent(w http.ResponseWriter, r *http.Request, opts handlerOptions, event map[string]any) bool {
	if opts.primitiveStore == nil {
		return true
	}
	threadID := eventThreadID(event)
	if threadID != "" {
		thread, err := opts.primitiveStore.GetThread(r.Context(), threadID)
		if err == nil {
			return requireAccessibleThreadMap(w, r, opts, thread, "event")
		}
		if err != nil && !errors.Is(err, primitives.ErrNotFound) {
			denyPMNotFound(w, "event")
			return false
		}
	}
	if resourceLooksLikePM(event) && !canAccessPMThread(r, opts, map[string]any{"pm_actor_id": anyString(event["actor_id"])}) {
		denyPMNotFound(w, "event")
		return false
	}
	return true
}

func requireAccessibleArtifact(w http.ResponseWriter, r *http.Request, opts handlerOptions, artifact map[string]any) bool {
	threadID := artifactThreadID(artifact)
	if threadID == "" {
		return true
	}
	return requireAccessibleThreadID(w, r, opts, threadID, "artifact")
}

func filterAccessibleEvents(r *http.Request, opts handlerOptions, events []map[string]any) []map[string]any {
	if opts.primitiveStore == nil {
		return events
	}
	out := make([]map[string]any, 0, len(events))
	allowed := map[string]bool{}
	denied := map[string]bool{}
	for _, event := range events {
		threadID := eventThreadID(event)
		if threadID == "" {
			if resourceLooksLikePM(event) && !canAccessPMThread(r, opts, map[string]any{"pm_actor_id": anyString(event["actor_id"])}) {
				continue
			}
			out = append(out, event)
			continue
		}
		if allowed[threadID] {
			out = append(out, event)
			continue
		}
		if denied[threadID] {
			continue
		}
		thread, err := opts.primitiveStore.GetThread(r.Context(), threadID)
		if err != nil {
			if errors.Is(err, primitives.ErrNotFound) {
				if resourceLooksLikePM(event) && !canAccessPMThread(r, opts, map[string]any{"pm_actor_id": anyString(event["actor_id"])}) {
					denied[threadID] = true
					continue
				}
				allowed[threadID] = true
				out = append(out, event)
				continue
			}
			denied[threadID] = true
			continue
		}
		if canAccessPMThread(r, opts, thread) {
			allowed[threadID] = true
			out = append(out, event)
			continue
		}
		denied[threadID] = true
	}
	return out
}

func filterAccessibleArtifacts(r *http.Request, opts handlerOptions, artifacts []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(artifacts))
	for _, artifact := range artifacts {
		threadID := artifactThreadID(artifact)
		if threadID == "" || threadAccessible(r, opts, threadID) {
			out = append(out, artifact)
		}
	}
	return out
}

func filterAccessibleInboxItems(r *http.Request, opts handlerOptions, items []map[string]any, projected []primitives.DerivedInboxItem) []map[string]any {
	if len(items) != len(projected) {
		return items
	}
	out := make([]map[string]any, 0, len(items))
	for i, item := range items {
		threadID := strings.TrimSpace(projected[i].ThreadID)
		if threadID == "" || threadAccessible(r, opts, threadID) {
			out = append(out, item)
		}
	}
	return out
}
