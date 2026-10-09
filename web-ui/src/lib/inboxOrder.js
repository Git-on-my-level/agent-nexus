/** Keep arrival order after the first ranked paint, independently per mailbox. */
export function createInboxOrder() {
  const mailboxes = new Map();
  return (key, rows, staleExpanded) => {
    let state = mailboxes.get(key);
    if (!state) {
      state = {
        ids: [],
        late: new Set(),
        initial: rows.length,
        arrivals: 0,
        shownStale: false,
      };
      mailboxes.set(key, state);
    }
    const present = new Set(rows.map((row) => row.id));
    state.ids = state.ids.filter((id) => present.has(id));
    for (const id of state.late) if (!present.has(id)) state.late.delete(id);
    const known = new Set(state.ids);
    for (const row of rows) {
      if (known.has(row.id)) continue;
      // Once stale rows have been shown, arrivals also follow that group.
      // Otherwise adding a fresh row could move an expanded stale selection.
      if (state.shownStale) state.late.add(row.id);
      state.ids.push(row.id);
      state.arrivals += 1;
    }
    const byId = new Map(rows.map((row) => [row.id, row]));
    const ordered = state.ids.map((id) => byId.get(id)).filter(Boolean);
    const lateRows = ordered.filter(
      (row) => state.late.has(row.id) && !row.stale,
    );
    const grouped = ordered.filter(
      (row) => !state.late.has(row.id) || row.stale,
    );
    const staleRows = grouped.filter((row) => row.stale);
    if (staleExpanded && staleRows.length) state.shownStale = true;
    return {
      currentRows: grouped.filter((row) => !row.stale),
      staleRows,
      lateRows,
      added: Math.max(0, state.arrivals - state.initial),
    };
  };
}
