package server

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
	webauthnlib "github.com/go-webauthn/webauthn/webauthn"
)

const defaultWebAuthnDisplayName = "OAR"

type WebAuthnConfig struct {
	RPDisplayName  string
	RPID           string
	RPOrigin       string
	AllowedOrigins []string
}

func (c WebAuthnConfig) buildForRequest(r *http.Request) (*webauthnlib.WebAuthn, error) {
	requestedOrigin, err := requestOrigin(r)
	if err != nil {
		return nil, err
	}

	effectiveOrigin, err := c.resolveOrigin(requestedOrigin)
	if err != nil {
		return nil, err
	}
	if effectiveOrigin == "" {
		return nil, fmt.Errorf("determine WebAuthn origin from request")
	}

	parsedOrigin, err := url.Parse(effectiveOrigin)
	if err != nil {
		return nil, fmt.Errorf("parse WebAuthn origin %q: %w", effectiveOrigin, err)
	}

	rpID := normalizeHostname(c.RPID)
	if rpID == "" {
		rpID = normalizeHostname(parsedOrigin.Hostname())
	}
	if rpID == "" {
		return nil, fmt.Errorf("determine WebAuthn RP ID from origin %q", effectiveOrigin)
	}
	if err := validateRPIDAgainstHost(rpID, parsedOrigin.Hostname()); err != nil {
		return nil, err
	}

	displayName := strings.TrimSpace(c.RPDisplayName)
	if displayName == "" {
		displayName = defaultWebAuthnDisplayName
	}

	return webauthnlib.New(&webauthnlib.Config{
		RPDisplayName: displayName,
		RPID:          rpID,
		RPOrigins:     []string{effectiveOrigin},
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			UserVerification: protocol.VerificationPreferred,
		},
	})
}

func (c WebAuthnConfig) resolveOrigin(requestedOrigin string) (string, error) {
	allowedOrigins, err := normalizeOrigins(c.AllowedOrigins)
	if err != nil {
		return "", fmt.Errorf("normalize configured WebAuthn allowed origins: %w", err)
	}
	if len(allowedOrigins) > 0 {
		if requestedOrigin == "" {
			return "", fmt.Errorf("determine WebAuthn origin from request")
		}
		if !containsOrigin(allowedOrigins, requestedOrigin) {
			return "", fmt.Errorf(
				"browser origin %q is not in configured WebAuthn allowed origins",
				requestedOrigin,
			)
		}
		return requestedOrigin, nil
	}

	configuredOrigin, err := normalizeOrigin(c.RPOrigin)
	if err != nil {
		return "", fmt.Errorf("normalize configured WebAuthn origin: %w", err)
	}
	if configuredOrigin != "" && requestedOrigin != "" && configuredOrigin != requestedOrigin {
		return "", fmt.Errorf(
			"configured WebAuthn origin %q does not match browser origin %q",
			configuredOrigin,
			requestedOrigin,
		)
	}
	if configuredOrigin != "" {
		return configuredOrigin, nil
	}
	return requestedOrigin, nil
}

func requestOrigin(r *http.Request) (string, error) {
	if origin, err := normalizeOrigin(r.Header.Get("Origin")); origin != "" || err != nil {
		return origin, err
	}

	forwardedProto := firstHeaderValue(r.Header.Get("X-Forwarded-Proto"))
	forwardedHost := firstHeaderValue(r.Header.Get("X-Forwarded-Host"))
	if requestPeerIP(r) != nil && requestPeerIP(r).IsLoopback() && forwardedProto != "" && forwardedHost != "" {
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
		return "", fmt.Errorf("parse origin %q: %w", raw, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("origin %q must include scheme and host", raw)
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return "", fmt.Errorf("origin %q must not include a path", raw)
	}

	return fmt.Sprintf("%s://%s", strings.ToLower(parsed.Scheme), strings.ToLower(parsed.Host)), nil
}

func normalizeOrigins(rawOrigins []string) ([]string, error) {
	origins := make([]string, 0, len(rawOrigins))
	for _, raw := range rawOrigins {
		origin, err := normalizeOrigin(raw)
		if err != nil {
			return nil, err
		}
		if origin == "" {
			continue
		}
		origins = append(origins, origin)
	}
	return origins, nil
}

func containsOrigin(origins []string, target string) bool {
	for _, origin := range origins {
		if origin == target {
			return true
		}
	}
	return false
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
		return fmt.Errorf("WebAuthn RP ID %q must exactly match origin host %q for IP addresses", normalizedRPID, normalizedHost)
	}
	if normalizedRPID == "localhost" || normalizedHost == "localhost" {
		return fmt.Errorf("WebAuthn RP ID %q must exactly match origin host %q for localhost", normalizedRPID, normalizedHost)
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
