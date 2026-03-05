package schema

import (
	"fmt"
	"sort"
	"strings"
)

func ValidateEnum(contract *Contract, enumName string, value string) error {
	if contract == nil {
		return fmt.Errorf("schema contract is required")
	}

	enum, ok := contract.Enums[enumName]
	if !ok {
		return fmt.Errorf("enum %q is not defined", enumName)
	}

	if enum.Policy == EnumPolicyOpen {
		return nil
	}

	if _, exists := enum.Values[value]; exists {
		return nil
	}

	allowed := append([]string(nil), enum.OrderedValue...)
	sort.Strings(allowed)
	return fmt.Errorf("invalid value %q for strict enum %s (allowed: %s)", value, enumName, strings.Join(allowed, ", "))
}

func SplitTypedRef(ref string) (string, string, error) {
	idx := strings.Index(ref, ":")
	if idx <= 0 || idx >= len(ref)-1 {
		return "", "", fmt.Errorf("typed ref %q must be in \"<prefix>:<value>\" form", ref)
	}

	prefix := strings.TrimSpace(ref[:idx])
	value := strings.TrimSpace(ref[idx+1:])
	if prefix == "" || value == "" {
		return "", "", fmt.Errorf("typed ref %q must be in \"<prefix>:<value>\" form", ref)
	}

	return prefix, value, nil
}

func ValidateTypedRef(_ *Contract, ref string) error {
	_, _, err := SplitTypedRef(ref)
	return err
}

func ValidateTypedRefs(contract *Contract, refs []string) error {
	for i, ref := range refs {
		if err := ValidateTypedRef(contract, ref); err != nil {
			return fmt.Errorf("refs[%d]: %w", i, err)
		}
	}
	return nil
}

func ValidateProvenance(contract *Contract, provenance map[string]any) error {
	if contract == nil {
		return fmt.Errorf("schema contract is required")
	}
	if provenance == nil {
		return fmt.Errorf("provenance is required")
	}

	sourcesSpec, ok := contract.Provenance.Fields["sources"]
	if !ok {
		return fmt.Errorf("schema provenance.sources definition is missing")
	}
	if sourcesSpec.Required {
		rawSources, exists := provenance["sources"]
		if !exists {
			return fmt.Errorf("provenance.sources is required")
		}
		if _, err := normalizeStringList(rawSources); err != nil {
			return fmt.Errorf("provenance.sources: %w", err)
		}
	}

	notesSpec, hasNotesSpec := contract.Provenance.Fields["notes"]
	rawNotes, hasNotes := provenance["notes"]
	if hasNotes {
		if _, ok := rawNotes.(string); !ok {
			return fmt.Errorf("provenance.notes must be a string")
		}
	} else if hasNotesSpec && notesSpec.Required {
		return fmt.Errorf("provenance.notes is required")
	}

	byFieldSpec, hasByFieldSpec := contract.Provenance.Fields["by_field"]
	rawByField, hasByField := provenance["by_field"]
	if hasByField {
		if err := validateByField(rawByField); err != nil {
			return fmt.Errorf("provenance.by_field: %w", err)
		}
	} else if hasByFieldSpec && byFieldSpec.Required {
		return fmt.Errorf("provenance.by_field is required")
	}

	return nil
}

func normalizeStringList(raw any) ([]string, error) {
	switch values := raw.(type) {
	case []string:
		for _, v := range values {
			if strings.TrimSpace(v) == "" {
				return nil, fmt.Errorf("list items must be non-empty strings")
			}
		}
		return values, nil
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			v, ok := value.(string)
			if !ok || strings.TrimSpace(v) == "" {
				return nil, fmt.Errorf("list items must be non-empty strings")
			}
			out = append(out, v)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("must be a list of strings")
	}
}

func validateByField(raw any) error {
	switch fields := raw.(type) {
	case map[string][]string:
		for key, values := range fields {
			if strings.TrimSpace(key) == "" {
				return fmt.Errorf("field keys must be non-empty")
			}
			if _, err := normalizeStringList(values); err != nil {
				return fmt.Errorf("field %q: %w", key, err)
			}
		}
		return nil
	case map[string]any:
		for key, rawValues := range fields {
			if strings.TrimSpace(key) == "" {
				return fmt.Errorf("field keys must be non-empty")
			}
			if _, err := normalizeStringList(rawValues); err != nil {
				return fmt.Errorf("field %q: %w", key, err)
			}
		}
		return nil
	default:
		return fmt.Errorf("must be a map of string to list<string>")
	}
}
