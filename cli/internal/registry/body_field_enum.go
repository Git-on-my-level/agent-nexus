package registry

import (
	"sort"
	"strings"
)

// BodyFieldEnum returns sorted enum_values for a body field on a command identified by cli_path
// (e.g. "boards patch" + "patch.status"). False if not found or no enum_values.
func BodyFieldEnum(cliPath, fieldName string) ([]string, bool) {
	cliPath = strings.TrimSpace(cliPath)
	fieldName = strings.TrimSpace(fieldName)
	if cliPath == "" || fieldName == "" {
		return nil, false
	}
	meta, err := LoadEmbedded()
	if err != nil {
		return nil, false
	}
	for _, cmd := range meta.Commands {
		if strings.TrimSpace(cmd.CLIPath) != cliPath {
			continue
		}
		if cmd.BodySchema == nil {
			continue
		}
		fields := append(append([]BodyField{}, cmd.BodySchema.Required...), cmd.BodySchema.Optional...)
		for _, f := range fields {
			if strings.TrimSpace(f.Name) != fieldName {
				continue
			}
			if len(f.EnumValues) == 0 {
				return nil, false
			}
			out := uniqueSortedStrings(f.EnumValues)
			return out, true
		}
	}
	return nil, false
}

func uniqueSortedStrings(in []string) []string {
	seen := map[string]struct{}{}
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		seen[v] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
