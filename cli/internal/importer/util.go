package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	frontMatterRE = regexp.MustCompile(`\A---\n(.*?)\n---\n`)
	codeFenceRE   = regexp.MustCompile("(?s)```.*?```")
	spaceRE       = regexp.MustCompile(`\s+`)
	wordRE        = regexp.MustCompile(`[A-Za-z0-9_]+`)
)

func utcNow() string {
	return time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
}

func sha256Bytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func normalizeText(text string) string {
	text = frontMatterRE.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = codeFenceRE.ReplaceAllString(text, " ")
	text = spaceRE.ReplaceAllString(text, " ")
	return strings.ToLower(strings.TrimSpace(text))
}

func contentPreview(text string, limit int) string {
	text = strings.ReplaceAll(text, "\x00", "")
	if len(text) <= limit {
		return text
	}
	return text[:limit]
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "import"
	}
	return value
}

func canonicalTitle(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = regexp.MustCompile(`[^a-z0-9 ]+`).ReplaceAllString(value, " ")
	value = spaceRE.ReplaceAllString(value, " ")
	return strings.TrimSpace(value)
}

func writeJSON(path string, payload any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func loadJSON(path string, into any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(data, into); err != nil {
		return err
	}
	return nil
}

func loadInventory(path string) ([]InventoryRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	items := make([]InventoryRecord, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var item InventoryRecord
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			return nil, fmt.Errorf("decode inventory line: %w", err)
		}
		items = append(items, item)
	}
	return items, nil
}

func writeInventory(path string, items []InventoryRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, item := range items {
		if err := enc.Encode(item); err != nil {
			return err
		}
	}
	return nil
}

func loadTextContent(item InventoryRecord) string {
	if strings.TrimSpace(item.TextCachePath) != "" {
		if data, err := os.ReadFile(item.TextCachePath); err == nil {
			return string(data)
		}
	}
	return item.Preview
}

func firstN[T any](items []T, n int) []T {
	if n < 0 || len(items) <= n {
		return items
	}
	return items[:n]
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func anyString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return fmt.Sprintf("%v", value)
	}
}

func asMap(value any) map[string]any {
	mapped, _ := value.(map[string]any)
	return mapped
}

func asStringSlice(value any) ([]string, bool) {
	raw, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]string); ok {
			return append([]string(nil), typed...), true
		}
		return nil, false
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		text, ok := item.(string)
		if !ok {
			return nil, false
		}
		out = append(out, text)
	}
	return out, true
}

func cloneValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for k, v := range typed {
			out[k] = cloneValue(v)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, v := range typed {
			out[i] = cloneValue(v)
		}
		return out
	case []string:
		out := make([]string, len(typed))
		copy(out, typed)
		return out
	default:
		return value
	}
}

func substituteRefs(value any, keyToRef map[string]string) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for k, v := range typed {
			out[k] = substituteRefs(v, keyToRef)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, v := range typed {
			out[i] = substituteRefs(v, keyToRef)
		}
		return out
	case []string:
		out := make([]string, len(typed))
		for i, v := range typed {
			resolved := substituteRefs(v, keyToRef)
			out[i] = anyString(resolved)
		}
		return out
	case string:
		if strings.HasPrefix(typed, "$REF:") {
			key := strings.TrimSpace(strings.TrimPrefix(typed, "$REF:"))
			if resolved, ok := keyToRef[key]; ok {
				return resolved
			}
		}
		return typed
	default:
		return typed
	}
}
