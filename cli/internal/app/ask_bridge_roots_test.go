//go:build !windows

package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	hostbridge "agent-nexus-cli/internal/bridge"
	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/integrations"
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

func TestHostBridgeDoctorDistinguishesLiveAndStaleExecution(t *testing.T) {
	w := hostbridge.Workspace{BaseURL: "http://fixture.test", ID: "ws"}
	agent := hostbridge.Agent{ActorID: "actor", Jobs: map[string]bool{"wake": true}}
	dir := filepath.Join(t.TempDir(), hostbridge.Namespace(w.BaseURL, agent.ActorID))
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := askRegistryPath(dir, "ask")
	entry := askResumeRegistration{BaseURL: w.BaseURL, ActorID: agent.ActorID, State: "executing", Command: "secret callback command"}
	if err := writeAskRegistration(path, entry); err != nil {
		t.Fatal(err)
	}
	assertCheck := func(ok bool, status string) {
		t.Helper()
		check := inspectBridgeResumeCheck(dir, w, agent)
		if check["ok"] != ok || ok && check["status"] != status || !ok && check["error"] == nil {
			t.Fatal(check)
		}
		raw, _ := json.Marshal(check)
		if strings.Contains(string(raw), entry.Command) {
			t.Fatal("command leaked", string(raw))
		}
	}
	assertCheck(false, "") // stale execution, no namespace owner
	unlock, err := lockAskBridgeReady(filepath.Join(dir, "consumer.lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if unlock != nil {
			unlock()
		}
	}()
	assertCheck(true, "in_progress")
	if err := checkResumeNamespace(dir, w, agent); err == nil {
		t.Fatal("startup adopted prior execution")
	}
	entry.State = "uncertain"
	if err := writeAskRegistration(path, entry); err != nil {
		t.Fatal(err)
	}
	assertCheck(false, "") // a live lock never excuses known uncertainty
	entry.State = "delivered"
	if err := writeAskRegistration(path, entry); err != nil {
		t.Fatal(err)
	}
	runtimePath := filepath.Join(dir, "runtime-receipts", "wake.json")
	if err := integrations.PrivateWrite(runtimePath, integrations.JSON(runtimeWakeReceipt{State: "executing"})); err != nil {
		t.Fatal(err)
	}
	assertCheck(true, "in_progress")
	if err := checkRuntimeReceipts(dir); err == nil {
		t.Fatal("startup adopted prior runtime execution")
	}
	unlock()
	unlock = nil
	assertCheck(false, "") // runtime execution also requires a live owner
	if err := integrations.PrivateWrite(runtimePath, integrations.JSON(runtimeWakeReceipt{State: "delivered"})); err != nil {
		t.Fatal(err)
	}
	assertCheck(true, "ready")
	// A type error quotes a JSON field name; do not echo attacker/user text.
	if err := os.WriteFile(path, []byte(`{"command":{"secret callback command":false}}`), 0600); err != nil {
		t.Fatal(err)
	}
	assertCheck(false, "")
	if err := os.WriteFile(path, []byte(`{"command":"secret callback command",BROKEN}`), 0600); err != nil {
		t.Fatal(err)
	}
	assertCheck(false, "")
}
