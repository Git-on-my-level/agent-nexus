// Ask-template flag validation and authoring warnings shared by ask-style
// authoring commands and usage preflight.

package app

import (
	"strings"

	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/output"
)

func hasAskTemplate(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--template" || strings.HasPrefix(arg, "--template=") {
			return true
		}
		// String flag values can themselves be "--template". Leave unknown flags
		// to normal usage validation; the only boolean authoring flags are below.
		if strings.HasPrefix(arg, "--") && !strings.Contains(arg, "=") && arg != "--force" && arg != "--dry-run" && arg != "--preview" {
			i++
		}
	}
	return false
}
func validateAskTemplate(args []string) error {
	if (len(args) == 2 && args[0] == "--template" && args[1] == "decision") || (len(args) == 1 && args[0] == "--template=decision") {
		return nil
	}
	return errnorm.Usage("invalid_flags", "usage: anx ask --template decision (prints Markdown; cannot combine with publishing flags)")
}
func askAuthoringWarnings(warnings []string) []output.Warning {
	out := make([]output.Warning, 0, len(warnings))
	for _, warning := range warnings {
		out = append(out, output.Warning{Code: "ask_authoring", Message: warning})
	}
	return out
}
