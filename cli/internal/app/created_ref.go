package app

import "strings"

// createdRef is the public ref of a resource a create command just wrote.
// Text output prints that ref as its first line. Dry runs return "".
func createdRef(command string, value any) string {
	root := asMap(value)
	if root == nil || root["dry_run"] == true {
		return ""
	}
	switch command {
	case "ask", "review", "escalate":
		return anyString(root["ask_id"])
	case "ask batch":
		return joinRefs(askBatchRefs(root))
	case "note":
		return anyString(root["note_ref"])
	case "work create":
		return firstNonEmpty(resourceRef(asMap(root["work"]), "card"), anyString(root["ref"]))
	case "cards create":
		return firstNonEmpty(resourceRef(asMap(root["card"]), "card"), anyString(root["ref"]))
	case "docs create":
		return firstNonEmpty(resourceRef(asMap(root["document"]), "document"), anyString(root["ref"]))
	case "topics create":
		return firstNonEmpty(resourceRef(asMap(root["topic"]), "topic"), anyString(root["ref"]))
	case "boards create":
		return firstNonEmpty(resourceRef(asMap(root["board"]), "board"), anyString(root["ref"]))
	case "artifacts create":
		return firstNonEmpty(resourceRef(asMap(root["artifact"]), "artifact"), anyString(root["ref"]))
	case "events create":
		return firstNonEmpty(resourceRef(asMap(root["event"]), "event"), anyString(root["ref"]))
	case "boards cards create-batch":
		return joinRefs(refsFromCards(asSlice(root["cards"])))
	case "plan step add":
		return strings.TrimSpace(anyString(root["ref"]))
	default:
		return ""
	}
}

func emitsCreatedRef(command string) bool {
	switch command {
	case "ask", "review", "escalate", "ask batch", "note",
		"work create", "cards create", "docs create", "topics create", "boards create",
		"artifacts create", "events create", "boards cards create-batch", "plan step add":
		return true
	default:
		return false
	}
}

// ensureCreatedRef stamps result.ref when the create command did not already
// return one, and returns the ref text output should print first.
func ensureCreatedRef(command string, root map[string]any) string {
	if root == nil || root["dry_run"] == true || !emitsCreatedRef(command) {
		return ""
	}
	ref := createdRef(command, root)
	if ref == "" {
		return ""
	}
	if strings.TrimSpace(anyString(root["ref"])) == "" {
		root["ref"] = ref
	}
	return ref
}

func resourceRef(item map[string]any, prefix string) string {
	if item == nil {
		return ""
	}
	if ref := strings.TrimSpace(anyString(item["ref"])); ref != "" {
		return ref
	}
	id := strings.TrimSpace(anyString(item["id"]))
	if id == "" {
		return ""
	}
	if strings.Contains(id, ":") {
		return id
	}
	if prefix == "" {
		return id
	}
	return prefix + ":" + id
}

func askBatchRefs(root map[string]any) []string {
	var refs []string
	for _, raw := range asSlice(root["asks"]) {
		if ref := anyString(asMap(raw)["ask_id"]); ref != "" {
			refs = append(refs, ref)
		}
	}
	return refs
}

func refsFromCards(items []any) []string {
	var refs []string
	for _, raw := range items {
		item := asMap(raw)
		if nested := asMap(item["card"]); len(nested) > 0 {
			item = nested
		}
		if ref := resourceRef(item, "card"); ref != "" {
			refs = append(refs, ref)
		}
	}
	return refs
}

func joinRefs(refs []string) string {
	var out []string
	for _, ref := range refs {
		ref = strings.TrimSpace(ref)
		if ref != "" {
			out = append(out, ref)
		}
	}
	return strings.Join(out, " ")
}
