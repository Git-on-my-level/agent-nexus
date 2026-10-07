package scopedrepo

// Validate again at the read boundary so malformed legacy/imported state cannot
// become a usable proof/cursor binding even if SQLite checks were bypassed.
func validWorkspaceNamespace(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, c := range value {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
