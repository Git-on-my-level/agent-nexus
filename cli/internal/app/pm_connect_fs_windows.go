package app

import "fmt"

// PM user services are supported only on Unix platforms.
func writePMConnectFile(string, []byte) error {
	return fmt.Errorf("PM connect diagnostics are unsupported on Windows")
}
