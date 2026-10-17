package app

import (
	hostbridge "agent-nexus-cli/internal/bridge"
	"agent-nexus-cli/internal/integrations"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/output"
	"agent-nexus-cli/internal/workspaceconfig"
)

// Ask answer-delivery modes. Every ask records one so an answer is never
// silently dropped: await blocks the caller, resume hands the answer to a
// resident executor through anx bridge run, batch queues answers for the next
// anx await --answers sweep, and none is an explicit opt-out.
const (
	askDeliveryAwait  = "await"
	askDeliveryResume = "resume"
	askDeliveryBatch  = "batch"
	askDeliveryNone   = "none"
)

type askDeliveryDecision struct {
	Mode     string
	Reason   string
	Command  string
	Warnings []string
}

type askDeliveryInput struct {
	Explicit     string
	ExplicitSet  bool
	OnAnswer     string
	OnAnswerFlag bool
	AskID        string
}

func validAskDeliveryMode(value string) bool {
	switch value {
	case askDeliveryAwait, askDeliveryResume, askDeliveryBatch, askDeliveryNone:
		return true
	}
	return false
}

// answerBridgeProbe is the outcome of inspecting the answer-registry lock.
// Unknown covers every state the probe cannot verify (open failures,
// unexpected flock errors, platforms without the bridge); delivery selection
// must treat unknown as "not a deliverer", never as "running".
type answerBridgeProbe int

const (
	answerBridgeAbsent answerBridgeProbe = iota
	answerBridgeRunning
	answerBridgeUnknown
)

// Per-agent readiness is reported by the canonical host supervisor. A held
// singleton alone never enables resume delivery for an unregistered agent.
func (a *App) answerBridgeState(cfg config.Resolved) answerBridgeProbe {
	if runtime.GOOS == "windows" {
		return answerBridgeUnknown
	}
	root, e := a.hostBridgeRoot()
	if e != nil {
		return answerBridgeUnknown
	}
	if _, statErr := os.Stat(root); os.IsNotExist(statErr) {
		return answerBridgeAbsent
	} else if statErr != nil {
		return answerBridgeUnknown
	}
	if askBridgeLockState(filepath.Join(root, "service.lock")) != answerBridgeRunning {
		return answerBridgeAbsent
	}
	r, e := hostbridge.Load(root)
	if e != nil {
		return answerBridgeUnknown
	}
	raw, e := integrations.PrivateRead(filepath.Join(root, "status.json"))
	if e != nil {
		return answerBridgeUnknown
	}
	var status localBridgeState
	if json.Unmarshal(raw, &status) != nil || time.Since(status.At) > 10*time.Second || time.Until(status.At) > time.Second {
		return answerBridgeUnknown
	}
	for _, w := range r.Workspaces {
		if w.BaseURL != strings.TrimRight(cfg.BaseURL, "/") {
			continue
		}
		for _, agent := range w.Agents {
			if agent.ActorID == cfg.ActorID && agent.Jobs["answers"] && status.Ready[hostbridge.AgentKey(w, agent)+"\x00answers"] {
				return answerBridgeRunning
			}
		}
	}
	return answerBridgeAbsent
}

// resolveAskDelivery picks the answer-delivery mode for one ask. Filing must
// never fail merely because the mode is missing, so the auto default is batch;
// only an explicit --delivery resume without a way to deliver errors.
func (a *App) resolveAskDelivery(cfg config.Resolved, in askDeliveryInput) (askDeliveryDecision, error) {
	decision := askDeliveryDecision{Command: a.resumeCommand(in.OnAnswer, in.AskID)}
	agentctlContinuation := strings.TrimSpace(a.Getenv("AGENTCTL_EXECUTION_ID")) != ""
	probe := a.answerBridgeState(cfg)
	bridgeRunning := probe == answerBridgeRunning
	canDeliver := bridgeRunning || agentctlContinuation

	if in.ExplicitSet {
		explicit := strings.ToLower(strings.TrimSpace(in.Explicit))
		if !validAskDeliveryMode(explicit) {
			return decision, errnorm.Usage("invalid_request", "--delivery must be one of: await, resume, batch, none")
		}
		switch explicit {
		case askDeliveryResume:
			if decision.Command == "" {
				return decision, errnorm.Usage("invalid_request", "--delivery resume needs an executor command; pass --on-answer <command>, set ANX_RESUME_CMD, or run under agentctl; choices: --delivery await (block on anx await <id>), --delivery batch (sweep with anx await --answers), --delivery none")
			}
			if !canDeliver {
				return decision, errnorm.Usage("delivery_unavailable", "no anx bridge run is running for this host and agent and no agentctl continuation is active; choices: keep a bridge running (anx bridge run, or set it up once with anx bridge install), --delivery await (block on anx await <id>), --delivery batch (sweep with anx await --answers), or --delivery none")
			}
			decision.Mode = askDeliveryResume
			switch {
			case bridgeRunning && agentctlContinuation:
				decision.Reason = "explicit --delivery resume; anx bridge run is running and an agentctl continuation is active"
			case bridgeRunning:
				decision.Reason = "explicit --delivery resume; anx bridge run is running for this host and agent"
			default:
				decision.Reason = "explicit --delivery resume; the agentctl continuation receives the answer"
			}
			return decision, nil
		case askDeliveryAwait, askDeliveryBatch, askDeliveryNone:
			if in.OnAnswerFlag {
				return decision, errnorm.Usage("invalid_request", "--on-answer registers resume delivery; drop it or use --delivery resume")
			}
			decision.Mode = explicit
			decision.Reason = "explicit --delivery " + explicit
			if strings.TrimSpace(a.Getenv("ANX_RESUME_CMD")) != "" {
				decision.Warnings = append(decision.Warnings, "ANX_RESUME_CMD is set but --delivery "+explicit+" skips resume registration")
			}
			return decision, nil
		}
	}

	// Explicit executor wiring (--on-answer, ANX_RESUME_CMD, agentctl) always
	// wins over defaults. Registration is durable, so a bridge that starts
	// later still delivers; the reason states whether one is reachable now.
	if decision.Command != "" {
		decision.Mode = askDeliveryResume
		switch {
		case bridgeRunning && agentctlContinuation:
			decision.Reason = "anx bridge run is running and an agentctl continuation is active"
		case bridgeRunning:
			decision.Reason = "anx bridge run is running for this host and agent"
		case agentctlContinuation:
			decision.Reason = "an agentctl continuation receives the answer"
		default:
			decision.Reason = "no anx bridge run is reachable right now; the registration is durable and a bridge started later will deliver"
		}
		return decision, nil
	}

	workspaceDefault, workspaceErr := a.workspaceAskDeliveryDefault(cfg)
	if workspaceErr != nil {
		decision.Warnings = append(decision.Warnings, "ignoring workspace ask-delivery default: "+workspaceErr.Error())
	}
	if workspaceDefault != "" {
		switch workspaceDefault {
		case askDeliveryResume:
			decision.Mode = askDeliveryBatch
			decision.Reason = "workspace default ask-delivery resume cannot deliver here (no running anx bridge run, no agentctl continuation, no resume command); using batch"
			decision.Warnings = append(decision.Warnings, "workspace ask-delivery default is resume but this host cannot deliver; filed as batch")
			return decision, nil
		default:
			decision.Mode = workspaceDefault
			decision.Reason = "workspace default (anx config ask-delivery " + workspaceDefault + ")"
			return decision, nil
		}
	}

	decision.Mode = askDeliveryBatch
	switch {
	case bridgeRunning:
		decision.Reason = "anx bridge run is running but no resume command is registered (--on-answer, ANX_RESUME_CMD or agentctl); answers queue until anx await --answers"
	case probe == answerBridgeUnknown:
		decision.Reason = "the answer-bridge lock could not be verified and no agentctl continuation is active; answers queue until anx await --answers"
	default:
		decision.Reason = "no running anx bridge run and no agentctl continuation; answers queue until anx await --answers"
	}
	return decision, nil
}

// askDeliveryFollowUp is the exact runnable command the asking agent (or a
// human auditing the ask) should run next, per the chosen mode.
func askDeliveryFollowUp(mode, askRef string) string {
	switch mode {
	case askDeliveryAwait:
		return "anx await " + askRef
	case askDeliveryBatch:
		return "anx await --answers"
	case askDeliveryResume:
		// The registered executor fires on its own; `anx orient` is the
		// runnable check that shows the delivery state for open resume asks.
		return "anx orient"
	default:
		return "anx inbox list --status answered"
	}
}

// askDeliveryResultView is the delivery block every ask result carries: the
// chosen mode, why it was chosen, and the exact follow-up command.
func askDeliveryResultView(decision askDeliveryDecision, askRef string) map[string]any {
	view := map[string]any{"mode": decision.Mode, "reason": decision.Reason, "next": askDeliveryFollowUp(decision.Mode, askRef)}
	if decision.Mode == askDeliveryResume {
		view["executor"] = "anx bridge run"
	}
	return view
}

func askDeliveryWarnings(decision askDeliveryDecision) []output.Warning {
	warnings := make([]output.Warning, 0, len(decision.Warnings))
	for _, warning := range decision.Warnings {
		warnings = append(warnings, output.Warning{Code: "ask_delivery", Message: warning})
	}
	return warnings
}

// orientDeliveryHealth flags open resume-mode asks that nothing on this host
// can deliver: no running anx bridge run and no agentctl continuation. An
// agentctl-managed orient counts as a resident executor host.
func (a *App) orientDeliveryHealth(cfg config.Resolved, openAsks []any) (map[string]any, []output.Warning) {
	openResumeAsks := make([]string, 0, len(openAsks))
	for _, raw := range openAsks {
		ask := asMap(raw)
		if anyString(ask["delivery"]) != askDeliveryResume {
			continue
		}
		if id := strings.TrimSpace(anyString(ask["id"])); id != "" {
			openResumeAsks = append(openResumeAsks, "event:"+id)
		}
	}
	if len(openResumeAsks) == 0 {
		return map[string]any{"open_resume_asks": []string{}}, nil
	}
	probe := a.answerBridgeState(cfg)
	health := map[string]any{"open_resume_asks": openResumeAsks, "bridge_running": probe == answerBridgeRunning}
	health["recommended_next"] = "anx bridge install"
	health["registration_next"] = "anx bridge add --as " + cfg.As
	health["without_bridge"] = "Unattended resumes wait; Git and spool drain are manual without their jobs. Await/batch remain available."
	if probe == answerBridgeUnknown {
		// An unverifiable probe is not evidence of absence; stay quiet rather
		// than alarm on a permissions hiccup.
		health["bridge_running"] = nil
		return health, nil
	}
	if probe == answerBridgeRunning || strings.TrimSpace(a.Getenv("AGENTCTL_EXECUTION_ID")) != "" {
		return health, nil
	}
	health["warning"] = fmt.Sprintf("%d open %s ask(s) expect the resident executor, but no anx bridge run is running for this host and no agentctl continuation is active; answers will not reach the executor", len(openResumeAsks), askDeliveryResume)
	return health, []output.Warning{{Code: "ask_delivery", Message: health["warning"].(string)}}
}

// workspaceAskDeliveryDefault reads the workspace ask-delivery preference.
// A missing preference is empty; an unreadable or invalid one is reported so
// filing can fall back to the auto default instead of failing.
func (a *App) workspaceAskDeliveryDefault(cfg config.Resolved) (string, error) {
	catalog, _, err := a.workspaceCatalog(cfg)
	if err != nil {
		return "", err
	}
	value := strings.ToLower(strings.TrimSpace(catalog.File.AskDelivery))
	if value == "" {
		return "", nil
	}
	if !validAskDeliveryMode(value) {
		return "", errnorm.Usage("invalid_request", "ask-delivery must be one of: await, resume, batch, none; run anx config ask-delivery <mode>")
	}
	return value, nil
}

// setWorkspaceAskDeliveryDefault persists the workspace preference next to the
// other CLI workspace preferences, outside any repository.
func setWorkspaceAskDeliveryDefault(dir, mode string) error {
	return workspaceconfig.Update(dir, func(current *workspaceconfig.Catalog) error {
		current.File.AskDelivery = mode
		return nil
	})
}
