package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

// HookSettings is local-only. Empty settings mean no feature is enabled.
type HookSettings struct {
	Version       int                `json:"schema_version"`
	Installations []HookInstallation `json:"installations"`
	Bindings      []HookBinding      `json:"bindings"`
	Rules         []HookRule         `json:"rules"`
}
type HookInstallation struct {
	ID           string `json:"id"`
	Recipe       string `json:"recipe"`
	Harness      string `json:"harness"`
	Scope        string `json:"scope"`
	Project      string `json:"project"`
	SettingsPath string `json:"settings_path,omitempty"`
	Command      string `json:"command"`
	Agent        string `json:"agent"`
	Workspace    string `json:"workspace,omitempty"`
	Card         string `json:"card_ref,omitempty"`
	AllowStale   bool   `json:"allow_stale"`
	Enabled      bool   `json:"enabled"`
}
type HookBinding struct {
	Workspace string `json:"workspace"`
	Actor     string `json:"actor"`
	Session   string `json:"session"`
	Card      string `json:"card_ref"`
}
type HookRule struct {
	Workspace string `json:"workspace"`
	Actor     string `json:"actor"`
	Directory string `json:"directory"`
	Card      string `json:"card_ref"`
}

func HookSettingsPath(dir string) string { return filepath.Join(dir, "hooks", "config.json") }
func LoadHooks(dir string) (HookSettings, error) {
	s := HookSettings{Version: 1, Installations: []HookInstallation{}, Bindings: []HookBinding{}, Rules: []HookRule{}}
	b, e := PrivateRead(HookSettingsPath(dir))
	if errors.Is(e, os.ErrNotExist) {
		return s, nil
	}
	if e != nil {
		return s, e
	}
	if e = json.Unmarshal(b, &s); e != nil {
		return s, e
	}
	if s.Version != 1 {
		return s, fmt.Errorf("unsupported hook configuration")
	}
	return s, nil
}
func UpdateHooks(ctx context.Context, dir string, fn func(*HookSettings) error) error {
	unlock, e := Lock(ctx, HookSettingsPath(dir)+".lock")
	if e != nil {
		return e
	}
	defer unlock()
	s, e := LoadHooks(dir)
	if e != nil {
		return e
	}
	if e = fn(&s); e != nil {
		return e
	}
	if len(s.Installations) > 100 || len(s.Bindings) > 1000 || len(s.Rules) > 1000 {
		return fmt.Errorf("hook configuration capacity reached")
	}
	return PrivateWrite(HookSettingsPath(dir), JSON(s))
}
func HookRecipe(name string) bool { return name == "context.inject" || name == "git.capture" }
func ValidCard(ref string) bool {
	return strings.HasPrefix(ref, "card:") && len(ref) > 5 && len(ref) <= 256 && strings.IndexFunc(ref, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) < 0
}

type HookEvent struct {
	Version int    `json:"schema_version"`
	Session string `json:"session_id,omitempty"`
	CWD     string `json:"cwd"`
	Card    string `json:"card_ref,omitempty"`
}
type HookBrief struct {
	Version int     `json:"schema_version"`
	Context string  `json:"context"`
	Card    *string `json:"card_ref"`
	AsOf    *string `json:"as_of"`
	Stale   bool    `json:"stale"`
}

func EmptyBrief() HookBrief { return HookBrief{Version: 1} }
func SessionKey(harness, session string) string {
	if session == "" {
		return ""
	}
	return Digest(harness + "\x00" + session)
}
func ParseHook(harness, event string, raw []byte) (HookEvent, error) {
	var p struct {
		HookEvent
		Conversation string   `json:"conversation_id"`
		Roots        []string `json:"workspace_roots"`
		Event        string   `json:"hook_event_name"`
		Source       string   `json:"source"`
	}
	if len(raw) > MaxPayloadBytes || !utf8.Valid(raw) {
		return p.HookEvent, fmt.Errorf("invalid_payload")
	}
	if e := json.Unmarshal(raw, &p); e != nil {
		return p.HookEvent, fmt.Errorf("invalid_payload")
	}
	if harness == "generic" && p.Version != 1 {
		return p.HookEvent, fmt.Errorf("unsupported_payload_version")
	}
	switch harness {
	case "generic":
	case "claude-code", "codex":
		expected := "SessionStart"
		if event == "tool.after" {
			expected = "PostToolUse"
		}
		if p.Event != expected {
			return p.HookEvent, fmt.Errorf("unexpected_event")
		}
		if expected == "SessionStart" && p.Source != "startup" && p.Source != "resume" && p.Source != "clear" && p.Source != "compact" && p.Source != "fork" {
			return p.HookEvent, fmt.Errorf("unsupported_source")
		}
	case "cursor":
		expected := "sessionStart"
		if event == "tool.after" {
			expected = "afterShellExecution"
		}
		if p.Event != expected {
			return p.HookEvent, fmt.Errorf("unexpected_event")
		}
		if p.CWD == "" {
			if len(p.Roots) != 1 {
				return p.HookEvent, fmt.Errorf("ambiguous_workspace_roots")
			}
			p.CWD = p.Roots[0]
		}
		if p.Session == "" {
			p.Session = p.Conversation
		}
	default:
		return p.HookEvent, fmt.Errorf("unsupported_harness")
	}
	if !filepath.IsAbs(p.CWD) || len(p.CWD) > 4096 || len(p.Session) > 1024 || strings.ContainsAny(p.CWD+p.Session, "\x00\r\n") {
		return p.HookEvent, fmt.Errorf("invalid_payload")
	}
	if p.Card != "" && !ValidCard(p.Card) {
		return p.HookEvent, fmt.Errorf("invalid_card")
	}
	p.Version = 1
	return p.HookEvent, nil
}
func RenderHook(harness string, b HookBrief) []byte {
	if harness == "generic" {
		return JSON(b)
	}
	if b.Context == "" {
		return nil
	}
	if harness == "cursor" {
		return JSON(map[string]any{"additional_context": b.Context})
	}
	return JSON(map[string]any{"hookSpecificOutput": map[string]any{"hookEventName": "SessionStart", "additionalContext": b.Context}})
}

// ResolveHookCard never falls through an explicit target and never uses presence.
func ResolveHookCard(s HookSettings, i HookInstallation, e HookEvent, workspace, actor string) (string, error) {
	if e.Card != "" {
		return e.Card, nil
	}
	session := SessionKey(i.Harness, e.Session)
	for _, b := range s.Bindings {
		if session != "" && b.Workspace == workspace && b.Actor == actor && b.Session == session {
			return b.Card, nil
		}
	}
	if i.Card != "" {
		return i.Card, nil
	}
	best := ""
	card := ""
	for _, r := range s.Rules {
		if r.Workspace != workspace || r.Actor != actor {
			continue
		}
		rel, err := filepath.Rel(r.Directory, e.CWD)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		if len(r.Directory) > len(best) {
			best, card = r.Directory, r.Card
		} else if len(r.Directory) == len(best) && r.Card != card {
			return "", fmt.Errorf("ambiguous_card_rule")
		}
	}
	return card, nil
}

// HookAdapter uses the existing lossless owned-command settings merger for Claude
// and Codex, and the Cursor flat command array. Unknown keys survive every merge.
func HookAdapterPath(home, project, harness, scope string) (string, error) {
	if harness == "generic" {
		if scope != "user" && scope != "project" {
			return "", fmt.Errorf("invalid scope")
		}
		return "", nil
	}
	if harness == "claude-code" {
		return (ClaudeCode{}).ConfigPath(home, project, scope)
	}
	if scope != "user" && scope != "project" {
		return "", fmt.Errorf("scope must be user or project")
	}
	root := project
	if scope == "user" {
		root = home
	}
	if harness == "codex" {
		return filepath.Join(root, ".codex", "hooks.json"), nil
	}
	if harness == "cursor" {
		return filepath.Join(root, ".cursor", "hooks.json"), nil
	}
	return "", fmt.Errorf("unsupported harness")
}
func MergeHook(raw []byte, harness, recipe, command, previous string, enable bool) ([]byte, bool, error) {
	event, matcher := "SessionStart", "startup|resume|clear|compact|fork"
	if recipe == "git.capture" {
		event, matcher = "PostToolUse", "Bash|Shell"
	}
	if harness != "cursor" {
		next, _, e := (ClaudeCode{}).Merge(raw, Recipe{Event: event, Matcher: matcher}, command, previous, enable)
		if e != nil {
			return nil, false, e
		}
		if enable {
			root, hooks, groups, e := settings(next, event)
			if e != nil {
				return nil, false, e
			}
			for j, g := range groups {
				group, _ := object(g)
				var handlers []json.RawMessage
				_ = json.Unmarshal(group["hooks"], &handlers)
				for k, h := range handlers {
					handler, _ := object(h)
					var cmd string
					_ = json.Unmarshal(handler["command"], &cmd)
					if cmd == command {
						handler["timeout"] = encode(2)
						if harness == "codex" && recipe == "context.inject" {
							handler["additionalContextLimit"] = encode(6 << 10)
						}
						handlers[k] = encode(handler)
					}
				}
				group["hooks"] = encode(handlers)
				groups[j] = encode(group)
			}
			hooks[event] = encode(groups)
			root["hooks"] = encode(hooks)
			next = JSON(root)
		}
		var a, b any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		_ = decoder.Decode(&a)
		decoder = json.NewDecoder(bytes.NewReader(next))
		decoder.UseNumber()
		_ = decoder.Decode(&b)
		if reflect.DeepEqual(a, b) {
			return raw, false, nil
		}
		return next, true, nil
	}
	event = "sessionStart"
	if recipe == "git.capture" {
		event = "afterShellExecution"
	}
	root, hooks, entries, e := settings(raw, event)
	if e != nil {
		return nil, false, e
	}
	if v, ok := root["version"]; ok && string(v) != "1" {
		return nil, false, fmt.Errorf("unsupported Cursor settings version")
	}
	kept := []json.RawMessage{}
	found := false
	for _, entry := range entries {
		m, e := object(entry)
		if e != nil {
			return nil, false, e
		}
		var cmd string
		_ = json.Unmarshal(m["command"], &cmd)
		if ownedCommand(cmd, command) || previous != "" && cmd == previous {
			found = true
			continue
		}
		kept = append(kept, entry)
	}
	if !enable && !found {
		return raw, false, nil
	}
	if enable {
		kept = append(kept, encode(map[string]any{"command": command, "timeout": 2}))
	}
	root["version"] = encode(1)
	hooks[event] = encode(kept)
	root["hooks"] = encode(hooks)
	next := JSON(root)
	var a, b any
	_ = json.Unmarshal(raw, &a)
	_ = json.Unmarshal(next, &b)
	if string(encode(a)) == string(encode(b)) {
		return raw, false, nil
	}
	return next, true, nil
}
