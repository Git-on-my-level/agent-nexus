// PM actor configuration for the embedded PM runtime.

package main

import (
	"fmt"
	"io"
)

// configuredPMActor warns once at startup while leaving read-only PM records available.
func configuredPMActor(stderr io.Writer) string {
	actor := envString("ANX_PM_AGENT_ACTOR_ID", "")
	if actor == "" {
		fmt.Fprintln(stderr, "WARNING: ANX_PM_AGENT_ACTOR_ID is empty; select a local PM with anx --as pm pm install on your computer")
	}
	return actor
}
