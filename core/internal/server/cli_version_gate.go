// CLI version compatibility floor: reads the client version header, compares
// semver triples against the configured minimum, and writes the upgrade
// payload when the client is too old.

package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func writeCLIOutdated(w http.ResponseWriter, opts handlerOptions) {
	payload := map[string]any{
		"error": errorPayload("cli_outdated", "CLI version is below the minimum compatible version for this core instance"),
		"upgrade": map[string]any{
			"min_cli_version":         strings.TrimSpace(opts.minCLIVersion),
			"recommended_cli_version": strings.TrimSpace(opts.recommendedCLIVersion),
			"cli_download_url":        strings.TrimSpace(opts.cliDownloadURL),
		},
	}
	writeJSON(w, http.StatusUpgradeRequired, payload)
}

func enforceCLIVersion(w http.ResponseWriter, r *http.Request, opts handlerOptions) bool {
	if r == nil || !shouldEnforceCLIVersion(r.URL.Path) {
		return false
	}
	clientVersion := clientCLIVersion(r)
	if clientVersion == "" {
		return false
	}
	outdated, compareErr := isCLIVersionOutdated(clientVersion, opts.minCLIVersion)
	if compareErr != nil || !outdated {
		return false
	}
	writeCLIOutdated(w, opts)
	return true
}

// clientCLIVersion reads the current header and the pre-rename X-OAR-CLI-Version
// header. Clients older than the ANX rename never send X-ANX-CLI-Version, so
// ignoring the legacy header lets them skip the compatibility floor.
func clientCLIVersion(r *http.Request) string {
	if r == nil {
		return ""
	}
	if version := strings.TrimSpace(r.Header.Get("X-ANX-CLI-Version")); version != "" {
		return version
	}
	return strings.TrimSpace(r.Header.Get("X-OAR-CLI-Version"))
}

func shouldEnforceCLIVersion(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	switch path {
	case "/health", "/livez", "/readyz", "/ops/health", "/ops/usage-summary", "/v1/usage/summary", "/ops/blob-usage/rebuild", "/version", "/meta/handshake", "/auth/token", "/auth/bootstrap/status":
		return false
	}
	if strings.HasPrefix(path, "/auth/passkey/") {
		return false
	}
	return true
}

func isCLIVersionOutdated(clientVersion string, minVersion string) (bool, error) {
	clientParts, err := parseSemanticVersion(clientVersion)
	if err != nil {
		return false, err
	}
	minParts, err := parseSemanticVersion(minVersion)
	if err != nil {
		return false, err
	}
	for i := 0; i < 3; i++ {
		if clientParts[i] < minParts[i] {
			return true, nil
		}
		if clientParts[i] > minParts[i] {
			return false, nil
		}
	}
	return false, nil
}

func parseSemanticVersion(raw string) ([3]int, error) {
	var out [3]int
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "v")
	if raw == "" {
		return out, fmt.Errorf("empty version")
	}
	if idx := strings.IndexAny(raw, "-+"); idx >= 0 {
		raw = raw[:idx]
	}
	parts := strings.Split(raw, ".")
	if len(parts) < 1 || len(parts) > 3 {
		return out, fmt.Errorf("invalid semantic version: %s", raw)
	}
	for i := 0; i < 3; i++ {
		if i >= len(parts) {
			out[i] = 0
			continue
		}
		segment := strings.TrimSpace(parts[i])
		if segment == "" {
			return out, fmt.Errorf("invalid semantic version segment")
		}
		value, err := strconv.Atoi(segment)
		if err != nil {
			return out, err
		}
		if value < 0 {
			return out, fmt.Errorf("invalid negative segment")
		}
		out[i] = value
	}
	return out, nil
}
