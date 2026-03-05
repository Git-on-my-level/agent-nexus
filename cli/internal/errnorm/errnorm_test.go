package errnorm

import "testing"

func TestFromHTTPFailureParsesRecoverableHint(t *testing.T) {
	t.Parallel()

	err := FromHTTPFailure(401, []byte(`{"error":{"code":"invalid_token","message":"invalid","recoverable":true,"hint":"rotate key"}}`))
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Code != "invalid_token" {
		t.Fatalf("unexpected code: %s", err.Code)
	}
	if err.Recoverable == nil || !*err.Recoverable {
		t.Fatalf("expected recoverable=true, got %#v", err.Recoverable)
	}
	if err.Hint != "rotate key" {
		t.Fatalf("unexpected hint: %q", err.Hint)
	}
}

func TestNormalizeAppliesMetadataDefaults(t *testing.T) {
	t.Parallel()

	err := Normalize(Usage("invalid_request", "bad request"))
	if err == nil {
		t.Fatal("expected normalized error")
	}
	if err.Recoverable == nil || !*err.Recoverable {
		t.Fatalf("expected recoverable=true for invalid_request, got %#v", err.Recoverable)
	}
	if err.Hint == "" {
		t.Fatal("expected non-empty hint")
	}
}
