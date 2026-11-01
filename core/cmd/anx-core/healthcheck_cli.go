// The -healthcheck CLI mode: an HTTP GET to /livez on the configured listen
// address, used by container HEALTHCHECK (distroless images have no curl).

package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

// runHealthcheckCLI performs an HTTP GET to /livez for the configured listen address.
// Used by container HEALTHCHECK (distroless images have no curl).
func runHealthcheckCLI() int {
	addr := strings.TrimSpace(os.Getenv("ANX_LISTEN_ADDR"))
	if addr == "" {
		host := strings.TrimSpace(os.Getenv("ANX_HOST"))
		if host == "" {
			host = defaultHost
		}
		port := defaultPort
		if portStr := strings.TrimSpace(os.Getenv("ANX_PORT")); portStr != "" {
			parsed, err := strconv.Atoi(portStr)
			if err != nil {
				fmt.Fprintf(os.Stderr, "healthcheck: invalid ANX_PORT %q: %v\n", portStr, err)
				return 1
			}
			port = parsed
		}
		addr = net.JoinHostPort(host, strconv.Itoa(port))
	}
	if strings.Contains(addr, "://") {
		fmt.Fprintln(os.Stderr, "healthcheck: ANX_LISTEN_ADDR must be host:port, not a URL")
		return 1
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: parse listen address %q: %v\n", addr, err)
		return 1
	}
	// When the server binds 0.0.0.0 or ::, dialing that address from the client is
	// unreliable inside containers; use loopback for the probe instead.
	dialHost := host
	switch host {
	case "0.0.0.0", "":
		dialHost = "127.0.0.1"
	case "::":
		dialHost = "::1"
	}
	url := "http://" + net.JoinHostPort(dialHost, portStr) + "/livez"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: build request: %v\n", err)
		return 1
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck: GET %s: %v\n", url, err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck: GET %s: status %d\n", url, resp.StatusCode)
		return 1
	}
	return 0
}
