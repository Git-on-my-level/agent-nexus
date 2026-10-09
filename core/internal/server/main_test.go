package server

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	code := m.Run()
	if err := cleanupTestWorkspace(); err != nil {
		fmt.Fprintln(os.Stderr, "remove test template:", err)
		code = 1
	}
	os.Exit(code)
}
