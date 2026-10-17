package app

import (
	"agent-nexus-cli/internal/errnorm"
	"regexp"
	"strings"
)

var cardSlugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
var cardUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// Mirrors core/internal/handles; core remains authoritative for conflicts.
func validateCardHandle(handle string) error {
	reserved := " api attachments cards documents events new search settings threads topics trash uuid "
	if len(handle) < 1 || len(handle) > 64 || !cardSlugPattern.MatchString(handle) || cardUUIDPattern.MatchString(handle) || strings.Contains(reserved, " "+handle+" ") {
		return errnorm.Usage("invalid_request", "--handle must be a canonical lowercase slug of 1-64 characters, excluding reserved names and UUIDs")
	}
	return nil
}
