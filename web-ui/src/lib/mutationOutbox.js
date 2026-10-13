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
} from "$lib/localWorkStore.js";
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
  if (entry.operation !== "events.create")
    throw Object.assign(new Error("Unknown saved operation"), { status: 400 });
  return createLocalMutationClient(entry.scope).createEvent(entry.request);
}

function confirm(entry, result = null) {
  mutationOutbox.update((rows) => rows.filter((row) => row.key !== entry.key));
  mutationConfirmations.update((rows) => ({
    ...rows,
    [entry.target]: { ...entry, state: "sent", result },
  }));
  window.dispatchEvent(
    new CustomEvent("anx:mutation-confirmed", { detail: { ...entry, result } }),
  );
}

export async function enqueueDiscussionMessage({
  event,
  target,
  draft,
  href,
  title = "Saved comment",
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
  const key = crypto.randomUUID();
  const entry = snapshot({
    key,
    scope,
    epoch: localWorkEpoch(scope),
    operation: "events.create",
    request: { request_key: key, event },
    target,
    draft,
    href,
    title,
    text: draft?.text ?? event.payload?.text ?? "",
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
        if (!active(entry)) return;
        await saveLocalWork(entry.scope, "mutation", entry.key, null, {
          epoch: entry.epoch,
          deletionReason: "confirmed",
        });
        volatile.delete(entry.key);
        // Preserve any newer composer contents, even in another tab.
        const current = await loadLocalWork(entry.scope, "draft", entry.target);
        if (JSON.stringify(current) === JSON.stringify(entry.draft))
          await saveLocalWork(entry.scope, "draft", entry.target, null, {
            epoch: entry.epoch,
          });
        confirm(entry, result);
      },
    );
  } catch (error) {
    if (!active(entry)) return;
    entry.failure = mutationFailure(error);
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
          confirm(entry);
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
  // Transfer to a durable composer before superseding; retain the old exact
  // request for recovery history rather than silently discarding it.
  await saveLocalWork(entry.scope, "draft", entry.target, entry.draft, {
    epoch: entry.epoch,
  });
  await persist({ ...entry, state: "superseded" });
  volatile.delete(key);
  window.dispatchEvent(
    new CustomEvent("anx:edit-form", {
      detail: { scope: entry.scope, target: entry.target, value: entry.draft },
    }),
  );
  requestRecovery();
}

if (typeof window !== "undefined") {
  localWorkScope.subscribe(() => {
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
