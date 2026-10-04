package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/hostidentity"
	"agent-nexus-cli/internal/httpclient"
	"agent-nexus-cli/internal/profile"
	"agent-nexus-cli/internal/workspaceconfig"
)

func (a *App) hostCall(ctx context.Context, cfg config.Resolved, method, path string, body any, headers map[string]string) (map[string]any, error) {
	client, err := httpclient.New(cfg)
	if err != nil {
		return nil, err
	}
	var data []byte
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
	}
	resp, err := client.RawCall(ctx, httpclient.RawRequest{Method: method, Path: path, Body: data, Headers: headers})
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindNetwork, "request_failed", "host request failed", err)
	}
	if resp.StatusCode >= 400 {
		return nil, errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
	}
	var parsed map[string]any
	if err := json.Unmarshal(resp.Body, &parsed); err != nil {
		return nil, err
	}
	return parsed, nil
}

func writeEnrollmentInstructions(progress io.Writer, start map[string]any) {
	code := anyString(start["user_code"])
	verify := anyString(start["verification_url"])
	parsed, err := url.Parse(verify)
	if err == nil && parsed != nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Hostname() != "" {
		fmt.Fprintf(progress, "enrollment user_code=%s verification_url=%s\nnext open %s\n", code, verify, verify)
		return
	}
	fmt.Fprintf(progress, "enrollment user_code=%s\ninstruction open Access → Hosts in the workspace web UI and approve this code\n", code)
}

func discoveredAdapters() []string {
	known := map[string]string{"claude": "claude", "codex": "codex", "cursor": "cursor-agent", "omp": "omp"}
	found := []string{}
	if report, err := loadRuntimeIdentity(os.Getenv); err == nil {
		return availableRuntimeAdapters(report)
	}
	if path, err := exec.LookPath("agentctl"); err == nil {
		if out, err := exec.Command(path, "doctor", "--output", "json").Output(); err == nil {
			var doc map[string]any
			if json.Unmarshal(out, &doc) == nil {
				for _, row := range asSlice(asMap(doc["result"])["adapters"]) {
					item := asMap(row)
					if anyString(item["status"]) != "ready" {
						continue
					}
					name := agentctlName(anyString(item["name"]))
					if agentNamePattern.MatchString(name) {
						found = append(found, name)
					}
				}
			}
		}
	}
	if len(found) == 0 {
		for name, bin := range known {
			if _, err := exec.LookPath(bin); err == nil {
				found = append(found, name)
			}
		}
	}
	sort.Strings(found)
	return found
}

func hostSlug(hostname string) string {
	slug := strings.ToLower(hostname)
	slug = regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if len(slug) > 32 {
		slug = strings.Trim(slug[:32], "-")
	}
	if slug == "" {
		slug = "host"
	}
	return slug
}

type localAdoption struct {
	Name    string
	Path    string
	KeyPath string
	AgentID string
	KeyID   string
}

func (a *App) adoptionCandidates(cfg config.Resolved, exclude map[string]bool) ([]localAdoption, error) {
	configDir, err := a.configDir(cfg)
	if err != nil {
		return nil, err
	}
	names, err := profile.ListAgentsAt(configDir)
	if err != nil {
		return nil, err
	}
	result := []localAdoption{}
	for _, name := range names {
		if exclude[name] {
			continue
		}
		path := profile.ProfilePathAt(configDir, name)
		p, ok, err := profile.Load(path)
		if err != nil {
			return nil, err
		}
		if !ok || p.AgentID == "" || p.KeyID == "" {
			continue
		}
		if strings.TrimRight(p.BaseURL, "/") != strings.TrimRight(cfg.BaseURL, "/") {
			continue
		}
		agentName := name
		if !agentNamePattern.MatchString(agentName) {
			return nil, errnorm.Usage("invalid_adoption_name", "profile "+name+" cannot be adopted as an agent name")
		}
		keyPath := p.PrivateKeyPath
		if keyPath == "" {
			keyPath = profile.KeyPathAt(configDir, name)
		}
		result = append(result, localAdoption{Name: agentName, Path: path, KeyPath: keyPath, AgentID: p.AgentID, KeyID: p.KeyID})
	}
	return result, nil
}

func adoptionProofs(candidates []localAdoption, nonce, public string) ([]any, error) {
	proofs := []any{}
	for _, p := range candidates {
		key, err := profile.LoadPrivateKey(p.KeyPath)
		if err != nil {
			return nil, err
		}
		msg := "anx-host-adopt|" + nonce + "|" + public + "|" + p.AgentID + "|" + p.Name
		proofs = append(proofs, map[string]any{"agent_id": p.AgentID, "key_id": p.KeyID, "agent_name": p.Name, "signature": base64.StdEncoding.EncodeToString(ed25519.Sign(key, []byte(msg)))})
	}
	return proofs, nil
}

func (a *App) runHost(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, string, error) {
	if len(args) == 0 {
		return nil, "host", errnorm.Usage("subcommand_required", "use anx host enroll|status|list|token|exclude|include|enrollments|tokens|revoke")
	}
	switch args[0] {
	case "enrollments":
		return a.runHostEnrollments(ctx, args[1:], cfg)
	case "tokens":
		return a.runHostTokens(ctx, args[1:], cfg)
	case "revoke":
		target, err := adminTarget(args[1:])
		if err != nil {
			return nil, "host revoke", err
		}
		r, e := a.invokeRawJSON(ctx, cfg, "host revoke", "DELETE", "/hosts/"+url.PathEscape(target), nil)
		return r, "host revoke", e
	case "discover":
		if len(args) != 1 {
			return nil, "host discover", errnorm.Usage("invalid_args", "host discover takes no arguments")
		}
		return a.discoverRuntime(), "host discover", nil
	case "enroll":
		r, e := a.hostEnroll(ctx, args[1:], cfg)
		name := "host enroll"
		for _, arg := range args[1:] {
			if arg == "--plan" {
				name = "host enroll --plan"
			}
		}
		return r, name, e
	case "token":
		r, e := a.hostToken(ctx, args[1:], cfg)
		return r, "host token", e
	case "bridge":
		return a.runHostBridge(ctx, args[1:], cfg)
	case "status", "list":
		if len(args) > 1 {
			return nil, "host " + args[0], errnorm.Usage("invalid_args", "unexpected arguments")
		}
		auth, e := a.resolveHostAgent(ctx, cfg)
		if e != nil {
			return nil, "host " + args[0], e
		}
		path := "/hosts"
		if args[0] == "status" {
			path = "/hosts/" + url.PathEscape(auth.HostID)
		}
		r, e := a.invokeRawJSON(ctx, auth, "host "+args[0], "GET", path, nil)
		return r, "host " + args[0], e
	case "exclude", "include":
		r, e := a.hostExclusion(ctx, args[0], args[1:], cfg)
		return r, "host " + args[0], e
	default:
		return nil, "host", errnorm.Usage("unknown_subcommand", "unknown host command")
	}
}

func (a *App) hostEnroll(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("host enroll")
	var name, token trackedString
	var excludes trackedStrings
	var plan, tokenStdin bool
	fs.Var(&name, "name", "Host slug")
	fs.Var(&token, "token", "Headless enrollment token")
	fs.Var(&excludes, "exclude", "Profile to leave standalone")
	fs.BoolVar(&tokenStdin, "token-stdin", false, "Read the headless enrollment token from stdin")
	fs.BoolVar(&plan, "plan", false, "Show adoption plan")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) != 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected arguments")
	}
	if tokenStdin {
		if token.set || plan {
			return nil, errnorm.Usage("invalid_flags", "--token-stdin cannot be combined with --token or --plan")
		}
		if a.StdinIsTTY != nil && a.StdinIsTTY() {
			return nil, errnorm.Usage("invalid_args", "--token-stdin requires piped input")
		}
		raw, err := io.ReadAll(io.LimitReader(a.Stdin, 4097))
		if err != nil || len(raw) > 4096 || strings.TrimSpace(string(raw)) == "" {
			return nil, errnorm.Usage("invalid_args", "stdin must contain one enrollment token")
		}
		token.value = strings.TrimSpace(string(raw))
		token.set = true
	}
	hostname, _ := os.Hostname()
	slug := name.value
	if slug == "" {
		slug = hostSlug(hostname)
	}
	if len(slug) > 32 || !regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`).MatchString(slug) {
		return nil, errnorm.Usage("invalid_host_slug", "invalid host slug")
	}
	excluded := map[string]bool{}
	for _, v := range excludes.values {
		excluded[v] = true
	}
	candidates, err := a.adoptionCandidates(cfg, excluded)
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, p := range candidates {
		names = append(names, p.Name)
	}
	adapters := discoveredAdapters()
	if plan {
		return &commandResult{Data: map[string]any{"host_slug": slug, "adopt": names, "exclude": excludes.values, "discovered_adapters": adapters}}, nil
	}
	configDir, err := a.configDir(cfg)
	if err != nil {
		return nil, err
	}
	if _, ok, err := hostidentity.LoadAt(configDir, cfg.BaseURL); err != nil {
		return nil, err
	} else if ok {
		return nil, errnorm.Local("host_already_enrolled", "this workspace already has an enrolled host")
	}
	// The handshake is optional on minimal cores; URL hashing keeps the local
	// workspace directory stable until the server exposes a workspace ID.
	handshake, _ := a.hostCall(ctx, cfg, "GET", "/meta/handshake", nil, nil)
	workspace := anyString(handshake["workspace_id"])
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	public := base64.StdEncoding.EncodeToString(pub)
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return nil, err
	}
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	proofs, err := adoptionProofs(candidates, nonce, public)
	if err != nil {
		return nil, err
	}
	osUser := "unknown"
	if u, e := user.Current(); e == nil {
		osUser = u.Username
	}
	request := map[string]any{"public_key": public, "requested_slug": slug, "os_user": osUser, "hostname": hostname, "discovered_adapters": adapters, "request_nonce": nonce, "adoptions": proofs}
	var response map[string]any
	if token.set {
		request["enrollment_token"] = token.value
		request["signature"] = base64.StdEncoding.EncodeToString(ed25519.Sign(key, []byte("anx-host-headless-enroll|"+nonce+"|"+slug+"|"+public)))
		response, err = a.hostCall(ctx, cfg, "POST", "/auth/hosts/enrollments/headless", request, nil)
	} else {
		start, e := a.hostCall(ctx, cfg, "POST", "/auth/hosts/enrollments", request, nil)
		if e != nil {
			return nil, e
		}
		id, poll := anyString(start["enrollment_id"]), anyString(start["poll_token"])
		var progress io.Writer = a.Stdout
		if cfg.JSON {
			progress = a.Stderr
		}
		writeEnrollmentInstructions(progress, start)
		interval := time.Duration(hostInt(start["poll_interval_seconds"])) * time.Second
		if interval < time.Second {
			interval = time.Second
		}
		expires, _ := time.Parse(time.RFC3339, anyString(start["expires_at"]))
		for {
			if !expires.IsZero() && time.Now().After(expires) {
				return nil, errnorm.Local("enrollment_expired", "host enrollment expired")
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(interval):
			}
			polled, e := a.hostCall(ctx, cfg, "GET", "/auth/hosts/enrollments/"+url.PathEscape(id), nil, map[string]string{"X-ANX-Enrollment-Token": poll})
			if e != nil {
				return nil, e
			}
			status := anyString(asMap(polled["enrollment"])["status"])
			if status == "denied" {
				return nil, errnorm.Local("enrollment_denied", "host enrollment denied")
			}
			if status == "approved" {
				break
			}
			if v := hostInt(polled["poll_interval_seconds"]); v > 0 {
				interval = time.Duration(v) * time.Second
			}
		}
		response, err = a.hostCall(ctx, cfg, "POST", "/auth/hosts/enrollments/"+url.PathEscape(id)+"/complete", map[string]any{"poll_token": poll, "signature": base64.StdEncoding.EncodeToString(ed25519.Sign(key, []byte("anx-host-enroll-complete|"+id+"|"+poll)))}, nil)
	}
	if err != nil {
		return nil, err
	}
	hostData := asMap(response["host"])
	host := hostidentity.Host{ID: anyString(hostData["id"]), KeyID: anyString(hostData["key_id"]), Slug: anyString(hostData["slug"]), WorkspaceID: workspace, WorkspaceSlug: anyString(handshake["workspace_slug"]), BaseURL: cfg.BaseURL}
	if host.ID == "" || host.KeyID == "" {
		return nil, fmt.Errorf("invalid host enrollment response")
	}
	if err := hostidentity.SaveAt(configDir, host, key); err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "host_persist_failed", "host enrolled but local key could not be saved", err)
	}
	for _, p := range candidates {
		if err := os.Remove(p.Path); err != nil {
			return nil, err
		}
		rel, relErr := filepath.Rel(profile.KeysDirAt(configDir), p.KeyPath)
		if relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			if err := os.Remove(p.KeyPath); err != nil && !os.IsNotExist(err) {
				return nil, err
			}
		}
	}
	alias := ""
	if err := workspaceconfig.Update(configDir, func(c *workspaceconfig.Catalog) error {
		alias = c.File.AddAlias(workspaceconfig.DerivedAlias(host), cfg.BaseURL)
		return nil
	}); err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "workspace_alias_persist_failed", "host enrolled; workspace alias could not be saved (inspect anx config workspaces)", err)
	}
	data := map[string]any{"host": hostData, "adopted": names, "excluded": excludes.values, "workspace_alias": alias, "base_url": cfg.BaseURL}
	if cfg.ConfigDir != "" {
		data["config_dir"] = cfg.ConfigDir
	}
	return &commandResult{Data: data}, nil
}

func hostInt(v any) int {
	if n, ok := v.(float64); ok {
		return int(n)
	}
	return 0
}

func (a *App) hostToken(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("host token")
	var name trackedString
	fs.Var(&name, "as", "Derived agent name")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) != 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected arguments")
	}
	if name.set {
		cfg.As = name.value
		cfg.IdentitySource = "flag:--as"
	}
	auth, err := a.resolveHostAgent(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &commandResult{Data: map[string]any{"token": auth.AccessToken, "expires_at": auth.AccessTokenExpiresAt, "agent": map[string]any{"id": auth.AgentID, "handle": auth.Username}}}, nil
}

func (a *App) hostExclusion(ctx context.Context, verb string, args []string, cfg config.Resolved) (*commandResult, error) {
	if len(args) != 1 || !agentNamePattern.MatchString(args[0]) {
		return nil, errnorm.Usage("invalid_args", "expected one agent name")
	}
	host, err := a.resolvedHost(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.Sources["base_url"] == "default" {
		cfg.BaseURL = host.BaseURL
	}
	key, err := hostidentity.Key(host)
	if err != nil {
		return nil, err
	}
	var auth config.Resolved
	for _, reader := range []string{"generic", "claude", "codex", "cursor", "omp"} {
		readCfg := cfg
		readCfg.As = reader
		readCfg.IdentitySource = "host:reader"
		auth, err = a.resolveHostAgent(ctx, readCfg)
		if err == nil {
			break
		}
	}
	if err != nil {
		return nil, err
	}
	current, err := a.invokeRawJSON(ctx, auth, "host status", "GET", "/hosts/"+url.PathEscape(host.ID), nil)
	if err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for _, v := range asSlice(asMap(commandResultBody(current)["host"])["excluded_names"]) {
		names[anyString(v)] = true
	}
	if verb == "exclude" {
		names[args[0]] = true
	} else {
		delete(names, args[0])
	}
	sorted := []string{}
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)
	body, _ := json.Marshal(map[string]any{"excluded_names": sorted})
	signed := time.Now().UTC().Format(time.RFC3339Nano)
	sum := sha256.Sum256(body)
	msg := "anx-host-patch|" + host.ID + "|" + signed + "|" + base64.RawURLEncoding.EncodeToString(sum[:])
	bare := cfg
	bare.AccessToken = ""
	client, err := httpclient.New(bare)
	if err != nil {
		return nil, err
	}
	resp, err := client.RawCall(ctx, httpclient.RawRequest{Method: "PATCH", Path: "/hosts/" + url.PathEscape(host.ID), Body: body, Headers: map[string]string{"X-ANX-Host-Key-Id": host.KeyID, "X-ANX-Host-Signed-At": signed, "X-ANX-Host-Signature": base64.StdEncoding.EncodeToString(ed25519.Sign(key, []byte(msg)))}})
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, errnorm.FromHTTPFailure(resp.StatusCode, resp.Body)
	}
	var result map[string]any
	if err := json.Unmarshal(resp.Body, &result); err != nil {
		return nil, err
	}
	_ = os.Remove(filepath.Join(filepath.Dir(host.PrivateKeyPath), "token-"+args[0]+".json"))
	return &commandResult{Data: result}, nil
}

func (a *App) runHostDoctor(ctx context.Context, cfg config.Resolved) (*commandResult, error) {
	checks := []doctorCheck{{Name: "workspace_resolution", OK: true, Status: "pass", Message: cfg.BaseURL + " via " + cfg.Sources["base_url"]}}
	add := func(name string, ok bool, message string) {
		checks = append(checks, doctorCheck{Name: name, OK: ok, Message: message})
	}
	host, err := a.resolvedHost(cfg)
	add("host_enrollment", err == nil, func() string {
		if err != nil {
			return err.Error()
		}
		return host.ID
	}())
	if err == nil {
		_, keyErr := hostidentity.Key(host)
		add("host_key_permissions", keyErr == nil, func() string {
			if keyErr != nil {
				return keyErr.Error()
			}
			return "owner-only host key"
		}())
	} else {
		add("host_key_permissions", false, "enroll host first")
	}
	name, source, identityErr := a.identityName(cfg)
	add("identity_resolution", identityErr == nil, func() string {
		if identityErr != nil {
			return identityErr.Error()
		}
		return name + " via " + source
	}())
	_, agentctlErr := exec.LookPath("agentctl")
	add("agentctl_presence", agentctlErr == nil, func() string {
		if agentctlErr != nil {
			return "agentctl not on PATH"
		}
		return "agentctl on PATH"
	}())
	if home, homeErr := a.skillHome(""); homeErr == nil {
		if skillsConfigDir, dirErr := resolveSkillsConfigDir(home, cfg.ConfigDir); dirErr == nil {
			preferences, _, prefErr := readSkillsSyncConfig(skillsConfigDir)
			if prefErr == nil {
				roles := []string{"participant"}
				if preferences.PMEnabled {
					roles = append(roles, "pm")
				}
				targets := detectSkillHarnesses(home, a.Getenv, a.skillsLookPath())
				states, stateErr := inspectDetectedSkillStates(targets, roles)
				if stateErr == nil {
					for _, state := range states {
						ok := state.State == "current" || state.State == "missing" || state.State == "deferred"
						checks = append(checks, doctorCheck{Name: "skill_" + state.Harness + "_" + state.Role, OK: ok, Status: state.State, Message: state.Path})
					}
				} else {
					checks = append(checks, doctorCheck{Name: "skills_status", OK: false, Status: "warn", Message: stateErr.Error()})
				}
			} else {
				checks = append(checks, doctorCheck{Name: "skills_status", OK: false, Status: "warn", Message: "ANX skill preferences could not be read: " + prefErr.Error()})
			}
		}
	}
	client, clientErr := httpclient.New(cfg)
	if clientErr == nil {
		resp, callErr := client.RawCall(ctx, httpclient.RawRequest{Method: "GET", Path: "/readyz"})
		add("core_health", callErr == nil && resp.StatusCode == 200, func() string {
			if callErr != nil {
				return callErr.Error()
			}
			return fmt.Sprintf("HTTP %d", resp.StatusCode)
		}())
	} else {
		add("core_health", false, clientErr.Error())
	}
	var versionErr *errnorm.Error
	if clientErr == nil {
		versionErr = cliVersionDoctorCheck(ctx, client, func(check doctorCheck) {
			checks = append(checks, check)
		})
	} else {
		checks = append(checks, doctorCheck{Name: "cli_version", OK: false, Status: "fail", Message: "core client unavailable"})
	}
	data := map[string]any{"base_url": cfg.BaseURL, "source": cfg.Sources["base_url"], "checks": checks}
	if versionErr != nil {
		details, _ := versionErr.Details.(map[string]any)
		if details == nil {
			details = map[string]any{}
		}
		details["base_url"] = cfg.BaseURL
		details["source"] = cfg.Sources["base_url"]
		details["checks"] = checks
		versionErr.Details = details
		return nil, versionErr
	}
	return &commandResult{Data: data}, nil
}

// cliVersionDoctorCheck fails when this CLI is below handshake min_cli_version
// and warns when it is below recommended_cli_version. /meta/handshake is
// exempt from the server's own outdated rejection, so doctor can still read it.
func cliVersionDoctorCheck(ctx context.Context, client *httpclient.Client, add func(doctorCheck)) *errnorm.Error {
	resp, err := client.RawCall(ctx, httpclient.RawRequest{Method: "GET", Path: "/meta/handshake"})
	if err != nil {
		add(doctorCheck{Name: "cli_version", OK: false, Status: "fail", Message: "handshake unreachable: " + err.Error()})
		return nil
	}
	if resp.StatusCode != 200 {
		add(doctorCheck{Name: "cli_version", OK: false, Status: "fail", Message: fmt.Sprintf("handshake HTTP %d", resp.StatusCode)})
		return nil
	}
	var body map[string]any
	if err = json.Unmarshal(resp.Body, &body); err != nil {
		add(doctorCheck{Name: "cli_version", OK: false, Status: "fail", Message: "handshake response is not JSON"})
		return nil
	}
	minVersion := strings.TrimSpace(anyString(body["min_cli_version"]))
	recommended := strings.TrimSpace(anyString(body["recommended_cli_version"]))
	current := normalizeReleaseTag(httpclient.CLIVersion)
	if minVersion == "" {
		add(doctorCheck{Name: "cli_version", OK: true, Status: "pass", Message: "core did not advertise a minimum CLI version"})
		return nil
	}
	belowMin, cmpErr := compareSemanticVersions(current, minVersion)
	if cmpErr != nil {
		add(doctorCheck{Name: "cli_version", OK: false, Status: "fail", Message: "cannot compare CLI version to " + minVersion})
		return nil
	}
	target := recommended
	if target == "" {
		target = minVersion
	}
	tag := normalizeReleaseTag(target)
	if belowMin < 0 {
		message := fmt.Sprintf("CLI %s is below minimum %s", current, normalizeReleaseTag(minVersion))
		add(doctorCheck{Name: "cli_version", OK: false, Status: "fail", Message: message, RecommendedCLIVersion: tag})
		outdated := errnorm.Local("cli_outdated", message+"; run anx update --version "+tag)
		outdated.Hint = "Run `anx update --version " + tag + "`."
		return errnorm.WithDetails(outdated, map[string]any{
			"cli_version":             current,
			"min_cli_version":         normalizeReleaseTag(minVersion),
			"recommended_cli_version": tag,
		})
	}
	if recommended != "" {
		belowRecommended, recErr := compareSemanticVersions(current, recommended)
		if recErr == nil && belowRecommended < 0 {
			message := fmt.Sprintf("CLI %s is below recommended %s", current, tag)
			add(doctorCheck{Name: "cli_version", OK: true, Status: "warn", Message: message, RecommendedCLIVersion: tag})
			return nil
		}
	}
	add(doctorCheck{Name: "cli_version", OK: true, Status: "pass", Message: fmt.Sprintf("CLI %s meets minimum %s", current, normalizeReleaseTag(minVersion))})
	return nil
}
