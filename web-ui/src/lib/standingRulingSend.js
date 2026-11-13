/**
 * Outbox senders for the Decide actions that keep Decide to decisions:
 *
 *   asks.not_a_decision  send an ask back as "not a decision", and when the
 *                        reader said "always", record that as a ruling
 *   rulings.create       make an answer already sent a standing ruling
 *   rulings.revoke       take a standing ruling back
 *
 * Each is replay-safe. Retiring replays by its request key. Creating a
 * ruling for a question replaces the live one for that question, so a
 * second send leaves one ruling. Revoking twice is a no-op in core.
 *
 * Returns undefined for any other operation, so the outbox falls through to
 * its own adapters.
 */
export const NOT_A_DECISION = "asks.not_a_decision";
export const RULING_CREATE = "rulings.create";
export const RULING_REVOKE = "rulings.revoke";

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
  if (entry.operation === RULING_REVOKE) {
    const result = await client.rulingsRevoke(entry.request.id);
    if (!result?.ruling?.id) throw new Error("Revoke receipt is incomplete.");
    return result;
  }
  return undefined;
}
