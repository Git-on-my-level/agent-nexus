// Development auth bypass gating: the workspace marker file that must exist
// before the passkey dev bypass is honored, config validation requiring a
// loopback listen address, and the file-existence check for the marker.

package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
)

// Local-only marker that must exist in the workspace root before the passkey
// dev bypass (synthetic human onboarding) is honored.
const devPasskeyBypassMarkerName = ".anx-dev-insecure-auth"

func validateDevAuthBypassConfig(addr string, enableDevActorMode bool, allowUnauthenticatedWrites bool) error {
	if !enableDevActorMode && !allowUnauthenticatedWrites {
		return nil
	}
	if strings.TrimSpace(os.Getenv("ANX_HOSTED_DEV_MODE")) != "1" {
		return fmt.Errorf("ANX_ENABLE_DEV_ACTOR_MODE and ANX_ALLOW_UNAUTHENTICATED_WRITES are local-only bypasses; set ANX_HOSTED_DEV_MODE=1 and bind anx-core to loopback for deliberate local development")
	}
	if !isLoopbackListenAddress(addr) {
		return fmt.Errorf("ANX_ENABLE_DEV_ACTOR_MODE and ANX_ALLOW_UNAUTHENTICATED_WRITES require a loopback listen address; got %q", addr)
	}
	return nil
}

func isLoopbackListenAddress(addr string) bool {
	addr = strings.TrimSpace(addr)
	if addr == "" || strings.Contains(addr, "://") {
		return false
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func fileExists(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}
