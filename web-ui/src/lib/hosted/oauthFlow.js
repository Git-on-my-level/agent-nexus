import { browser } from "$app/environment";

import {
  readHostedLaunchParams,
  sanitizeHostedReturnPath,
} from "$lib/hosted/launchFlow.js";

const HOSTED_OAUTH_STORAGE_KEY = "oar_hosted_oauth_continuations_v1";
const HOSTED_OAUTH_MAX_AGE_MS = 15 * 60 * 1000;

export function normalizeHostedOAuthProvider(rawProvider) {
  const provider = String(rawProvider ?? "")
    .trim()
    .toLowerCase();
  return provider === "google" || provider === "github" ? provider : "";
}

export function hostedOAuthCallbackPath(rawProvider) {
  const provider = normalizeHostedOAuthProvider(rawProvider);
  return provider ? `/hosted/oauth/${provider}/callback` : "";
}

export function deriveHostedOAuthRedirectURI(rawProvider, locationLike) {
  const callbackPath = hostedOAuthCallbackPath(rawProvider);
  if (!callbackPath) {
    return "";
  }
  const candidate = locationLike ?? (browser ? window.location : null);
  const origin = String(candidate?.origin ?? "").trim();
  const pathname = String(candidate?.pathname ?? "").trim() || callbackPath;
  if (!origin) {
    return pathname;
  }
  return `${origin}${pathname}`;
}

function normalizeHostedNextPath(rawValue) {
  const candidate = String(rawValue ?? "").trim();
  if (!candidate) {
    return "";
  }
  return sanitizeHostedReturnPath(candidate, "");
}

export function buildHostedOAuthContinuation(urlLike, options = {}) {
  const url =
    urlLike instanceof URL
      ? urlLike
      : new URL(String(urlLike ?? ""), "http://localhost");
  const launch = readHostedLaunchParams(url.searchParams);
  return {
    mode:
      String(options.mode ?? "signin").trim() === "signup"
        ? "signup"
        : "signin",
    next: normalizeHostedNextPath(url.searchParams.get("next")),
    workspaceSlug: launch.workspaceSlug,
    workspaceId: launch.workspaceId,
    returnPath: launch.returnPath,
    inviteToken: String(options.inviteToken ?? "").trim(),
  };
}

function readHostedOAuthStorage() {
  if (!browser) {
    return {};
  }
  try {
    const raw = window.sessionStorage.getItem(HOSTED_OAUTH_STORAGE_KEY);
    if (!raw) {
      return {};
    }
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch {
    return {};
  }
}

function writeHostedOAuthStorage(value) {
  if (!browser) {
    return;
  }
  try {
    const entries = Object.entries(value ?? {});
    if (entries.length === 0) {
      window.sessionStorage.removeItem(HOSTED_OAUTH_STORAGE_KEY);
      return;
    }
    window.sessionStorage.setItem(
      HOSTED_OAUTH_STORAGE_KEY,
      JSON.stringify(Object.fromEntries(entries)),
    );
  } catch {
    // Best effort only.
  }
}

function pruneHostedOAuthStorage(storage, now = Date.now()) {
  return Object.fromEntries(
    Object.entries(storage ?? {}).filter(([, value]) => {
      const savedAt = Number(value?.savedAt ?? 0);
      return (
        Number.isFinite(savedAt) && now - savedAt < HOSTED_OAUTH_MAX_AGE_MS
      );
    }),
  );
}

export function storeHostedOAuthContinuation(state, continuation) {
  const normalizedState = String(state ?? "").trim();
  if (!browser || !normalizedState) {
    return;
  }
  const storage = pruneHostedOAuthStorage(readHostedOAuthStorage());
  storage[normalizedState] = {
    mode:
      String(continuation?.mode ?? "signin").trim() === "signup"
        ? "signup"
        : "signin",
    next: normalizeHostedNextPath(continuation?.next),
    workspaceSlug: String(continuation?.workspaceSlug ?? "").trim(),
    workspaceId: String(continuation?.workspaceId ?? "").trim(),
    returnPath: sanitizeHostedReturnPath(continuation?.returnPath, "/"),
    inviteToken: String(continuation?.inviteToken ?? "").trim(),
    savedAt: Date.now(),
  };
  writeHostedOAuthStorage(storage);
}

export function readHostedOAuthContinuation(state) {
  const normalizedState = String(state ?? "").trim();
  if (!browser || !normalizedState) {
    return null;
  }
  const storage = pruneHostedOAuthStorage(readHostedOAuthStorage());
  const value = storage[normalizedState];
  if (!value || typeof value !== "object") {
    return null;
  }
  return {
    mode:
      String(value.mode ?? "signin").trim() === "signup" ? "signup" : "signin",
    next: normalizeHostedNextPath(value.next),
    workspaceSlug: String(value.workspaceSlug ?? "").trim(),
    workspaceId: String(value.workspaceId ?? "").trim(),
    returnPath: sanitizeHostedReturnPath(value.returnPath, "/"),
    inviteToken: String(value.inviteToken ?? "").trim(),
  };
}

export function clearHostedOAuthContinuation(state) {
  const normalizedState = String(state ?? "").trim();
  if (!browser || !normalizedState) {
    return;
  }
  const storage = pruneHostedOAuthStorage(readHostedOAuthStorage());
  delete storage[normalizedState];
  writeHostedOAuthStorage(storage);
}

export function resolveHostedPostAuthPath(continuation) {
  const next = normalizeHostedNextPath(continuation?.next);
  if (next) {
    return next;
  }
  return String(continuation?.mode ?? "").trim() === "signup"
    ? "/hosted/onboarding/organization"
    : "/hosted/dashboard";
}
