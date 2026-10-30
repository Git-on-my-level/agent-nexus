package app

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"unicode"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

// The tree is server-owned. Paths are resolved for each invocation, never saved
// as identity, and advisory home suggestions never trigger a mutation.
type workTreeCommand struct {
	name, ref, under, reason, after, requestKey string
	root, unsorted, includeDone                 bool
	depth                                       int
}

func isWorkTreeCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == "mv" || args[0] == "tree" || args[0] == "where" {
		return true
	}
	return len(args) > 1 && args[0] == "work" && (args[1] == "move" || args[1] == "tree" || args[1] == "ls")
}

func parseWorkTreeCommand(args []string) (workTreeCommand, error) {
	p := workTreeCommand{depth: 2}
	width := 1
	p.name = args[0]
	if args[0] == "work" {
		width = 2
		p.name = "work " + args[1]
	}
	fs := newSilentFlagSet(p.name)
	switch p.name {
	case "mv", "work move":
		fs.StringVar(&p.under, "under", "", "Parent card ref or path")
		fs.BoolVar(&p.root, "root", false, "Move to top level")
		fs.BoolVar(&p.unsorted, "unsorted", false, "Move to Unsorted")
		fs.StringVar(&p.reason, "reason", "", "Audit reason (required for root or moving an initiative under a parent)")
		fs.StringVar(&p.after, "after", "", "Preceding sibling plan step id")
		fs.StringVar(&p.requestKey, "request-key", "", "Retain the same key and body until confirmed")
	case "tree", "work tree", "work ls":
		if p.name != "work ls" {
			fs.IntVar(&p.depth, "depth", 2, "Expansion depth (1..3)")
		}
		fs.BoolVar(&p.includeDone, "include-done", false, "Include completed cards")
	}
	tail := args[width:]
	var positions []string
	for len(tail) > 0 && !strings.HasPrefix(tail[0], "-") {
		positions = append(positions, tail[0])
		tail = tail[1:]
	}
	if err := fs.Parse(tail); err != nil {
		return p, errnorm.Usage("invalid_flags", err.Error())
	}
	positions = append(positions, fs.Args()...)
	if len(positions) > 1 {
		return p, errnorm.Usage("invalid_args", "expected a single ref, path or quoted text")
	}
	if len(positions) == 1 {
		p.ref = strings.TrimSpace(positions[0])
	}
	if p.ref == "" && p.name != "tree" && p.name != "work tree" {
		return p, errnorm.Usage("invalid_args", "a ref or path is required")
	}
	if p.depth < 1 || p.depth > 3 {
		return p, errnorm.Usage("invalid_request", "--depth must be between 1 and 3")
	}
	p.under = strings.TrimSpace(p.under)
	p.after = strings.TrimSpace(p.after)
	if p.name == "mv" || p.name == "work move" {
		choices := 0
		if p.under != "" {
			choices++
		}
		if p.root {
			choices++
		}
		if p.unsorted {
			choices++
		}
		if choices != 1 {
			return p, errnorm.Usage("invalid_request", "choose exactly one of --under <ref|path>, --root, --unsorted")
		}
		p.reason = strings.TrimSpace(p.reason)
		if len([]rune(p.reason)) > 2000 || (p.root && p.reason == "") {
			return p, errnorm.Usage("invalid_request", "--reason must be 1..2000 characters; it is required with --root and when moving an initiative (the server checks)")
		}
	}
	for _, ref := range []string{p.ref, p.under} {
		if p.name == "where" && ref == p.ref && !strings.HasPrefix(ref, "card:") && !isWorkPath(ref) {
			continue
		}
		if strings.ContainsAny(ref, "\\?#%") || strings.ContainsFunc(ref, unicode.IsControl) || ref == "." || ref == ".." || (strings.Contains(ref, "/") && !validWorkPath(ref)) {
			return p, errnorm.Usage("invalid_request", "expected a card ref or work path, not a URL")
		}
	}
	return p, nil
}

func validWorkPath(ref string) bool {
	parts := strings.Split(ref, "/")
	if len(parts) < 2 || len(parts) > 4 || (parts[0] != "initiatives" && parts[0] != "unsorted") {
		return false
	}
	for _, part := range parts[1:] {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "\\?#%") || strings.ContainsFunc(part, unicode.IsSpace) || strings.ContainsFunc(part, unicode.IsControl) {
			return false
		}
	}
	return true
}

func isWorkPath(ref string) bool {
	return strings.Contains(ref, "/") && !strings.HasPrefix(ref, "card:")
}

func (a *App) resolveWorkPath(ctx context.Context, cfg config.Resolved, ref string) (string, error) {
	if !isWorkPath(ref) {
		return ref, nil
	}
	if !validWorkPath(ref) {
		return "", errnorm.Usage("invalid_request", "expected initiatives/ or unsorted/ followed by 1..3 segments")
	}
	result, err := a.invokeRawJSON(ctx, cfg, "work resolve", "GET", "/work/resolve?"+url.Values{"path": {ref}}.Encode(), nil)
	if err != nil {
		return "", err
	}
	resolved := anyString(commandResultBody(result)["ref"])
	if !strings.HasPrefix(resolved, "card:") {
		return "", errnorm.New(errnorm.KindRemote, "invalid_response", "path resolution returned no canonical card ref")
	}
	return resolved, nil
}

func (a *App) runWorkTreeCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	p, err := parseWorkTreeCommand(args)
	if err != nil {
		return nil, p.name, err
	}
	p.ref, err = a.resolveWorkPath(ctx, cfg, p.ref)
	if err != nil {
		return nil, p.name, err
	}
	method, path := "GET", "/work/tree"
	query := url.Values{}
	var body any
	switch p.name {
	case "mv", "work move":
		p.under, err = a.resolveWorkPath(ctx, cfg, p.under)
		if err != nil {
			return nil, p.name, err
		}
		method, path = "POST", "/work/"+url.PathEscape(p.ref)+"/move"
		object := map[string]any{}
		if p.under != "" {
			object["under"] = p.under
		}
		if p.root {
			object["root"] = true
		}
		if p.unsorted {
			object["unsorted"] = true
		}
		if p.reason != "" {
			object["reason"] = p.reason
		}
		if p.after != "" {
			object["after"] = p.after
		}
		if p.requestKey != "" {
			object["request_key"] = p.requestKey
		}
		if err := ensureGroupingRequestKey(object); err != nil {
			return nil, p.name, err
		}
		body = object
	case "where":
		path = "/work/where"
		if strings.HasPrefix(p.ref, "card:") {
			query.Set("ref", p.ref)
		} else {
			query.Set("q", p.ref)
		}
	default:
		if p.ref != "" {
			query.Set("root", p.ref)
		}
		query.Set("depth", fmt.Sprint(p.depth))
		query.Set("include_done", fmt.Sprint(p.includeDone))
	}
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	result, err := a.invokeRawJSON(ctx, cfg, p.name, method, path, body)
	if err != nil {
		// A tree 404 may mean a missing root or an older server. Explain version
		// skew while preserving the not-found exit code in both cases.
		if strings.HasPrefix(path, "/work/tree") && (errnorm.ExitCode(err) == 3 || asMap(errnorm.Normalize(err).Details)["status"] == 404) {
			return nil, p.name, errnorm.New(errnorm.KindRemote, "not_found", "Work tree or root not found; update your anx server/CLI to matching versions if these routes are unavailable")
		}
		return result, p.name, err
	}
	payload := commandResultBody(result)
	if payload == nil {
		return nil, p.name, errnorm.New(errnorm.KindRemote, "invalid_response", "work API returned an incomplete response")
	}
	switch p.name {
	case "mv", "work move":
		if anyString(asMap(payload["work"])["ref"]) == "" {
			return nil, p.name, errnorm.New(errnorm.KindRemote, "invalid_response", "move response has no work item")
		}
		result.Text = formatWorkCommandText("work get", payload)
	case "where":
		result.Text = formatWorkWhere(payload)
	default:
		if _, ok := payload["truncated"].(bool); !ok {
			return nil, p.name, errnorm.New(errnorm.KindRemote, "invalid_response", "work tree response is incomplete; update your anx server/CLI to matching versions")
		}
		if p.ref != "" && asMap(payload["node"]) == nil {
			return nil, p.name, errnorm.New(errnorm.KindRemote, "invalid_response", "work tree response has no root node; update your anx server/CLI to matching versions")
		}
		if p.ref == "" {
			_, roots := payload["initiatives"].([]any)
			_, tray := payload["unsorted"].([]any)
			if !roots || !tray {
				return nil, p.name, errnorm.New(errnorm.KindRemote, "invalid_response", "work tree response has no initiatives or Unsorted tray; update your anx server/CLI to matching versions")
			}
		}
		if p.name == "work ls" {
			node := asMap(payload["node"])
			if node == nil {
				return nil, p.name, errnorm.New(errnorm.KindRemote, "invalid_response", "tree response has no root node")
			}
			payload = map[string]any{"children": node["children"], "has_more": node["has_more"], "truncated": payload["truncated"]}
			asMap(result.Data)["body"] = payload
		}
		result.Text = formatWorkTree(payload)
	}
	return result, p.name, nil
}

func formatWorkTree(body map[string]any) string {
	var b strings.Builder
	var render func(any, int)
	render = func(raw any, depth int) {
		if depth > 3 {
			return
		}
		node := asMap(raw)
		fmt.Fprintf(&b, "%s%s  %s  state=%s", strings.Repeat("  ", depth), anyString(node["ref"]), anyString(node["title"]), anyString(node["state"]))
		if path := anyString(node["path"]); path != "" {
			fmt.Fprintf(&b, "  path=%s", path)
		}
		if owner := anyString(node["owner"]); owner != "" {
			fmt.Fprintf(&b, "  owner=%s", owner)
		}
		b.WriteString("\n")
		for _, child := range asSlice(node["children"]) {
			render(child, depth+1)
		}
		if node["has_more"] == true {
			fmt.Fprintf(&b, "%s… more children\n", strings.Repeat("  ", depth+1))
		}
	}
	if node := asMap(body["node"]); node != nil {
		render(node, 0)
	} else if children, ok := body["children"]; ok {
		for _, node := range asSlice(children) {
			render(node, 0)
		}
		if body["has_more"] == true {
			b.WriteString("… more children\n")
		}
	} else {
		b.WriteString("Initiatives\n")
		for _, node := range asSlice(body["initiatives"]) {
			render(node, 1)
		}
		b.WriteString("Unsorted\n")
		for _, node := range asSlice(body["unsorted"]) {
			render(node, 1)
		}
	}
	if body["truncated"] == true {
		b.WriteString("Tree truncated; read a subtree for more detail.\n")
	}
	return strings.TrimSpace(b.String())
}

func formatWorkWhere(body map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "where ref=%s path=%s parent=%s initiative=%v needs_home=%v\n", anyString(body["ref"]), anyString(body["path"]), anyString(body["parent_ref"]), body["is_initiative"], body["needs_home"])
	for _, raw := range asSlice(body["suggestions"]) {
		home := asMap(raw)
		fmt.Fprintf(&b, "suggestion %s  %s  path=%s reasons=%s\n", anyString(home["ref"]), anyString(home["title"]), anyString(home["path"]), strings.Join(stringList(home["reasons"]), ","))
	}
	return strings.TrimSpace(b.String())
}

func workTreeHelp(topic string) (string, bool) {
	switch topic {
	case "mv", "work move":
		return "Usage: anx " + topic + " <ref|path> (--under <ref|path> | --root | --unsorted) [--reason <text>] [--after <step-id>] [--request-key <key>]\nMove the stored parent without changing plan steps. --reason (1..2000 characters) is required with --root and whenever an initiative moves (under a parent or to Unsorted). Paths are resolved at request time. Retain the original request key and body for replay; an unkeyed re-run of a promotion that already applied returns 409. Use --json for one envelope.", true
	case "tree", "work tree":
		return "Usage: anx " + topic + " [ref|path] [--depth <1..3>] [--include-done]\nRead initiatives and Unsorted or one subtree; default depth 2. Children are in creation order. Truncation is explicit; read a subtree for more detail. Use --json for one envelope.", true
	case "work ls":
		return "Usage: anx work ls <ref|path> [--include-done]\nRead immediate children using a depth-2 subtree. has_more and truncated retain the server's bounded-read markers. Use --json for one envelope.", true
	case "where":
		return "Usage: anx where <ref|path|quoted-text>\nInspect the current home and suggested parents. Confirm with anx work move <ref> --under <suggestion>. Suggestions never file work automatically. Use --json for one envelope.", true
	}
	return "", false
}

func init() {
	for _, topic := range []struct {
		path, summary, shape string
		flags                []localHelperFlag
	}{
		{"mv", "Move a card in the stored work tree.", "`work`", []localHelperFlag{{Name: "--under <ref|path>", Description: "Stored parent."}, {Name: "--root", Description: "Move to top level; requires reason."}, {Name: "--unsorted", Description: "Move to Unsorted."}, {Name: "--reason <text>", Description: "Audit reason, at most 2000 characters."}, {Name: "--after <step-id>", Description: "Sibling plan step id."}, {Name: "--request-key <key>", Description: "Retain the same key and body until confirmed."}}},
		{"tree", "Read the stored work tree.", "`initiatives`, `unsorted`, or `node`; `truncated`", []localHelperFlag{{Name: "--depth <n>", Description: "Expansion depth 1..3; default 2."}, {Name: "--include-done", Description: "Include completed cards."}}},
		{"work ls", "List immediate children of a ref or path.", "`children`, `has_more`, `truncated`", []localHelperFlag{{Name: "--include-done", Description: "Include completed cards."}}},
	} {
		localHelperTopics = append(localHelperTopics, localHelperTopic{Path: topic.path, Summary: topic.summary, JSONShape: topic.shape, Composition: "Server-owned parentage; paths are aliases and canonical refs remain stable.", Flags: topic.flags})
	}
}
