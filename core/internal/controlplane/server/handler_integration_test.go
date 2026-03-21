package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"organization-autorunner-core/internal/controlplane"
	cpstorage "organization-autorunner-core/internal/controlplane/storage"
)

const (
	testRPID   = "control.oar.test"
	testOrigin = "https://control.oar.test"
)

type controlPlaneTestEnv struct {
	t         *testing.T
	root      string
	workspace *cpstorage.Workspace
	server    *httptest.Server
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
	if inviteURL := asString(t, createInviteResp["invite_url"]); inviteURL == "" {
		t.Fatal("expected invite_url in create invite response")
	}

	_, memberSession := registerAccount(t, env, "member@example.com", "Member", "cred-member")
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

	firstWorkspaceCreate := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces", map[string]any{
		"organization_id": organizationID,
		"slug":            "ops",
		"display_name":    "Ops",
		"region":          "us-central1",
		"workspace_tier":  "standard",
	}, http.StatusCreated, authHeaders(ownerToken))
	firstWorkspace := asMap(t, firstWorkspaceCreate["workspace"])
	firstJob := asMap(t, firstWorkspaceCreate["provisioning_job"])

	secondWorkspaceCreate := requestJSON(t, http.MethodPost, env.server.URL+"/workspaces", map[string]any{
		"organization_id": organizationID,
		"slug":            "eng",
		"display_name":    "Engineering",
		"region":          "us-east1",
		"workspace_tier":  "plus",
	}, http.StatusCreated, authHeaders(ownerToken))
	secondWorkspace := asMap(t, secondWorkspaceCreate["workspace"])
	secondJob := asMap(t, secondWorkspaceCreate["provisioning_job"])

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

func newControlPlaneTestEnv(t *testing.T, root string) *controlPlaneTestEnv {
	t.Helper()

	if root == "" {
		root = t.TempDir()
	}
	workspace, err := cpstorage.InitializeWorkspace(context.Background(), root)
	if err != nil {
		t.Fatalf("initialize control-plane workspace: %v", err)
	}

	service := controlplane.NewService(workspace, controlplane.Config{})
	server := httptest.NewServer(NewHandler(service, Config{
		HealthCheck: workspace.Ping,
		WebAuthnConfig: WebAuthnConfig{
			RPID:     testRPID,
			RPOrigin: testOrigin,
		},
	}))

	return &controlPlaneTestEnv{
		t:         t,
		root:      root,
		workspace: workspace,
		server:    server,
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

func registerAccount(t *testing.T, env *controlPlaneTestEnv, email string, displayName string, credentialID string) (map[string]any, map[string]any) {
	t.Helper()

	startResp := requestJSON(t, http.MethodPost, env.server.URL+"/account/passkeys/registrations/start", map[string]any{
		"email":        email,
		"display_name": displayName,
	}, http.StatusOK, originHeaders())
	options := asMap(t, startResp["public_key_options"])
	user := asMap(t, options["user"])

	finishResp := requestJSON(t, http.MethodPost, env.server.URL+"/account/passkeys/registrations/finish", map[string]any{
		"registration_session_id": asString(t, startResp["registration_session_id"]),
		"credential":              registrationCredential(t, options, credentialID, asString(t, user["id"])),
	}, http.StatusOK, originHeaders())

	return asMap(t, finishResp["account"]), asMap(t, finishResp["session"])
}

func loginAccount(t *testing.T, env *controlPlaneTestEnv, email string, credentialID string) map[string]any {
	t.Helper()

	startResp := requestJSON(t, http.MethodPost, env.server.URL+"/account/sessions/start", map[string]any{
		"email": email,
	}, http.StatusOK, originHeaders())
	options := asMap(t, startResp["public_key_options"])

	finishResp := requestJSON(t, http.MethodPost, env.server.URL+"/account/sessions/finish", map[string]any{
		"session_id": asString(t, startResp["session_id"]),
		"credential": assertionCredential(t, options, credentialID),
	}, http.StatusOK, originHeaders())

	return asMap(t, finishResp["session"])
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
