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
		CommandID: "control.accounts.passkeys.register.finish",
		CLIPath:   "accounts passkeys register finish",
		Group:     "accounts",
		Method:    "POST",
		Path:      "/account/passkeys/registrations/finish",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"control-auth", "passkeys", "sessions"},
		Adjacent:  []string{"control.accounts.passkeys.register.start", "control.accounts.sessions.finish", "control.accounts.sessions.revoke-current", "control.accounts.sessions.start"},
		Examples: []Example{
			{
				Title:   "Finish account registration",
				Command: "oar api call --base-url https://control.oar.example --method POST --path /account/passkeys/registrations/finish --body @registration-finish.json",
			},
		},
	},
	{
		CommandID: "control.accounts.passkeys.register.start",
		CLIPath:   "accounts passkeys register start",
		Group:     "accounts",
		Method:    "POST",
		Path:      "/account/passkeys/registrations/start",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"control-auth", "passkeys", "accounts"},
		Adjacent:  []string{"control.accounts.passkeys.register.finish", "control.accounts.sessions.finish", "control.accounts.sessions.revoke-current", "control.accounts.sessions.start"},
		Examples: []Example{
			{
				Title:   "Start account registration",
				Command: "oar api call --base-url https://control.oar.example --method POST --path /account/passkeys/registrations/start --body '{\"email\":\"ops@example.com\",\"display_name\":\"Ops Lead\"}'",
			},
		},
	},
	{
		CommandID: "control.accounts.sessions.finish",
		CLIPath:   "accounts sessions finish",
		Group:     "accounts",
		Method:    "POST",
		Path:      "/account/sessions/finish",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"control-auth", "sessions", "passkeys"},
		Adjacent:  []string{"control.accounts.passkeys.register.finish", "control.accounts.passkeys.register.start", "control.accounts.sessions.revoke-current", "control.accounts.sessions.start"},
		Examples: []Example{
			{
				Title:   "Finish control-plane sign-in",
				Command: "oar api call --base-url https://control.oar.example --method POST --path /account/sessions/finish --body @session-finish.json",
			},
		},
	},
	{
		CommandID: "control.accounts.sessions.revoke-current",
		CLIPath:   "accounts sessions revoke-current",
		Group:     "accounts",
		Method:    "DELETE",
		Path:      "/account/sessions/current",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"control-auth", "sessions"},
		Adjacent:  []string{"control.accounts.passkeys.register.finish", "control.accounts.passkeys.register.start", "control.accounts.sessions.finish", "control.accounts.sessions.start"},
		Examples: []Example{
			{
				Title:   "Revoke current session",
				Command: "oar api call --base-url https://control.oar.example --method DELETE --path /account/sessions/current --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID: "control.accounts.sessions.start",
		CLIPath:   "accounts sessions start",
		Group:     "accounts",
		Method:    "POST",
		Path:      "/account/sessions/start",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"control-auth", "sessions", "passkeys"},
		Adjacent:  []string{"control.accounts.passkeys.register.finish", "control.accounts.passkeys.register.start", "control.accounts.sessions.finish", "control.accounts.sessions.revoke-current"},
		Examples: []Example{
			{
				Title:   "Start control-plane sign-in",
				Command: "oar api call --base-url https://control.oar.example --method POST --path /account/sessions/start --body '{\"email\":\"ops@example.com\"}'",
			},
		},
	},
	{
		CommandID: "control.billing.webhooks.stripe.receive",
		CLIPath:   "billing webhooks stripe receive",
		Group:     "billing",
		Method:    "POST",
		Path:      "/billing/webhooks/stripe",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"billing", "webhooks", "subscriptions"},
		Examples: []Example{
			{
				Title:   "Stripe webhook",
				Command: "curl -X POST https://control.oar.example/billing/webhooks/stripe -H 'Stripe-Signature: <signature>' -H 'Content-Type: application/json' --data-binary @event.json",
			},
		},
	},
	{
		CommandID:  "control.organizations.billing.checkout-session.create",
		CLIPath:    "organizations billing checkout-session create",
		Group:      "organizations",
		Method:     "POST",
		Path:       "/organizations/{organization_id}/billing/checkout-session",
		PathParams: []string{"organization_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"organizations", "billing", "checkout"},
		Adjacent:   []string{"control.organizations.billing.customer-portal-session.create", "control.organizations.billing.get", "control.organizations.create", "control.organizations.get", "control.organizations.invites.create", "control.organizations.invites.list", "control.organizations.invites.revoke", "control.organizations.list", "control.organizations.memberships.list", "control.organizations.memberships.update", "control.organizations.update", "control.organizations.usage-summary.get", "control.organizations.workspace-inventory.list"},
		Examples: []Example{
			{
				Title:   "Create checkout session",
				Command: "oar api call --base-url https://control.oar.example --method POST --path /organizations/org_123/billing/checkout-session --body '{\"plan_tier\":\"team\"}' --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID:  "control.organizations.billing.customer-portal-session.create",
		CLIPath:    "organizations billing customer-portal-session create",
		Group:      "organizations",
		Method:     "POST",
		Path:       "/organizations/{organization_id}/billing/customer-portal-session",
		PathParams: []string{"organization_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"organizations", "billing", "portal"},
		Adjacent:   []string{"control.organizations.billing.checkout-session.create", "control.organizations.billing.get", "control.organizations.create", "control.organizations.get", "control.organizations.invites.create", "control.organizations.invites.list", "control.organizations.invites.revoke", "control.organizations.list", "control.organizations.memberships.list", "control.organizations.memberships.update", "control.organizations.update", "control.organizations.usage-summary.get", "control.organizations.workspace-inventory.list"},
		Examples: []Example{
			{
				Title:   "Create customer portal session",
				Command: "oar api call --base-url https://control.oar.example --method POST --path /organizations/org_123/billing/customer-portal-session --body '{}' --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID:  "control.organizations.billing.get",
		CLIPath:    "organizations billing get",
		Group:      "organizations",
		Method:     "GET",
		Path:       "/organizations/{organization_id}/billing",
		PathParams: []string{"organization_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"organizations", "billing", "plans"},
		Adjacent:   []string{"control.organizations.billing.checkout-session.create", "control.organizations.billing.customer-portal-session.create", "control.organizations.create", "control.organizations.get", "control.organizations.invites.create", "control.organizations.invites.list", "control.organizations.invites.revoke", "control.organizations.list", "control.organizations.memberships.list", "control.organizations.memberships.update", "control.organizations.update", "control.organizations.usage-summary.get", "control.organizations.workspace-inventory.list"},
		Examples: []Example{
			{
				Title:   "Get billing summary",
				Command: "oar api call --base-url https://control.oar.example --method GET --path /organizations/org_123/billing --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID: "control.organizations.create",
		CLIPath:   "organizations create",
		Group:     "organizations",
		Method:    "POST",
		Path:      "/organizations",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"organizations", "tenancy", "billing"},
		Adjacent:  []string{"control.organizations.billing.checkout-session.create", "control.organizations.billing.customer-portal-session.create", "control.organizations.billing.get", "control.organizations.get", "control.organizations.invites.create", "control.organizations.invites.list", "control.organizations.invites.revoke", "control.organizations.list", "control.organizations.memberships.list", "control.organizations.memberships.update", "control.organizations.update", "control.organizations.usage-summary.get", "control.organizations.workspace-inventory.list"},
		Examples: []Example{
			{
				Title:   "Create organization",
				Command: "oar api call --base-url https://control.oar.example --method POST --path /organizations --body '{\"slug\":\"acme\",\"display_name\":\"Acme\",\"plan_tier\":\"team\"}' --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID:  "control.organizations.get",
		CLIPath:    "organizations get",
		Group:      "organizations",
		Method:     "GET",
		Path:       "/organizations/{organization_id}",
		PathParams: []string{"organization_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"organizations", "tenancy"},
		Adjacent:   []string{"control.organizations.billing.checkout-session.create", "control.organizations.billing.customer-portal-session.create", "control.organizations.billing.get", "control.organizations.create", "control.organizations.invites.create", "control.organizations.invites.list", "control.organizations.invites.revoke", "control.organizations.list", "control.organizations.memberships.list", "control.organizations.memberships.update", "control.organizations.update", "control.organizations.usage-summary.get", "control.organizations.workspace-inventory.list"},
		Examples: []Example{
			{
				Title:   "Get organization",
				Command: "oar api call --base-url https://control.oar.example --method GET --path /organizations/org_123 --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID:  "control.organizations.invites.create",
		CLIPath:    "organizations invites create",
		Group:      "organizations",
		Method:     "POST",
		Path:       "/organizations/{organization_id}/invites",
		PathParams: []string{"organization_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"organizations", "invites", "access"},
		Adjacent:   []string{"control.organizations.billing.checkout-session.create", "control.organizations.billing.customer-portal-session.create", "control.organizations.billing.get", "control.organizations.create", "control.organizations.get", "control.organizations.invites.list", "control.organizations.invites.revoke", "control.organizations.list", "control.organizations.memberships.list", "control.organizations.memberships.update", "control.organizations.update", "control.organizations.usage-summary.get", "control.organizations.workspace-inventory.list"},
		Examples: []Example{
			{
				Title:   "Invite organization admin",
				Command: "oar api call --base-url https://control.oar.example --method POST --path /organizations/org_123/invites --body '{\"email\":\"finance@example.com\",\"role\":\"admin\"}' --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID:  "control.organizations.invites.list",
		CLIPath:    "organizations invites list",
		Group:      "organizations",
		Method:     "GET",
		Path:       "/organizations/{organization_id}/invites",
		PathParams: []string{"organization_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"organizations", "invites", "access"},
		Adjacent:   []string{"control.organizations.billing.checkout-session.create", "control.organizations.billing.customer-portal-session.create", "control.organizations.billing.get", "control.organizations.create", "control.organizations.get", "control.organizations.invites.create", "control.organizations.invites.revoke", "control.organizations.list", "control.organizations.memberships.list", "control.organizations.memberships.update", "control.organizations.update", "control.organizations.usage-summary.get", "control.organizations.workspace-inventory.list"},
		Examples: []Example{
			{
				Title:   "List org invites",
				Command: "oar api call --base-url https://control.oar.example --method GET --path /organizations/org_123/invites --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID:  "control.organizations.invites.revoke",
		CLIPath:    "organizations invites revoke",
		Group:      "organizations",
		Method:     "POST",
		Path:       "/organizations/{organization_id}/invites/{invite_id}/revoke",
		PathParams: []string{"organization_id", "invite_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"organizations", "invites", "access"},
		Adjacent:   []string{"control.organizations.billing.checkout-session.create", "control.organizations.billing.customer-portal-session.create", "control.organizations.billing.get", "control.organizations.create", "control.organizations.get", "control.organizations.invites.create", "control.organizations.invites.list", "control.organizations.list", "control.organizations.memberships.list", "control.organizations.memberships.update", "control.organizations.update", "control.organizations.usage-summary.get", "control.organizations.workspace-inventory.list"},
		Examples: []Example{
			{
				Title:   "Revoke org invite",
				Command: "oar api call --base-url https://control.oar.example --method POST --path /organizations/org_123/invites/inv_123/revoke --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID: "control.organizations.list",
		CLIPath:   "organizations list",
		Group:     "organizations",
		Method:    "GET",
		Path:      "/organizations",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"organizations", "tenancy"},
		Adjacent:  []string{"control.organizations.billing.checkout-session.create", "control.organizations.billing.customer-portal-session.create", "control.organizations.billing.get", "control.organizations.create", "control.organizations.get", "control.organizations.invites.create", "control.organizations.invites.list", "control.organizations.invites.revoke", "control.organizations.memberships.list", "control.organizations.memberships.update", "control.organizations.update", "control.organizations.usage-summary.get", "control.organizations.workspace-inventory.list"},
		Examples: []Example{
			{
				Title:   "List organizations",
				Command: "oar api call --base-url https://control.oar.example --method GET --path /organizations --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID:  "control.organizations.memberships.list",
		CLIPath:    "organizations memberships list",
		Group:      "organizations",
		Method:     "GET",
		Path:       "/organizations/{organization_id}/memberships",
		PathParams: []string{"organization_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"organizations", "memberships", "access"},
		Adjacent:   []string{"control.organizations.billing.checkout-session.create", "control.organizations.billing.customer-portal-session.create", "control.organizations.billing.get", "control.organizations.create", "control.organizations.get", "control.organizations.invites.create", "control.organizations.invites.list", "control.organizations.invites.revoke", "control.organizations.list", "control.organizations.memberships.update", "control.organizations.update", "control.organizations.usage-summary.get", "control.organizations.workspace-inventory.list"},
		Examples: []Example{
			{
				Title:   "List memberships",
				Command: "oar api call --base-url https://control.oar.example --method GET --path /organizations/org_123/memberships --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID:  "control.organizations.memberships.update",
		CLIPath:    "organizations memberships update",
		Group:      "organizations",
		Method:     "PATCH",
		Path:       "/organizations/{organization_id}/memberships/{membership_id}",
		PathParams: []string{"organization_id", "membership_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"organizations", "memberships", "access"},
		Adjacent:   []string{"control.organizations.billing.checkout-session.create", "control.organizations.billing.customer-portal-session.create", "control.organizations.billing.get", "control.organizations.create", "control.organizations.get", "control.organizations.invites.create", "control.organizations.invites.list", "control.organizations.invites.revoke", "control.organizations.list", "control.organizations.memberships.list", "control.organizations.update", "control.organizations.usage-summary.get", "control.organizations.workspace-inventory.list"},
		Examples: []Example{
			{
				Title:   "Promote organization member",
				Command: "oar api call --base-url https://control.oar.example --method PATCH --path /organizations/org_123/memberships/mem_123 --body '{\"role\":\"owner\"}' --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID:  "control.organizations.update",
		CLIPath:    "organizations update",
		Group:      "organizations",
		Method:     "PATCH",
		Path:       "/organizations/{organization_id}",
		PathParams: []string{"organization_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"organizations", "billing", "lifecycle"},
		Adjacent:   []string{"control.organizations.billing.checkout-session.create", "control.organizations.billing.customer-portal-session.create", "control.organizations.billing.get", "control.organizations.create", "control.organizations.get", "control.organizations.invites.create", "control.organizations.invites.list", "control.organizations.invites.revoke", "control.organizations.list", "control.organizations.memberships.list", "control.organizations.memberships.update", "control.organizations.usage-summary.get", "control.organizations.workspace-inventory.list"},
		Examples: []Example{
			{
				Title:   "Update organization plan",
				Command: "oar api call --base-url https://control.oar.example --method PATCH --path /organizations/org_123 --body '{\"plan_tier\":\"scale\"}' --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID:  "control.organizations.usage-summary.get",
		CLIPath:    "organizations usage-summary get",
		Group:      "organizations",
		Method:     "GET",
		Path:       "/organizations/{organization_id}/usage-summary",
		PathParams: []string{"organization_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"usage", "plans", "quotas"},
		Adjacent:   []string{"control.organizations.billing.checkout-session.create", "control.organizations.billing.customer-portal-session.create", "control.organizations.billing.get", "control.organizations.create", "control.organizations.get", "control.organizations.invites.create", "control.organizations.invites.list", "control.organizations.invites.revoke", "control.organizations.list", "control.organizations.memberships.list", "control.organizations.memberships.update", "control.organizations.update", "control.organizations.workspace-inventory.list"},
		Examples: []Example{
			{
				Title:   "Get usage summary",
				Command: "oar api call --base-url https://control.oar.example --method GET --path /organizations/org_123/usage-summary --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID:  "control.organizations.workspace-inventory.list",
		CLIPath:    "organizations workspace-inventory list",
		Group:      "organizations",
		Method:     "GET",
		Path:       "/organizations/{organization_id}/workspace-inventory",
		PathParams: []string{"organization_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"workspaces", "inventory", "operations"},
		Adjacent:   []string{"control.organizations.billing.checkout-session.create", "control.organizations.billing.customer-portal-session.create", "control.organizations.billing.get", "control.organizations.create", "control.organizations.get", "control.organizations.invites.create", "control.organizations.invites.list", "control.organizations.invites.revoke", "control.organizations.list", "control.organizations.memberships.list", "control.organizations.memberships.update", "control.organizations.update", "control.organizations.usage-summary.get"},
		Examples: []Example{
			{
				Title:   "List workspace inventory",
				Command: "oar api call --base-url https://control.oar.example --method GET --path '/organizations/org_123/workspace-inventory' --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID:  "control.provisioning.jobs.get",
		CLIPath:    "provisioning jobs get",
		Group:      "provisioning",
		Method:     "GET",
		Path:       "/provisioning/jobs/{job_id}",
		PathParams: []string{"job_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"provisioning", "lifecycle", "workspaces"},
		Examples: []Example{
			{
				Title:   "Poll provisioning job",
				Command: "oar api call --base-url https://control.oar.example --method GET --path /provisioning/jobs/job_123 --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID:  "control.workspaces.backups.create",
		CLIPath:    "workspaces backups create",
		Group:      "workspaces",
		Method:     "POST",
		Path:       "/workspaces/{workspace_id}/backups",
		PathParams: []string{"workspace_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"workspaces", "backups", "provisioning"},
		Adjacent:   []string{"control.workspaces.create", "control.workspaces.decommission", "control.workspaces.get", "control.workspaces.heartbeat.record", "control.workspaces.launch-sessions.create", "control.workspaces.list", "control.workspaces.replace", "control.workspaces.restore", "control.workspaces.restore-drills.create", "control.workspaces.resume", "control.workspaces.routing-manifest.get", "control.workspaces.session-exchange.create", "control.workspaces.suspend", "control.workspaces.upgrade.create"},
		Examples: []Example{
			{
				Title:   "Run backup job",
				Command: "oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/backups --body '{\"schedule_name\":\"nightly\",\"retention_days\":30}' --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID: "control.workspaces.create",
		CLIPath:   "workspaces create",
		Group:     "workspaces",
		Method:    "POST",
		Path:      "/workspaces",
		InputMode: "json-body",
		Stability: "beta",
		Concepts:  []string{"workspaces", "provisioning", "registry"},
		Adjacent:  []string{"control.workspaces.backups.create", "control.workspaces.decommission", "control.workspaces.get", "control.workspaces.heartbeat.record", "control.workspaces.launch-sessions.create", "control.workspaces.list", "control.workspaces.replace", "control.workspaces.restore", "control.workspaces.restore-drills.create", "control.workspaces.resume", "control.workspaces.routing-manifest.get", "control.workspaces.session-exchange.create", "control.workspaces.suspend", "control.workspaces.upgrade.create"},
		Examples: []Example{
			{
				Title:   "Provision workspace",
				Command: "oar api call --base-url https://control.oar.example --method POST --path /workspaces --body '{\"organization_id\":\"org_123\",\"slug\":\"ops\",\"display_name\":\"Ops\",\"region\":\"us-central1\",\"workspace_tier\":\"standard\"}' --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID:  "control.workspaces.decommission",
		CLIPath:    "workspaces decommission",
		Group:      "workspaces",
		Method:     "POST",
		Path:       "/workspaces/{workspace_id}/decommission",
		PathParams: []string{"workspace_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"workspaces", "lifecycle", "routing"},
		Adjacent:   []string{"control.workspaces.backups.create", "control.workspaces.create", "control.workspaces.get", "control.workspaces.heartbeat.record", "control.workspaces.launch-sessions.create", "control.workspaces.list", "control.workspaces.replace", "control.workspaces.restore", "control.workspaces.restore-drills.create", "control.workspaces.resume", "control.workspaces.routing-manifest.get", "control.workspaces.session-exchange.create", "control.workspaces.suspend", "control.workspaces.upgrade.create"},
		Examples: []Example{
			{
				Title:   "Decommission workspace",
				Command: "oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/decommission --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID:  "control.workspaces.get",
		CLIPath:    "workspaces get",
		Group:      "workspaces",
		Method:     "GET",
		Path:       "/workspaces/{workspace_id}",
		PathParams: []string{"workspace_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"workspaces", "registry"},
		Adjacent:   []string{"control.workspaces.backups.create", "control.workspaces.create", "control.workspaces.decommission", "control.workspaces.heartbeat.record", "control.workspaces.launch-sessions.create", "control.workspaces.list", "control.workspaces.replace", "control.workspaces.restore", "control.workspaces.restore-drills.create", "control.workspaces.resume", "control.workspaces.routing-manifest.get", "control.workspaces.session-exchange.create", "control.workspaces.suspend", "control.workspaces.upgrade.create"},
		Examples: []Example{
			{
				Title:   "Read workspace",
				Command: "oar api call --base-url https://control.oar.example --method GET --path /workspaces/ws_123 --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID:  "control.workspaces.heartbeat.record",
		CLIPath:    "workspaces heartbeat record",
		Group:      "workspaces",
		Method:     "POST",
		Path:       "/workspaces/{workspace_id}/heartbeat",
		PathParams: []string{"workspace_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"workspaces", "heartbeat", "operations"},
		Adjacent:   []string{"control.workspaces.backups.create", "control.workspaces.create", "control.workspaces.decommission", "control.workspaces.get", "control.workspaces.launch-sessions.create", "control.workspaces.list", "control.workspaces.replace", "control.workspaces.restore", "control.workspaces.restore-drills.create", "control.workspaces.resume", "control.workspaces.routing-manifest.get", "control.workspaces.session-exchange.create", "control.workspaces.suspend", "control.workspaces.upgrade.create"},
		Examples: []Example{
			{
				Title:   "Record heartbeat",
				Command: "oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/heartbeat --header 'Authorization: Bearer <workspace-service-token>' --body @heartbeat.json",
			},
		},
	},
	{
		CommandID:  "control.workspaces.launch-sessions.create",
		CLIPath:    "workspaces launch-sessions create",
		Group:      "workspaces",
		Method:     "POST",
		Path:       "/workspaces/{workspace_id}/launch-sessions",
		PathParams: []string{"workspace_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"workspaces", "launch", "grants"},
		Adjacent:   []string{"control.workspaces.backups.create", "control.workspaces.create", "control.workspaces.decommission", "control.workspaces.get", "control.workspaces.heartbeat.record", "control.workspaces.list", "control.workspaces.replace", "control.workspaces.restore", "control.workspaces.restore-drills.create", "control.workspaces.resume", "control.workspaces.routing-manifest.get", "control.workspaces.session-exchange.create", "control.workspaces.suspend", "control.workspaces.upgrade.create"},
		Examples: []Example{
			{
				Title:   "Launch workspace UI",
				Command: "oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/launch-sessions --body '{\"return_path\":\"/ws/ops/threads\"}' --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID: "control.workspaces.list",
		CLIPath:   "workspaces list",
		Group:     "workspaces",
		Method:    "GET",
		Path:      "/workspaces",
		InputMode: "none",
		Stability: "beta",
		Concepts:  []string{"workspaces", "registry", "tenancy"},
		Adjacent:  []string{"control.workspaces.backups.create", "control.workspaces.create", "control.workspaces.decommission", "control.workspaces.get", "control.workspaces.heartbeat.record", "control.workspaces.launch-sessions.create", "control.workspaces.replace", "control.workspaces.restore", "control.workspaces.restore-drills.create", "control.workspaces.resume", "control.workspaces.routing-manifest.get", "control.workspaces.session-exchange.create", "control.workspaces.suspend", "control.workspaces.upgrade.create"},
		Examples: []Example{
			{
				Title:   "List workspaces for an organization",
				Command: "oar api call --base-url https://control.oar.example --method GET --path '/workspaces?organization_id=org_123' --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID:  "control.workspaces.replace",
		CLIPath:    "workspaces replace",
		Group:      "workspaces",
		Method:     "POST",
		Path:       "/workspaces/{workspace_id}/replace",
		PathParams: []string{"workspace_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"workspaces", "lifecycle", "restore"},
		Adjacent:   []string{"control.workspaces.backups.create", "control.workspaces.create", "control.workspaces.decommission", "control.workspaces.get", "control.workspaces.heartbeat.record", "control.workspaces.launch-sessions.create", "control.workspaces.list", "control.workspaces.restore", "control.workspaces.restore-drills.create", "control.workspaces.resume", "control.workspaces.routing-manifest.get", "control.workspaces.session-exchange.create", "control.workspaces.suspend", "control.workspaces.upgrade.create"},
		Examples: []Example{
			{
				Title:   "Replace workspace",
				Command: "oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/replace --body '{\"backup_dir\":\"/var/backups/ws_123-20260321T000000Z\"}' --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID:  "control.workspaces.restore",
		CLIPath:    "workspaces restore",
		Group:      "workspaces",
		Method:     "POST",
		Path:       "/workspaces/{workspace_id}/restore",
		PathParams: []string{"workspace_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"workspaces", "lifecycle", "restore"},
		Adjacent:   []string{"control.workspaces.backups.create", "control.workspaces.create", "control.workspaces.decommission", "control.workspaces.get", "control.workspaces.heartbeat.record", "control.workspaces.launch-sessions.create", "control.workspaces.list", "control.workspaces.replace", "control.workspaces.restore-drills.create", "control.workspaces.resume", "control.workspaces.routing-manifest.get", "control.workspaces.session-exchange.create", "control.workspaces.suspend", "control.workspaces.upgrade.create"},
		Examples: []Example{
			{
				Title:   "Restore workspace",
				Command: "oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/restore --body '{\"backup_dir\":\"/var/backups/ws_123-20260321T000000Z\"}' --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID:  "control.workspaces.restore-drills.create",
		CLIPath:    "workspaces restore-drills create",
		Group:      "workspaces",
		Method:     "POST",
		Path:       "/workspaces/{workspace_id}/restore-drills",
		PathParams: []string{"workspace_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"workspaces", "backups", "restore", "drills"},
		Adjacent:   []string{"control.workspaces.backups.create", "control.workspaces.create", "control.workspaces.decommission", "control.workspaces.get", "control.workspaces.heartbeat.record", "control.workspaces.launch-sessions.create", "control.workspaces.list", "control.workspaces.replace", "control.workspaces.restore", "control.workspaces.resume", "control.workspaces.routing-manifest.get", "control.workspaces.session-exchange.create", "control.workspaces.suspend", "control.workspaces.upgrade.create"},
		Examples: []Example{
			{
				Title:   "Run restore drill",
				Command: "oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/restore-drills --body '{\"backup_dir\":\"/var/backups/ws_123-20260321T000000Z\"}' --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID:  "control.workspaces.resume",
		CLIPath:    "workspaces resume",
		Group:      "workspaces",
		Method:     "POST",
		Path:       "/workspaces/{workspace_id}/resume",
		PathParams: []string{"workspace_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"workspaces", "lifecycle", "routing"},
		Adjacent:   []string{"control.workspaces.backups.create", "control.workspaces.create", "control.workspaces.decommission", "control.workspaces.get", "control.workspaces.heartbeat.record", "control.workspaces.launch-sessions.create", "control.workspaces.list", "control.workspaces.replace", "control.workspaces.restore", "control.workspaces.restore-drills.create", "control.workspaces.routing-manifest.get", "control.workspaces.session-exchange.create", "control.workspaces.suspend", "control.workspaces.upgrade.create"},
		Examples: []Example{
			{
				Title:   "Resume workspace",
				Command: "oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/resume --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID:  "control.workspaces.routing-manifest.get",
		CLIPath:    "workspaces routing-manifest get",
		Group:      "workspaces",
		Method:     "GET",
		Path:       "/workspaces/{workspace_id}/routing-manifest",
		PathParams: []string{"workspace_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"workspaces", "registry", "routing"},
		Adjacent:   []string{"control.workspaces.backups.create", "control.workspaces.create", "control.workspaces.decommission", "control.workspaces.get", "control.workspaces.heartbeat.record", "control.workspaces.launch-sessions.create", "control.workspaces.list", "control.workspaces.replace", "control.workspaces.restore", "control.workspaces.restore-drills.create", "control.workspaces.resume", "control.workspaces.session-exchange.create", "control.workspaces.suspend", "control.workspaces.upgrade.create"},
		Examples: []Example{
			{
				Title:   "Read routing manifest",
				Command: "oar api call --base-url https://control.oar.example --method GET --path /workspaces/ws_123/routing-manifest --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID:  "control.workspaces.session-exchange.create",
		CLIPath:    "workspaces session-exchange create",
		Group:      "workspaces",
		Method:     "POST",
		Path:       "/workspaces/{workspace_id}/session-exchange",
		PathParams: []string{"workspace_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"workspaces", "grants", "launch"},
		Adjacent:   []string{"control.workspaces.backups.create", "control.workspaces.create", "control.workspaces.decommission", "control.workspaces.get", "control.workspaces.heartbeat.record", "control.workspaces.launch-sessions.create", "control.workspaces.list", "control.workspaces.replace", "control.workspaces.restore", "control.workspaces.restore-drills.create", "control.workspaces.resume", "control.workspaces.routing-manifest.get", "control.workspaces.suspend", "control.workspaces.upgrade.create"},
		Examples: []Example{
			{
				Title:   "Exchange launch token",
				Command: "oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/session-exchange --body '{\"exchange_token\":\"<token>\"}'",
			},
		},
	},
	{
		CommandID:  "control.workspaces.suspend",
		CLIPath:    "workspaces suspend",
		Group:      "workspaces",
		Method:     "POST",
		Path:       "/workspaces/{workspace_id}/suspend",
		PathParams: []string{"workspace_id"},
		InputMode:  "none",
		Stability:  "beta",
		Concepts:   []string{"workspaces", "lifecycle", "routing"},
		Adjacent:   []string{"control.workspaces.backups.create", "control.workspaces.create", "control.workspaces.decommission", "control.workspaces.get", "control.workspaces.heartbeat.record", "control.workspaces.launch-sessions.create", "control.workspaces.list", "control.workspaces.replace", "control.workspaces.restore", "control.workspaces.restore-drills.create", "control.workspaces.resume", "control.workspaces.routing-manifest.get", "control.workspaces.session-exchange.create", "control.workspaces.upgrade.create"},
		Examples: []Example{
			{
				Title:   "Suspend workspace",
				Command: "oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/suspend --header 'Authorization: Bearer <control-session>'",
			},
		},
	},
	{
		CommandID:  "control.workspaces.upgrade.create",
		CLIPath:    "workspaces upgrade create",
		Group:      "workspaces",
		Method:     "POST",
		Path:       "/workspaces/{workspace_id}/upgrade",
		PathParams: []string{"workspace_id"},
		InputMode:  "json-body",
		Stability:  "beta",
		Concepts:   []string{"workspaces", "upgrades", "provisioning"},
		Adjacent:   []string{"control.workspaces.backups.create", "control.workspaces.create", "control.workspaces.decommission", "control.workspaces.get", "control.workspaces.heartbeat.record", "control.workspaces.launch-sessions.create", "control.workspaces.list", "control.workspaces.replace", "control.workspaces.restore", "control.workspaces.restore-drills.create", "control.workspaces.resume", "control.workspaces.routing-manifest.get", "control.workspaces.session-exchange.create", "control.workspaces.suspend"},
		Examples: []Example{
			{
				Title:   "Run upgrade job",
				Command: "oar api call --base-url https://control.oar.example --method POST --path /workspaces/ws_123/upgrade --body '{\"desired_version\":\"hosted-instance/v2\"}' --header 'Authorization: Bearer <control-session>'",
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

func (c *Client) ControlAccountsPasskeysRegisterFinish(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.accounts.passkeys.register.finish", nil, opts)
}

func (c *Client) ControlAccountsPasskeysRegisterStart(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.accounts.passkeys.register.start", nil, opts)
}

func (c *Client) ControlAccountsSessionsFinish(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.accounts.sessions.finish", nil, opts)
}

func (c *Client) ControlAccountsSessionsRevokeCurrent(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.accounts.sessions.revoke-current", nil, opts)
}

func (c *Client) ControlAccountsSessionsStart(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.accounts.sessions.start", nil, opts)
}

func (c *Client) ControlBillingWebhooksStripeReceive(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.billing.webhooks.stripe.receive", nil, opts)
}

func (c *Client) ControlOrganizationsBillingCheckoutSessionCreate(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.organizations.billing.checkout-session.create", pathParams, opts)
}

func (c *Client) ControlOrganizationsBillingCustomerPortalSessionCreate(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.organizations.billing.customer-portal-session.create", pathParams, opts)
}

func (c *Client) ControlOrganizationsBillingGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.organizations.billing.get", pathParams, opts)
}

func (c *Client) ControlOrganizationsCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.organizations.create", nil, opts)
}

func (c *Client) ControlOrganizationsGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.organizations.get", pathParams, opts)
}

func (c *Client) ControlOrganizationsInvitesCreate(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.organizations.invites.create", pathParams, opts)
}

func (c *Client) ControlOrganizationsInvitesList(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.organizations.invites.list", pathParams, opts)
}

func (c *Client) ControlOrganizationsInvitesRevoke(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.organizations.invites.revoke", pathParams, opts)
}

func (c *Client) ControlOrganizationsList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.organizations.list", nil, opts)
}

func (c *Client) ControlOrganizationsMembershipsList(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.organizations.memberships.list", pathParams, opts)
}

func (c *Client) ControlOrganizationsMembershipsUpdate(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.organizations.memberships.update", pathParams, opts)
}

func (c *Client) ControlOrganizationsUpdate(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.organizations.update", pathParams, opts)
}

func (c *Client) ControlOrganizationsUsageSummaryGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.organizations.usage-summary.get", pathParams, opts)
}

func (c *Client) ControlOrganizationsWorkspaceInventoryList(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.organizations.workspace-inventory.list", pathParams, opts)
}

func (c *Client) ControlProvisioningJobsGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.provisioning.jobs.get", pathParams, opts)
}

func (c *Client) ControlWorkspacesBackupsCreate(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.workspaces.backups.create", pathParams, opts)
}

func (c *Client) ControlWorkspacesCreate(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.workspaces.create", nil, opts)
}

func (c *Client) ControlWorkspacesDecommission(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.workspaces.decommission", pathParams, opts)
}

func (c *Client) ControlWorkspacesGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.workspaces.get", pathParams, opts)
}

func (c *Client) ControlWorkspacesHeartbeatRecord(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.workspaces.heartbeat.record", pathParams, opts)
}

func (c *Client) ControlWorkspacesLaunchSessionsCreate(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.workspaces.launch-sessions.create", pathParams, opts)
}

func (c *Client) ControlWorkspacesList(ctx context.Context, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.workspaces.list", nil, opts)
}

func (c *Client) ControlWorkspacesReplace(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.workspaces.replace", pathParams, opts)
}

func (c *Client) ControlWorkspacesRestore(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.workspaces.restore", pathParams, opts)
}

func (c *Client) ControlWorkspacesRestoreDrillsCreate(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.workspaces.restore-drills.create", pathParams, opts)
}

func (c *Client) ControlWorkspacesResume(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.workspaces.resume", pathParams, opts)
}

func (c *Client) ControlWorkspacesRoutingManifestGet(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.workspaces.routing-manifest.get", pathParams, opts)
}

func (c *Client) ControlWorkspacesSessionExchangeCreate(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.workspaces.session-exchange.create", pathParams, opts)
}

func (c *Client) ControlWorkspacesSuspend(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.workspaces.suspend", pathParams, opts)
}

func (c *Client) ControlWorkspacesUpgradeCreate(ctx context.Context, pathParams map[string]string, opts RequestOptions) (*http.Response, []byte, error) {
	return c.Invoke(ctx, "control.workspaces.upgrade.create", pathParams, opts)
}
