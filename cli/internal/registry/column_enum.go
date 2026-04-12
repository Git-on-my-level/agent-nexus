package registry

import (
	"sort"
	"strings"
	"sync"
)

var columnKeyEnumOnce sync.Once
var columnKeyEnumValues []string

// ColumnKeyEnumValues returns sorted unique enum values for board column_key from
// embedded command metadata (e.g. cards move). Empty if none are declared.
func ColumnKeyEnumValues() []string {
	columnKeyEnumOnce.Do(func() {
		meta, err := LoadEmbedded()
		if err != nil {
			return
		}
		seen := map[string]struct{}{}
		for _, cmd := range meta.Commands {
			if cmd.BodySchema == nil {
				continue
			}
			fields := append(append([]BodyField{}, cmd.BodySchema.Required...), cmd.BodySchema.Optional...)
			for _, f := range fields {
				name := strings.TrimSpace(f.Name)
				if name != "column_key" && name != "move.column_key" {
					continue
				}
				for _, v := range f.EnumValues {
					v = strings.TrimSpace(v)
					if v == "" {
						continue
					}
					seen[v] = struct{}{}
				}
			}
		}
		out := make([]string, 0, len(seen))
		for v := range seen {
			out = append(out, v)
		}
		sort.Strings(out)
		columnKeyEnumValues = out
	})
	out := make([]string, len(columnKeyEnumValues))
	copy(out, columnKeyEnumValues)
	return out
}
