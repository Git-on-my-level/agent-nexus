package resourceaccess

import (
	"database/sql/driver"
	"encoding/json"
	"strings"
	"unicode"

	"modernc.org/sqlite"
)

// WithoutProse returns a reference manifest with prose bodies removed. The
// manifest is a sorted JSON array of atoms produced by referenceAtoms; a prose
// body contributes the whole string verbatim plus a $anx-ref-text$ candidate,
// while the references embedded in it are already separate atoms. Dropping the
// body and its candidate therefore keeps every reference atom and every
// identity that can be resolved from the text, without storing the text twice.
//
// This is a storage transform only. Callers must keep the raw access edges
// created from the complete manifest; the artifact update trigger recognizes
// exactly this transform and does not rebuild them. Malformed input is
// returned unchanged so a caller can never prune an unclassified manifest.
func WithoutProse(manifest string) string {
	var atoms []string
	if err := json.Unmarshal([]byte(manifest), &atoms); err != nil {
		return manifest
	}
	kept := make([]string, 0, len(atoms))
	for _, atom := range atoms {
		if proseManifestAtom(atom) {
			continue
		}
		kept = append(kept, atom)
	}
	encoded, err := json.Marshal(kept)
	if err != nil {
		return manifest
	}
	return string(encoded)
}

// proseManifestAtom classifies one manifest element with the same rules that
// produced it. Typed references (including legacy IDs with internal
// whitespace), exact URLs and bare identifier atoms are references. Everything
// else is prose text whose embedded references are separate atoms.
func proseManifestAtom(atom string) bool {
	if strings.HasPrefix(atom, textReferencePrefix) {
		return true
	}
	trimmed := strings.TrimSpace(atom)
	if trimmed == "" {
		return true
	}
	if kind, _, ok := strings.Cut(trimmed, ":"); ok {
		switch strings.ToLower(strings.TrimSpace(kind)) {
		case "thread", "board", "card", "topic", "document", "doc", "event", "artifact",
			"card_revision", "document_revision", "wakeup", "plan", "inbox", "run":
			return false
		}
	}
	if embeddedURL.FindString(trimmed) == trimmed {
		return false
	}
	// A bare identifier has no separators. Keep it for legacy bare-ID matching;
	// prose has whitespace or an embedded reference marker.
	if !strings.ContainsFunc(atom, unicode.IsSpace) && !embeddedRefStart.MatchString(atom) {
		return false
	}
	return true
}

func init() {
	sqlite.MustRegisterDeterministicScalarFunction("anx_resource_without_prose", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		manifest, err := referenceBytes(args[0])
		if err != nil {
			return nil, err
		}
		return WithoutProse(manifest), nil
	})
}
