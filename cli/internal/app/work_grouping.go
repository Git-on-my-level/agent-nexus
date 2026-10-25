package app

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"

	"agent-nexus-cli/internal/errnorm"
)

func ensureGroupingRequestKey(body map[string]any) error {
	if strings.TrimSpace(anyString(body["request_key"])) != "" {
		return nil
	}
	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		return errnorm.Local("request_key_failed", "failed to allocate request key")
	}
	body["request_key"] = hex.EncodeToString(key)
	return nil
}
func applyCardGroupingFlags(body map[string]any, under, after string) {
	if strings.TrimSpace(under) != "" {
		body["under"] = strings.TrimSpace(under)
	}
	if strings.TrimSpace(after) != "" {
		body["after"] = strings.TrimSpace(after)
	}
	body["create_surface"] = "cards.create"
}
func withoutGroupingFlags(args []string) []string {
	out := []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--top-level" || strings.HasPrefix(arg, "--top-level=") {
			continue
		}
		if arg == "--under" || arg == "--after" {
			i++
			continue
		}
		if strings.HasPrefix(arg, "--under=") || strings.HasPrefix(arg, "--after=") {
			continue
		}
		out = append(out, arg)
	}
	return out
}

func cardCreateTopLevel(args []string) bool {
	var value bool
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--top-level":
			value = true
		case strings.HasPrefix(arg, "--top-level="):
			value, _ = strconv.ParseBool(strings.TrimPrefix(arg, "--top-level="))
		case arg == "--dry-run" || strings.Contains(arg, "=") || !strings.HasPrefix(arg, "-"):
		default:
			i++ // Every other cards-create flag consumes its value.
		}
	}
	return value
}
