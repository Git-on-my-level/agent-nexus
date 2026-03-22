import { createControlClient } from "./controlClient.js";

const CONTROL_SESSION_COOKIE = "oar_control_session";
const CONTROL_ACCOUNT_COOKIE = "oar_control_account";
const CONTROL_INVITE_COOKIE = "oar_control_invite";

const controlSessionState = {
  accessToken: "",
  account: null,
};

function isSecureCookieRequest(event) {
  return event.url.protocol === "https:";
}

function buildControlCookieOptions(event) {
  return {
    httpOnly: true,
    sameSite: "lax",
    secure: isSecureCookieRequest(event),
    path: "/",
  };
}

export function getControlSessionCookieName() {
  return CONTROL_SESSION_COOKIE;
}

export function readControlInviteToken(event) {
  return event.cookies.get(CONTROL_INVITE_COOKIE) ?? "";
}

export function writeControlInviteToken(event, inviteToken) {
  const normalized = String(inviteToken ?? "").trim();
  if (!normalized) {
    clearControlInviteToken(event);
    return;
  }

  event.cookies.set(
    CONTROL_INVITE_COOKIE,
    normalized,
    buildControlCookieOptions(event),
  );
}

export function clearControlInviteToken(event) {
  event.cookies.delete(CONTROL_INVITE_COOKIE, { path: "/" });
}

export function readControlAccessToken(event) {
  return event.cookies.get(CONTROL_SESSION_COOKIE) ?? "";
}

export function writeControlAccessToken(event, accessToken) {
  const normalized = String(accessToken ?? "").trim();
  if (!normalized) {
    clearControlAccessToken(event);
    return;
  }

  event.cookies.set(
    CONTROL_SESSION_COOKIE,
    normalized,
    buildControlCookieOptions(event),
  );
  controlSessionState.accessToken = normalized;
}

export function clearControlAccessToken(event) {
  event.cookies.delete(CONTROL_SESSION_COOKIE, { path: "/" });
  controlSessionState.accessToken = "";
  controlSessionState.account = null;
}

export function readControlAccount(event) {
  const raw = event.cookies.get(CONTROL_ACCOUNT_COOKIE) ?? "";
  if (!raw) {
    return null;
  }

  try {
    return JSON.parse(raw);
  } catch {
    return null;
  }
}

export function writeControlAccount(event, account) {
  if (!account) {
    clearControlAccount(event);
    return;
  }

  const serialized = JSON.stringify({
    id: account.id,
    email: account.email,
    display_name: account.display_name,
    status: account.status,
    created_at: account.created_at,
    last_login_at: account.last_login_at,
  });

  event.cookies.set(
    CONTROL_ACCOUNT_COOKIE,
    serialized,
    buildControlCookieOptions(event),
  );
  controlSessionState.account = account;
}

export function clearControlAccount(event) {
  event.cookies.delete(CONTROL_ACCOUNT_COOKIE, { path: "/" });
  controlSessionState.account = null;
}

export function getControlSessionState() {
  return {
    accessToken: controlSessionState.accessToken,
    account: controlSessionState.account,
  };
}

export function setControlSessionState(accessToken, account) {
  controlSessionState.accessToken = accessToken || "";
  controlSessionState.account = account || null;
}

export function clearControlSessionState() {
  controlSessionState.accessToken = "";
  controlSessionState.account = null;
}

export function getControlClient(event) {
  const accessToken = readControlAccessToken(event);
  return createControlClient(accessToken);
}

export function isAuthenticatedControlSession(event) {
  const accessToken = readControlAccessToken(event);
  return Boolean(accessToken);
}

export async function loadControlAccount(event) {
  const accessToken = readControlAccessToken(event);
  if (!accessToken) {
    return null;
  }

  const cachedAccount = readControlAccount(event);
  if (cachedAccount) {
    setControlSessionState(accessToken, cachedAccount);
    return cachedAccount;
  }

  return null;
}

export async function startControlLogin(event, email) {
  const client = createControlClient();
  const response = await client.startSession({ email });
  return response;
}

export async function finishControlLogin(
  event,
  sessionId,
  credential,
  inviteToken = "",
) {
  const client = createControlClient();
  const response = await client.finishSession({
    session_id: sessionId,
    credential,
    ...(inviteToken ? { invite_token: inviteToken } : {}),
  });

  const account = response.account;
  const session = response.session;
  const accessToken = session?.access_token;

  if (accessToken) {
    writeControlAccessToken(event, accessToken);
  }

  if (account) {
    writeControlAccount(event, account);
    setControlSessionState(accessToken, account);
  }

  return { account, session };
}

export async function startControlRegistration(event, email, displayName) {
  const client = createControlClient();
  const response = await client.startPasskeyRegistration({
    email,
    display_name: displayName,
  });
  return response;
}

export async function finishControlRegistration(
  event,
  registrationSessionId,
  credential,
  inviteToken = "",
) {
  const client = createControlClient();
  const response = await client.finishPasskeyRegistration({
    registration_session_id: registrationSessionId,
    credential,
    ...(inviteToken ? { invite_token: inviteToken } : {}),
  });

  const account = response.account;
  const session = response.session;
  const accessToken = session?.access_token;

  if (accessToken) {
    writeControlAccessToken(event, accessToken);
  }

  if (account) {
    writeControlAccount(event, account);
    setControlSessionState(accessToken, account);
  }

  return { account, session };
}

export async function loadControlSession(event) {
  const accessToken = readControlAccessToken(event);
  const account = readControlAccount(event);

  if (!accessToken) {
    return null;
  }

  if (account) {
    try {
      const client = createControlClient(accessToken);
      await client.validateSession();
    } catch (error) {
      if (error?.status === 401) {
        clearControlAccessToken(event);
        clearControlAccount(event);
        clearControlSessionState();
        return null;
      }
    }
  }

  setControlSessionState(accessToken, account);
  return { accessToken, account };
}

export function isAuthenticated(event) {
  const accessToken = readControlAccessToken(event);
  return !!accessToken;
}

export async function logoutControlSession(event) {
  const accessToken = readControlAccessToken(event);

  if (accessToken) {
    try {
      const client = createControlClient(accessToken);
      await client.revokeCurrentSession();
    } catch {
      // Ignore errors during logout
    }
  }

  clearControlAccessToken(event);
  clearControlAccount(event);
  clearControlInviteToken(event);
  clearControlSessionState();
}
