import { browser } from "$app/environment";

import {
  readHostedLaunchParams,
  sanitizeHostedReturnPath,
} from "$lib/hosted/launchFlow.js";

const HOSTED_OAUTH_STORAGE_KEY = "oar_hosted_oauth_continuations_v1";
const HOSTED_OAUTH_MAX_AGE_MS = 15 * 60 * 1000;

export function normalizeHostedOAuthMode(rawMode) {
  return String(rawMode ?? "signin").trim() === "signup" ? "signup" : "signin";
}

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
    mode: normalizeHostedOAuthMode(options.mode),
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
    mode: normalizeHostedOAuthMode(continuation?.mode),
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
    mode: normalizeHostedOAuthMode(value.mode),
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
  return normalizeHostedOAuthMode(continuation?.mode) === "signup"
    ? "/hosted/onboarding/organization"
    : "/hosted/dashboard";
}

export function buildHostedOAuthRecoveryPath(continuation) {
  const mode = normalizeHostedOAuthMode(continuation?.mode);
  const basePath = mode === "signup" ? "/hosted/signup" : "/hosted/signin";
  const params = new URLSearchParams();

  const next = normalizeHostedNextPath(continuation?.next);
  const workspaceSlug = String(continuation?.workspaceSlug ?? "").trim();
  const workspaceId = String(continuation?.workspaceId ?? "").trim();
  const returnPath = sanitizeHostedReturnPath(continuation?.returnPath, "/");
  const inviteToken = String(continuation?.inviteToken ?? "").trim();

  if (next) {
    params.set("next", next);
  }
  if (workspaceSlug) {
    params.set("workspace", workspaceSlug);
  }
  if (workspaceId) {
    params.set("workspace_id", workspaceId);
  }
  if (returnPath !== "/") {
    params.set("return_path", returnPath);
  }
  if (mode === "signup" && inviteToken) {
    params.set("invite", inviteToken);
  }

  return params.size > 0 ? `${basePath}?${params.toString()}` : basePath;
}

export async function readHostedOAuthError(response) {
  try {
    const payload = await response.json();
    return (
      payload?.error?.message ||
      payload?.error?.code ||
      response.statusText ||
      "Request failed."
    );
  } catch {
    return response.statusText || "Request failed.";
  }
}

export function friendlyHostedOAuthProviderError(rawError, rawMode) {
  const mode = normalizeHostedOAuthMode(rawMode);
  const normalized = String(rawError ?? "")
    .trim()
    .toLowerCase();
  if (
    normalized === "access_denied" ||
    normalized === "user_denied" ||
    normalized === "cancelled_on_user_request"
  ) {
    return mode === "signup"
      ? "Signup was canceled at the identity provider."
      : "Sign-in was canceled at the identity provider.";
  }
  return mode === "signup"
    ? "Signup could not be completed at the identity provider."
    : "Sign-in could not be completed at the identity provider.";
}

export async function startHostedOAuthFlow({
  cpFetch,
  provider,
  pageUrl,
  mode,
  inviteToken,
}) {
  const normalizedProvider = normalizeHostedOAuthProvider(provider);
  if (!normalizedProvider) {
    throw new Error("Unsupported OAuth provider.");
  }

  const start = await cpFetch(`account/oauth/${normalizedProvider}/start`, {
    method: "POST",
    body: "{}",
  });
  if (!start.ok) {
    throw new Error(await readHostedOAuthError(start));
  }

  const startBody = await start.json();
  const oauthSession = startBody?.oauth_session;
  const authorizationURL = String(oauthSession?.authorization_url ?? "").trim();
  const state = String(oauthSession?.state ?? "").trim();
  if (!authorizationURL || !state) {
    throw new Error("Unexpected response from control plane.");
  }

  storeHostedOAuthContinuation(
    state,
    buildHostedOAuthContinuation(pageUrl, {
      mode,
      inviteToken,
    }),
  );

  return {
    authorizationURL,
    state,
    provider: normalizedProvider,
  };
}

export async function createHostedLaunchSession({
  cpFetch,
  workspaceId,
  returnPath,
}) {
  const normalizedWorkspaceId = String(workspaceId ?? "").trim();
  if (!normalizedWorkspaceId) {
    throw new Error("Workspace continuation is missing a workspace id.");
  }

  const response = await cpFetch(
    `workspaces/${encodeURIComponent(normalizedWorkspaceId)}/launch-sessions`,
    {
      method: "POST",
      body: JSON.stringify({
        return_path: sanitizeHostedReturnPath(returnPath, "/"),
      }),
    },
  );

  if (!response.ok) {
    throw new Error(await readHostedOAuthError(response));
  }

  return response.json();
}
