// Evidence ref extraction from PM runner replies: trailing evidence block and
// JSON evidence_refs parsing, ref validation, and resolvable-ref filtering
// against the API.

package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func collectDeliberateEvidenceRefs(extracted, raw string) (string, []string) {
	clean, blockRefs := splitEvidenceTrailer(extracted)
	refs := append([]string{}, jsonEvidenceRefsFromText(raw)...)
	refs = append(refs, jsonEvidenceRefsFromText(extracted)...)
	refs = append(refs, blockRefs...)
	return clean, uniqueEvidenceRefs(refs)
}

func extractEvidenceRefs(text string) []string {
	_, refs := collectDeliberateEvidenceRefs(text, text)
	return refs
}

func splitEvidenceTrailer(text string) (string, []string) {
	lines := strings.Split(text, "\n")
	marker := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == evidenceBlockMarker {
			marker = i
		}
	}
	if marker < 0 {
		return strings.TrimSpace(text), nil
	}
	body := strings.TrimSpace(strings.Join(lines[:marker], "\n"))
	return body, parseEvidenceRefLines(lines[marker+1:])
}

func parseEvidenceRefLines(lines []string) []string {
	var out []string
	for _, line := range lines {
		if ref := parseEvidenceRefToken(line); ref != "" {
			out = append(out, ref)
		}
	}
	return out
}

func jsonEvidenceRefsFromText(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || (raw[0] != '{' && raw[0] != '[') {
		return nil
	}
	var payload any
	if json.Unmarshal([]byte(raw), &payload) != nil {
		return nil
	}
	if obj := assistantMessageObject(payload); obj != nil {
		return evidenceRefsFromAny(obj["evidence_refs"])
	}
	if top, ok := payload.(map[string]any); ok {
		return evidenceRefsFromAny(top["evidence_refs"])
	}
	return nil
}

func assistantMessageObject(v any) map[string]any {
	switch t := v.(type) {
	case map[string]any:
		if role, _ := t["role"].(string); strings.EqualFold(role, "assistant") {
			return t
		}
		var found map[string]any
		for _, key := range []string{"messages", "result", "content", "output", "data"} {
			if obj := assistantMessageObject(t[key]); obj != nil {
				found = obj
			}
		}
		return found
	case []any:
		var found map[string]any
		for _, item := range t {
			if obj := assistantMessageObject(item); obj != nil {
				found = obj
			}
		}
		return found
	default:
		return nil
	}
}

func evidenceRefsFromAny(raw any) []string {
	var out []string
	for _, item := range stringList(raw) {
		if ref := parseEvidenceRefToken(item); ref != "" {
			out = append(out, ref)
		}
	}
	return out
}

func parseEvidenceRefToken(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimRight(raw, typedRefTrailPunct)
	if !evidenceRefRe.MatchString(raw) {
		return ""
	}
	return raw
}

func uniqueEvidenceRefs(refs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, ref := range refs {
		if ref == "" || seen[ref] {
			continue
		}
		seen[ref] = true
		out = append(out, ref)
		if len(out) >= 50 {
			break
		}
	}
	return out
}

func (a *App) filterResolvableEvidenceRefs(ctx context.Context, cfg config.Resolved, refs []string) []string {
	kept := make([]string, 0, len(refs))
	for _, ref := range refs {
		command, method, path, reason := evidenceRefLookup(ref)
		if reason != "" {
			a.pmLog("pm serve: dropping evidence ref %s: %s\n", ref, reason)
			continue
		}
		if _, err := a.invokeRawJSON(ctx, cfg, command, method, path, nil); err != nil {
			a.pmLog("pm serve: dropping evidence ref %s: %s\n", ref, evidenceRefDropReason(err))
			continue
		}
		kept = append(kept, ref)
	}
	return kept
}

func evidenceRefLookup(ref string) (command, method, path, dropReason string) {
	kind, _, ok := strings.Cut(ref, ":")
	if !ok || strings.TrimSpace(kind) == "" {
		return "", "", "", "malformed typed ref"
	}
	escaped := url.PathEscape(ref)
	switch strings.ToLower(kind) {
	case "decision":
		return "pm decisions get", http.MethodGet, "/pm/decisions/" + escaped, ""
	case "artifact":
		return "artifacts get", http.MethodGet, "/artifacts/" + escaped, ""
	case "event":
		return "events get", http.MethodGet, "/events/" + escaped, ""
	case "work", "card":
		return "work get", http.MethodGet, "/work/" + escaped, ""
	case "topic":
		return "topics get", http.MethodGet, "/topics/" + escaped, ""
	case "document":
		return "docs get", http.MethodGet, "/docs/" + escaped, ""
	default:
		return "", "", "", "unsupported evidence kind"
	}
}

func evidenceRefDropReason(err error) string {
	var typed *errnorm.Error
	if errors.As(err, &typed) {
		msg := strings.TrimSpace(typed.Message)
		switch {
		case typed.Code != "" && msg != "" && typed.Code != "remote_error":
			return typed.Code + ": " + msg
		case msg != "":
			return msg
		case typed.Code != "":
			return typed.Code
		}
	}
	if err == nil {
		return "unresolvable"
	}
	return err.Error()
}

const typedRefTrailPunct = ".,;:)]\"'"
