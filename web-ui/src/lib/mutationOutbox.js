import { goto } from "$app/navigation";
import { get, writable } from "svelte/store";
import { createLocalMutationClient } from "$lib/coreClient.js";
import { localWorkScope, canUseLocalWorkScope } from "$lib/localWorkScope.js";
import { authenticatedAgent } from "$lib/authSession.js";
import { devActorMode } from "$lib/workspaceContext.js";
import {
  listLocalWork,
  loadLocalWork,
  loadLocalWorkRecord,
  saveLocalWork,
  localWorkEpoch,
  localWorkPrincipal,
  onLocalWorkChanged,
  protectLocalWorkPreparation,
  requestMutationSendLock,
} from "$lib/localWorkStore.js";
import {
  attachmentFile,
  saveUploadedAttachment,
  consumeUploadedAttachments,
  refreshUploadedAttachments,
  uploadedAttachments,
} from "$lib/attachmentWork.js";
import { commitInboxView } from "$lib/inboxViewCache.js";
import { isPmNotOnboardedRefusal, PM_STATES } from "$lib/pm/onboardingState.js";
import { publishPmPresence } from "$lib/pm/presence.js";
import { mutationFailure } from "$lib/mutationStatus.js";
import {
  replayExpired,
  expiredReplayError,
  intentFingerprint,
  freshRequest,
  autoApplyStageKey,
  randomUUID,
} from "$lib/replayPolicy.js";

export const mutationOutbox = writable([]);
export const mutationConfirmations = writable({});
const volatile = new Map();
const inFlightInTab = new Set();
const confirmedInTab = new Map();
let timer;
let running = false;
let requested = false;

const snapshot = (value) => JSON.parse(JSON.stringify(value));
const inTabKey = (entry) => `${entry.scope}\0${entry.epoch}\0${entry.key}`;
const active = (entry) =>
  entry.scope === get(localWorkScope) &&
  canUseLocalWorkScope(entry.scope) &&
  entry.epoch === localWorkEpoch(entry.scope);
const persist = (entry, lease = null) =>
  saveLocalWork(entry.scope, "mutation", entry.key, entry, {
    epoch: entry.epoch,
    lease,
  });
const visible = (entry) => entry.state !== "superseded";

/** Only audited adapters belong here. A new endpoint must first prove atomic
 * mutation/replay and changed-body key rejection in real core tests. */
async function send(entry, lease = null) {
  if (!active(entry))
    throw new Error("The submitting identity is no longer active.");
  if (replayExpired(entry.createdAt)) throw expiredReplayError();
  const client = createLocalMutationClient(entry.scope);
  if (entry.operation === "inbox.controls") {
    for (const item of entry.request.items) {
      if (
        entry.receipt?.controls?.some(
          (done) => done.key === item.body.request_key,
        )
      )
        continue;
      if (!active(entry))
        throw new Error("The submitting identity is no longer active.");
      let result;
      switch (item.action) {
        case "read":
          result = await client.markHomeRead(item.body);
          break;
        case "archive":
          result = await client.archiveCard(item.id, item.body);
          break;
        case "dispatch":
          result = await client.dispatchPmDecision(item.id, item.body);
          break;
        case "reconcile":
          result = await client.reconcilePmAction(item.id, item.body);
          break;
        case "acknowledge":
          result = await client.acknowledgePmAction(item.id, item.body);
          break;
        default:
          throw Object.assign(new Error("Unknown inbox control"), {
            status: 400,
          });
      }
      if (
        item.action === "read"
          ? !result.ok
          : item.action === "archive"
            ? !result.card?.id
            : result.id !== item.id && item.action !== "dispatch"
      )
        throw new Error("Inbox control receipt is incomplete.");
      if (item.action === "dispatch" && !result.id)
        throw new Error("Delivery admission receipt is incomplete.");
      if (entry.epoch !== localWorkEpoch(entry.scope))
        throw new Error("The submitting identity is no longer active.");
      if (item.action === "archive" && item.ref)
        commitInboxView(entry.scope, { archivedRef: item.ref });
      entry.receipt = {
        controls: [
          ...(entry.receipt?.controls || []),
          {
            key: item.body.request_key,
            id: item.id,
            action: item.action,
            result,
          },
        ],
      };
      await persist(entry, lease);
      mutationOutbox.update((rows) =>
        rows.map((row) => (row.key === entry.key ? snapshot(entry) : row)),
      );
      window.dispatchEvent(
        new CustomEvent("anx:mutation-progress", { detail: snapshot(entry) }),
      );
    }
    return entry.receipt;
  }
  if (entry.operation === "events.lifecycle") {
    const methods = {
      archive: "archiveEvent",
      unarchive: "unarchiveEvent",
      trash: "trashEvent",
      restore: "restoreEvent",
    };
    if (!entry.request.selected_bases && entry.request.items.length > 1)
      throw Object.assign(
        new Error(
          "This saved bulk request predates reply version checking. Edit the saved request before sending it.",
        ),
        {
          publicMessage:
            "Review and edit this saved bulk request before sending it; its selected reply versions need to be protected.",
        },
      );
    for (const item of entry.request.items) {
      if (entry.receipt?.events?.some((done) => done.id === item.id)) continue;
      if (!active(entry))
        throw new Error("The submitting identity is no longer active.");
      const method = methods[item.action];
      if (!method)
        throw Object.assign(new Error("Unknown event action"), { status: 400 });
      const result = await client[method](
        item.id,
        entry.request.selected_bases
          ? { ...item.body, selected_bases: entry.request.selected_bases }
          : item.body,
      );
      if (result.event?.id !== item.id)
        throw new Error("Event lifecycle receipt is incomplete.");
      if (!active(entry))
        throw new Error("The submitting identity is no longer active.");
      const covered = entry.request.items.filter(
        (candidate) =>
          candidate.action === item.action &&
          (candidate.id === item.id ||
            result.affected_event_ids?.includes(candidate.id)),
      );
      entry.receipt = {
        events: [
          ...(entry.receipt?.events || []),
          ...covered
            .filter(
              (candidate) =>
                !entry.receipt?.events?.some(
                  (done) => done.id === candidate.id,
                ),
            )
            .map((candidate) => ({ id: candidate.id, result })),
        ],
      };
      await persist(entry, lease);
      mutationOutbox.update((rows) =>
        rows.map((row) => (row.key === entry.key ? snapshot(entry) : row)),
      );
      window.dispatchEvent(
        new CustomEvent("anx:mutation-progress", { detail: snapshot(entry) }),
      );
    }
    return entry.receipt;
  }
  if (entry.operation === "docs.lifecycle") {
    const methods = {
      archive: "archiveDocument",
      unarchive: "unarchiveDocument",
      trash: "trashDocument",
      restore: "restoreDocument",
    };
    for (const item of entry.request.items) {
      if (entry.receipt?.documents?.some((done) => done.id === item.id))
        continue;
      if (!active(entry))
        throw new Error("The submitting identity is no longer active.");
      const method = methods[item.action];
      if (!method)
        throw Object.assign(new Error("Unknown document action"), {
          status: 400,
        });
      const result = await client[method](item.id, item.body);
      if (
        result.document?.id !== item.id ||
        !result.document?.updated_at ||
        !result.revision
      )
        throw new Error("Document lifecycle receipt is incomplete.");
      if (!active(entry))
        throw new Error("The submitting identity is no longer active.");
      entry.receipt = {
        documents: [
          ...(entry.receipt?.documents || []),
          { id: item.id, result },
        ],
      };
      await persist(entry, lease);
      mutationOutbox.update((rows) =>
        rows.map((row) => (row.key === entry.key ? snapshot(entry) : row)),
      );
      window.dispatchEvent(
        new CustomEvent("anx:mutation-progress", { detail: snapshot(entry) }),
      );
    }
    return entry.receipt;
  }
  if (entry.operation === "notifications.settings.set") {
    const result = await client.setNotificationSettings(entry.request.body);
    if (!Array.isArray(result?.channels))
      throw new Error("Notification settings receipt is incomplete.");
    return result;
  }
  if (entry.operation === "topics.patch") {
    const result = await client.updateTopic(
      entry.request.id,
      entry.request.body,
    );
    if (result.topic?.id !== entry.request.id || !result.topic?.updated_at)
      throw new Error("Project receipt is incomplete.");
    return result;
  }
  if (entry.operation === "cards.move") {
    const result = await client.moveBoardCard(
      entry.request.board,
      entry.request.id,
      entry.request.body,
    );
    if (!result.card?.id || !result.board?.updated_at)
      throw new Error("Move receipt is incomplete.");
    return result;
  }
  if (entry.operation === "curation.ungroup") {
    const result = await client.ungroupCurationBatch(entry.request.body);
    if (typeof result?.ungrouped !== "boolean")
      throw new Error("Ungroup receipt is incomplete.");
    return result;
  }
  if (entry.operation === "pm.decisions.create") {
    const decision = await client.createPmDecision(entry.request.body);
    if (!decision.id) throw new Error("Proposal receipt is incomplete.");
    return { decision };
  }
  if (entry.operation === "cards.patch") {
    const result = await client.updateBoardCard(
      "",
      entry.request.id,
      entry.request.body,
    );
    if (!result.card?.id) throw new Error("Assignment receipt is incomplete.");
    return result;
  }
  if (entry.operation === "work.create") {
    const result = await client.createWork(entry.request.body);
    if (!result.work?.ref && !result.work?.id)
      throw new Error("Initiative receipt is incomplete.");
    return result;
  }
  if (
    entry.operation === "docs.create" ||
    entry.operation === "docs.patch" ||
    entry.operation === "docs.revise"
  ) {
    const result =
      entry.operation === "docs.create"
        ? await client.createDocument(entry.request.body)
        : entry.operation === "docs.patch"
          ? await client.patchDocument(entry.request.id, entry.request.body)
          : await client.updateDocument(entry.request.id, entry.request.body);
    if (!result.document?.id && !result.document?.document_id)
      throw new Error("Document receipt is incomplete.");
    return result;
  }
  if (entry.operation === "artifacts.attachments.create") {
    const result = await client.createArtifactAttachment({
      refs: entry.request.refs,
      request_key: entry.request.request_key || entry.key,
      file: attachmentFile(entry.request),
    });
    if (!result?.artifact?.id)
      throw new Error("Attachment receipt is incomplete.");
    await saveUploadedAttachment(entry, result);
    return result;
  }
  if (entry.operation === "events.create")
    return client.createEvent(entry.request);
  if (entry.operation === "pm.chat.send") {
    let id = entry.receipt?.conversationId || entry.request.conversation_id;
    if (!id) {
      const conversation = await client.createPmConversation(
        entry.request.creation,
      );
      if (!conversation.id)
        throw new Error("Conversation receipt is incomplete.");
      id = conversation.id;
      entry.receipt = { ...entry.receipt, conversationId: id };
      await persist(entry, lease);
    }
    if (!active(entry))
      throw new Error("The submitting identity is no longer active.");
    const turn = await client.sendPmMessage(id, entry.request.message);
    if (!turn.id) throw new Error("Message receipt is incomplete.");
    return { conversationId: id, turn };
  }
  if (entry.operation === "pm.decision.answer") {
    const decision =
      entry.receipt?.decision ||
      (await client.answerPmDecision(
        entry.request.decision_id,
        entry.request.answer,
      ));
    if (!decision.id) throw new Error("Decision receipt is incomplete.");
    entry.receipt = { ...entry.receipt, decision };
    await persist(entry, lease);
    commitInboxView(entry.scope, { decision });
    if (!entry.request.autoApply) return { decision };
    if (!active(entry))
      throw new Error("The submitting identity is no longer active.");
    // Persist each stage's original key and base before sending. A replayed
    // dispatch receipt describes admission, so read the current action once to
    // capture the separate read-back base; never refresh an already saved base.
    if (!entry.request.autoDispatch) {
      const base = await client.getPmAction(decision.action_id);
      if (
        base.id !== decision.action_id ||
        !Number.isInteger(base.revision) ||
        !Number.isInteger(decision.revision)
      )
        throw new Error("Delivery base is incomplete.");
      if (!active(entry))
        throw new Error("The submitting identity is no longer active.");
      entry.request.autoDispatch = {
        request_key: await autoApplyStageKey(entry, "autoDispatch"),
        if_revision: decision.revision,
        if_action_revision: base.revision,
      };
      await persist(entry, lease);
    }
    let action = entry.receipt.dispatchedAction;
    if (!action) {
      action = await client.dispatchPmDecision(
        decision.id,
        entry.request.autoDispatch,
      );
      if (action.id !== decision.action_id)
        throw new Error("Action receipt is incomplete.");
      entry.receipt = { ...entry.receipt, dispatchedAction: action };
      await persist(entry, lease);
    }
    if (action.status !== "failed") {
      if (!active(entry))
        throw new Error("The submitting identity is no longer active.");
      if (!entry.request.autoReconcile) {
        const base = await client.getPmAction(action.id);
        if (base.id !== action.id || !Number.isInteger(base.revision))
          throw new Error("Read-back base is incomplete.");
        if (!active(entry))
          throw new Error("The submitting identity is no longer active.");
        entry.request.autoReconcile = {
          request_key: await autoApplyStageKey(entry, "autoReconcile"),
          if_revision: base.revision,
        };
        await persist(entry, lease);
      }
      action =
        entry.receipt.reconciledAction ||
        (await client.reconcilePmAction(
          action.id,
          entry.request.autoReconcile,
        ));
      if (action.id !== decision.action_id)
        throw new Error("Read-back receipt is incomplete.");
      entry.receipt = { ...entry.receipt, reconciledAction: action };
      await persist(entry, lease);
    }
    return { decision, action };
  }
  throw Object.assign(new Error("Unknown saved operation"), { status: 400 });
}

function confirm(entry, result = null) {
  const decision = result?.decision || entry.receipt?.decision;
  if (entry.operation === "pm.decision.answer" && decision)
    commitInboxView(entry.scope, { decision });
  mutationOutbox.update((rows) =>
    rows.filter((row) => row.key !== entry.key || row.scope !== entry.scope),
  );
  if (active(entry))
    mutationConfirmations.update((rows) => ({
      ...rows,
      [entry.target]: { ...entry, state: "sent", result },
    }));
  window.dispatchEvent(
    new CustomEvent("anx:mutation-confirmed", { detail: { ...entry, result } }),
  );
}

export function enqueueDiscussionMessage({ event, ...options }) {
  const key = randomUUID();
  return enqueueLocalMutation({
    ...options,
    key,
    operation: "events.create",
    request: { request_key: key, event },
  });
}

export function enqueueAuthoring({ operation, body, id = "", ...options }) {
  if (
    ![
      "work.create",
      "docs.create",
      "docs.patch",
      "docs.revise",
      "cards.patch",
      "topics.patch",
    ].includes(operation)
  )
    throw new Error("This authoring operation has not been audited.");
  const key = randomUUID();
  const draft = options.draft || {};
  const preview =
    options.preview ??
    (operation === "work.create"
      ? [
          ["Outcome", draft.title],
          ["Context", draft.summary],
          ["Acceptance criteria", draft.criteria],
          ["Accountable owner", draft.owner],
          ["Next actor", draft.nextActor],
          ["Next action", draft.nextAction],
          ["Due date", draft.due],
          ["Group", draft.board],
        ]
          .filter(([, value]) => value)
          .map(([label, value]) => `${label}: ${value}`)
          .join("\n")
      : operation === "docs.create"
        ? [draft.title, draft.summary, draft.content]
            .filter(Boolean)
            .join("\n\n")
        : (draft.content ?? draft.summary ?? draft.text ?? ""));
  return enqueueLocalMutation({
    ...options,
    key,
    operation,
    preview,
    request: {
      id,
      body: {
        ...body,
        request_key: key,
        actor_id: JSON.parse(get(localWorkScope))[3],
      },
    },
  });
}

export function inboxControlLabel(action) {
  return (
    {
      read: "Mark read",
      archive: "Archive initiative",
      dispatch: "Deliver approved instruction",
      reconcile: "Check source receipt",
      acknowledge: "Acknowledge delivery",
    }[action] || "Inbox action"
  );
}

export function enqueueInboxControls({ draft, ...options }) {
  if (!draft?.items?.length || draft.items.length > 100)
    throw new Error("Select at most 100 inbox controls.");
  const items = draft.items.map((item) => {
    if (
      !item.id ||
      !["read", "archive", "dispatch", "reconcile", "acknowledge"].includes(
        item.action,
      )
    )
      throw new Error("Review the saved inbox control.");
    const body = { ...item.body, request_key: randomUUID() };
    if (
      item.action === "read"
        ? body.group_ref !== item.id ||
          !body.expected_newest_event_cursor?.id ||
          !body.expected_newest_event_cursor?.ts
        : item.action === "archive"
          ? !Number.isInteger(body.if_version) ||
            body.if_version < 0 ||
            !body.if_lifecycle ||
            !("archived_at" in body.if_lifecycle) ||
            !("trashed_at" in body.if_lifecycle) ||
            typeof body.if_latest_observation_id !== "string"
          : !Number.isInteger(body.if_revision) ||
            body.if_revision < 1 ||
            (item.action === "dispatch" &&
              (!Number.isInteger(body.if_action_revision) ||
                body.if_action_revision < 1))
    )
      throw new Error("Load the current inbox item before saving this action.");
    return { ...item, body };
  });
  return enqueueLocalMutation({
    ...options,
    key: randomUUID(),
    operation: "inbox.controls",
    request: { items },
    draft,
    title: "Inbox request",
    preview: items
      .map((item) => `${inboxControlLabel(item.action)}: ${item.title}`)
      .join("\n"),
  });
}

export function enqueueEventLifecycle({ draft, ...options }) {
  const actor_id = JSON.parse(get(localWorkScope))[3];
  const events = snapshot(draft.events);
  if (
    !events.length ||
    events.some((event) => !event.id || !event.if_lifecycle)
  )
    throw new Error(
      "The original event versions are unavailable. Your request is retained.",
    );
  if (
    get(mutationOutbox).some(
      (entry) =>
        entry.operation === "events.lifecycle" &&
        entry.request.items.some((item) =>
          events.some((event) => event.id === item.id),
        ),
    )
  )
    throw new Error(
      "A saved request already includes this event. Confirm or edit it first.",
    );
  return enqueueLocalMutation({
    ...options,
    draft,
    key: randomUUID(),
    operation: "events.lifecycle",
    title: "Saved event action",
    preview: `${draft.action}: ${events.map((event) => event.title).join(", ")}\n${draft.reason || ""}`,
    request: {
      selected_bases: Object.fromEntries(
        events.map((event) => [event.id, event.if_lifecycle]),
      ),
      items: events.map((event) => ({
        id: event.id,
        action: draft.action,
        body: {
          actor_id,
          request_key: randomUUID(),
          if_lifecycle: event.if_lifecycle,
          ...(draft.action === "trash" ? { reason: draft.reason || "" } : {}),
        },
      })),
    },
  });
}

export function enqueueDocumentLifecycle({ draft, ...options }) {
  const scope = get(localWorkScope);
  const actor_id = JSON.parse(scope)[3];
  const documents = snapshot(draft.documents);
  if (!documents.length || documents.some((doc) => !doc.id || !doc.updated_at))
    throw new Error(
      "The original document version is unavailable. Your request is retained; use the latest version explicitly.",
    );
  if (
    get(mutationOutbox).some(
      (entry) =>
        entry.operation === "docs.lifecycle" &&
        entry.request.items.some((item) =>
          documents.some((doc) => doc.id === item.id),
        ),
    )
  )
    throw new Error(
      "A saved request already includes this document. Confirm or edit that request first.",
    );
  const key = randomUUID();
  return enqueueLocalMutation({
    ...options,
    draft,
    key,
    operation: "docs.lifecycle",
    title: "Saved document action",
    preview: `${draft.action}: ${documents.map((doc) => doc.title || doc.id).join(", ")}\n${draft.reason || ""}`,
    request: {
      items: documents.map((doc) => ({
        id: doc.id,
        action: draft.action,
        body: {
          actor_id,
          request_key: randomUUID(),
          if_updated_at: doc.updated_at,
          ...(["trash", "restore"].includes(draft.action)
            ? { reason: draft.reason || "" }
            : {}),
        },
      })),
    },
  });
}

export async function enqueueAttachment({ file, refs, target, href }) {
  const scope = get(localWorkScope);
  const epoch = localWorkEpoch(scope);
  const finish = protectLocalWorkPreparation(scope);
  try {
    const buffer = file.arrayBuffer
      ? await file.arrayBuffer()
      : await new Promise((resolve, reject) => {
          const reader = new FileReader();
          reader.onload = () => resolve(reader.result);
          reader.onerror = () => reject(reader.error);
          reader.readAsArrayBuffer(file);
        });
    const bytes = new Uint8Array(buffer);
    let raw = "";
    for (let offset = 0; offset < bytes.length; offset += 32768)
      raw += String.fromCharCode(...bytes.subarray(offset, offset + 32768));
    if (scope !== get(localWorkScope) || epoch !== localWorkEpoch(scope))
      throw new Error("The identity selecting this file is no longer active.");
    const key = randomUUID();
    return await enqueueLocalMutation({
      key,
      operation: "artifacts.attachments.create",
      target: `upload:${target}:${key}`,
      href,
      title: "Saved attachment",
      draft: { text: file.name },
      request: {
        composerTarget: target,
        refs,
        request_key: key,
        file: { name: file.name, type: file.type, bytes: btoa(raw) },
      },
    });
  } finally {
    finish();
  }
}

export function enqueuePmMessage({
  conversationId = "",
  text,
  workRef = "",
  contextRefs = [],
  key = randomUUID(),
  ...options
}) {
  const actor_id = JSON.parse(get(localWorkScope))[3];
  return enqueueLocalMutation({
    ...options,
    key,
    operation: "pm.chat.send",
    request: {
      conversation_id: conversationId,
      ...(!conversationId
        ? {
            creation: {
              request_key: randomUUID(),
              replay_key: randomUUID(),
              actor_id,
              title: text.slice(0, 100),
              ...(workRef ? { work_ref: workRef } : {}),
              ...(contextRefs.length ? { context_refs: contextRefs } : {}),
            },
          }
        : {}),
      // Opt in only when building a new intent. Recovery sends saved request
      // bodies verbatim, including legacy entries without receipt keys.
      message: {
        request_key: key,
        replay_key: randomUUID(),
        actor_id,
        text,
      },
    },
  });
}

export function enqueuePmDecisionAnswer({
  decisionId,
  revision,
  approve,
  text,
  autoApply = false,
  ...options
}) {
  const key = randomUUID();
  const actor_id = JSON.parse(get(localWorkScope))[3];
  return enqueueLocalMutation({
    ...options,
    key,
    operation: "pm.decision.answer",
    request: {
      decision_id: decisionId,
      autoApply,
      ...(autoApply
        ? {
            autoDispatchKey: randomUUID(),
            autoReconcileKey: randomUUID(),
          }
        : {}),
      answer: { request_key: key, actor_id, revision, approve, text },
    },
  });
}

/**
 * Undo an accepted batch. Ungrouping by key is idempotent in core, so a
 * retry of the saved request is replay-safe.
 */
export function enqueueCurationUngroup({ key: batchKey, ...options }) {
  return enqueueLocalMutation({
    ...options,
    key: randomUUID(),
    operation: "curation.ungroup",
    request: { body: { key: batchKey } },
  });
}

export async function enqueueLocalMutation({
  draftTarget,
  operation,
  request,
  key,
  target,
  draft,
  href,
  title = "Saved response",
  preview,
}) {
  const scope = get(localWorkScope);
  if (!canUseLocalWorkScope(scope))
    throw new Error("Sign in before saving a message.");
  const entry = snapshot({
    key,
    scope,
    epoch: localWorkEpoch(scope),
    operation,
    request,
    target,
    draftTarget: draftTarget || target,
    draft,
    href,
    title,
    text:
      preview ??
      draft?.text ??
      draft?.content ??
      draft?.summary ??
      JSON.stringify(draft, null, 2),
    state: "pending",
    attempts: 0,
    nextAt: Date.now(),
    createdAt: Date.now(),
  });
  // Publish before awaiting to close repeated-submit races. No network until
  // both this write and the per-scope lock have completed.
  volatile.set(key, entry);
  mutationOutbox.update((rows) => [...rows, snapshot(entry)]);
  try {
    await persist(entry);
    volatile.delete(key);
  } catch (error) {
    entry.state = "attention";
    entry.failure = mutationFailure(
      Object.assign(error, { localPersistence: true }),
    );
    requested = true;
    await recoverMutations();
    throw error;
  }
  requestRecovery();
  return entry;
}

/**
 * Drain one persisted outbox item. Cross-tab lease acquisition is a best-effort
 * optimization only; if two tabs enter send concurrently for the same item,
 * both transmit the request_key / replay_key persisted when the item was
 * enqueued, so server-side replay produces at most one effect. If storage is
 * unavailable during/after send, in-tab tracking (inFlightInTab,
 * confirmedInTab) still sends at most once per tab with the persisted key.
 */
async function attempt(entry) {
  if (!active(entry) || navigator.onLine === false) return;
  const tabKey = inTabKey(entry);
  if (confirmedInTab.has(tabKey) || inFlightInTab.has(tabKey)) return;
  try {
    await requestMutationSendLock(entry.scope, async (lease) => {
      try {
        await attemptLocked(lease);
      } catch (error) {
        if (error?.leaseLost || lease?.isHeld?.() === false || !active(entry))
          throw error;
        // Checkpoint the failure before releasing the send lock. While the
        // send is in flight the persisted row says "sending" with a nextAt
        // that is already due; a tab waiting on this lock would otherwise
        // read that row and resend at once, skipping the backoff and
        // inflating the attempt count that sets the next delay.
        await recordFailure(error, lease);
      }
    });
  } catch (error) {
    if (!active(entry)) return;
    if (error?.leaseLost) {
      confirmedInTab.delete(tabKey);
      const latest = await loadLocalWorkRecord(
        entry.scope,
        "mutation",
        entry.key,
      );
      if (latest?.deleted && latest.deletionReason === "confirmed") {
        confirmedInTab.set(tabKey, latest.confirmation ?? null);
        volatile.delete(entry.key);
        confirm(entry, latest.confirmation ?? null);
        return;
      }
      volatile.delete(entry.key);
      clearTimeout(timer);
      timer = setTimeout(requestRecovery, 250);
      return;
    }
    await recordFailure(error);
  }

  async function attemptLocked(lease) {
    if (
      !active(entry) ||
      confirmedInTab.has(tabKey) ||
      inFlightInTab.has(tabKey)
    )
      return;
    const latest = await loadLocalWorkRecord(
      entry.scope,
      "mutation",
      entry.key,
    );
    if (latest?.deleted) {
      if (latest.deletionReason === "confirmed") {
        confirmedInTab.set(tabKey, latest.confirmation ?? null);
        volatile.delete(entry.key);
        confirm(entry, latest.confirmation ?? null);
      } else {
        volatile.delete(entry.key);
      }
      return;
    }
    if (!latest || latest.value.state === "superseded") {
      volatile.delete(entry.key);
      return;
    }
    const saved = latest.value;
    if (saved.state === "attention" || saved.nextAt > Date.now()) return;
    entry = saved;
    if (replayExpired(entry.createdAt)) throw expiredReplayError();
    const fingerprint = await intentFingerprint(entry.request);
    const records = await listLocalWork(entry.scope, "mutation", {
      includeDeleted: true,
    });
    let adopted = null;
    for (const record of records) {
      if (record.target === entry.key) continue;
      if (record.deleted) {
        if (
          record.deletionReason === "confirmed" &&
          record.submission?.confirmedAt >= entry.createdAt &&
          record.submission?.target === entry.target &&
          record.submission.operation === entry.operation &&
          record.submission.fingerprint === fingerprint
        ) {
          adopted = record;
          break;
        }
        continue;
      }
      const other = record.value;
      if (
        !visible(other) ||
        replayExpired(other.createdAt) ||
        other.target !== entry.target ||
        other.operation !== entry.operation
      )
        continue;
      if (
        other.createdAt > entry.createdAt ||
        (other.createdAt === entry.createdAt && other.key > entry.key)
      )
        continue;
      if ((await intentFingerprint(other.request)) !== fingerprint) continue;
      // Another tab owns the identical persisted submission. Adopt its
      // active request, including its original keys and checkpoints.
      await persist(
        {
          ...entry,
          state: "superseded",
          adoptedKey: other.key,
        },
        lease,
      );
      volatile.delete(entry.key);
      return;
    }
    entry.state = "sending";
    entry.attempts += 1;
    await persist(entry, lease);
    inFlightInTab.add(tabKey);
    mutationOutbox.update((rows) =>
      rows.map((row) => (row.key === entry.key ? snapshot(entry) : row)),
    );
    try {
      if (!active(entry)) return;
      const result = adopted ? adopted.confirmation : await send(entry, lease);
      confirmedInTab.set(tabKey, result);
      if (entry.epoch !== localWorkEpoch(entry.scope)) return;
      try {
        await saveLocalWork(entry.scope, "mutation", entry.key, null, {
          epoch: entry.epoch,
          lease,
          deletionReason: "confirmed",
          confirmation: result,
          submission: {
            target: entry.target,
            operation: entry.operation,
            fingerprint,
            confirmedAt: Date.now(),
          },
        });
      } catch (confirmSaveErr) {
        if (!confirmSaveErr?.localPersistence) throw confirmSaveErr;
      }
      volatile.delete(entry.key);
      // Preserve any newer composer contents, even in another tab.
      const current = await loadLocalWork(
        entry.scope,
        "draft",
        entry.draftTarget || entry.target,
      );
      if (JSON.stringify(current) === JSON.stringify(entry.draft)) {
        try {
          await saveLocalWork(
            entry.scope,
            "draft",
            entry.draftTarget || entry.target,
            null,
            {
              epoch: entry.epoch,
              lease,
            },
          );
        } catch (draftErr) {
          if (draftErr?.leaseLost) throw draftErr;
        }
      }
      if (entry.operation === "events.create") {
        lease?.assertHeld();
        await consumeUploadedAttachments(
          entry.scope,
          entry.request.event.refs,
          entry.epoch,
        );
      }
      confirm(entry, result);
    } finally {
      inFlightInTab.delete(tabKey);
    }
  }

  async function recordFailure(error, lease = null) {
    const latest = await loadLocalWorkRecord(
      entry.scope,
      "mutation",
      entry.key,
    );
    if (latest?.deleted && latest.deletionReason === "confirmed") {
      confirmedInTab.set(tabKey, latest.confirmation ?? null);
      volatile.delete(entry.key);
      confirm(entry, latest.confirmation ?? null);
      return;
    }
    if (
      entry.operation === "pm.decisions.create" &&
      isPmNotOnboardedRefusal(error)
    ) {
      publishPmPresence(JSON.parse(entry.scope)[1], PM_STATES.NOT_ONBOARDED);
      error.publicMessage =
        "This workspace has no PM to carry out this source move. Set up your PM, then retry. Your destination and evidence are saved.";
    }
    entry.failure = mutationFailure(
      error,
      entry.operation === "artifacts.attachments.create"
        ? "file"
        : ["cards.move", "pm.decisions.create"].includes(entry.operation)
          ? "move"
          : entry.operation === "cards.patch"
            ? "assignment"
            : entry.operation === "inbox.controls"
              ? "inbox action"
              : entry.operation === "events.lifecycle"
                ? "event action"
                : entry.operation === "docs.lifecycle"
                  ? "document action"
                  : entry.operation === "topics.patch"
                    ? "topic edit"
                    : entry.operation === "notifications.settings.set"
                      ? "notification settings"
                      : "response",
    );
    entry.state = entry.failure.state;
    entry.nextAt =
      Date.now() +
      Math.min(
        60_000,
        2_000 * 2 ** Math.min(5, Math.max(0, entry.attempts - 1)),
      );
    volatile.set(entry.key, entry);
    try {
      await persist(entry, lease);
      volatile.delete(entry.key);
    } catch (saveError) {
      if (saveError?.leaseLost) throw saveError;
      entry.state = "attention";
      entry.failure = mutationFailure(
        Object.assign(saveError, { localPersistence: true }),
      );
    }
  }
}

function requestRecovery() {
  requested = true;
  if (!running) void recoverMutations();
}

export async function recoverMutations() {
  if (running) {
    requested = true;
    return;
  }
  running = true;
  clearTimeout(timer);
  try {
    do {
      requested = false;
      const scope = get(localWorkScope);
      await refreshUploadedAttachments(scope);
      if (!canUseLocalWorkScope(scope)) {
        mutationOutbox.set([]);
        break;
      }
      const records = await listLocalWork(scope, "mutation", {
        includeDeleted: true,
      });
      if (scope !== get(localWorkScope) || !canUseLocalWorkScope(scope))
        continue;
      // A tab can mount after a batch already confirmed elsewhere. Recover
      // its delivery display from the persisted receipt, without replaying
      // confirmation events, clearing drafts or issuing historical refreshes.
      mutationConfirmations.update((previous) => {
        const next = { ...previous };
        for (const record of records) {
          const receipt = record.confirmation;
          const submitted = record.submission;
          if (
            !record.deleted ||
            record.deletionReason !== "confirmed" ||
            submitted?.operation !== "docs.lifecycle" ||
            !Array.isArray(receipt?.documents)
          )
            continue;
          const current = next[submitted.target];
          if (
            (current?.confirmedAt ?? current?.createdAt ?? 0) >
            submitted.confirmedAt
          )
            continue;
          next[submitted.target] = {
            key: record.target,
            scope,
            epoch: record.epoch,
            target: submitted.target,
            operation: submitted.operation,
            state: "sent",
            confirmedAt: submitted.confirmedAt,
            receipt,
            result: receipt,
            request: { items: receipt.documents.map(({ id }) => ({ id })) },
          };
        }
        return next;
      });
      const rows = new Map(
        records
          .filter(
            (record) =>
              !record.deleted && !confirmedInTab.has(inTabKey(record.value)),
          )
          .map((record) => [record.target, record.value]),
      );
      for (const [key, entry] of volatile) {
        if (!active(entry)) continue;
        const latest = await loadLocalWorkRecord(scope, "mutation", key);
        if (latest?.deleted) volatile.delete(key);
        else rows.set(key, entry);
      }
      for (const entry of get(mutationOutbox)) {
        if (!active(entry) || rows.has(entry.key)) continue;
        const latest = await loadLocalWorkRecord(scope, "mutation", entry.key);
        if (latest?.deleted && latest.deletionReason === "confirmed")
          confirm(entry, latest.confirmation);
      }
      let entries = [...rows.values()]
        .filter((entry) => active(entry) && visible(entry))
        .sort((a, b) => a.createdAt - b.createdAt);
      for (const entry of entries) {
        if (replayExpired(entry.createdAt) && !entry.failure?.expired) {
          entry.state = "attention";
          entry.failure = mutationFailure(expiredReplayError());
          volatile.set(entry.key, entry);
          try {
            await persist(entry);
            volatile.delete(entry.key);
          } catch {
            /* Retain the visible request; sending remains blocked. */
          }
        }
      }
      // Expiry checkpoints yield to storage; a principal may change while they
      // complete. Never publish the old principal's recovered plaintext late.
      entries = entries.filter(active);
      mutationOutbox.set(entries.map(snapshot));
      if (navigator.onLine !== false) {
        const due = entries.find(
          (entry) => entry.state !== "attention" && entry.nextAt <= Date.now(),
        );
        if (due) {
          await attempt(due);
          requested = true;
          continue;
        }
      }
      const next = entries
        .filter((entry) => entry.state !== "attention")
        .map((entry) => entry.nextAt);
      if (next.length && navigator.onLine !== false)
        timer = setTimeout(
          requestRecovery,
          Math.max(250, Math.min(...next) - Date.now()),
        );
    } while (requested);
  } finally {
    running = false;
    if (requested) requestRecovery();
  }
}

/**
 * Retire a saved mutation after its replacement is already persisted.
 * The record stays as `superseded` so a crash cannot drop the only copy
 * before the new intent is durable.
 */
export async function supersedeLocalMutation(
  key,
  { replacementKey = "" } = {},
) {
  const entry = get(mutationOutbox).find((row) => row.key === key);
  if (!entry || !active(entry) || entry.state === "superseded") return false;
  const saved = {
    ...snapshot(entry),
    state: "superseded",
    ...(replacementKey ? { replacementKey } : {}),
  };
  await persist(saved);
  volatile.delete(key);
  mutationOutbox.update((rows) =>
    rows.filter((row) => row.key !== key || row.scope !== entry.scope),
  );
  requestRecovery();
  return true;
}

export async function retryMutation(key, { asNew = false } = {}) {
  const entry = get(mutationOutbox).find((row) => row.key === key);
  if (!entry || !active(entry)) return;
  if (replayExpired(entry.createdAt)) {
    if (!asNew) {
      requestRecovery();
      return;
    }
    const replacement = {
      ...snapshot(entry),
      key: randomUUID(),
      request: freshRequest(entry.request, entry.operation, entry.receipt),
      createdAt: Date.now(),
      state: "pending",
      failure: null,
      attempts: 0,
      nextAt: Date.now(),
    };
    // Checkpoint the new intent before superseding the old. If interrupted,
    // the old one stays in attention and can never drain automatically.
    try {
      await persist(replacement);
      await persist({
        ...entry,
        state: "superseded",
        replacementKey: replacement.key,
      });
      volatile.delete(key);
    } catch (error) {
      if (!active(entry)) return;
      entry.state = "attention";
      entry.failure = {
        ...mutationFailure(Object.assign(error, { localPersistence: true })),
        expired: true,
      };
      volatile.set(key, entry);
    }
    requestRecovery();
    return;
  }
  entry.state = "pending";
  entry.nextAt = Date.now();
  volatile.set(key, entry);
  try {
    await persist(entry);
    volatile.delete(key);
  } catch (error) {
    entry.state = "attention";
    entry.failure = mutationFailure(
      Object.assign(error, { localPersistence: true }),
    );
  }
  requestRecovery();
}

export async function editMutation(key) {
  const entry = get(mutationOutbox).find((row) => row.key === key);
  if (!entry || !active(entry) || entry.state !== "attention") return;
  if (entry.operation === "artifacts.attachments.create") {
    await persist({ ...entry, state: "superseded" });
    volatile.delete(key);
    requestRecovery();
    return;
  }
  // Transfer to a durable composer before superseding; retain the old exact
  // request for recovery history rather than silently discarding it.
  const currentDraft = [
    "docs.lifecycle",
    "topics.patch",
    "events.lifecycle",
  ].includes(entry.operation)
    ? await loadLocalWork(
        entry.scope,
        "draft",
        entry.draftTarget || entry.target,
      )
    : null;
  const lifecycleDraft =
    currentDraft &&
    currentDraft.action === entry.draft?.action &&
    JSON.stringify(currentDraft.documents?.map((doc) => doc.id)) ===
      JSON.stringify(entry.draft.documents?.map((doc) => doc.id))
      ? currentDraft
      : entry.draft;
  const eventDraft =
    currentDraft &&
    currentDraft.action === entry.draft?.action &&
    JSON.stringify(currentDraft.events?.map((event) => event.id)) ===
      JSON.stringify(entry.draft.events?.map((event) => event.id))
      ? currentDraft
      : entry.draft;
  const draft =
    entry.operation === "inbox.controls"
      ? {
          ...entry.draft,
          items: entry.draft.items.filter(
            (_, index) =>
              !entry.receipt?.controls?.some(
                (done) =>
                  done.key === entry.request.items[index].body.request_key,
              ),
          ),
          confirmed: [
            ...(entry.draft.confirmed || []),
            ...(entry.receipt?.controls || []),
          ],
        }
      : entry.operation === "events.lifecycle"
        ? {
            ...eventDraft,
            events: eventDraft.events.filter(
              (event) =>
                !entry.receipt?.events?.some((done) => done.id === event.id),
            ),
            confirmed: [
              ...(entry.draft.confirmed || []),
              ...(entry.receipt?.events || []),
            ],
          }
        : entry.operation === "topics.patch"
          ? currentDraft || entry.draft
          : entry.operation === "docs.lifecycle"
            ? {
                ...lifecycleDraft,
                documents: lifecycleDraft.documents.filter(
                  (doc) =>
                    !entry.receipt?.documents?.some(
                      (done) => done.id === doc.id,
                    ),
                ),
                confirmed: [
                  ...(entry.draft.confirmed || []),
                  ...(entry.receipt?.documents || []),
                ],
              }
            : entry.receipt?.conversationId
              ? { ...entry.draft, conversationId: entry.receipt.conversationId }
              : entry.draft;
  // Start the synchronous draft journal, then restore the visible composer
  // before yielding. A late restore after persistence could replace text the
  // reader typed while the transfer was in flight. Supersede only after both
  // durable writes succeed; a storage failure retains the old request too.
  const savingDraft = saveLocalWork(
    entry.scope,
    "draft",
    entry.draftTarget || entry.target,
    draft,
    {
      epoch: entry.epoch,
    },
  );
  const unhandled =
    active(entry) &&
    window.dispatchEvent(
      new CustomEvent("anx:edit-form", {
        cancelable: true,
        detail: {
          scope: entry.scope,
          target: entry.draftTarget || entry.target,
          value: draft,
        },
      }),
    );
  await savingDraft;
  await persist({ ...entry, state: "superseded" });
  volatile.delete(key);
  if (!active(entry)) return;
  if (unhandled && entry.href) await goto(entry.href);

  requestRecovery();
}

if (typeof window !== "undefined") {
  localWorkScope.subscribe(() => {
    uploadedAttachments.set([]);
    mutationOutbox.set([]);
    mutationConfirmations.set({});
    requestRecovery();
  });
  authenticatedAgent.subscribe(requestRecovery);
  devActorMode.subscribe(requestRecovery);
  onLocalWorkChanged(requestRecovery);
  window.addEventListener("online", requestRecovery);
  window.addEventListener("focus", requestRecovery);
  window.addEventListener("anx:local-work-purged", (event) => {
    for (const [key, entry] of volatile)
      if (localWorkPrincipal(entry.scope) === event.detail?.principal)
        volatile.delete(key);
    mutationOutbox.set([]);
    mutationConfirmations.set({});
    requestRecovery();
  });
}
