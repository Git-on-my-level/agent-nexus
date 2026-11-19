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
	recipient.registerHumanPasskey(t, "operator", "Operator", recipient.createHumanInviteToken(t))
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
	interrupt := filepath.Join(dir, "interrupt")
	script := filepath.Join(dir, "receive.sh")
	callback := fmt.Sprintf(`#!/bin/sh
set -eu
read -r intro
set -- $intro
id="$4"
cat >/dev/null
printf '%%s\n' "$id" >> %s
if [ -f %s ]; then exec sleep 5; fi
%s --json agent handoffs ack "$id" --outcome ref_blocked --blocked-ref %s
`, integrations.ShellQuote(calls), integrations.ShellQuote(interrupt), integrations.ShellQuote(recipient.cliBin), integrations.ShellQuote(ref))
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
	recipient.runCLIExpectOK(t, "receiver", nil, "bridge", "add", "--as", "receiver", "--runtime", runtimeFile)
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
				restarted, _ := os.ReadFile(filepath.Join(dir, "after-interruption.log"))
				t.Fatalf("%s\nbefore: %s\nafter: %s\nafter interruption: %s", label, before, after, restarted)
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
	stopAfter := startBridge("after")
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

	// Kill the bridge after a second callback starts, before it can acknowledge.
	// The next bridge must process an answer wake for the same recipient.
	if err := os.WriteFile(interrupt, nil, 0600); err != nil {
		t.Fatal(err)
	}
	expiresAt = time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	interrupted := mustStringPath(t, send().Payload, "result.handoff_id")
	wantCalls := id + "\n" + interrupted + "\n"
	waitUntil("handoff callback did not start", func() bool {
		raw, _ := os.ReadFile(calls)
		return string(raw) == wantCalls
	})
	stopAfter()
	if err := os.Remove(interrupt); err != nil {
		t.Fatal(err)
	}
	answerCalls := filepath.Join(dir, "answer-calls")
	answerScript := filepath.Join(dir, "answer.sh")
	if err := os.WriteFile(answerScript, []byte("#!/bin/sh\nset -eu\ncat >/dev/null\nprintf 'answered\\n' >> "+integrations.ShellQuote(answerCalls)+"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	work := recipient.runCLIExpectOK(t, "receiver", map[string]any{"title": "Unrelated answer", "source": map[string]any{"authority": "nexus"}}, "work", "create", "--from-file", "-")
	ask := recipient.runCLIExpectOK(t, "receiver", nil, "ask", "Proceed?", "--subject-ref", mustStringPath(t, work.Payload, "result.work.ref"),
		"--situation", "A launch decision is waiting.", "--impact", "The team needs a decision.", "--why", "This is the quickest safe approach.", "--recommend", "Proceed", "--on-answer", answerScript)
	startBridge("after-interruption")
	recipient.runCLIExpectOK(t, "operator", map[string]any{"inbox_item_id": findAskInboxID(t, recipient, mustStringPath(t, ask.Payload, "result.ask_id")), "response_text": "Proceed", "outcome": "answered"}, "debug", "inbox", "respond", "--from-file", "-")
	waitUntil("interrupted handoff blocked an unrelated answer wake", func() bool {
		raw, _ := os.ReadFile(answerCalls)
		return string(raw) == "answered\n"
	})
	raw, err = os.ReadFile(calls)
	if err != nil || string(raw) != wantCalls {
		t.Fatalf("interrupted handoff callback ran again: %q, %v", raw, err)
	}
	read := sender.runCLIExpectOK(t, "sender", nil, "agent", "handoffs", "get", interrupted)
	if mustStringPath(t, read.Payload, "result.delivery_status") != "claimed" ||
		mustStringPath(t, read.Payload, "result.pickup_status") != "pending" {
		t.Fatalf("interrupted callback was incorrectly settled: %s", read.Stdout)
	}

}
