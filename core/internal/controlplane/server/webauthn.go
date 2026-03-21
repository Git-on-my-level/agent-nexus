package server

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

type WebAuthnConfig struct {
	RPID     string
	RPOrigin string
}

func (c WebAuthnConfig) resolveForRequest(r *http.Request) (string, string, error) {
	requestOrigin, err := requestOrigin(r)
	if err != nil {
		return "", "", err
	}
	configuredOrigin, err := normalizeOrigin(c.RPOrigin)
	if err != nil {
		return "", "", fmt.Errorf("normalize configured WebAuthn origin: %w", err)
	}
	if configuredOrigin != "" && requestOrigin != "" && configuredOrigin != requestOrigin {
		return "", "", fmt.Errorf("configured WebAuthn origin %q does not match browser origin %q", configuredOrigin, requestOrigin)
	}

	effectiveOrigin := configuredOrigin
	if effectiveOrigin == "" {
		effectiveOrigin = requestOrigin
	}
	if effectiveOrigin == "" {
		return "", "", fmt.Errorf("determine WebAuthn origin from request")
	}
	parsedOrigin, err := url.Parse(effectiveOrigin)
	if err != nil {
		return "", "", fmt.Errorf("parse WebAuthn origin %q: %w", effectiveOrigin, err)
	}

	rpID := normalizeHostname(c.RPID)
	if rpID == "" {
		rpID = normalizeHostname(parsedOrigin.Hostname())
	}
	if rpID == "" {
		return "", "", fmt.Errorf("determine WebAuthn RP ID from origin")
	}
	if err := validateRPIDAgainstHost(rpID, parsedOrigin.Hostname()); err != nil {
		return "", "", err
	}
	return rpID, effectiveOrigin, nil
}

func requestOrigin(r *http.Request) (string, error) {
	if origin, err := normalizeOrigin(r.Header.Get("Origin")); origin != "" || err != nil {
		return origin, err
	}

	forwardedProto := firstHeaderValue(r.Header.Get("X-Forwarded-Proto"))
	forwardedHost := firstHeaderValue(r.Header.Get("X-Forwarded-Host"))
	if forwardedProto != "" && forwardedHost != "" {
		return normalizeOrigin(fmt.Sprintf("%s://%s", forwardedProto, forwardedHost))
	}

	host := strings.TrimSpace(r.Host)
	if host == "" {
		return "", nil
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return normalizeOrigin(fmt.Sprintf("%s://%s", scheme, host))
}

func normalizeOrigin(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("origin must include scheme and host")
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", fmt.Errorf("origin must not include a path")
	}
	return strings.ToLower(parsed.Scheme) + "://" + strings.ToLower(parsed.Host), nil
}

func validateRPIDAgainstHost(rpID string, host string) error {
	normalizedRPID := normalizeHostname(rpID)
	normalizedHost := normalizeHostname(host)
	if normalizedRPID == "" || normalizedHost == "" {
		return fmt.Errorf("WebAuthn RP ID and host must be non-empty")
	}
	if normalizedRPID == normalizedHost {
		return nil
	}
	if net.ParseIP(normalizedRPID) != nil || net.ParseIP(normalizedHost) != nil {
		return fmt.Errorf("WebAuthn RP ID %q must exactly match origin host %q", normalizedRPID, normalizedHost)
	}
	if normalizedRPID == "localhost" || normalizedHost == "localhost" {
		return fmt.Errorf("WebAuthn RP ID %q must exactly match origin host %q", normalizedRPID, normalizedHost)
	}
	if strings.HasSuffix(normalizedHost, "."+normalizedRPID) {
		return nil
	}
	return fmt.Errorf("WebAuthn RP ID %q must equal or be a suffix of origin host %q", normalizedRPID, normalizedHost)
}

func normalizeHostname(raw string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), ".")
}

func firstHeaderValue(raw string) string {
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, ",")
	return strings.TrimSpace(parts[0])
}
