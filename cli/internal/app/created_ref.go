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

// ensureCreatedRef stamps result.ref for a single new resource and returns the
// text to print before the envelope. Batch creates print one ref per line and
// stamp result.ref only when the response already has a batch id.
func ensureCreatedRef(command string, root map[string]any) string {
	if root == nil || root["dry_run"] == true || !emitsCreatedRef(command) {
		return ""
	}
	if isBatchCreate(command) {
		if id := strings.TrimSpace(anyString(root["batch_id"])); id != "" && strings.TrimSpace(anyString(root["ref"])) == "" {
			root["ref"] = id
		}
		return strings.Join(batchCreatedRefs(command, root), "\n")
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

func isBatchCreate(command string) bool {
	return command == "ask batch" || command == "boards cards create-batch"
}

func batchCreatedRefs(command string, root map[string]any) []string {
	switch command {
	case "ask batch":
		return askBatchRefs(root)
	case "boards cards create-batch":
		return refsFromCards(root["cards"])
	default:
		return nil
	}
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
	eachObject(root["asks"], func(item map[string]any) {
		if ref := anyString(item["ask_id"]); ref != "" {
			refs = append(refs, ref)
		}
	})
	return refs
}

func refsFromCards(items any) []string {
	var refs []string
	eachObject(items, func(item map[string]any) {
		if nested := asMap(item["card"]); len(nested) > 0 {
			item = nested
		}
		if ref := resourceRef(item, "card"); ref != "" {
			refs = append(refs, ref)
		}
	})
	return refs
}

func eachObject(raw any, fn func(map[string]any)) {
	switch items := raw.(type) {
	case []any:
		for _, raw := range items {
			if item := asMap(raw); item != nil {
				fn(item)
			}
		}
	case []map[string]any:
		for _, item := range items {
			if item != nil {
				fn(item)
			}
		}
	}
}
