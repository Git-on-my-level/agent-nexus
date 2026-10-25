package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"agent-nexus-core/internal/actors"
	"agent-nexus-core/internal/auth"
	"agent-nexus-core/internal/delivery"
	"agent-nexus-core/internal/primitives"
	"agent-nexus-core/internal/router"

	"github.com/google/uuid"
)

// WorkTrackingClock delivers the one default reminder and one owner digest
// per UTC day. Bookkeeping is durable; retries reuse the delivery identity.
type WorkTrackingClock struct {
	Store                      *primitives.Store
	AuthStore                  *auth.Store
	Deliveries                 delivery.Store
	WorkspaceID, WorkspaceName string
	Interval                   time.Duration
	Notify                     func(context.Context, primitives.TrackingNotice) error
}

func (c *WorkTrackingClock) Run(ctx context.Context) {
	interval := c.Interval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := c.Step(ctx, time.Now().UTC()); err != nil && ctx.Err() == nil {
			log.Printf("work tracking clock: %v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *WorkTrackingClock) Step(ctx context.Context, now time.Time) error {
	if c.Store == nil {
		return nil
	}
	if err := c.Store.MaintainWorkTracking(ctx, now); err != nil {
		return err
	}
	reminders, err := c.Store.PendingTrackingReminders(ctx)
	if err != nil {
		return err
	}
	digests, err := c.Store.PendingTrackingDigests(ctx, now)
	if err != nil {
		return err
	}
	var failures []error
	for _, notice := range append(reminders, digests...) {
		valid, err := c.Store.ValidateTrackingNotice(ctx, notice)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		if len(valid.Cards) > 0 {
			notify := c.Notify
			if notify == nil {
				notify = c.notify
			}
			if err = notify(ctx, valid); err != nil {
				failures = append(failures, err)
				continue
			}
		}
		// A canceled/now-inaccessible snapshot is acknowledged without exposing
		// it. Its card note and digest responsibility remain durable.
		if notice.Kind == "default_promise_lapsed" {
			err = c.Store.AckTrackingReminder(ctx, notice, now)
		} else {
			err = c.Store.AckTrackingDigest(ctx, notice, now)
		}
		if err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (c *WorkTrackingClock) notify(ctx context.Context, n primitives.TrackingNotice) error {
	if c.AuthStore == nil {
		return nil
	} // Card notes and brief still work in local dev.
	actor := strings.TrimPrefix(n.Owner, "actor:")
	p, err := c.AuthStore.PrincipalForActor(ctx, actor)
	if errors.Is(err, auth.ErrAgentNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	refs := []string{}
	for _, card := range n.Cards {
		refs = append(refs, card.Ref)
	}
	title := "Default promise elapsed"
	text := "Default 12-hour promise elapsed. Record progress or hand off. Further reminders are included in the daily digest."
	if n.Kind == "work_tracking_digest" {
		title = "Daily work tracking digest"
		text = fmt.Sprintf("Daily work tracking digest: %d card(s) need ownership or an explicit promise. This is a bounded preview; use anx work list for the full inventory.", len(n.Cards))
	}
	for _, card := range n.Cards {
		text += "\n" + card.Ref + ": " + card.Title + " (" + card.Reason + ")"
	}
	if n.EventID == "" {
		// The digest event is a clearly marked automatic note, never evidence.
		// A stable owner/day ID binds the delivery to all preview contributors.
		n.EventID = uuid.NewSHA1(uuid.NameSpaceURL, []byte(n.Key)).String()
		_, err = c.Store.AppendEvent(ctx, actors.SystemActorID, map[string]any{"id": n.EventID, "type": "message_posted", "thread_id": n.ThreadID, "summary": title, "refs": refs, "payload": map[string]any{"automatic": true, "source": "work_tracking_digest", "text": text}})
		if err != nil && !errors.Is(err, primitives.ErrConflict) {
			return err
		}
	}
	if p.PrincipalKind == string(auth.PrincipalKindAgent) {
		return c.queueAgent(ctx, n, p.Username, text, refs)
	}
	if c.Deliveries.DB == nil {
		return nil
	}
	kind := delivery.KindPush
	if n.Kind == "work_tracking_digest" {
		kind = delivery.KindDigest
	}
	settings, err := c.Deliveries.Settings(ctx, actor)
	if err != nil {
		return err
	}
	options, err := c.Deliveries.ChannelOptions(ctx)
	if err != nil {
		return err
	}
	channels := map[string]bool{}
	for _, setting := range settings {
		if (kind == delivery.KindPush && setting.PushEnabled) || (kind == delivery.KindDigest && setting.DigestEnabled) {
			channels[setting.Channel] = true
		}
	}
	for channel, option := range options {
		if option.Addressing == delivery.AddressingPrincipal && option.Accepts(kind) {
			channels[channel] = true
		}
	}
	for channel := range channels {
		if option, ok := options[channel]; ok && !option.Accepts(kind) {
			continue
		}
		_, err = c.Store.EnqueueTrackingDelivery(ctx, n, delivery.Request{IdempotencyKey: n.Key + ":" + channel, PrincipalID: actor, Channel: channel, Kind: kind, SourceRef: "event:" + n.EventID, Payload: map[string]any{"type": n.Kind, "title": title, "text": text, "cards": n.Cards, "has_more": true, "workspace": c.WorkspaceName}}, time.Now().UTC())
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *WorkTrackingClock) queueAgent(ctx context.Context, n primitives.TrackingNotice, handle, text string, refs []string) error {
	actor := strings.TrimPrefix(n.Owner, "actor:")
	workspace := c.WorkspaceID
	if workspace == "" {
		workspace = "ws_main"
	}
	// The ID deliberately excludes the first preview card/thread, so pages
	// for one owner on the same day can never enqueue separate digest wakes.
	wakeID := router.WakeupArtifactID(workspace, "", n.Key, actor)
	wakeRefs := append(append([]string{}, refs...), "event:"+n.EventID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := c.Store.CreateArtifact(ctx, actors.SystemActorID, map[string]any{"id": wakeID, "kind": router.WakeArtifactKind, "summary": n.Kind + " for @" + handle, "refs": wakeRefs, "target_handle": handle, "target_actor_id": actor, "workspace_id": workspace, "thread_id": n.ThreadID}, map[string]any{"version": router.WakePacketVersion, "wakeup_id": wakeID, "wake_reason": n.Kind, "target_handle": handle, "target_actor_id": actor, "workspace_id": workspace, "thread_id": n.ThreadID, "trigger_event_id": n.EventID, "trigger_created_at": now, "trigger_text": text, "subject_ref": n.Cards[0].Ref}, "structured")
	if err != nil && !errors.Is(err, primitives.ErrConflict) {
		return err
	}
	// Check the snapshot once more after artifact I/O. The queue and its
	// delivery mirror already deduplicate wakeID atomically.
	valid, err := c.Store.ValidateTrackingNotice(ctx, n)
	if err != nil {
		return err
	}
	if len(valid.Cards) != len(n.Cards) {
		return nil
	}
	_, err = c.Store.UpsertTrackingWakeup(ctx, n, primitives.AgentWakeup{WakeupID: wakeID, Status: primitives.AgentWakeupStatusRequested, TargetHandle: handle, TargetActorID: actor, WorkspaceID: workspace, WorkspaceName: c.WorkspaceName, ThreadID: n.ThreadID, TriggerEventID: n.EventID, TriggerCreatedAt: now, TriggerText: text, Refs: append(wakeRefs, "artifact:"+wakeID)})
	return err
}
