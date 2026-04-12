package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type Example struct {
	Title       string `json:"title"`
	Command     string `json:"command"`
	Description string `json:"description,omitempty"`
}

type CommandSpec struct {
	CommandID  string    `json:"command_id"`
	CLIPath    string    `json:"cli_path"`
	Group      string    `json:"group,omitempty"`
	Method     string    `json:"method"`
	Path       string    `json:"path"`
	PathParams []string  `json:"path_params,omitempty"`
	InputMode  string    `json:"input_mode,omitempty"`
	Stability  string    `json:"stability,omitempty"`
	Concepts   []string  `json:"concepts,omitempty"`
	Adjacent   []string  `json:"adjacent_commands,omitempty"`
	Examples   []Example `json:"examples,omitempty"`
}

var CommandRegistry = []CommandSpec{
	{
		CommandID: "actors.create",
		CLIPath:   "actors create",
		Group:     "actors",
		Method:    "POST",
		Path:      "/actors",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"actors", "auth"},
		Adjacent:  []string{"actors.list"},
	},
	{
		CommandID: "actors.list",
		CLIPath:   "actors list",
		Group:     "actors",
		Method:    "GET",
		Path:      "/actors",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"actors", "auth"},
		Adjacent:  []string{"actors.create"},
	},
	{
		CommandID: "agent.notifications.dismiss",
		CLIPath:   "agent notifications dismiss",
		Group:     "agent",
		Method:    "POST",
		Path:      "/agent-notifications/dismiss",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"agents", "notifications", "write"},
		Adjacent:  []string{"agent.notifications.list", "agent.notifications.read"},
	},
	{
		CommandID: "agent.notifications.list",
		CLIPath:   "agent notifications list",
		Group:     "agent",
		Method:    "GET",
		Path:      "/agent-notifications",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"agents", "notifications"},
		Adjacent:  []string{"agent.notifications.dismiss", "agent.notifications.read"},
	},
	{
		CommandID: "agent.notifications.read",
		CLIPath:   "agent notifications read",
		Group:     "agent",
		Method:    "POST",
		Path:      "/agent-notifications/read",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"agents", "notifications", "write"},
		Adjacent:  []string{"agent.notifications.dismiss", "agent.notifications.list"},
	},
	{
		CommandID: "agents.me.get",
		CLIPath:   "agents me",
		Group:     "agents",
		Method:    "GET",
		Path:      "/agents/me",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"auth", "agents"},
		Adjacent:  []string{"agents.me.keys.rotate", "agents.me.patch", "agents.me.revoke"},
	},
	{
		CommandID: "agents.me.keys.rotate",
		CLIPath:   "agents me keys rotate",
		Group:     "agents",
		Method:    "POST",
		Path:      "/agents/me/keys/rotate",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"auth", "agents"},
		Adjacent:  []string{"agents.me.get", "agents.me.patch", "agents.me.revoke"},
	},
	{
		CommandID: "agents.me.patch",
		CLIPath:   "agents me patch",
		Group:     "agents",
		Method:    "PATCH",
		Path:      "/agents/me",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"auth", "agents"},
		Adjacent:  []string{"agents.me.get", "agents.me.keys.rotate", "agents.me.revoke"},
	},
	{
		CommandID: "agents.me.revoke",
		CLIPath:   "agents me revoke",
		Group:     "agents",
		Method:    "POST",
		Path:      "/agents/me/revoke",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"auth", "agents"},
		Adjacent:  []string{"agents.me.get", "agents.me.keys.rotate", "agents.me.patch"},
	},
	{
		CommandID:  "artifacts.archive",
		CLIPath:    "artifacts archive",
		Group:      "artifacts",
		Method:     "POST",
		Path:       "/artifacts/{artifact_id}/archive",
		PathParams: []string{"artifact_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"artifacts", "write"},
		Adjacent:   []string{"artifacts.content", "artifacts.create", "artifacts.get", "artifacts.list", "artifacts.purge", "artifacts.restore", "artifacts.trash", "artifacts.unarchive"},
	},
	{
		CommandID:  "artifacts.content",
		CLIPath:    "artifacts content",
		Group:      "artifacts",
		Method:     "GET",
		Path:       "/artifacts/{artifact_id}/content",
		PathParams: []string{"artifact_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"artifacts"},
		Adjacent:   []string{"artifacts.archive", "artifacts.create", "artifacts.get", "artifacts.list", "artifacts.purge", "artifacts.restore", "artifacts.trash", "artifacts.unarchive"},
	},
	{
		CommandID: "artifacts.create",
		CLIPath:   "artifacts create",
		Group:     "artifacts",
		Method:    "POST",
		Path:      "/artifacts",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"artifacts", "write"},
		Adjacent:  []string{"artifacts.archive", "artifacts.content", "artifacts.get", "artifacts.list", "artifacts.purge", "artifacts.restore", "artifacts.trash", "artifacts.unarchive"},
	},
	{
		CommandID:  "artifacts.get",
		CLIPath:    "artifacts get",
		Group:      "artifacts",
		Method:     "GET",
		Path:       "/artifacts/{artifact_id}",
		PathParams: []string{"artifact_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"artifacts"},
		Adjacent:   []string{"artifacts.archive", "artifacts.content", "artifacts.create", "artifacts.list", "artifacts.purge", "artifacts.restore", "artifacts.trash", "artifacts.unarchive"},
	},
	{
		CommandID: "artifacts.list",
		CLIPath:   "artifacts list",
		Group:     "artifacts",
		Method:    "GET",
		Path:      "/artifacts",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"artifacts"},
		Adjacent:  []string{"artifacts.archive", "artifacts.content", "artifacts.create", "artifacts.get", "artifacts.purge", "artifacts.restore", "artifacts.trash", "artifacts.unarchive"},
	},
	{
		CommandID:  "artifacts.purge",
		CLIPath:    "artifacts purge",
		Group:      "artifacts",
		Method:     "POST",
		Path:       "/artifacts/{artifact_id}/purge",
		PathParams: []string{"artifact_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"artifacts", "write"},
		Adjacent:   []string{"artifacts.archive", "artifacts.content", "artifacts.create", "artifacts.get", "artifacts.list", "artifacts.restore", "artifacts.trash", "artifacts.unarchive"},
	},
	{
		CommandID:  "artifacts.restore",
		CLIPath:    "artifacts restore",
		Group:      "artifacts",
		Method:     "POST",
		Path:       "/artifacts/{artifact_id}/restore",
		PathParams: []string{"artifact_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"artifacts", "write"},
		Adjacent:   []string{"artifacts.archive", "artifacts.content", "artifacts.create", "artifacts.get", "artifacts.list", "artifacts.purge", "artifacts.trash", "artifacts.unarchive"},
	},
	{
		CommandID:  "artifacts.trash",
		CLIPath:    "artifacts trash",
		Group:      "artifacts",
		Method:     "POST",
		Path:       "/artifacts/{artifact_id}/trash",
		PathParams: []string{"artifact_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"artifacts", "write"},
		Adjacent:   []string{"artifacts.archive", "artifacts.content", "artifacts.create", "artifacts.get", "artifacts.list", "artifacts.purge", "artifacts.restore", "artifacts.unarchive"},
	},
	{
		CommandID:  "artifacts.unarchive",
		CLIPath:    "artifacts unarchive",
		Group:      "artifacts",
		Method:     "POST",
		Path:       "/artifacts/{artifact_id}/unarchive",
		PathParams: []string{"artifact_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"artifacts", "write"},
		Adjacent:   []string{"artifacts.archive", "artifacts.content", "artifacts.create", "artifacts.get", "artifacts.list", "artifacts.purge", "artifacts.restore", "artifacts.trash"},
	},
	{
		CommandID: "auth.agents.register",
		CLIPath:   "auth agents register",
		Group:     "auth",
		Method:    "POST",
		Path:      "/auth/agents/register",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"auth", "agents"},
		Adjacent:  []string{"auth.audit.list", "auth.bootstrap.status", "auth.invites.create", "auth.invites.list", "auth.invites.revoke", "auth.passkey.dev.login", "auth.passkey.dev.register", "auth.passkey.login.options", "auth.passkey.login.verify", "auth.passkey.register.options", "auth.passkey.register.verify", "auth.principals.list", "auth.principals.revoke", "auth.token"},
	},
	{
		CommandID: "auth.audit.list",
		CLIPath:   "auth audit list",
		Group:     "auth",
		Method:    "GET",
		Path:      "/auth/audit",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"auth", "audit"},
		Adjacent:  []string{"auth.agents.register", "auth.bootstrap.status", "auth.invites.create", "auth.invites.list", "auth.invites.revoke", "auth.passkey.dev.login", "auth.passkey.dev.register", "auth.passkey.login.options", "auth.passkey.login.verify", "auth.passkey.register.options", "auth.passkey.register.verify", "auth.principals.list", "auth.principals.revoke", "auth.token"},
	},
	{
		CommandID: "auth.bootstrap.status",
		CLIPath:   "auth bootstrap status",
		Group:     "auth",
		Method:    "GET",
		Path:      "/auth/bootstrap/status",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"auth"},
		Adjacent:  []string{"auth.agents.register", "auth.audit.list", "auth.invites.create", "auth.invites.list", "auth.invites.revoke", "auth.passkey.dev.login", "auth.passkey.dev.register", "auth.passkey.login.options", "auth.passkey.login.verify", "auth.passkey.register.options", "auth.passkey.register.verify", "auth.principals.list", "auth.principals.revoke", "auth.token"},
	},
	{
		CommandID: "auth.invites.create",
		CLIPath:   "auth invites create",
		Group:     "auth",
		Method:    "POST",
		Path:      "/auth/invites",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"auth"},
		Adjacent:  []string{"auth.agents.register", "auth.audit.list", "auth.bootstrap.status", "auth.invites.list", "auth.invites.revoke", "auth.passkey.dev.login", "auth.passkey.dev.register", "auth.passkey.login.options", "auth.passkey.login.verify", "auth.passkey.register.options", "auth.passkey.register.verify", "auth.principals.list", "auth.principals.revoke", "auth.token"},
	},
	{
		CommandID: "auth.invites.list",
		CLIPath:   "auth invites list",
		Group:     "auth",
		Method:    "GET",
		Path:      "/auth/invites",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"auth"},
		Adjacent:  []string{"auth.agents.register", "auth.audit.list", "auth.bootstrap.status", "auth.invites.create", "auth.invites.revoke", "auth.passkey.dev.login", "auth.passkey.dev.register", "auth.passkey.login.options", "auth.passkey.login.verify", "auth.passkey.register.options", "auth.passkey.register.verify", "auth.principals.list", "auth.principals.revoke", "auth.token"},
	},
	{
		CommandID:  "auth.invites.revoke",
		CLIPath:    "auth invites revoke",
		Group:      "auth",
		Method:     "POST",
		Path:       "/auth/invites/{invite_id}/revoke",
		PathParams: []string{"invite_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"auth"},
		Adjacent:   []string{"auth.agents.register", "auth.audit.list", "auth.bootstrap.status", "auth.invites.create", "auth.invites.list", "auth.passkey.dev.login", "auth.passkey.dev.register", "auth.passkey.login.options", "auth.passkey.login.verify", "auth.passkey.register.options", "auth.passkey.register.verify", "auth.principals.list", "auth.principals.revoke", "auth.token"},
	},
	{
		CommandID: "auth.passkey.dev.login",
		CLIPath:   "auth passkey dev login",
		Group:     "auth",
		Method:    "POST",
		Path:      "/auth/passkey/dev/login",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"auth", "passkeys"},
		Adjacent:  []string{"auth.agents.register", "auth.audit.list", "auth.bootstrap.status", "auth.invites.create", "auth.invites.list", "auth.invites.revoke", "auth.passkey.dev.register", "auth.passkey.login.options", "auth.passkey.login.verify", "auth.passkey.register.options", "auth.passkey.register.verify", "auth.principals.list", "auth.principals.revoke", "auth.token"},
	},
	{
		CommandID: "auth.passkey.dev.register",
		CLIPath:   "auth passkey dev register",
		Group:     "auth",
		Method:    "POST",
		Path:      "/auth/passkey/dev/register",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"auth", "passkeys"},
		Adjacent:  []string{"auth.agents.register", "auth.audit.list", "auth.bootstrap.status", "auth.invites.create", "auth.invites.list", "auth.invites.revoke", "auth.passkey.dev.login", "auth.passkey.login.options", "auth.passkey.login.verify", "auth.passkey.register.options", "auth.passkey.register.verify", "auth.principals.list", "auth.principals.revoke", "auth.token"},
	},
	{
		CommandID: "auth.passkey.login.options",
		CLIPath:   "auth passkey login options",
		Group:     "auth",
		Method:    "POST",
		Path:      "/auth/passkey/login/options",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"auth", "passkeys"},
		Adjacent:  []string{"auth.agents.register", "auth.audit.list", "auth.bootstrap.status", "auth.invites.create", "auth.invites.list", "auth.invites.revoke", "auth.passkey.dev.login", "auth.passkey.dev.register", "auth.passkey.login.verify", "auth.passkey.register.options", "auth.passkey.register.verify", "auth.principals.list", "auth.principals.revoke", "auth.token"},
	},
	{
		CommandID: "auth.passkey.login.verify",
		CLIPath:   "auth passkey login verify",
		Group:     "auth",
		Method:    "POST",
		Path:      "/auth/passkey/login/verify",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"auth", "passkeys"},
		Adjacent:  []string{"auth.agents.register", "auth.audit.list", "auth.bootstrap.status", "auth.invites.create", "auth.invites.list", "auth.invites.revoke", "auth.passkey.dev.login", "auth.passkey.dev.register", "auth.passkey.login.options", "auth.passkey.register.options", "auth.passkey.register.verify", "auth.principals.list", "auth.principals.revoke", "auth.token"},
	},
	{
		CommandID: "auth.passkey.register.options",
		CLIPath:   "auth passkey register options",
		Group:     "auth",
		Method:    "POST",
		Path:      "/auth/passkey/register/options",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"auth", "passkeys"},
		Adjacent:  []string{"auth.agents.register", "auth.audit.list", "auth.bootstrap.status", "auth.invites.create", "auth.invites.list", "auth.invites.revoke", "auth.passkey.dev.login", "auth.passkey.dev.register", "auth.passkey.login.options", "auth.passkey.login.verify", "auth.passkey.register.verify", "auth.principals.list", "auth.principals.revoke", "auth.token"},
	},
	{
		CommandID: "auth.passkey.register.verify",
		CLIPath:   "auth passkey register verify",
		Group:     "auth",
		Method:    "POST",
		Path:      "/auth/passkey/register/verify",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"auth", "passkeys"},
		Adjacent:  []string{"auth.agents.register", "auth.audit.list", "auth.bootstrap.status", "auth.invites.create", "auth.invites.list", "auth.invites.revoke", "auth.passkey.dev.login", "auth.passkey.dev.register", "auth.passkey.login.options", "auth.passkey.login.verify", "auth.passkey.register.options", "auth.principals.list", "auth.principals.revoke", "auth.token"},
	},
	{
		CommandID: "auth.principals.list",
		CLIPath:   "auth principals list",
		Group:     "auth",
		Method:    "GET",
		Path:      "/auth/principals",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"auth"},
		Adjacent:  []string{"auth.agents.register", "auth.audit.list", "auth.bootstrap.status", "auth.invites.create", "auth.invites.list", "auth.invites.revoke", "auth.passkey.dev.login", "auth.passkey.dev.register", "auth.passkey.login.options", "auth.passkey.login.verify", "auth.passkey.register.options", "auth.passkey.register.verify", "auth.principals.revoke", "auth.token"},
	},
	{
		CommandID:  "auth.principals.revoke",
		CLIPath:    "auth principals revoke",
		Group:      "auth",
		Method:     "POST",
		Path:       "/auth/principals/{principal_id}/revoke",
		PathParams: []string{"principal_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"auth"},
		Adjacent:   []string{"auth.agents.register", "auth.audit.list", "auth.bootstrap.status", "auth.invites.create", "auth.invites.list", "auth.invites.revoke", "auth.passkey.dev.login", "auth.passkey.dev.register", "auth.passkey.login.options", "auth.passkey.login.verify", "auth.passkey.register.options", "auth.passkey.register.verify", "auth.principals.list", "auth.token"},
	},
	{
		CommandID: "auth.token",
		CLIPath:   "auth token",
		Group:     "auth",
		Method:    "POST",
		Path:      "/auth/token",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"auth"},
		Adjacent:  []string{"auth.agents.register", "auth.audit.list", "auth.bootstrap.status", "auth.invites.create", "auth.invites.list", "auth.invites.revoke", "auth.passkey.dev.login", "auth.passkey.dev.register", "auth.passkey.login.options", "auth.passkey.login.verify", "auth.passkey.register.options", "auth.passkey.register.verify", "auth.principals.list", "auth.principals.revoke"},
	},
	{
		CommandID:  "boards.archive",
		CLIPath:    "boards archive",
		Group:      "boards",
		Method:     "POST",
		Path:       "/boards/{board_id}/archive",
		PathParams: []string{"board_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"boards", "write"},
		Adjacent:   []string{"boards.cards.create", "boards.cards.batch_add", "boards.cards.get", "boards.cards.list", "boards.create", "boards.get", "boards.list", "boards.patch", "boards.purge", "boards.restore", "boards.trash", "boards.unarchive", "boards.workspace"},
	},
	{
		CommandID:  "boards.cards.batch_add",
		CLIPath:    "boards cards create-batch",
		Group:      "boards",
		Method:     "POST",
		Path:       "/boards/{board_id}/cards/batch",
		PathParams: []string{"board_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"boards", "cards", "write"},
		Adjacent:   []string{"boards.archive", "boards.cards.create", "boards.cards.get", "boards.cards.list", "boards.create", "boards.get", "boards.list", "boards.patch", "boards.purge", "boards.restore", "boards.trash", "boards.unarchive", "boards.workspace"},
	},
	{
		CommandID:  "boards.cards.create",
		CLIPath:    "boards cards create",
		Group:      "boards",
		Method:     "POST",
		Path:       "/boards/{board_id}/cards",
		PathParams: []string{"board_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"boards", "cards", "write"},
		Adjacent:   []string{"boards.archive", "boards.cards.batch_add", "boards.cards.get", "boards.cards.list", "boards.create", "boards.get", "boards.list", "boards.patch", "boards.purge", "boards.restore", "boards.trash", "boards.unarchive", "boards.workspace"},
	},
	{
		CommandID:  "boards.cards.get",
		CLIPath:    "boards cards get",
		Group:      "boards",
		Method:     "GET",
		Path:       "/boards/{board_id}/cards/{card_id}",
		PathParams: []string{"board_id", "card_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"boards", "cards"},
		Adjacent:   []string{"boards.archive", "boards.cards.create", "boards.cards.batch_add", "boards.cards.list", "boards.create", "boards.get", "boards.list", "boards.patch", "boards.purge", "boards.restore", "boards.trash", "boards.unarchive", "boards.workspace"},
	},
	{
		CommandID:  "boards.cards.list",
		CLIPath:    "boards cards list",
		Group:      "boards",
		Method:     "GET",
		Path:       "/boards/{board_id}/cards",
		PathParams: []string{"board_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"boards", "cards"},
		Adjacent:   []string{"boards.archive", "boards.cards.create", "boards.cards.batch_add", "boards.cards.get", "boards.create", "boards.get", "boards.list", "boards.patch", "boards.purge", "boards.restore", "boards.trash", "boards.unarchive", "boards.workspace"},
	},
	{
		CommandID: "boards.create",
		CLIPath:   "boards create",
		Group:     "boards",
		Method:    "POST",
		Path:      "/boards",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"boards", "write"},
		Adjacent:  []string{"boards.archive", "boards.cards.create", "boards.cards.batch_add", "boards.cards.get", "boards.cards.list", "boards.get", "boards.list", "boards.patch", "boards.purge", "boards.restore", "boards.trash", "boards.unarchive", "boards.workspace"},
	},
	{
		CommandID:  "boards.get",
		CLIPath:    "boards get",
		Group:      "boards",
		Method:     "GET",
		Path:       "/boards/{board_id}",
		PathParams: []string{"board_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"boards"},
		Adjacent:   []string{"boards.archive", "boards.cards.create", "boards.cards.batch_add", "boards.cards.get", "boards.cards.list", "boards.create", "boards.list", "boards.patch", "boards.purge", "boards.restore", "boards.trash", "boards.unarchive", "boards.workspace"},
	},
	{
		CommandID: "boards.list",
		CLIPath:   "boards list",
		Group:     "boards",
		Method:    "GET",
		Path:      "/boards",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"boards"},
		Adjacent:  []string{"boards.archive", "boards.cards.create", "boards.cards.batch_add", "boards.cards.get", "boards.cards.list", "boards.create", "boards.get", "boards.patch", "boards.purge", "boards.restore", "boards.trash", "boards.unarchive", "boards.workspace"},
	},
	{
		CommandID:  "boards.patch",
		CLIPath:    "boards patch",
		Group:      "boards",
		Method:     "PATCH",
		Path:       "/boards/{board_id}",
		PathParams: []string{"board_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"boards", "write", "concurrency"},
		Adjacent:   []string{"boards.archive", "boards.cards.create", "boards.cards.batch_add", "boards.cards.get", "boards.cards.list", "boards.create", "boards.get", "boards.list", "boards.purge", "boards.restore", "boards.trash", "boards.unarchive", "boards.workspace"},
	},
	{
		CommandID:  "boards.purge",
		CLIPath:    "boards purge",
		Group:      "boards",
		Method:     "POST",
		Path:       "/boards/{board_id}/purge",
		PathParams: []string{"board_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"boards", "write"},
		Adjacent:   []string{"boards.archive", "boards.cards.create", "boards.cards.batch_add", "boards.cards.get", "boards.cards.list", "boards.create", "boards.get", "boards.list", "boards.patch", "boards.restore", "boards.trash", "boards.unarchive", "boards.workspace"},
	},
	{
		CommandID:  "boards.restore",
		CLIPath:    "boards restore",
		Group:      "boards",
		Method:     "POST",
		Path:       "/boards/{board_id}/restore",
		PathParams: []string{"board_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"boards", "write"},
		Adjacent:   []string{"boards.archive", "boards.cards.create", "boards.cards.batch_add", "boards.cards.get", "boards.cards.list", "boards.create", "boards.get", "boards.list", "boards.patch", "boards.purge", "boards.trash", "boards.unarchive", "boards.workspace"},
	},
	{
		CommandID:  "boards.trash",
		CLIPath:    "boards trash",
		Group:      "boards",
		Method:     "POST",
		Path:       "/boards/{board_id}/trash",
		PathParams: []string{"board_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"boards", "write"},
		Adjacent:   []string{"boards.archive", "boards.cards.create", "boards.cards.batch_add", "boards.cards.get", "boards.cards.list", "boards.create", "boards.get", "boards.list", "boards.patch", "boards.purge", "boards.restore", "boards.unarchive", "boards.workspace"},
	},
	{
		CommandID:  "boards.unarchive",
		CLIPath:    "boards unarchive",
		Group:      "boards",
		Method:     "POST",
		Path:       "/boards/{board_id}/unarchive",
		PathParams: []string{"board_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"boards", "write"},
		Adjacent:   []string{"boards.archive", "boards.cards.create", "boards.cards.batch_add", "boards.cards.get", "boards.cards.list", "boards.create", "boards.get", "boards.list", "boards.patch", "boards.purge", "boards.restore", "boards.trash", "boards.workspace"},
	},
	{
		CommandID:  "boards.workspace",
		CLIPath:    "boards workspace",
		Group:      "boards",
		Method:     "GET",
		Path:       "/boards/{board_id}/workspace",
		PathParams: []string{"board_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"boards", "workspace"},
		Adjacent:   []string{"boards.archive", "boards.cards.create", "boards.cards.batch_add", "boards.cards.get", "boards.cards.list", "boards.create", "boards.get", "boards.list", "boards.patch", "boards.purge", "boards.restore", "boards.trash", "boards.unarchive"},
	},
	{
		CommandID:  "cards.archive",
		CLIPath:    "cards archive",
		Group:      "cards",
		Method:     "POST",
		Path:       "/cards/{card_id}/archive",
		PathParams: []string{"card_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"cards", "write"},
		Adjacent:   []string{"cards.create", "cards.get", "cards.list", "cards.move", "cards.patch", "cards.purge", "cards.restore", "cards.timeline", "cards.trash"},
	},
	{
		CommandID: "cards.create",
		CLIPath:   "cards create",
		Group:     "cards",
		Method:    "POST",
		Path:      "/cards",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"cards", "boards", "write"},
		Adjacent:  []string{"cards.archive", "cards.get", "cards.list", "cards.move", "cards.patch", "cards.purge", "cards.restore", "cards.timeline", "cards.trash"},
	},
	{
		CommandID:  "cards.get",
		CLIPath:    "cards get",
		Group:      "cards",
		Method:     "GET",
		Path:       "/cards/{card_id}",
		PathParams: []string{"card_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"cards"},
		Adjacent:   []string{"cards.archive", "cards.create", "cards.list", "cards.move", "cards.patch", "cards.purge", "cards.restore", "cards.timeline", "cards.trash"},
	},
	{
		CommandID: "cards.list",
		CLIPath:   "cards list",
		Group:     "cards",
		Method:    "GET",
		Path:      "/cards",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"cards"},
		Adjacent:  []string{"cards.archive", "cards.create", "cards.get", "cards.move", "cards.patch", "cards.purge", "cards.restore", "cards.timeline", "cards.trash"},
	},
	{
		CommandID:  "cards.move",
		CLIPath:    "cards move",
		Group:      "cards",
		Method:     "POST",
		Path:       "/cards/{card_id}/move",
		PathParams: []string{"card_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"cards", "boards", "write"},
		Adjacent:   []string{"cards.archive", "cards.create", "cards.get", "cards.list", "cards.patch", "cards.purge", "cards.restore", "cards.timeline", "cards.trash"},
	},
	{
		CommandID:  "cards.patch",
		CLIPath:    "cards patch",
		Group:      "cards",
		Method:     "PATCH",
		Path:       "/cards/{card_id}",
		PathParams: []string{"card_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"cards", "write", "concurrency"},
		Adjacent:   []string{"cards.archive", "cards.create", "cards.get", "cards.list", "cards.move", "cards.purge", "cards.restore", "cards.timeline", "cards.trash"},
	},
	{
		CommandID:  "cards.purge",
		CLIPath:    "cards purge",
		Group:      "cards",
		Method:     "POST",
		Path:       "/cards/{card_id}/purge",
		PathParams: []string{"card_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"cards", "write"},
		Adjacent:   []string{"cards.archive", "cards.create", "cards.get", "cards.list", "cards.move", "cards.patch", "cards.restore", "cards.timeline", "cards.trash"},
	},
	{
		CommandID:  "cards.restore",
		CLIPath:    "cards restore",
		Group:      "cards",
		Method:     "POST",
		Path:       "/cards/{card_id}/restore",
		PathParams: []string{"card_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"cards", "write"},
		Adjacent:   []string{"cards.archive", "cards.create", "cards.get", "cards.list", "cards.move", "cards.patch", "cards.purge", "cards.timeline", "cards.trash"},
	},
	{
		CommandID:  "cards.timeline",
		CLIPath:    "cards timeline",
		Group:      "cards",
		Method:     "GET",
		Path:       "/cards/{card_id}/timeline",
		PathParams: []string{"card_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"cards", "timeline"},
		Adjacent:   []string{"cards.archive", "cards.create", "cards.get", "cards.list", "cards.move", "cards.patch", "cards.purge", "cards.restore", "cards.trash"},
	},
	{
		CommandID:  "cards.trash",
		CLIPath:    "cards trash",
		Group:      "cards",
		Method:     "POST",
		Path:       "/cards/{card_id}/trash",
		PathParams: []string{"card_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"cards", "write"},
		Adjacent:   []string{"cards.archive", "cards.create", "cards.get", "cards.list", "cards.move", "cards.patch", "cards.purge", "cards.restore", "cards.timeline"},
	},
	{
		CommandID: "derived.rebuild",
		CLIPath:   "derived rebuild",
		Group:     "derived",
		Method:    "POST",
		Path:      "/derived/rebuild",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"projections", "maintenance"},
	},
	{
		CommandID:  "docs.archive",
		CLIPath:    "docs archive",
		Group:      "docs",
		Method:     "POST",
		Path:       "/docs/{document_id}/archive",
		PathParams: []string{"document_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"docs", "write"},
		Adjacent:   []string{"docs.create", "docs.get", "docs.list", "docs.purge", "docs.restore", "docs.revisions.create", "docs.revisions.get", "docs.revisions.list", "docs.trash", "docs.unarchive"},
	},
	{
		CommandID: "docs.create",
		CLIPath:   "docs create",
		Group:     "docs",
		Method:    "POST",
		Path:      "/docs",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"docs", "write"},
		Adjacent:  []string{"docs.archive", "docs.get", "docs.list", "docs.purge", "docs.restore", "docs.revisions.create", "docs.revisions.get", "docs.revisions.list", "docs.trash", "docs.unarchive"},
	},
	{
		CommandID:  "docs.get",
		CLIPath:    "docs get",
		Group:      "docs",
		Method:     "GET",
		Path:       "/docs/{document_id}",
		PathParams: []string{"document_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"docs"},
		Adjacent:   []string{"docs.archive", "docs.create", "docs.list", "docs.purge", "docs.restore", "docs.revisions.create", "docs.revisions.get", "docs.revisions.list", "docs.trash", "docs.unarchive"},
	},
	{
		CommandID: "docs.list",
		CLIPath:   "docs list",
		Group:     "docs",
		Method:    "GET",
		Path:      "/docs",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"docs"},
		Adjacent:  []string{"docs.archive", "docs.create", "docs.get", "docs.purge", "docs.restore", "docs.revisions.create", "docs.revisions.get", "docs.revisions.list", "docs.trash", "docs.unarchive"},
	},
	{
		CommandID:  "docs.purge",
		CLIPath:    "docs purge",
		Group:      "docs",
		Method:     "POST",
		Path:       "/docs/{document_id}/purge",
		PathParams: []string{"document_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"docs", "write"},
		Adjacent:   []string{"docs.archive", "docs.create", "docs.get", "docs.list", "docs.restore", "docs.revisions.create", "docs.revisions.get", "docs.revisions.list", "docs.trash", "docs.unarchive"},
	},
	{
		CommandID:  "docs.restore",
		CLIPath:    "docs restore",
		Group:      "docs",
		Method:     "POST",
		Path:       "/docs/{document_id}/restore",
		PathParams: []string{"document_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"docs", "write"},
		Adjacent:   []string{"docs.archive", "docs.create", "docs.get", "docs.list", "docs.purge", "docs.revisions.create", "docs.revisions.get", "docs.revisions.list", "docs.trash", "docs.unarchive"},
	},
	{
		CommandID:  "docs.revisions.create",
		CLIPath:    "docs revisions create",
		Group:      "docs",
		Method:     "POST",
		Path:       "/docs/{document_id}/revisions",
		PathParams: []string{"document_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"docs", "revisions", "write"},
		Adjacent:   []string{"docs.archive", "docs.create", "docs.get", "docs.list", "docs.purge", "docs.restore", "docs.revisions.get", "docs.revisions.list", "docs.trash", "docs.unarchive"},
	},
	{
		CommandID:  "docs.revisions.get",
		CLIPath:    "docs revisions get",
		Group:      "docs",
		Method:     "GET",
		Path:       "/docs/{document_id}/revisions/{revision_id}",
		PathParams: []string{"document_id", "revision_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"docs", "revisions"},
		Adjacent:   []string{"docs.archive", "docs.create", "docs.get", "docs.list", "docs.purge", "docs.restore", "docs.revisions.create", "docs.revisions.list", "docs.trash", "docs.unarchive"},
	},
	{
		CommandID:  "docs.revisions.list",
		CLIPath:    "docs revisions list",
		Group:      "docs",
		Method:     "GET",
		Path:       "/docs/{document_id}/revisions",
		PathParams: []string{"document_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"docs", "revisions"},
		Adjacent:   []string{"docs.archive", "docs.create", "docs.get", "docs.list", "docs.purge", "docs.restore", "docs.revisions.create", "docs.revisions.get", "docs.trash", "docs.unarchive"},
	},
	{
		CommandID:  "docs.trash",
		CLIPath:    "docs trash",
		Group:      "docs",
		Method:     "POST",
		Path:       "/docs/{document_id}/trash",
		PathParams: []string{"document_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"docs", "write"},
		Adjacent:   []string{"docs.archive", "docs.create", "docs.get", "docs.list", "docs.purge", "docs.restore", "docs.revisions.create", "docs.revisions.get", "docs.revisions.list", "docs.unarchive"},
	},
	{
		CommandID:  "docs.unarchive",
		CLIPath:    "docs unarchive",
		Group:      "docs",
		Method:     "POST",
		Path:       "/docs/{document_id}/unarchive",
		PathParams: []string{"document_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"docs", "write"},
		Adjacent:   []string{"docs.archive", "docs.create", "docs.get", "docs.list", "docs.purge", "docs.restore", "docs.revisions.create", "docs.revisions.get", "docs.revisions.list", "docs.trash"},
	},
	{
		CommandID:  "events.archive",
		CLIPath:    "events archive",
		Group:      "events",
		Method:     "POST",
		Path:       "/events/{event_id}/archive",
		PathParams: []string{"event_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"events", "write"},
		Adjacent:   []string{"events.create", "events.get", "events.list", "events.restore", "events.stream", "events.trash", "events.unarchive"},
	},
	{
		CommandID: "events.create",
		CLIPath:   "events create",
		Group:     "events",
		Method:    "POST",
		Path:      "/events",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"events", "write"},
		Adjacent:  []string{"events.archive", "events.get", "events.list", "events.restore", "events.stream", "events.trash", "events.unarchive"},
	},
	{
		CommandID:  "events.get",
		CLIPath:    "events get",
		Group:      "events",
		Method:     "GET",
		Path:       "/events/{event_id}",
		PathParams: []string{"event_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"events"},
		Adjacent:   []string{"events.archive", "events.create", "events.list", "events.restore", "events.stream", "events.trash", "events.unarchive"},
	},
	{
		CommandID: "events.list",
		CLIPath:   "events list",
		Group:     "events",
		Method:    "GET",
		Path:      "/events",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"events"},
		Adjacent:  []string{"events.archive", "events.create", "events.get", "events.restore", "events.stream", "events.trash", "events.unarchive"},
	},
	{
		CommandID:  "events.restore",
		CLIPath:    "events restore",
		Group:      "events",
		Method:     "POST",
		Path:       "/events/{event_id}/restore",
		PathParams: []string{"event_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"events", "write"},
		Adjacent:   []string{"events.archive", "events.create", "events.get", "events.list", "events.stream", "events.trash", "events.unarchive"},
	},
	{
		CommandID: "events.stream",
		CLIPath:   "events stream",
		Group:     "events",
		Method:    "GET",
		Path:      "/events/stream",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"events"},
		Adjacent:  []string{"events.archive", "events.create", "events.get", "events.list", "events.restore", "events.trash", "events.unarchive"},
	},
	{
		CommandID:  "events.trash",
		CLIPath:    "events trash",
		Group:      "events",
		Method:     "POST",
		Path:       "/events/{event_id}/trash",
		PathParams: []string{"event_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"events", "write"},
		Adjacent:   []string{"events.archive", "events.create", "events.get", "events.list", "events.restore", "events.stream", "events.unarchive"},
	},
	{
		CommandID:  "events.unarchive",
		CLIPath:    "events unarchive",
		Group:      "events",
		Method:     "POST",
		Path:       "/events/{event_id}/unarchive",
		PathParams: []string{"event_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"events", "write"},
		Adjacent:   []string{"events.archive", "events.create", "events.get", "events.list", "events.restore", "events.stream", "events.trash"},
	},
	{
		CommandID:  "inbox.acknowledge",
		CLIPath:    "inbox acknowledge",
		Group:      "inbox",
		Method:     "POST",
		Path:       "/inbox/{inbox_id}/acknowledge",
		PathParams: []string{"inbox_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"inbox", "write"},
		Adjacent:   []string{"inbox.get", "inbox.list", "inbox.stream"},
	},
	{
		CommandID:  "inbox.get",
		CLIPath:    "inbox get",
		Group:      "inbox",
		Method:     "GET",
		Path:       "/inbox/{inbox_id}",
		PathParams: []string{"inbox_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"inbox"},
		Adjacent:   []string{"inbox.acknowledge", "inbox.list", "inbox.stream"},
	},
	{
		CommandID: "inbox.list",
		CLIPath:   "inbox list",
		Group:     "inbox",
		Method:    "GET",
		Path:      "/inbox",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"inbox"},
		Adjacent:  []string{"inbox.acknowledge", "inbox.get", "inbox.stream"},
	},
	{
		CommandID: "inbox.stream",
		CLIPath:   "inbox stream",
		Group:     "inbox",
		Method:    "GET",
		Path:      "/inbox/stream",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"inbox"},
		Adjacent:  []string{"inbox.acknowledge", "inbox.get", "inbox.list"},
	},
	{
		CommandID:  "meta.commands.get",
		CLIPath:    "meta commands get",
		Group:      "meta",
		Method:     "GET",
		Path:       "/meta/commands/{command_id}",
		PathParams: []string{"command_id"},
		InputMode:  "none",
		Stability:  "stable",
		Concepts:   []string{"compatibility"},
		Adjacent:   []string{"meta.commands.list", "meta.concepts.get", "meta.concepts.list", "meta.handshake", "meta.health", "meta.livez", "meta.readyz", "meta.version"},
	},
	{
		CommandID: "meta.commands.list",
		CLIPath:   "meta commands list",
		Group:     "meta",
		Method:    "GET",
		Path:      "/meta/commands",
		InputMode: "none",
		Stability: "stable",
		Concepts:  []string{"compatibility"},
		Adjacent:  []string{"meta.commands.get", "meta.concepts.get", "meta.concepts.list", "meta.handshake", "meta.health", "meta.livez", "meta.readyz", "meta.version"},
	},
	{
		CommandID:  "meta.concepts.get",
		CLIPath:    "meta concepts get",
		Group:      "meta",
		Method:     "GET",
		Path:       "/meta/concepts/{concept_name}",
		PathParams: []string{"concept_name"},
		InputMode:  "none",
		Stability:  "stable",
		Concepts:   []string{"compatibility"},
		Adjacent:   []string{"meta.commands.get", "meta.commands.list", "meta.concepts.list", "meta.handshake", "meta.health", "meta.livez", "meta.readyz", "meta.version"},
	},
	{
		CommandID: "meta.concepts.list",
		CLIPath:   "meta concepts list",
		Group:     "meta",
		Method:    "GET",
		Path:      "/meta/concepts",
		InputMode: "none",
		Stability: "stable",
		Concepts:  []string{"compatibility"},
		Adjacent:  []string{"meta.commands.get", "meta.commands.list", "meta.concepts.get", "meta.handshake", "meta.health", "meta.livez", "meta.readyz", "meta.version"},
	},
	{
		CommandID: "meta.handshake",
		CLIPath:   "meta handshake",
		Group:     "meta",
		Method:    "GET",
		Path:      "/meta/handshake",
		InputMode: "none",
		Stability: "stable",
		Concepts:  []string{"compatibility"},
		Adjacent:  []string{"meta.commands.get", "meta.commands.list", "meta.concepts.get", "meta.concepts.list", "meta.health", "meta.livez", "meta.readyz", "meta.version"},
	},
	{
		CommandID: "meta.health",
		CLIPath:   "meta health",
		Group:     "meta",
		Method:    "GET",
		Path:      "/health",
		InputMode: "none",
		Stability: "stable",
		Concepts:  []string{"health"},
		Adjacent:  []string{"meta.commands.get", "meta.commands.list", "meta.concepts.get", "meta.concepts.list", "meta.handshake", "meta.livez", "meta.readyz", "meta.version"},
	},
	{
		CommandID: "meta.livez",
		CLIPath:   "meta livez",
		Group:     "meta",
		Method:    "GET",
		Path:      "/livez",
		InputMode: "none",
		Stability: "stable",
		Concepts:  []string{"health"},
		Adjacent:  []string{"meta.commands.get", "meta.commands.list", "meta.concepts.get", "meta.concepts.list", "meta.handshake", "meta.health", "meta.readyz", "meta.version"},
	},
	{
		CommandID: "meta.readyz",
		CLIPath:   "meta readyz",
		Group:     "meta",
		Method:    "GET",
		Path:      "/readyz",
		InputMode: "none",
		Stability: "stable",
		Concepts:  []string{"health", "readiness"},
		Adjacent:  []string{"meta.commands.get", "meta.commands.list", "meta.concepts.get", "meta.concepts.list", "meta.handshake", "meta.health", "meta.livez", "meta.version"},
	},
	{
		CommandID: "meta.version",
		CLIPath:   "meta version",
		Group:     "meta",
		Method:    "GET",
		Path:      "/version",
		InputMode: "none",
		Stability: "stable",
		Concepts:  []string{"compatibility"},
		Adjacent:  []string{"meta.commands.get", "meta.commands.list", "meta.concepts.get", "meta.concepts.list", "meta.handshake", "meta.health", "meta.livez", "meta.readyz"},
	},
	{
		CommandID: "ops.blob.usage.rebuild",
		CLIPath:   "ops blob usage rebuild",
		Group:     "ops",
		Method:    "POST",
		Path:      "/ops/blob-usage/rebuild",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"ops", "maintenance"},
		Adjacent:  []string{"ops.health", "ops.usage.summary"},
	},
	{
		CommandID: "ops.health",
		CLIPath:   "ops health",
		Group:     "ops",
		Method:    "GET",
		Path:      "/ops/health",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"health", "ops"},
		Adjacent:  []string{"ops.blob.usage.rebuild", "ops.usage.summary"},
	},
	{
		CommandID: "ops.usage.summary",
		CLIPath:   "ops usage summary",
		Group:     "ops",
		Method:    "GET",
		Path:      "/ops/usage-summary",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"ops", "quotas"},
		Adjacent:  []string{"ops.blob.usage.rebuild", "ops.health"},
	},
	{
		CommandID: "packets.receipts.create",
		CLIPath:   "packets receipts create",
		Group:     "packets",
		Method:    "POST",
		Path:      "/packets/receipts",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"packets", "evidence"},
		Adjacent:  []string{"packets.reviews.create"},
	},
	{
		CommandID: "packets.reviews.create",
		CLIPath:   "packets reviews create",
		Group:     "packets",
		Method:    "POST",
		Path:      "/packets/reviews",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"packets", "evidence"},
		Adjacent:  []string{"packets.receipts.create"},
	},
	{
		CommandID: "ref_edges.list",
		CLIPath:   "ref-edges list",
		Group:     "ref-edges",
		Method:    "GET",
		Path:      "/ref-edges",
		InputMode: "query",
		Stability: "beta",
		Concepts:  []string{"refs", "inspection"},
	},
	{
		CommandID:  "threads.context",
		CLIPath:    "threads context",
		Group:      "threads",
		Method:     "GET",
		Path:       "/threads/{thread_id}/context",
		PathParams: []string{"thread_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"threads", "inspection"},
		Adjacent:   []string{"threads.inspect", "threads.list", "threads.timeline", "threads.workspace"},
	},
	{
		CommandID:  "threads.inspect",
		CLIPath:    "threads inspect",
		Group:      "threads",
		Method:     "GET",
		Path:       "/threads/{thread_id}",
		PathParams: []string{"thread_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"threads", "inspection"},
		Adjacent:   []string{"threads.context", "threads.list", "threads.timeline", "threads.workspace"},
	},
	{
		CommandID: "threads.list",
		CLIPath:   "threads list",
		Group:     "threads",
		Method:    "GET",
		Path:      "/threads",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"threads", "inspection"},
		Adjacent:  []string{"threads.context", "threads.inspect", "threads.timeline", "threads.workspace"},
	},
	{
		CommandID:  "threads.timeline",
		CLIPath:    "threads timeline",
		Group:      "threads",
		Method:     "GET",
		Path:       "/threads/{thread_id}/timeline",
		PathParams: []string{"thread_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"threads", "timeline"},
		Adjacent:   []string{"threads.context", "threads.inspect", "threads.list", "threads.workspace"},
	},
	{
		CommandID:  "threads.workspace",
		CLIPath:    "threads workspace",
		Group:      "threads",
		Method:     "GET",
		Path:       "/threads/{thread_id}/workspace",
		PathParams: []string{"thread_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"threads", "workspace"},
		Adjacent:   []string{"threads.context", "threads.inspect", "threads.list", "threads.timeline"},
	},
	{
		CommandID:  "topics.archive",
		CLIPath:    "topics archive",
		Group:      "topics",
		Method:     "POST",
		Path:       "/topics/{topic_id}/archive",
		PathParams: []string{"topic_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"topics", "write"},
		Adjacent:   []string{"topics.create", "topics.get", "topics.list", "topics.patch", "topics.restore", "topics.timeline", "topics.trash", "topics.unarchive", "topics.workspace"},
	},
	{
		CommandID: "topics.create",
		CLIPath:   "topics create",
		Group:     "topics",
		Method:    "POST",
		Path:      "/topics",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"topics", "write"},
		Adjacent:  []string{"topics.archive", "topics.get", "topics.list", "topics.patch", "topics.restore", "topics.timeline", "topics.trash", "topics.unarchive", "topics.workspace"},
	},
	{
		CommandID:  "topics.get",
		CLIPath:    "topics get",
		Group:      "topics",
		Method:     "GET",
		Path:       "/topics/{topic_id}",
		PathParams: []string{"topic_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"topics"},
		Adjacent:   []string{"topics.archive", "topics.create", "topics.list", "topics.patch", "topics.restore", "topics.timeline", "topics.trash", "topics.unarchive", "topics.workspace"},
	},
	{
		CommandID: "topics.list",
		CLIPath:   "topics list",
		Group:     "topics",
		Method:    "GET",
		Path:      "/topics",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"topics"},
		Adjacent:  []string{"topics.archive", "topics.create", "topics.get", "topics.patch", "topics.restore", "topics.timeline", "topics.trash", "topics.unarchive", "topics.workspace"},
	},
	{
		CommandID:  "topics.patch",
		CLIPath:    "topics patch",
		Group:      "topics",
		Method:     "PATCH",
		Path:       "/topics/{topic_id}",
		PathParams: []string{"topic_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"topics", "write", "concurrency"},
		Adjacent:   []string{"topics.archive", "topics.create", "topics.get", "topics.list", "topics.restore", "topics.timeline", "topics.trash", "topics.unarchive", "topics.workspace"},
	},
	{
		CommandID:  "topics.restore",
		CLIPath:    "topics restore",
		Group:      "topics",
		Method:     "POST",
		Path:       "/topics/{topic_id}/restore",
		PathParams: []string{"topic_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"topics", "write"},
		Adjacent:   []string{"topics.archive", "topics.create", "topics.get", "topics.list", "topics.patch", "topics.timeline", "topics.trash", "topics.unarchive", "topics.workspace"},
	},
	{
		CommandID:  "topics.timeline",
		CLIPath:    "topics timeline",
		Group:      "topics",
		Method:     "GET",
		Path:       "/topics/{topic_id}/timeline",
		PathParams: []string{"topic_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"topics", "timeline"},
		Adjacent:   []string{"topics.archive", "topics.create", "topics.get", "topics.list", "topics.patch", "topics.restore", "topics.trash", "topics.unarchive", "topics.workspace"},
	},
	{
		CommandID:  "topics.trash",
		CLIPath:    "topics trash",
		Group:      "topics",
		Method:     "POST",
		Path:       "/topics/{topic_id}/trash",
		PathParams: []string{"topic_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"topics", "write"},
		Adjacent:   []string{"topics.archive", "topics.create", "topics.get", "topics.list", "topics.patch", "topics.restore", "topics.timeline", "topics.unarchive", "topics.workspace"},
	},
	{
		CommandID:  "topics.unarchive",
		CLIPath:    "topics unarchive",
		Group:      "topics",
		Method:     "POST",
		Path:       "/topics/{topic_id}/unarchive",
		PathParams: []string{"topic_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"topics", "write"},
		Adjacent:   []string{"topics.archive", "topics.create", "topics.get", "topics.list", "topics.patch", "topics.restore", "topics.timeline", "topics.trash", "topics.workspace"},
	},
	{
		CommandID:  "topics.workspace",
		CLIPath:    "topics workspace",
		Group:      "topics",
		Method:     "GET",
		Path:       "/topics/{topic_id}/workspace",
		PathParams: []string{"topic_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"topics", "workspace"},
		Adjacent:   []string{"topics.archive", "topics.create", "topics.get", "topics.list", "topics.patch", "topics.restore", "topics.timeline", "topics.trash", "topics.unarchive"},
	},
}

var commandIndex = func() map[string]CommandSpec {
	index := make(map[string]CommandSpec, len(CommandRegistry))
	for _, cmd := range CommandRegistry {
		index[cmd.CommandID] = cmd
	}
	return index
}()

type RequestOptions struct {
	Query   map[string][]string
	Headers map[string]string
	Body    any
}

type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

func New(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), HTTPClient: httpClient}
}

func (c *Client) Invoke(ctx context.Context, commandID string, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	if c == nil {
		return nil, nil, fmt.Errorf("client is nil")
	}
	if strings.TrimSpace(c.BaseURL) == "" {
		return nil, nil, fmt.Errorf("base url is required")
	}
	if c.HTTPClient == nil {
		return nil, nil, fmt.Errorf("http client is required")
	}
	cmd, ok := commandIndex[commandID]
	if !ok {
		return nil, nil, fmt.Errorf("unknown command id: %s", commandID)
	}
	path, err := renderPath(cmd.Path, pathParams)
	if err != nil {
		return nil, nil, err
	}
	urlString := c.BaseURL + path
	u, err := url.Parse(urlString)
	if err != nil {
		return nil, nil, fmt.Errorf("parse request url: %w", err)
	}
	if len(opts.Query) > 0 {
		q := u.Query()
		for key, values := range opts.Query {
			for _, value := range values {
				q.Add(key, value)
			}
		}
		u.RawQuery = q.Encode()
	}
	var body io.Reader
	if opts.Body != nil {
		encoded, err := json.Marshal(opts.Body)
		if err != nil {
			return nil, nil, fmt.Errorf("encode request body: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, cmd.Method, u.String(), body)
	if err != nil {
		return nil, nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if opts.Body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range opts.Headers {
		if strings.TrimSpace(key) == "" {
			continue
		}
		req.Header.Set(key, value)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("perform request: %w", err)
	}
	bodyBytes, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if readErr != nil {
		return resp, nil, fmt.Errorf("read response: %w", readErr)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return resp, bodyBytes, fmt.Errorf("request failed: status=%d body=%s", resp.StatusCode, string(bodyBytes))
	}
	return resp, bodyBytes, nil
}

func renderPath(template string, pathParams map[string]string) (string, error) {
	b := template
	for {
		start := strings.IndexByte(b, '{')
		if start < 0 {
			return b, nil
		}
		end := strings.IndexByte(b[start:], '}')
		if end < 0 {
			return "", fmt.Errorf("invalid path template: %s", template)
		}
		end += start
		name := b[start+1 : end]
		value, ok := pathParams[name]
		if !ok {
			return "", fmt.Errorf("missing path param %q", name)
		}
		b = b[:start] + url.PathEscape(value) + b[end+1:]
	}
}

func (c *Client) ActorsCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "actors.create", nil, opts)
}

func (c *Client) ActorsList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "actors.list", nil, opts)
}

func (c *Client) AgentNotificationsDismiss(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "agent.notifications.dismiss", nil, opts)
}

func (c *Client) AgentNotificationsList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "agent.notifications.list", nil, opts)
}

func (c *Client) AgentNotificationsRead(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "agent.notifications.read", nil, opts)
}

func (c *Client) AgentsMeGet(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "agents.me.get", nil, opts)
}

func (c *Client) AgentsMeKeysRotate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "agents.me.keys.rotate", nil, opts)
}

func (c *Client) AgentsMePatch(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "agents.me.patch", nil, opts)
}

func (c *Client) AgentsMeRevoke(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "agents.me.revoke", nil, opts)
}

func (c *Client) ArtifactsArchive(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "artifacts.archive", pathParams, opts)
}

func (c *Client) ArtifactsContent(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "artifacts.content", pathParams, opts)
}

func (c *Client) ArtifactsCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "artifacts.create", nil, opts)
}

func (c *Client) ArtifactsGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "artifacts.get", pathParams, opts)
}

func (c *Client) ArtifactsList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "artifacts.list", nil, opts)
}

func (c *Client) ArtifactsPurge(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "artifacts.purge", pathParams, opts)
}

func (c *Client) ArtifactsRestore(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "artifacts.restore", pathParams, opts)
}

func (c *Client) ArtifactsTrash(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "artifacts.trash", pathParams, opts)
}

func (c *Client) ArtifactsUnarchive(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "artifacts.unarchive", pathParams, opts)
}

func (c *Client) AuthAgentsRegister(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "auth.agents.register", nil, opts)
}

func (c *Client) AuthAuditList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "auth.audit.list", nil, opts)
}

func (c *Client) AuthBootstrapStatus(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "auth.bootstrap.status", nil, opts)
}

func (c *Client) AuthInvitesCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "auth.invites.create", nil, opts)
}

func (c *Client) AuthInvitesList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "auth.invites.list", nil, opts)
}

func (c *Client) AuthInvitesRevoke(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "auth.invites.revoke", pathParams, opts)
}

func (c *Client) AuthPasskeyDevLogin(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "auth.passkey.dev.login", nil, opts)
}

func (c *Client) AuthPasskeyDevRegister(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "auth.passkey.dev.register", nil, opts)
}

func (c *Client) AuthPasskeyLoginOptions(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "auth.passkey.login.options", nil, opts)
}

func (c *Client) AuthPasskeyLoginVerify(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "auth.passkey.login.verify", nil, opts)
}

func (c *Client) AuthPasskeyRegisterOptions(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "auth.passkey.register.options", nil, opts)
}

func (c *Client) AuthPasskeyRegisterVerify(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "auth.passkey.register.verify", nil, opts)
}

func (c *Client) AuthPrincipalsList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "auth.principals.list", nil, opts)
}

func (c *Client) AuthPrincipalsRevoke(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "auth.principals.revoke", pathParams, opts)
}

func (c *Client) AuthToken(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "auth.token", nil, opts)
}

func (c *Client) BoardsArchive(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.archive", pathParams, opts)
}

func (c *Client) BoardsCardsBatchAdd(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.cards.batch_add", pathParams, opts)
}

func (c *Client) BoardsCardsCreate(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.cards.create", pathParams, opts)
}

func (c *Client) BoardsCardsGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.cards.get", pathParams, opts)
}

func (c *Client) BoardsCardsList(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.cards.list", pathParams, opts)
}

func (c *Client) BoardsCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.create", nil, opts)
}

func (c *Client) BoardsGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.get", pathParams, opts)
}

func (c *Client) BoardsList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.list", nil, opts)
}

func (c *Client) BoardsPatch(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.patch", pathParams, opts)
}

func (c *Client) BoardsPurge(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.purge", pathParams, opts)
}

func (c *Client) BoardsRestore(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.restore", pathParams, opts)
}

func (c *Client) BoardsTrash(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.trash", pathParams, opts)
}

func (c *Client) BoardsUnarchive(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.unarchive", pathParams, opts)
}

func (c *Client) BoardsWorkspace(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.workspace", pathParams, opts)
}

func (c *Client) CardsArchive(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "cards.archive", pathParams, opts)
}

func (c *Client) CardsCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "cards.create", nil, opts)
}

func (c *Client) CardsGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "cards.get", pathParams, opts)
}

func (c *Client) CardsList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "cards.list", nil, opts)
}

func (c *Client) CardsMove(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "cards.move", pathParams, opts)
}

func (c *Client) CardsPatch(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "cards.patch", pathParams, opts)
}

func (c *Client) CardsPurge(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "cards.purge", pathParams, opts)
}

func (c *Client) CardsRestore(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "cards.restore", pathParams, opts)
}

func (c *Client) CardsTimeline(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "cards.timeline", pathParams, opts)
}

func (c *Client) CardsTrash(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "cards.trash", pathParams, opts)
}

func (c *Client) DerivedRebuild(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "derived.rebuild", nil, opts)
}

func (c *Client) DocsArchive(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "docs.archive", pathParams, opts)
}

func (c *Client) DocsCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "docs.create", nil, opts)
}

func (c *Client) DocsGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "docs.get", pathParams, opts)
}

func (c *Client) DocsList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "docs.list", nil, opts)
}

func (c *Client) DocsPurge(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "docs.purge", pathParams, opts)
}

func (c *Client) DocsRestore(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "docs.restore", pathParams, opts)
}

func (c *Client) DocsRevisionsCreate(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "docs.revisions.create", pathParams, opts)
}

func (c *Client) DocsRevisionsGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "docs.revisions.get", pathParams, opts)
}

func (c *Client) DocsRevisionsList(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "docs.revisions.list", pathParams, opts)
}

func (c *Client) DocsTrash(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "docs.trash", pathParams, opts)
}

func (c *Client) DocsUnarchive(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "docs.unarchive", pathParams, opts)
}

func (c *Client) EventsArchive(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "events.archive", pathParams, opts)
}

func (c *Client) EventsCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "events.create", nil, opts)
}

func (c *Client) EventsGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "events.get", pathParams, opts)
}

func (c *Client) EventsList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "events.list", nil, opts)
}

func (c *Client) EventsRestore(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "events.restore", pathParams, opts)
}

func (c *Client) EventsStream(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "events.stream", nil, opts)
}

func (c *Client) EventsTrash(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "events.trash", pathParams, opts)
}

func (c *Client) EventsUnarchive(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "events.unarchive", pathParams, opts)
}

func (c *Client) InboxAcknowledge(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "inbox.acknowledge", pathParams, opts)
}

func (c *Client) InboxGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "inbox.get", pathParams, opts)
}

func (c *Client) InboxList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "inbox.list", nil, opts)
}

func (c *Client) InboxStream(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "inbox.stream", nil, opts)
}

func (c *Client) MetaCommandsGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "meta.commands.get", pathParams, opts)
}

func (c *Client) MetaCommandsList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "meta.commands.list", nil, opts)
}

func (c *Client) MetaConceptsGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "meta.concepts.get", pathParams, opts)
}

func (c *Client) MetaConceptsList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "meta.concepts.list", nil, opts)
}

func (c *Client) MetaHandshake(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "meta.handshake", nil, opts)
}

func (c *Client) MetaHealth(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "meta.health", nil, opts)
}

func (c *Client) MetaLivez(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "meta.livez", nil, opts)
}

func (c *Client) MetaReadyz(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "meta.readyz", nil, opts)
}

func (c *Client) MetaVersion(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "meta.version", nil, opts)
}

func (c *Client) OpsBlobUsageRebuild(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "ops.blob.usage.rebuild", nil, opts)
}

func (c *Client) OpsHealth(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "ops.health", nil, opts)
}

func (c *Client) OpsUsageSummary(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "ops.usage.summary", nil, opts)
}

func (c *Client) PacketsReceiptsCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "packets.receipts.create", nil, opts)
}

func (c *Client) PacketsReviewsCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "packets.reviews.create", nil, opts)
}

func (c *Client) RefEdgesList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "ref_edges.list", nil, opts)
}

func (c *Client) ThreadsContext(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "threads.context", pathParams, opts)
}

func (c *Client) ThreadsInspect(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "threads.inspect", pathParams, opts)
}

func (c *Client) ThreadsList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "threads.list", nil, opts)
}

func (c *Client) ThreadsTimeline(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "threads.timeline", pathParams, opts)
}

func (c *Client) ThreadsWorkspace(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "threads.workspace", pathParams, opts)
}

func (c *Client) TopicsArchive(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "topics.archive", pathParams, opts)
}

func (c *Client) TopicsCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "topics.create", nil, opts)
}

func (c *Client) TopicsGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "topics.get", pathParams, opts)
}

func (c *Client) TopicsList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "topics.list", nil, opts)
}

func (c *Client) TopicsPatch(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "topics.patch", pathParams, opts)
}

func (c *Client) TopicsRestore(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "topics.restore", pathParams, opts)
}

func (c *Client) TopicsTimeline(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "topics.timeline", pathParams, opts)
}

func (c *Client) TopicsTrash(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "topics.trash", pathParams, opts)
}

func (c *Client) TopicsUnarchive(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "topics.unarchive", pathParams, opts)
}

func (c *Client) TopicsWorkspace(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "topics.workspace", pathParams, opts)
}
