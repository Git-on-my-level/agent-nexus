package resourceaccess

import (
	"database/sql/driver"
	"encoding/json"
	"strings"

	"modernc.org/sqlite"
)

// ExternalKeyPublications is the field inventory distinguishing published
// identities from references to them. Native resource atoms are always scanned
// by OwnershipSources; this distinction only affects external-key inheritance.
// Unknown fields, including extensions of a source entry, remain references.
var ExternalKeyPublications = map[string][]string{
	"work_metadata":        {"metadata_json.source", "metadata_json.source_refs.*"},
	"work_observation":     {"body_json.facts", "body_json.evidence.*"},
	"work_evidence_record": {"evidence_json"},
}

var publishedIdentityFields = []string{"authority", "connection_id", "native_id", "url", "identifier", "identifier_aliases", "aliases"}

func removePublishedIdentity(value any, path []string) {
	if len(path) == 0 {
		if obj, ok := value.(map[string]any); ok {
			for _, field := range publishedIdentityFields {
				delete(obj, field)
			}
		}
		return
	}
	if path[0] == "*" {
		if items, ok := value.([]any); ok {
			for _, item := range items {
				removePublishedIdentity(item, path[1:])
			}
		}
	} else if obj, ok := value.(map[string]any); ok {
		removePublishedIdentity(obj[path[0]], path[1:])
	}
}

func externalKeyReferenceAtomsJSON(value, kind string) string {
	var body any
	if json.Unmarshal([]byte(value), &body) != nil {
		return ReferenceAtomsJSON(value)
	}
	for _, path := range ExternalKeyPublications[kind] {
		removePublishedIdentity(body, strings.Split(path, "."))
	}
	raw, _ := json.Marshal(body)
	atoms, _ := json.Marshal(referenceAtoms(string(raw), true))
	return string(atoms)
}

// ExternalKeyReferenceExpression indexes non-publication atoms once per
// canonical write, rather than decoding a publisher once per matching alias.
func ExternalKeyReferenceExpression(source OwnershipSource, columns []string, prefix string) string {
	fields := []string{}
	for _, col := range columns {
		value := prefix + col
		fields = append(fields, "'"+col+"',CASE WHEN json_valid("+value+") THEN json("+value+") ELSE "+value+" END")
	}
	return "anx_resource_external_key_refs(CAST(json_object(" + strings.Join(fields, ",") + ") AS BLOB),CAST('" + source.Kind + "' AS BLOB))"
}

func init() {
	sqlite.MustRegisterDeterministicScalarFunction("anx_resource_external_key_refs", 2, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		value, err := referenceBytes(args[0])
		if err != nil {
			return nil, err
		}
		kind, err := referenceBytes(args[1])
		if err != nil {
			return nil, err
		}
		return externalKeyReferenceAtomsJSON(value, kind), nil
	})
}
