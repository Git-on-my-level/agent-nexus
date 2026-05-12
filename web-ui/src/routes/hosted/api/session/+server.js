import { dev } from "$app/environment";
import { json } from "@sveltejs/kit";

import {
  CP_ACCESS_TOKEN_COOKIE,
  CP_DEV_ACCESS_TOKEN_COOKIE,
  CP_TOKEN_MAX_AGE_SEC,
} from "$lib/hosted/cpSessionConstants.js";

function jsonError(status, code, message) {
  return json(
    {
      error: {
        code,
        message,
      },
    },
    { status },
  );
}

function enforceSameOriginMutation(event) {
  const origin = String(event.request.headers.get("origin") ?? "").trim();
  if (origin) {
    try {
      if (new URL(origin).origin !== event.url.origin) {
        return jsonError(
          403,
          "csrf_rejected",
          "Cross-origin session mutation rejected.",
        );
      }
    } catch {
      return jsonError(403, "csrf_rejected", "Invalid Origin header.");
    }
  }

  const fetchSite = String(event.request.headers.get("sec-fetch-site") ?? "")
    .trim()
    .toLowerCase();
  if (fetchSite && fetchSite !== "same-origin" && fetchSite !== "none") {
    return jsonError(
      403,
      "csrf_rejected",
      "Cross-site session mutation rejected.",
    );
  }

  return null;
}

function enforceSameOriginJsonMutation(event) {
  const originError = enforceSameOriginMutation(event);
  if (originError) {
    return originError;
  }
  const contentType = String(event.request.headers.get("content-type") ?? "")
    .toLowerCase()
    .split(";")[0]
    .trim();
  if (contentType !== "application/json") {
    return jsonError(
      415,
      "unsupported_media_type",
      "Expected application/json.",
    );
  }
  return null;
}

export async function POST(event) {
  if (dev) {
    return jsonError(
      403,
      "dev_session_client_only",
      "Hosted control-plane session cookies are set from the browser only in development.",
    );
  }

  const mutationError = enforceSameOriginJsonMutation(event);
  if (mutationError) {
    return mutationError;
  }

  let body;
  try {
    body = await event.request.json();
  } catch {
    return jsonError(400, "invalid_json", "Expected JSON body.");
  }

  const token = String(body?.access_token ?? "").trim();
  if (!token) {
    return jsonError(400, "missing_token", "access_token is required.");
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
  const mutationError = enforceSameOriginMutation(event);
  if (mutationError) {
    return mutationError;
  }

  event.cookies.delete(CP_ACCESS_TOKEN_COOKIE, { path: "/" });
  event.cookies.delete(CP_DEV_ACCESS_TOKEN_COOKIE, { path: "/" });
  return json({ ok: true });
}
