package app

import (
	"sync"

	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/output"
)

// attributionSkips turns writes resent without run attribution into one
// warning per command that says why and how to keep attribution.
type attributionSkips struct {
	mu      sync.Mutex
	details map[string]any
}

func (s *attributionSkips) observe(details map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.details == nil {
		s.details = details
	}
}

func (s *attributionSkips) warnings() []output.Warning {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.details == nil {
		return nil
	}
	run := anyString(s.details["run"])
	message := "write saved without run attribution: core refused " + run + " (" + firstNonEmpty(anyString(s.details["message"]), anyString(s.details["reason"]), "run attribution invalid") + "). " +
		"A run is attributed to the agent that first wrote in it; write with that agent's --as to attribute, or unset AGENTCTL_EXECUTION_ID when writing as a different identity."
	return []output.Warning{{Code: "run_attribution_skipped", Message: message, Details: s.details}}
}

// annotateError keeps the skipped attribution visible when the command
// failed after a write already landed unattributed: the failure is rendered
// instead of the result, so the warning would otherwise be lost.
func (s *attributionSkips) annotateError(err error) error {
	warnings := s.warnings()
	if err == nil || len(warnings) == 0 {
		return err
	}
	e := errnorm.Normalize(err)
	details, _ := e.Details.(map[string]any)
	if details == nil {
		details = map[string]any{}
	}
	if _, taken := details["run_attribution_skipped"]; !taken {
		details["run_attribution_skipped"] = map[string]any{"message": warnings[0].Message, "details": warnings[0].Details}
	}
	e.Details = details
	return e
}
