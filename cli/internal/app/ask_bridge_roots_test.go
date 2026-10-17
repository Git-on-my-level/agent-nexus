//go:build !windows

package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	hostbridge "agent-nexus-cli/internal/bridge"
	"agent-nexus-cli/internal/config"
)

func TestHostBridgeFencesEveryExecutedResumeRoot(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	legacy, e := lockAskBridgeReady(filepath.Join(second, "consumer.lock"))
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	a := newTestApp(t)
	if e = a.consumeHostWakes(ctx, config.Resolved{}, hostbridge.Agent{RegistryDirs: []string{first, second}}, nil); e == nil {
		t.Fatal("host overlapped legacy on second root")
	}
	// Failure must release all partial acquisitions, regardless of path order.
	probe, e := lockAskBridgeReady(filepath.Join(first, "consumer.lock"))
	if e != nil {
		t.Fatal(e)
	}
	probe()
	legacy()
	unlock, e := lockResumeRoots([]string{second, first, second})
	if e != nil {
		t.Fatal(e)
	}
	defer unlock()
	for _, dir := range []string{first, second} {
		if askBridgeLockState(filepath.Join(dir, "consumer.lock")) != answerBridgeRunning {
			t.Fatal("unfenced executed root", dir)
		}
	}
}
