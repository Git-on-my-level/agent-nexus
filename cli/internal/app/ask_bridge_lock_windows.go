//go:build windows

package app

import (
	"fmt"
	"os/exec"
)

func lockAskBridge(path string) (func(), error) {
	return nil, fmt.Errorf("answer bridge requires macOS or Linux")
}

func configureAnswerCommand(cmd *exec.Cmd) {}
