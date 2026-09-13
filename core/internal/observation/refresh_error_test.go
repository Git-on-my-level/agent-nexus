package observation

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestHumanRefreshErrorMapping(t *testing.T) {
	next := time.Date(2026, 9, 13, 5, 0, 0, 0, time.UTC)
	cases := []struct {
		err     error
		source  string
		next    time.Time
		code    string
		contain string
	}{
		{failure(ErrPolicy, "generated reader has no active version"), "github", time.Time{}, "policy_denied", "Generated reader has no active version"},
		{&ReadError{Kind: ErrRateLimit, Message: "source request failed", RetryAfter: time.Hour}, "github", next, "rate_limited", "GitHub rate limit reached; next attempt at 2026-09-13T05:00:00Z"},
		{failure(ErrPermission, "source credential unavailable"), "github", time.Time{}, "permission", "Source credential unavailable"},
		{&ReadError{Kind: ErrPermission, Message: "source request failed", Status: 401}, "github", time.Time{}, "permission", "Source authentication failed"},
		{fmt.Errorf("store write"), "github", time.Time{}, "unavailable", "Source read or observation persistence failed"},
	}
	for _, tc := range cases {
		code, message := HumanRefreshError(tc.err, tc.source, tc.next)
		if code != tc.code || !strings.Contains(message, tc.contain) {
			t.Fatalf("code=%s message=%q want %s %q", code, message, tc.code, tc.contain)
		}
		got := PersistableRefreshError(tc.err, tc.source, tc.next)
		if got["code"] != tc.code || anyString(got["message"]) != message {
			t.Fatalf("persistable %#v", got)
		}
	}
}

func anyString(v any) string {
	s, _ := v.(string)
	return s
}
