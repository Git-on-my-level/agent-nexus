// Package workspaceconfig owns user-global CLI workspace preferences. It has
// no server or control-plane dependency and reads only local enrollment records.
package workspaceconfig

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"agent-nexus-cli/internal/filelock"
	"agent-nexus-cli/internal/hostidentity"
)

const Filename = "workspaces.json"

type File struct {
	Default string            `json:"default,omitempty"`
	Aliases map[string]string `json:"aliases"`
	Rules   map[string]string `json:"directory_rules,omitempty"`
}

type Workspace struct {
	Alias       string `json:"alias"`
	BaseURL     string `json:"base_url"`
	WorkspaceID string `json:"workspace_id,omitempty"`
	Enrolled    bool   `json:"enrolled"`
}

type Catalog struct {
	File       File
	Workspaces []Workspace
}

func Load(configDir string) (Catalog, error) {
	f := File{Aliases: map[string]string{}, Rules: map[string]string{}}
	data, err := os.ReadFile(filepath.Join(configDir, Filename))
	if err != nil && !os.IsNotExist(err) {
		return Catalog{}, err
	}
	if err == nil {
		if err := json.Unmarshal(data, &f); err != nil {
			return Catalog{}, fmt.Errorf("decode %s: %w", Filename, err)
		}
	}
	if f.Aliases == nil {
		f.Aliases = map[string]string{}
	}
	if f.Rules == nil {
		f.Rules = map[string]string{}
	}
	for alias, base := range f.Aliases {
		if !aliasPattern.MatchString(alias) {
			return Catalog{}, fmt.Errorf("invalid workspace alias %q", alias)
		}
		if err := ValidateURL(base); err != nil {
			return Catalog{}, err
		}
	}
	hosts, err := hostidentity.ListAt(configDir)
	if err != nil {
		return Catalog{}, err
	}
	// Sorting by URL makes collision suffixes independent of filesystem order.
	sort.Slice(hosts, func(i, j int) bool { return hosts[i].BaseURL < hosts[j].BaseURL })
	for _, host := range hosts {
		if err := ValidateURL(host.BaseURL); err != nil {
			return Catalog{}, err
		}
		f.AddAlias(DerivedAlias(host), host.BaseURL)
	}
	catalog := Catalog{File: f, Workspaces: []Workspace{}}
	aliases := []string{}
	for alias := range f.Aliases {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	for _, alias := range aliases {
		ws := Workspace{Alias: alias, BaseURL: f.Aliases[alias]}
		for _, h := range hosts {
			if sameURL(ws.BaseURL, h.BaseURL) {
				ws.Enrolled = true
				ws.WorkspaceID = h.WorkspaceID
				break
			}
		}
		catalog.Workspaces = append(catalog.Workspaces, ws)
	}
	return catalog, nil
}

var aliasPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,127}$`)
var aliasReplace = regexp.MustCompile(`[^a-z0-9_-]+`)

func DerivedAlias(h hostidentity.Host) string {
	value := h.WorkspaceSlug
	if value == "" {
		u, _ := url.Parse(h.BaseURL)
		if u != nil {
			value = strings.Trim(filepath.Base(strings.TrimRight(u.Path, "/")), "/.")
		}
		if value == "" {
			value = strings.TrimPrefix(h.WorkspaceID, "ws_")
		}
		if value == "" && u != nil {
			value = u.Hostname()
		}
	}

	value = strings.Trim(aliasReplace.ReplaceAllString(strings.ToLower(value), "-"), "-_")
	if len(value) > 110 {
		value = strings.TrimRight(value[:110], "-_")
	}
	if value == "" {
		value = "workspace"
	}
	return value
}

func sameURL(a, b string) bool { return strings.TrimRight(a, "/") == strings.TrimRight(b, "/") }

// AddAlias preserves existing aliases, and never rebinds an alias on collision.
func (f *File) AddAlias(preferred, base string) string {
	existing := []string{}
	for alias, value := range f.Aliases {
		if sameURL(value, base) {
			existing = append(existing, alias)
		}
	}
	if len(existing) > 0 {
		sort.Strings(existing)
		return existing[0]
	}
	alias := preferred
	for n := 2; f.Aliases[alias] != ""; n++ {
		alias = fmt.Sprintf("%s-%d", preferred, n)
	}
	f.Aliases[alias] = strings.TrimRight(base, "/")
	return alias
}

func ValidateURL(base string) error {
	u, err := url.Parse(base)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("workspace must be an alias or an absolute http(s) base URL: %q", base)
	}
	return nil
}

func (c Catalog) Target(target string) (string, error) {
	if base, ok := c.File.Aliases[target]; ok {
		return base, nil
	}
	if err := ValidateURL(target); err != nil {
		return "", fmt.Errorf("unknown workspace %q; run anx config workspaces to list aliases", target)
	}
	return strings.TrimRight(target, "/"), nil
}

// Update serializes local preference writes and replaces the file atomically.
// Reads and auto-discovery never write configuration.
func Update(configDir string, change func(*Catalog) error) error {
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return err
	}
	lock, err := filelock.OpenNoFollow(filepath.Join(configDir, "workspaces.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := filelock.Lock(lock); err != nil {
		return err
	}
	defer filelock.Unlock(lock)
	c, err := Load(configDir)
	if err != nil {
		return err
	}
	if err := change(&c); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c.File, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(configDir, ".workspaces-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(configDir, Filename))
}

// NormalizeGlob requires user-global paths. ** matches zero or more complete
// path components; *, ?, and character classes use filepath.Match semantics.
func NormalizeGlob(pattern, home string) (string, error) {
	if pattern == "~" || strings.HasPrefix(pattern, "~/") {
		if home == "" {
			return "", fmt.Errorf("home directory unavailable for %q", pattern)
		}
		pattern = filepath.Join(home, strings.TrimPrefix(pattern, "~"))
	}
	if !filepath.IsAbs(pattern) {
		return "", fmt.Errorf("directory glob must be absolute or start with ~/; quote globs to prevent shell expansion")
	}
	pattern = filepath.Clean(pattern)
	for _, segment := range strings.Split(pattern, string(filepath.Separator)) {
		if segment == "**" {
			continue
		}
		if strings.Contains(segment, "**") {
			return "", fmt.Errorf("** must be a complete path component")
		}
		if _, err := filepath.Match(segment, ""); err != nil {
			return "", fmt.Errorf("invalid directory glob %q: %w", pattern, err)
		}
	}
	return pattern, nil
}

// canonicalPath resolves existing ancestors, preserving not-yet-created suffixes.
// This lets a logical home path (/var on macOS, or a symlinked work directory)
// match the physical cwd without requiring every globbed directory to exist.
func canonicalPath(path string) string {
	suffix := []string{}
	prefix := filepath.Clean(path)
	for {
		if real, err := filepath.EvalSymlinks(prefix); err == nil {
			parts := append([]string{onDiskPath(real)}, suffix...)
			return filepath.Join(parts...)
		}
		parent := filepath.Dir(prefix)
		if parent == prefix {
			return filepath.Clean(path)
		}
		suffix = append([]string{filepath.Base(prefix)}, suffix...)
		prefix = parent
	}
}

// onDiskPath recovers the spelling of an existing path component by component.
// SameFile, rather than case folding, makes differently spelled names equivalent
// only when this filesystem resolves them to the same directory. EvalSymlinks
// alone preserves caller spelling on case-insensitive volumes (including APFS).
func onDiskPath(path string) string {
	root := filepath.VolumeName(path) + string(filepath.Separator)
	relative := strings.TrimPrefix(path, root)
	parent := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		if component == "" {
			continue
		}
		requested := filepath.Join(parent, component)
		entries, err := os.ReadDir(parent)
		if err != nil {
			return path
		}
		exact := false
		for _, entry := range entries {
			if entry.Name() == component {
				exact = true
				break
			}
		}
		if exact {
			parent = requested
			continue
		}
		info, err := os.Stat(requested)
		if err != nil {
			return path
		}
		found := false
		for _, entry := range entries {
			candidate := filepath.Join(parent, entry.Name())
			candidateInfo, err := entry.Info()
			if err == nil && os.SameFile(info, candidateInfo) {
				parent = candidate
				found = true
				break
			}
		}
		if !found {
			return path
		}
	}
	return parent
}

func canonicalGlob(pattern string) string {
	prefix := pattern
	suffix := []string{}
	for strings.ContainsAny(prefix, "*?[") {
		suffix = append([]string{filepath.Base(prefix)}, suffix...)
		prefix = filepath.Dir(prefix)
	}
	return filepath.Join(append([]string{canonicalPath(prefix)}, suffix...)...)
}

func matchSegments(pattern, path []string) bool {
	if len(pattern) == 0 {
		return len(path) == 0
	}
	if pattern[0] == "**" {
		if matchSegments(pattern[1:], path) {
			return true
		}
		return len(path) > 0 && matchSegments(pattern, path[1:])
	}
	if len(path) == 0 {
		return false
	}
	ok, _ := filepath.Match(pattern[0], path[0])
	return ok && matchSegments(pattern[1:], path[1:])
}

// MatchingRule ranks by literal prefix, then total literal length; ties use
// lexical order so a config file's map iteration never affects routing.
func (c Catalog) MatchingRule(cwd, home string) (string, string, error) {
	type rule struct {
		raw              string
		prefix, literals int
	}
	rules := []rule{}
	cwd = canonicalPath(cwd)
	for raw := range c.File.Rules {
		normalized, err := NormalizeGlob(raw, home)
		if err != nil {
			return "", "", err
		}
		normalized = canonicalGlob(normalized)
		if !matchSegments(strings.Split(normalized, string(filepath.Separator)), strings.Split(filepath.Clean(cwd), string(filepath.Separator))) {
			continue
		}
		prefix := len(normalized)
		if i := strings.IndexAny(normalized, "*?["); i >= 0 {
			prefix = i
		}
		literals := len(strings.Map(func(r rune) rune {
			if strings.ContainsRune("*?[]", r) {
				return -1
			}
			return r
		}, normalized))
		rules = append(rules, rule{raw, prefix, literals})
	}
	sort.Slice(rules, func(i, j int) bool {
		if rules[i].prefix != rules[j].prefix {
			return rules[i].prefix > rules[j].prefix
		}
		if rules[i].literals != rules[j].literals {
			return rules[i].literals > rules[j].literals
		}
		return rules[i].raw < rules[j].raw
	})
	if len(rules) == 0 {
		return "", "", nil
	}
	raw := rules[0].raw
	base, err := c.Target(c.File.Rules[raw])
	return raw, base, err
}

func (c Catalog) Resolve(cwd, home string) (string, string, error) {
	rule, base, err := c.MatchingRule(cwd, home)
	if err != nil {
		return "", "", err
	}
	if rule != "" {
		return base, "config:directory-rule:" + rule, nil
	}
	if c.File.Default != "" {
		base, err := c.Target(c.File.Default)
		return base, "config:default", err
	}
	enrolled := map[string]bool{}
	for _, ws := range c.Workspaces {
		if ws.Enrolled {
			enrolled[strings.TrimRight(ws.BaseURL, "/")] = true
		}
	}
	if len(enrolled) == 1 {
		for base := range enrolled {
			return base, "bridge:auto-single", nil
		}
	}
	if len(enrolled) > 1 {
		return "", "", &AmbiguousError{Workspaces: c.Workspaces}
	}
	return "", "default", nil
}

type AmbiguousError struct{ Workspaces []Workspace }

func (e *AmbiguousError) Error() string {
	choices := []string{}
	for _, ws := range e.Workspaces {
		if ws.Enrolled {
			choices = append(choices, fmt.Sprintf("%s (%s): anx config use %s", ws.Alias, ws.BaseURL, ws.Alias))
		}
	}
	return "multiple workspaces are enrolled and none is selected; choose a default with " + strings.Join(choices, "; ") + "; or inspect anx config workspaces"
}
