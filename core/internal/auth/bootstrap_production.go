package auth

import (
	"fmt"
	"os"
	"strings"
)

// BootstrapTokenPlaceholder is the documented .env example value. It must not be
// used as a live ANX_BOOTSTRAP_TOKEN in non-development deployments; anx-core
// and hosted scripts refuse to start when it is set with production flags.
const BootstrapTokenPlaceholder = "REPLACE_WITH_SECURE_BOOTSTRAP_TOKEN"

const minNonDevBootstrapTokenLength = 32

// NonDevDeploymentFromEnviron is true when the process environment indicates a
// non-development deployment (production-like). Used to guard against leaving
// the template bootstrap token in place. Aligned with hosted scripts
// (ANX_HOSTED_DEV_MODE, ANX_ENV, ANX_ANX_IS_PROD).
func NonDevDeploymentFromEnviron() bool {
	if strings.TrimSpace(os.Getenv("ANX_HOSTED_DEV_MODE")) == "1" {
		return false
	}
	e := strings.ToLower(strings.TrimSpace(os.Getenv("ANX_ENV")))
	switch e {
	case "development", "dev", "test", "local":
		return false
	}
	if e == "production" {
		return true
	}
	p := strings.ToLower(strings.TrimSpace(os.Getenv("ANX_ANX_IS_PROD")))
	return p == "1" || p == "true" || p == "yes"
}

// ValidateBootstrapTokenForNonDevDeploy returns an error when ANX_BOOTSTRAP_TOKEN
// is unsafe for a non-development deployment. Empty token is allowed (no
// bootstrap-onboarding). Fail-closed: set ANX_HOSTED_DEV_MODE=1 for local runs
// that intentionally use template or short tokens.
func ValidateBootstrapTokenForNonDevDeploy(bootstrapToken string) error {
	t := strings.TrimSpace(bootstrapToken)
	if t == "" {
		return nil
	}
	if !NonDevDeploymentFromEnviron() {
		return nil
	}
	if t == BootstrapTokenPlaceholder {
		return fmt.Errorf("ANX_BOOTSTRAP_TOKEN must not be the template value %q in a non-development deployment; generate a real secret or use ANX_HOSTED_DEV_MODE=1 for local-only", BootstrapTokenPlaceholder)
	}
	if len(t) < minNonDevBootstrapTokenLength {
		return fmt.Errorf("ANX_BOOTSTRAP_TOKEN must be at least %d characters in a non-development deployment", minNonDevBootstrapTokenLength)
	}
	if isKnownWeakBootstrapToken(t) {
		return fmt.Errorf("ANX_BOOTSTRAP_TOKEN must not use a known weak value in a non-development deployment")
	}
	return nil
}

func isKnownWeakBootstrapToken(token string) bool {
	switch strings.ToLower(strings.TrimSpace(token)) {
	case "changeme", "change-me", "password", "secret", "bootstrap", "bootstrap-token", "test", "token":
		return true
	default:
		return false
	}
}
