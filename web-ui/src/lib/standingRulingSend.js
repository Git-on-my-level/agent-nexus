/**
 * Outbox senders for the Decide actions that keep Decide to decisions:
 *
 *   asks.not_a_decision  send an ask back as "not a decision", and when the
 *                        reader said "always", record that as a ruling
 *   rulings.create       make an answer already sent a standing ruling
 *   rulings.revoke       take a standing ruling back
 *
 *   inbox.skip           clear a decision without deciding it (Decide's
 *                        Skip): outcome skipped, never a ruling
 *   inbox.skip_many      the same for many decisions, sent in chunks through
 *                        POST /inbox/responses with a receipt per item
 *
 * Each is replay-safe. Retiring replays by its request key. Creating a
 * ruling for a question replaces the live one for that question, so a
 * second send leaves one ruling. Revoking twice is a no-op in core. A skip
 * is an inbox response that replays by its idempotency key.
 *
 * Returns undefined for any other operation, so the outbox falls through to
 * its own adapters.
 */
export const NOT_A_DECISION = "asks.not_a_decision";
export const RULING_CREATE = "rulings.create";
export const RULING_REVOKE = "rulings.revoke";
export const SKIP = "inbox.skip";
export const SKIP_MANY = "inbox.skip_many";

/** Items per POST /inbox/responses; core accepts up to 100. */
export const SKIP_CHUNK = 25;

/** Core's conflict for an ask that closed some other way: nothing to skip. */
const CLOSED_MESSAGES = [
  "Ask is no longer open",
  "this ask batch has already been answered",
];
const closedElsewhere = (error) =>
  error?.status === 409 &&
  ((error?.body?.error?.code === "conflict" &&
    CLOSED_MESSAGES.includes(error?.body?.error?.message)) ||
    // This skip's own key already answered the ask: a skip has no words of
    // the reader's to lose, so it is done.
    error?.body?.error?.code === "idempotency_conflict");

/**
 * A skip tells the requester when it can be told. When core says the
 * requester can't be reached it is recorded without a wake, under its own
 * key: a changed body must never reuse the first one.
 */
async function sendSkip(
  client,
  { item_id: itemId, body, quiet_key: quietKey },
) {
  let result;
  try {
    result = await client.respondInboxItem(itemId, body);
  } catch (error) {
    if (closedElsewhere(error)) return { skipped: false, closed: true };
    if (
      error?.body?.error?.code !== "notification_target_required" ||
      body.notify_mode === "none" ||
      !quietKey
    )
      throw error;
    try {
      result = await client.respondInboxItem(itemId, {
        ...body,
        notify_mode: "none",
        idempotency_key: quietKey,
      });
    } catch (retryError) {
      if (closedElsewhere(retryError)) return { skipped: false, closed: true };
      throw retryError;
    }
  }
  if (!result?.event?.id && !result?.batch_id)
    throw new Error("Skip receipt is incomplete.");
  return { skipped: true };
}

const retryableStatus = (status) =>
  !status || status >= 500 || status === 408 || status === 429;

/**
 * No POST /inbox/responses here: an older core answers a generic 404/405,
 * and a hosted proxy that has not classified the route yet answers 404
 * route_not_available.
 */
const noBatchRoute = (error) =>
  (error?.status === 404 &&
    (error?.body?.error?.message === "endpoint not found" ||
      error?.body?.error?.code === "route_not_available")) ||
  error?.status === 405;

async function respondChunk(client, chunk) {
  try {
    const result = await client.respondInboxItems(
      chunk.map(({ item_id: itemId, body, quiet_key: quietKey }) => ({
        ...body,
        inbox_item_id: itemId,
        ...(quietKey ? { quiet_idempotency_key: quietKey } : {}),
      })),
    );
    if (
      !Array.isArray(result?.results) ||
      result.results.length !== chunk.length
    )
      throw new Error("Skip receipt is incomplete.");
    return result.results;
  } catch (error) {
    if (!noBatchRoute(error)) throw error;
    // One at a time, exactly as a single skip, on a core without the route.
    const results = [];
    for (const item of chunk) {
      try {
        const sent = await sendSkip(client, item);
        results.push({ status: sent.closed ? "closed" : "responded" });
      } catch (itemError) {
        if (retryableStatus(Number(itemError?.status || 0))) throw itemError;
        results.push({
          status: "failed",
          http_status: Number(itemError.status),
          error: itemError?.body?.error,
        });
      }
    }
    return results;
  }
}

/**
 * Skip many decisions: chunks go out until every item is done, and each
 * chunk's receipts are checkpointed so a retry sends only what is left.
 * Items core reports as deferred go in the next chunk. Items core refuses
 * leave the entry needing attention, naming how many and why; a transient
 * refusal retries with backoff.
 */
async function sendSkipMany(client, entry, checkpoint) {
  const items = entry.request.items ?? [];
  const done = { ...(entry.receipt?.done ?? {}) };
  const failed = new Map();
  for (;;) {
    const left = items.filter(
      (item) => !done[item.item_id] && !failed.has(item.item_id),
    );
    if (!left.length) break;
    const chunk = left.slice(0, SKIP_CHUNK);
    const results = await respondChunk(client, chunk);
    let progressed = false;
    results.forEach((result, index) => {
      const id = chunk[index].item_id;
      if (result?.status === "responded" || result?.status === "closed") {
        done[id] = result.status;
        progressed = true;
      } else if (result?.status === "failed") {
        failed.set(id, result);
        progressed = true;
      }
    });
    if (progressed) await checkpoint?.({ ...entry.receipt, done });
    else
      throw Object.assign(new Error("No skip was attempted; trying again."), {
        status: 503,
      });
  }
  if (!failed.size) return { skipped: Object.keys(done).length, done };
  const first = [...failed.values()][0];
  const status = Number(first?.http_status || 0);
  const reason = String(first?.error?.message || "refused").slice(0, 300);
  const count = failed.size;
  throw Object.assign(
    new Error(`${count} of ${items.length} skips failed: ${reason}`),
    retryableStatus(status)
      ? { status: status || 503 }
      : {
          status,
          publicMessage: `${count} ${count === 1 ? "skip" : "skips"} could not be sent (${reason}). The rest went through.`,
        },
  );
}

export async function sendStandingMutation(client, entry, checkpoint) {
  if (entry.operation === NOT_A_DECISION) {
    const { ask_ref: askRef, body, always } = entry.request;
    const retired = await client.asksRetire(askRef, body);
    if (!retired?.status) throw new Error("Retire receipt is incomplete.");
    if (!always) return { retired: true };
    const made = await client.rulingsCreate({
      ask_ref: askRef,
      kind: "not_a_decision",
    });
    if (!made?.ruling?.id) throw new Error("Ruling receipt is incomplete.");
    return { retired: true, ruling: made.ruling };
  }
  if (entry.operation === RULING_CREATE) {
    const made = await client.rulingsCreate(entry.request.body);
    if (!made?.ruling?.id) throw new Error("Ruling receipt is incomplete.");
    return made;
  }
  if (entry.operation === SKIP) return sendSkip(client, entry.request);
  if (entry.operation === SKIP_MANY)
    return sendSkipMany(client, entry, checkpoint);
  if (entry.operation === RULING_REVOKE) {
    const result = await client.rulingsRevoke(entry.request.id);
    if (!result?.ruling?.id) throw new Error("Revoke receipt is incomplete.");
    return result;
  }
  return undefined;
}
