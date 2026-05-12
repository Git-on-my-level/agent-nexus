package main

import (
	"strings"
	"testing"
)

func TestValidateDevAuthBypassConfigRejectsProductionLikeBypasses(t *testing.T) {
	t.Setenv("ANX_HOSTED_DEV_MODE", "")
	t.Setenv("ANX_ENV", "production")

	err := validateDevAuthBypassConfig("127.0.0.1:8000", true, false)
	if err == nil {
		t.Fatal("expected dev actor mode to require hosted dev mode")
	}
	if !strings.Contains(err.Error(), "ANX_HOSTED_DEV_MODE=1") {
		t.Fatalf("expected clear hosted dev mode error, got %v", err)
	}

	err = validateDevAuthBypassConfig("127.0.0.1:8000", false, true)
	if err == nil {
		t.Fatal("expected unauthenticated writes to require hosted dev mode")
	}
	if !strings.Contains(err.Error(), "ANX_HOSTED_DEV_MODE=1") {
		t.Fatalf("expected clear hosted dev mode error, got %v", err)
	}
}

func TestValidateDevAuthBypassConfigRequiresLoopback(t *testing.T) {
	t.Setenv("ANX_HOSTED_DEV_MODE", "1")

	for _, addr := range []string{"0.0.0.0:8000", ":8000", "[::]:8000", "192.0.2.10:8000"} {
		if err := validateDevAuthBypassConfig(addr, true, true); err == nil {
			t.Fatalf("expected loopback error for %q", addr)
		}
	}
}

func TestValidateDevAuthBypassConfigAllowsExplicitLoopbackDev(t *testing.T) {
	t.Setenv("ANX_HOSTED_DEV_MODE", "1")

	for _, addr := range []string{"127.0.0.1:8000", "localhost:8000", "[::1]:8000"} {
		if err := validateDevAuthBypassConfig(addr, true, true); err != nil {
			t.Fatalf("expected %q to be allowed in hosted dev mode: %v", addr, err)
		}
	}
}

func TestValidateDevAuthBypassConfigAllowsProductionWhenBypassesDisabled(t *testing.T) {
	t.Setenv("ANX_HOSTED_DEV_MODE", "")
	t.Setenv("ANX_ENV", "production")

	if err := validateDevAuthBypassConfig("0.0.0.0:8000", false, false); err != nil {
		t.Fatalf("disabled bypasses should not constrain production bind address: %v", err)
	}
}
