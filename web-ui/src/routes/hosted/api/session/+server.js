import { dev } from "$app/environment";
import { json } from "@sveltejs/kit";

import {
  CP_ACCESS_TOKEN_COOKIE,
  CP_DEV_ACCESS_TOKEN_COOKIE,
  CP_TOKEN_MAX_AGE_SEC,
} from "$lib/hosted/cpSessionConstants.js";

export async function POST(event) {
  if (dev) {
    return json(
      {
        error: {
          code: "dev_session_client_only",
          message:
            "Hosted control-plane session cookies are set from the browser only in development.",
        },
      },
      { status: 403 },
    );
  }

  let body;
  try {
    body = await event.request.json();
  } catch {
    return json(
      {
        error: {
          code: "invalid_json",
          message: "Expected JSON body.",
        },
      },
      { status: 400 },
    );
  }

  const token = String(body?.access_token ?? "").trim();
  if (!token) {
    return json(
      {
        error: {
          code: "missing_token",
          message: "access_token is required.",
        },
      },
      { status: 400 },
    );
  }

  // Non-dev only (see `if (dev)` above). Do not infer Secure from
  // `event.url.protocol`: behind TLS termination the app often sees `http:`
  // on the internal hop, which would clear Secure and allow insecure delivery.
  event.cookies.set(CP_ACCESS_TOKEN_COOKIE, token, {
    path: "/",
    maxAge: CP_TOKEN_MAX_AGE_SEC,
    httpOnly: true,
    sameSite: "lax",
    secure: true,
  });
  event.cookies.delete(CP_DEV_ACCESS_TOKEN_COOKIE, { path: "/" });

  return json({ ok: true });
}

export async function DELETE(event) {
  event.cookies.delete(CP_ACCESS_TOKEN_COOKIE, { path: "/" });
  event.cookies.delete(CP_DEV_ACCESS_TOKEN_COOKIE, { path: "/" });
  return json({ ok: true });
}
