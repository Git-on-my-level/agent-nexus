package errnorm

import (
	"fmt"
	"strings"
	"testing"
)

func TestWorkspaceContextPreservesRemoteFailure(t *testing.T) {
	err := FromHTTPFailure(404, []byte(`{"error":{"code":"not_found","message":"card missing","recoverable":false,"details":{"ref":"card:ship"}}}`))
	err.Details.(map[string]any)["retry_after"] = "10"
	got := WithWorkspace(fmt.Errorf("resolve subject: %w", err), "project", "core.example.test", "config:directory-rule:~/project/**")
	got = WithWorkspace(got, "project", "core.example.test", "config:directory-rule:~/project/**")
	details := got.Details.(map[string]any)
	if got.Code != "not_found" || ExitCode(got) != 3 || RecoverableValue(got) || details["retry_after"] != "10" || details["parsed"] == nil {
		t.Fatalf("lost original failure: %#v", got)
	}
	if !strings.HasPrefix(got.Message, "card missing;") || strings.Count(got.Message, "searched workspace") != 1 || details["workspace"] != "project" || details["workspace_source"] != "config:directory-rule:~/project/**" || !strings.Contains(got.Hint, "--workspace <alias>") {
		t.Fatalf("bad diagnostics: %#v", got)
	}
}

func TestWorkspaceContextLeavesLocalErrorsAlone(t *testing.T) {
	if got := WithWorkspace(nil, "project", "core.example.test", "config:default"); got != nil {
		t.Fatal(got)
	}
	err := Local("draft_not_found", "draft missing")
	if got := WithWorkspace(err, "project", "core.example.test", "config:default"); got.Message != "draft missing" || got.Details != nil {
		t.Fatalf("annotated a local error: %#v", got)
	}
}
