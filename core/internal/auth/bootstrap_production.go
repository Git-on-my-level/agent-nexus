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
// is the template placeholder in a non-development deployment. Empty token is
// allowed (no bootstrap-onboarding). Fail-closed: set ANX_HOSTED_DEV_MODE=1
// for local runs that intentionally use the placeholder string.
func ValidateBootstrapTokenForNonDevDeploy(bootstrapToken string) error {
	t := strings.TrimSpace(bootstrapToken)
	if t == "" || t != BootstrapTokenPlaceholder {
		return nil
	}
	if !NonDevDeploymentFromEnviron() {
		return nil
	}
	return fmt.Errorf("ANX_BOOTSTRAP_TOKEN must not be the template value %q in a non-development deployment; generate a real secret or use ANX_HOSTED_DEV_MODE=1 for local-only", BootstrapTokenPlaceholder)
}
