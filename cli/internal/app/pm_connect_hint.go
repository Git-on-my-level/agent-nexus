package app

// localize never trusts remote hint text, including records from older versions.
func (f *pmConnectFailure) localize() {
	if !pmConnectIdentifier(f.Code) {
		f.Code = "unknown"
	}
	if f.Code != "pm_actor_mismatch" {
		f.ConfiguredActorID, f.ActingActorID = "", ""
	}
	if !pmConnectIdentifier(f.ConfiguredActorID) {
		f.ConfiguredActorID = ""
	}
	if !pmConnectIdentifier(f.ActingActorID) {
		f.ActingActorID = ""
	}
	if f.Status == 409 {
		f.Retryable = false
		f.Hint = "Another actor is already registered as PM. Ask a workspace operator to rebind ANX_PM_AGENT_ACTOR_ID to this runner's actor and restart anx-core before reconnecting."
		return
	}
	switch f.Code {
	case "pm_actor_mismatch":
		f.Hint = "Ask a workspace operator to rebind ANX_PM_AGENT_ACTOR_ID to the acting actor and restart anx-core."
		if f.ConfiguredActorID != "" && f.ActingActorID != "" {
			f.Hint = "Configured actor: " + f.ConfiguredActorID + "; acting actor: " + f.ActingActorID + ". Rebind ANX_PM_AGENT_ACTOR_ID to " + f.ActingActorID + " and restart anx-core."
		}
	case "auth_required", "authentication_required", "invalid_token":
		f.Hint = "Check the selected PM profile's authentication and host enrollment."
	case "forbidden", "authorization_denied", "agent_revoked", "agent_archived":
		f.Hint = "Ask a workspace operator to check the selected PM actor's authorization."
	case "network_error", "request_failed", "outcome_unknown", "storage_unavailable", "unavailable", "http_500", "http_502", "http_503", "http_504":
		f.Hint = "Check network/core availability; the runner retries with backoff."
	default:
		f.Hint = "Check workspace/core configuration for this connect failure code."
	}
}

func pmConnectIdentifier(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}
