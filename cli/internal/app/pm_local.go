// Process seams for the local PM loop: the init() that registers the pm
// serve/ask local helper help entries, the injectable exec/time hooks, shared
// tuning constants, and small logging/context helpers the other pm_*.go files
// use.

package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"time"
)

func init() {
	localHelperTopics = append(localHelperTopics,
		localHelperTopic{
			Path:        "pm serve",
			Summary:     "Claim queued PM turns and run them through agentctl with the anx CLI as tools.",
			JSONShape:   "`turn_id`, `execution_id`, `status`, `provider`, `model`",
			Composition: "Local runner. Claims one leased turn, writes a small prompt file, launches the configured harness through agentctl, then completes or fails the turn. Does not call a model in-process.",
			Examples: []string{
				"anx pm serve --runner 'omp -p --mode json --model zai/glm-5.3 --auto-approve'",
				"anx pm serve --runner 'hermes chat --query-file {prompt_file} -Q'",
			},
			Flags: []localHelperFlag{
				{Name: "--runner <argv>", Description: "Harness argv. Without {prompt}, this is passed to `agentctl run --`. With {prompt_file} (or legacy {prompt}), argv is executed directly after substituting the prompt file path. Evidence refs come from a trailing ---evidence--- block or a JSON evidence_refs array on the reply object (the same object assistant text is read from), never from prose or nested tool output. Topic and document refs are verified like card/work/artifact/event/decision. Replies over the turn's max_output_bytes (default 64000, core's turn-text ceiling) are stored with a visible truncation marker."},
				{Name: "--work-dir <dir>", Description: "Directory for prompt files and the runner id (default .tmp/pm-runner). Must be the agentctl working root when agentctl is used."},
				{Name: "--poll-interval <duration>", Description: "Sleep between empty claims and after a released turn (default 2s)."},
				{Name: "--max-concurrent <n>", Description: "In-process cap on turns this runner executes at once (default 1). Each worker claims with a distinct runner id (<id>-<slot>). Core also bounds workspace sending turns."},
			},
		},
		localHelperTopic{
			Path:        "pm ask",
			Summary:     "Create a PM conversation and post one human question.",
			JSONShape:   "`conversation`, `turn`",
			Composition: "Local helper over `pm conversations create` and `pm conversations message`. A queued turn is not an assistant reply; run `anx pm serve` for that.",
			Examples: []string{
				"anx pm ask \"What needs my decision?\"",
				"anx pm ask --wait \"What needs my decision?\"",
			},
			Flags: []localHelperFlag{
				{Name: "--wait", Description: "Poll until the turn has a response, fails, or the deadline passes."},
				{Name: "--work-ref <ref>", Description: "Optional work/card ref to attach to the conversation."},
				{Name: "--title <text>", Description: "Conversation title (defaults to a prefix of the question)."},
				{Name: "--request-key <key>", Description: "Stable request key for create+message replay."},
				{Name: "--conversation-id <id>", Description: "Post into an existing conversation instead of creating one."},
			},
		},
		localHelperTopic{
			Path:        "pm channels doctor",
			Summary:     "Check PM channel secrets, webhook reachability, and binding state without sending a chat message.",
			JSONShape:   "`checks`, `ok`",
			Composition: "Local diagnostic. Reads env, probes webhook URLs with GET, and lists bindings. Does not send Telegram or Discord messages.",
			Examples: []string{
				"anx pm channels doctor",
				"anx pm channels doctor --telegram-webhook-url \"${ANX_TELEGRAM_WEBHOOK_URL:?set the intended URL to probe}\" --discord-webhook-url \"${ANX_DISCORD_WEBHOOK_URL:?set the intended URL to probe}\"",
			},
			Flags: []localHelperFlag{
				{Name: "--telegram-webhook-url <url>", Description: "Telegram ingress URL to probe with GET (fake or core). Does not POST an update."},
				{Name: "--discord-webhook-url <url>", Description: "Discord interactions URL to probe with GET (fake or core). Does not POST an interaction."},
			},
		},
	)
}

var (
	lookPath               = exec.LookPath
	runCmd                 = runHarnessCmd
	nowFn                  = time.Now
	sleepFn                = sleepCtx
	heartbeatRetryStart    = 2 * time.Second
	heartbeatRetryCap      = 8 * time.Second
	startupAckPoll         = 90 * time.Second
	pmServeShutdownGrace   = 5 * time.Second
	harnessKillGrace       = 2 * time.Second
	harnessWaitDelay       = 3 * time.Second
	claimForbiddenLimit    = 3
	claimNonRetryableLimit = 10
	claimBackoffStart      = time.Second
	claimBackoffCap        = 60 * time.Second
	harnessLogWriter       io.Writer
	evidenceRefRe          = regexp.MustCompile(`^(?:card|work|artifact|event|topic|document|decision):[A-Za-z0-9._:-]+$`)
	providerModelRe        = regexp.MustCompile(`"provider"\s*:\s*"([^"]+)"\s*,\s*"model"\s*:\s*"([^"]+)"`)
)

const (
	evidenceBlockMarker             = "---evidence---"
	defaultPMMaxOutputBytes         = 64000
	terminalRetryBudget             = time.Minute
	replyTruncationMarkerTmpl       = "\n\n[reply truncated by anx pm serve at %d bytes; %d bytes were dropped]"
	maxHarnessRunsPerTurn           = 3
	maxDeliveryAttempts             = 3
	maxUndeliverableCauseBytes      = 180
	claimCapacityLogInterval        = time.Minute
	emptyAskConversationReuseWindow = 5 * time.Minute
	emptyAskConversationScanLimit   = 5
	defaultLeaseHeartbeatInterval   = 20 * time.Second
	minLeaseHeartbeatInterval       = 10 * time.Millisecond
	harnessGiveUpReason             = "The PM could not produce a deliverable reply after several attempts."
	undeliverableReplyReason        = "The PM produced a reply but it could not be delivered. Ask again, or check that a runner is attached."
)

func (a *App) pmLog(format string, args ...any) {
	if a == nil || a.Stderr == nil {
		return
	}
	fmt.Fprintf(a.Stderr, format, args...)
	flushWriter(a.Stderr)
}

func flushWriter(w io.Writer) {
	type flusher interface{ Flush() error }
	if f, ok := w.(flusher); ok {
		_ = f.Flush()
	}
}

func writerIsTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
