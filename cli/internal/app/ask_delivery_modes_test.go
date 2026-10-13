package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/workspaceconfig"
)

func deliveryTestApp(t *testing.T, serverURL string, env map[string]string) (*App, *bytes.Buffer) {
	t.Helper()
	a, out := dailyTestApp(t, serverURL)
	baseGetenv := a.Getenv
	a.Getenv = func(k string) string {
		if value, ok := env[k]; ok {
			return value
		}
		return baseGetenv(k)
	}
	return a, out
}

func deliveryTestCfg(serverURL string) config.Resolved {
	return config.Resolved{BaseURL: serverURL, ActorID: "actor-1"}
}

func holdAnswerBridgeLock(t *testing.T, a *App, serverURL string) {
	t.Helper()
	unlock, err := lockAskBridge(filepath.Join(mustAskRegistryDir(t, a, serverURL), "bridge.lock"))
	if err != nil {
		t.Fatalf("hold bridge lock: %v", err)
	}
	t.Cleanup(unlock)
}

func mustAskRegistryDir(t *testing.T, a *App, serverURL string) string {
	t.Helper()
	dir, err := a.askRegistryDir(deliveryTestCfg(serverURL))
	if err != nil {
		t.Fatalf("registry dir: %v", err)
	}
	return dir
}

func envelopeResult(t *testing.T, out *bytes.Buffer) map[string]any {
	t.Helper()
	result := asMap(dailyJSON(t, out)["result"])
	if result == nil {
		t.Fatalf("envelope has no result object: %s", out.String())
	}
	return result
}

func TestResolveAskDeliveryMatrix(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()

	decision := func(t *testing.T, env map[string]string, holdLock bool, in askDeliveryInput) (askDeliveryDecision, error) {
		t.Helper()
		a, _ := deliveryTestApp(t, server.URL, env)
		if holdLock {
			holdAnswerBridgeLock(t, a, server.URL)
		}
		return a.resolveAskDelivery(deliveryTestCfg(server.URL), in)
	}

	t.Run("explicit resume requires a command", func(t *testing.T) {
		_, err := decision(t, nil, true, askDeliveryInput{Explicit: "resume", ExplicitSet: true})
		if err == nil || !strings.Contains(err.Error(), "--on-answer") {
			t.Fatalf("expected command guidance, got %v", err)
		}
	})
	t.Run("explicit resume without bridge or continuation errors naming choices", func(t *testing.T) {
		_, err := decision(t, map[string]string{"ANX_RESUME_CMD": "hook"}, false, askDeliveryInput{Explicit: "resume", ExplicitSet: true})
		if err == nil || !strings.Contains(err.Error(), "--delivery batch") {
			t.Fatalf("expected choices, got %v", err)
		}
	})
	t.Run("explicit resume with running bridge delivers", func(t *testing.T) {
		got, err := decision(t, map[string]string{"ANX_RESUME_CMD": "hook"}, true, askDeliveryInput{Explicit: "resume", ExplicitSet: true})
		if err != nil || got.Mode != "resume" || got.Command != "hook" {
			t.Fatalf("decision=%+v err=%v", got, err)
		}
	})
	t.Run("explicit resume with agentctl continuation delivers", func(t *testing.T) {
		got, err := decision(t, map[string]string{"AGENTCTL_EXECUTION_ID": "exec-1"}, false, askDeliveryInput{Explicit: "resume", ExplicitSet: true})
		if err != nil || got.Mode != "resume" || !strings.Contains(got.Command, "agentctl continue") {
			t.Fatalf("decision=%+v err=%v", got, err)
		}
	})
	t.Run("on-answer conflicts with non-resume delivery", func(t *testing.T) {
		_, err := decision(t, nil, false, askDeliveryInput{Explicit: "batch", ExplicitSet: true, OnAnswer: "hook", OnAnswerFlag: true})
		if err == nil || !strings.Contains(err.Error(), "--on-answer") {
			t.Fatalf("expected conflict, got %v", err)
		}
	})
	t.Run("batch ignores resume env with a warning", func(t *testing.T) {
		got, err := decision(t, map[string]string{"ANX_RESUME_CMD": "hook"}, false, askDeliveryInput{Explicit: "batch", ExplicitSet: true})
		if err != nil || got.Mode != "batch" || len(got.Warnings) != 1 {
			t.Fatalf("decision=%+v err=%v", got, err)
		}
	})
	t.Run("none is a valid explicit opt-out", func(t *testing.T) {
		got, err := decision(t, nil, false, askDeliveryInput{Explicit: "none", ExplicitSet: true})
		if err != nil || got.Mode != "none" {
			t.Fatalf("decision=%+v err=%v", got, err)
		}
	})
	t.Run("invalid mode", func(t *testing.T) {
		_, err := decision(t, nil, false, askDeliveryInput{Explicit: "sometimes", ExplicitSet: true})
		if err == nil || !strings.Contains(err.Error(), "await, resume, batch, none") {
			t.Fatalf("expected mode choices, got %v", err)
		}
	})
	t.Run("workspace default resume falls back to batch when host cannot deliver", func(t *testing.T) {
		a, _ := deliveryTestApp(t, server.URL, nil)
		dir, err := a.configDir(deliveryTestCfg(server.URL))
		if err != nil {
			t.Fatal(err)
		}
		if err := setWorkspaceAskDeliveryDefault(dir, "resume"); err != nil {
			t.Fatal(err)
		}
		got, err := a.resolveAskDelivery(deliveryTestCfg(server.URL), askDeliveryInput{AskID: "ask"})
		if err != nil || got.Mode != "batch" || len(got.Warnings) != 1 {
			t.Fatalf("decision=%+v err=%v", got, err)
		}
	})
	t.Run("workspace default batch applies", func(t *testing.T) {
		a, _ := deliveryTestApp(t, server.URL, nil)
		dir, err := a.configDir(deliveryTestCfg(server.URL))
		if err != nil {
			t.Fatal(err)
		}
		if err := setWorkspaceAskDeliveryDefault(dir, "batch"); err != nil {
			t.Fatal(err)
		}
		got, err := a.resolveAskDelivery(deliveryTestCfg(server.URL), askDeliveryInput{})
		if err != nil || got.Mode != "batch" || !strings.Contains(got.Reason, "workspace default") {
			t.Fatalf("decision=%+v err=%v", got, err)
		}
	})
	t.Run("auto default resumes when the bridge runs and a command exists", func(t *testing.T) {
		got, err := decision(t, map[string]string{"ANX_RESUME_CMD": "hook"}, true, askDeliveryInput{})
		if err != nil || got.Mode != "resume" {
			t.Fatalf("decision=%+v err=%v", got, err)
		}
	})
	t.Run("legacy on-answer wiring always registers resume without failing", func(t *testing.T) {
		got, err := decision(t, nil, false, askDeliveryInput{OnAnswer: "hook", OnAnswerFlag: true, AskID: "ask"})
		if err != nil || got.Mode != "resume" || got.Command != "hook" {
			t.Fatalf("decision=%+v err=%v", got, err)
		}
		if !strings.Contains(got.Reason, "durable") {
			t.Fatalf("reason must state durable registration: %q", got.Reason)
		}
	})
	t.Run("resume env wins over workspace default", func(t *testing.T) {
		a, _ := deliveryTestApp(t, server.URL, map[string]string{"ANX_RESUME_CMD": "hook"})
		dir, err := a.configDir(deliveryTestCfg(server.URL))
		if err != nil {
			t.Fatal(err)
		}
		if err := setWorkspaceAskDeliveryDefault(dir, "batch"); err != nil {
			t.Fatal(err)
		}
		got, err := a.resolveAskDelivery(deliveryTestCfg(server.URL), askDeliveryInput{})
		if err != nil || got.Mode != "resume" {
			t.Fatalf("decision=%+v err=%v", got, err)
		}
	})
	t.Run("auto default batches when the bridge runs but nothing can execute", func(t *testing.T) {
		got, err := decision(t, nil, true, askDeliveryInput{})
		if err != nil || got.Mode != "batch" || !strings.Contains(got.Reason, "no resume command is registered") {
			t.Fatalf("decision=%+v err=%v", got, err)
		}
	})
	t.Run("auto default batches on a quiet host", func(t *testing.T) {
		got, err := decision(t, nil, false, askDeliveryInput{})
		if err != nil || got.Mode != "batch" || !strings.Contains(got.Reason, "await --answers") {
			t.Fatalf("decision=%+v err=%v", got, err)
		}
	})
	t.Run("unverifiable lock never selects resume", func(t *testing.T) {
		a, _ := deliveryTestApp(t, server.URL, nil)
		// A config dir beneath a regular file cannot host the registry, so the
		// probe cannot inspect the lock.
		blocked := filepath.Join(t.TempDir(), "blocker.txt")
		if err := os.WriteFile(blocked, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg := deliveryTestCfg(server.URL)
		cfg.ConfigDir = filepath.Join(blocked, "config")
		if got := a.answerBridgeState(cfg); got != answerBridgeUnknown {
			t.Fatalf("probe state %v, want unknown", got)
		}
		decision, err := a.resolveAskDelivery(cfg, askDeliveryInput{OnAnswer: "hook", OnAnswerFlag: true, AskID: "ask"})
		if err != nil || decision.Mode != "resume" {
			t.Fatalf("legacy wiring must still file durably: %+v %v", decision, err)
		}
		if !strings.Contains(decision.Reason, "durable") {
			t.Fatalf("reason must state durable registration: %q", decision.Reason)
		}
		// An explicit resume, though, must not pass on an unverifiable probe.
		_, err = a.resolveAskDelivery(cfg, askDeliveryInput{Explicit: "resume", ExplicitSet: true, OnAnswer: "hook", AskID: "ask"})
		if err == nil || !strings.Contains(err.Error(), "choices") {
			t.Fatalf("explicit resume must fail when the probe is unknown, got %v", err)
		}
	})
	t.Run("auto default resumes under agentctl", func(t *testing.T) {
		got, err := decision(t, map[string]string{"AGENTCTL_EXECUTION_ID": "exec-1"}, false, askDeliveryInput{})
		if err != nil || got.Mode != "resume" {
			t.Fatalf("decision=%+v err=%v", got, err)
		}
	})
}

func TestAskDeliveryFollowUpsAreRunnable(t *testing.T) {
	cases := map[string]string{
		askDeliveryAwait:  "anx await event:ask-1",
		askDeliveryBatch:  "anx await --answers",
		askDeliveryResume: "anx orient",
		askDeliveryNone:   "anx inbox list --status answered",
	}
	for mode, want := range cases {
		if got := askDeliveryFollowUp(mode, "event:ask-1"); got != want {
			t.Fatalf("mode %s follow-up %q, want runnable %q", mode, got, want)
		}
	}
}

func askServer(t *testing.T, eventRef string, captured *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost && r.URL.Path == "/events" {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if captured != nil {
				*captured = body
			}
			fmt.Fprintf(w, `{"event":{"id":%q,"ref":%q,"type":"human_attention_requested"}}`, strings.TrimPrefix(eventRef, "event:"), eventRef)
			return
		}
		if r.URL.Path == "/cards/card:task" {
			fmt.Fprint(w, `{"card":{"ref":"card:task","thread_id":"thread-1","column_key":"in_progress"}}`)
			return
		}
		fmt.Fprint(w, `{}`)
	}))
}

func TestAskResultCarriesDelivery(t *testing.T) {
	var created map[string]any
	server := askServer(t, "event:ask-1", &created)
	defer server.Close()

	a, out := deliveryTestApp(t, server.URL, nil)
	if exit := a.Run([]string{"--json", "--as", "worker", "ask", "Ship now?", "--recommend", "Yes", "--subject-ref", "card:task", "--delivery", "none"}); exit != 0 {
		t.Fatalf("exit=%d output=%s", exit, out.String())
	}
	payload := asMap(asMap(asMap(created["event"])["payload"]))
	if payload["delivery"] != "none" {
		t.Fatalf("payload delivery=%v", payload["delivery"])
	}
	delivery := asMap(envelopeResult(t, out)["delivery"])
	if delivery["mode"] != "none" || anyString(delivery["reason"]) == "" || !strings.Contains(anyString(delivery["next"]), "inbox list --status answered") {
		t.Fatalf("delivery view=%#v", delivery)
	}
}

func TestAskDefaultDeliveryPrintsBatchFollowUp(t *testing.T) {
	var created map[string]any
	server := askServer(t, "event:ask-2", &created)
	defer server.Close()

	a, out := deliveryTestApp(t, server.URL, nil)
	if exit := a.Run([]string{"--json", "--as", "worker", "ask", "Pick a lane?", "--recommend", "Lane 1", "--subject-ref", "card:task"}); exit != 0 {
		t.Fatalf("exit=%d output=%s", exit, out.String())
	}
	payload := asMap(asMap(asMap(created["event"])["payload"]))
	if payload["delivery"] != "batch" {
		t.Fatalf("payload delivery=%v", payload["delivery"])
	}
	delivery := asMap(envelopeResult(t, out)["delivery"])
	if delivery["mode"] != "batch" || anyString(delivery["next"]) != "anx await --answers" {
		t.Fatalf("delivery view=%#v", delivery)
	}
}

func orientServerWithResumeAsk(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/agents/"):
			fmt.Fprint(w, `{"agent":{"id":"agent-1","actor_id":"actor-1","handle":"worker.host"},"open_asks":[{"id":"ask-9","title":"Blocked","delivery":"resume"}],"recent_notes":[]}`)
		case r.URL.Path == "/work":
			fmt.Fprint(w, `{"work":[],"has_more":false}`)
		case r.URL.Path == "/agent-inbox/asks":
			fmt.Fprint(w, `{"items":[],"page_info":{"has_more":false}}`)
		case r.URL.Path == "/agent-notifications":
			fmt.Fprint(w, `{"items":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestOrientDeliveryHealthWarnsWhenNothingDelivers(t *testing.T) {
	server := orientServerWithResumeAsk(t)
	defer server.Close()

	a, out := deliveryTestApp(t, server.URL, nil)
	if exit := a.Run([]string{"--json", "--as", "worker", "orient"}); exit != 0 {
		t.Fatalf("exit=%d output=%s", exit, out.String())
	}
	result := envelopeResult(t, out)
	health := asMap(result["delivery_health"])
	if got := asSlice(health["open_resume_asks"]); len(got) != 1 || anyString(got[0]) != "event:ask-9" {
		t.Fatalf("health=%#v", health)
	}
	if health["bridge_running"] != false || anyString(health["warning"]) == "" {
		t.Fatalf("health=%#v", health)
	}
}

func TestOrientDeliveryHealthQuietWhenBridgeRuns(t *testing.T) {
	server := orientServerWithResumeAsk(t)
	defer server.Close()

	a, out := deliveryTestApp(t, server.URL, nil)
	holdAnswerBridgeLock(t, a, server.URL)
	if exit := a.Run([]string{"--json", "--as", "worker", "orient"}); exit != 0 {
		t.Fatalf("exit=%d output=%s", exit, out.String())
	}
	result := envelopeResult(t, out)
	health := asMap(result["delivery_health"])
	if health["bridge_running"] != true || anyString(health["warning"]) != "" {
		t.Fatalf("health=%#v", health)
	}
}

func TestConfigAskDeliverySetAndShow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()

	a, out := deliveryTestApp(t, server.URL, nil)
	if exit := a.Run([]string{"--json", "--as", "worker", "config", "ask-delivery", "resume"}); exit != 0 {
		t.Fatalf("set exit=%d output=%s", exit, out.String())
	}
	dir, err := a.configDir(deliveryTestCfg(server.URL))
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := workspaceconfig.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.File.AskDelivery != "resume" {
		t.Fatalf("stored=%q", catalog.File.AskDelivery)
	}
	out.Reset()
	if exit := a.Run([]string{"--json", "--as", "worker", "config", "ask-delivery"}); exit != 0 {
		t.Fatalf("get exit=%d output=%s", exit, out.String())
	}
	if got := envelopeResult(t, out)["ask_delivery"]; got != "resume" {
		t.Fatalf("readback=%v", got)
	}
	out.Reset()
	if exit := a.Run([]string{"--json", "--as", "worker", "config", "ask-delivery", "sometimes"}); exit == 0 {
		t.Fatal("invalid mode accepted")
	}
}

func TestBridgeUnitInstallDryRunAndIdempotentWrite(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unit install targets launchd/systemd hosts")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()

	a, out := deliveryTestApp(t, server.URL, nil)
	if exit := a.Run([]string{"--json", "--as", "worker", "bridge", "install", "--dry-run"}); exit != 0 {
		t.Fatalf("dry-run exit=%d output=%s", exit, out.String())
	}
	plan := envelopeResult(t, out)
	if plan["dry_run"] != true || anyString(plan["agent"]) != "worker" {
		t.Fatalf("plan=%#v", plan)
	}
	content := anyString(plan["content"])
	if runtime.GOOS == "darwin" {
		if !strings.Contains(content, "<string>bridge</string>") || !strings.Contains(content, "<string>worker</string>") || !strings.Contains(content, "KeepAlive") {
			t.Fatalf("plist missing bridge invocation: %s", content)
		}
	} else if !strings.Contains(content, "bridge run --as worker") || !strings.Contains(content, "WantedBy=default.target") {
		t.Fatalf("unit missing bridge invocation: %s", content)
	}
	home, _ := a.UserHomeDir()
	if !strings.HasPrefix(anyString(plan["unit_path"]), home) {
		t.Fatalf("unit path not anchored to HOME: %s (home %s)", plan["unit_path"], home)
	}
}

func TestBridgeUnitInstallWritesOnceThenReportsCurrent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unit install targets launchd/systemd hosts")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()

	a, _ := deliveryTestApp(t, server.URL, map[string]string{"ANX_BASE_URL": server.URL})
	var wrote []string
	var commands [][]string
	originalMkdir, originalWrite, originalRead, originalRun := bridgeMkdirAll, bridgeWriteFile, bridgeReadFile, bridgeCommandRun
	originalLstat, originalChmod, originalRename, originalStat := bridgeLstat, bridgeChmod, bridgeRename, bridgeStat
	t.Cleanup(func() {
		bridgeMkdirAll, bridgeWriteFile, bridgeReadFile, bridgeCommandRun = originalMkdir, originalWrite, originalRead, originalRun
		bridgeLstat, bridgeChmod, bridgeRename, bridgeStat = originalLstat, originalChmod, originalRename, originalStat
	})
	bridgeMkdirAll = func(path string, perm os.FileMode) error { return nil }
	bridgeReadFile = func(path string) ([]byte, error) { return nil, os.ErrNotExist }
	bridgeLstat = func(path string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	bridgeStat = func(path string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	bridgeChmod = func(path string, perm os.FileMode) error { return nil }
	bridgeWriteFile = func(path string, data []byte, perm os.FileMode) error {
		wrote = append(wrote, string(data))
		return nil
	}
	bridgeRename = func(from, to string) error {
		content := wrote[len(wrote)-1]
		bridgeReadFile = func(string) ([]byte, error) { return []byte(content), nil }
		return nil
	}
	bridgeCommandRun = func(ctx context.Context, name string, args ...string) (string, string, error) {
		commands = append(commands, append([]string{name}, args...))
		return "", "", nil
	}
	cfg := deliveryTestCfg(server.URL)
	first, err := a.runBridgeUnitInstall(context.Background(), []string{"--as", "worker"}, cfg)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if len(wrote) != 1 || first.Data.(map[string]any)["started"] != true {
		t.Fatalf("first install wrote=%d data=%#v", len(wrote), first.Data)
	}
	assertBridgeUnitActivateCommands(t, commands, false)
	commands = nil
	second, err := a.runBridgeUnitInstall(context.Background(), []string{"--as", "worker"}, cfg)
	if err != nil {
		t.Fatalf("reinstall: %v", err)
	}
	data := second.Data.(map[string]any)
	if len(wrote) != 1 || data["already_current"] != true {
		t.Fatalf("second install must be idempotent: wrote=%d data=%#v", len(wrote), data)
	}
	assertBridgeUnitActivateCommands(t, commands, true)
}

func TestBridgeUnitInstallReloadsWhenUnitChanges(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unit install targets launchd/systemd hosts")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()

	a, _ := deliveryTestApp(t, server.URL, map[string]string{"ANX_BASE_URL": server.URL})
	var wrote []string
	var commands [][]string
	originalMkdir, originalWrite, originalRead, originalRun := bridgeMkdirAll, bridgeWriteFile, bridgeReadFile, bridgeCommandRun
	originalLstat, originalChmod, originalRename, originalStat := bridgeLstat, bridgeChmod, bridgeRename, bridgeStat
	t.Cleanup(func() {
		bridgeMkdirAll, bridgeWriteFile, bridgeReadFile, bridgeCommandRun = originalMkdir, originalWrite, originalRead, originalRun
		bridgeLstat, bridgeChmod, bridgeRename, bridgeStat = originalLstat, originalChmod, originalRename, originalStat
	})
	bridgeMkdirAll = func(path string, perm os.FileMode) error { return nil }
	bridgeReadFile = func(path string) ([]byte, error) { return []byte("stale unit"), nil }
	bridgeLstat = func(path string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	bridgeStat = func(path string) (os.FileInfo, error) { return fakeFileInfo{mode: 0o644}, nil }
	bridgeChmod = func(path string, perm os.FileMode) error { return nil }
	bridgeRename = func(from, to string) error { return nil }
	bridgeWriteFile = func(path string, data []byte, perm os.FileMode) error {
		wrote = append(wrote, string(data))
		bridgeReadFile = func(string) ([]byte, error) { return data, nil }
		return nil
	}
	bridgeCommandRun = func(ctx context.Context, name string, args ...string) (string, string, error) {
		commands = append(commands, append([]string{name}, args...))
		if len(args) > 0 && args[0] == "bootout" {
			return "", "Boot-out failed: 3: No such process", fmt.Errorf("exit status 1")
		}
		return "", "", nil
	}
	result, err := a.runBridgeUnitInstall(context.Background(), []string{"--as", "worker"}, deliveryTestCfg(server.URL))
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	data := result.Data.(map[string]any)
	if len(wrote) != 1 || data["already_current"] != false || data["started"] != true {
		t.Fatalf("changed unit must be rewritten and started: wrote=%d data=%#v", len(wrote), data)
	}
	assertBridgeUnitActivateCommands(t, commands, false)
}

func assertBridgeUnitActivateCommands(t *testing.T, commands [][]string, alreadyCurrent bool) {
	t.Helper()
	joined := make([]string, len(commands))
	for i, command := range commands {
		joined[i] = strings.Join(command, " ")
	}
	switch runtime.GOOS {
	case "darwin":
		if len(commands) < 2 || !strings.Contains(joined[0], "bootout") || !strings.Contains(joined[1], "bootstrap") {
			t.Fatalf("expected bootout then bootstrap, got %q", joined)
		}
	case "linux":
		if len(commands) < 2 || !strings.Contains(joined[0], "daemon-reload") || !strings.Contains(joined[1], "enable") || !strings.Contains(joined[1], "--now") {
			t.Fatalf("expected daemon-reload then enable --now, got %q", joined)
		}
		hasRestart := false
		for _, command := range joined {
			if strings.Contains(command, "restart") {
				hasRestart = true
			}
		}
		if alreadyCurrent && hasRestart {
			t.Fatalf("unchanged reinstall should not restart: %q", joined)
		}
		if !alreadyCurrent && !hasRestart {
			t.Fatalf("changed or first install should restart the unit: %q", joined)
		}
	default:
		if len(commands) == 0 {
			t.Fatalf("expected activation commands, got none")
		}
	}
}

func TestBridgeUnitPlanContentPerPlatform(t *testing.T) {
	plan, err := answerBridgeUnitPlan("/srv/anx-it", "worker.host", "/usr/local/bin/anx", "/srv/anx-it/.config/anx/logs/bridge.log", map[string]string{"ANX_BASE_URL": "https://core.example.test"})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "darwin" {
		if !strings.Contains(plan.Content, "<string>worker.host</string>") || !strings.Contains(plan.Content, "RunAtLoad") {
			t.Fatalf("plist missing agent: %s", plan.Content)
		}
	} else if !strings.Contains(plan.Content, "bridge run --as worker.host") {
		t.Fatalf("unit missing agent: %s", plan.Content)
	}
	if runtime.GOOS == "linux" && !strings.Contains(plan.Content, "ANX_BASE_URL=https://core.example.test") {
		t.Fatalf("unit missing env: %s", plan.Content)
	}
	if runtime.GOOS == "darwin" && (!strings.Contains(plan.Content, "<key>ANX_BASE_URL</key>") || !strings.Contains(plan.Content, "EnvironmentVariables")) {
		t.Fatalf("plist missing env: %s", plan.Content)
	}
	if plan.Path == "" || plan.Label == "" || len(plan.StartArgs) == 0 {
		t.Fatalf("incomplete plan: %#v", plan)
	}
	plan, err = answerBridgeUnitPlan("/srv/anx-it", "wo%rker", "/opt/anx", "/var/log/anx/x.log", nil)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "linux" && strings.Contains(plan.Content, "%r") && !strings.Contains(plan.Content, "wo%%rker") {
		t.Fatalf("unescaped %% in systemd unit: %s", plan.Content)
	}
}
