package controlplane

import (
	"fmt"
	"net/url"
	"strings"
)

func NormalizePublicBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("public base URL must include scheme and host")
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = strings.TrimRight(parsed.RawPath, "/")
	return strings.TrimRight(parsed.String(), "/"), nil
}

func PublicBaseOrigin(raw string) (string, error) {
	normalized, err := NormalizePublicBaseURL(raw)
	if err != nil || normalized == "" {
		return normalized, err
	}
	parsed, err := url.Parse(normalized)
	if err != nil {
		return "", err
	}
	parsed.Path = ""
	parsed.RawPath = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func WorkspaceURLTemplateFromPublicBase(raw string) (string, error) {
	normalized, err := NormalizePublicBaseURL(raw)
	if err != nil || normalized == "" {
		return normalized, err
	}
	return normalized + "/%s", nil
}

func InviteURLTemplateFromPublicBase(raw string) (string, error) {
	normalized, err := NormalizePublicBaseURL(raw)
	if err != nil || normalized == "" {
		return normalized, err
	}
	return normalized + "/invites/%s", nil
}
