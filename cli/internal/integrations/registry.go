// Package integrations contains optional harness adapters and event recipes.
// Nothing is installed or sent until a user explicitly enables an integration.
package integrations

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
)

type Recipe struct {
	Name         string                                     `json:"name"`
	Harness      string                                     `json:"harness"`
	DefaultAgent string                                     `json:"default_agent"`
	Description  string                                     `json:"description"`
	Event        string                                     `json:"event"`
	Matcher      string                                     `json:"matcher"`
	Parse        func([]byte) (Event, error)                `json:"-"`
	CommandID    string                                     `json:"command_id"`
	Body         func(Event, Target, string) map[string]any `json:"-"`
}

var Recipes = []Recipe{{Name: "claude-code-suggested-tasks", Harness: "claude-code", DefaultAgent: "claude-code", Description: "Mirror Claude Code suggested tasks as ANX cards.", Event: "PostToolUse", Matcher: "^mcp__ccd_session__spawn_task$", Parse: parseSuggestedTask, CommandID: "cards.create", Body: CardBody}}

func (r Recipe) BuildBody(e Event, target Target) map[string]any { return r.Body(e, target, r.Harness) }

func Lookup(name string) (Recipe, error) {
	for _, r := range Recipes {
		if r.Name == name {
			return r, nil
		}
	}
	return Recipe{}, fmt.Errorf("unknown integration %q", name)
}

// Adapter owns only harness settings; recipes own the event-to-action mapping.
type Adapter interface {
	Scopes() []string // default scope first
	ProjectScoped(scope string) bool
	ConfigPath(home, project, scope string) (string, error)
	Merge(raw []byte, recipe Recipe, command, previous string, enable bool) ([]byte, bool, error)
	Inspect(raw []byte, recipe Recipe, command string) (present, matches bool, err error)
}

var Adapters = map[string]Adapter{"claude-code": ClaudeCode{}}

func InstallationID(name, settingsPath string) string {
	return Digest(name + "\x00" + settingsPath)[:24]
}
func Digest(s string) string { sum := sha256.Sum256([]byte(s)); return hex.EncodeToString(sum[:]) }
func ConfigPath(dir, name, id string) string {
	return filepath.Join(dir, "integrations", name, id+".json")
}

// ShellQuote never expands paths or allows a path to become shell syntax.
func ShellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
func HookCommand(executable, dir, name, id string) string {
	return ShellQuote(executable) + " --config-dir " + ShellQuote(dir) + " integrations run " + name + " --installation " + id
}

type Target struct {
	Board  string `json:"board"`
	Column string `json:"column,omitempty"`
}
type Config struct {
	Name         string            `json:"name"`
	ID           string            `json:"installation"`
	Scope        string            `json:"scope"`
	Project      string            `json:"project"`
	SettingsPath string            `json:"settings_path"`
	Executable   string            `json:"executable"`
	Command      string            `json:"command"`
	Workspace    string            `json:"workspace,omitempty"`
	Agent        string            `json:"agent"`
	Boards       map[string]Target `json:"boards"` // keyed by resolved workspace base URL
}

// ownedCommand accepts only a single shell-quoted absolute executable followed
// by the complete ANX-generated argv namespace. This repairs an interrupted
// binary-path upgrade even after the new config replaced the old command. It
// never recognizes arbitrary prefixes, suffix additions or shell expressions.
func ownedCommand(candidate, expected string) bool {
	if expected == "" {
		return false
	}
	if candidate == expected {
		return true
	}
	const marker = " --config-dir "
	offset := 0
	for {
		i := strings.Index(expected[offset:], marker)
		if i < 0 {
			return false
		}
		i += offset
		if quotedAbsolute(expected[:i]) {
			suffix := expected[i:]
			return strings.HasSuffix(candidate, suffix) && quotedAbsolute(strings.TrimSuffix(candidate, suffix))
		}
		offset = i + len(marker)
	}
}
func quotedAbsolute(word string) bool {
	if len(word) < 3 || word[0] != '\'' || word[len(word)-1] != '\'' {
		return false
	}
	value := strings.ReplaceAll(word[1:len(word)-1], "'\\''", "'")
	return filepath.IsAbs(value) && ShellQuote(value) == word
}
