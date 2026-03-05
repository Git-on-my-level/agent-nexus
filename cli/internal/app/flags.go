package app

import (
	"flag"
	"fmt"
	"strings"
	"time"
)

type trackedBool struct {
	set   bool
	value bool
}

func (t *trackedBool) Set(raw string) error {
	t.set = true
	if strings.TrimSpace(raw) == "" {
		t.value = true
		return nil
	}
	parsed, err := strconvParseBool(raw)
	if err != nil {
		return err
	}
	t.value = parsed
	return nil
}

func (t *trackedBool) String() string {
	if t == nil {
		return "false"
	}
	if t.value {
		return "true"
	}
	return "false"
}

func (t *trackedBool) IsBoolFlag() bool { return true }

type trackedString struct {
	set   bool
	value string
}

func (t *trackedString) Set(raw string) error {
	t.set = true
	t.value = raw
	return nil
}

func (t *trackedString) String() string {
	if t == nil {
		return ""
	}
	return t.value
}

type trackedDuration struct {
	set   bool
	value time.Duration
}

func (t *trackedDuration) Set(raw string) error {
	parsed, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil {
		return err
	}
	t.set = true
	t.value = parsed
	return nil
}

func (t *trackedDuration) String() string {
	if t == nil {
		return "0s"
	}
	return t.value.String()
}

type headerList []string

func (h *headerList) String() string {
	if h == nil {
		return ""
	}
	return strings.Join(*h, ",")
}

func (h *headerList) Set(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("header must not be empty")
	}
	if !strings.Contains(raw, ":") {
		return fmt.Errorf("header must be in key:value format")
	}
	*h = append(*h, raw)
	return nil
}

func parseHeaders(entries []string) (map[string]string, error) {
	out := make(map[string]string, len(entries))
	for _, entry := range entries {
		parts := strings.SplitN(entry, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid header %q", entry)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key == "" {
			return nil, fmt.Errorf("invalid header %q: key is empty", entry)
		}
		out[key] = value
	}
	return out, nil
}

func newSilentFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(discardWriter{})
	return fs
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func strconvParseBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "t", "true", "y", "yes", "on":
		return true, nil
	case "0", "f", "false", "n", "no", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value %q", raw)
	}
}
