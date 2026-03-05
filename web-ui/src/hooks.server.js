import { env } from "$env/dynamic/private";

function normalizeBaseUrl(value) {
  return String(value ?? "")
    .trim()
    .replace(/\/+$/, "");
}

function shouldProxyToCore(pathname) {
  return (
    pathname.startsWith("/meta/") ||
    pathname === "/version" ||
    pathname === "/actors" ||
    pathname === "/threads" ||
    pathname.startsWith("/threads/") ||
    pathname === "/commitments" ||
    pathname.startsWith("/commitments/") ||
    pathname === "/artifacts" ||
    pathname.startsWith("/artifacts/") ||
    pathname === "/events" ||
    pathname.startsWith("/events/") ||
    pathname === "/work_orders" ||
    pathname.startsWith("/work_orders/") ||
    pathname === "/receipts" ||
    pathname.startsWith("/receipts/") ||
    pathname === "/reviews" ||
    pathname.startsWith("/reviews/") ||
    pathname === "/snapshots" ||
    pathname.startsWith("/snapshots/") ||
    pathname === "/derived/rebuild" ||
    pathname === "/inbox" ||
    pathname === "/inbox/ack"
  );
}

function isDocumentNavigationRequest(request) {
  const method = request.method.toUpperCase();
  if (method !== "GET" && method !== "HEAD") {
    return false;
  }

  const secFetchDest = request.headers.get("sec-fetch-dest");
  if (secFetchDest === "document") {
    return true;
  }

  const accept = request.headers.get("accept") ?? "";
  return accept.includes("text/html");
}

async function proxyToCore(event, coreBaseUrl) {
  const targetUrl = new URL(
    `${event.url.pathname}${event.url.search}`,
    `${coreBaseUrl}/`,
  ).toString();

  const headers = new Headers(event.request.headers);
  headers.delete("host");
  headers.delete("origin");

  const method = event.request.method.toUpperCase();
  const requestInit = {
    method,
    headers,
  };

  if (method !== "GET" && method !== "HEAD") {
    requestInit.body = event.request.body;
    requestInit.duplex = "half";
  }

  let upstreamResponse;
  try {
    upstreamResponse = await fetch(targetUrl, requestInit);
  } catch (error) {
    const reason = error instanceof Error ? error.message : String(error);
    return new Response(
      JSON.stringify({
        error: {
          code: "core_unreachable",
          message: `Unable to reach oar-core at ${coreBaseUrl}. Start backend with ../core/scripts/dev and retry.`,
          reason,
        },
      }),
      {
        status: 503,
        headers: {
          "content-type": "application/json",
        },
      },
    );
  }
  const responseHeaders = new Headers(upstreamResponse.headers);
  responseHeaders.delete("content-encoding");
  responseHeaders.delete("content-length");

  return new Response(upstreamResponse.body, {
    status: upstreamResponse.status,
    statusText: upstreamResponse.statusText,
    headers: responseHeaders,
  });
}

export async function handle({ event, resolve }) {
  const coreBaseUrl = normalizeBaseUrl(
    env.OAR_CORE_BASE_URL || env.PUBLIC_OAR_CORE_BASE_URL,
  );

  const pathname = event.url.pathname;
  const documentNavigation = isDocumentNavigationRequest(event.request);
  const shouldProxy =
    coreBaseUrl && shouldProxyToCore(pathname) && !documentNavigation;

  if (shouldProxy) {
    return proxyToCore(event, coreBaseUrl);
  }

  return resolve(event);
}
