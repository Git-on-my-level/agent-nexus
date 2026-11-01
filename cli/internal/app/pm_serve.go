// The `anx pm serve` loop: claim queued PM turns, run each claimed turn
// through the harness, and manage the per-serve runner identity.

package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
)

func (a *App) runPMServe(ctx context.Context, args []string, cfg config.Resolved) (*commandResult, error) {
	fs := newSilentFlagSet("pm serve")
	var runner, workDir, pollInterval, maxConcurrentFlag trackedString
	fs.Var(&runner, "runner", "Harness argv; evidence refs come from a trailing ---evidence--- block or JSON evidence_refs on the reply object, not prose or nested tool output. Over-limit replies get a truncation marker. Default max_output_bytes is 64000 unless the claimed turn sets a lower value.")
	fs.Var(&workDir, "work-dir", "Prompt file directory")
	fs.Var(&pollInterval, "poll-interval", "Empty-claim sleep")
	fs.Var(&maxConcurrentFlag, "max-concurrent", "In-process turn cap")
	if err := fs.Parse(args); err != nil {
		return nil, errnorm.Usage("invalid_flags", err.Error())
	}
	if len(fs.Args()) > 0 {
		return nil, errnorm.Usage("invalid_args", "unexpected positional arguments for anx pm serve")
	}
	argv, err := splitRunnerArgv(strings.TrimSpace(runner.value))
	if err != nil || len(argv) == 0 {
		return nil, errnorm.Usage("invalid_request", "--runner is required; example: --runner 'omp -p --mode json --model zai/glm-5.3 --auto-approve'")
	}
	direct := runnerUsesPromptPlaceholder(argv)
	dir := strings.TrimSpace(workDir.value)
	if dir == "" {
		dir = filepath.Join(".tmp", "pm-runner")
	}
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "work_dir_failed", "failed to create --work-dir", err)
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "work_dir_failed", "failed to resolve --work-dir", err)
	}
	interval := 2 * time.Second
	if strings.TrimSpace(pollInterval.value) != "" {
		parsed, parseErr := time.ParseDuration(pollInterval.value)
		if parseErr != nil || parsed < 200*time.Millisecond {
			return nil, errnorm.Usage("invalid_request", "--poll-interval must be a duration of at least 200ms")
		}
		interval = parsed
	}
	maxConcurrent := 1
	if strings.TrimSpace(maxConcurrentFlag.value) != "" {
		n, parseErr := strconv.Atoi(strings.TrimSpace(maxConcurrentFlag.value))
		if parseErr != nil || n < 1 || n > 16 {
			return nil, errnorm.Usage("invalid_request", "--max-concurrent must be an integer from 1 to 16")
		}
		maxConcurrent = n
	}
	agentctl := ""
	if !direct {
		agentctl, err = lookPath("agentctl")
		if err != nil {
			return nil, errnorm.New(errnorm.KindLocal, "dependency_unavailable", "agentctl is not on PATH; pass a --runner argv that includes {prompt} to exec a harness directly")
		}
	}
	runnerID, err := loadOrCreateRunnerID(absDir)
	if err != nil {
		return nil, err
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "runner_path_failed", "failed to resolve the anx executable for the PM runner", err)
	}
	env, pathWarning, err := pmRunnerChildEnv(cfg, os.Environ(), executable)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "runner_path_failed", "failed to prepare PATH for the PM runner", err)
	}
	if pathWarning != "" {
		a.pmLog("pm serve: warning: %s\n", pathWarning)
	}
	if !writerIsTTY(a.Stderr) {
		prev := harnessLogWriter
		harnessLogWriter = a.Stderr
		defer func() { harnessLogWriter = prev }()
	}
	workerIDs := make([]string, maxConcurrent)
	for i := 0; i < maxConcurrent; i++ {
		workerIDs[i] = pmServeWorkerRunnerID(runnerID, i+1)
	}
	hostLabel, _ := os.Hostname()
	hostLabel = safePMConnectionLabel(hostLabel, "local computer")
	_, connectErr := a.invokeRawJSON(ctx, cfg, "pm connect", "POST", "/pm/connect", map[string]any{"runner": pmRunnerLabel(argv), "host": hostLabel})
	// Older cores lack this optional connection operation; their accepted claims
	// retain the legacy presence signal. Authentication failures never fall back.
	if connectErr != nil && ctx.Err() == nil && httpStatusFromErr(connectErr) != 404 {
		return nil, connectErr
	}
	a.pmLog("pm serve: agent=%s runner_id=%s worker_ids=%s work_dir=%s max_concurrent=%d\n", cfg.Agent, runnerID, strings.Join(workerIDs, ","), absDir, maxConcurrent)
	serveCtx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	harnessRoot, stopHarness := context.WithCancel(context.Background())
	defer stopHarness()
	type heldTurn struct {
		id       string
		runnerID string
		turn     map[string]any
	}
	var (
		mu                 sync.Mutex
		inFlight           sync.WaitGroup
		held               = map[string]heldTurn{}
		usedSlots          = make([]bool, maxConcurrent)
		forbiddenStreak    int
		nonRetryableStreak int
		backoff            = claimBackoffStart
		lastCapacityLog    time.Time
	)
	pickSlot := func() int {
		for i, used := range usedSlots {
			if !used {
				return i + 1
			}
		}
		return 0
	}
	shutdown := func() (*commandResult, error) {
		stopHarness()
		drained := make(chan struct{})
		go func() {
			inFlight.Wait()
			close(drained)
		}()
		timer := time.NewTimer(pmServeShutdownGrace)
		defer timer.Stop()
		select {
		case <-drained:
		case <-timer.C:
		}
		mu.Lock()
		pending := make([]heldTurn, 0, len(held))
		for _, turn := range held {
			pending = append(pending, turn)
		}
		mu.Unlock()
		releaseCtx, cancelRelease := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelRelease()
		released := make([]string, 0, len(pending))
		for _, item := range pending {
			lease := ""
			if item.turn != nil {
				lease = anyString(item.turn["lease_token"])
			}
			id := firstNonEmpty(item.runnerID, runnerID)
			if err := a.releaseTurn(releaseCtx, cfg, id, item.id, lease); err != nil {
				a.pmLog("pm serve: release %s failed: %v\n", item.id, err)
				continue
			}
			a.pmLog("pm serve: released turn %s : shutdown\n", item.id)
			released = append(released, item.id)
		}
		return &commandResult{
			Text: fmt.Sprintf("pm serve stopped; released %d turn(s)", len(released)),
			Data: map[string]any{"stopped": true, "released": released},
		}, nil
	}
	for {
		select {
		case <-serveCtx.Done():
			return shutdown()
		default:
		}
		mu.Lock()
		slot := pickSlot()
		mu.Unlock()
		if slot == 0 {
			if sleepErr := sleepFn(serveCtx, interval); sleepErr != nil {
				return shutdown()
			}
			continue
		}
		slotID := workerIDs[slot-1]
		claimed, claimErr := a.invokeRawJSON(serveCtx, cfg, "pm turns claim", "POST", "/pm/turns/claim", map[string]any{"runner_id": slotID})
		if claimErr == nil {
			_ = writePMPrivateFile(filepath.Join(absDir, "last-claim"), []byte(nowFn().UTC().Format(time.RFC3339Nano)))
		}
		if cap, ok := parsePMClaimCapacity(claimed, claimErr); ok {
			forbiddenStreak = 0
			nonRetryableStreak = 0
			backoff = claimBackoffStart
			if lastCapacityLog.IsZero() || nowFn().Sub(lastCapacityLog) >= claimCapacityLogInterval {
				a.pmLog("pm serve: %s\n", formatPMClaimCapacityText(cap))
				lastCapacityLog = nowFn()
			}
			if sleepErr := sleepFn(serveCtx, interval); sleepErr != nil {
				return shutdown()
			}
			continue
		}
		if claimErr != nil {
			if errors.Is(claimErr, context.Canceled) || errors.Is(claimErr, context.DeadlineExceeded) {
				return shutdown()
			}
			if claimErrorForbidden(claimErr) {
				nonRetryableStreak = 0
				forbiddenStreak++
				if forbiddenStreak == 1 {
					a.pmLog("pm serve: claim forbidden: pm.respond requires the configured PM actor (ANX_PM_AGENT_ACTOR_ID); this agent is %s\n", firstNonEmpty(cfg.Agent, "unknown"))
				}
				if forbiddenStreak >= claimForbiddenLimit {
					msg := fmt.Sprintf("Claim failed with a forbidden error %d times. Exiting.", claimForbiddenLimit)
					a.pmLog("pm serve: %s\n", msg)
					return nil, errnorm.New(errnorm.KindRemote, "claim_failed", msg)
				}
				a.pmLog("pm serve: claim failed (%s); retrying in %s, %d of %d\n", claimErrorLabel(claimErr), backoff, forbiddenStreak, claimForbiddenLimit)
				if sleepErr := sleepFn(serveCtx, backoff); sleepErr != nil {
					return shutdown()
				}
				if backoff < claimBackoffCap {
					backoff *= 2
					if backoff > claimBackoffCap {
						backoff = claimBackoffCap
					}
				}
				continue
			}
			if !claimErrorRetryable(claimErr) {
				forbiddenStreak = 0
				nonRetryableStreak++
				if nonRetryableStreak >= claimNonRetryableLimit {
					msg := fmt.Sprintf("Claim failed with a non-retryable error %d times. Exiting.", claimNonRetryableLimit)
					a.pmLog("pm serve: %s\n", msg)
					return nil, errnorm.New(errnorm.KindRemote, "claim_failed", msg)
				}
				a.pmLog("pm serve: claim failed (%s); retrying in %s, %d of %d\n", claimErrorLabel(claimErr), backoff, nonRetryableStreak, claimNonRetryableLimit)
				if sleepErr := sleepFn(serveCtx, backoff); sleepErr != nil {
					return shutdown()
				}
				if backoff < claimBackoffCap {
					backoff *= 2
					if backoff > claimBackoffCap {
						backoff = claimBackoffCap
					}
				}
				continue
			}
			forbiddenStreak = 0
			nonRetryableStreak = 0
			backoff = claimBackoffStart
			a.pmLog("pm serve: claim failed: %v\n", claimErr)
			if sleepErr := sleepFn(serveCtx, interval); sleepErr != nil {
				return shutdown()
			}
			continue
		}
		forbiddenStreak = 0
		nonRetryableStreak = 0
		backoff = claimBackoffStart
		status, _ := asMap(claimed.Data)["status_code"].(int)
		if status == 204 || commandResultBody(claimed) == nil {
			if sleepErr := sleepFn(serveCtx, interval); sleepErr != nil {
				return shutdown()
			}
			continue
		}
		turn := commandResultBody(claimed)
		if anyString(turn["lease_owner"]) == "" {
			turn["lease_owner"] = slotID
		}
		turnID := anyString(turn["id"])
		if a.turnMem().isGivenUp(turnID) {
			a.giveUpOnTurn(serveCtx, serveCtx, cfg, absDir, turn, nowFn())
			if sleepErr := sleepFn(serveCtx, interval); sleepErr != nil {
				return shutdown()
			}
			continue
		}
		mu.Lock()
		if _, exists := held[turnID]; exists {
			mu.Unlock()
			if sleepErr := sleepFn(serveCtx, interval); sleepErr != nil {
				return shutdown()
			}
			continue
		}
		usedSlots[slot-1] = true
		held[turnID] = heldTurn{id: turnID, runnerID: slotID, turn: turn}
		mu.Unlock()
		a.pmLog("pm serve: claimed turn %s runner_id=%s\n", turnID, slotID)
		runTurn := func() {
			settled := a.handleClaimedTurn(ctx, serveCtx, cfg, absDir, agentctl, argv, env, turn, harnessRoot)
			mu.Lock()
			if settled {
				delete(held, turnID)
				usedSlots[slot-1] = false
			}
			mu.Unlock()
		}
		inFlight.Add(1)
		if maxConcurrent == 1 {
			done := make(chan struct{})
			go func() {
				defer inFlight.Done()
				runTurn()
				close(done)
			}()
			select {
			case <-done:
			case <-serveCtx.Done():
				return shutdown()
			}
			if a.turnMem().isGivenUp(turnID) {
				if sleepErr := sleepFn(serveCtx, interval); sleepErr != nil {
					return shutdown()
				}
			}
			continue
		}
		go func() {
			defer inFlight.Done()
			runTurn()
		}()
	}
}

func shuttingDown(ctx context.Context) bool {
	return ctx != nil && ctx.Err() != nil
}

func (a *App) handleClaimedTurn(ctx, shutdownCtx context.Context, cfg config.Resolved, workDir, agentctl string, argv, env []string, turn map[string]any, harnessBase context.Context) bool {
	if turn == nil {
		return true
	}
	turnID := anyString(turn["id"])
	for rerun := 0; rerun < 2; rerun++ {
		settled, retry := a.runClaimedTurn(ctx, shutdownCtx, cfg, workDir, agentctl, argv, env, turn, harnessBase)
		if !retry {
			return settled
		}
		if rerun == 1 || !a.turnMem().canRunHarness(turnID) {
			if !a.turnMem().canRunHarness(turnID) {
				return a.giveUpOnTurn(ctx, shutdownCtx, cfg, workDir, turn, nowFn())
			}
			a.pmLog("pm serve: turn %s lease lost again after re-run; releasing lease\n", turnID)
			a.releaseTurnBestEffort(ctx, cfg, turn, workDir, "lease lost after re-run")
			return true
		}
	}
	return true
}

func (a *App) runClaimedTurn(ctx, shutdownCtx context.Context, cfg config.Resolved, workDir, agentctl string, argv, env []string, turn map[string]any, harnessBase context.Context) (settled bool, retryHarness bool) {
	started := nowFn()
	turnID := anyString(turn["id"])
	leaseToken := anyString(turn["lease_token"])
	deadline := parseTurnDeadline(turn)
	maxBytes := defaultPMMaxOutputBytes
	if n, ok := intFromAny(turn["max_output_bytes"]); ok && n >= 256 {
		maxBytes = n
	}
	direct := runnerUsesPromptPlaceholder(argv)
	var progress *pmProgress
	if supported, _ := turn["activity_supported"].(bool); supported && !turnIsTerminal(turn) {
		progress = newPMProgress(turn, maxBytes)
		a.emitPMProgress(ctx, cfg, turnID, leaseToken, progress)
	}
	fail := func(reason string, raw []byte) (bool, bool) {
		if progress != nil {
			progress.status("Finished")
			a.emitPMProgress(ctx, cfg, turnID, leaseToken, progress)
		}
		return a.settleFailedTurn(ctx, shutdownCtx, cfg, workDir, turn, started, reason, raw, direct), false
	}
	complete := func(text, provider, model string, raw []byte) (bool, bool) {
		if shuttingDown(shutdownCtx) {
			return false, false
		}
		if progress != nil {
			progress.status("Finished")
			a.emitPMProgress(ctx, cfg, turnID, leaseToken, progress)
		}
		_, err := a.completeTurnUntil(ctx, shutdownCtx, cfg, turnID, leaseToken, text, raw, maxBytes, deadline)
		if err == nil {
			removeTurnReply(workDir, turnID)
			a.turnMem().observeTerminal(turnID)
			suffix := ""
			if provider != "" {
				suffix = fmt.Sprintf(" provider=%s model=%s", provider, model)
			}
			a.pmLog("pm serve: turn %s completed in %ds%s\n", turnID, elapsedSeconds(started), suffix)
			return true, false
		}
		if shuttingDown(shutdownCtx) || errors.Is(err, context.Canceled) {
			return false, false
		}
		a.saveTurnReplyFile(workDir, turnID, text)
		return a.handleFailedComplete(ctx, shutdownCtx, cfg, workDir, turn, err)
	}
	if turnIsTerminal(turn) {
		removeTurnReply(workDir, turnID)
		a.turnMem().observeTerminal(turnID)
		return true, false
	}
	if reply, replyErr := readTurnReply(workDir, turnID); replyErr != nil {
		a.pmLog("pm serve: turn %s saved reply unreadable: %v; falling back to harness\n", turnID, replyErr)
	} else if reply != "" {
		if shuttingDown(shutdownCtx) {
			return false, false
		}
		_, err := a.completeTurnUntil(ctx, shutdownCtx, cfg, turnID, leaseToken, reply, nil, maxBytes, deadline)
		if err == nil {
			removeTurnReply(workDir, turnID)
			a.turnMem().observeTerminal(turnID)
			a.pmLog("pm serve: turn %s completed in %ds from saved reply\n", turnID, elapsedSeconds(started))
			return true, false
		}
		if shuttingDown(shutdownCtx) || errors.Is(err, context.Canceled) {
			return false, false
		}
		a.pmLog("pm serve: turn %s saved reply complete failed: %v\n", turnID, err)
		return a.handleFailedComplete(ctx, shutdownCtx, cfg, workDir, turn, err)
	}
	if !a.turnMem().canRunHarness(turnID) {
		return a.giveUpOnTurn(ctx, shutdownCtx, cfg, workDir, turn, started), false
	}
	prompt := buildPMPrompt(cfg.Agent, turn, maxBytes)
	promptPath := filepath.Join(workDir, "turn-"+sanitizeFilePart(turnID)+".md")
	if err := writePMPrivateFile(promptPath, []byte(prompt)); err != nil {
		return fail("failed to write prompt file: "+err.Error(), nil)
	}
	defer os.Remove(promptPath)
	remain := time.Until(deadline)
	if remain < time.Second {
		return fail(humanTurnFailure("deadline", ""), nil)
	}
	if reason := missingHarnessSecretReason(argv, env); reason != "" {
		return fail(reason, nil)
	}
	a.turnMem().noteHarnessRun(turnID)
	env = overlayEnv(env, "ANX_PM_LEASE_TOKEN", leaseToken)
	env = overlayEnv(env, "ANX_PM_TURN_ID", turnID)
	env = overlayEnv(env, "ANX_PM_AGENT", cfg.Agent)
	if progress != nil {
		env = overlayEnv(env, "ANX_PM_TURN_ID", turnID)
		env = overlayEnv(env, "ANX_PM_ACTIVITY_ENABLED", "1")
		env = overlayEnv(env, "ANX_PM_BASE_URL", cfg.BaseURL)
	}
	base := harnessBase
	if base == nil {
		base = ctx
	}
	runCtx, cancel := context.WithTimeout(base, remain)
	defer cancel()
	if progress != nil {
		runCtx = context.WithValue(runCtx, pmProgressKey{}, progress)
		progress.status("Running")
		a.emitPMProgress(runCtx, cfg, turnID, leaseToken, progress)
	}
	leaseLost := a.watchTurnLease(runCtx, cancel, cfg, turn)
	ifLeaseLost := func(text string) (bool, bool, bool) {
		if !leaseLost.Load() {
			return false, false, false
		}
		if strings.TrimSpace(text) != "" {
			a.saveTurnReplyFile(workDir, turnID, text)
		}
		settled, retry := a.handleHeartbeatLeaseLoss(ctx, shutdownCtx, cfg, workDir, turn)
		return settled, retry, true
	}
	if runnerUsesPromptPlaceholder(argv) {
		expanded := expandPromptPlaceholder(argv, promptPath)
		if len(expanded) == 0 || strings.TrimSpace(expanded[0]) == "" {
			return fail("invalid --runner after {prompt} expansion", nil)
		}
		stdout, stderr, err := runCmd(runCtx, expanded[0], expanded[1:], workDir, env)
		// Flush final events before completion cancels the heartbeat watcher.
		// A failed progress write must not turn a successful answer into failure.
		if progressFromContext(runCtx) != nil {
			flushCtx, flushCancel := context.WithTimeout(runCtx, 5*time.Second)
			_, lost, _ := a.heartbeatTurn(flushCtx, cfg, turnID, leaseToken)
			flushCancel()
			if lost {
				leaseLost.Store(true)
			}
		}
		a.logRunnerStderr(turnID, stderr)
		stdout = withoutPMProgress(stdout)
		text := assistantTextFromRunnerOutput(stdout)
		if settled, retry, ok := ifLeaseLost(text); ok {
			return settled, retry
		}
		if err != nil {
			if shuttingDown(shutdownCtx) || errors.Is(err, context.Canceled) {
				return false, false
			}
			if text != "" && errors.Is(err, os.ErrDeadlineExceeded) && !errors.Is(err, context.DeadlineExceeded) {
				return complete(text, "", "", stdout)
			}
			a.pmLog("pm serve: turn %s harness error: %v\n", turnID, err)
			return fail(harnessCmdFailure(err), joinCmdOutput(stdout, stderr))
		}
		if text == "" {
			return fail(humanTurnFailure("no_assistant", ""), stderr)
		}
		return complete(text, "", "", stdout)
	}
	runArgs := []string{
		"run", "--background",
		"--label", "anx-pm",
		"--timeout", remain.Round(time.Second).String(),
		"--prompt-file", promptPath,
		"--prompt-delivery", "argv",
		"--",
	}
	runArgs = append(runArgs, argv...)
	launchOut, launchErrOut, launchErr := runCmd(runCtx, agentctl, runArgs, workDir, env)
	a.logRunnerStderr(turnID, launchErrOut)
	if settled, retry, ok := ifLeaseLost(""); ok {
		return settled, retry
	}
	execID, launchTimeout := "", ""
	if launchErr != nil {
		if shuttingDown(shutdownCtx) || errors.Is(launchErr, context.Canceled) {
			a.logAgentctlOutlives(turnID, firstNonEmpty(extractExecutionID(launchOut), execID))
			return false, false
		}
		var ok bool
		execID, launchTimeout, ok = continuingAgentctlLaunch(launchOut)
		if !ok {
			return fail(humanTurnFailure("launch_failed", ""), joinCmdOutput(launchOut, launchErrOut))
		}
		a.pmLog("pm serve: turn %s agentctl: %s\n", turnID, strings.TrimSpace(string(launchOut)))
	} else {
		execID = extractExecutionID(launchOut)
	}
	if execID == "" {
		return fail(humanTurnFailure("launch_failed", ""), joinCmdOutput(launchOut, launchErrOut))
	}
	awaitArgs := []string{"await", execID, "--through-execution-deadline", "--ignore-attention"}
	awaitOut, awaitErrOut, awaitErr := runCmd(runCtx, agentctl, awaitArgs, workDir, env)
	a.logRunnerStderr(turnID, awaitErrOut)
	if settled, retry, ok := ifLeaseLost(""); ok {
		return settled, retry
	}
	if awaitErr != nil && agentctlNotFound(awaitOut, awaitErr) {
		a.pmLog("pm serve: turn %s agentctl: %s\n", turnID, strings.TrimSpace(string(awaitOut)))
		if visErr := waitForAgentctlExecution(runCtx, agentctl, execID, workDir, env); visErr != nil {
			if settled, retry, ok := ifLeaseLost(""); ok {
				return settled, retry
			}
			if shuttingDown(shutdownCtx) || errors.Is(visErr, context.Canceled) {
				a.logAgentctlOutlives(turnID, execID)
				return false, false
			}
			return fail(humanTurnFailure("startup_timeout", launchTimeout), joinCmdOutput(awaitOut, awaitErrOut))
		}
		awaitOut, awaitErrOut, awaitErr = runCmd(runCtx, agentctl, awaitArgs, workDir, env)
		a.logRunnerStderr(turnID, awaitErrOut)
	}
	if awaitErr != nil {
		if settled, retry, ok := ifLeaseLost(""); ok {
			return settled, retry
		}
		if shuttingDown(shutdownCtx) || errors.Is(awaitErr, context.Canceled) {
			a.logAgentctlOutlives(turnID, execID)
			return false, false
		}
		return fail(humanTurnFailure("await_failed", ""), joinCmdOutput(awaitOut, awaitErrOut))
	}
	contentOut, contentErrOut, _ := runCmd(runCtx, agentctl, []string{"result", execID, "--content"}, workDir, env)
	a.logRunnerStderr(turnID, contentErrOut)
	metaOut, _, _ := runCmd(runCtx, agentctl, []string{"result", execID}, workDir, env)
	blob := string(contentOut) + "\n" + string(metaOut) + "\n" + string(awaitOut) + "\n" + string(launchOut)
	provider, model := extractProviderModel(blob)
	text := assistantTextFromRunnerOutput(contentOut)
	if settled, retry, ok := ifLeaseLost(text); ok {
		return settled, retry
	}
	if text == "" {
		return fail(humanTurnFailure("no_assistant", ""), contentErrOut)
	}
	return complete(text, provider, model, contentOut)
}

func pmServeWorkerRunnerID(base string, slot int) string {
	return strings.TrimSpace(base) + "-" + strconv.Itoa(slot)
}

func loadOrCreateRunnerID(dir string) (string, error) {
	path := filepath.Join(dir, "runner-id")
	if raw, err := os.ReadFile(path); err == nil {
		id := strings.TrimSpace(string(raw))
		if id != "" {
			return id, nil
		}
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", errnorm.Wrap(errnorm.KindLocal, "runner_id_failed", "failed to create runner id", err)
	}
	id := hex.EncodeToString(b[:])
	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return "", errnorm.Wrap(errnorm.KindLocal, "runner_id_failed", "failed to persist runner id", err)
	}
	return id, nil
}
