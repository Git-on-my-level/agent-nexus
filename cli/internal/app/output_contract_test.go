package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func TestEnvelopeV2AndUnknownCommandRepair(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		args []string
		ok   bool
		code string
		exit int
		next string
	}{
		{[]string{"--json", "version"}, true, "", 0, ""},
		{[]string{"--json", "verison"}, false, "unknown_command", 2, "version"},
	} {
		var stdout, stderr bytes.Buffer
		a := New()
		a.Stdout = &stdout
		a.Stderr = &stderr
		if got := a.Run(tc.args); got != tc.exit {
			t.Fatalf("%v exit=%d stderr=%s", tc.args, got, stderr.String())
		}
		var doc map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		if doc["schema_version"] != float64(2) || doc["ok"] != tc.ok {
			t.Fatalf("%v: %#v", tc.args, doc)
		}
		if _, ok := doc["warnings"].([]any); !ok {
			t.Fatalf("warnings missing: %#v", doc)
		}
		if tc.ok {
			if _, ok := doc["result"].(map[string]any); !ok {
				t.Fatalf("result missing: %#v", doc)
			}
		} else {
			errorDoc := doc["error"].(map[string]any)
			if errorDoc["code"] != tc.code || errorDoc["exit_code"] != float64(tc.exit) {
				t.Fatalf("error: %#v", errorDoc)
			}
			actions := errorDoc["next_actions"].([]any)
			if len(actions) == 0 || actions[0].(map[string]any)["argv"].([]any)[1] != tc.next {
				t.Fatalf("repair: %#v", actions)
			}
		}
	}
}

func TestStateDerivedActionsAndActorReferences(t *testing.T) {
	t.Parallel()
	actions := deriveNextActions("cards create", []string{"cards", "create"}, map[string]any{"card": map[string]any{"ref": "card:launch"}})
	if len(actions) != 2 || strings.Join(actions[1].Argv, " ") != "anx cards assign card:launch --assignee-ref me" || !actions[1].Mutates {
		t.Fatalf("card actions: %#v", actions)
	}
	fromID := deriveNextActions("cards create", nil, map[string]any{"card": map[string]any{"id": "card-123"}})
	if len(fromID) != 2 || strings.Join(fromID[0].Argv, " ") != "anx cards get card:card-123" {
		t.Fatalf("card id actions: %#v", fromID)
	}
	page := deriveNextActions("work list", []string{"work", "list", "--owner", "actor:alice", "--cursor", "old"}, map[string]any{"next_cursor": "opaque+next"})
	if len(page) != 1 || strings.Join(page[0].Argv, " ") != "anx work list --owner actor:alice --cursor opaque+next" {
		t.Fatalf("page action: %#v", page)
	}
	proposal := deriveNextActions("docs revise", nil, map[string]any{"proposal_id": "draft-1"})
	if len(proposal) != 1 || strings.Join(proposal[0].Argv, " ") != "anx docs revise --apply --proposal-id draft-1" {
		t.Fatalf("proposal: %#v", proposal)
	}
	args, err := normalizeActorArgs([]string{"work", "list", "--owner", "me"}, config.Resolved{ActorID: "actor-alice"})
	if err != nil || args[3] != "actor:actor-alice" {
		t.Fatalf("me: %v %#v", err, args)
	}
	args, err = normalizeActorArgs([]string{"cards", "assign", "card:a", "--assignee-ref", "bob"}, config.Resolved{})
	if err != nil || args[4] != "actor:bob" {
		t.Fatalf("bare actor: %v %#v", err, args)
	}
	warnings, repairs := resultWarnings("work list", []string{"work", "list", "--owner", "actor:missing"}, map[string]any{"items": []any{}})
	if len(warnings) != 1 || warnings[0].Code != "empty_actor_filter" || len(repairs) != 2 || strings.Join(repairs[0].Argv, " ") != "anx work list" {
		t.Fatalf("actor filter warning: %#v %#v", warnings, repairs)
	}
	warnings, repairs = resultWarnings("debug events list", []string{"debug", "events", "list", "--actor-id=missing"}, map[string]any{"events": []any{}})
	if len(warnings) != 1 || len(repairs) != 2 || strings.Join(repairs[0].Argv, " ") != "anx debug events list" {
		t.Fatalf("diagnostic actor filter warning: %#v %#v", warnings, repairs)
	}
	notFound := deriveErrorActions("cards get", errnorm.Local("card_not_found", "missing"))
	if len(notFound) != 1 || strings.Join(notFound[0].Argv, " ") != "anx cards list" {
		t.Fatalf("not found repair: %#v", notFound)
	}
	docNotFound := deriveErrorActions("docs get", errnorm.WithDetails(errnorm.Local("not_found", "missing document"), map[string]any{"requested_ref": "doc:launch"}))
	if len(docNotFound) != 1 || strings.Join(docNotFound[0].Argv, " ") != "anx docs search doc:launch" {
		t.Fatalf("document search repair: %#v", docNotFound)
	}
}

func TestDebugRoutingAndMetadata(t *testing.T) {
	t.Parallel()
	var stdout, stderr bytes.Buffer
	a := New()
	a.Stdout = &stdout
	a.Stderr = &stderr
	if got := a.Run([]string{"--json", "debug", "meta", "commands"}); got != 0 {
		t.Fatalf("exit=%d stderr=%s", got, stderr.String())
	}
	var doc struct {
		Result struct {
			Commands []struct {
				CLIPath         string `json:"cli_path"`
				SideEffectClass string `json:"side_effect_class"`
			} `json:"commands"`
		} `json:"result"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Result.Commands) == 0 {
		t.Fatal("empty metadata")
	}
	for _, command := range doc.Result.Commands {
		if command.SideEffectClass == "" || strings.HasPrefix(command.CLIPath, "agents me") || strings.HasPrefix(command.CLIPath, "agent notifications") || strings.HasPrefix(command.CLIPath, "ops ") || command.CLIPath == "usage summary" {
			t.Fatalf("invalid command: %#v", command)
		}
	}
	stdout.Reset()
	stderr.Reset()
	if got := a.Run([]string{"--json", "meta", "commands"}); got != 2 {
		t.Fatalf("old path exit=%d", got)
	}
}

func TestOutdatedRepairAndExitCode(t *testing.T) {
	t.Parallel()
	var stdout bytes.Buffer
	a := New()
	a.Stdout = &stdout
	if got := a.renderError(resolveMachineCommandIdentity("work list"), true, errnorm.Local("cli_outdated", "upgrade required")); got != 7 {
		t.Fatalf("exit=%d", got)
	}
	var doc struct {
		Error struct {
			ExitCode    int `json:"exit_code"`
			NextActions []struct {
				Argv []string `json:"argv"`
			} `json:"next_actions"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Error.ExitCode != 7 || len(doc.Error.NextActions) != 1 || strings.Join(doc.Error.NextActions[0].Argv, " ") != "anx update" {
		t.Fatalf("repair: %#v", doc.Error)
	}
}

func TestUnknownDiagnosticSubcommandRepair(t *testing.T) {
	t.Parallel()
	actions := deriveErrorActions("debug events", errnorm.Usage("unknown_subcommand", `unknown events subcommand "lsit"`))
	if len(actions) != 1 || strings.Join(actions[0].Argv, " ") != "anx debug events list" {
		t.Fatalf("diagnostic repair: %#v", actions)
	}
}

func TestEnvelopeResultOmitsCredentialMaterial(t *testing.T) {
	t.Parallel()
	value := sanitizeEnvelopeResult(map[string]any{"profile": map[string]any{"username": "alice", "access_token": "secret", "refresh_token": "secret", "private_key_path": "/tmp/key"}}).(map[string]any)
	profile := value["profile"].(map[string]any)
	if len(profile) != 1 || profile["username"] != "alice" {
		t.Fatalf("unsafe result: %#v", value)
	}
}
