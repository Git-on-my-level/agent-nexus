package integrations

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
)

type ClaudeCode struct{}

func (ClaudeCode) Scopes() []string                { return []string{"user", "project", "local"} }
func (ClaudeCode) ProjectScoped(scope string) bool { return scope != "user" }

func (ClaudeCode) ConfigPath(home, project, scope string) (string, error) {
	switch scope {
	case "user":
		return filepath.Join(home, ".claude", "settings.json"), nil
	case "project":
		return filepath.Join(project, ".claude", "settings.json"), nil
	case "local":
		return filepath.Join(project, ".claude", "settings.local.json"), nil
	default:
		return "", fmt.Errorf("scope must be user, project, or local")
	}
}

// RawMessage objects preserve unknown values exactly (including large numbers).
// JSON object keys are reformatted; array and unrelated hook ordering is retained.
func object(raw []byte) (map[string]json.RawMessage, error) {
	if !json.Valid(raw) {
		return nil, fmt.Errorf("settings must contain a valid JSON object")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("settings must contain a valid JSON object")
	}
	m := map[string]json.RawMessage{}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return nil, err
		}
		key := token.(string)
		if _, exists := m[key]; exists {
			return nil, fmt.Errorf("refusing duplicate JSON key %q", key)
		}
		var value json.RawMessage
		if err = decoder.Decode(&value); err != nil {
			return nil, err
		}
		m[key] = value
	}
	return m, nil
}
func settings(raw []byte, event string) (map[string]json.RawMessage, map[string]json.RawMessage, []json.RawMessage, error) {
	root, err := object(raw)
	if err != nil {
		return nil, nil, nil, err
	}
	hooks := map[string]json.RawMessage{}
	if v, ok := root["hooks"]; ok {
		hooks, err = object(v)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	var groups []json.RawMessage
	if v, ok := hooks[event]; ok {
		if err = json.Unmarshal(v, &groups); err != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return nil, nil, nil, fmt.Errorf("hooks.%s must be an array", event)
		}
	}
	return root, hooks, groups, nil
}
func encode(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
func expectedHook(command string) json.RawMessage {
	return encode(map[string]any{"type": "command", "command": command, "timeout": 10})
}
func (ClaudeCode) Merge(raw []byte, r Recipe, command, previous string, enable bool) ([]byte, bool, error) {
	root, hooks, groups, err := settings(raw, r.Event)
	if err != nil {
		return nil, false, err
	}
	var out []json.RawMessage
	found := false
	installed := false
	for _, g := range groups {
		group, err := object(g)
		if err != nil {
			return nil, false, err
		}
		var entries []json.RawMessage
		if rawHooks := bytes.TrimSpace(group["hooks"]); len(rawHooks) == 0 || rawHooks[0] != '[' {
			return nil, false, fmt.Errorf("hook group must contain a hooks array")
		}
		if err = json.Unmarshal(group["hooks"], &entries); err != nil {
			return nil, false, fmt.Errorf("hook group must contain a hooks array")
		}
		kept := make([]json.RawMessage, 0, len(entries))
		removed := false
		for _, h := range entries {
			handler, err := object(h)
			if err != nil {
				return nil, false, err
			}
			var cmd string
			_ = json.Unmarshal(handler["command"], &cmd)
			owned := ownedCommand(cmd, command) || previous != "" && cmd == previous
			if !owned {
				kept = append(kept, h)
				continue
			}
			// Remove just our handler, even if a user added siblings to its group.
			found = true
			removed = true
		}
		// Unknown group fields belong to the user. If removing our last
		// handler would discard them, retain the complete group unchanged.
		knownFields := 1 // hooks
		if _, ok := group["matcher"]; ok {
			knownFields++
		}
		if len(kept) == 0 && len(group) > knownFields {
			if enable && removed {
				group["matcher"] = encode(r.Matcher)
				group["hooks"] = encode([]json.RawMessage{expectedHook(command)})
				out = append(out, encode(group))
				installed = true
			} else {
				out = append(out, g)
			}
		} else if len(kept) > 0 {
			group["hooks"] = encode(kept)
			out = append(out, encode(group))
		}
	}
	if enable && !installed {
		out = append(out, encode(map[string]any{"matcher": r.Matcher, "hooks": []json.RawMessage{expectedHook(command)}}))
	}
	if !enable && !found {
		return raw, false, nil
	}
	if out == nil {
		out = []json.RawMessage{}
	}
	hooks[r.Event] = encode(out)
	root["hooks"] = encode(hooks)
	next, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, false, err
	}
	next = append(next, '\n')
	var before, after any
	_ = json.Unmarshal(raw, &before)
	_ = json.Unmarshal(next, &after)
	// Same installation must be byte-for-byte unchanged on repeated enable.
	if reflect.DeepEqual(before, after) {
		return raw, false, nil
	}
	return next, true, nil
}
func (ClaudeCode) Inspect(raw []byte, r Recipe, command string) (bool, bool, error) {
	if command == "" {
		_, _, _, err := settings(raw, r.Event)
		return false, false, err
	}
	_, _, groups, err := settings(raw, r.Event)
	if err != nil {
		return false, false, err
	}
	count := 0
	matches := false
	for _, g := range groups {
		group, err := object(g)
		if err != nil {
			return false, false, err
		}
		var entries []json.RawMessage
		if rawHooks := bytes.TrimSpace(group["hooks"]); len(rawHooks) == 0 || rawHooks[0] != '[' {
			return false, false, fmt.Errorf("hook group must contain a hooks array")
		}
		if err = json.Unmarshal(group["hooks"], &entries); err != nil {
			return false, false, fmt.Errorf("hook group must contain a hooks array")
		}
		var matcher string
		_ = json.Unmarshal(group["matcher"], &matcher)
		for _, h := range entries {
			m, err := object(h)
			if err != nil {
				return false, false, err
			}
			var cmd string
			_ = json.Unmarshal(m["command"], &cmd)
			if ownedCommand(cmd, command) {
				count++
				var got, want any
				_ = json.Unmarshal(h, &got)
				_ = json.Unmarshal(expectedHook(command), &want)
				matches = reflect.DeepEqual(got, want) && matcher == r.Matcher
			}
		}
	}
	return count > 0, count == 1 && matches, nil
}
