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
} from "$lib/replayPolicy.js";

export const mutationOutbox = writable([]);
export const mutationConfirmations = writable({});
const volatile = new Map();
let timer;
let running = false;
let requested = false;

const snapshot = (value) => JSON.parse(JSON.stringify(value));
const active = (entry) =>
  entry.scope === get(localWorkScope) &&
  canUseLocalWorkScope(entry.scope) &&
  entry.epoch === localWorkEpoch(entry.scope);
const persist = (entry) =>
  saveLocalWork(entry.scope, "mutation", entry.key, entry, {
    epoch: entry.epoch,
  });
const visible = (entry) => entry.state !== "superseded";

/** Only audited adapters belong here. A new endpoint must first prove atomic
 * mutation/replay and changed-body key rejection in real core tests. */
async function send(entry) {
  if (!active(entry))
    throw new Error("The submitting identity is no longer active.");
  if (replayExpired(entry.createdAt)) throw expiredReplayError();
  const client = createLocalMutationClient(entry.scope);
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
      await persist(entry);
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
      await persist(entry);
      mutationOutbox.update((rows) =>
        rows.map((row) => (row.key === entry.key ? snapshot(entry) : row)),
      );
      window.dispatchEvent(
        new CustomEvent("anx:mutation-progress", { detail: snapshot(entry) }),
      );
    }
    return entry.receipt;
  }
  if (entry.operation === "topics.patch") {
    const result = await client.updateTopic(
      entry.request.id,
      entry.request.body,
    );
    if (result.topic?.id !== entry.request.id || !result.topic?.updated_at)
      throw new Error("Topic receipt is incomplete.");
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
      throw new Error("Task receipt is incomplete.");
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
      request_key: entry.key,
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
      await persist(entry);
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
    await persist(entry);
    commitInboxView(entry.scope, { decision });
    if (!entry.request.autoApply) return { decision };
    if (!active(entry))
      throw new Error("The submitting identity is no longer active.");
    let action = await client.dispatchPmDecision(decision.id);
    if (!action.id) throw new Error("Action receipt is incomplete.");
    if (action.status !== "failed") {
      if (!active(entry))
        throw new Error("The submitting identity is no longer active.");
      action = await client.reconcilePmAction(action.id);
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
  const key = crypto.randomUUID();
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
  const key = crypto.randomUUID();
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
          ["Board", draft.board],
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
    key: crypto.randomUUID(),
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
          request_key: crypto.randomUUID(),
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
  const key = crypto.randomUUID();
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
          request_key: crypto.randomUUID(),
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
    const key = crypto.randomUUID();
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
  key = crypto.randomUUID(),
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
              request_key: crypto.randomUUID(),
              replay_key: crypto.randomUUID(),
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
        replay_key: crypto.randomUUID(),
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
  const key = crypto.randomUUID();
  const actor_id = JSON.parse(get(localWorkScope))[3];
  return enqueueLocalMutation({
    ...options,
    key,
    operation: "pm.decision.answer",
    request: {
      decision_id: decisionId,
      autoApply,
      answer: { request_key: key, actor_id, revision, approve, text },
    },
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

async function attempt(entry) {
  if (!active(entry) || navigator.onLine === false) return;
  try {
    if (!navigator.locks)
      throw Object.assign(new Error("Web Locks are unavailable"), {
        status: 400,
        publicMessage:
          "Sending is paused in this browser. Your text is saved here.",
      });
    await navigator.locks.request(
      `anx:mutation-send:${entry.scope}`,
      async () => {
        if (!active(entry)) return;
        const latest = await loadLocalWorkRecord(
          entry.scope,
          "mutation",
          entry.key,
        );
        if (!latest || latest.deleted || latest.value.state === "superseded") {
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
          if ((await intentFingerprint(other.request)) !== fingerprint)
            continue;
          // Another tab owns the identical persisted submission. Adopt its
          // active request, including its original keys and checkpoints.
          await persist({
            ...entry,
            state: "superseded",
            adoptedKey: other.key,
          });
          volatile.delete(entry.key);
          return;
        }
        entry.state = "sending";
        entry.attempts += 1;
        await persist(entry);
        mutationOutbox.update((rows) =>
          rows.map((row) => (row.key === entry.key ? snapshot(entry) : row)),
        );
        if (!active(entry)) return;
        const result = adopted ? adopted.confirmation : await send(entry);
        if (entry.epoch !== localWorkEpoch(entry.scope)) return;
        await saveLocalWork(entry.scope, "mutation", entry.key, null, {
          epoch: entry.epoch,
          deletionReason: "confirmed",
          confirmation: result,
          submission: {
            target: entry.target,
            operation: entry.operation,
            fingerprint,
            confirmedAt: Date.now(),
          },
        });
        volatile.delete(entry.key);
        // Preserve any newer composer contents, even in another tab.
        const current = await loadLocalWork(
          entry.scope,
          "draft",
          entry.draftTarget || entry.target,
        );
        if (JSON.stringify(current) === JSON.stringify(entry.draft))
          await saveLocalWork(
            entry.scope,
            "draft",
            entry.draftTarget || entry.target,
            null,
            {
              epoch: entry.epoch,
            },
          );
        if (entry.operation === "events.create")
          await consumeUploadedAttachments(
            entry.scope,
            entry.request.event.refs,
            entry.epoch,
          );
        confirm(entry, result);
      },
    );
  } catch (error) {
    if (!active(entry)) return;
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
            : entry.operation === "events.lifecycle"
              ? "event action"
              : entry.operation === "docs.lifecycle"
                ? "document action"
                : entry.operation === "topics.patch"
                  ? "topic edit"
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
      await persist(entry);
      volatile.delete(entry.key);
    } catch (saveError) {
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
          .filter((record) => !record.deleted)
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
      key: crypto.randomUUID(),
      request: freshRequest(entry.request, entry.operation),
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
    entry.operation === "events.lifecycle"
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
                  !entry.receipt?.documents?.some((done) => done.id === doc.id),
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
