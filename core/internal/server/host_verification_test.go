package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"
)

func TestHostEnrollmentVerificationURL(t *testing.T) {
	for _, tc := range []struct {
		name, configured, want string
		invalid                bool
	}{
		{name: "self hosted separate port", configured: "http://127.0.0.1:5291/o/local/w/local", want: "http://127.0.0.1:5291/o/local/w/local/access/hosts/enroll"},
		{name: "hosted proxy", configured: "https://example.com/o/acme/w/main/", want: "https://example.com/o/acme/w/main/access/hosts/enroll"},
		{name: "unknown", configured: "", want: ""},
		{name: "core URL", configured: "https://example.com/ws/acme/main", invalid: true},
		{name: "credentials", configured: "https://user:pass@example.com/o/acme/w/main", invalid: true},
		{name: "query", configured: "https://example.com/o/acme/w/main?x=1", invalid: true},
		{name: "empty query marker", configured: "https://example.com/o/acme/w/main?", invalid: true},
		{name: "empty fragment marker", configured: "https://example.com/o/acme/w/main#", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := HostEnrollmentVerificationURL(tc.configured)
			if tc.invalid {
				if err == nil {
					t.Fatalf("accepted invalid URL %q", tc.configured)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("verification URL = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestHostEnrollmentWithoutPublicWebURLOmitsLink(t *testing.T) {
	env := newAuthIntegrationEnv(t, authIntegrationOptions{})
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public := base64.StdEncoding.EncodeToString(pub)
	nonce := base64.RawURLEncoding.EncodeToString(pub[:16])
	status, response := hostHTTP(t, "POST", env.server.URL+"/auth/hosts/enrollments", "", map[string]any{
		"public_key": public, "requested_slug": "test-host", "os_user": "tester", "hostname": "machine",
		"discovered_adapters": []string{}, "request_nonce": nonce, "adoptions": []any{},
	})
	hostStatus(t, status, 201, response)
	if _, ok := response["verification_url"]; ok {
		t.Fatalf("unconfigured core returned a link: %#v", response)
	}
	if _, ok := response["verification_url_path"]; ok {
		t.Fatalf("obsolete verification path returned: %#v", response)
	}
	if code, _ := response["user_code"].(string); !strings.Contains(code, "-") {
		t.Fatalf("missing user code: %#v", response)
	}
}
