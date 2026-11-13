// Environment variable parsing helpers (string, CSV, int, int64, duration,
// bool) and the listen-address-derived default core base URL.

package main

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultHost          = "127.0.0.1"
	defaultPort          = 8000
	defaultSchemaPath    = "../contracts/anx-schema.yaml"
	defaultWorkspaceRoot = ".anx-workspace"
	defaultAPIVersion    = "v0"
	defaultInstanceID    = "core-local"

	defaultWorkspaceMaxBlobBytes        int64 = 1 << 30
	defaultWorkspaceMaxArtifacts        int64 = 100000
	defaultWorkspaceMaxDocuments        int64 = 50000
	defaultWorkspaceMaxRevisions        int64 = 250000
	defaultWorkspaceMaxUploadBytes      int64 = 8 << 20
	defaultRequestBodyLimit             int64 = 1 << 20
	defaultAuthRequestBodyLimit         int64 = 256 << 10
	defaultContentRequestBodyLimit      int64 = 8 << 20
	defaultAuthRouteRateLimitPerMinute        = 600
	defaultAuthRouteRateBurst                 = 100
	defaultWriteRouteRateLimitPerMinute       = 1200
	defaultWriteRouteRateBurst                = 200
)

func envString(name string, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func envCSV(name string) []string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		values = append(values, part)
	}
	return values
}

func envInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid integer value for %s: %q\n", name, value)
		os.Exit(1)
	}
	return parsed
}

func envInt64(name string, fallback int64) int64 {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid integer value for %s: %q\n", name, value)
		os.Exit(1)
	}
	return parsed
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	parsed, err := time.ParseDuration(value)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid duration value for %s: %q\n", name, value)
		os.Exit(1)
	}
	return parsed
}

func envBool(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.ParseBool(value)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid boolean value for %s: %q\n", name, value)
		os.Exit(1)
	}
	return parsed
}

func defaultCoreBaseURL(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "http://127.0.0.1:8000"
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "http://" + addr
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}
