//go:build integration && !windows

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	hostbridge "agent-nexus-cli/internal/bridge"
	"agent-nexus-cli/internal/integrations"
)

func TestAddressedHandoffAcrossEnrollmentsAndBridgeRestart(t *testing.T) {
	recipient := newPasskeyLiveCoreHarness(t)
	recipient.enrollHost(t, "receiver")
	sender := *recipient
	sender.homeDir = t.TempDir()
	grant := sender.postCore(t, "/auth/hosts/enrollment-tokens", sender.adminToken,
		map[string]any{"label": "Second test host", "expires_at": time.Now().Add(20 * time.Minute).UTC().Format(time.RFC3339)})
	sender.runCLIExpectOK(t, "sender", nil, "host", "enroll", "--name", "sending-host", "--token="+mustStringPath(t, grant, "token"))
	recipientID := recipient.actorID(t, "receiver")
	senderID := sender.actorID(t, "sender")
	if recipientID == senderID {
		t.Fatal("test requires two enrolled identities")
	}

	dir := t.TempDir()
	ref := "multica:issue:00000000-0000-4000-8000-000000000001"
	calls := filepath.Join(dir, "calls")
	script := filepath.Join(dir, "receive.sh")
	callback := fmt.Sprintf(`#!/bin/sh
set -eu
read -r intro
set -- $intro
id="$4"
cat >/dev/null
printf '%%s\n' "$id" >> %s
%s --json agent handoffs ack "$id" --outcome ref_blocked --blocked-ref %s
`, integrations.ShellQuote(calls), integrations.ShellQuote(recipient.cliBin), integrations.ShellQuote(ref))
	if err := os.WriteFile(script, []byte(callback), 0700); err != nil {
		t.Fatal(err)
	}
	runtimeFile := filepath.Join(dir, "runtime.json")
	runtimeJSON, err := json.Marshal(hostbridge.Runtime{Argv: []string{script}, CWD: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runtimeFile, runtimeJSON, 0600); err != nil {
		t.Fatal(err)
	}
	recipient.runCLIExpectOK(t, "receiver", nil, "bridge", "add", "--as", "receiver", "--runtime", runtimeFile, "--answers=false")
	startBridge := func(label string) func() {
		t.Helper()
		ctx, cancel := context.WithCancel(context.Background())
		log, err := os.Create(filepath.Join(dir, label+".log"))
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		cmd := exec.CommandContext(ctx, recipient.cliBin, "--json", "bridge", "run")
		cmd.Env = append(integrations.GitEnvironment(os.Environ()), "HOME="+recipient.homeDir, "ANX_CONFIG_DIR=", "ANX_AS=", "ANX_BASE_URL=", "ANX_ACCESS_TOKEN=", "ANX_UPDATE_POLICY=off", "ANX_SKILLS_AUTO_SYNC=0", "AGENTCTL_EXECUTION_ID=", "AGENTCTL_ADAPTER=")
		cmd.Stdout, cmd.Stderr = log, log
		if err := cmd.Start(); err != nil {
			cancel()
			log.Close()
			t.Fatal(err)
		}
		stopped := false
		stop := func() {
			if !stopped {
				stopped = true
				cancel()
				_ = cmd.Wait()
				log.Close()
			}
		}
		t.Cleanup(stop)
		return stop
	}
	firstStop := startBridge("before")
	statusPath := filepath.Join(hostbridge.Root(recipient.homeDir), "status.json")
	waitUntil := func(label string, condition func() bool) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for !condition() {
			if !time.Now().Before(deadline) {
				before, _ := os.ReadFile(filepath.Join(dir, "before.log"))
				after, _ := os.ReadFile(filepath.Join(dir, "after.log"))
				t.Fatalf("%s\nbefore: %s\nafter: %s", label, before, after)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	waitUntil("initial runtime was not ready", func() bool {
		raw, _ := os.ReadFile(statusPath)
		var status struct{ Ready map[string]bool }
		if json.Unmarshal(raw, &status) != nil {
			return false
		}
		for job, ready := range status.Ready {
			if ready && strings.HasSuffix(job, "\x00wake") {
				return true
			}
		}
		return false
	})
	firstStop()

	expiresAt := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	send := func() cliResult {
		return sender.runCLIExpectOK(t, "sender", nil, "agent", "handoffs", "send", "--to", recipientID, "--expires-at", expiresAt, "--ref", ref)
	}
	queued := send()
	id := mustStringPath(t, queued.Payload, "result.handoff_id")
	if mustStringPath(t, queued.Payload, "result.delivery_status") != "queued" ||
		mustStringPath(t, send().Payload, "result.handoff_id") != id {
		t.Fatal("offline handoff did not deduplicate")
	}
	startBridge("after")
	waitUntil("restarted runtime did not deliver a durable pickup", func() bool {
		read := sender.runCLIExpectOK(t, "sender", nil, "agent", "handoffs", "get", id)
		return mustStringPath(t, read.Payload, "result.delivery_status") == "bridge_delivered" &&
			mustStringPath(t, read.Payload, "result.pickup_status") == "acknowledged" &&
			mustStringPath(t, read.Payload, "result.blocked_ref") == ref
	})
	replay := send()
	if mustStringPath(t, replay.Payload, "result.handoff_id") != id ||
		mustStringPath(t, replay.Payload, "result.pickup_status") != "acknowledged" {
		t.Fatal("replay did not return the durable outcome")
	}
	raw, err := os.ReadFile(calls)
	if err != nil || string(raw) != id+"\n" {
		t.Fatalf("expected one callback: %q, %v", raw, err)
	}
}
