package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/output"
)

// All sessions of a harness on one host share a derived agent principal, and
// answer read state is keyed on that principal. A session's answer wait is
// therefore local state, never read state: it records which asks this session
// filed and which answers it has already been handed. Nothing here marks an
// answer read, so one session's wait cannot hide an answer from another.
//
//	answer-waits/<workspace>/owners/<ask>              one session key per line
//	answer-waits/<workspace>/waits/<wait>/pending/<ask> asks this wait still owes
//	answer-waits/<workspace>/waits/<wait>/seen.json     answers already handed over
const (
	answerWaitDefaultSettle = 20 * time.Second
	answerWaitSeenCap       = 2000
	answerWaitRetention     = 90 * 24 * time.Hour
	// A filed ask is recorded before its request is sent, so a recent 404 can
	// be that request still in flight.
	answerWaitOrphanAfter = 10 * time.Minute
	// At most this many asks are read per poll; the rest take their turn, so an
	// agent with many open asks has a bounded request rate and still sees them all.
	answerWaitPollBudget = 25
)

var answerWaitPoll = 5 * time.Second

// answerSessionKey identifies the conversation asking. ANX_SESSION overrides
// the harness marker for callers that outlive one harness session. A child
// agent can inherit its parent's marker, so two markers fall through to the
// run's own execution id.
func (a *App) answerSessionKey() string {
	if value := strings.TrimSpace(a.Getenv("ANX_SESSION")); value != "" {
		return answerWaitDigest("anx\x00" + value)
	}
	claude := strings.TrimSpace(a.Getenv("CLAUDE_CODE_SESSION_ID"))
	codex := strings.TrimSpace(a.Getenv("CODEX_THREAD_ID"))
	switch {
	case claude != "" && codex == "":
		return answerWaitDigest("claude\x00" + claude)
	case codex != "" && claude == "":
		return answerWaitDigest("codex\x00" + codex)
	}
	if value := strings.TrimSpace(a.Getenv("AGENTCTL_EXECUTION_ID")); value != "" {
		return answerWaitDigest("agentctl\x00" + value)
	}
	return ""
}

func answerWaitDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}

type answerWaitStore struct {
	root    string
	wait    string
	session string
}

type answerWaitPending struct {
	AskID      string `json:"ask_id"`
	Title      string `json:"title,omitempty"`
	RecordedAt string `json:"recorded_at"`
}

type answerWaitSeen struct {
	Baselined bool     `json:"baselined"`
	IDs       []string `json:"ids"`
}

func (a *App) answerWaitStore(cfg config.Resolved) (*answerWaitStore, error) {
	dir := cfg.ConfigDir
	if dir == "" {
		home, err := a.UserHomeDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(home, ".config", "anx")
	}
	session := a.answerSessionKey()
	root := filepath.Join(dir, "answer-waits", answerWaitDigest(strings.TrimRight(cfg.BaseURL, "/")))
	return &answerWaitStore{root: root, session: session, wait: filepath.Join(root, "waits", answerWaitDigest(cfg.As+"\x00"+session))}, nil
}

// ensure creates the layout on the first write, so read-only commands that
// consult the ledger leave no trace.
func (s *answerWaitStore) ensure() error {
	for _, path := range []string{filepath.Join(s.root, "owners"), filepath.Join(s.wait, "pending")} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func answerWaitAskFile(askID string) string {
	sum := sha256.Sum256([]byte(askID))
	return hex.EncodeToString(sum[:])
}

// recordAsk claims an ask for this session. Each ask is its own file, so a
// filing can never lose a race with a running wait.
func (s *answerWaitStore) recordAsk(askID, title string, now time.Time) error {
	if err := s.ensure(); err != nil {
		return err
	}
	raw, err := json.Marshal(answerWaitPending{AskID: askID, Title: title, RecordedAt: now.UTC().Format(time.RFC3339Nano)})
	if err != nil {
		return err
	}
	if err = writeAnswerWaitFile(filepath.Join(s.wait, "pending", answerWaitAskFile(askID)), raw); err != nil {
		return err
	}
	if s.session != "" {
		file, err := os.OpenFile(filepath.Join(s.root, "owners", answerWaitAskFile(askID)), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, err = file.WriteString(s.session + "\n")
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return err
		}
	}
	s.prune(now)
	return nil
}

func writeAnswerWaitFile(path string, raw []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".wait-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(raw); err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func (s *answerWaitStore) prune(now time.Time) {
	for _, dir := range []string{filepath.Join(s.root, "owners"), filepath.Join(s.root, "waits")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			info, err := entry.Info()
			if err == nil && now.Sub(info.ModTime()) > answerWaitRetention {
				_ = os.RemoveAll(filepath.Join(dir, entry.Name()))
			}
		}
	}
}

// owners lists the sessions that filed an ask. Empty means unknown: filed
// elsewhere, without a session, or before this ledger existed.
func (s *answerWaitStore) owners(askID string) []string {
	raw, err := os.ReadFile(filepath.Join(s.root, "owners", answerWaitAskFile(askID)))
	if err != nil {
		return nil
	}
	return strings.Fields(string(raw))
}

// origin reports whose answer this is from this session's point of view.
func (s *answerWaitStore) origin(askID string) (mine, other bool) {
	owners := s.owners(askID)
	if s.session == "" || len(owners) == 0 {
		return false, false
	}
	for _, owner := range owners {
		if owner == s.session {
			return true, false
		}
	}
	return false, true
}

func (s *answerWaitStore) pending() []answerWaitPending {
	dir := filepath.Join(s.wait, "pending")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]answerWaitPending, 0, len(entries))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var item answerWaitPending
		if json.Unmarshal(raw, &item) == nil && item.AskID != "" {
			out = append(out, item)
		}
	}
	return out
}

func (s *answerWaitStore) dropPending(askID string) {
	_ = os.Remove(filepath.Join(s.wait, "pending", answerWaitAskFile(askID)))
}

func (s *answerWaitStore) loadSeen() answerWaitSeen {
	var seen answerWaitSeen
	if raw, err := os.ReadFile(filepath.Join(s.wait, "seen.json")); err == nil {
		_ = json.Unmarshal(raw, &seen)
	}
	return seen
}

// saveSeen merges with what is on disk. Two waits racing here can at worst
// drop an id and hand that answer over twice; delivery is at-least-once.
func (s *answerWaitStore) saveSeen(ids []string) error {
	if err := s.ensure(); err != nil {
		return err
	}
	seen := s.loadSeen()
	known := map[string]bool{}
	for _, id := range seen.IDs {
		known[id] = true
	}
	for _, id := range ids {
		if !known[id] {
			known[id] = true
			seen.IDs = append(seen.IDs, id)
		}
	}
	if len(seen.IDs) > answerWaitSeenCap {
		seen.IDs = seen.IDs[len(seen.IDs)-answerWaitSeenCap:]
	}
	seen.Baselined = true
	raw, err := json.Marshal(seen)
	if err != nil {
		return err
	}
	return writeAnswerWaitFile(filepath.Join(s.wait, "seen.json"), raw)
}

// recordAskForSession is best effort: an ask is never refused over local state.
func (a *App) recordAskForSession(cfg config.Resolved, askID, title string) *output.Warning {
	store, err := a.answerWaitStore(cfg)
	if err == nil {
		err = store.recordAsk(askID, title, time.Now())
	}
	if err != nil {
		return &output.Warning{Code: "answer_wait_unrecorded", Message: "could not record this ask for anx await --answers; wait on it with anx await " + askID}
	}
	return nil
}

// normalizeAskRef accepts every spelling the CLI prints for an ask: event:<id>,
// a bare id, and the inbox item ref <category>:<thread>:<subject>:<source event>
// with or without its inbox: prefix. The source event is the ask.
func normalizeAskRef(raw string) string {
	ref := strings.TrimSpace(raw)
	if strings.HasPrefix(ref, "event:") {
		return ref
	}
	parts := strings.Split(strings.TrimPrefix(ref, "inbox:"), ":")
	if len(parts) == 4 {
		switch parts[0] {
		case "ask", "review", "escalate":
			return "event:" + parts[3]
		}
	}
	return "event:" + ref
}

type answerWaitOptions struct {
	timeout time.Duration
	settle  time.Duration
	all     bool
}

type answerWaitSweep struct {
	found       map[string]map[string]any
	owed        map[string]string
	open        map[string]bool
	otherUnread int
	olderUnread int
}

// runAwaitAnswers blocks until this session has answers it has not been handed.
//
// Cost per poll: one GET /asks/<id> per ask this session still waits on, plus
// one bounded page of the actor's newest asks. Looking owned asks up by id is
// what finds an old ask answered late; that page is ordered by ask time, so
// paging would have to walk history. Answers to asks of unknown origin are found
// only through the page.
func (a *App) runAwaitAnswers(ctx context.Context, cfg config.Resolved, opts answerWaitOptions) (*commandResult, error) {
	waitCtx, cancel := context.WithTimeout(ctx, opts.timeout)
	defer cancel()
	store, err := a.answerWaitStore(cfg)
	if err != nil {
		return nil, errnorm.Wrap(errnorm.KindLocal, "answer_wait_state_failed", "cannot open local answer wait state", err)
	}
	actorID := ""
	pollOffset := 0
	seenState := store.loadSeen()
	seen := map[string]bool{}
	for _, id := range seenState.IDs {
		seen[id] = true
	}
	scope := "session"
	switch {
	case opts.all:
		scope = "all"
	case store.session == "":
		scope = "actor"
	}
	found := map[string]map[string]any{}
	owed := map[string]string{}
	baselined := seenState.Baselined
	var last *answerWaitSweep
	older := 0
	sweep := func() error {
		if actorID == "" {
			var err error
			if _, actorID, err = a.resolveOwnAsksActor(waitCtx, cfg); err != nil {
				return err
			}
		}
		current, err := a.sweepAnswers(waitCtx, cfg, store, actorID, seen, baselined, opts.all, pollOffset)
		if err != nil {
			return err
		}
		pollOffset += answerWaitPollBudget
		last = current
		for id, item := range current.found {
			found[id] = item
		}
		for id, ask := range current.owed {
			owed[id] = ask
		}
		if !baselined {
			// The first wait of a session starts after whatever was already
			// answered. Its own asks are exempt: they are pending, not baselined.
			baseline := []string{}
			for id := range current.owed {
				baseline = append(baseline, id)
			}
			older = current.olderUnread
			if err := store.saveSeen(baseline); err != nil {
				return errnorm.Wrap(errnorm.KindLocal, "answer_wait_state_failed", "cannot save local answer wait state", err)
			}
			for _, id := range baseline {
				seen[id] = true
			}
			owed = map[string]string{}
			baselined = true
		}
		return nil
	}
	deliver := func() (*commandResult, error) {
		ids := make([]string, 0, len(found))
		for id := range found {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool {
			left, right := anyString(asMap(found[ids[i]]["answer"])["at"]), anyString(asMap(found[ids[j]]["answer"])["at"])
			if left != right {
				return left < right
			}
			return ids[i] < ids[j]
		})
		answers := make([]any, 0, len(ids))
		for _, id := range ids {
			answers = append(answers, found[id])
		}
		data := map[string]any{"answers": answers, "count": len(answers), "scope": scope, "other_session_unread": last.otherUnread, "open_asks": len(last.open)}
		if older > 0 {
			data["older_unread_not_returned"] = older
		}
		result := &commandResult{Data: data}
		// Pending entries go only once the cursor is durable, so a failed save
		// repeats these answers instead of losing them.
		err := store.saveSeen(ids)
		if err == nil {
			for _, id := range ids {
				store.dropPending(anyString(found[id]["ask_id"]))
			}
		}
		if err != nil {
			result.Warnings = append(result.Warnings, output.Warning{Code: "answer_wait_state_failed", Message: "answers returned, but the local cursor could not be saved; the next wait may return them again"})
		}
		return result, nil
	}
	timedOut := func(cause error) error {
		details := map[string]any{"target": "answers", "timeout": opts.timeout.String(), "scope": scope, "continuation": "anx inbox list --unread shows every unread answer for this agent"}
		if last != nil {
			details["open_asks"] = len(last.open)
			details["other_session_unread"] = last.otherUnread
		}
		if cause != nil {
			details["last_error"] = cause.Error()
		}
		return errnorm.WithDetails(errnorm.New(errnorm.KindNetwork, "timeout", "await timed out waiting for answers"), details)
	}
	var quietUntil time.Time
	seenCount := 0
	lastErr := sweep()
	ticker := time.NewTicker(answerWaitPoll)
	defer ticker.Stop()
	for {
		if lastErr != nil && waitCtx.Err() == nil && !answerWaitRetryable(lastErr) {
			return nil, lastErr
		}
		if len(found) > seenCount {
			seenCount = len(found)
			quietUntil = time.Now().Add(opts.settle)
		}
		if len(found) > 0 && (opts.settle <= 0 || len(last.open) == 0 || !time.Now().Before(quietUntil)) {
			return deliver()
		}
		select {
		case <-waitCtx.Done():
			if len(found) > 0 {
				return deliver()
			}
			return nil, timedOut(lastErr)
		case <-ticker.C:
		}
		lastErr = sweep()
	}
}

// answerWaitRetryable keeps a long wait alive across an outage. Anything the
// server refused on purpose still ends the wait.
func answerWaitRetryable(err error) bool {
	var normalized *errnorm.Error
	if !errors.As(err, &normalized) {
		return false
	}
	if normalized.Kind == errnorm.KindNetwork {
		return true
	}
	// Recoverable also covers auth_required and not_found, which more waiting
	// cannot fix, so only the HTTP status decides here.
	details, _ := normalized.Details.(map[string]any)
	status, _ := details["status"].(int)
	return status >= 500 || status == http.StatusTooManyRequests
}

func (a *App) sweepAnswers(ctx context.Context, cfg config.Resolved, store *answerWaitStore, actorID string, seen map[string]bool, baselined, all bool, pollOffset int) (*answerWaitSweep, error) {
	sweep := &answerWaitSweep{found: map[string]map[string]any{}, owed: map[string]string{}, open: map[string]bool{}}
	pendingAsks := store.pending()
	for index, pending := range pendingAsks {
		// An ask outside this poll's turn is assumed still open.
		if len(pendingAsks) > answerWaitPollBudget && (index-pollOffset%len(pendingAsks)+len(pendingAsks))%len(pendingAsks) >= answerWaitPollBudget {
			sweep.open[pending.AskID] = true
			continue
		}
		result, err := a.invokeRawJSON(ctx, cfg, "await answer", "GET", "/asks/"+url.PathEscape(strings.TrimPrefix(pending.AskID, "event:")), nil)
		if err != nil {
			var normalized *errnorm.Error
			if errors.As(err, &normalized) && normalized.Code == "not_found" {
				recorded, parseErr := time.Parse(time.RFC3339Nano, pending.RecordedAt)
				if parseErr != nil || time.Since(recorded) > answerWaitOrphanAfter {
					store.dropPending(pending.AskID)
				}
				continue
			}
			return nil, err
		}
		body := commandResultBody(result)
		if anyString(body["status"]) == "open" {
			sweep.open[pending.AskID] = true
			continue
		}
		item := answerWaitItem(pending, body)
		sweep.found[answerWaitItemKey(item)] = item
	}
	items, _, err := a.fetchOwnAsksPageForActor(ctx, cfg, actorID, "", 200)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		askID := anyString(item["ask_id"])
		mine, other := store.origin(askID)
		if other && !all {
			if item["answer_unread"] == true {
				sweep.otherUnread++
			}
			continue
		}
		answer := asMap(item["answer"])
		if answer == nil {
			if !mine {
				sweep.open[askID] = true
			}
			continue
		}
		id := strings.TrimSpace(anyString(answer["response_event_id"]))
		if id == "" || seen[id] || sweep.found[id] != nil {
			continue
		}
		if mine && baselined {
			// Filed by this session, handed over, and its pending entry gone.
			continue
		}
		// Only what this wait could have been owed is baselined. Another session's
		// answer stays available to the first --all.
		if !baselined && !other {
			sweep.owed[id] = askID
			if item["answer_unread"] == true {
				sweep.olderUnread++
			}
			continue
		}
		sweep.found[id] = item
	}
	return sweep, nil
}

func answerWaitItemKey(item map[string]any) string {
	if id := strings.TrimSpace(anyString(asMap(item["answer"])["response_event_id"])); id != "" {
		return id
	}
	return anyString(item["ask_id"])
}

// answerWaitItem builds the inbox row shape from the single-ask read. A closed
// ask with no answer (withdrawn, expired) still ends the wait: asking again or
// moving on is the agent's call. Its answer is null.
func answerWaitItem(pending answerWaitPending, body map[string]any) map[string]any {
	item := map[string]any{"ask_id": pending.AskID, "title": pending.Title, "status": body["status"], "subject_ref": body["subject_ref"], "answer": nil}
	response := asMap(body["response"])
	id := strings.TrimSpace(anyString(response["response_event_id"]))
	switch anyString(body["status"]) {
	case "answered", "needs_context":
		item["answer"] = map[string]any{
			"text": response["response_text"], "outcome": response["outcome"], "responder": response["responding_actor_id"],
			"at": response["at"], "response_event_id": id, "response_event_ref": "event:" + id,
		}
	default:
		if id != "" {
			item["closed_event_ref"] = "event:" + id
		}
	}
	return item
}

func (a *App) annotateAnsweredOrigin(brief map[string]any, cfg config.Resolved) {
	store, err := a.answerWaitStore(cfg)
	if err != nil {
		return
	}
	for _, raw := range asSlice(brief["answered_for_you"]) {
		item := asMap(raw)
		askID := normalizeAskRef(anyString(item["ref"]))
		mine, other := store.origin(askID)
		switch {
		case mine:
			item["session_origin"] = "this_session"
			item["ask_id"] = askID
		case other:
			item["session_origin"] = "other_session"
		}
	}
}

func otherSessionAnswerError(askID string) error {
	return errnorm.Usage("other_session_answer", fmt.Sprintf("%s was filed by another session sharing this agent; marking it read hides it from that session's orient. Pass --force only if this session has acted on the answer", askID))
}
