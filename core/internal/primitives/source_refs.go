package primitives

import (
	"encoding/json"
	"net/url"
	"strings"
	"time"
)

// Source refs are generic card-backed work annotations. No adapter format or
// credentials enter this contract. Omitted lists preserve; empty lists clear.
func validateSourceRefs(raw any) error {
	body, err := json.Marshal(raw)
	var entries []map[string]any
	if err != nil || json.Unmarshal(body, &entries) != nil || entries == nil || len(entries) > 2000 {
		return workInvalid("source_refs must be an array of at most 2000 source identities")
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		for _, key := range []string{"authority", "connection_id", "native_id"} {
			value, ok := entry[key].(string)
			if !ok || strings.TrimSpace(value) == "" {
				return workInvalid("source_refs requires nonempty string %s", key)
			}
		}
		identity := workString(entry["authority"]) + "\x00" + workString(entry["connection_id"]) + "\x00" + workString(entry["native_id"])
		if seen[identity] {
			return workInvalid("source_refs contains duplicate source identity")
		}
		seen[identity] = true
		if err := validateIndexedAliases(entry, "source_refs"); err != nil {
			return err
		}
		for _, key := range []string{"identifier", "title", "url", "status", "phase", "observed_at", "source_activity_at"} {
			rawValue, exists := entry[key]
			if !exists || rawValue == nil {
				continue
			}
			value, ok := rawValue.(string)
			if !ok {
				return workInvalid("source_refs.%s must be a string or null", key)
			}
			if value == "" {
				continue
			}
			switch key {
			case "url":
				u, err := url.Parse(value)
				if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
					return workInvalid("source_refs.url must be an absolute HTTP(S) URL without credentials")
				}
			case "observed_at", "source_activity_at":
				if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
					return workInvalid("source_refs.%s must be an RFC3339 timestamp", key)
				}
			}
		}
	}
	return nil
}

// All inputs projected into the index share alias caps, including source and
// observation facts/evidence extensions. Unknown non-indexed fields round-trip.
func validateIndexedAliases(entry map[string]any, path string) error {
	for _, key := range []string{"authority", "connection_id", "native_id", "identifier", "url"} {
		raw, exists := entry[key]
		if !exists || raw == nil {
			continue
		}
		value, ok := raw.(string)
		if !ok || len(value) > 2048 {
			return workInvalid("%s.%s must be a string of at most 2048 bytes", path, key)
		}
	}
	for _, key := range []string{"identifier_aliases", "aliases"} {
		raw, exists := entry[key]
		if !exists {
			continue
		}
		data, _ := json.Marshal(raw)
		var aliases []string
		if json.Unmarshal(data, &aliases) != nil || aliases == nil || len(aliases) > 50 {
			return workInvalid("%s.%s must be an array of at most 50 strings", path, key)
		}
		seen := map[string]bool{}
		for _, alias := range aliases {
			if strings.TrimSpace(alias) == "" || len(alias) > 2048 || seen[alias] {
				return workInvalid("%s.%s must contain unique nonempty strings of at most 2048 bytes", path, key)
			}
			seen[alias] = true
		}
	}
	return nil
}
func validateObservationAliases(observation map[string]any) error {
	if err := validateIndexedAliases(workMap(observation["facts"]), "facts"); err != nil {
		return err
	}
	raw, exists := observation["evidence"]
	if !exists {
		return nil
	}
	data, err := json.Marshal(raw)
	var entries []map[string]any
	if err != nil || json.Unmarshal(data, &entries) != nil || entries == nil || len(entries) > 2000 {
		return workInvalid("evidence must be an array of at most 2000 objects")
	}
	for _, entry := range entries {
		if err := validateIndexedAliases(entry, "evidence"); err != nil {
			return err
		}
	}
	return nil
}
