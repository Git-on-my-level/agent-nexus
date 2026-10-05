/**
 * Which reads are still worth applying.
 *
 * A surface that polls while the reader decides has two kinds of stale read:
 * one that started before the decision, and one that started after it began
 * but before it settled. Both saw the world without the outcome, so applying
 * either puts the decided row back with its controls live, under a notice
 * saying it is done.
 *
 * The counter therefore moves twice per decision, on start and on settle, and
 * a read captures it before its first await:
 *
 *   const epoch = decisions.current();
 *   const result = await read();
 *   if (decisions.isStale(epoch)) return;
 *
 * Reads issued after a decision settles capture the new value and are kept,
 * which is what lets the reconciling read run straight after `during`.
 *
 * `invalidate` covers the other reason an outstanding read stops being worth
 * applying: the reader changed. A response fetched for one principal must
 * never land in a page now being read by another.
 */
export function createDecisionEpoch() {
  let epoch = 0;
  return {
    /** Capture before the first await of a read. */
    current: () => epoch,
    /** True when something invalidating happened while the read was out. */
    isStale: (captured) => captured !== epoch,
    /** Discard every read currently in flight. */
    invalidate() {
      epoch += 1;
    },
    /**
     * Run a decision, invalidating every read that overlapped it.
     *
     * @template T
     * @param {() => Promise<T>} decide
     * @returns {Promise<T>}
     */
    async during(decide) {
      epoch += 1;
      try {
        return await decide();
      } finally {
        epoch += 1;
      }
    },
  };
}
