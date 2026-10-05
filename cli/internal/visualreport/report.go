// Package visualreport exposes the shared contract to CLI report commands.
package visualreport

import shared "agent-nexus-visualreport"

const (
	Kind      = shared.Kind
	Version   = shared.Version
	MaxBytes  = shared.MaxBytes
	MaxErrors = shared.MaxErrors
)

type Result = shared.Result
type Query = shared.Query

func Validate(content []byte) Result { return shared.Validate(content) }

func Canonicalize(content []byte) []byte { return shared.Canonicalize(content) }

func PanelTypes() []string { return shared.PanelTypes() }

func IsLive(kind string) bool { return shared.IsLive(kind) }

func ParseQuery(kind string, content []byte) (Query, error) {
	return shared.ParseQuery(kind, content)
}
