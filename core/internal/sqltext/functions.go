// Package sqltext provides deterministic projection keys shared by SQL and Go.
package sqltext

import (
	"database/sql/driver"
	"modernc.org/sqlite"
	"strings"
	"time"
)

func init() {
	sqlite.MustRegisterDeterministicScalarFunction("anx_unicode_lower", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		s, _ := args[0].(string)
		return strings.ToLower(s), nil
	})
	sqlite.MustRegisterDeterministicScalarFunction("anx_unicode_trim", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		s, _ := args[0].(string)
		return strings.TrimSpace(s), nil
	})
	sqlite.MustRegisterDeterministicScalarFunction("anx_timestamp_key", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		s, _ := args[0].(string)
		t, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return "", nil
		}
		return strings.TrimSuffix(t.UTC().Format(time.RFC3339Nano), "Z"), nil
	})
}
