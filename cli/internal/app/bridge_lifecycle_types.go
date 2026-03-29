package app

import (
	"os"
	"regexp"
	"time"
)

var (
	bridgeReadFile             = os.ReadFile
	bridgeOpenFile             = os.OpenFile
	bridgeSectionHeaderPattern = regexp.MustCompile(`^\s*\[([A-Za-z0-9_-]+)\]\s*(?:#.*)?$`)

	bridgeStartManagedProcess func(bridgeManagedConfig, string) (bridgeManagedRuntime, error)
	bridgeStopManagedProcess  func(bridgeManagedRuntime, time.Duration, bool) (bridgeManagedRuntime, error)
	bridgeProcessAlive        func(int) bool
	bridgeProcessCommandLine  func(int) (string, error)
)

type bridgeManagedRuntime struct {
	Kind             string   `json:"kind"`
	ConfigPath       string   `json:"config_path"`
	ManagerDir       string   `json:"manager_dir"`
	ProcessStatePath string   `json:"process_state_path"`
	LogPath          string   `json:"log_path"`
	BridgeBinary     string   `json:"bridge_binary"`
	Command          []string `json:"command"`
	PID              int      `json:"pid"`
	PGID             int      `json:"pgid"`
	StartedAt        string   `json:"started_at"`
	StoppedAt        string   `json:"stopped_at"`
	LastSignal       string   `json:"last_signal"`
}

type bridgeManagedConfig struct {
	RuntimeKind      string
	RunCommand       string
	ConfigPath       string
	DisplayName      string
	ManagerDir       string
	ProcessStatePath string
	LogPath          string
	RouterStatePath  string
}
