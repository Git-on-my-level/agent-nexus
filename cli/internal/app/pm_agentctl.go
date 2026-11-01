// agentctl runner integration: launching and waiting for agentctl executions,
// parsing agentctl JSON output into execution ids and assistant text, and the
// shared map field lookups used for that parsing.

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func continuingAgentctlLaunch(raw []byte) (execID, timeout string, ok bool) {
	env := parseAgentctlJSON(raw)
	errObj := asMap(env["error"])
	details := asMap(errObj["details"])
	execID = firstNonEmpty(anyString(details["execution_id"]), extractExecutionID(raw))
	if !strings.HasPrefix(execID, "exec-") {
		return "", "", false
	}
	if !anyBool(details["worker_continues"]) && !anyBool(errObj["retryable"]) {
		return "", "", false
	}
	return execID, anyString(details["timeout"]), true
}

func waitForAgentctlExecution(ctx context.Context, agentctl, execID, workDir string, env []string) error {
	deadline := nowFn().Add(startupAckPoll)
	delay := time.Second
	for {
		out, _, err := runCmd(ctx, agentctl, []string{"status", execID}, workDir, env)
		if agentctlExecutionVisible(out, err) {
			return nil
		}
		if !nowFn().Before(deadline) {
			if err != nil {
				return err
			}
			return fmt.Errorf("execution not found")
		}
		remain := deadline.Sub(nowFn())
		if delay > remain {
			delay = remain
		}
		if delay < time.Millisecond {
			return fmt.Errorf("execution not found")
		}
		if err := sleepFn(ctx, delay); err != nil {
			return err
		}
		if delay < 8*time.Second {
			delay *= 2
		}
	}
}

func agentctlNotFound(raw []byte, err error) bool {
	env := parseAgentctlJSON(raw)
	errObj := asMap(env["error"])
	code := strings.ToLower(anyString(errObj["code"]))
	if code == "not_found" || code == "unknown_execution" {
		return true
	}
	blob := strings.ToLower(string(raw))
	if err != nil {
		blob += " " + strings.ToLower(err.Error())
	}
	return strings.Contains(blob, "not found") || strings.Contains(blob, "unknown execution")
}

func agentctlExecutionVisible(raw []byte, err error) bool {
	if agentctlNotFound(raw, err) {
		return false
	}
	if extractExecutionID(raw) != "" {
		return true
	}
	env := parseAgentctlJSON(raw)
	if ok, _ := env["ok"].(bool); ok {
		return true
	}
	return err == nil && len(bytes.TrimSpace(raw)) > 0
}

func parseAgentctlJSON(raw []byte) map[string]any {
	var payload any
	if json.Unmarshal(raw, &payload) == nil {
		return asMap(payload)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if json.Unmarshal([]byte(line), &payload) == nil {
			if m := asMap(payload); len(m) > 0 {
				return m
			}
		}
	}
	return map[string]any{}
}

func anyBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return strings.EqualFold(strings.TrimSpace(t), "true")
	}
	return false
}

func extractExecutionID(raw []byte) string {
	var payload any
	if json.Unmarshal(raw, &payload) != nil {
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if json.Unmarshal([]byte(line), &payload) == nil {
				if id := findStringField(payload, "id"); strings.HasPrefix(id, "exec-") {
					return id
				}
			}
		}
		return ""
	}
	if id := findStringField(payload, "id"); strings.HasPrefix(id, "exec-") {
		return id
	}
	if nested := asMap(asMap(payload)["result"])["id"]; nested != nil {
		if id := fmt.Sprint(nested); strings.HasPrefix(id, "exec-") {
			return id
		}
	}
	return findPrefixedString(payload, "exec-")
}

func extractAssistantText(content, blob string) string {
	content = strings.TrimSpace(content)
	if content != "" && !strings.HasPrefix(content, "{") && !strings.HasPrefix(content, "[") {
		return content
	}
	var payload any
	for _, raw := range []string{content, blob} {
		if json.Unmarshal([]byte(raw), &payload) != nil {
			continue
		}
		if text := lastAssistantText(payload); text != "" {
			return text
		}
	}
	return content
}

func lastAssistantText(v any) string {
	switch t := v.(type) {
	case map[string]any:
		if role, _ := t["role"].(string); role == "assistant" {
			if s := messageText(t); s != "" {
				return s
			}
		}
		var found string
		for _, key := range []string{"messages", "result", "content", "output", "data"} {
			if s := lastAssistantText(t[key]); s != "" {
				found = s
			}
		}
		if found != "" {
			return found
		}
		return messageText(t)
	case []any:
		var found string
		for _, item := range t {
			if s := lastAssistantText(item); s != "" {
				found = s
			}
		}
		return found
	case string:
		return ""
	default:
		return ""
	}
}

func messageText(m map[string]any) string {
	if s, ok := m["text"].(string); ok && strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	if s, ok := m["content"].(string); ok && strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(anyString(m["content"]))
}

func extractProviderModel(raw string) (string, string) {
	matches := providerModelRe.FindAllStringSubmatch(raw, -1)
	if len(matches) == 0 {
		return "", ""
	}
	last := matches[len(matches)-1]
	return last[1], last[2]
}

func joinCmdOutput(stdout, stderr []byte) []byte {
	if len(stderr) == 0 {
		return stdout
	}
	if len(stdout) == 0 {
		return stderr
	}
	out := make([]byte, 0, len(stdout)+len(stderr))
	out = append(out, stdout...)
	out = append(out, stderr...)
	return out
}

func assistantTextFromRunnerOutput(stdout []byte) string {
	return strings.TrimSpace(extractAssistantText(string(stdout), string(stdout)))
}

func (a *App) logRunnerStderr(turnID string, stderr []byte) {
	if len(stderr) == 0 || harnessLogWriter != nil {
		return
	}
	a.pmLog("pm serve: turn %s runner stderr: %s\n", turnID, strings.TrimSpace(string(stderr)))
}

func findStringField(v any, key string) string {
	switch t := v.(type) {
	case map[string]any:
		if s, ok := t[key].(string); ok {
			return s
		}
		for _, item := range t {
			if s := findStringField(item, key); s != "" {
				return s
			}
		}
	case []any:
		for _, item := range t {
			if s := findStringField(item, key); s != "" {
				return s
			}
		}
	}
	return ""
}

func findPrefixedString(v any, prefix string) string {
	switch t := v.(type) {
	case map[string]any:
		for _, item := range t {
			if s := findPrefixedString(item, prefix); s != "" {
				return s
			}
		}
	case []any:
		for _, item := range t {
			if s := findPrefixedString(item, prefix); s != "" {
				return s
			}
		}
	case string:
		if strings.HasPrefix(t, prefix) {
			return t
		}
	}
	return ""
}

func intFromAny(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	case string:
		i, err := strconv.Atoi(n)
		return i, err == nil
	}
	return 0, false
}
