package auth

import (
	"testing"
)

func TestNonDevDeploymentFromEnviron(t *testing.T) {
	t.Run("dev mode clears non-dev", func(t *testing.T) {
		t.Setenv("ANX_HOSTED_DEV_MODE", "1")
		t.Setenv("ANX_ENV", "production")
		t.Setenv("ANX_ANX_IS_PROD", "")
		if NonDevDeploymentFromEnviron() {
			t.Fatal("expected dev mode to override")
		}
	})
	t.Run("production env", func(t *testing.T) {
		t.Setenv("ANX_HOSTED_DEV_MODE", "")
		t.Setenv("ANX_ENV", "production")
		t.Setenv("ANX_ANX_IS_PROD", "")
		if !NonDevDeploymentFromEnviron() {
			t.Fatal("expected production")
		}
	})
	t.Run("anx is prod", func(t *testing.T) {
		t.Setenv("ANX_HOSTED_DEV_MODE", "")
		t.Setenv("ANX_ENV", "")
		t.Setenv("ANX_ANX_IS_PROD", "1")
		if !NonDevDeploymentFromEnviron() {
			t.Fatal("expected non-dev from ANX_ANX_IS_PROD")
		}
	})
	t.Run("empty env is not non-dev for guard", func(t *testing.T) {
		t.Setenv("ANX_HOSTED_DEV_MODE", "")
		t.Setenv("ANX_ENV", "")
		t.Setenv("ANX_ANX_IS_PROD", "")
		if NonDevDeploymentFromEnviron() {
			t.Fatal("expected empty env to allow placeholder (no production signal)")
		}
	})
}

func TestValidateBootstrapTokenForNonDevDeploy(t *testing.T) {
	t.Setenv("ANX_ENV", "production")
	t.Setenv("ANX_HOSTED_DEV_MODE", "")
	if err := ValidateBootstrapTokenForNonDevDeploy(""); err != nil {
		t.Fatalf("empty: %v", err)
	}
	if err := ValidateBootstrapTokenForNonDevDeploy("real-secret-token"); err != nil {
		t.Fatalf("real token: %v", err)
	}
	if err := ValidateBootstrapTokenForNonDevDeploy(BootstrapTokenPlaceholder); err == nil {
		t.Fatal("expected error for placeholder in production")
	}
	t.Setenv("ANX_ENV", "development")
	t.Setenv("ANX_ANX_IS_PROD", "")
	if err := ValidateBootstrapTokenForNonDevDeploy(BootstrapTokenPlaceholder); err != nil {
		t.Fatalf("dev env allows placeholder: %v", err)
	}
}
