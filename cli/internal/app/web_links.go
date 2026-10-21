package app

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

// webURLDecorationTimeout bounds the best-effort handshake read that adds
// web_url to single-object results, so a slow core never stalls a command.
const webURLDecorationTimeout = 2 * time.Second

// webURLLookup caches one /meta/handshake read per base URL for an invocation.
type webURLLookup struct {
	baseURL string
	webURL  string
	err     error
}

var linkRefPrefixPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// workspaceWebURL reads the workspace web UI root from /meta/handshake at most
// once per App and base URL. An absent web_url returns "" with a nil error.
func (a *App) workspaceWebURL(ctx context.Context, cfg config.Resolved) (string, error) {
	base := strings.TrimSpace(cfg.BaseURL)
	if a.webURL != nil && a.webURL.baseURL == base {
		return a.webURL.webURL, a.webURL.err
	}
	lookup := &webURLLookup{baseURL: base}
	handshake, err := a.hostCall(ctx, cfg, "GET", "/meta/handshake", nil, nil)
	if err != nil {
		lookup.err = err
	} else if raw := strings.TrimSpace(anyString(handshake["web_url"])); raw != "" {
		lookup.webURL, lookup.err = normalizeWorkspaceWebURL(raw)
	}
	a.webURL = lookup
	return lookup.webURL, lookup.err
}

func normalizeWorkspaceWebURL(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errnorm.WithDetails(errnorm.New(errnorm.KindRemote, "invalid_response", "workspace handshake reported an invalid web_url; it must be an absolute http(s) URL without query or fragment"), map[string]any{"web_url": raw})
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

// webLinkForRef is the browser link for a typed ref. The web UI resolves
// handles, slugs and ids behind /r/<ref>.
func webLinkForRef(webURL, ref string) string {
	return strings.TrimRight(webURL, "/") + "/r/" + url.PathEscape(ref)
}

// parseLinkRef accepts only a typed `<prefix>:<value>` ref. Bare values and
// URLs are usage errors: a link must name what it points to.
func parseLinkRef(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	prefix, value, ok := strings.Cut(trimmed, ":")
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	value = strings.TrimSpace(value)
	if !ok || value == "" || !linkRefPrefixPattern.MatchString(prefix) || strings.HasPrefix(value, "//") {
		return "", errnorm.Usage("invalid_request", fmt.Sprintf("`anx link` requires typed refs such as card:<handle>, document:<handle> or event:<ask-id>; got %q", raw))
	}
	return prefix + ":" + value, nil
}

func parseLinkArgs(args []string) ([]string, error) {
	refs := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--" {
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return nil, errnorm.Usage("invalid_flags", fmt.Sprintf("unknown flag %q for `anx link`; usage: anx link <ref>...", arg))
		}
		ref, err := parseLinkRef(arg)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	if len(refs) == 0 {
		return nil, errnorm.Usage("invalid_args", "usage: anx link <ref>...; pass at least one typed ref")
	}
	return refs, nil
}

func webURLMissingError() error {
	err := errnorm.Local("needs_context", "this workspace does not report a web UI URL; set ANX_WEB_BASE_URL on the workspace core to the absolute URL of the workspace web UI root (for example https://example.com/o/<org>/w/<workspace>)")
	err.Hint = "Configure ANX_WEB_BASE_URL on the workspace core and restart it. The CLI does not derive web links from the API base URL."
	return errnorm.WithDetails(err, map[string]any{"missing": "web_url", "configure_env": "ANX_WEB_BASE_URL"})
}

func (a *App) runLinkCommand(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	if len(args) == 0 || (len(args) == 1 && isHelpToken(args[0])) {
		return &commandResult{Text: linkCommandHelpText(), Data: map[string]any{"help_text": linkCommandHelpText()}}, nil
	}
	refs, err := parseLinkArgs(args)
	if err != nil {
		return nil, err
	}
	webURL, err := a.workspaceWebURL(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if webURL == "" {
		return nil, webURLMissingError()
	}
	links := make([]any, 0, len(refs))
	lines := make([]string, 0, len(refs))
	for _, ref := range refs {
		link := webLinkForRef(webURL, ref)
		links = append(links, map[string]any{"ref": ref, "web_url": link})
		lines = append(lines, link)
	}
	return &commandResult{Text: strings.Join(lines, "\n"), Data: map[string]any{"links": links}}, nil
}

// primaryWebRef names the one typed ref a single-object result is about.
// Commands absent here never trigger the handshake read.
func primaryWebRef(command string, root map[string]any) string {
	if root == nil || root["dry_run"] == true {
		return ""
	}
	object := func(key, prefix string) string {
		return resourceRef(asMap(root[key]), prefix)
	}
	typed := func(raw, prefix string) string {
		raw = strings.TrimSpace(raw)
		if raw == "" || strings.Contains(raw, ":") {
			return raw
		}
		return prefix + ":" + raw
	}
	var ref string
	switch command {
	case "work get", "work create", "context", "work context":
		ref = firstNonEmpty(object("work", "card"), anyString(root["ref"]))
	case "cards get", "cards create", "cards revise", "cards move", "cards assign", "cards resolve", "cards reopen", "cards patch":
		ref = firstNonEmpty(object("card", "card"), anyString(root["ref"]))
	case "cards message", "cards reply":
		ref = typed(anyString(root["card_id"]), "card")
	case "docs get", "docs create", "docs put", "docs revise", "docs patch":
		ref = firstNonEmpty(object("document", "document"), anyString(root["ref"]))
	case "docs message", "docs reply", "topics message", "topics reply":
		kind := canonicalResourceKind(anyString(root["subject_kind"]))
		if kind == "document" || kind == "topic" {
			ref = typed(anyString(root["subject_id"]), kind)
		}
	case "topics get", "topics create":
		ref = firstNonEmpty(object("topic", "topic"), anyString(root["ref"]))
	case "boards get", "boards create":
		ref = firstNonEmpty(object("board", "board"), anyString(root["ref"]))
	case "artifacts create":
		ref = firstNonEmpty(object("artifact", "artifact"), anyString(root["ref"]))
	case "ask", "review", "escalate":
		ref = anyString(root["ask_id"])
	}
	ref = strings.TrimSpace(ref)
	if _, err := parseLinkRef(ref); err != nil {
		return ""
	}
	return ref
}

// decorateWebURL adds result.web_url for the primary ref of a single-object
// result and returns that link. It is best effort: a failed or slow handshake,
// or a workspace without web_url, leaves the result unchanged.
func (a *App) decorateWebURL(cfg config.Resolved, command string, value any) string {
	root, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	if _, exists := root["web_url"]; exists {
		return ""
	}
	ref := primaryWebRef(command, root)
	if ref == "" {
		return ""
	}
	timeout := webURLDecorationTimeout
	if cfg.Timeout > 0 && cfg.Timeout < timeout {
		timeout = cfg.Timeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	webURL, err := a.workspaceWebURL(ctx, cfg)
	if err != nil || webURL == "" {
		return ""
	}
	link := webLinkForRef(webURL, ref)
	root["web_url"] = link
	return link
}

func linkCommandHelpText() string {
	return strings.TrimSpace(`Local Help: link

- Kind: local helper
- Summary: Print one browser URL per typed ref, one per line, for sharing with a human.
- Requires: the workspace core reports web_url in /meta/handshake (set ANX_WEB_BASE_URL on the core).
- JSON shape: result.links[] with exactly ref and web_url.
- Examples:
  - anx link card:<card-handle>
  - anx link document:<doc-handle> event:<ask-id>
  - anx --json link card:<card-handle>`)
}
