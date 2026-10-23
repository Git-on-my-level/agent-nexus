package app

import (
	"agent-nexus-cli/internal/errnorm"
	"strings"
)

func validateDashboardPinsArgs(args []string) error {
	usage := func() error {
		return errnorm.Usage("invalid_args", "usage: anx workspace dashboard pins list | set <document:ref>... | set none")
	}
	if len(args) == 1 && args[0] == "list" {
		return nil
	}
	if len(args) < 2 || args[0] != "set" || len(args) > 13 {
		return usage()
	}
	if len(args) == 2 && args[1] == "none" {
		return nil
	}
	seen := map[string]bool{}
	for _, ref := range args[1:] {
		if !strings.HasPrefix(ref, "document:") || len(ref) <= len("document:") || len(ref) > 256 || strings.ContainsAny(ref, " /\\?#%\n\r\t") || seen[ref] {
			return usage()
		}
		seen[ref] = true
	}
	return nil
}
