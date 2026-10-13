import { get, writable } from "svelte/store";

import { initialsOf } from "$lib/avatarModel.js";

// Optional external account profile. The standalone workspace needs no account service.
export const hostedSession = writable({ phase: "unauthed", account: null });
export async function loadHostedSession() {
  return get(hostedSession);
}

export function initialsFor(account) {
  const name = String(account?.display_name ?? "").trim();
  const email = String(account?.email ?? "").trim();
  if (name) return initialsOf(name);
  return email ? email.slice(0, 2).toUpperCase() : "·";
}
