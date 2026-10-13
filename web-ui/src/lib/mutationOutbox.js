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
import { mutationFailure } from "$lib/mutationStatus.js";

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
  const client = createLocalMutationClient(entry.scope);
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
              actor_id,
              title: text.slice(0, 100),
              ...(workRef ? { work_ref: workRef } : {}),
              ...(contextRefs.length ? { context_refs: contextRefs } : {}),
            },
          }
        : {}),
      message: { request_key: key, actor_id, text },
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

async function enqueueLocalMutation({
  operation,
  request,
  key,
  target,
  draft,
  href,
  title = "Saved response",
}) {
  const scope = get(localWorkScope);
  if (!canUseLocalWorkScope(scope))
    throw new Error("Sign in before saving a message.");
  // One uncertain post per composer avoids a second key for a double-click.
  if (
    get(mutationOutbox).some(
      (entry) => entry.target === target && visible(entry),
    )
  )
    throw new Error("Review the saved message before posting another.");
  const entry = snapshot({
    key,
    scope,
    epoch: localWorkEpoch(scope),
    operation,
    request,
    target,
    draft,
    href,
    title,
    text: draft?.text ?? "",
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
        entry.state = "sending";
        entry.attempts += 1;
        await persist(entry);
        mutationOutbox.update((rows) =>
          rows.map((row) => (row.key === entry.key ? snapshot(entry) : row)),
        );
        const result = await send(entry);
        if (entry.epoch !== localWorkEpoch(entry.scope)) return;
        await saveLocalWork(entry.scope, "mutation", entry.key, null, {
          epoch: entry.epoch,
          deletionReason: "confirmed",
          confirmation: result,
        });
        volatile.delete(entry.key);
        // Preserve any newer composer contents, even in another tab.
        const current = await loadLocalWork(entry.scope, "draft", entry.target);
        if (JSON.stringify(current) === JSON.stringify(entry.draft))
          await saveLocalWork(entry.scope, "draft", entry.target, null, {
            epoch: entry.epoch,
          });
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
    entry.failure = mutationFailure(
      error,
      entry.operation === "artifacts.attachments.create" ? "file" : "response",
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
      const rows = new Map(
        (await listLocalWork(scope, "mutation")).map((record) => [
          record.target,
          record.value,
        ]),
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

export async function retryMutation(key) {
  const entry = get(mutationOutbox).find((row) => row.key === key);
  if (!entry || !active(entry)) return;
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
  const draft = entry.receipt?.conversationId
    ? { ...entry.draft, conversationId: entry.receipt.conversationId }
    : entry.draft;
  await saveLocalWork(entry.scope, "draft", entry.target, draft, {
    epoch: entry.epoch,
  });
  await persist({ ...entry, state: "superseded" });
  volatile.delete(key);
  window.dispatchEvent(
    new CustomEvent("anx:edit-form", {
      detail: { scope: entry.scope, target: entry.target, value: draft },
    }),
  );
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
