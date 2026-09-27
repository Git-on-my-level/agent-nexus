package hostidentity

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestResolveUsesHostTokenAndExplicitAgent(t *testing.T) {
	var got [4]string
	resolved, err := Resolve(context.Background(), Options{As: "reviewer", BaseURL: "https://core.test", ConfigDir: "/tmp/anx", Timeout: 7 * time.Second}, Environment{
		Getenv: func(key string) string {
			if key == "ANX_AS" {
				return "ignored"
			}
			return ""
		},
		RunToken: func(_ context.Context, anxPath, configDir, baseURL, agent string) ([]byte, error) {
			got = [4]string{anxPath, configDir, baseURL, agent}
			return []byte(`{"ok":true,"result":{"token":"derived-token","agent":{"handle":"reviewer.host"}}}`), nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Agent != "reviewer" || resolved.AccessToken != "derived-token" || resolved.Timeout != 7*time.Second {
		t.Fatalf("unexpected resolved identity: %+v", resolved)
	}
	if got != [4]string{"anx", "/tmp/anx", "https://core.test", "reviewer"} {
		t.Fatalf("unexpected token command: %#v", got)
	}
}

func TestResolveRequiresAsAndAbsoluteConfigDir(t *testing.T) {
	_, err := Resolve(context.Background(), Options{ConfigDir: "/tmp/anx"}, Environment{})
	if err == nil || !strings.Contains(err.Error(), "--as") {
		t.Fatalf("expected --as guidance, got %v", err)
	}
	_, err = Resolve(context.Background(), Options{As: "codex", ConfigDir: "relative"}, Environment{})
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("expected absolute config directory error, got %v", err)
	}
}

func TestResolveRejectsFailedOrMismatchedHostToken(t *testing.T) {
	for _, response := range []string{
		`{"ok":false,"error":{"message":"host not enrolled"}}`,
		`{"ok":true,"result":{"token":"token","agent":{"handle":"other.host"}}}`,
	} {
		_, err := Resolve(context.Background(), Options{As: "codex", ConfigDir: "/tmp/anx"}, Environment{
			RunToken: func(context.Context, string, string, string, string) ([]byte, error) { return []byte(response), nil },
		})
		if err == nil {
			t.Fatalf("expected failure for response %s", response)
		}
	}
}
