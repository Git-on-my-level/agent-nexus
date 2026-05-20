import { browser } from "$app/environment";

const TOKEN_KEY = "anx_admin_token";
const ACTOR_KEY = "anx_admin_actor";

export function readAdminToken() {
  if (!browser) return "";
  return localStorage.getItem(TOKEN_KEY) ?? "";
}

export function readAdminActor() {
  if (!browser) return "";
  return localStorage.getItem(ACTOR_KEY) ?? "";
}

export function writeAdminCredentials(token, actor) {
  if (!browser) return;
  const cleanToken = String(token ?? "").trim();
  const cleanActor = String(actor ?? "").trim();
  if (cleanToken) localStorage.setItem(TOKEN_KEY, cleanToken);
  else localStorage.removeItem(TOKEN_KEY);
  if (cleanActor) localStorage.setItem(ACTOR_KEY, cleanActor);
  else localStorage.removeItem(ACTOR_KEY);
}

export function clearAdminCredentials() {
  if (!browser) return;
  localStorage.removeItem(TOKEN_KEY);
  localStorage.removeItem(ACTOR_KEY);
}

export function adminHeaders(token, actor) {
  const headers = { "x-anx-admin-token": String(token ?? "").trim() };
  const a = String(actor ?? "").trim();
  if (a) headers["x-anx-admin-actor"] = a;
  return headers;
}

export const ADMIN_TOKEN_STORAGE_KEY = TOKEN_KEY;
export const ADMIN_ACTOR_STORAGE_KEY = ACTOR_KEY;
