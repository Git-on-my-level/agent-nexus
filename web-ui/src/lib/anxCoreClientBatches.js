/**
 * Client rows for the batch lifecycle routes. They live beside
 * `anxCoreClient.js` (which is at its size limit) and are spread into its
 * command table, so they get the same routing and error handling as every
 * other method. Item bodies already name their actor, exactly as the single
 * lifecycle calls send them, so nothing is injected here.
 */
export const batchCommands = (b) => [
  [
    "changeEventsLifecycle",
    "events.lifecycle_many",
    (items, selectedBases) => ({
      options: b({
        items,
        ...(selectedBases ? { selected_bases: selectedBases } : {}),
      }),
    }),
  ],
  [
    "changeDocumentsLifecycle",
    "docs.lifecycle_many",
    (items) => ({ options: b({ items }) }),
  ],
];
