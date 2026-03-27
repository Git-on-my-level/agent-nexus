package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"organization-autorunner-core/internal/controlplane"
	cpstorage "organization-autorunner-core/internal/controlplane/storage"
	"organization-autorunner-core/internal/controlplaneauth"
)

const (
	testRPID   = "control.oar.test"
	testOrigin = "https://control.oar.test"
)

type controlPlaneTestEnv struct {
	t               *testing.T
	root            string
	workspace       *cpstorage.Workspace
	service         *controlplane.Service
	server          *httptest.Server
	grantIssuer     string
	grantAudience   string
	grantPublicKey  ed25519.PublicKey
	grantPrivateKey ed25519.PrivateKey
}

func TestControlPlaneAccountOrganizationWorkspaceInviteJobAuditFlow(t *testing.T) {
	env := newControlPlaneTestEnv(t, "")
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "owner@example.com", "Owner", "cred-owner")

	loginSession := loginAccount(t, env, "owner@example.com", "cred-owner")
	ownerToken := asString(t, loginSession["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "acme",
		"display_name": "Acme",
		"plan_tier":    "team",
	}, http.StatusCreated, authHeaders(ownerToken))
	organization := asMap(t, createOrganizationResp["organization"])
	organizationID := asString(t, organization["id"])

	organizationsPage := requestJSON(t, http.MethodGet, env.server.URL+"/organizations?limit=1", nil, http.StatusOK, authHeaders(ownerToken))
	if got := len(asSlice(t, organizationsPage["organizations"])); got != 1 {
		t.Fatalf("expected 1 organization, got %d", got)
	}

	createInviteResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations/"+organizationID+"/invites", map[string]any{
		"email": "member@example.com",
		"role":  "member",
	}, http.StatusCreated, authHeaders(ownerToken))
	inviteURL := asString(t, createInviteResp["invite_url"])
	if inviteURL == "" {
		t.Fatal("expected invite_url in create invite response")
	}
	inviteToken := inviteTokenFromURL(t, inviteURL)

	_, memberSession := registerAccount(t, env, "member@example.com", "Member", "cred-member", inviteToken)
	memberToken := asString(t, memberSession["access_token"])

	memberOrganizations := requestJSON(t, http.MethodGet, env.server.URL+"/organizations", nil, http.StatusOK, authHeaders(memberToken))
	if got := len(asSlice(t, memberOrganizations["organizations"])); got != 1 {
		t.Fatalf("expected member to see 1 organization, got %d", got)
	}

	firstMembershipPage := requestJSON(t, http.MethodGet, env.server.URL+"/organizations/"+organizationID+"/memberships?limit=1", nil, http.StatusOK, authHeaders(ownerToken))
	if got := len(asSlice(t, firstMembershipPage["memberships"])); got != 1 {
		t.Fatalf("expected 1 membership on first page, got %d", got)
	}
	membershipCursor := asString(t, firstMembershipPage["next_cursor"])
	if membershipCursor == "" {
		t.Fatal("expected next_cursor for memberships pagination")
	}
	secondMembershipPage := requestJSON(t, http.MethodGet, env.server.URL+"/organizations/"+organizationID+"/memberships?limit=1&cursor="+url.QueryEscape(membershipCursor), nil, http.StatusOK, authHeaders(ownerToken))
	if got := len(asSlice(t, secondMembershipPage["memberships"])); got != 1 {
		t.Fatalf("expected 1 membership on second page, got %d", got)
	}

	firstInvitePage := requestJSON(t, http.MethodGet, env.server.URL+"/organizations/"+organizationID+"/invites?limit=1", nil, http.StatusOK, authHeaders(ownerToken))
	if got := len(asSlice(t, firstInvitePage["invites"])); got != 1 {
		t.Fatalf("expected 1 invite, got %d", got)
	}

	firstWorkspaceCreate := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces", workspaceCreatePayload(t, organizationID, "ops", "Ops", "us-central1", "standard"), http.StatusCreated, authHeaders(ownerToken))
	firstWorkspace := asMap(t, firstWorkspaceCreate["workspace"])
	firstJob := asMap(t, firstWorkspaceCreate["provisioning_job"])

	secondWorkspaceCreate := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces", workspaceCreatePayload(t, organizationID, "eng", "Engineering", "us-east1", "plus"), http.StatusCreated, authHeaders(ownerToken))
	secondWorkspace := asMap(t, secondWorkspaceCreate["workspace"])
	secondJob := asMap(t, secondWorkspaceCreate["provisioning_job"])
	if got := asString(t, firstWorkspace["host_id"]); got == "" {
		t.Fatal("expected host_id on first workspace")
	}
	if got := asString(t, firstWorkspace["workspace_root"]); got == "" {
		t.Fatal("expected workspace_root on first workspace")
	}
	if got := int(asFloat(t, firstWorkspace["listen_port"])); got <= 0 {
		t.Fatalf("expected positive listen_port on first workspace, got %d", got)
	}
	if got := asString(t, secondWorkspace["host_id"]); got != asString(t, firstWorkspace["host_id"]) {
		t.Fatalf("expected second workspace on same packed host, got %q vs %q", got, asString(t, firstWorkspace["host_id"]))
	}
	if got := int(asFloat(t, secondWorkspace["listen_port"])); got == int(asFloat(t, firstWorkspace["listen_port"])) {
		t.Fatalf("expected distinct listen ports, got %d", got)
	}

	workspacesPage := requestJSON(t, http.MethodGet, env.server.URL+"/workspaces?organization_id="+url.QueryEscape(organizationID)+"&limit=1", nil, http.StatusOK, authHeaders(ownerToken))
	if got := len(asSlice(t, workspacesPage["workspaces"])); got != 1 {
		t.Fatalf("expected 1 workspace on first page, got %d", got)
	}
	if asString(t, workspacesPage["next_cursor"]) == "" {
		t.Fatal("expected next_cursor for workspaces pagination")
	}

	jobResp := requestJSON(t, http.MethodGet, env.server.URL+"/provisioning/jobs/"+asString(t, firstJob["id"]), nil, http.StatusOK, authHeaders(ownerToken))
	if got := asString(t, asMap(t, jobResp["job"])["id"]); got != asString(t, firstJob["id"]) {
		t.Fatalf("expected provisioning job id %q, got %q", asString(t, firstJob["id"]), got)
	}

	firstJobsPage := requestJSON(t, http.MethodGet, env.server.URL+"/provisioning/jobs?organization_id="+url.QueryEscape(organizationID)+"&limit=1", nil, http.StatusOK, authHeaders(ownerToken))
	if got := len(asSlice(t, firstJobsPage["jobs"])); got != 1 {
		t.Fatalf("expected 1 job on first page, got %d", got)
	}
	jobsCursor := asString(t, firstJobsPage["next_cursor"])
	if jobsCursor == "" {
		t.Fatal("expected next_cursor for jobs pagination")
	}
	secondJobsPage := requestJSON(t, http.MethodGet, env.server.URL+"/provisioning/jobs?organization_id="+url.QueryEscape(organizationID)+"&limit=1&cursor="+url.QueryEscape(jobsCursor), nil, http.StatusOK, authHeaders(ownerToken))
	if got := len(asSlice(t, secondJobsPage["jobs"])); got != 1 {
		t.Fatalf("expected 1 job on second page, got %d", got)
	}

	firstAccountsPage := requestJSON(t, http.MethodGet, env.server.URL+"/accounts?limit=1", nil, http.StatusOK, authHeaders(ownerToken))
	if got := len(asSlice(t, firstAccountsPage["accounts"])); got != 1 {
		t.Fatalf("expected 1 account on first page, got %d", got)
	}
	if asString(t, firstAccountsPage["next_cursor"]) == "" {
		t.Fatal("expected next_cursor for accounts pagination")
	}

	firstAuditPage := requestJSON(t, http.MethodGet, env.server.URL+"/audit-events?organization_id="+url.QueryEscape(organizationID)+"&limit=2", nil, http.StatusOK, authHeaders(ownerToken))
	if got := len(asSlice(t, firstAuditPage["events"])); got != 2 {
		t.Fatalf("expected 2 audit events on first page, got %d", got)
	}
	auditCursor := asString(t, firstAuditPage["next_cursor"])
	if auditCursor == "" {
		t.Fatal("expected next_cursor for audit pagination")
	}
	secondAuditPage := requestJSON(t, http.MethodGet, env.server.URL+"/audit-events?organization_id="+url.QueryEscape(organizationID)+"&limit=2&cursor="+url.QueryEscape(auditCursor), nil, http.StatusOK, authHeaders(ownerToken))
	if got := len(asSlice(t, secondAuditPage["events"])); got == 0 {
		t.Fatal("expected more audit events on second page")
	}

	usageResp := requestJSON(t, http.MethodGet, env.server.URL+"/organizations/"+organizationID+"/usage-summary", nil, http.StatusOK, authHeaders(ownerToken))
	usageSummary := asMap(t, usageResp["summary"])
	usage := asMap(t, usageSummary["usage"])
	if got := int(asFloat(t, usage["workspace_count"])); got != 2 {
		t.Fatalf("expected usage workspace_count=2, got %d", got)
	}

	launchResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces/"+asString(t, firstWorkspace["id"])+"/launch-sessions", map[string]any{
		"return_path": "/ws/ops",
	}, http.StatusOK, authHeaders(memberToken))
	launchSession := asMap(t, launchResp["launch_session"])
	exchangeResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces/"+asString(t, firstWorkspace["id"])+"/session-exchange", map[string]any{
		"exchange_token": asString(t, launchSession["exchange_token"]),
	}, http.StatusOK, nil)
	grant := asMap(t, exchangeResp["grant"])
	if got := asString(t, grant["scope"]); got != "workspace:"+asString(t, firstWorkspace["id"]) {
		t.Fatalf("expected workspace grant scope, got %q", got)
	}
	grantExpiresAt, err := time.Parse(time.RFC3339Nano, asString(t, grant["expires_at"]))
	if err != nil {
		t.Fatalf("parse workspace grant expiry: %v", err)
	}
	if !grantExpiresAt.After(time.Now().UTC().Add(11 * time.Minute)) {
		t.Fatalf("expected exchanged workspace grant to outlive launch ttl, got %s", grantExpiresAt.Format(time.RFC3339Nano))
	}

	if got := asString(t, secondWorkspace["id"]); got == "" {
		t.Fatal("expected second workspace id")
	}
	if got := asString(t, secondJob["id"]); got == "" {
		t.Fatal("expected second provisioning job id")
	}

	revokeResp := requestJSON(t, http.MethodDelete, env.server.URL+"/account/sessions/current", nil, http.StatusOK, authHeaders(ownerToken))
	if !asBool(t, revokeResp["revoked"]) {
		t.Fatal("expected revoked=true")
	}

	requestJSON(t, http.MethodGet, env.server.URL+"/organizations", nil, http.StatusUnauthorized, authHeaders(ownerToken))

	_ = ownerSession
}

func TestControlPlaneWorkspaceProvisioningProducesReachableDeploymentAndRoutingManifest(t *testing.T) {
	env := newControlPlaneTestEnv(t, "")
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "owner2@example.com", "Owner Two", "cred-owner-2")
	ownerToken := asString(t, ownerSession["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "route-org",
		"display_name": "Route Org",
		"plan_tier":    "team",
	}, http.StatusCreated, authHeaders(ownerToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	createWorkspaceResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces", workspaceCreatePayload(t, organizationID, "route", "Route Workspace", "us-central1", "standard"), http.StatusCreated, authHeaders(ownerToken))
	t.Log("provisioning response received")
	workspace := asMap(t, createWorkspaceResp["workspace"])
	workspaceRoot := asString(t, workspace["workspace_root"])
	if workspaceRoot == "" {
		t.Fatal("expected workspace_root in workspace response")
	}
	if got := asString(t, workspace["host_id"]); got == "" {
		t.Fatal("expected host_id in workspace response")
	}
	listenPort := int(asFloat(t, workspace["listen_port"]))
	if listenPort <= 0 {
		t.Fatalf("expected positive listen_port in workspace response, got %d", listenPort)
	}
	if got := asString(t, workspace["core_origin"]); got != fmt.Sprintf("http://127.0.0.1:%d", listenPort) {
		t.Fatalf("expected core_origin %q, got %q", fmt.Sprintf("http://127.0.0.1:%d", listenPort), got)
	}
	deploymentRoot := asString(t, workspace["deployment_root"])
	if deploymentRoot == "" {
		t.Fatal("expected deployment_root in workspace response")
	}
	if got := workspaceRoot; got != filepath.Join(deploymentRoot, "workspace") {
		t.Fatalf("expected workspace_root %q, got %q", filepath.Join(deploymentRoot, "workspace"), got)
	}
	if got := asString(t, workspace["routing_manifest_path"]); got == "" {
		t.Fatal("expected routing_manifest_path in workspace response")
	}

	manifestResp := requestJSON(t, http.MethodGet, env.server.URL+"/workspaces/"+asString(t, workspace["id"])+"/routing-manifest", nil, http.StatusOK, authHeaders(ownerToken))
	t.Log("routing manifest response received")
	manifest := asMap(t, manifestResp["routing_manifest"])
	if got := asString(t, manifest["host_id"]); got != asString(t, workspace["host_id"]) {
		t.Fatalf("expected routing manifest host_id %q, got %q", asString(t, workspace["host_id"]), got)
	}
	if got := asString(t, manifest["workspace_root"]); got != workspaceRoot {
		t.Fatalf("expected routing manifest workspace_root %q, got %q", workspaceRoot, got)
	}
	if got := int(asFloat(t, manifest["listen_port"])); got != listenPort {
		t.Fatalf("expected routing manifest listen_port %d, got %d", listenPort, got)
	}
	if got := asString(t, manifest["core_origin"]); got != fmt.Sprintf("http://127.0.0.1:%d", listenPort) {
		t.Fatalf("expected routing manifest core_origin %q, got %q", fmt.Sprintf("http://127.0.0.1:%d", listenPort), got)
	}
	if got := asString(t, manifest["deployment_root"]); got != deploymentRoot {
		t.Fatalf("expected routing manifest deployment_root %q, got %q", deploymentRoot, got)
	}

	coreWorkspaceRoot := filepath.Join(deploymentRoot, "workspace")
	t.Log("starting core server")
	coreCmd, baseURL, cleanup := startCoreServerForTest(t, coreWorkspaceRoot)
	defer cleanup()
	t.Log("core server ready")

	readyResp := requestJSON(t, http.MethodGet, baseURL+"/readyz", nil, http.StatusOK, nil)
	t.Log("readyz response received")
	if !asBool(t, readyResp["ok"]) {
		t.Fatal("expected reachable workspace core to report ok=true")
	}

	_ = coreCmd
}

func TestControlPlaneOrganizationBillingSummaryAndStripeWebhookScaffold(t *testing.T) {
	env := newControlPlaneTestEnvWithConfig(t, "", "", func(config *controlplane.Config) {
		config.Stripe = controlplane.StripeConfig{
			PlanPriceIDs: map[string]string{
				"team":       "price_team_test",
				"scale":      "price_scale_test",
				"enterprise": "price_enterprise_test",
			},
		}
	})
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "owner-billing@example.com", "Owner Billing", "cred-owner-billing")
	ownerToken := asString(t, ownerSession["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "billing-ready",
		"display_name": "Billing Ready",
		"plan_tier":    "starter",
	}, http.StatusCreated, authHeaders(ownerToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	billingResp := requestJSON(t, http.MethodGet, env.server.URL+"/organizations/"+organizationID+"/billing", nil, http.StatusOK, authHeaders(ownerToken))
	billingSummary := asMap(t, billingResp["summary"])
	billingAccount := asMap(t, billingSummary["billing_account"])
	if got := asString(t, billingAccount["billing_status"]); got != "free" {
		t.Fatalf("expected free billing status, got %q", got)
	}
	configuration := asMap(t, billingSummary["configuration"])
	if asBool(t, configuration["configured"]) {
		t.Fatal("expected stripe scaffold to report incomplete configuration in test env")
	}

	webhookResp := requestJSON(t, http.MethodPost, env.server.URL+"/billing/webhooks/stripe", map[string]any{
		"id":   "evt_test_billing_ready",
		"type": "customer.subscription.updated",
		"data": map[string]any{
			"object": map[string]any{
				"id":                   "sub_test_billing_ready",
				"customer":             "cus_test_billing_ready",
				"status":               "active",
				"cancel_at_period_end": false,
				"current_period_end":   float64(1775000000),
				"metadata": map[string]any{
					"organization_id": organizationID,
				},
				"items": map[string]any{
					"data": []any{
						map[string]any{
							"price": map[string]any{
								"id": "price_team_test",
							},
						},
					},
				},
			},
		},
	}, http.StatusOK, nil)
	webhook := asMap(t, webhookResp["webhook"])
	if got := asString(t, webhook["verification_status"]); got != "skipped" {
		t.Fatalf("expected skipped verification status without configured secret, got %q", got)
	}

	updatedBillingResp := requestJSON(t, http.MethodGet, env.server.URL+"/organizations/"+organizationID+"/billing", nil, http.StatusOK, authHeaders(ownerToken))
	updatedBillingSummary := asMap(t, updatedBillingResp["summary"])
	updatedBillingAccount := asMap(t, updatedBillingSummary["billing_account"])
	if got := asString(t, updatedBillingAccount["stripe_customer_id"]); got != "cus_test_billing_ready" {
		t.Fatalf("expected stripe customer id to be updated, got %q", got)
	}
	if got := asString(t, updatedBillingAccount["stripe_subscription_status"]); got != "active" {
		t.Fatalf("expected active subscription status, got %q", got)
	}
	if got := asString(t, updatedBillingAccount["billing_status"]); got != "active" {
		t.Fatalf("expected active billing status, got %q", got)
	}
	if got := asString(t, updatedBillingSummary["plan_tier"]); got != "team" {
		t.Fatalf("expected synced organization plan tier team, got %q", got)
	}
}

func TestControlPlaneBillingCheckoutAndCustomerPortalSessions(t *testing.T) {
	var stripeRequests []struct {
		path string
		form url.Values
		auth string
	}
	stripeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse stripe form: %v", err)
		}
		stripeRequests = append(stripeRequests, struct {
			path string
			form url.Values
			auth string
		}{
			path: r.URL.Path,
			form: r.PostForm,
			auth: r.Header.Get("Authorization"),
		})
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/customers":
			_, _ = io.WriteString(w, `{"id":"cus_checkout_test"}`)
		case "/v1/checkout/sessions":
			_, _ = io.WriteString(w, `{"id":"cs_checkout_test","url":"https://checkout.stripe.test/session/cs_checkout_test"}`)
		case "/v1/billing_portal/sessions":
			_, _ = io.WriteString(w, `{"id":"bps_checkout_test","url":"https://billing.stripe.test/session/bps_checkout_test"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer stripeServer.Close()

	env := newControlPlaneTestEnvWithConfig(t, "", "", func(config *controlplane.Config) {
		config.Stripe = controlplane.StripeConfig{
			APIBaseURL:         stripeServer.URL,
			SecretKey:          "sk_test_checkout",
			CheckoutSuccessURL: "https://app.oar.test/billing/success",
			CheckoutCancelURL:  "https://app.oar.test/billing/cancel",
			PortalReturnURL:    "https://app.oar.test/dashboard",
			PlanPriceIDs: map[string]string{
				"team":       "price_team_test",
				"scale":      "price_scale_test",
				"enterprise": "price_enterprise_test",
			},
		}
	})
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "owner-paid@example.com", "Owner Paid", "cred-owner-paid")
	ownerToken := asString(t, ownerSession["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "billing-live",
		"display_name": "Billing Live",
		"plan_tier":    "starter",
	}, http.StatusCreated, authHeaders(ownerToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	checkoutResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations/"+organizationID+"/billing/checkout-session", map[string]any{
		"plan_tier": "team",
	}, http.StatusOK, authHeaders(ownerToken))
	checkoutSession := asMap(t, checkoutResp["session"])
	if got := asString(t, checkoutSession["status"]); got != "created" {
		t.Fatalf("expected created checkout session, got %q", got)
	}
	if got := asString(t, checkoutSession["url"]); got != "https://checkout.stripe.test/session/cs_checkout_test" {
		t.Fatalf("unexpected checkout url %q", got)
	}

	portalResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations/"+organizationID+"/billing/customer-portal-session", map[string]any{}, http.StatusOK, authHeaders(ownerToken))
	portalSession := asMap(t, portalResp["session"])
	if got := asString(t, portalSession["status"]); got != "created" {
		t.Fatalf("expected created portal session, got %q", got)
	}
	if got := asString(t, portalSession["url"]); got != "https://billing.stripe.test/session/bps_checkout_test" {
		t.Fatalf("unexpected portal url %q", got)
	}

	if len(stripeRequests) != 3 {
		t.Fatalf("expected 3 stripe requests, got %d", len(stripeRequests))
	}
	if stripeRequests[0].path != "/v1/customers" {
		t.Fatalf("expected first stripe request to create customer, got %q", stripeRequests[0].path)
	}
	if got := stripeRequests[1].form.Get("line_items[0][price]"); got != "price_team_test" {
		t.Fatalf("expected checkout price_team_test, got %q", got)
	}
	if got := stripeRequests[1].form.Get("subscription_data[metadata][organization_id]"); got != organizationID {
		t.Fatalf("expected checkout organization metadata, got %q", got)
	}
	if got := stripeRequests[2].form.Get("customer"); got != "cus_checkout_test" {
		t.Fatalf("expected portal customer cus_checkout_test, got %q", got)
	}
	for _, request := range stripeRequests {
		if !strings.HasPrefix(request.auth, "Basic ") {
			t.Fatalf("expected stripe basic auth header, got %q", request.auth)
		}
	}
}

func TestControlPlaneStripeWebhookRetriesExistingUnprocessedEvent(t *testing.T) {
	env := newControlPlaneTestEnvWithConfig(t, "", "", func(config *controlplane.Config) {
		config.Stripe = controlplane.StripeConfig{
			PlanPriceIDs: map[string]string{
				"team":       "price_team_test",
				"scale":      "price_scale_test",
				"enterprise": "price_enterprise_test",
			},
		}
	})
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "owner-webhook-retry@example.com", "Owner Webhook Retry", "cred-owner-webhook-retry")
	ownerToken := asString(t, ownerSession["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "webhook-retry",
		"display_name": "Webhook Retry",
		"plan_tier":    "starter",
	}, http.StatusCreated, authHeaders(ownerToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	nowText := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := env.workspace.DB().ExecContext(
		context.Background(),
		`INSERT INTO stripe_webhook_events(
			event_id, event_type, verification_status, organization_id, stripe_customer_id, stripe_subscription_id,
			received_at, processed_at, payload_json, signature_header, processing_error
		) VALUES (?, ?, ?, ?, ?, ?, ?, NULL, ?, ?, ?)`,
		"evt_retry_webhook",
		"customer.subscription.updated",
		"skipped",
		organizationID,
		"cus_retry_webhook",
		"sub_retry_webhook",
		nowText,
		`{"id":"evt_retry_webhook"}`,
		"",
		"transient failure",
	); err != nil {
		t.Fatalf("seed webhook event: %v", err)
	}

	requestJSON(t, http.MethodPost, env.server.URL+"/billing/webhooks/stripe", map[string]any{
		"id":   "evt_retry_webhook",
		"type": "customer.subscription.updated",
		"data": map[string]any{
			"object": map[string]any{
				"id":                   "sub_retry_webhook",
				"customer":             "cus_retry_webhook",
				"status":               "active",
				"cancel_at_period_end": false,
				"current_period_end":   float64(1775000000),
				"metadata": map[string]any{
					"organization_id": organizationID,
				},
				"items": map[string]any{
					"data": []any{
						map[string]any{
							"price": map[string]any{
								"id": "price_team_test",
							},
						},
					},
				},
			},
		},
	}, http.StatusOK, nil)

	billingResp := requestJSON(t, http.MethodGet, env.server.URL+"/organizations/"+organizationID+"/billing", nil, http.StatusOK, authHeaders(ownerToken))
	billingSummary := asMap(t, billingResp["summary"])
	if got := asString(t, billingSummary["plan_tier"]); got != "team" {
		t.Fatalf("expected retried webhook to sync team plan, got %q", got)
	}
	billingAccount := asMap(t, billingSummary["billing_account"])
	if got := asString(t, billingAccount["billing_status"]); got != "active" {
		t.Fatalf("expected retried webhook to update billing status, got %q", got)
	}

	var (
		processedAt     sql.NullString
		processingError string
	)
	if err := env.workspace.DB().QueryRowContext(context.Background(), `SELECT processed_at, processing_error FROM stripe_webhook_events WHERE event_id = ?`, "evt_retry_webhook").Scan(&processedAt, &processingError); err != nil {
		t.Fatalf("load webhook event: %v", err)
	}
	if !processedAt.Valid || strings.TrimSpace(processedAt.String) == "" {
		t.Fatal("expected retried webhook event to be marked processed")
	}
	if strings.TrimSpace(processingError) != "" {
		t.Fatalf("expected processing_error to be cleared, got %q", processingError)
	}
}

func TestControlPlaneStripeWebhookFallsBackToSubscriptionLookup(t *testing.T) {
	env := newControlPlaneTestEnvWithConfig(t, "", "", func(config *controlplane.Config) {
		config.Stripe = controlplane.StripeConfig{
			PlanPriceIDs: map[string]string{
				"team":       "price_team_test",
				"scale":      "price_scale_test",
				"enterprise": "price_enterprise_test",
			},
		}
	})
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "owner-subscription-lookup@example.com", "Owner Subscription Lookup", "cred-owner-subscription-lookup")
	ownerToken := asString(t, ownerSession["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "subscription-lookup",
		"display_name": "Subscription Lookup",
		"plan_tier":    "starter",
	}, http.StatusCreated, authHeaders(ownerToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	if _, err := env.workspace.DB().ExecContext(
		context.Background(),
		`UPDATE organization_billing
			SET stripe_subscription_id = ?, billing_status = ?, cancel_at_period_end = ?, updated_at = ?
		WHERE organization_id = ?`,
		"sub_lookup_fallback",
		"active",
		1,
		time.Now().UTC().Format(time.RFC3339Nano),
		organizationID,
	); err != nil {
		t.Fatalf("seed subscription lookup: %v", err)
	}

	requestJSON(t, http.MethodPost, env.server.URL+"/billing/webhooks/stripe", map[string]any{
		"id":   "evt_subscription_lookup",
		"type": "invoice.paid",
		"data": map[string]any{
			"object": map[string]any{
				"subscription":       "sub_lookup_fallback",
				"customer":           "cus_missing_lookup",
				"status":             "paid",
				"current_period_end": float64(1775000000),
				"lines": map[string]any{
					"data": []any{
						map[string]any{
							"price": map[string]any{
								"id": "price_team_test",
							},
						},
					},
				},
			},
		},
	}, http.StatusOK, nil)

	billingResp := requestJSON(t, http.MethodGet, env.server.URL+"/organizations/"+organizationID+"/billing", nil, http.StatusOK, authHeaders(ownerToken))
	billingSummary := asMap(t, billingResp["summary"])
	if got := asString(t, billingSummary["plan_tier"]); got != "team" {
		t.Fatalf("expected subscription lookup fallback to sync team plan, got %q", got)
	}
	billingAccount := asMap(t, billingSummary["billing_account"])
	if got := asString(t, billingAccount["stripe_customer_id"]); got != "cus_missing_lookup" {
		t.Fatalf("expected fallback webhook to update stripe customer id, got %q", got)
	}
	if got := asString(t, billingAccount["billing_status"]); got != "active" {
		t.Fatalf("expected unsupported invoice status to preserve active billing status, got %q", got)
	}
	if got := billingAccount["cancel_at_period_end"]; got != true {
		t.Fatalf("expected sparse webhook to preserve cancel_at_period_end=true, got %#v", got)
	}
}

func TestControlPlaneCreateWorkspaceWritesPlanQuotaEnv(t *testing.T) {
	env := newControlPlaneTestEnv(t, "")
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "owner-plan-quota@example.com", "Owner Plan Quota", "cred-owner-plan-quota")
	ownerToken := asString(t, ownerSession["access_token"])

	createStarterOrgResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "quota-free",
		"display_name": "Quota Free",
		"plan_tier":    "starter",
	}, http.StatusCreated, authHeaders(ownerToken))
	starterOrganizationID := asString(t, asMap(t, createStarterOrgResp["organization"])["id"])
	createStarterWorkspaceResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces", workspaceCreatePayload(t, starterOrganizationID, "free-ws", "Free Workspace", "us-central1", "standard"), http.StatusCreated, authHeaders(ownerToken))
	starterWorkspace := asMap(t, createStarterWorkspaceResp["workspace"])
	starterEnvFile := filepath.Join(asString(t, starterWorkspace["deployment_root"]), "config", "env.production")
	if got := envVarFromFile(t, starterEnvFile, "OAR_WORKSPACE_MAX_BLOB_BYTES"); got != "262144000" {
		t.Fatalf("expected starter max blob bytes 262144000, got %q", got)
	}
	if got := envVarFromFile(t, starterEnvFile, "OAR_WORKSPACE_MAX_ARTIFACTS"); got != "1000" {
		t.Fatalf("expected starter max artifacts 1000, got %q", got)
	}

	createTeamOrgResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "quota-team",
		"display_name": "Quota Team",
		"plan_tier":    "team",
	}, http.StatusCreated, authHeaders(ownerToken))
	teamOrganizationID := asString(t, asMap(t, createTeamOrgResp["organization"])["id"])
	createTeamWorkspaceResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces", workspaceCreatePayload(t, teamOrganizationID, "team-ws", "Team Workspace", "us-central1", "standard"), http.StatusCreated, authHeaders(ownerToken))
	teamWorkspace := asMap(t, createTeamWorkspaceResp["workspace"])
	teamEnvFile := filepath.Join(asString(t, teamWorkspace["deployment_root"]), "config", "env.production")
	if got := envVarFromFile(t, teamEnvFile, "OAR_WORKSPACE_MAX_BLOB_BYTES"); got != "26843545600" {
		t.Fatalf("expected team max blob bytes 26843545600, got %q", got)
	}
	if got := envVarFromFile(t, teamEnvFile, "OAR_WORKSPACE_MAX_ARTIFACTS"); got != "25000" {
		t.Fatalf("expected team max artifacts 25000, got %q", got)
	}
}

func TestControlPlaneSessionExchangeRequiresActiveMembership(t *testing.T) {
	env := newControlPlaneTestEnv(t, "")
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "owner-launch@example.com", "Owner Launch", "cred-owner-launch")
	ownerToken := asString(t, ownerSession["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "launch-guard",
		"display_name": "Launch Guard",
		"plan_tier":    "team",
	}, http.StatusCreated, authHeaders(ownerToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	createInviteResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations/"+organizationID+"/invites", map[string]any{
		"email": "member-launch@example.com",
		"role":  "member",
	}, http.StatusCreated, authHeaders(ownerToken))
	inviteToken := inviteTokenFromURL(t, asString(t, createInviteResp["invite_url"]))

	memberAccount, memberSession := registerAccount(t, env, "member-launch@example.com", "Member Launch", "cred-member-launch", inviteToken)
	memberToken := asString(t, memberSession["access_token"])

	createWorkspaceResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces", workspaceCreatePayload(t, organizationID, "ops", "Ops", "us-central1", "standard"), http.StatusCreated, authHeaders(ownerToken))
	workspaceID := asString(t, asMap(t, createWorkspaceResp["workspace"])["id"])

	launchResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces/"+workspaceID+"/launch-sessions", map[string]any{
		"return_path": "/",
	}, http.StatusOK, authHeaders(memberToken))
	launchSession := asMap(t, launchResp["launch_session"])

	if _, err := env.workspace.DB().ExecContext(
		context.Background(),
		`UPDATE organization_memberships SET status = ? WHERE organization_id = ? AND account_id = ?`,
		"suspended",
		organizationID,
		asString(t, memberAccount["id"]),
	); err != nil {
		t.Fatalf("suspend membership: %v", err)
	}

	exchangeResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces/"+workspaceID+"/session-exchange", map[string]any{
		"exchange_token": asString(t, launchSession["exchange_token"]),
	}, http.StatusForbidden, nil)

	if got := asString(t, asMap(t, exchangeResp["error"])["code"]); got != "access_denied" {
		t.Fatalf("expected access_denied, got %q", got)
	}
}

func TestControlPlaneSessionExchangeConsumesLaunchTokensAtomically(t *testing.T) {
	env := newControlPlaneTestEnv(t, "")
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "owner-launch-race@example.com", "Owner Launch Race", "cred-owner-launch-race")
	ownerToken := asString(t, ownerSession["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "launch-race",
		"display_name": "Launch Race",
		"plan_tier":    "team",
	}, http.StatusCreated, authHeaders(ownerToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	createInviteResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations/"+organizationID+"/invites", map[string]any{
		"email": "member-launch-race@example.com",
		"role":  "member",
	}, http.StatusCreated, authHeaders(ownerToken))
	inviteToken := inviteTokenFromURL(t, asString(t, createInviteResp["invite_url"]))

	_, memberSession := registerAccount(t, env, "member-launch-race@example.com", "Member Launch Race", "cred-member-launch-race", inviteToken)
	memberToken := asString(t, memberSession["access_token"])

	createWorkspaceResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces", workspaceCreatePayload(t, organizationID, "race", "Race Workspace", "us-central1", "standard"), http.StatusCreated, authHeaders(ownerToken))
	workspaceID := asString(t, asMap(t, createWorkspaceResp["workspace"])["id"])

	launchResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces/"+workspaceID+"/launch-sessions", map[string]any{
		"return_path": "/",
	}, http.StatusOK, authHeaders(memberToken))
	exchangeToken := asString(t, asMap(t, launchResp["launch_session"])["exchange_token"])

	type result struct {
		status int
		body   string
		err    error
	}

	results := make(chan result, 2)
	client := &http.Client{Timeout: 10 * time.Second}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			payload, err := json.Marshal(map[string]any{"exchange_token": exchangeToken})
			if err != nil {
				results <- result{err: err}
				return
			}
			req, err := http.NewRequest(http.MethodPost, env.server.URL+"/workspaces/"+workspaceID+"/session-exchange", bytes.NewReader(payload))
			if err != nil {
				results <- result{err: err}
				return
			}
			req.Header.Set("Content-Type", "application/json")

			resp, err := client.Do(req)
			if err != nil {
				results <- result{err: err}
				return
			}
			defer resp.Body.Close()

			rawBody, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				results <- result{err: readErr}
				return
			}
			results <- result{status: resp.StatusCode, body: string(rawBody)}
		}()
	}

	close(start)
	wg.Wait()
	close(results)

	var okCount int
	var conflictCount int
	for result := range results {
		if result.err != nil {
			t.Fatalf("exchange request failed: %v", result.err)
		}
		switch result.status {
		case http.StatusOK:
			okCount++
		case http.StatusConflict:
			conflictCount++
			var payload map[string]any
			if err := json.Unmarshal([]byte(result.body), &payload); err != nil {
				t.Fatalf("decode conflict payload: %v", err)
			}
			if got := asString(t, asMap(t, payload["error"])["code"]); got != "exchange_invalid" {
				t.Fatalf("expected exchange_invalid, got %q with body %s", got, result.body)
			}
		default:
			t.Fatalf("unexpected status %d body=%s", result.status, result.body)
		}
	}
	if okCount != 1 || conflictCount != 1 {
		t.Fatalf("expected one successful exchange and one conflict, got ok=%d conflict=%d", okCount, conflictCount)
	}
}

func TestControlPlaneWorkspaceLifecycleMutationsRequireManageRoleAndPersistState(t *testing.T) {
	env := newControlPlaneTestEnv(t, "")
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "owner-lifecycle@example.com", "Owner Lifecycle", "cred-owner-lifecycle")
	ownerToken := asString(t, ownerSession["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "lifecycle-org",
		"display_name": "Lifecycle Org",
		"plan_tier":    "team",
	}, http.StatusCreated, authHeaders(ownerToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	createInviteResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations/"+organizationID+"/invites", map[string]any{
		"email": "member-lifecycle@example.com",
		"role":  "member",
	}, http.StatusCreated, authHeaders(ownerToken))
	inviteToken := inviteTokenFromURL(t, asString(t, createInviteResp["invite_url"]))

	_, memberSession := registerAccount(t, env, "member-lifecycle@example.com", "Member Lifecycle", "cred-member-lifecycle", inviteToken)
	memberToken := asString(t, memberSession["access_token"])

	createWorkspaceResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces", workspaceCreatePayload(t, organizationID, "lifecycle", "Lifecycle Workspace", "us-central1", "standard"), http.StatusCreated, authHeaders(ownerToken))
	workspaceID := asString(t, asMap(t, createWorkspaceResp["workspace"])["id"])

	deniedSuspendResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces/"+workspaceID+"/suspend", nil, http.StatusForbidden, authHeaders(memberToken))
	if got := asString(t, asMap(t, deniedSuspendResp["error"])["code"]); got != "access_denied" {
		t.Fatalf("expected suspend to require manage role, got %q", got)
	}

	suspendResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces/"+workspaceID+"/suspend", nil, http.StatusOK, authHeaders(ownerToken))
	suspendedWorkspace := asMap(t, suspendResp["workspace"])
	if got := asString(t, suspendedWorkspace["status"]); got != "suspended" {
		t.Fatalf("expected suspend response status=suspended, got %q", got)
	}

	getWorkspaceResp := requestJSON(t, http.MethodGet, env.server.URL+"/workspaces/"+workspaceID, nil, http.StatusOK, authHeaders(ownerToken))
	if got := asString(t, asMap(t, getWorkspaceResp["workspace"])["status"]); got != "suspended" {
		t.Fatalf("expected persisted workspace status=suspended, got %q", got)
	}

	manifestResp := requestJSON(t, http.MethodGet, env.server.URL+"/workspaces/"+workspaceID+"/routing-manifest", nil, http.StatusOK, authHeaders(ownerToken))
	if got := asString(t, asMap(t, manifestResp["routing_manifest"])["current_state"]); got != "suspended" {
		t.Fatalf("expected persisted routing manifest current_state=suspended, got %q", got)
	}

	resumeResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces/"+workspaceID+"/resume", nil, http.StatusOK, authHeaders(ownerToken))
	resumedWorkspace := asMap(t, resumeResp["workspace"])
	if got := asString(t, resumedWorkspace["status"]); got != "ready" {
		t.Fatalf("expected resume response status=ready, got %q", got)
	}

	getWorkspaceResp = requestJSON(t, http.MethodGet, env.server.URL+"/workspaces/"+workspaceID, nil, http.StatusOK, authHeaders(ownerToken))
	if got := asString(t, asMap(t, getWorkspaceResp["workspace"])["status"]); got != "ready" {
		t.Fatalf("expected persisted workspace status=ready after resume, got %q", got)
	}
}

func TestControlPlaneCreateWorkspaceRequiresServiceIdentity(t *testing.T) {
	env := newControlPlaneTestEnv(t, "")
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "owner-service-identity@example.com", "Owner Service Identity", "cred-owner-service-identity")
	ownerToken := asString(t, ownerSession["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "service-identity-org",
		"display_name": "Service Identity Org",
		"plan_tier":    "team",
	}, http.StatusCreated, authHeaders(ownerToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	createWorkspaceResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces", map[string]any{
		"organization_id": organizationID,
		"slug":            "missing-service-identity",
		"display_name":    "Missing Service Identity",
		"region":          "us-central1",
		"workspace_tier":  "standard",
	}, http.StatusBadRequest, authHeaders(ownerToken))

	errPayload := asMap(t, createWorkspaceResp["error"])
	if got := asString(t, errPayload["code"]); got != "invalid_request" {
		t.Fatalf("expected invalid_request, got %q", got)
	}
	if got := asString(t, errPayload["message"]); got != "service_identity_id and service_identity_public_key are required" {
		t.Fatalf("expected service identity validation message, got %q", got)
	}
}

func TestControlPlaneCreateWorkspaceRejectsReservedSlug(t *testing.T) {
	env := newControlPlaneTestEnv(t, "")
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "owner-reserved@example.com", "Owner Reserved", "cred-owner-reserved")
	ownerToken := asString(t, ownerSession["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "reserved-org",
		"display_name": "Reserved Org",
		"plan_tier":    "team",
	}, http.StatusCreated, authHeaders(ownerToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	resp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces", workspaceCreatePayload(t, organizationID, "dashboard", "Reserved Workspace", "us-central1", "standard"), http.StatusBadRequest, authHeaders(ownerToken))
	errPayload := asMap(t, resp["error"])
	if got := asString(t, errPayload["code"]); got != "invalid_request" {
		t.Fatalf("expected invalid_request, got %q", got)
	}
	if got := asString(t, errPayload["message"]); !strings.Contains(got, "reserved") {
		t.Fatalf("expected reserved-slug message, got %q", got)
	}
}

func TestControlPlaneOrganizationRetainsAtLeastOneActiveManager(t *testing.T) {
	env := newControlPlaneTestEnv(t, "")
	defer env.Close()

	ownerAccount, ownerSession := registerAccount(t, env, "owner-manager@example.com", "Owner Manager", "cred-owner-manager")
	ownerToken := asString(t, ownerSession["access_token"])
	ownerID := asString(t, ownerAccount["id"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "manager-guard",
		"display_name": "Manager Guard",
		"plan_tier":    "team",
	}, http.StatusCreated, authHeaders(ownerToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	var ownerMembershipID string
	if err := env.workspace.DB().QueryRowContext(
		context.Background(),
		`SELECT id FROM organization_memberships WHERE organization_id = ? AND account_id = ?`,
		organizationID,
		ownerID,
	).Scan(&ownerMembershipID); err != nil {
		t.Fatalf("load owner membership: %v", err)
	}

	blockedResp := requestJSON(t, http.MethodPatch, env.server.URL+"/organizations/"+organizationID+"/memberships/"+ownerMembershipID, map[string]any{
		"role": "viewer",
	}, http.StatusConflict, authHeaders(ownerToken))
	if got := asString(t, asMap(t, blockedResp["error"])["code"]); got != "manager_required" {
		t.Fatalf("expected manager_required, got %q", got)
	}

	createInviteResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations/"+organizationID+"/invites", map[string]any{
		"email": "admin-manager@example.com",
		"role":  "admin",
	}, http.StatusCreated, authHeaders(ownerToken))
	adminInviteToken := inviteTokenFromURL(t, asString(t, createInviteResp["invite_url"]))

	_, _ = registerAccount(t, env, "admin-manager@example.com", "Admin Manager", "cred-admin-manager", adminInviteToken)

	allowedResp := requestJSON(t, http.MethodPatch, env.server.URL+"/organizations/"+organizationID+"/memberships/"+ownerMembershipID, map[string]any{
		"role": "viewer",
	}, http.StatusOK, authHeaders(ownerToken))
	if got := asString(t, asMap(t, allowedResp["membership"])["role"]); got != "viewer" {
		t.Fatalf("expected owner demotion after adding admin, got role %q", got)
	}
}

func TestControlPlaneCreateWorkspaceEnforcesQuotaInsideInsert(t *testing.T) {
	env := newControlPlaneTestEnv(t, "")
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "owner-quota-race@example.com", "Owner Quota Race", "cred-owner-quota-race")
	ownerToken := asString(t, ownerSession["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "quota-race",
		"display_name": "Quota Race",
		"plan_tier":    "starter",
	}, http.StatusCreated, authHeaders(ownerToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	type result struct {
		status int
		body   string
		err    error
	}

	inputs := []map[string]any{
		workspaceCreatePayload(t, organizationID, "alpha", "Alpha", "us-central1", "standard"),
		workspaceCreatePayload(t, organizationID, "beta", "Beta", "us-east1", "standard"),
	}

	results := make(chan result, len(inputs))
	client := &http.Client{Timeout: 15 * time.Second}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, payload := range inputs {
		wg.Add(1)
		go func(payload map[string]any) {
			defer wg.Done()
			<-start

			rawPayload, err := json.Marshal(payload)
			if err != nil {
				results <- result{err: err}
				return
			}
			req, err := http.NewRequest(http.MethodPost, env.server.URL+"/workspaces", bytes.NewReader(rawPayload))
			if err != nil {
				results <- result{err: err}
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+ownerToken)

			resp, err := client.Do(req)
			if err != nil {
				results <- result{err: err}
				return
			}
			defer resp.Body.Close()

			rawBody, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				results <- result{err: readErr}
				return
			}
			results <- result{status: resp.StatusCode, body: string(rawBody)}
		}(payload)
	}

	close(start)
	wg.Wait()
	close(results)

	var createdCount int
	var quotaCount int
	for result := range results {
		if result.err != nil {
			t.Fatalf("create workspace request failed: %v", result.err)
		}
		switch result.status {
		case http.StatusCreated:
			createdCount++
		case http.StatusUnprocessableEntity:
			quotaCount++
			var payload map[string]any
			if err := json.Unmarshal([]byte(result.body), &payload); err != nil {
				t.Fatalf("decode quota payload: %v", err)
			}
			if got := asString(t, asMap(t, payload["error"])["code"]); got != "quota_exceeded" {
				t.Fatalf("expected quota_exceeded, got %q with body %s", got, result.body)
			}
		default:
			t.Fatalf("unexpected status %d body=%s", result.status, result.body)
		}
	}
	if createdCount != 1 || quotaCount != 1 {
		t.Fatalf("expected one created workspace and one quota rejection, got created=%d quota=%d", createdCount, quotaCount)
	}

	var workspaceCount int
	if err := env.workspace.DB().QueryRowContext(context.Background(), `SELECT COUNT(1) FROM workspaces WHERE organization_id = ?`, organizationID).Scan(&workspaceCount); err != nil {
		t.Fatalf("count workspaces: %v", err)
	}
	if workspaceCount != 1 {
		t.Fatalf("expected exactly one persisted workspace, got %d", workspaceCount)
	}
}

func TestControlPlaneWorkspaceBackupMaintenanceAndRetentionSweep(t *testing.T) {
	env := newControlPlaneTestEnv(t, "")
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "backup-owner@example.com", "Backup Owner", "cred-backup-owner")
	ownerToken := asString(t, ownerSession["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "backup-org",
		"display_name": "Backup Org",
		"plan_tier":    "team",
	}, http.StatusCreated, authHeaders(ownerToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	createWorkspaceResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces", workspaceCreatePayload(t, organizationID, "nightly", "Nightly Workspace", "us-central1", "standard"), http.StatusCreated, authHeaders(ownerToken))
	workspace := asMap(t, createWorkspaceResp["workspace"])
	workspaceID := asString(t, workspace["id"])
	coreWorkspaceRoot := filepath.Join(asString(t, workspace["deployment_root"]), "workspace")
	_, _, cleanup := startCoreServerForTest(t, coreWorkspaceRoot)
	defer cleanup()

	dueAt := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	if _, err := env.workspace.DB().ExecContext(context.Background(), `UPDATE workspace_backup_schedules
		SET next_run_at = ?, retention_days = ?, interval_seconds = ?, schedule_name = ?, updated_at = ?
		WHERE workspace_id = ?`,
		dueAt,
		1,
		int(time.Hour/time.Second),
		"nightly",
		time.Now().UTC().Format(time.RFC3339Nano),
		workspaceID,
	); err != nil {
		t.Fatalf("force workspace backup schedule due: %v", err)
	}

	if err := env.service.RunBackupMaintenancePass(context.Background()); err != nil {
		t.Fatalf("run backup maintenance pass: %v", err)
	}

	var (
		runID              string
		status             string
		backupDir          string
		retentionExpiresAt sql.NullString
		failureReason      sql.NullString
		prunedAt           sql.NullString
	)
	row := env.workspace.DB().QueryRowContext(context.Background(), `SELECT id, status, backup_dir, retention_expires_at, failure_reason, pruned_at
		FROM workspace_backup_runs
		WHERE workspace_id = ?
		ORDER BY requested_at DESC, id DESC
		LIMIT 1`, workspaceID)
	if err := row.Scan(&runID, &status, &backupDir, &retentionExpiresAt, &failureReason, &prunedAt); err != nil {
		t.Fatalf("load workspace backup run: %v", err)
	}
	if status != "succeeded" {
		t.Fatalf("expected scheduled backup run to succeed, got %q (%s)", status, strings.TrimSpace(failureReason.String))
	}
	if backupDir == "" {
		t.Fatal("expected backup_dir in backup run record")
	}
	if !retentionExpiresAt.Valid || strings.TrimSpace(retentionExpiresAt.String) == "" {
		t.Fatal("expected retention_expires_at in backup run record")
	}
	if prunedAt.Valid {
		t.Fatal("did not expect backup run to be pruned before retention expiry")
	}

	inventoryResp := requestJSON(t, http.MethodGet, env.server.URL+"/organizations/"+organizationID+"/workspace-inventory?limit=1", nil, http.StatusOK, authHeaders(ownerToken))
	inventoryItems := asSlice(t, inventoryResp["workspaces"])
	if len(inventoryItems) != 1 {
		t.Fatalf("expected 1 workspace in inventory, got %d", len(inventoryItems))
	}
	inventoryWorkspace := asMap(t, asMap(t, inventoryItems[0])["workspace"])
	if got := asString(t, inventoryWorkspace["last_successful_backup_at"]); got == "" {
		t.Fatal("expected inventory to expose last_successful_backup_at after scheduled backup")
	}

	pastRetention := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	if _, err := env.workspace.DB().ExecContext(context.Background(), `UPDATE workspace_backup_runs
		SET retention_expires_at = ?, updated_at = ?
		WHERE id = ?`, pastRetention, time.Now().UTC().Format(time.RFC3339Nano), runID); err != nil {
		t.Fatalf("force backup retention expiry: %v", err)
	}

	if err := env.service.RunBackupMaintenancePass(context.Background()); err != nil {
		t.Fatalf("run backup retention sweep: %v", err)
	}

	if _, err := os.Stat(backupDir); !os.IsNotExist(err) {
		t.Fatalf("expected backup bundle to be pruned, stat err=%v", err)
	}

	row = env.workspace.DB().QueryRowContext(context.Background(), `SELECT pruned_at, prune_failure_reason
		FROM workspace_backup_runs
		WHERE id = ?`, runID)
	var pruneFailureReason sql.NullString
	if err := row.Scan(&prunedAt, &pruneFailureReason); err != nil {
		t.Fatalf("load pruned backup run: %v", err)
	}
	if !prunedAt.Valid || strings.TrimSpace(prunedAt.String) == "" {
		t.Fatal("expected backup run to be marked as pruned")
	}
}

func TestControlPlaneProvisionRestoreFailureAndRetrySemantics(t *testing.T) {
	env := newControlPlaneTestEnv(t, "")
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "owner3@example.com", "Owner Three", "cred-owner-3")
	ownerToken := asString(t, ownerSession["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "retry-org",
		"display_name": "Retry Org",
		"plan_tier":    "team",
	}, http.StatusCreated, authHeaders(ownerToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	createWorkspaceResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces", workspaceCreatePayload(t, organizationID, "retry", "Retry Workspace", "us-central1", "standard"), http.StatusCreated, authHeaders(ownerToken))
	workspace := asMap(t, createWorkspaceResp["workspace"])
	workspaceID := asString(t, workspace["id"])
	deploymentRoot := asString(t, workspace["deployment_root"])
	if deploymentRoot == "" {
		t.Fatal("expected deployment_root in workspace response")
	}

	coreWorkspaceRoot := filepath.Join(deploymentRoot, "workspace")
	_, _, cleanup := startCoreServerForTest(t, coreWorkspaceRoot)
	defer cleanup()

	repoRoot := findRepoRoot(t)
	backupDir := filepath.Join(t.TempDir(), "backup")
	runScript(t, filepath.Join(repoRoot, "scripts", "hosted", "backup-workspace.sh"), "--instance-root", deploymentRoot, "--output-dir", backupDir)
	cleanup()

	badBackupDir := filepath.Join(t.TempDir(), "backup-bad")
	copyDir(t, backupDir, badBackupDir)
	tamperManifest(t, filepath.Join(badBackupDir, "manifest.env"))

	restoreFailureResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces/"+workspaceID+"/restore", map[string]any{
		"backup_dir": badBackupDir,
	}, http.StatusOK, authHeaders(ownerToken))
	restoreFailureJob := asMap(t, restoreFailureResp["provisioning_job"])
	if got := asString(t, restoreFailureJob["status"]); got != "failed" {
		t.Fatalf("expected failed restore response job, got %q", got)
	}

	failedJobsResp := requestJSON(t, http.MethodGet, env.server.URL+"/provisioning/jobs?workspace_id="+url.QueryEscape(workspaceID), nil, http.StatusOK, authHeaders(ownerToken))
	failedJobs := asSlice(t, failedJobsResp["jobs"])
	if len(failedJobs) != 2 {
		t.Fatalf("expected 2 jobs after failed restore, got %d", len(failedJobs))
	}
	restoreJob := asMap(t, failedJobs[1])
	if got := asString(t, restoreJob["status"]); got != "failed" {
		t.Fatalf("expected failed restore job, got %q", got)
	}
	if got := asString(t, restoreJob["failure_reason"]); got == "" {
		t.Fatal("expected failure_reason for failed restore job")
	}
	if got := asString(t, restoreJob["stderr_tail"]); got == "" {
		t.Fatal("expected stderr_tail for failed restore job")
	}

	requestJSON(t, http.MethodPost, env.server.URL+"/workspaces/"+workspaceID+"/restore", map[string]any{
		"backup_dir": backupDir,
	}, http.StatusOK, authHeaders(ownerToken))

	retryJobsResp := requestJSON(t, http.MethodGet, env.server.URL+"/provisioning/jobs?workspace_id="+url.QueryEscape(workspaceID), nil, http.StatusOK, authHeaders(ownerToken))
	retryJobs := asSlice(t, retryJobsResp["jobs"])
	if len(retryJobs) != 3 {
		t.Fatalf("expected 3 jobs after retry, got %d", len(retryJobs))
	}
	lastJob := asMap(t, retryJobs[2])
	t.Logf("retry job payload: %#v", lastJob)
	if got := asString(t, lastJob["status"]); got != "succeeded" {
		t.Fatalf("expected succeeded retry job, got %q", got)
	}
}

func TestControlPlaneProvisioningFailureIsDurableAndRetryable(t *testing.T) {
	failingScriptsDir := failingProvisionScriptsDir(t)
	env := newControlPlaneTestEnvWithScripts(t, "", failingScriptsDir)
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "owner4@example.com", "Owner Four", "cred-owner-4")
	ownerToken := asString(t, ownerSession["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "broken-org",
		"display_name": "Broken Org",
		"plan_tier":    "team",
	}, http.StatusCreated, authHeaders(ownerToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	provisionFailureResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces", workspaceCreatePayload(t, organizationID, "broken", "Broken Workspace", "us-central1", "standard"), http.StatusCreated, authHeaders(ownerToken))
	provisionFailureJob := asMap(t, provisionFailureResp["provisioning_job"])
	if got := asString(t, provisionFailureJob["status"]); got != "failed" {
		t.Fatalf("expected failed provisioning response job, got %q", got)
	}

	jobsResp := requestJSON(t, http.MethodGet, env.server.URL+"/provisioning/jobs?organization_id="+url.QueryEscape(organizationID), nil, http.StatusOK, authHeaders(ownerToken))
	jobs := asSlice(t, jobsResp["jobs"])
	if len(jobs) != 1 {
		t.Fatalf("expected 1 failed provisioning job, got %d", len(jobs))
	}
	job := asMap(t, jobs[0])
	if got := asString(t, job["status"]); got != "failed" {
		t.Fatalf("expected failed job, got %q", got)
	}
	if got := asString(t, job["failure_reason"]); got == "" {
		t.Fatal("expected failure_reason for failed provisioning job")
	}
	if got := asString(t, job["stderr_tail"]); got == "" {
		t.Fatal("expected stderr_tail for failed provisioning job")
	}
}

func TestControlPlaneFleetHeartbeatBackupUpgradeDrillAndInventory(t *testing.T) {
	env := newControlPlaneTestEnv(t, "")
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "fleet@example.com", "Fleet Owner", "cred-fleet-owner")
	ownerToken := asString(t, ownerSession["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "fleet-org",
		"display_name": "Fleet Org",
		"plan_tier":    "team",
	}, http.StatusCreated, authHeaders(ownerToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	serviceIdentityID := "svc_fleet_workspace"
	_, serviceIdentityPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate workspace service key: %v", err)
	}
	serviceIdentityKey, err := controlplaneauth.NewWorkspaceServiceIdentity(controlplaneauth.WorkspaceServiceIdentityConfig{
		ID:         serviceIdentityID,
		PrivateKey: serviceIdentityPrivateKey,
	})
	if err != nil {
		t.Fatalf("new workspace service identity: %v", err)
	}

	createWorkspaceResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces", map[string]any{
		"organization_id":             organizationID,
		"slug":                        "fleet",
		"display_name":                "Fleet Workspace",
		"region":                      "us-central1",
		"workspace_tier":              "standard",
		"service_identity_id":         serviceIdentityID,
		"service_identity_public_key": serviceIdentityKey.PublicKeyBase64(),
	}, http.StatusCreated, authHeaders(ownerToken))
	workspace := asMap(t, createWorkspaceResp["workspace"])
	workspaceID := asString(t, workspace["id"])
	deploymentRoot := asString(t, workspace["deployment_root"])
	if deploymentRoot == "" {
		t.Fatal("expected deployment_root in workspace response")
	}

	heartbeatToken, _, err := serviceIdentityKey.SignClientAssertion(controlplaneauth.WorkspaceServiceAssertionAudience, 10*time.Minute, map[string]any{
		"workspace_id":    workspaceID,
		"organization_id": organizationID,
		"purpose":         "heartbeat",
	})
	if err != nil {
		t.Fatalf("sign heartbeat token: %v", err)
	}

	heartbeatResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces/"+workspaceID+"/heartbeat", map[string]any{
		"version": "hosted-instance/v1",
		"build":   "build-001",
		"health_summary": map[string]any{
			"status": "healthy",
			"checks": map[string]any{
				"db": "ok",
			},
		},
		"projection_maintenance_summary": map[string]any{
			"status": "current",
		},
		"usage_summary": map[string]any{
			"usage": map[string]any{
				"blob_bytes":              int64(2 * 1024 * 1024 * 1024),
				"blob_objects":            3,
				"artifact_count":          1,
				"document_count":          0,
				"document_revision_count": 0,
			},
			"quota": map[string]any{
				"max_blob_bytes":         1024 * 1024,
				"max_artifacts":          100,
				"max_documents":          100,
				"max_document_revisions": 500,
				"max_upload_bytes":       1024 * 1024,
			},
			"generated_at": time.Now().UTC().Format(time.RFC3339Nano),
		},
		"last_successful_backup_at": "2026-03-21T00:00:00Z",
	}, http.StatusOK, map[string]string{"Authorization": "Bearer " + heartbeatToken})
	heartbeatWorkspace := asMap(t, heartbeatResp["workspace"])
	if got := asString(t, heartbeatWorkspace["heartbeat_version"]); got != "hosted-instance/v1" {
		t.Fatalf("expected heartbeat version to be recorded, got %q", got)
	}
	if got := asString(t, heartbeatWorkspace["desired_version"]); got != "hosted-instance/v1" {
		t.Fatalf("expected desired_version to start at current version, got %q", got)
	}
	usageResp := requestJSON(t, http.MethodGet, env.server.URL+"/organizations/"+organizationID+"/usage-summary", nil, http.StatusOK, authHeaders(ownerToken))
	usageSummary := asMap(t, usageResp["summary"])
	usage := asMap(t, usageSummary["usage"])
	if got := int(asFloat(t, usage["storage_gb"])); got != 2 {
		t.Fatalf("expected usage storage_gb=2 after heartbeat metering, got %d", got)
	}

	coreWorkspaceRoot := filepath.Join(deploymentRoot, "workspace")
	_, _, cleanup := startCoreServerForTest(t, coreWorkspaceRoot)
	defer cleanup()

	backupResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces/"+workspaceID+"/backups", map[string]any{
		"schedule_name":  "nightly",
		"retention_days": 30,
	}, http.StatusOK, authHeaders(ownerToken))
	backupJob := asMap(t, backupResp["provisioning_job"])
	if got := asString(t, backupJob["status"]); got != "succeeded" {
		t.Fatalf("expected backup job to succeed, got %q", got)
	}
	backupDir := asString(t, asMap(t, backupJob["result"])["backup_dir"])
	if backupDir == "" {
		t.Fatal("expected backup_dir in backup job result")
	}

	upgradeResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces/"+workspaceID+"/upgrade", map[string]any{
		"desired_version": "hosted-instance/v2",
	}, http.StatusOK, authHeaders(ownerToken))
	upgradeWorkspace := asMap(t, upgradeResp["workspace"])
	if got := asString(t, upgradeWorkspace["deployed_version"]); got != "hosted-instance/v2" {
		t.Fatalf("expected deployed_version to update, got %q", got)
	}
	if got := asString(t, upgradeWorkspace["desired_version"]); got != "hosted-instance/v2" {
		t.Fatalf("expected desired_version to update, got %q", got)
	}

	heartbeatToken, _, err = serviceIdentityKey.SignClientAssertion(controlplaneauth.WorkspaceServiceAssertionAudience, 10*time.Minute, map[string]any{
		"workspace_id":    workspaceID,
		"organization_id": organizationID,
		"purpose":         "heartbeat",
	})
	if err != nil {
		t.Fatalf("sign post-upgrade heartbeat token: %v", err)
	}
	postUpgradeHeartbeatResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces/"+workspaceID+"/heartbeat", map[string]any{
		"version": "hosted-instance/v2",
		"build":   "build-002",
		"health_summary": map[string]any{
			"status": "healthy",
			"checks": map[string]any{
				"db": "ok",
			},
		},
		"projection_maintenance_summary": map[string]any{
			"status": "current",
		},
		"usage_summary": map[string]any{
			"usage": map[string]any{
				"blob_bytes":              2048,
				"blob_objects":            4,
				"artifact_count":          2,
				"document_count":          1,
				"document_revision_count": 1,
			},
			"quota": map[string]any{
				"max_blob_bytes":         1024 * 1024,
				"max_artifacts":          100,
				"max_documents":          100,
				"max_document_revisions": 500,
				"max_upload_bytes":       1024 * 1024,
			},
			"generated_at": time.Now().UTC().Format(time.RFC3339Nano),
		},
		"last_successful_backup_at": time.Now().UTC().Format(time.RFC3339Nano),
	}, http.StatusOK, map[string]string{"Authorization": "Bearer " + heartbeatToken})
	postUpgradeHeartbeatWorkspace := asMap(t, postUpgradeHeartbeatResp["workspace"])
	if got := asString(t, postUpgradeHeartbeatWorkspace["heartbeat_version"]); got != "hosted-instance/v2" {
		t.Fatalf("expected post-upgrade heartbeat version, got %q", got)
	}

	restoreDrillResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces/"+workspaceID+"/restore-drills", map[string]any{
		"backup_dir": backupDir,
	}, http.StatusOK, authHeaders(ownerToken))
	restoreDrillJob := asMap(t, restoreDrillResp["provisioning_job"])
	if got := asString(t, restoreDrillJob["status"]); got != "succeeded" {
		t.Fatalf("expected restore drill to succeed, got %q", got)
	}

	badBackupDir := filepath.Join(t.TempDir(), "backup-bad")
	copyDir(t, backupDir, badBackupDir)
	tamperManifest(t, filepath.Join(badBackupDir, "manifest.env"))

	failedDrillResp := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces/"+workspaceID+"/restore-drills", map[string]any{
		"backup_dir": badBackupDir,
	}, http.StatusOK, authHeaders(ownerToken))
	failedDrillJob := asMap(t, failedDrillResp["provisioning_job"])
	if got := asString(t, failedDrillJob["status"]); got != "failed" {
		t.Fatalf("expected failed restore drill, got %q", got)
	}
	if got := asString(t, failedDrillJob["failure_reason"]); got == "" {
		t.Fatal("expected failure_reason for failed restore drill")
	}

	inventoryResp := requestJSON(t, http.MethodGet, env.server.URL+"/organizations/"+organizationID+"/workspace-inventory?limit=1", nil, http.StatusOK, authHeaders(ownerToken))
	if got := asString(t, inventoryResp["organization_id"]); got != organizationID {
		t.Fatalf("expected inventory organization_id=%q, got %q", organizationID, got)
	}
	inventoryWorkspaces := asSlice(t, inventoryResp["workspaces"])
	if len(inventoryWorkspaces) != 1 {
		t.Fatalf("expected 1 workspace in inventory, got %d", len(inventoryWorkspaces))
	}
	inventoryItem := asMap(t, inventoryWorkspaces[0])
	inventoryWorkspace := asMap(t, inventoryItem["workspace"])
	if got := asString(t, inventoryWorkspace["heartbeat_version"]); got != "hosted-instance/v2" {
		t.Fatalf("expected inventory heartbeat_version=%q, got %q", "hosted-instance/v2", got)
	}
	if got := asString(t, inventoryWorkspace["desired_version"]); got != "hosted-instance/v2" {
		t.Fatalf("expected inventory desired_version=%q, got %q", "hosted-instance/v2", got)
	}
	if got := int(asFloat(t, inventoryItem["open_failed_job_count"])); got < 1 {
		t.Fatalf("expected at least one open failed job, got %d", got)
	}
	failedJobs := asSlice(t, inventoryItem["open_failed_jobs"])
	if len(failedJobs) == 0 {
		t.Fatal("expected open failed jobs in inventory")
	}
	if got := asString(t, asMap(t, failedJobs[0])["kind"]); got != "workspace_restore_drill" {
		t.Fatalf("expected restore drill in failed jobs, got %q", got)
	}
}

func TestControlPlaneCeremoniesAndSessionsSurviveRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "control-plane")

	env1 := newControlPlaneTestEnv(t, root)
	startRegistrationResp := requestJSON(t, http.MethodPost, env1.server.URL+"/account/passkeys/registrations/start", map[string]any{
		"email":        "restart@example.com",
		"display_name": "Restart User",
	}, http.StatusOK, originHeaders())
	registrationSessionID := asString(t, startRegistrationResp["registration_session_id"])
	registrationOptions := asMap(t, startRegistrationResp["public_key_options"])
	registrationUser := asMap(t, registrationOptions["user"])
	env1.Close()

	env2 := newControlPlaneTestEnv(t, root)
	finishRegistrationResp := requestJSON(t, http.MethodPost, env2.server.URL+"/account/passkeys/registrations/finish", map[string]any{
		"registration_session_id": registrationSessionID,
		"credential":              registrationCredential(t, registrationOptions, "cred-restart", asString(t, registrationUser["id"])),
	}, http.StatusOK, originHeaders())
	session := asMap(t, finishRegistrationResp["session"])
	accessToken := asString(t, session["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env2.server.URL+"/organizations", map[string]any{
		"slug":         "restart-org",
		"display_name": "Restart Org",
		"plan_tier":    "starter",
	}, http.StatusCreated, authHeaders(accessToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	startLoginResp := requestJSON(t, http.MethodPost, env2.server.URL+"/account/sessions/start", map[string]any{
		"email": "restart@example.com",
	}, http.StatusOK, originHeaders())
	loginSessionID := asString(t, startLoginResp["session_id"])
	loginOptions := asMap(t, startLoginResp["public_key_options"])
	env2.Close()

	env3 := newControlPlaneTestEnv(t, root)
	organizationsResp := requestJSON(t, http.MethodGet, env3.server.URL+"/organizations", nil, http.StatusOK, authHeaders(accessToken))
	if got := len(asSlice(t, organizationsResp["organizations"])); got != 1 {
		t.Fatalf("expected persisted session to access 1 organization, got %d", got)
	}

	finishLoginResp := requestJSON(t, http.MethodPost, env3.server.URL+"/account/sessions/finish", map[string]any{
		"session_id": loginSessionID,
		"credential": assertionCredential(t, loginOptions, "cred-restart"),
	}, http.StatusOK, originHeaders())
	restartedLoginSession := asMap(t, finishLoginResp["session"])
	restartedToken := asString(t, restartedLoginSession["access_token"])

	organizationResp := requestJSON(t, http.MethodGet, env3.server.URL+"/organizations/"+organizationID, nil, http.StatusOK, authHeaders(restartedToken))
	if got := asString(t, asMap(t, organizationResp["organization"])["id"]); got != organizationID {
		t.Fatalf("expected organization %q after restarted login, got %q", organizationID, got)
	}
	env3.Close()
}

func TestControlPlanePasskeyRegistrationConsumesCeremoniesAtomically(t *testing.T) {
	env := newControlPlaneTestEnv(t, "")
	defer env.Close()

	startResp := requestJSON(t, http.MethodPost, env.server.URL+"/account/passkeys/registrations/start", map[string]any{
		"email":        "registration-race@example.com",
		"display_name": "Registration Race",
	}, http.StatusOK, originHeaders())
	registrationSessionID := asString(t, startResp["registration_session_id"])
	options := asMap(t, startResp["public_key_options"])
	user := asMap(t, options["user"])

	type result struct {
		status int
		body   string
		err    error
	}

	inputs := []map[string]any{
		{
			"registration_session_id": registrationSessionID,
			"credential": registrationCredential(
				t,
				options,
				"cred-registration-race-a",
				asString(t, user["id"]),
			),
		},
		{
			"registration_session_id": registrationSessionID,
			"credential": registrationCredential(
				t,
				options,
				"cred-registration-race-b",
				asString(t, user["id"]),
			),
		},
	}

	results := make(chan result, len(inputs))
	client := &http.Client{Timeout: 10 * time.Second}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, payload := range inputs {
		wg.Add(1)
		go func(payload map[string]any) {
			defer wg.Done()
			<-start

			rawPayload, err := json.Marshal(payload)
			if err != nil {
				results <- result{err: err}
				return
			}
			req, err := http.NewRequest(http.MethodPost, env.server.URL+"/account/passkeys/registrations/finish", bytes.NewReader(rawPayload))
			if err != nil {
				results <- result{err: err}
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", testOrigin)

			resp, err := client.Do(req)
			if err != nil {
				results <- result{err: err}
				return
			}
			defer resp.Body.Close()

			rawBody, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				results <- result{err: readErr}
				return
			}
			results <- result{status: resp.StatusCode, body: string(rawBody)}
		}(payload)
	}

	close(start)
	wg.Wait()
	close(results)

	var okCount int
	var expiredCount int
	for result := range results {
		if result.err != nil {
			t.Fatalf("finish registration request failed: %v", result.err)
		}
		switch result.status {
		case http.StatusOK:
			okCount++
		case http.StatusUnauthorized:
			expiredCount++
			var payload map[string]any
			if err := json.Unmarshal([]byte(result.body), &payload); err != nil {
				t.Fatalf("decode registration race payload: %v", err)
			}
			if got := asString(t, asMap(t, payload["error"])["code"]); got != "session_expired" {
				t.Fatalf("expected session_expired, got %q with body %s", got, result.body)
			}
		default:
			t.Fatalf("unexpected status %d body=%s", result.status, result.body)
		}
	}
	if okCount != 1 || expiredCount != 1 {
		t.Fatalf("expected one successful registration and one expired response, got ok=%d expired=%d", okCount, expiredCount)
	}

	var credentialCount int
	if err := env.workspace.DB().QueryRowContext(context.Background(), `SELECT COUNT(1) FROM passkey_credentials`).Scan(&credentialCount); err != nil {
		t.Fatalf("count passkey credentials: %v", err)
	}
	if credentialCount != 1 {
		t.Fatalf("expected exactly one persisted passkey credential, got %d", credentialCount)
	}
}

func TestControlPlaneAccountSessionsConsumeCeremoniesAtomically(t *testing.T) {
	env := newControlPlaneTestEnv(t, "")
	defer env.Close()

	_, _ = registerAccount(t, env, "login-race@example.com", "Login Race", "cred-login-race")

	startResp := requestJSON(t, http.MethodPost, env.server.URL+"/account/sessions/start", map[string]any{
		"email": "login-race@example.com",
	}, http.StatusOK, originHeaders())
	sessionID := asString(t, startResp["session_id"])
	options := asMap(t, startResp["public_key_options"])

	type result struct {
		status int
		body   string
		err    error
	}

	results := make(chan result, 2)
	client := &http.Client{Timeout: 10 * time.Second}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			rawPayload, err := json.Marshal(map[string]any{
				"session_id": sessionID,
				"credential": assertionCredential(t, options, "cred-login-race"),
			})
			if err != nil {
				results <- result{err: err}
				return
			}
			req, err := http.NewRequest(http.MethodPost, env.server.URL+"/account/sessions/finish", bytes.NewReader(rawPayload))
			if err != nil {
				results <- result{err: err}
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", testOrigin)

			resp, err := client.Do(req)
			if err != nil {
				results <- result{err: err}
				return
			}
			defer resp.Body.Close()

			rawBody, readErr := io.ReadAll(resp.Body)
			if readErr != nil {
				results <- result{err: readErr}
				return
			}
			results <- result{status: resp.StatusCode, body: string(rawBody)}
		}()
	}

	close(start)
	wg.Wait()
	close(results)

	var okCount int
	var expiredCount int
	for result := range results {
		if result.err != nil {
			t.Fatalf("finish session request failed: %v", result.err)
		}
		switch result.status {
		case http.StatusOK:
			okCount++
		case http.StatusUnauthorized:
			expiredCount++
			var payload map[string]any
			if err := json.Unmarshal([]byte(result.body), &payload); err != nil {
				t.Fatalf("decode login race payload: %v", err)
			}
			if got := asString(t, asMap(t, payload["error"])["code"]); got != "session_expired" {
				t.Fatalf("expected session_expired, got %q with body %s", got, result.body)
			}
		default:
			t.Fatalf("unexpected status %d body=%s", result.status, result.body)
		}
	}
	if okCount != 1 || expiredCount != 1 {
		t.Fatalf("expected one successful login and one expired response, got ok=%d expired=%d", okCount, expiredCount)
	}
}

func TestControlPlaneRegistrationLeavesInvitePendingWithoutInviteToken(t *testing.T) {
	env := newControlPlaneTestEnv(t, "")
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "owner-invite@example.com", "Owner Invite", "cred-owner-invite")
	ownerToken := asString(t, ownerSession["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "invite-gate",
		"display_name": "Invite Gate",
		"plan_tier":    "team",
	}, http.StatusCreated, authHeaders(ownerToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	createInviteResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations/"+organizationID+"/invites", map[string]any{
		"email": "member-invite@example.com",
		"role":  "member",
	}, http.StatusCreated, authHeaders(ownerToken))
	if got := asString(t, createInviteResp["invite_url"]); got == "" {
		t.Fatal("expected invite_url in create invite response")
	}

	_, memberSession := registerAccount(t, env, "member-invite@example.com", "Member Invite", "cred-member-invite")
	memberToken := asString(t, memberSession["access_token"])

	memberOrganizations := requestJSON(t, http.MethodGet, env.server.URL+"/organizations", nil, http.StatusOK, authHeaders(memberToken))
	got := 0
	if items, ok := memberOrganizations["organizations"]; ok && items != nil {
		got = len(asSlice(t, items))
	}
	if got != 0 {
		t.Fatalf("expected registration without invite token to keep invite pending, got %d organizations", got)
	}
}

func TestControlPlaneLoginAcceptsPendingInviteWithInviteToken(t *testing.T) {
	env := newControlPlaneTestEnv(t, "")
	defer env.Close()

	_, ownerSession := registerAccount(t, env, "owner-login-invite@example.com", "Owner Login Invite", "cred-owner-login-invite")
	ownerToken := asString(t, ownerSession["access_token"])

	_, memberSession := registerAccount(t, env, "member-login-invite@example.com", "Member Login Invite", "cred-member-login-invite")
	memberToken := asString(t, memberSession["access_token"])

	createOrganizationResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations", map[string]any{
		"slug":         "login-invite",
		"display_name": "Login Invite",
		"plan_tier":    "team",
	}, http.StatusCreated, authHeaders(ownerToken))
	organizationID := asString(t, asMap(t, createOrganizationResp["organization"])["id"])

	createInviteResp := requestJSON(t, http.MethodPost, env.server.URL+"/organizations/"+organizationID+"/invites", map[string]any{
		"email": "member-login-invite@example.com",
		"role":  "member",
	}, http.StatusCreated, authHeaders(ownerToken))
	inviteToken := inviteTokenFromURL(t, asString(t, createInviteResp["invite_url"]))

	memberOrganizations := requestJSON(t, http.MethodGet, env.server.URL+"/organizations", nil, http.StatusOK, authHeaders(memberToken))
	got := 0
	if items, ok := memberOrganizations["organizations"]; ok && items != nil {
		got = len(asSlice(t, items))
	}
	if got != 0 {
		t.Fatalf("expected invited account to have no organizations before using invite token, got %d", got)
	}

	loginSession := loginAccount(t, env, "member-login-invite@example.com", "cred-member-login-invite", inviteToken)
	memberToken = asString(t, loginSession["access_token"])

	memberOrganizations = requestJSON(t, http.MethodGet, env.server.URL+"/organizations", nil, http.StatusOK, authHeaders(memberToken))
	if got := len(asSlice(t, memberOrganizations["organizations"])); got != 1 {
		t.Fatalf("expected login with invite token to accept membership, got %d organizations", got)
	}
}

func newControlPlaneTestEnv(t *testing.T, root string) *controlPlaneTestEnv {
	return newControlPlaneTestEnvWithConfig(t, root, "", nil)
}

func newControlPlaneTestEnvWithScripts(t *testing.T, root string, hostedScriptsDir string) *controlPlaneTestEnv {
	return newControlPlaneTestEnvWithConfig(t, root, hostedScriptsDir, nil)
}

func newControlPlaneTestEnvWithConfig(t *testing.T, root string, hostedScriptsDir string, mutate func(*controlplane.Config)) *controlPlaneTestEnv {
	t.Helper()

	if root == "" {
		root = t.TempDir()
	}
	workspace, err := cpstorage.InitializeWorkspace(context.Background(), root)
	if err != nil {
		t.Fatalf("initialize control-plane workspace: %v", err)
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate workspace grant key: %v", err)
	}
	grantIssuer := "https://control-plane.example.test"
	grantAudience := "oar-core"
	grantSigner, err := controlplaneauth.NewWorkspaceHumanGrantSigner(controlplaneauth.WorkspaceHumanGrantSignerConfig{
		Issuer:     grantIssuer,
		Audience:   grantAudience,
		PrivateKey: privateKey,
	})
	if err != nil {
		t.Fatalf("new workspace grant signer: %v", err)
	}
	serviceConfig := controlplane.Config{
		WorkspaceGrantSigner: grantSigner,
		HostedScriptsDir:     hostedScriptsDir,
	}
	if mutate != nil {
		mutate(&serviceConfig)
	}
	service := controlplane.NewService(workspace, serviceConfig)
	server := httptest.NewServer(NewHandler(service, Config{
		HealthCheck: workspace.Ping,
		WebAuthnConfig: WebAuthnConfig{
			RPID:     testRPID,
			RPOrigin: testOrigin,
		},
	}))

	return &controlPlaneTestEnv{
		t:               t,
		root:            root,
		workspace:       workspace,
		service:         service,
		server:          server,
		grantIssuer:     grantIssuer,
		grantAudience:   grantAudience,
		grantPublicKey:  publicKey,
		grantPrivateKey: privateKey,
	}
}

func (env *controlPlaneTestEnv) Close() {
	if env == nil {
		return
	}
	if env.server != nil {
		env.server.Close()
		env.server = nil
	}
	if env.workspace != nil {
		if err := env.workspace.Close(); err != nil {
			env.t.Fatalf("close workspace: %v", err)
		}
		env.workspace = nil
	}
}

func registerAccount(t *testing.T, env *controlPlaneTestEnv, email string, displayName string, credentialID string, inviteToken ...string) (map[string]any, map[string]any) {
	t.Helper()

	startResp := requestJSON(t, http.MethodPost, env.server.URL+"/account/passkeys/registrations/start", map[string]any{
		"email":        email,
		"display_name": displayName,
	}, http.StatusOK, originHeaders())
	options := asMap(t, startResp["public_key_options"])
	user := asMap(t, options["user"])

	finishPayload := map[string]any{
		"registration_session_id": asString(t, startResp["registration_session_id"]),
		"credential":              registrationCredential(t, options, credentialID, asString(t, user["id"])),
	}
	if len(inviteToken) > 0 && strings.TrimSpace(inviteToken[0]) != "" {
		finishPayload["invite_token"] = inviteToken[0]
	}

	finishResp := requestJSON(t, http.MethodPost, env.server.URL+"/account/passkeys/registrations/finish", finishPayload, http.StatusOK, originHeaders())

	return asMap(t, finishResp["account"]), asMap(t, finishResp["session"])
}

func loginAccount(t *testing.T, env *controlPlaneTestEnv, email string, credentialID string, inviteToken ...string) map[string]any {
	t.Helper()

	startResp := requestJSON(t, http.MethodPost, env.server.URL+"/account/sessions/start", map[string]any{
		"email": email,
	}, http.StatusOK, originHeaders())
	options := asMap(t, startResp["public_key_options"])

	finishPayload := map[string]any{
		"session_id": asString(t, startResp["session_id"]),
		"credential": assertionCredential(t, options, credentialID),
	}
	if len(inviteToken) > 0 && strings.TrimSpace(inviteToken[0]) != "" {
		finishPayload["invite_token"] = inviteToken[0]
	}

	finishResp := requestJSON(t, http.MethodPost, env.server.URL+"/account/sessions/finish", finishPayload, http.StatusOK, originHeaders())

	return asMap(t, finishResp["session"])
}

func inviteTokenFromURL(t *testing.T, inviteURL string) string {
	t.Helper()

	parsed, err := url.Parse(inviteURL)
	if err != nil {
		t.Fatalf("parse invite url: %v", err)
	}
	token := strings.TrimPrefix(parsed.Path, "/invites/")
	if token == "" || token == parsed.Path {
		t.Fatalf("expected invite token path in %q", inviteURL)
	}
	return token
}

func registrationCredential(t *testing.T, options map[string]any, credentialID string, userHandle string) map[string]any {
	t.Helper()
	return map[string]any{
		"id":    credentialID,
		"rawId": credentialID,
		"type":  "public-key",
		"response": map[string]any{
			"clientDataJSON": encodeClientData(t, asString(t, options["challenge"]), "webauthn.create"),
			"userHandle":     userHandle,
		},
	}
}

func assertionCredential(t *testing.T, options map[string]any, credentialID string) map[string]any {
	t.Helper()
	return map[string]any{
		"id":    credentialID,
		"rawId": credentialID,
		"type":  "public-key",
		"response": map[string]any{
			"clientDataJSON": encodeClientData(t, asString(t, options["challenge"]), "webauthn.get"),
		},
	}
}

func encodeClientData(t *testing.T, challenge string, credentialType string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"challenge": challenge,
		"origin":    testOrigin,
		"type":      credentialType,
	})
	if err != nil {
		t.Fatalf("marshal client data: %v", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func requestJSON(t *testing.T, method string, endpoint string, payload any, wantStatus int, headers map[string]string) map[string]any {
	t.Helper()

	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequest(method, endpoint, body)
	if err != nil {
		t.Fatalf("new %s request: %v", method, err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, endpoint, err)
	}
	defer resp.Body.Close()

	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s: expected status %d, got %d: %s", method, endpoint, wantStatus, resp.StatusCode, string(rawBody))
	}

	if len(rawBody) == 0 {
		return map[string]any{}
	}
	var decoded map[string]any
	if err := json.Unmarshal(rawBody, &decoded); err != nil {
		t.Fatalf("decode response JSON: %v\nbody=%s", err, string(rawBody))
	}
	return decoded
}

func authHeaders(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func workspaceCreatePayload(t *testing.T, organizationID string, slug string, displayName string, region string, workspaceTier string) map[string]any {
	t.Helper()

	serviceIdentity := newWorkspaceServiceIdentity(t, "svc_"+strings.ReplaceAll(slug, "-", "_"))
	return map[string]any{
		"organization_id":             organizationID,
		"slug":                        slug,
		"display_name":                displayName,
		"region":                      region,
		"workspace_tier":              workspaceTier,
		"service_identity_id":         serviceIdentity.ID(),
		"service_identity_public_key": serviceIdentity.PublicKeyBase64(),
	}
}

func newWorkspaceServiceIdentity(t *testing.T, id string) *controlplaneauth.WorkspaceServiceIdentity {
	t.Helper()

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate workspace service key: %v", err)
	}
	serviceIdentity, err := controlplaneauth.NewWorkspaceServiceIdentity(controlplaneauth.WorkspaceServiceIdentityConfig{
		ID:         id,
		PrivateKey: privateKey,
	})
	if err != nil {
		t.Fatalf("new workspace service identity: %v", err)
	}
	return serviceIdentity
}

func originHeaders() map[string]string {
	return map[string]string{"Origin": testOrigin}
}

func asMap(t *testing.T, raw any) map[string]any {
	t.Helper()
	value, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %#v", raw)
	}
	return value
}

func asSlice(t *testing.T, raw any) []any {
	t.Helper()
	value, ok := raw.([]any)
	if !ok {
		t.Fatalf("expected []any, got %#v", raw)
	}
	return value
}

func asString(t *testing.T, raw any) string {
	t.Helper()
	value, ok := raw.(string)
	if !ok {
		t.Fatalf("expected string, got %#v", raw)
	}
	return value
}

func asBool(t *testing.T, raw any) bool {
	t.Helper()
	value, ok := raw.(bool)
	if !ok {
		t.Fatalf("expected bool, got %#v", raw)
	}
	return value
}

func asFloat(t *testing.T, raw any) float64 {
	t.Helper()
	value, ok := raw.(float64)
	if !ok {
		t.Fatalf("expected float64, got %#v", raw)
	}
	return value
}

func envVarFromFile(t *testing.T, path string, key string) string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read env file %s: %v", path, err)
	}
	prefix := key + "="
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	t.Fatalf("env key %s not found in %s", key, path)
	return ""
}

func startCoreServerForTest(t *testing.T, workspaceRoot string) (*exec.Cmd, string, func()) {
	t.Helper()

	repoRoot := findRepoRoot(t)
	schemaPath := filepath.Join(repoRoot, "contracts", "oar-schema.yaml")
	if _, err := os.Stat(schemaPath); err != nil {
		t.Fatalf("schema path not found: %v", err)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("allocate listener: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	cmd := exec.Command("go", "run", "./cmd/oar-core", "--listen-addr", addr, "--workspace-root", workspaceRoot, "--schema-path", schemaPath)
	cmd.Dir = filepath.Join(repoRoot, "core")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdoutStderr := &bytes.Buffer{}
	cmd.Stdout = stdoutStderr
	cmd.Stderr = stdoutStderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start core server: %v", err)
	}

	baseURL := "http://" + addr
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(baseURL + "/readyz")
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	resp, err := http.Get(baseURL + "/readyz")
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil && resp.Body != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		if cmd.Process != nil {
			terminateProcessGroup(cmd)
		}
		t.Fatalf("core server did not become ready; logs:\n%s", stdoutStderr.String())
	}
	if resp != nil && resp.Body != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	cleanup := func() {
		terminateProcessGroup(cmd)
	}
	return cmd, baseURL, cleanup
}

func terminateProcessGroup(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		candidate := filepath.Join(dir, "contracts", "oar-schema.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repository root not found from %s", dir)
		}
		dir = parent
	}
}

func runScript(t *testing.T, scriptPath string, args ...string) {
	t.Helper()

	cmd := exec.Command(scriptPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s failed: %v\n%s", scriptPath, err, string(output))
	}
}

func copyDir(t *testing.T, sourceDir string, targetDir string) {
	t.Helper()

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("create target dir: %v", err)
	}
	err := filepath.WalkDir(sourceDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		targetPath := filepath.Join(targetDir, rel)
		if d.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		perm := info.Mode().Perm()
		if perm == 0 {
			perm = 0o644
		}
		if perm&0o111 != 0 {
			perm = 0o755
		}
		return os.WriteFile(targetPath, data, perm)
	})
	if err != nil {
		t.Fatalf("copy directory: %v", err)
	}
}

func tamperManifest(t *testing.T, manifestPath string) {
	t.Helper()

	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	updated := strings.ReplaceAll(string(raw), "FORMAT_VERSION=hosted-ops-backup/v1", "FORMAT_VERSION=hosted-ops-backup/v999")
	if updated == string(raw) {
		t.Fatalf("expected to update manifest format version in %s", manifestPath)
	}
	if err := os.WriteFile(manifestPath, []byte(updated), 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func failingProvisionScriptsDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "provision-workspace.sh")
	content := []byte("#!/usr/bin/env bash\nset -euo pipefail\nprintf 'simulated provision failure\\n' >&2\nexit 23\n")
	if err := os.WriteFile(scriptPath, content, 0o755); err != nil {
		t.Fatalf("write failing provision script: %v", err)
	}
	return dir
}
