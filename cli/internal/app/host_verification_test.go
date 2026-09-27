package app

import (
	"bytes"
	"strings"
	"testing"
)

func TestHostEnrollmentInstructionsUseOnlyConfiguredWebURL(t *testing.T) {
	for _, tc := range []struct {
		name, verify, want string
	}{
		{name: "self hosted separate port", verify: "http://127.0.0.1:5291/o/local/w/local/access/hosts/enroll", want: "verification_url=http://127.0.0.1:5291/o/local/w/local/access/hosts/enroll"},
		{name: "hosted proxy", verify: "https://example.com/o/acme/w/main/access/hosts/enroll", want: "verification_url=https://example.com/o/acme/w/main/access/hosts/enroll"},
		{name: "unknown", want: "open Access → Hosts in the workspace web UI"},
		{name: "invalid", verify: "/access/hosts/enroll", want: "open Access → Hosts in the workspace web UI"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			writeEnrollmentInstructions(&out, map[string]any{"user_code": "ABCD-EFGH", "verification_url": tc.verify})
			if !strings.Contains(out.String(), "user_code=ABCD-EFGH") || !strings.Contains(out.String(), tc.want) {
				t.Fatalf("wrong instructions: %q", out.String())
			}
			if tc.verify == "" || strings.HasPrefix(tc.verify, "/") {
				if strings.Contains(out.String(), "verification_url=") {
					t.Fatalf("invented verification URL: %q", out.String())
				}
			}
		})
	}
}
