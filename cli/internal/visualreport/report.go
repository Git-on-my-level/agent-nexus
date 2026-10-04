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

func Validate(content []byte) Result { return shared.Validate(content) }

func PanelTypes() []string { return shared.PanelTypes() }
