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

/** Core's conflict for an ask that closed some other way: nothing to skip. */
const CLOSED_MESSAGES = [
  "Ask is no longer open",
  "this ask batch has already been answered",
];
const closedElsewhere = (error) =>
  error?.status === 409 &&
  error?.body?.error?.code === "conflict" &&
  CLOSED_MESSAGES.includes(error?.body?.error?.message);

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

export async function sendStandingMutation(client, entry) {
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
  if (entry.operation === RULING_REVOKE) {
    const result = await client.rulingsRevoke(entry.request.id);
    if (!result?.ruling?.id) throw new Error("Revoke receipt is incomplete.");
    return result;
  }
  return undefined;
}
