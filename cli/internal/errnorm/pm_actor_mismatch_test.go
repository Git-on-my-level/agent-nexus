package errnorm

import (
	"strings"
	"testing"
)

func TestPMActorMismatchRepair(t *testing.T) {
	for _, body := range []string{
		`{"error":{"code":"pm_actor_mismatch","message":"configured old; acting new","hint":"Rebind ANX_PM_AGENT_ACTOR_ID to new and restart anx-core.","recoverable":false,"details":{"configured_actor_id":"old","acting_actor_id":"new"}}}`,
		`{"error":{"code":"pm_actor_mismatch","message":"configured old; acting new"}}`,
	} {
		err := FromHTTPFailure(403, []byte(body))
		if ExitCode(err) != 5 || err.Recoverable == nil || *err.Recoverable || !strings.Contains(err.Hint, "ANX_PM_AGENT_ACTOR_ID") || strings.Contains(err.Hint, "whoami") {
			t.Fatalf("wrong repair: %+v", err)
		}
	}
}
