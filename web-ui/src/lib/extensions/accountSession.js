import { get, writable } from "svelte/store";

// Optional external account profile. The standalone workspace needs no account service.
export const hostedSession = writable({ phase: "unauthed", account: null });
export async function loadHostedSession() {
  return get(hostedSession);
}

export function initialsFor(account) {
  const name = String(account?.display_name ?? "").trim();
  const email = String(account?.email ?? "").trim();
  if (name) {
    const parts = name.split(/\s+/).filter(Boolean);
    return parts.length >= 2
      ? (parts[0][0] + parts[1][0]).toUpperCase()
      : name.slice(0, 2).toUpperCase();
  }
  return email ? email.slice(0, 2).toUpperCase() : "·";
}
