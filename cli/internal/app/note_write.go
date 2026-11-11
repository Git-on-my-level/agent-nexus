package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"agent-nexus-cli/internal/config"
	"agent-nexus-cli/internal/errnorm"
	"agent-nexus-cli/internal/output"
)

// Note writes are idempotent and bounded.
//
// A note used to be sent without a request_key, so a write that outlived the
// caller's shell limit could not be retried without risking a duplicate, and
// a busy workspace writer (SQLite busy, 503 "busy") made the command wait out
// every stage's own budget. Now each note carries a request_key that is stored
// on disk before the first send and reused, with the exact body, by every
// rerun of the same note until the server confirms it; core replays a
// confirmed key instead of posting twice. A note whose key cannot be stored
// is not sent (the repo rule: work that is not persisted is not sent), and an
// unconfirmed key never expires: dropping it would let a rerun post twice.
// Busy and uncertain answers are retried inside the client timeout, and a
// failure says rerunning is safe.

const (
	noteRetryBackoffBase = time.Second
	noteRetryBackoffMax  = 5 * time.Second
	// notePresenceBudget bounds the best-effort presence update after a note.
	notePresenceBudget = 5 * time.Second
)

type pendingNote struct {
	Key     string         `json:"request_key"`
	Body    map[string]any `json:"body"`
	Created time.Time      `json:"created_at"`
}

type pendingNoteStore struct{ dir string }

func (a *App) pendingNotes(cfg config.Resolved) (*pendingNoteStore, error) {
	dir := cfg.ConfigDir
	if dir == "" {
		home, err := a.UserHomeDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(home, ".config", "anx")
	}
	return &pendingNoteStore{dir: filepath.Join(dir, "pending-notes", answerWaitDigest(strings.TrimRight(cfg.BaseURL, "/")))}, nil
}

// noteFingerprint names one intended note: the same writer, card and input.
// Anything the CLI derives at send time (a kept promise's fresh checkpoint)
// is excluded, so a rerun finds the stored body instead of building a new one.
func noteFingerprint(cfg config.Resolved, parsed noteArgs) string {
	raw, _ := json.Marshal([]any{cfg.As, cfg.ActorID, parsed.cardRef, parsed.text, parsed.nextSet, parsed.nextWhen, parsed.nextWhat, parsed.evidence, parsed.met, parsed.decidedBy})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (s *pendingNoteStore) path(fingerprint string) string {
	return filepath.Join(s.dir, fingerprint+".json")
}

// load returns the stored unconfirmed send of this note, however old: its
// outcome is unknown, so only its own key may send it again.
func (s *pendingNoteStore) load(fingerprint string) (pendingNote, bool) {
	raw, err := os.ReadFile(s.path(fingerprint))
	if err != nil {
		return pendingNote{}, false
	}
	var p pendingNote
	if json.Unmarshal(raw, &p) != nil || p.Key == "" || p.Body == nil {
		return pendingNote{}, false
	}
	return p, true
}

func (s *pendingNoteStore) save(fingerprint string, p pendingNote) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, ".note-")
	if err != nil {
		return err
	}
	_, err = tmp.Write(raw)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), s.path(fingerprint))
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

func (s *pendingNoteStore) clear(fingerprint string) { _ = os.Remove(s.path(fingerprint)) }

// noteWriteRetryable: the server did not apply the note (busy writer, 5xx) or
// the outcome is unknown. Both are safe to resend with the same request_key.
func noteWriteRetryable(err error) bool {
	var e *errnorm.Error
	if !errors.As(err, &e) {
		return false
	}
	switch e.Code {
	case "busy", "outcome_unknown", "network_error", "request_failed", "timeout", "storage_unavailable":
		return true
	}
	if details, ok := e.Details.(map[string]any); ok {
		if status, ok := details["status"].(int); ok {
			return status == 408 || status == 429 || status >= 500
		}
	}
	return e.Kind == errnorm.KindNetwork
}

// postNote sends the note event. A stored pending note with the same
// fingerprint is resent as stored; otherwise body is stored first.
// The returned time is when a resumed note was first attempted (zero if new).
func (a *App) postNote(ctx context.Context, cfg config.Resolved, parsed noteArgs, body map[string]any) (*commandResult, time.Time, error) {
	var resumed time.Time
	fingerprint := noteFingerprint(cfg, parsed)
	store, err := a.pendingNotes(cfg)
	if err != nil {
		return nil, resumed, noteKeyUnsaved("", err)
	}
	if p, ok := store.load(fingerprint); ok {
		body, resumed = p.Body, p.Created
		if resumed.IsZero() {
			resumed = time.Unix(0, 0).UTC()
		}
	} else {
		key := make([]byte, 16)
		if _, err := rand.Read(key); err != nil {
			return nil, resumed, errnorm.Local("request_key_failed", "failed to allocate note request key")
		}
		body["request_key"] = "note:" + hex.EncodeToString(key)
		if err := store.save(fingerprint, pendingNote{Key: anyString(body["request_key"]), Body: body, Created: a.clockNow().UTC()}); err != nil {
			return nil, resumed, noteKeyUnsaved(store.dir, err)
		}
	}
	key := anyString(body["request_key"])
	budget := cfg.Timeout
	if budget <= 0 {
		budget = config.DefaultTimeout
	}
	deadline := time.Now().Add(budget)
	backoff := noteRetryBackoffBase
	for attempt := 1; ; attempt++ {
		attemptCfg := cfg
		attemptCfg.Timeout = time.Until(deadline)
		result, err := a.invokeTypedJSON(ctx, attemptCfg, "note", "events.create", nil, nil, body)
		if err == nil {
			store.clear(fingerprint)
			return result, resumed, nil
		}
		if !noteWriteRetryable(err) {
			store.clear(fingerprint) // Definitive refusal: nothing was written.
			return nil, resumed, err
		}
		wait := backoff
		if time.Until(deadline) <= wait+time.Second || ctx.Err() != nil {
			return nil, resumed, noteNotConfirmed(err, key, attempt)
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, resumed, noteNotConfirmed(err, key, attempt)
		case <-timer.C:
		}
		backoff = min(2*backoff, noteRetryBackoffMax)
	}
}

func noteNotConfirmed(cause error, key string, attempts int) error {
	e := errnorm.Normalize(cause)
	details, _ := e.Details.(map[string]any)
	if details == nil {
		details = map[string]any{}
	}
	details["request_key"], details["attempts"], details["safe_to_rerun"] = key, attempts, true
	e.Details = details
	e.Hint = "The note was not confirmed. Rerun the same anx note command: it resends this note with the same request_key, so it cannot post twice. Raise --timeout if the workspace stays busy."
	return e
}

// noteKeyUnsaved refuses to send a note whose request_key could not be stored:
// an unconfirmed send could then never be retried without risking a duplicate.
func noteKeyUnsaved(dir string, cause error) error {
	e := errnorm.Local("note_key_unsaved", "note not sent: its retry key could not be stored locally ("+cause.Error()+")")
	e.Details = map[string]any{"dir": dir, "write_applied": false}
	e.Hint = "Nothing was written. Make the anx config directory writable (or set ANX_CONFIG_DIR to a writable directory) and rerun the same command."
	return e
}

// startNotedCard applies the note rule: a note that promises a next step or
// cites evidence is progress, so a card that has not started (backlog or
// ready) starts, exactly as anx work start would, when the writer owns it or
// nobody does. A card someone else owns stays put, with a warning naming the
// owner. Failures never undo the note; they warn with the command to run.
func (a *App) startNotedCard(ctx context.Context, cfg config.Resolved, card map[string]any, cardRef, writer string) (map[string]any, []output.Warning) {
	phase := strings.TrimSpace(anyString(card["column_key"]))
	if phase != "backlog" && phase != "ready" {
		return nil, nil
	}
	startArgv := []string{"anx", "work", "start", cardRef}
	owners := stringList(card["assignee_refs"])
	if len(owners) == 0 {
		if owner := strings.TrimSpace(anyString(card["assignee"])); owner != "" {
			owners = []string{owner}
		}
	}
	self := ""
	if writer = strings.TrimPrefix(strings.TrimSpace(writer), "actor:"); writer != "" {
		self = "actor:" + writer
	}
	mine := false
	for _, owner := range owners {
		if owner == self || strings.TrimPrefix(owner, "actor:") == writer {
			mine = true
		}
	}
	if len(owners) > 0 && !mine {
		return nil, []output.Warning{noteWarning("note_card_not_started", "note posted, but "+cardRef+" is still "+phase+": it belongs to "+strings.Join(owners, ", ")+", who starts it; to take it over run anx work start", startArgv)}
	}
	if self == "" {
		return nil, []output.Warning{noteWarning("note_card_not_started", "note posted, but "+cardRef+" is still "+phase+" (writer identity unknown); start it with anx work start", startArgv)}
	}
	assigned := false
	if len(owners) == 0 {
		patch := map[string]any{"patch": map[string]any{"assignee_refs": []string{self}}, "if_updated_at": card["updated_at"]}
		if err := finalizeOptionalMutationBodyActorID(patch, cfg); err == nil {
			_, err = a.invokeTypedJSONWithIDResolution(ctx, cfg, "cards assign", "cards.patch", "card_id", cardRef, cardIDLookupSpec, nil, patch)
			assigned = err == nil
		}
		if !assigned {
			return nil, []output.Warning{noteWarning("note_start_failed", "note posted, but "+cardRef+" could not be assigned to you and is still "+phase+"; run anx work start", startArgv)}
		}
	}
	if err := a.dailyMove(ctx, cfg, cardRef, "in_progress"); err != nil {
		return nil, []output.Warning{noteWarning("note_start_failed", "note posted, but "+cardRef+" could not move to in_progress and is still "+phase+"; run anx work start", startArgv)}
	}
	started := map[string]any{"from": phase, "to": "in_progress", "reason": "a note with --next or --evidence starts a card that has not started"}
	if assigned {
		started["assigned"] = self
	}
	return started, nil
}

// notePresence is a convenience; it never holds a note write for long.
func (a *App) notePresence(ctx context.Context, cfg config.Resolved, cardRef, text string) {
	presenceCfg := cfg
	presenceCfg.Timeout = min(notePresenceBudget, max(cfg.Timeout, time.Second))
	_, _ = a.invokeRawJSON(ctx, presenceCfg, "note presence", "PATCH", "/agents/me/presence", map[string]any{"current_card_ref": cardRef, "note": truncateRunes(text, 500)})
}

// keptPromise is the promise an evidence note re-states. Evidence satisfies
// every earlier note promise in the fold, so a note that names no next would
// otherwise blank the card's clock. Core's fold decides what is live; this only
// reads it. A lapsed promise keeps its text with a fresh checkpoint, because the
// evidence just proved movement.
func keptPromise(card map[string]any, now time.Time) (at time.Time, what string, ok bool) {
	health := asMap(card["plan_health"])
	state := anyString(health["state"])
	if len(health) == 0 || state == "done" {
		return time.Time{}, "", false
	}
	promise := asMap(health["promise"])
	fresh := now.Add(promiseHorizon).UTC()
	switch anyString(promise["source"]) {
	case "note":
		at, err := time.Parse(time.RFC3339Nano, anyString(promise["at"]))
		if err != nil || !at.After(now) {
			at = fresh
		}
		return at.UTC(), anyString(promise["what"]), true
	case "", "default":
		// A digest card reports on its own schedule; only a real note promise
		// on one needs keeping.
		if state == "digest" {
			return time.Time{}, "", false
		}
		return fresh, "report progress", true
	}
	// Cadence and plan promises live outside notes; evidence cannot clear them.
	return time.Time{}, "", false
}
