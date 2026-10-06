package resourceaccess

import "sort"

// OwnershipSource is the executable storage-field inventory. Every column is
// scanned recursively (JSON) or as text; triggers atomically maintain its edges.
// A source kind identifies one table, never two: replacement must not erase
// another table's evidence for the same resource.
type OwnershipSource struct {
	Table, Kind, ID string
	Columns         []string
}

var OwnershipSources = []OwnershipSource{
	{"events", "event", "id", []string{"refs_json", "payload_json", "trash_reason"}},
	{"threads", "thread", "id", []string{"body_json", "provenance_json", "trash_reason"}},
	{"topics", "topic", "id", []string{"title", "summary", "provenance_json", "extensions_json", "trash_reason"}},
	{"boards", "board", "id", []string{"title", "summary", "owners_json", "refs_json", "column_schema_json", "trash_reason"}},
	{"cards", "card", "id", []string{"title", "summary", "definition_of_done_json", "resolution_refs_json", "refs_json", "provenance_json", "pinned_document_id", "trash_reason"}},
	{"documents", "document", "id", []string{"title", "summary", "source", "search_text", "supersedes_json", "refs_json", "provenance_json", "tags_json", "hosts_json", "trash_reason"}},
	{"document_revisions", "document_revision", "revision_id", []string{"refs_json", "artifact_id", "prev_revision_id"}},
	{"card_revisions", "card_revision", "revision_id", []string{"refs_json", "artifact_id", "prev_revision_id"}},
	{"artifacts", "artifact", "id", []string{"refs_json", "metadata_json", "content_refs_json", "trash_reason"}},
	{"work_metadata", "work_metadata", "card_id", []string{"metadata_json", "refresh_json"}},
	{"work_observations", "work_observation", "id", []string{"body_json"}},
	{"work_evidence_records", "work_evidence_record", "id", []string{"evidence_json"}},
	// Lookup keys publish external ownership identities in the central graph;
	// they do not make numeric evidence IDs public resource identities. A
	// publication is distinct from a plan/work field referencing that key.
	{"work_evidence_index", "work_evidence_alias", "id", []string{"lookup_key"}},
	{"work_participants", "participant", "id", []string{"request_json"}},
	{"card_plans", "plan", "card_id", []string{"body_json"}},
	{"agent_wakeups", "wakeup", "wakeup_id", []string{"refs_json", "trigger_text", "thread_title", "failure_reason"}},
	{"runs", "run", "id", []string{"card_ref", "labels_json", "repository", "branch", "model", "external_id"}},
	{"derived_inbox_items", "inbox", "id", []string{"data_json", "thread_id", "source_card_id", "source_event_id"}},
}

// FilterSources are ancillary rows with no navigable canonical resource kind.
// Their complete rows are filtered by the same predicate before query limits.
var FilterSources = map[string][]string{
	"actors":                 {"display_name", "tags_json"},
	"agents":                 {"metadata_json"},
	"hosts":                  {"display_name", "hostname", "os_user", "discovered_adapters_json"},
	"host_enrollments":       {"adoptions_json", "discovered_adapters_json", "hostname", "os_user"},
	"host_enrollment_tokens": {"label"},
	"auth_invites":           {"note"},
	"auth_audit_events":      {"metadata_json"},
	"secrets":                {"name", "description"},
	"series_adapters":        {"description"},
	"series_definitions":     {"unit"},
}

// Ancillary profiles inherit content visibility but never become resource
// identities themselves. Their indexed denials are leaves of the graph.
func FilterOwnershipSources() []OwnershipSource {
	var sources []OwnershipSource
	for table, columns := range FilterSources {
		id := "id"
		if table == "series_adapters" || table == "series_definitions" {
			id = "name"
		}
		sources = append(sources, OwnershipSource{table, "filter/" + table, id, columns})
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Table < sources[j].Table })
	return sources
}
