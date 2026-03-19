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
		CommandID: "actors.list",
		CLIPath:   "actors list",
		Group:     "actors",
		Method:    "GET",
		Path:      "/actors",
		InputMode: "none",
		Stability: "stable",
		Concepts:  []string{"identity"},
		Adjacent:  []string{"actors.register"},
		Examples: []Example{
			{
				Title:   "List actors",
				Command: "oar actors list --json",
			},
			{
				Title:   "Search actors by name",
				Command: "oar actors list --q \"bot\" --json",
			},
			{
				Title:   "Paginated actor list",
				Command: "oar actors list --limit 50 --json",
			},
		},
	},
	{
		CommandID: "actors.register",
		CLIPath:   "actors register",
		Group:     "actors",
		Method:    "POST",
		Path:      "/actors",
		InputMode: "json-body",
		Stability: "stable",
		Concepts:  []string{"identity"},
		Adjacent:  []string{"actors.list"},
		Examples: []Example{
			{
				Title:   "Register actor",
				Command: "oar actors register --id bot-1 --display-name \"Bot 1\" --created-at 2026-03-04T10:00:00Z --json",
			},
		},
	},
	{
		CommandID: "agents.me.get",
		CLIPath:   "agents me get",
		Group:     "agents",
		Method:    "GET",
		Path:      "/agents/me",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"auth", "identity"},
		Adjacent:  []string{"agents.me.keys.rotate", "agents.me.patch", "agents.me.revoke"},
		Examples: []Example{
			{
				Title:   "Get current profile",
				Command: "oar agents me get --json",
			},
		},
	},
	{
		CommandID: "agents.me.keys.rotate",
		CLIPath:   "agents me keys rotate",
		Group:     "agents",
		Method:    "POST",
		Path:      "/agents/me/keys/rotate",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"auth", "key-management"},
		Adjacent:  []string{"agents.me.get", "agents.me.patch", "agents.me.revoke"},
		Examples: []Example{
			{
				Title:   "Rotate key",
				Command: "oar agents me keys rotate --public-key <base64-ed25519-pubkey> --json",
			},
		},
	},
	{
		CommandID: "agents.me.patch",
		CLIPath:   "agents me patch",
		Group:     "agents",
		Method:    "PATCH",
		Path:      "/agents/me",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"auth", "identity"},
		Adjacent:  []string{"agents.me.get", "agents.me.keys.rotate", "agents.me.revoke"},
		Examples: []Example{
			{
				Title:   "Rename current agent",
				Command: "oar agents me patch --username renamed_agent --json",
			},
		},
	},
	{
		CommandID: "agents.me.revoke",
		CLIPath:   "agents me revoke",
		Group:     "agents",
		Method:    "POST",
		Path:      "/agents/me/revoke",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"auth", "revocation"},
		Adjacent:  []string{"agents.me.get", "agents.me.keys.rotate", "agents.me.patch"},
		Examples: []Example{
			{
				Title:   "Revoke self",
				Command: "oar agents me revoke --json",
			},
		},
	},
	{
		CommandID:  "artifacts.content.get",
		CLIPath:    "artifacts content get",
		Group:      "artifacts",
		Method:     "GET",
		Path:       "/artifacts/{artifact_id}/content",
		PathParams: []string{"artifact_id"},
		InputMode:  "none",
		Stability:  "stable",
		Concepts:   []string{"artifacts", "content"},
		Adjacent:   []string{"artifacts.create", "artifacts.get", "artifacts.list", "artifacts.tombstone"},
		Examples: []Example{
			{
				Title:   "Download content",
				Command: "oar artifacts content get --artifact-id artifact_123 > artifact.bin",
			},
		},
	},
	{
		CommandID: "artifacts.create",
		CLIPath:   "artifacts create",
		Group:     "artifacts",
		Method:    "POST",
		Path:      "/artifacts",
		InputMode: "file-and-body",
		Stability: "stable",
		Concepts:  []string{"artifacts", "evidence"},
		Adjacent:  []string{"artifacts.content.get", "artifacts.get", "artifacts.list", "artifacts.tombstone"},
		Examples: []Example{
			{
				Title:   "Create structured artifact",
				Command: "oar artifacts create --from-file artifact-create.json --json",
			},
		},
	},
	{
		CommandID:  "artifacts.get",
		CLIPath:    "artifacts get",
		Group:      "artifacts",
		Method:     "GET",
		Path:       "/artifacts/{artifact_id}",
		PathParams: []string{"artifact_id"},
		InputMode:  "none",
		Stability:  "stable",
		Concepts:   []string{"artifacts"},
		Adjacent:   []string{"artifacts.content.get", "artifacts.create", "artifacts.list", "artifacts.tombstone"},
		Examples: []Example{
			{
				Title:   "Get artifact",
				Command: "oar artifacts get --artifact-id artifact_123 --json",
			},
		},
	},
	{
		CommandID: "artifacts.list",
		CLIPath:   "artifacts list",
		Group:     "artifacts",
		Method:    "GET",
		Path:      "/artifacts",
		InputMode: "none",
		Stability: "stable",
		Concepts:  []string{"artifacts", "filtering"},
		Adjacent:  []string{"artifacts.content.get", "artifacts.create", "artifacts.get", "artifacts.tombstone"},
		Examples: []Example{
			{
				Title:   "List work orders for a thread",
				Command: "oar artifacts list --kind work_order --thread-id thread_123 --json",
			},
		},
	},
	{
		CommandID:  "artifacts.tombstone",
		CLIPath:    "artifacts tombstone",
		Group:      "artifacts",
		Method:     "POST",
		Path:       "/artifacts/{artifact_id}/tombstone",
		PathParams: []string{"artifact_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"artifacts", "lifecycle"},
		Adjacent:   []string{"artifacts.content.get", "artifacts.create", "artifacts.get", "artifacts.list"},
		Examples: []Example{
			{
				Title:   "Tombstone artifact",
				Command: "oar artifacts tombstone --artifact-id artifact_123 --reason \"superseded by newer version\" --json",
			},
		},
	},
	{
		CommandID: "auth.agents.register",
		CLIPath:   "auth register",
		Group:     "auth",
		Method:    "POST",
		Path:      "/auth/agents/register",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"auth", "identity"},
		Adjacent:  []string{"auth.bootstrap.status", "auth.invites.create", "auth.invites.list", "auth.invites.revoke", "auth.passkey.login.options", "auth.passkey.login.verify", "auth.passkey.register.options", "auth.passkey.register.verify", "auth.token"},
		Examples: []Example{
			{
				Title:   "Bootstrap first agent",
				Command: "oar auth register --username agent.one --bootstrap-token <token> --json",
			},
			{
				Title:   "Register invited agent",
				Command: "oar auth register --username agent.two --invite-token <token> --json",
			},
		},
	},
	{
		CommandID: "auth.bootstrap.status",
		CLIPath:   "auth bootstrap status",
		Group:     "auth",
		Method:    "GET",
		Path:      "/auth/bootstrap/status",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"auth", "onboarding"},
		Adjacent:  []string{"auth.invites.create", "auth.invites.list", "auth.invites.revoke", "auth.passkey.login.options", "auth.passkey.login.verify", "auth.passkey.register.options", "auth.passkey.register.verify", "auth.agents.register", "auth.token"},
		Examples: []Example{
			{
				Title:   "Read bootstrap status",
				Command: "oar auth bootstrap status --json",
			},
		},
	},
	{
		CommandID: "auth.invites.create",
		CLIPath:   "auth invites create",
		Group:     "auth",
		Method:    "POST",
		Path:      "/auth/invites",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"auth", "onboarding"},
		Adjacent:  []string{"auth.bootstrap.status", "auth.invites.list", "auth.invites.revoke", "auth.passkey.login.options", "auth.passkey.login.verify", "auth.passkey.register.options", "auth.passkey.register.verify", "auth.agents.register", "auth.token"},
		Examples: []Example{
			{
				Title:   "Create agent invite",
				Command: "oar auth invites create --kind agent --note 'ops bot' --json",
			},
		},
	},
	{
		CommandID: "auth.invites.list",
		CLIPath:   "auth invites list",
		Group:     "auth",
		Method:    "GET",
		Path:      "/auth/invites",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"auth", "onboarding"},
		Adjacent:  []string{"auth.bootstrap.status", "auth.invites.create", "auth.invites.revoke", "auth.passkey.login.options", "auth.passkey.login.verify", "auth.passkey.register.options", "auth.passkey.register.verify", "auth.agents.register", "auth.token"},
		Examples: []Example{
			{
				Title:   "List invites",
				Command: "oar auth invites list --json",
			},
		},
	},
	{
		CommandID:  "auth.invites.revoke",
		CLIPath:    "auth invites revoke",
		Group:      "auth",
		Method:     "POST",
		Path:       "/auth/invites/{invite_id}/revoke",
		PathParams: []string{"invite_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"auth", "onboarding"},
		Adjacent:   []string{"auth.bootstrap.status", "auth.invites.create", "auth.invites.list", "auth.passkey.login.options", "auth.passkey.login.verify", "auth.passkey.register.options", "auth.passkey.register.verify", "auth.agents.register", "auth.token"},
		Examples: []Example{
			{
				Title:   "Revoke invite",
				Command: "oar auth invites revoke --invite-id invite_123 --json",
			},
		},
	},
	{
		CommandID: "auth.passkey.login.options",
		CLIPath:   "auth passkey login options",
		Group:     "auth",
		Method:    "POST",
		Path:      "/auth/passkey/login/options",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"auth", "passkey"},
		Adjacent:  []string{"auth.bootstrap.status", "auth.invites.create", "auth.invites.list", "auth.invites.revoke", "auth.passkey.login.verify", "auth.passkey.register.options", "auth.passkey.register.verify", "auth.agents.register", "auth.token"},
	},
	{
		CommandID: "auth.passkey.login.verify",
		CLIPath:   "auth passkey login verify",
		Group:     "auth",
		Method:    "POST",
		Path:      "/auth/passkey/login/verify",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"auth", "passkey"},
		Adjacent:  []string{"auth.bootstrap.status", "auth.invites.create", "auth.invites.list", "auth.invites.revoke", "auth.passkey.login.options", "auth.passkey.register.options", "auth.passkey.register.verify", "auth.agents.register", "auth.token"},
	},
	{
		CommandID: "auth.passkey.register.options",
		CLIPath:   "auth passkey register options",
		Group:     "auth",
		Method:    "POST",
		Path:      "/auth/passkey/register/options",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"auth", "passkey"},
		Adjacent:  []string{"auth.bootstrap.status", "auth.invites.create", "auth.invites.list", "auth.invites.revoke", "auth.passkey.login.options", "auth.passkey.login.verify", "auth.passkey.register.verify", "auth.agents.register", "auth.token"},
	},
	{
		CommandID: "auth.passkey.register.verify",
		CLIPath:   "auth passkey register verify",
		Group:     "auth",
		Method:    "POST",
		Path:      "/auth/passkey/register/verify",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"auth", "passkey"},
		Adjacent:  []string{"auth.bootstrap.status", "auth.invites.create", "auth.invites.list", "auth.invites.revoke", "auth.passkey.login.options", "auth.passkey.login.verify", "auth.passkey.register.options", "auth.agents.register", "auth.token"},
	},
	{
		CommandID: "auth.token",
		CLIPath:   "auth token",
		Group:     "auth",
		Method:    "POST",
		Path:      "/auth/token",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"auth", "token-lifecycle"},
		Adjacent:  []string{"auth.bootstrap.status", "auth.invites.create", "auth.invites.list", "auth.invites.revoke", "auth.passkey.login.options", "auth.passkey.login.verify", "auth.passkey.register.options", "auth.passkey.register.verify", "auth.agents.register"},
		Examples: []Example{
			{
				Title:   "Refresh token grant",
				Command: "oar auth token --grant-type refresh_token --refresh-token <token> --json",
			},
			{
				Title:   "Assertion grant",
				Command: "oar auth token --grant-type assertion --agent-id <id> --key-id <id> --signed-at <rfc3339> --signature <base64> --json",
			},
		},
	},
	{
		CommandID:  "boards.cards.add",
		CLIPath:    "boards cards add",
		Group:      "boards",
		Method:     "POST",
		Path:       "/boards/{board_id}/cards",
		PathParams: []string{"board_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"boards", "planning", "ordering", "concurrency"},
		Adjacent:   []string{"boards.cards.list", "boards.cards.move", "boards.cards.remove", "boards.cards.update", "boards.create", "boards.get", "boards.list", "boards.update", "boards.workspace"},
		Examples: []Example{
			{
				Title:   "Add card to backlog",
				Command: "oar boards cards add --board-id board_product_launch --from-file board-card-add.json --json",
			},
		},
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
		Concepts:   []string{"boards", "planning", "ordering"},
		Adjacent:   []string{"boards.cards.add", "boards.cards.move", "boards.cards.remove", "boards.cards.update", "boards.create", "boards.get", "boards.list", "boards.update", "boards.workspace"},
		Examples: []Example{
			{
				Title:   "List board cards",
				Command: "oar boards cards list --board-id board_product_launch --json",
			},
		},
	},
	{
		CommandID:  "boards.cards.move",
		CLIPath:    "boards cards move",
		Group:      "boards",
		Method:     "POST",
		Path:       "/boards/{board_id}/cards/{thread_id}/move",
		PathParams: []string{"board_id", "thread_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"boards", "planning", "ordering", "concurrency"},
		Adjacent:   []string{"boards.cards.add", "boards.cards.list", "boards.cards.remove", "boards.cards.update", "boards.create", "boards.get", "boards.list", "boards.update", "boards.workspace"},
		Examples: []Example{
			{
				Title:   "Move card into review",
				Command: "oar boards cards move --board-id board_product_launch --thread-id thread_123 --from-file board-card-move.json --json",
			},
		},
	},
	{
		CommandID:  "boards.cards.remove",
		CLIPath:    "boards cards remove",
		Group:      "boards",
		Method:     "POST",
		Path:       "/boards/{board_id}/cards/{thread_id}/remove",
		PathParams: []string{"board_id", "thread_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"boards", "planning", "concurrency"},
		Adjacent:   []string{"boards.cards.add", "boards.cards.list", "boards.cards.move", "boards.cards.update", "boards.create", "boards.get", "boards.list", "boards.update", "boards.workspace"},
		Examples: []Example{
			{
				Title:   "Remove board card",
				Command: "oar boards cards remove --board-id board_product_launch --thread-id thread_123 --from-file board-card-remove.json --json",
			},
		},
	},
	{
		CommandID:  "boards.cards.update",
		CLIPath:    "boards cards update",
		Group:      "boards",
		Method:     "PATCH",
		Path:       "/boards/{board_id}/cards/{thread_id}",
		PathParams: []string{"board_id", "thread_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"boards", "planning", "docs", "concurrency"},
		Adjacent:   []string{"boards.cards.add", "boards.cards.list", "boards.cards.move", "boards.cards.remove", "boards.create", "boards.get", "boards.list", "boards.update", "boards.workspace"},
		Examples: []Example{
			{
				Title:   "Update pinned document",
				Command: "oar boards cards update --board-id board_product_launch --thread-id thread_123 --from-file board-card-update.json --json",
			},
		},
	},
	{
		CommandID: "boards.create",
		CLIPath:   "boards create",
		Group:     "boards",
		Method:    "POST",
		Path:      "/boards",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"boards", "planning", "concurrency"},
		Adjacent:  []string{"boards.cards.add", "boards.cards.list", "boards.cards.move", "boards.cards.remove", "boards.cards.update", "boards.get", "boards.list", "boards.update", "boards.workspace"},
		Examples: []Example{
			{
				Title:   "Create board",
				Command: "oar boards create --from-file board-create.json --json",
			},
		},
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
		Concepts:   []string{"boards", "planning"},
		Adjacent:   []string{"boards.cards.add", "boards.cards.list", "boards.cards.move", "boards.cards.remove", "boards.cards.update", "boards.create", "boards.list", "boards.update", "boards.workspace"},
		Examples: []Example{
			{
				Title:   "Get board",
				Command: "oar boards get --board-id board_product_launch --json",
			},
		},
	},
	{
		CommandID: "boards.list",
		CLIPath:   "boards list",
		Group:     "boards",
		Method:    "GET",
		Path:      "/boards",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"boards", "planning", "summaries"},
		Adjacent:  []string{"boards.cards.add", "boards.cards.list", "boards.cards.move", "boards.cards.remove", "boards.cards.update", "boards.create", "boards.get", "boards.update", "boards.workspace"},
		Examples: []Example{
			{
				Title:   "List boards",
				Command: "oar boards list --json",
			},
			{
				Title:   "List active boards for an owner",
				Command: "oar boards list --status active --owner actor_ceo --json",
			},
			{
				Title:   "Search boards by label",
				Command: "oar boards list --q \"launch\" --json",
			},
			{
				Title:   "Paginated board list",
				Command: "oar boards list --limit 30 --json",
			},
		},
	},
	{
		CommandID:  "boards.update",
		CLIPath:    "boards update",
		Group:      "boards",
		Method:     "PATCH",
		Path:       "/boards/{board_id}",
		PathParams: []string{"board_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"boards", "planning", "concurrency"},
		Adjacent:   []string{"boards.cards.add", "boards.cards.list", "boards.cards.move", "boards.cards.remove", "boards.cards.update", "boards.create", "boards.get", "boards.list", "boards.workspace"},
		Examples: []Example{
			{
				Title:   "Update board metadata",
				Command: "oar boards update --board-id board_product_launch --from-file board-update.json --json",
			},
		},
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
		Concepts:   []string{"boards", "planning", "threads", "docs", "commitments", "inbox"},
		Adjacent:   []string{"boards.cards.add", "boards.cards.list", "boards.cards.move", "boards.cards.remove", "boards.cards.update", "boards.create", "boards.get", "boards.list", "boards.update"},
		Examples: []Example{
			{
				Title:   "Board workspace",
				Command: "oar boards workspace --board-id board_product_launch --json",
			},
		},
	},
	{
		CommandID: "commitments.create",
		CLIPath:   "commitments create",
		Group:     "commitments",
		Method:    "POST",
		Path:      "/commitments",
		InputMode: "json-body",
		Stability: "stable",
		Concepts:  []string{"commitments"},
		Adjacent:  []string{"commitments.get", "commitments.list", "commitments.patch"},
		Examples: []Example{
			{
				Title:   "Create commitment",
				Command: "oar commitments create --from-file commitment.json --json",
			},
		},
	},
	{
		CommandID:  "commitments.get",
		CLIPath:    "commitments get",
		Group:      "commitments",
		Method:     "GET",
		Path:       "/commitments/{commitment_id}",
		PathParams: []string{"commitment_id"},
		InputMode:  "none",
		Stability:  "stable",
		Concepts:   []string{"commitments"},
		Adjacent:   []string{"commitments.create", "commitments.list", "commitments.patch"},
		Examples: []Example{
			{
				Title:   "Get commitment",
				Command: "oar commitments get --commitment-id commitment_123 --json",
			},
		},
	},
	{
		CommandID: "commitments.list",
		CLIPath:   "commitments list",
		Group:     "commitments",
		Method:    "GET",
		Path:      "/commitments",
		InputMode: "none",
		Stability: "stable",
		Concepts:  []string{"commitments", "filtering"},
		Adjacent:  []string{"commitments.create", "commitments.get", "commitments.patch"},
		Examples: []Example{
			{
				Title:   "List open commitments for a thread",
				Command: "oar commitments list --thread-id thread_123 --status open --json",
			},
		},
	},
	{
		CommandID:  "commitments.patch",
		CLIPath:    "commitments patch",
		Group:      "commitments",
		Method:     "PATCH",
		Path:       "/commitments/{commitment_id}",
		PathParams: []string{"commitment_id"},
		InputMode:  "json-body",
		Stability:  "stable",
		Concepts:   []string{"commitments", "patch", "provenance"},
		Adjacent:   []string{"commitments.create", "commitments.get", "commitments.list"},
		Examples: []Example{
			{
				Title:   "Mark commitment done",
				Command: "oar commitments patch --commitment-id commitment_123 --from-file commitment-patch.json --json",
			},
		},
	},
	{
		CommandID: "derived.rebuild",
		CLIPath:   "derived rebuild",
		Group:     "derived",
		Method:    "POST",
		Path:      "/derived/rebuild",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"derived-views", "maintenance"},
		Examples: []Example{
			{
				Title:   "Rebuild derived",
				Command: "oar derived rebuild --actor-id system --json",
			},
		},
	},
	{
		CommandID: "docs.create",
		CLIPath:   "docs create",
		Group:     "docs",
		Method:    "POST",
		Path:      "/docs",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"docs", "revisions"},
		Adjacent:  []string{"docs.get", "docs.history", "docs.list", "docs.revision.get", "docs.tombstone", "docs.update"},
		Examples: []Example{
			{
				Title:   "Create document",
				Command: "oar docs create --from-file doc-create.json --json",
			},
		},
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
		Concepts:   []string{"docs", "revisions"},
		Adjacent:   []string{"docs.create", "docs.history", "docs.list", "docs.revision.get", "docs.tombstone", "docs.update"},
		Examples: []Example{
			{
				Title:   "Get document head",
				Command: "oar docs get --document-id product-constitution --json",
			},
		},
	},
	{
		CommandID:  "docs.history",
		CLIPath:    "docs history",
		Group:      "docs",
		Method:     "GET",
		Path:       "/docs/{document_id}/history",
		PathParams: []string{"document_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"docs", "revisions", "lineage"},
		Adjacent:   []string{"docs.create", "docs.get", "docs.list", "docs.revision.get", "docs.tombstone", "docs.update"},
		Examples: []Example{
			{
				Title:   "List document history",
				Command: "oar docs history --document-id product-constitution --json",
			},
		},
	},
	{
		CommandID: "docs.list",
		CLIPath:   "docs list",
		Group:     "docs",
		Method:    "GET",
		Path:      "/docs",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"docs", "revisions"},
		Adjacent:  []string{"docs.create", "docs.get", "docs.history", "docs.revision.get", "docs.tombstone", "docs.update"},
		Examples: []Example{
			{
				Title:   "List documents",
				Command: "oar docs list --json",
			},
			{
				Title:   "Search documents by title",
				Command: "oar docs list --q \"constitution\" --json",
			},
			{
				Title:   "Paginated document list",
				Command: "oar docs list --limit 50 --json",
			},
		},
	},
	{
		CommandID:  "docs.revision.get",
		CLIPath:    "docs revision get",
		Group:      "docs",
		Method:     "GET",
		Path:       "/docs/{document_id}/revisions/{revision_id}",
		PathParams: []string{"document_id", "revision_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"docs", "revisions"},
		Adjacent:   []string{"docs.create", "docs.get", "docs.history", "docs.list", "docs.tombstone", "docs.update"},
		Examples: []Example{
			{
				Title:   "Get revision",
				Command: "oar docs revision get --document-id product-constitution --revision-id 019f... --json",
			},
		},
	},
	{
		CommandID:  "docs.tombstone",
		CLIPath:    "docs tombstone",
		Group:      "docs",
		Method:     "POST",
		Path:       "/docs/{document_id}/tombstone",
		PathParams: []string{"document_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"docs", "lifecycle"},
		Adjacent:   []string{"docs.create", "docs.get", "docs.history", "docs.list", "docs.revision.get", "docs.update"},
		Examples: []Example{
			{
				Title:   "Tombstone document",
				Command: "oar docs tombstone --document-id product-constitution --reason \"replaced by v2\" --json",
			},
		},
	},
	{
		CommandID:  "docs.update",
		CLIPath:    "docs update",
		Group:      "docs",
		Method:     "PATCH",
		Path:       "/docs/{document_id}",
		PathParams: []string{"document_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"docs", "revisions", "concurrency"},
		Adjacent:   []string{"docs.create", "docs.get", "docs.history", "docs.list", "docs.revision.get", "docs.tombstone"},
		Examples: []Example{
			{
				Title:   "Update document",
				Command: "oar docs update --document-id product-constitution --from-file doc-update.json --json",
			},
		},
	},
	{
		CommandID: "events.create",
		CLIPath:   "events create",
		Group:     "events",
		Method:    "POST",
		Path:      "/events",
		InputMode: "json-body",
		Stability: "stable",
		Concepts:  []string{"events", "append-only"},
		Adjacent:  []string{"events.get", "events.stream"},
		Examples: []Example{
			{
				Title:   "Append event",
				Command: "oar events create --from-file event.json --json",
			},
		},
	},
	{
		CommandID:  "events.get",
		CLIPath:    "events get",
		Group:      "events",
		Method:     "GET",
		Path:       "/events/{event_id}",
		PathParams: []string{"event_id"},
		InputMode:  "none",
		Stability:  "stable",
		Concepts:   []string{"events"},
		Adjacent:   []string{"events.create", "events.stream"},
		Examples: []Example{
			{
				Title:   "Get event",
				Command: "oar events get --event-id event_123 --json",
			},
		},
	},
	{
		CommandID: "events.stream",
		CLIPath:   "events stream",
		Group:     "events",
		Method:    "GET",
		Path:      "/events/stream",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"events", "streaming"},
		Adjacent:  []string{"events.create", "events.get"},
		Examples: []Example{
			{
				Title:   "Stream all events",
				Command: "oar events stream --json",
			},
			{
				Title:   "Resume by id",
				Command: "oar events stream --last-event-id <event_id> --json",
			},
		},
	},
	{
		CommandID: "inbox.ack",
		CLIPath:   "inbox ack",
		Group:     "inbox",
		Method:    "POST",
		Path:      "/inbox/ack",
		InputMode: "json-body",
		Stability: "stable",
		Concepts:  []string{"inbox", "events"},
		Adjacent:  []string{"inbox.get", "inbox.list", "inbox.stream"},
		Examples: []Example{
			{
				Title:   "Ack inbox item",
				Command: "oar inbox ack --thread-id thread_123 --inbox-item-id inbox:item-1 --json",
			},
			{
				Title:   "Ack inbox item by id",
				Command: "oar inbox ack inbox:decision_needed:thread_123:none:event_1 --json",
			},
		},
	},
	{
		CommandID:  "inbox.get",
		CLIPath:    "inbox get",
		Group:      "inbox",
		Method:     "GET",
		Path:       "/inbox/{inbox_item_id}",
		PathParams: []string{"inbox_item_id"},
		InputMode:  "none",
		Stability:  "stable",
		Concepts:   []string{"inbox", "derived-views"},
		Adjacent:   []string{"inbox.ack", "inbox.list", "inbox.stream"},
		Examples: []Example{
			{
				Title:   "Get inbox item by canonical id",
				Command: "oar inbox get --id inbox:decision_needed:thread_123:none:event_123 --json",
			},
			{
				Title:   "Get inbox item by alias",
				Command: "oar inbox get --id ibx_abcd1234ef56 --json",
			},
		},
	},
	{
		CommandID: "inbox.list",
		CLIPath:   "inbox list",
		Group:     "inbox",
		Method:    "GET",
		Path:      "/inbox",
		InputMode: "none",
		Stability: "stable",
		Concepts:  []string{"inbox", "derived-views"},
		Adjacent:  []string{"inbox.ack", "inbox.get", "inbox.stream"},
		Examples: []Example{
			{
				Title:   "List inbox",
				Command: "oar inbox list --json",
			},
		},
	},
	{
		CommandID: "inbox.stream",
		CLIPath:   "inbox stream",
		Group:     "inbox",
		Method:    "GET",
		Path:      "/inbox/stream",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"inbox", "derived-views", "streaming"},
		Adjacent:  []string{"inbox.ack", "inbox.get", "inbox.list"},
		Examples: []Example{
			{
				Title:   "Stream inbox updates",
				Command: "oar inbox stream --json",
			},
			{
				Title:   "Resume inbox stream",
				Command: "oar inbox stream --last-event-id <id> --json",
			},
		},
	},
	{
		CommandID:  "meta.commands.get",
		CLIPath:    "meta commands get",
		Group:      "meta",
		Method:     "GET",
		Path:       "/meta/commands/{command_id}",
		PathParams: []string{"command_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"meta", "introspection"},
		Adjacent:   []string{"meta.commands.list", "meta.concepts.get", "meta.concepts.list", "meta.handshake", "meta.health", "meta.version"},
		Examples: []Example{
			{
				Title:   "Read command metadata",
				Command: "oar meta commands get --command-id threads.list --json",
			},
		},
	},
	{
		CommandID: "meta.commands.list",
		CLIPath:   "meta commands list",
		Group:     "meta",
		Method:    "GET",
		Path:      "/meta/commands",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"meta", "introspection"},
		Adjacent:  []string{"meta.commands.get", "meta.concepts.get", "meta.concepts.list", "meta.handshake", "meta.health", "meta.version"},
		Examples: []Example{
			{
				Title:   "List command metadata",
				Command: "oar meta commands list --json",
			},
		},
	},
	{
		CommandID:  "meta.concepts.get",
		CLIPath:    "meta concepts get",
		Group:      "meta",
		Method:     "GET",
		Path:       "/meta/concepts/{concept_name}",
		PathParams: []string{"concept_name"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"meta", "concepts"},
		Adjacent:   []string{"meta.commands.get", "meta.commands.list", "meta.concepts.list", "meta.handshake", "meta.health", "meta.version"},
		Examples: []Example{
			{
				Title:   "Read one concept",
				Command: "oar meta concepts get --concept-name compatibility --json",
			},
		},
	},
	{
		CommandID: "meta.concepts.list",
		CLIPath:   "meta concepts list",
		Group:     "meta",
		Method:    "GET",
		Path:      "/meta/concepts",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"meta", "concepts"},
		Adjacent:  []string{"meta.commands.get", "meta.commands.list", "meta.concepts.get", "meta.handshake", "meta.health", "meta.version"},
		Examples: []Example{
			{
				Title:   "List concepts",
				Command: "oar meta concepts list --json",
			},
		},
	},
	{
		CommandID: "meta.handshake",
		CLIPath:   "meta handshake",
		Group:     "meta",
		Method:    "GET",
		Path:      "/meta/handshake",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"compatibility", "handshake"},
		Adjacent:  []string{"meta.commands.get", "meta.commands.list", "meta.concepts.get", "meta.concepts.list", "meta.health", "meta.version"},
		Examples: []Example{
			{
				Title:   "Read handshake metadata",
				Command: "oar meta handshake --json",
			},
		},
	},
	{
		CommandID: "meta.health",
		CLIPath:   "meta health",
		Group:     "meta",
		Method:    "GET",
		Path:      "/health",
		InputMode: "none",
		Stability: "stable",
		Concepts:  []string{"health", "readiness"},
		Adjacent:  []string{"meta.commands.get", "meta.commands.list", "meta.concepts.get", "meta.concepts.list", "meta.handshake", "meta.version"},
		Examples: []Example{
			{
				Title:   "Health check",
				Command: "oar meta health --json",
			},
		},
	},
	{
		CommandID: "meta.version",
		CLIPath:   "meta version",
		Group:     "meta",
		Method:    "GET",
		Path:      "/version",
		InputMode: "none",
		Stability: "stable",
		Concepts:  []string{"compatibility", "schema"},
		Adjacent:  []string{"meta.commands.get", "meta.commands.list", "meta.concepts.get", "meta.concepts.list", "meta.handshake", "meta.health"},
		Examples: []Example{
			{
				Title:   "Read version",
				Command: "oar meta version --json",
			},
		},
	},
	{
		CommandID: "packets.receipts.create",
		CLIPath:   "packets receipts create",
		Group:     "packets",
		Method:    "POST",
		Path:      "/receipts",
		InputMode: "json-body",
		Stability: "stable",
		Concepts:  []string{"packets", "receipts"},
		Adjacent:  []string{"packets.reviews.create", "packets.work-orders.create"},
		Examples: []Example{
			{
				Title:   "Create receipt",
				Command: "oar packets receipts create --from-file receipt.json --json",
			},
		},
	},
	{
		CommandID: "packets.reviews.create",
		CLIPath:   "packets reviews create",
		Group:     "packets",
		Method:    "POST",
		Path:      "/reviews",
		InputMode: "json-body",
		Stability: "stable",
		Concepts:  []string{"packets", "reviews"},
		Adjacent:  []string{"packets.receipts.create", "packets.work-orders.create"},
		Examples: []Example{
			{
				Title:   "Create review",
				Command: "oar packets reviews create --from-file review.json --json",
			},
		},
	},
	{
		CommandID: "packets.work-orders.create",
		CLIPath:   "packets work-orders create",
		Group:     "packets",
		Method:    "POST",
		Path:      "/work_orders",
		InputMode: "json-body",
		Stability: "stable",
		Concepts:  []string{"packets", "work-orders"},
		Adjacent:  []string{"packets.receipts.create", "packets.reviews.create"},
		Examples: []Example{
			{
				Title:   "Create work order",
				Command: "oar packets work-orders create --from-file work-order.json --json",
			},
		},
	},
	{
		CommandID:  "snapshots.get",
		CLIPath:    "snapshots get",
		Group:      "snapshots",
		Method:     "GET",
		Path:       "/snapshots/{snapshot_id}",
		PathParams: []string{"snapshot_id"},
		InputMode:  "none",
		Stability:  "stable",
		Concepts:   []string{"snapshots"},
		Examples: []Example{
			{
				Title:   "Get snapshot",
				Command: "oar snapshots get --snapshot-id snapshot_123 --json",
			},
		},
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
		Concepts:   []string{"threads", "events", "artifacts", "commitments", "docs"},
		Adjacent:   []string{"threads.create", "threads.get", "threads.list", "threads.patch", "threads.timeline", "threads.workspace"},
		Examples: []Example{
			{
				Title:   "Context with defaults",
				Command: "oar threads context --thread-id thread_123 --json",
			},
			{
				Title:   "Context with artifact previews",
				Command: "oar threads context --thread-id thread_123 --include-artifact-content --max-events 50 --json",
			},
		},
	},
	{
		CommandID: "threads.create",
		CLIPath:   "threads create",
		Group:     "threads",
		Method:    "POST",
		Path:      "/threads",
		InputMode: "json-body",
		Stability: "stable",
		Concepts:  []string{"threads", "snapshots"},
		Adjacent:  []string{"threads.context", "threads.get", "threads.list", "threads.patch", "threads.timeline", "threads.workspace"},
		Examples: []Example{
			{
				Title:   "Create thread",
				Command: "oar threads create --from-file thread.json --json",
			},
		},
	},
	{
		CommandID:  "threads.get",
		CLIPath:    "threads get",
		Group:      "threads",
		Method:     "GET",
		Path:       "/threads/{thread_id}",
		PathParams: []string{"thread_id"},
		InputMode:  "none",
		Stability:  "stable",
		Concepts:   []string{"threads"},
		Adjacent:   []string{"threads.context", "threads.create", "threads.list", "threads.patch", "threads.timeline", "threads.workspace"},
		Examples: []Example{
			{
				Title:   "Read thread",
				Command: "oar threads get --thread-id thread_123 --json",
			},
		},
	},
	{
		CommandID: "threads.list",
		CLIPath:   "threads list",
		Group:     "threads",
		Method:    "GET",
		Path:      "/threads",
		InputMode: "none",
		Stability: "stable",
		Concepts:  []string{"threads", "filtering"},
		Adjacent:  []string{"threads.context", "threads.create", "threads.get", "threads.patch", "threads.timeline", "threads.workspace"},
		Examples: []Example{
			{
				Title:   "List active p1 threads",
				Command: "oar threads list --status active --priority p1 --json",
			},
			{
				Title:   "Search threads by title",
				Command: "oar threads list --q \"launch\" --json",
			},
			{
				Title:   "Paginated thread list",
				Command: "oar threads list --limit 20 --json",
			},
		},
	},
	{
		CommandID:  "threads.patch",
		CLIPath:    "threads patch",
		Group:      "threads",
		Method:     "PATCH",
		Path:       "/threads/{thread_id}",
		PathParams: []string{"thread_id"},
		InputMode:  "json-body",
		Stability:  "stable",
		Concepts:   []string{"threads", "patch"},
		Adjacent:   []string{"threads.context", "threads.create", "threads.get", "threads.list", "threads.timeline", "threads.workspace"},
		Examples: []Example{
			{
				Title:   "Patch thread",
				Command: "oar threads patch --thread-id thread_123 --from-file patch.json --json",
			},
		},
	},
	{
		CommandID:  "threads.timeline",
		CLIPath:    "threads timeline",
		Group:      "threads",
		Method:     "GET",
		Path:       "/threads/{thread_id}/timeline",
		PathParams: []string{"thread_id"},
		InputMode:  "none",
		Stability:  "stable",
		Concepts:   []string{"threads", "events", "provenance"},
		Adjacent:   []string{"threads.context", "threads.create", "threads.get", "threads.list", "threads.patch", "threads.workspace"},
		Examples: []Example{
			{
				Title:   "Timeline",
				Command: "oar threads timeline --thread-id thread_123 --json",
			},
		},
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
		Concepts:   []string{"threads", "events", "artifacts", "commitments", "docs", "boards", "inbox"},
		Adjacent:   []string{"threads.context", "threads.create", "threads.get", "threads.list", "threads.patch", "threads.timeline"},
		Examples: []Example{
			{
				Title:   "Workspace with defaults",
				Command: "oar threads workspace --thread-id thread_123 --json",
			},
			{
				Title:   "Workspace with hydrated related review events",
				Command: "oar threads workspace --thread-id thread_123 --include-related-event-content --include-artifact-content --json",
			},
		},
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

func (c *Client) ActorsList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "actors.list", nil, opts)
}

func (c *Client) ActorsRegister(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "actors.register", nil, opts)
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

func (c *Client) ArtifactsContentGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "artifacts.content.get", pathParams, opts)
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

func (c *Client) ArtifactsTombstone(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "artifacts.tombstone", pathParams, opts)
}

func (c *Client) AuthAgentsRegister(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "auth.agents.register", nil, opts)
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

func (c *Client) AuthToken(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "auth.token", nil, opts)
}

func (c *Client) BoardsCardsAdd(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.cards.add", pathParams, opts)
}

func (c *Client) BoardsCardsList(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.cards.list", pathParams, opts)
}

func (c *Client) BoardsCardsMove(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.cards.move", pathParams, opts)
}

func (c *Client) BoardsCardsRemove(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.cards.remove", pathParams, opts)
}

func (c *Client) BoardsCardsUpdate(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.cards.update", pathParams, opts)
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

func (c *Client) BoardsUpdate(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.update", pathParams, opts)
}

func (c *Client) BoardsWorkspace(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "boards.workspace", pathParams, opts)
}

func (c *Client) CommitmentsCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "commitments.create", nil, opts)
}

func (c *Client) CommitmentsGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "commitments.get", pathParams, opts)
}

func (c *Client) CommitmentsList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "commitments.list", nil, opts)
}

func (c *Client) CommitmentsPatch(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "commitments.patch", pathParams, opts)
}

func (c *Client) DerivedRebuild(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "derived.rebuild", nil, opts)
}

func (c *Client) DocsCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "docs.create", nil, opts)
}

func (c *Client) DocsGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "docs.get", pathParams, opts)
}

func (c *Client) DocsHistory(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "docs.history", pathParams, opts)
}

func (c *Client) DocsList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "docs.list", nil, opts)
}

func (c *Client) DocsRevisionGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "docs.revision.get", pathParams, opts)
}

func (c *Client) DocsTombstone(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "docs.tombstone", pathParams, opts)
}

func (c *Client) DocsUpdate(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "docs.update", pathParams, opts)
}

func (c *Client) EventsCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "events.create", nil, opts)
}

func (c *Client) EventsGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "events.get", pathParams, opts)
}

func (c *Client) EventsStream(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "events.stream", nil, opts)
}

func (c *Client) InboxAck(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "inbox.ack", nil, opts)
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

func (c *Client) MetaVersion(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "meta.version", nil, opts)
}

func (c *Client) PacketsReceiptsCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "packets.receipts.create", nil, opts)
}

func (c *Client) PacketsReviewsCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "packets.reviews.create", nil, opts)
}

func (c *Client) PacketsWorkOrdersCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "packets.work-orders.create", nil, opts)
}

func (c *Client) SnapshotsGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "snapshots.get", pathParams, opts)
}

func (c *Client) ThreadsContext(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "threads.context", pathParams, opts)
}

func (c *Client) ThreadsCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "threads.create", nil, opts)
}

func (c *Client) ThreadsGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "threads.get", pathParams, opts)
}

func (c *Client) ThreadsList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "threads.list", nil, opts)
}

func (c *Client) ThreadsPatch(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "threads.patch", pathParams, opts)
}

func (c *Client) ThreadsTimeline(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "threads.timeline", pathParams, opts)
}

func (c *Client) ThreadsWorkspace(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "threads.workspace", pathParams, opts)
}
