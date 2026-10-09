package visualreport

import (
	"fmt"
	"strings"
)

func (v *validator) layout(raw any, panelIDs map[string]bool) {
	usedPanels, usedTabs := map[string]bool{}, map[string]bool{}
	count := 0
	var visit func(any, string, int)
	text := func(value any, path string, max int) {
		s, ok := value.(string)
		if !ok || strLen(s) == 0 || strLen(s) > max || strings.TrimFunc(s, jsWhitespace) == "" {
			v.add(path, fmt.Sprintf("requires nonempty text of at most %d characters", max))
		}
	}
	children := func(value any, path string, depth int) {
		items, ok := value.([]any)
		if !ok || len(items) == 0 || len(items) > 32 {
			v.add(path, "requires 1–32 children")
			return
		}
		for i, child := range items {
			visit(child, fmt.Sprintf("%s[%d]", path, i), depth)
		}
	}
	visit = func(value any, path string, depth int) {
		count++
		if count > 100 || depth > 6 {
			v.add(path, "layout exceeds 100 nodes or 6 levels")
			return
		}
		node, ok := value.(map[string]any)
		if !ok {
			v.add(path, "must be an object")
			return
		}
		if span, exists := node["span"]; exists {
			n, ok := numeric(span)
			if !ok || n != float64(int(n)) || n < 1 || n > 4 {
				v.add(path, "span must be 1–4")
			}
		}
		switch node["type"] {
		case "panel":
			m, ok := object(node, path, []string{"type", "panel_id"}, []string{"span"}, v.add)
			if !ok {
				return
			}
			id, _ := m["panel_id"].(string)
			if !panelIDs[id] {
				v.add(path, "references a missing panel")
			}
			if usedPanels[id] {
				v.add(path, "panel references must be unique")
			}
			usedPanels[id] = true
		case "stack":
			m, ok := object(node, path, []string{"type", "children"}, []string{"span"}, v.add)
			if ok {
				children(m["children"], path+".children", depth+1)
			}
		case "grid":
			// columns is an optional cap, not a requirement: a grid that names
			// none flows its children by available width and content.
			m, ok := object(node, path, []string{"type", "children"}, []string{"span", "columns"}, v.add)
			if !ok {
				return
			}
			if raw, exists := m["columns"]; exists {
				n, ok := numeric(raw)
				if !ok || n != float64(int(n)) || n != 2 && n != 3 && n != 4 {
					v.add(path, "columns must be 2, 3, or 4")
				}
			}
			children(m["children"], path+".children", depth+1)
		case "section", "disclosure":
			optional := []string{"span", "description"}
			if node["type"] == "disclosure" {
				optional = []string{"span", "open"}
			}
			m, ok := object(node, path, []string{"type", "title", "children"}, optional, v.add)
			if !ok {
				return
			}
			text(m["title"], path+".title", 200)
			if x, ok := m["description"]; ok {
				text(x, path+".description", 2000)
			}
			if x, ok := m["open"]; ok {
				if _, good := x.(bool); !good {
					v.add(path, "open must be a boolean")
				}
			}
			children(m["children"], path+".children", depth+1)
		case "tabs":
			m, ok := object(node, path, []string{"type", "id", "items"}, []string{"span"}, v.add)
			if !ok {
				return
			}
			id, _ := m["id"].(string)
			if !identifierPattern.MatchString(id) || usedTabs[id] {
				v.add(path, "tab groups need unique identifiers")
			}
			usedTabs[id] = true
			items, ok := m["items"].([]any)
			if !ok || len(items) == 0 || len(items) > 8 {
				v.add(path, "tabs require 1–8 items")
				return
			}
			itemIDs := map[string]bool{}
			for i, raw := range items {
				p := fmt.Sprintf("%s.items[%d]", path, i)
				item, ok := object(raw, p, []string{"id", "label", "children"}, nil, v.add)
				if !ok {
					continue
				}
				id, _ := item["id"].(string)
				if !identifierPattern.MatchString(id) || itemIDs[id] {
					v.add(p, "tab items need unique identifiers")
				}
				itemIDs[id] = true
				text(item["label"], p+".label", 200)
				children(item["children"], p+".children", depth+1)
			}
		default:
			v.add(path, "unsupported layout type")
		}
	}
	visit(raw, "root", 0)
}
