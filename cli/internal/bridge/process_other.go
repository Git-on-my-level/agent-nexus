//go:build !darwin && !linux

package bridge

import (
	"context"
	"fmt"
	"os"
)

func Processes(context.Context) ([]Process, error) {
	return nil, fmt.Errorf("host bridge requires macOS or Linux")
}

func unitOwned(os.FileInfo) bool { return false }
