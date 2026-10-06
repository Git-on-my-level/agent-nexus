package app

import (
	"agent-nexus-cli/internal/config"
	"strings"
	"testing"
)

func TestBoardRoleFlagBodies(t *testing.T) {
	a := &App{Stdin: strings.NewReader("")}
	body, dry, err := a.parseBoardCreateInput([]string{"--title", "Initiatives", "--role", "initiatives", "--dry-run"}, config.Resolved{}, "boards create")
	if err != nil || !dry {
		t.Fatalf("body=%v dry=%v err=%v", body, dry, err)
	}
	if body.(map[string]any)["board"].(map[string]any)["role"] != "initiatives" {
		t.Fatal(body)
	}
	for _, role := range []string{"initiatives", ""} {
		id, patch, dry, err := a.parseIDAndBodyInputWithOptions([]string{"board:initiatives", "--role", role, "--dry-run"}, "board-id", "board id", "boards patch", jsonBodyInputOptions{allowDryRun: true})
		if err != nil || id != "board:initiatives" || !dry || patch.(map[string]any)["patch"].(map[string]any)["role"] != role {
			t.Fatalf("id=%s body=%v dry=%v err=%v", id, patch, dry, err)
		}
	}
}
