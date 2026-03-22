import { json } from "@sveltejs/kit";

import { getControlBaseUrl } from "$lib/server/controlClient.js";
import { readControlAccessToken } from "$lib/server/controlSession.js";

const ABSOLUTE_TARGET_PATTERN = /^[a-z][a-z0-9+.-]*:/i;

function buildProxyHeaders(event) {
  const headers = new Headers();
  const contentType = event.request.headers.get("content-type");
  const accept = event.request.headers.get("accept");
  const accessToken = readControlAccessToken(event);

  if (accept) {
    headers.set("accept", accept);
  }
  if (contentType) {
    headers.set("content-type", contentType);
  }
  if (accessToken) {
    headers.set("authorization", `Bearer ${accessToken}`);
  }

  return headers;
}

async function proxyControlRequest(event) {
  const segments = String(event.params.segments ?? "").replace(/^\/+/, "");
  if (!segments) {
    return json(
      {
        error: {
          code: "invalid_request",
          message: "Missing control API path.",
        },
      },
      { status: 400 },
    );
  }
  if (ABSOLUTE_TARGET_PATTERN.test(segments)) {
    return json(
      {
        error: {
          code: "invalid_request",
          message: "Control API path must be relative.",
        },
      },
      { status: 400 },
    );
  }

  const accessToken = readControlAccessToken(event);
  if (!accessToken) {
    return json(
      {
        error: {
          code: "unauthorized",
          message: "Control session is required.",
        },
      },
      { status: 401 },
    );
  }

  const controlBaseUrl = new URL(getControlBaseUrl());
  const basePath = controlBaseUrl.pathname.endsWith("/")
    ? controlBaseUrl.pathname
    : `${controlBaseUrl.pathname}/`;
  const targetUrl = new URL(controlBaseUrl);
  targetUrl.pathname = `${basePath}${segments}`;
  targetUrl.search = event.url.search;
  if (
    targetUrl.origin !== controlBaseUrl.origin ||
    !targetUrl.pathname.startsWith(basePath)
  ) {
    return json(
      {
        error: {
          code: "invalid_request",
          message: "Control API path must stay within the control plane.",
        },
      },
      { status: 400 },
    );
  }
  const headers = buildProxyHeaders(event);
  let body;
  if (event.request.method !== "GET" && event.request.method !== "HEAD") {
    const payload = await event.request.arrayBuffer();
    body = payload.byteLength > 0 ? payload : undefined;
  }

  const upstream = await fetch(targetUrl, {
    method: event.request.method,
    headers,
    body,
  });

  const responseHeaders = new Headers(upstream.headers);
  responseHeaders.delete("content-length");
  responseHeaders.delete("content-encoding");
  responseHeaders.set("cache-control", "no-store");

  return new Response(upstream.body, {
    status: upstream.status,
    statusText: upstream.statusText,
    headers: responseHeaders,
  });
}

export const GET = proxyControlRequest;
export const POST = proxyControlRequest;
export const PATCH = proxyControlRequest;
export const DELETE = proxyControlRequest;
