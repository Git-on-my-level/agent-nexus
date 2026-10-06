package storage

import (
	"database/sql/driver"

	"agent-nexus-core/internal/handles"
	"modernc.org/sqlite"
)

func init() {
	// Read predicates must resolve the same aliases as public serialization.
	// This is a runtime function only; no persisted schema depends on it.
	sqlite.MustRegisterDeterministicScalarFunction("anx_normalize_handle", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		value, _ := args[0].(string)
		return handles.Normalize(value), nil
	})
}
