import { isNexusOwned, workKey } from "$lib/pm/presentation.js";
import {
  isTaskMove,
  taskMoveDraft,
  taskMoveTarget,
} from "$lib/taskMoveWork.js";

/**
 * Where a Watch row should sit while a move is unconfirmed.
 * A live outbox entry wins. A confirmation holds the destination until the
 * brief itself shows it. Attention (a move core rejected) does not.
 * @returns {string | null}
 */
export function watchPlacement(row, outbox, confirmations) {
  const target = taskMoveTarget({ ref: row?.ref });
  const matches = (outbox ?? []).filter(
    (item) =>
      item?.target === target &&
      item.state !== "superseded" &&
      isTaskMove(item),
  );
  const live = matches.find((item) => item.state !== "attention");
  const phase = live?.draft?.phase;
  if (phase && phase !== row?.placement) return phase;
  if (matches.some((item) => item.state === "attention")) return null;
  const confirmed = confirmations?.[target];
  const confirmedPhase = confirmed?.draft?.phase;
  if (
    confirmed &&
    isTaskMove(confirmed) &&
    confirmedPhase &&
    confirmedPhase !== row?.placement
  )
    return confirmedPhase;
  return null;
}

/** Operator-facing reason for a rejected move of one of these initiatives. */
export function watchMoveAttention(outbox, refs) {
  const wanted = new Set(refs ?? []);
  for (const ref of wanted) {
    const target = taskMoveTarget({ ref });
    const matches = (outbox ?? []).filter(
      (item) =>
        item?.target === target &&
        item.state !== "superseded" &&
        isTaskMove(item),
    );
    if (matches.some((item) => item.state !== "attention")) continue;
    const failed = matches.find((item) => item.state === "attention");
    if (failed)
      return failed.failure?.message || "Couldn't save that move. Try again.";
  }
  return "";
}

/**
 * Column to paint. Outbox and confirmation win over a local phase recorded
 * before that entry is visible. A brief tick alone does not drop it; an
 * attention entry does.
 */
export function displayedPlacement(
  row,
  outbox,
  confirmations,
  localPhase = "",
) {
  const overlay = watchPlacement(row, outbox, confirmations);
  if (overlay) return overlay;
  if (watchMoveAttention(outbox, [row?.ref])) return "";
  if (!localPhase || localPhase === row?.placement) return "";
  const target = taskMoveTarget({ ref: row?.ref });
  const known = (outbox ?? []).some(
    (item) =>
      item?.target === target &&
      item.state !== "superseded" &&
      isTaskMove(item),
  );
  if (known) return "";
  if (confirmations?.[target] && isTaskMove(confirmations[target])) return "";
  return localPhase;
}

function phaseOf(entry) {
  return entry?.draft?.phase || entry?.request?.body?.column_key || "";
}

function anchorOf(entry) {
  return String(entry?.request?.body?.before_card_id ?? "").trim();
}

/**
 * One move at a time per initiative. A newer destination replaces one still
 * being read, and only the latest is enqueued. Watch never sends a positional
 * anchor: column order here is computed attention order, and a card from
 * another board is an anchor core rejects.
 */
export function createWatchMover({
  readWork,
  enqueue,
  entries,
  supersede,
  confirmExternal,
}) {
  /** @type {Map<string, { latest: { row: object, placement: string } | null, promise: Promise<object> }>} */
  const flights = new Map();

  function move(row, placement) {
    if (!row?.ref || !placement) return Promise.resolve({ status: "ignored" });
    const existing = flights.get(row.ref);
    if (existing) {
      existing.latest = { row, placement };
      return existing.promise;
    }
    if (placement === row.placement)
      return Promise.resolve({ status: "ignored" });
    const flight = { latest: null, promise: null };
    flights.set(row.ref, flight);
    flight.promise = drain(row, placement, flight).finally(() => {
      if (flights.get(row.ref) === flight) flights.delete(row.ref);
    });
    return flight.promise;
  }

  async function drain(row, placement, flight) {
    let current = { row, placement };
    let result = { status: "ignored" };
    while (current) {
      const requested = current;
      flight.latest = null;
      result = await submit(requested.row, requested.placement, flight);
      const next = flight.latest;
      flight.latest = null;
      if (
        next &&
        (result.status === "coalesced" ||
          next.placement !== requested.placement)
      ) {
        current = next;
        continue;
      }
      return result;
    }
    return result;
  }

  async function submit(row, placement, flight) {
    const latest = () => flight.latest;
    if (latest()) return { status: "coalesced" };
    if (placement === "done") return { status: "needs-evidence" };
    let work;
    let boardToken = "";
    try {
      const read = await readWork(row);
      if (latest()) return { status: "coalesced" };
      work = read?.work;
      boardToken = read?.boardToken || "";
      if (!work) return { status: "unreadable" };
    } catch (error) {
      return { status: "unreadable", error };
    }
    if (latest()) return { status: "coalesced" };

    const open = entries().filter(
      (entry) =>
        entry?.target === taskMoveTarget(work) && entry.state !== "superseded",
    );
    if (open.some((entry) => entry.state === "sending")) {
      if (latest()) return { status: "coalesced" };
      return { status: "busy", message: "That move is still being sent." };
    }
    const reusable = open.every(
      (entry) =>
        entry.state !== "attention" &&
        phaseOf(entry) === placement &&
        !anchorOf(entry),
    );
    if (open.length && reusable) return { status: "queued", placement };

    if (!isNexusOwned(work)) {
      if (!confirmExternal(work)) return { status: "cancelled" };
      if (latest()) return { status: "coalesced" };
    }

    const draft = taskMoveDraft(work, placement, {
      key: workKey(work),
      ref: "",
      boardToken,
      beforeCardId: "",
      approved: true,
    });
    let entry;
    try {
      entry = await enqueue(draft);
    } catch (error) {
      return { status: "failed", error };
    }
    const next = latest();
    if (next && next.placement !== placement) {
      // The latest destination has to be durable before the one we just
      // saved is retired, or a crash would drop the move the person chose.
      flight.latest = null;
      try {
        const saved = await enqueue(
          taskMoveDraft(work, next.placement, {
            key: workKey(work),
            ref: "",
            boardToken,
            beforeCardId: "",
            approved: true,
          }),
        );
        await supersede(entry.key, saved?.key || "");
        for (const old of open) {
          if (old.key === entry?.key || old.key === saved?.key) continue;
          await supersede(old.key, saved?.key || "");
        }
        const newer = latest();
        if (newer && newer.placement !== next.placement)
          return { status: "coalesced" };
        return { status: "queued", placement: next.placement };
      } catch (error) {
        return { status: "failed", error };
      }
    }
    for (const old of open) {
      if (old.key === entry?.key) continue;
      try {
        await supersede(old.key, entry?.key || "");
      } catch (error) {
        return { status: "failed", error };
      }
    }
    return { status: "queued", placement };
  }

  return { move };
}
