/**
 * Client rows for standing rulings and for sending an ask back. They live
 * beside `anxCoreClient.js` (which is at its size limit) and are spread into
 * its command table, so they get the same actor, routing and error handling
 * as every other method.
 */
export const rulingCommands = (q, b, pb, pathParams) => [
  ["rulingsList", "rulings.list", (filters) => ({ options: q(filters) })],
  ["rulingsCreate", "rulings.create", (payload) => ({ options: b(payload) })],
  [
    "rulingsRevoke",
    "rulings.revoke",
    (id) => pb(pathParams({ ruling_id: id }), {}),
  ],
  [
    "asksRetire",
    "asks.retire",
    (askRef, payload) => pb(pathParams({ ask_id: askRef }), payload),
  ],
];
