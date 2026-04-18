//go:build windows

package app

import (
	"context"
	"time"

	"agent-nexus-cli/internal/errnorm"
)

func (a *App) runBridgeStart(ctx context.Context, args []string) (*commandResult, error) {
	return nil, errnorm.New(errnorm.KindLocal, "not_supported", "bridge commands are not supported on Windows")
}

func (a *App) runBridgeStop(args []string) (*commandResult, error) {
	return nil, errnorm.New(errnorm.KindLocal, "not_supported", "bridge commands are not supported on Windows")
}

func (a *App) runBridgeRestart(ctx context.Context, args []string) (*commandResult, error) {
	return nil, errnorm.New(errnorm.KindLocal, "not_supported", "bridge commands are not supported on Windows")
}

func (a *App) runBridgeStatus(ctx context.Context, args []string) (*commandResult, error) {
	return nil, errnorm.New(errnorm.KindLocal, "not_supported", "bridge commands are not supported on Windows")
}

func (a *App) runBridgeLogs(args []string) (*commandResult, error) {
	return nil, errnorm.New(errnorm.KindLocal, "not_supported", "bridge commands are not supported on Windows")
}

func loadBridgeManagedConfig(configPath string) (bridgeManagedConfig, error) {
	return bridgeManagedConfig{}, errnorm.New(errnorm.KindLocal, "not_supported", "bridge commands are not supported on Windows")
}

func inferBridgeRuntimeKind(content string, configPath string) (runtimeKind string, runCommand string, displayName string, err error) {
	return "", "", "", errnorm.New(errnorm.KindLocal, "not_supported", "bridge commands are not supported on Windows")
}

func bridgeConfigHasSection(content string, section string) bool {
	return false
}

func bridgeManagerDir(configPath string) string {
	return ""
}

func sanitizeBridgeManagerName(value string) string {
	return value
}

func shortBridgeHash(value string) string {
	return ""
}

func shortHex(value string, limit int) string {
	return ""
}

func loadManagedRuntimeState(path string) (bridgeManagedRuntime, bool) {
	return bridgeManagedRuntime{}, false
}

func writeManagedRuntimeState(runtimeState bridgeManagedRuntime) error {
	return errnorm.New(errnorm.KindLocal, "not_supported", "bridge commands are not supported on Windows")
}

func defaultBridgeStartManagedProcess(managedConfig bridgeManagedConfig, bridgeBinary string) (bridgeManagedRuntime, error) {
	return bridgeManagedRuntime{}, errnorm.New(errnorm.KindLocal, "not_supported", "bridge commands are not supported on Windows")
}

func defaultBridgeStopManagedProcess(runtimeState bridgeManagedRuntime, timeout time.Duration, force bool) (bridgeManagedRuntime, error) {
	return bridgeManagedRuntime{}, errnorm.New(errnorm.KindLocal, "not_supported", "bridge commands are not supported on Windows")
}

func defaultBridgeProcessAlive(pid int) bool {
	return false
}

func bridgeManagedRuntimeRunning(runtimeState bridgeManagedRuntime) (bool, string) {
	return false, ""
}

func defaultBridgeProcessCommandLine(pid int) (string, error) {
	return "", errnorm.New(errnorm.KindLocal, "not_supported", "bridge commands are not supported on Windows")
}

func tailLines(content string, limit int) string {
	return ""
}

func resolveBridgeBinary(home string, installDir string, binDir string) (string, error) {
	return "", errnorm.New(errnorm.KindLocal, "not_supported", "bridge commands are not supported on Windows")
}
