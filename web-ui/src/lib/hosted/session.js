/**
 * Hosted SaaS session helpers — small client-side store for the signed-in
 * account and the active organization. Used by the hosted shell to render
 * the user menu, org switcher, and auth-aware navigation.
 */

import { writable, get } from "svelte/store";

import { browser } from "$app/environment";

import {
  clearHostedCpAccessToken,
  hostedCpFetch,
} from "$lib/hosted/cpFetch.js";

const ACTIVE_ORG_KEY = "oar_hosted_active_org_id";

/** @typedef {{ id: string, email?: string, display_name?: string }} HostedAccount */
/** @typedef {{ id: string, slug: string, display_name: string, plan_tier?: string }} HostedOrganization */

/** @type {import('svelte/store').Writable<{
 *   phase: 'idle'|'loading'|'authed'|'unauthed'|'error',
 *   account: HostedAccount|null,
 *   organizations: HostedOrganization[],
 *   activeOrgId: string,
 *   error: string,
 * }>} */
export const hostedSession = writable({
  phase: "idle",
  account: null,
  organizations: [],
  activeOrgId: "",
  error: "",
});

function readActiveOrgFromStorage() {
  if (!browser) return "";
  try {
    return String(window.localStorage.getItem(ACTIVE_ORG_KEY) ?? "").trim();
  } catch {
    return "";
  }
}

function writeActiveOrgToStorage(orgId) {
  if (!browser) return;
  try {
    if (orgId) {
      window.localStorage.setItem(ACTIVE_ORG_KEY, orgId);
    } else {
      window.localStorage.removeItem(ACTIVE_ORG_KEY);
    }
  } catch {
    // ignore quota / privacy mode
  }
}

async function readError(res) {
  try {
    const j = await res.json();
    return j?.error?.message || j?.error?.code || res.statusText;
  } catch {
    return res.statusText;
  }
}

/**
 * Load (or refresh) the signed-in account + organizations.
 * Returns the resolved store snapshot for callers that need it inline.
 */
export async function loadHostedSession() {
  hostedSession.update((s) => ({ ...s, phase: "loading", error: "" }));

  let me = null;
  try {
    const res = await hostedCpFetch("account/me");
    if (res.status === 401) {
      hostedSession.set({
        phase: "unauthed",
        account: null,
        organizations: [],
        activeOrgId: "",
        error: "",
      });
      return get(hostedSession);
    }
    if (!res.ok) {
      hostedSession.update((s) => ({
        ...s,
        phase: "error",
        error: `Could not load account (${res.status}).`,
      }));
      return get(hostedSession);
    }
    const body = await res.json();
    me = body?.account ?? body ?? null;
  } catch {
    // Some control plane builds may not expose /account/me — fall back to
    // listing organizations as a liveness probe and synthesise a placeholder.
    me = null;
  }

  let organizations = [];
  try {
    const orgRes = await hostedCpFetch("organizations?limit=100");
    if (orgRes.status === 401) {
      hostedSession.set({
        phase: "unauthed",
        account: null,
        organizations: [],
        activeOrgId: "",
        error: "",
      });
      return get(hostedSession);
    }
    if (!orgRes.ok) {
      const errMsg = await readError(orgRes);
      hostedSession.update((s) => ({
        ...s,
        phase: "error",
        error: errMsg,
      }));
      return get(hostedSession);
    }
    const body = await orgRes.json();
    organizations = Array.isArray(body?.organizations)
      ? body.organizations
      : [];
  } catch (err) {
    hostedSession.update((s) => ({
      ...s,
      phase: "error",
      error:
        err instanceof Error ? err.message : "Failed to load organizations.",
    }));
    return get(hostedSession);
  }

  const stored = readActiveOrgFromStorage();
  let activeOrgId = "";
  if (stored && organizations.some((o) => String(o.id) === stored)) {
    activeOrgId = stored;
  } else if (organizations.length > 0) {
    activeOrgId = String(organizations[0].id ?? "");
    writeActiveOrgToStorage(activeOrgId);
  }

  hostedSession.set({
    phase: "authed",
    account: me,
    organizations,
    activeOrgId,
    error: "",
  });
  return get(hostedSession);
}

/** Set the active organization (persists to localStorage). */
export function setActiveOrg(orgId) {
  const id = String(orgId ?? "").trim();
  hostedSession.update((s) => ({ ...s, activeOrgId: id }));
  writeActiveOrgToStorage(id);
}

/** Sign out — clears server session cookie + local token + store. */
export async function signOutHostedSession() {
  try {
    await hostedCpFetch("account/sessions/current", { method: "DELETE" });
  } catch {
    // best-effort
  }
  clearHostedCpAccessToken();
  writeActiveOrgToStorage("");
  hostedSession.set({
    phase: "unauthed",
    account: null,
    organizations: [],
    activeOrgId: "",
    error: "",
  });
}

/** Generate human initials from a display name or email. */
export function initialsFor(account) {
  const name = String(account?.display_name ?? "").trim();
  const email = String(account?.email ?? "").trim();
  const source = name || email;
  if (!source) return "·";
  if (name) {
    const parts = name.split(/\s+/).filter(Boolean);
    if (parts.length >= 2) {
      return (parts[0][0] + parts[1][0]).toUpperCase();
    }
    return name.slice(0, 2).toUpperCase();
  }
  return email.slice(0, 2).toUpperCase();
}
