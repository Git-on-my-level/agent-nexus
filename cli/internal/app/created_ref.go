package app

import "strings"

// createdRef returns the new resource's ref for create commands so text
// output can lead with it ("created card:launch"). Dry runs return "".
func createdRef(command string, value any) string {
	root := asMap(value)
	if root == nil || root["dry_run"] == true {
		return ""
	}
	switch command {
	case "ask", "review", "escalate":
		return anyString(root["ask_id"])
	case "ask batch":
		var refs []string
		for _, raw := range asSlice(root["asks"]) {
			if ref := anyString(asMap(raw)["ask_id"]); ref != "" {
				refs = append(refs, ref)
			}
		}
		return strings.Join(refs, " ")
	case "note":
		return anyString(root["note_ref"])
	case "work create":
		return firstNonEmpty(anyString(asMap(root["work"])["ref"]), anyString(root["ref"]))
	case "cards create":
		return firstNonEmpty(anyString(asMap(root["card"])["ref"]), anyString(root["ref"]))
	case "docs create":
		return firstNonEmpty(anyString(asMap(root["document"])["ref"]), anyString(root["ref"]))
	case "topics create":
		return firstNonEmpty(anyString(asMap(root["topic"])["ref"]), anyString(root["ref"]))
	case "boards create":
		return firstNonEmpty(anyString(asMap(root["board"])["ref"]), anyString(root["ref"]))
	}
	return ""
}
