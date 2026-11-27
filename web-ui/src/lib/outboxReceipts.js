import { PINS_OPERATION } from "$lib/dashboardPins.js";

/**
 * A tab can mount after a batch already confirmed elsewhere. Recover its
 * delivery display from the persisted receipts, without replaying
 * confirmation events, clearing drafts or issuing historical refreshes.
 */
export function recoveredReceipts(previous, records, scope) {
  const next = { ...previous };
  for (const record of records) {
    const receipt = record.confirmation;
    const submitted = record.submission;
    if (!record.deleted || record.deletionReason !== "confirmed") continue;
    const current = next[submitted?.target];
    if (
      (current?.confirmedAt ?? current?.createdAt ?? 0) > submitted?.confirmedAt
    )
      continue;
    // Another tab's pin edit landed: its receipt is core's newer set.
    if (
      submitted?.operation === PINS_OPERATION &&
      Array.isArray(receipt?.document_refs)
    ) {
      next[submitted.target] = {
        key: record.target,
        scope,
        epoch: record.epoch,
        target: submitted.target,
        operation: submitted.operation,
        state: "sent",
        confirmedAt: submitted.confirmedAt,
        result: receipt,
      };
      continue;
    }
    if (
      submitted?.operation !== "docs.lifecycle" ||
      !Array.isArray(receipt?.documents)
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
}
