/**
 * Outbox senders for bulk event and document lifecycle (archive, unarchive,
 * trash, restore):
 *
 *   events.lifecycle  sent in chunks through POST /events/lifecycle, with the
 *                     selection's frozen reply bases once per chunk
 *   docs.lifecycle    sent in chunks through POST /docs/lifecycle
 *
 * Each item keeps the body and request key it was saved with, and core runs
 * it through the single route's rules, so a resend through either route
 * replays instead of conflicting. Receipts keep their shape
 * (`receipt.events` / `receipt.documents`) and are checkpointed once per
 * chunk, so a retry sends only what is left. A lone unfinished item, and
 * every item on a core or hosted proxy without the batch route, goes through
 * its single route, as it always has.
 */

/** Items per batch request; core accepts up to 100. */
export const LIFECYCLE_CHUNK = 25;

const EVENT_METHODS = {
  archive: "archiveEvent",
  unarchive: "unarchiveEvent",
  trash: "trashEvent",
  restore: "restoreEvent",
};
const DOCUMENT_METHODS = {
  archive: "archiveDocument",
  unarchive: "unarchiveDocument",
  trash: "trashDocument",
  restore: "restoreDocument",
};

const inactive = () =>
  new Error("The submitting identity is no longer active.");

/**
 * No batch lifecycle route here: an older core answers a generic 404/405,
 * and a hosted proxy that has not classified the route yet answers 404
 * route_not_available.
 */
const noBatchRoute = (error) =>
  (error?.status === 404 &&
    (error?.body?.error?.message === "endpoint not found" ||
      error?.body?.error?.code === "route_not_available")) ||
  error?.status === 405;

/**
 * The error the single route's client call would have thrown for this item,
 * so the outbox classifies a batch item's failure exactly as before.
 */
function itemFailure(result, path) {
  const status = Number(result?.http_status || 0) || undefined;
  const body = result?.error ? { error: result.error } : undefined;
  const details = body?.error?.message || "";
  const error = new Error(
    body?.error?.code === "quota_exceeded"
      ? details
      : `anx-core request failed: POST ${path} (${status ?? "unknown"})${details ? ` - ${details}` : ""}`,
  );
  return Object.assign(error, { status, details, body });
}

const eventReceiptOk = (item, result) => result?.event?.id === item.id;
const documentReceiptOk = (item, result) =>
  result?.document?.id === item.id &&
  Boolean(result?.document?.updated_at) &&
  Boolean(result?.revision);

/** Receipts that one event result covers: itself and its reply cascade. */
function eventCoverage(entry, item, result, receipted) {
  return entry.request.items
    .filter(
      (candidate) =>
        candidate.action === item.action &&
        (candidate.id === item.id ||
          result.affected_event_ids?.includes(candidate.id)) &&
        !receipted.has(candidate.id),
    )
    .map((candidate) => ({ id: candidate.id, result }));
}

const kinds = {
  "events.lifecycle": {
    field: "events",
    methods: EVENT_METHODS,
    label: "event",
    noun: "Event",
    path: "/events/lifecycle",
    batch: (client, items, entry) =>
      client.changeEventsLifecycle(items, entry.request.selected_bases),
    single: (client, method, item, entry) =>
      client[method](
        item.id,
        entry.request.selected_bases
          ? { ...item.body, selected_bases: entry.request.selected_bases }
          : item.body,
      ),
    ok: eventReceiptOk,
    coverage: eventCoverage,
  },
  "docs.lifecycle": {
    field: "documents",
    methods: DOCUMENT_METHODS,
    label: "document",
    noun: "Document",
    path: "/docs/lifecycle",
    batch: (client, items) => client.changeDocumentsLifecycle(items),
    single: (client, method, item) => client[method](item.id, item.body),
    ok: documentReceiptOk,
    coverage: (entry, item, result) => [{ id: item.id, result }],
  },
};

/** Whether sendLifecycle sends this saved operation. */
export const isLifecycleOperation = (operation) =>
  Object.hasOwn(kinds, operation);

function methodFor(kind, item) {
  const method = kind.methods[item.action];
  if (!method)
    throw Object.assign(new Error(`Unknown ${kind.label} action`), {
      status: 400,
    });
  return method;
}

/** One item at a time through its single route, checkpointing each. */
async function sendOneByOne(client, entry, kind, { active, checkpoint }) {
  for (const item of entry.request.items) {
    const done = entry.receipt?.[kind.field] || [];
    if (done.some((receipt) => receipt.id === item.id)) continue;
    if (!active()) throw inactive();
    const result = await kind.single(
      client,
      methodFor(kind, item),
      item,
      entry,
    );
    if (!kind.ok(item, result))
      throw new Error(`${kind.noun} lifecycle receipt is incomplete.`);
    if (!active()) throw inactive();
    const receipted = new Set(done.map((receipt) => receipt.id));
    await checkpoint({
      [kind.field]: [...done, ...kind.coverage(entry, item, result, receipted)],
    });
  }
}

/**
 * Send one chunk through the batch route. Returns false when this core has
 * no batch route, so the caller falls back to single sends.
 */
async function sendChunk(client, entry, kind, chunk, { active, checkpoint }) {
  let response;
  try {
    response = await kind.batch(
      client,
      chunk.map((item) => ({ ...item.body, id: item.id, action: item.action })),
      entry,
    );
  } catch (error) {
    if (noBatchRoute(error)) return false;
    throw error;
  }
  const results = response?.results;
  if (!Array.isArray(results) || results.length !== chunk.length)
    throw new Error(`${kind.noun} lifecycle receipt is incomplete.`);
  if (!active()) throw inactive();
  const done = [...(entry.receipt?.[kind.field] || [])];
  const receipted = new Set(done.map((receipt) => receipt.id));
  let attempted = false;
  // Successes first: an item an earlier or later reply cascade in this chunk
  // already covered is done, whatever its own attempt reported.
  results.forEach((result, index) => {
    if (result?.status !== "done") return;
    attempted = true;
    const item = chunk[index];
    if (!kind.ok(item, result.response))
      throw new Error(`${kind.noun} lifecycle receipt is incomplete.`);
    for (const receipt of kind.coverage(
      entry,
      item,
      result.response,
      receipted,
    )) {
      receipted.add(receipt.id);
      done.push(receipt);
    }
  });
  const failedAt = results.findIndex(
    (result, index) =>
      result?.status === "failed" && !receipted.has(chunk[index].id),
  );
  if (done.length > (entry.receipt?.[kind.field]?.length || 0))
    await checkpoint({ [kind.field]: done });
  if (failedAt >= 0) throw itemFailure(results[failedAt], kind.path);
  if (!attempted && results.every((result) => result?.status === "deferred"))
    throw Object.assign(
      new Error(`No ${kind.label} change was attempted; trying again.`),
      { status: 503 },
    );
  return true;
}

/**
 * Send a saved bulk lifecycle request. `active` reports whether the
 * submitting identity is still signed in; `checkpoint` persists a receipt.
 */
export async function sendLifecycle(client, entry, { active, checkpoint }) {
  const kind = kinds[entry.operation];
  if (
    entry.operation === "events.lifecycle" &&
    !entry.request.selected_bases &&
    entry.request.items.length > 1
  )
    throw Object.assign(
      new Error(
        "This saved bulk request predates reply version checking. Edit the saved request before sending it.",
      ),
      {
        publicMessage:
          "Review and edit this saved bulk request before sending it; its selected reply versions need to be protected.",
      },
    );
  // The receipt is the entry's own: set it before persisting, so the next
  // chunk and a retry both see what is already done.
  const save = (receipt) => {
    entry.receipt = receipt;
    return checkpoint(receipt);
  };
  const io = { active, checkpoint: save };
  for (;;) {
    const receipted = new Set(
      (entry.receipt?.[kind.field] || []).map((receipt) => receipt.id),
    );
    const left = entry.request.items.filter((item) => !receipted.has(item.id));
    if (!left.length) break;
    if (!active()) throw inactive();
    const chunk = left.slice(0, LIFECYCLE_CHUNK);
    for (const item of chunk) methodFor(kind, item);
    // One item left gains nothing from a batch: it takes its single route.
    if (
      left.length === 1 ||
      !(await sendChunk(client, entry, kind, chunk, io))
    ) {
      await sendOneByOne(client, entry, kind, io);
      break;
    }
  }
  return entry.receipt;
}
