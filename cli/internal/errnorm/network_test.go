package errnorm

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"net"
	"net/url"
	"strings"
	"syscall"
	"testing"
)

func TestTransportClassificationDoesNotExposeURL(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cause error
		class string
		exit  int
	}{
		{"timeout", context.DeadlineExceeded, "timeout", 8},
		{"refused", syscall.ECONNREFUSED, "connection_refused", 6},
		{"reset", syscall.ECONNRESET, "connection_reset", 6},
		{"dns", &net.DNSError{Err: "no such host", Name: "secret.invalid"}, "dns", 6},
		{"tls", x509.UnknownAuthorityError{}, "tls", 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Normalize(Wrap(KindNetwork, "request_failed", "host assertion failed", &url.Error{Op: "Post", URL: "https://user:token@example.invalid?token=secret", Err: tc.cause}))
			data, _ := json.Marshal(struct {
				Message string
				Hint    string
				Details any
			}{err.Message, err.Hint, err.Details})
			details, _ := err.Details.(map[string]any)
			if details["failure_class"] != tc.class || ExitCode(err) != tc.exit || strings.Contains(string(data), "secret") || strings.Contains(err.Error(), "secret") || strings.Contains(err.Hint, "credentials") || !strings.Contains(err.Message, strings.ReplaceAll(tc.class, "_", " ")) {
				t.Fatalf("exit=%d output=%s", ExitCode(err), data)
			}
		})
	}
}

func TestHTTPStatusClassification(t *testing.T) {
	for _, status := range []int{401, 403, 404, 502, 503, 504} {
		err := FromHTTPFailure(status, []byte("proxy unavailable"))
		d := err.Details.(map[string]any)
		class, exit := "http_status", 1
		if status == 401 || status == 403 {
			class, exit = "auth_rejection", 5
		}
		if status >= 500 {
			exit = 6
		}
		if d["failure_class"] != class || ExitCode(err) != exit {
			t.Fatalf("status=%d exit=%d error=%+v", status, ExitCode(err), err)
		}
	}
}
