// Resource ref parsing, alias normalization and ref rewriting for
// cross-workspace moves.

package app

import (
	"strings"
)

func moveResourceRef(kind, id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	return kind + ":" + id
}

func moveRefID(ref string) string {
	_, suffix, ok := strings.Cut(moveNormalizeRef(ref), ":")
	if ok {
		return suffix
	}
	return strings.TrimSpace(ref)
}

func moveRewriteRef(ref string, mapping map[string]string) string {
	// Normalize aliases only for identity lookup; an unmapped reference remains
	// exactly as the caller supplied it.
	normalized := moveNormalizeRef(ref)
	if mapped := mapping[normalized]; mapped != "" {
		return moveNormalizeRef(mapped)
	}
	if mapped := mapping[ref]; mapped != "" {
		return moveNormalizeRef(mapped)
	}
	return ref
}

// Normalize aliases once before they enter the move plan. Core accepts doc:
// for documents and bare source URLs for cards; every move phase uses this form.
func moveNormalizeRef(ref string) string {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "http://") {
		return "card:" + ref
	}
	kind, suffix, ok := strings.Cut(ref, ":")
	if !ok {
		return ref
	}
	kind, suffix = strings.TrimSpace(kind), strings.TrimSpace(suffix)
	if kind == "doc" {
		kind = "document"
	}
	return kind + ":" + suffix
}

func moveRefKind(ref string) string {
	kind, _, ok := strings.Cut(moveNormalizeRef(ref), ":")
	if !ok {
		return ""
	}
	return kind
}

func moveRewriteRefs(refs any, mapping map[string]string) []string {
	values := moveStringList(refs)
	for index := range values {
		values[index] = moveRewriteRef(values[index], mapping)
	}
	return moveUnique(values)
}

func moveListRefs(workspace map[string]any, key, kind string) []string {
	refs := []string{}
	for _, raw := range asSlice(workspace[key]) {
		object := moveExtractObject(raw, strings.TrimSuffix(kind, "s"))
		ref := moveFieldString(object, "ref")
		if ref == "" {
			ref = moveResourceRef(kind, moveFieldString(object, "id"))
		}
		if ref != "" {
			refs = append(refs, ref)
		}
	}
	return refs
}
