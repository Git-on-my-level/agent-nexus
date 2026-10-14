import { get } from "svelte/store";
import { localWorkScope } from "$lib/localWorkScope.js";
import {
  listLocalWork,
  saveLocalWork,
  localWorkEpoch,
  localWorkPrincipal,
} from "$lib/localWorkStore.js";

const prefix = (scope, key) =>
  `anx:public-input:v1:${JSON.stringify([...JSON.parse(scope).slice(0, 2), "identity:create"])}:attempt:${key}:`;
async function attempts(scope, key) {
  if (localWorkPrincipal(scope)) {
    const rows = await listLocalWork(scope, "protocol-attempt");
    return rows
      .map((row) => row.value)
      .filter((row) => row?.requestKey === key);
  }
  const start = prefix(scope, key);
  return Object.keys(localStorage)
    .filter((name) => name.startsWith(start))
    .map((name) => JSON.parse(localStorage.getItem(name) || "null"))
    .filter(Boolean);
}
async function write(scope, attempt) {
  if (scope !== get(localWorkScope) || attempt.epoch !== localWorkEpoch(scope))
    throw new Error("Identity changed");
  if (localWorkPrincipal(scope))
    await saveLocalWork(
      scope,
      "protocol-attempt",
      `identity:${attempt.requestKey}:${attempt.id}`,
      attempt,
      { epoch: attempt.epoch },
    );
  else
    localStorage.setItem(
      prefix(scope, attempt.requestKey) + attempt.id,
      JSON.stringify(attempt),
    );
}
export async function identityAttemptUncertain(scope, key, fallback = false) {
  const rows = await attempts(scope, key);
  return rows.length ? rows.some((row) => row.state === "started") : fallback;
}
async function begin(scope, key, previouslyUncertain) {
  const epoch = localWorkEpoch(scope);
  const rows = await attempts(scope, key);
  if (scope !== get(localWorkScope) || epoch !== localWorkEpoch(scope))
    throw new Error("Identity changed");
  if (rows.some((row) => row.state === "retired"))
    throw new Error("This rejected request was edited in another tab");
  // Retain an older unknown send when adopting a journal without attempt rows.
  if (!rows.length && previouslyUncertain)
    await write(scope, {
      requestKey: key,
      id: "legacy",
      state: "started",
      epoch,
    });
  const attempt = {
    scope,
    requestKey: key,
    id: crypto.randomUUID(),
    state: "started",
    epoch,
  };
  await write(scope, attempt);
  return attempt;
}
export async function rejectIdentityAttempt(attempt) {
  await write(attempt.scope, { ...attempt, state: "rejected" });
  return identityAttemptUncertain(attempt.scope, attempt.requestKey);
}

function coordinate(scope, key, operation) {
  if (!navigator.locks)
    throw new Error("Safe identity coordination unavailable");
  return navigator.locks.request(`anx:identity:${scope}:${key}`, operation);
}
export function beginIdentityAttempt(scope, key, previouslyUncertain) {
  return coordinate(scope, key, () => begin(scope, key, previouslyUncertain));
}
export function retireIdentityRequest(scope, key, fallback) {
  return coordinate(scope, key, async () => {
    if (await identityAttemptUncertain(scope, key, fallback)) return false;
    await write(scope, {
      requestKey: key,
      id: "retired",
      state: "retired",
      epoch: localWorkEpoch(scope),
    });
    return true;
  });
}
